// Package server implements the AgentRuntime gRPC service defined in
// proto/agent/runtime/v1. This is a runnable skeleton: Run / RunStream /
// RunAgent / RunAgentStream / HealthCheck are backed by an eino chat model,
// while Resume / Stop return Unimplemented until the full engine lands.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"
	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/dispatcher"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/memory"
	"github.com/good-fish-man/agent-runtime/internal/provider"
	"github.com/good-fish-man/agent-runtime/internal/research"
	"github.com/good-fish-man/agent-runtime/internal/tools"
	"github.com/good-fish-man/agent-runtime/internal/types"
	protocol "github.com/good-fish-man/athena-protocol/protocol/v5"
	log "github.com/good-fish-man/logx"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Config configures the server's default model (used when a request omits models).
type Config struct {
	DefaultModel eino.ModelConfig

	// Memory is optional. When Store is non-nil the server injects memory into
	// the system prompt and mines new memories after each response.
	Store        *memory.MemStore
	Reviewer     *memory.BackgroundReviewer
	InjectMemory bool

	// Dispatch carries operator-level defaults (sandbox / skills) into each run.
	Dispatch dispatcher.Config
}

// Server implements runtimev1.AgentRuntimeServer.
type Server struct {
	runtimev1.UnimplementedAgentRuntimeServer
	cfg Config
}

// New creates a Server.
func New(cfg Config) *Server { return &Server{cfg: cfg} }

// ---- helpers ----

func newTraceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("t-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// resolveTraceID applies precedence: request field > metadata x-trace-id/traceparent > generated.
func resolveTraceID(ctx context.Context, fromRequest string) string {
	if fromRequest != "" {
		return fromRequest
	}
	if value := log.ReqID(ctx); value != "" {
		return value
	}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(constant.MetaKeyTraceID); len(v) > 0 && v[0] != "" {
			return v[0]
		}
		if v := md.Get(constant.MetaKeyTraceparent); len(v) > 0 && v[0] != "" {
			// traceparent = version-traceid-spanid-flags
			parts := strings.Split(v[0], "-")
			if len(parts) >= 2 && parts[1] != "" {
				return parts[1]
			}
		}
	}
	return newTraceID()
}

// bindTrace resolves the request trace_id and returns the context that must be
// propagated to every downstream operation.
func (s *Server) bindTrace(ctx context.Context, fromRequest string) (context.Context, string) {
	traceID := resolveTraceID(ctx, fromRequest)
	return log.WithReqID(ctx, traceID), traceID
}

// modelConfig picks the request's default model, else the server default.
func (s *Server) modelConfig(models map[string]*runtimev1.ModelConfig) (eino.ModelConfig, error) {
	if m, ok := models[constant.ModelRoleDefault]; ok && m != nil && m.Name != "" {
		return fromProtoModel(m), nil
	}
	for _, role := range []string{"rewrite", "skill", "summarize"} {
		if m := models[role]; m != nil && m.Name != "" {
			return fromProtoModel(m), nil
		}
	}
	if s.cfg.DefaultModel.Name != "" {
		return s.cfg.DefaultModel, nil
	}
	return eino.ModelConfig{}, status.Errorf(codes.InvalidArgument,
		"no model configured: provide models[%q] or set %s env", constant.ModelRoleDefault, constant.EnvDefaultModel)
}

func fromProtoModel(m *runtimev1.ModelConfig) eino.ModelConfig {
	return eino.ModelConfig{
		Provider:    m.GetProvider(),
		Name:        m.GetName(),
		APIKey:      m.GetApiKey(),
		APIBase:     m.GetApiBase(),
		Temperature: m.GetTemperature(),
		MaxTokens:   int(m.GetMaxTokens()),
		TopP:        m.GetTopP(),
		ExtraFields: protoExtraFields(m),
	}
}

func fromProtoToolModel(m *runtimev1.ModelConfig) types.ModelConfig {
	if m == nil {
		return types.ModelConfig{}
	}
	return types.ModelConfig{
		Provider: m.GetProvider(), Name: m.GetName(), APIKey: m.GetApiKey(), APIBase: m.GetApiBase(),
		Temperature: m.GetTemperature(), MaxTokens: int(m.GetMaxTokens()), TopP: m.GetTopP(), ExtraFields: protoExtraFields(m),
	}
}

func fromProtoMessages(msgs []*runtimev1.ChatMessage) []eino.ChatMessage {
	out := make([]eino.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, eino.ChatMessage{Role: m.GetRole(), Content: m.GetContent()})
	}
	return out
}

// ---- memory helpers ----

// memoryScope extracts session/user/agent IDs from a request context Struct.
func memoryScope(ctx *structpb.Struct) (sessionID, userID, agentID string) {
	if ctx == nil {
		return "", "", ""
	}
	fields := ctx.GetFields()
	get := func(k string) string {
		if v, ok := fields[k]; ok {
			return v.GetStringValue()
		}
		return ""
	}
	return get(constant.ContextKeySessionID), get(constant.ContextKeyUserID), get(constant.ContextKeyAgentID)
}

func providerTaskID(requestContext *structpb.Struct, requestID, traceID string) string {
	if requestContext != nil {
		for _, key := range []string{"task_id", "goal_id", "session_id"} {
			if value, ok := requestContext.GetFields()[key]; ok && strings.TrimSpace(value.GetStringValue()) != "" {
				return strings.TrimSpace(value.GetStringValue())
			}
		}
	}
	if strings.TrimSpace(requestID) != "" {
		return strings.TrimSpace(requestID)
	}
	return traceID
}

// projectDir extracts the working directory for filesystem/shell tools from the
// request context (context.project_dir). Returns "." when unset.
func projectDir(ctx *structpb.Struct) string {
	if ctx == nil {
		return "."
	}
	if v, ok := ctx.GetFields()[constant.ContextKeyProjectDir]; ok {
		if dir := v.GetStringValue(); dir != "" {
			return dir
		}
	}
	return "."
}

// memoryInstruction initializes the request's memory scopes and returns the
// formatted memory block for injection into the agent instruction.
func (s *Server) memoryInstruction(ctx context.Context, reqCtx *structpb.Struct) (instruction, sessionID, userID, agentID string) {
	if s.cfg.Store == nil || !s.cfg.InjectMemory {
		sessionID, userID, agentID = memoryScope(reqCtx)
		return "", sessionID, userID, agentID
	}
	sessionID, userID, agentID = memoryScope(reqCtx)
	if sessionID == "" && userID == "" && agentID == "" {
		return "", sessionID, userID, agentID
	}
	if err := s.cfg.Store.InitializeAll(ctx, sessionID, userID, agentID); err != nil {
		// Degrade gracefully: no memory block on failure.
		return "", sessionID, userID, agentID
	}
	return memory.LoadMemorySection(s.cfg.Store, sessionID, userID, agentID), sessionID, userID, agentID
}

// reviewMemories launches async memory extraction after a response.
func (s *Server) reviewMemories(ctx context.Context, model eino.ModelConfig, sessionID, userID, agentID, userInput, assistantOutput string) {
	if s.cfg.Reviewer == nil {
		return
	}
	s.cfg.Reviewer.ReviewIfNeeded(ctx, model, sessionID, userID, agentID, userInput, assistantOutput)
}

// currentMemories returns the session's stored memories as proto messages.
func (s *Server) currentMemories(ctx context.Context, sessionID string) []*runtimev1.MemoryEntry {
	if s.cfg.Store == nil || sessionID == "" {
		return nil
	}
	entries, err := s.cfg.Store.GetAll(ctx, memory.EntryTypeSession, sessionID)
	if err != nil || len(entries) == 0 {
		return nil
	}
	out := make([]*runtimev1.MemoryEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, &runtimev1.MemoryEntry{
			Name:        e.Name,
			Description: e.Description,
			Type:        memoryKindToProto(e.MemoryKind),
			Content:     e.Content,
			Importance:  int32(e.Importance),
		})
	}
	return out
}

func memoryKindToProto(kind string) runtimev1.MemoryType {
	switch kind {
	case memory.MemoryKindUser:
		return runtimev1.MemoryType_MEMORY_TYPE_USER
	case memory.MemoryKindFeedback:
		return runtimev1.MemoryType_MEMORY_TYPE_FEEDBACK
	case memory.MemoryKindProject:
		return runtimev1.MemoryType_MEMORY_TYPE_PROJECT
	case memory.MemoryKindReference:
		return runtimev1.MemoryType_MEMORY_TYPE_REFERENCE
	default:
		return runtimev1.MemoryType_MEMORY_TYPE_UNSPECIFIED
	}
}

// ---- unary RPCs ----

// Run performs a non-streaming completion.
func (s *Server) Run(ctx context.Context, req *runtimev1.RunRequest) (*runtimev1.RunResponse, error) {
	ctx, traceID := s.bindTrace(ctx, req.GetTraceId())
	ctx, usageCollector := eino.WithUsageCollector(ctx)
	log.Infow(ctx, "run", "prompt_len", len(req.GetPrompt()), "messages", len(req.GetMessages()), "stream", false)
	mc, err := s.modelConfig(req.GetModels())
	if err != nil {
		return nil, log.WrapError(err, "Server.Run.modelConfig")
	}
	client, err := eino.NewClient(ctx, mc)
	if err != nil {
		return nil, log.GRPCError(err, codes.Internal, "Server.Run.NewClient", "init model")
	}
	instruction, sessionID, userID, agentID := s.memoryInstruction(ctx, req.GetContext())
	ctx = provider.WithInvocationScope(ctx, userID, providerTaskID(req.GetContext(), req.GetRequestId(), traceID))
	disp := s.newRunDispatcher(ctx, client, req, instruction)
	start := time.Now()
	res, err := disp.Run(ctx, req.GetPrompt(), fromProtoMessages(req.GetMessages()))
	if err != nil {
		return nil, log.GRPCError(err, codes.Unavailable, "Server.Run.Dispatch", "model call failed")
	}
	s.reviewMemories(ctx, mc, sessionID, userID, agentID, req.GetPrompt(), res.Content)
	responseMetadata := buildResponseMetadata(client.Name(), time.Since(start).Milliseconds(), res.Usage, usageCollector)
	return &runtimev1.RunResponse{
		Content:      res.Content,
		FinishReason: res.FinishReason,
		TokensUsed:   responseMetadata.TokensUsed,
		TraceId:      traceID,
		Memories:     s.currentMemories(ctx, sessionID),
		Metadata:     responseMetadata,
	}, nil
}

// RunAgent performs a non-streaming autonomous task (natural language input).
func (s *Server) RunAgent(ctx context.Context, req *runtimev1.AgentRequest) (*runtimev1.AgentResponse, error) {
	ctx, traceID := s.bindTrace(ctx, req.GetTraceId())
	ctx, usageCollector := eino.WithUsageCollector(ctx)
	log.Infow(ctx, "agent", "task_len", len(req.GetTask()), "stream", false)
	if req.GetTask() == "" {
		return nil, status.Error(codes.InvalidArgument, "task is required")
	}
	mc, err := s.modelConfig(req.GetModels())
	if err != nil {
		return nil, log.WrapError(err, "Server.RunAgent.modelConfig")
	}
	client, err := eino.NewClient(ctx, mc)
	if err != nil {
		return nil, log.GRPCError(err, codes.Internal, "Server.RunAgent.NewClient", "init model")
	}
	instruction, sessionID, userID, agentID := s.memoryInstruction(ctx, req.GetContext())
	ctx = provider.WithInvocationScope(ctx, userID, providerTaskID(req.GetContext(), req.GetRequestId(), traceID))
	disp := s.newAgentDispatcher(ctx, client, req, instruction)
	start := time.Now()
	res, err := disp.Run(ctx, req.GetTask(), nil)
	if err != nil {
		return nil, log.GRPCError(err, codes.Unavailable, "Server.RunAgent.Dispatch", "model call failed")
	}
	s.reviewMemories(ctx, mc, sessionID, userID, agentID, req.GetTask(), res.Content)
	responseMetadata := buildResponseMetadata(client.Name(), time.Since(start).Milliseconds(), res.Usage, usageCollector)
	return &runtimev1.AgentResponse{
		Content:      res.Content,
		FinishReason: res.FinishReason,
		TokensUsed:   responseMetadata.TokensUsed,
		TraceId:      traceID,
		Metadata:     responseMetadata,
	}, nil
}

// GenerateMedia invokes a user-selected media model directly, without an LLM
// deciding whether or how to call the model.
func (s *Server) GenerateMedia(ctx context.Context, req *runtimev1.MediaGenerationRequest) (*runtimev1.MediaGenerationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "media request is required")
	}
	ctx, traceID := s.bindTrace(ctx, req.GetTraceId())
	if req.GetModel() == nil || strings.TrimSpace(req.GetModel().GetName()) == "" {
		return nil, status.Error(codes.InvalidArgument, "media model is required")
	}
	if operation := strings.ToLower(strings.TrimSpace(req.GetOperation())); operation != "" && operation != "generate" {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported media operation %q", operation)
	}

	model := fromProtoToolModel(req.GetModel())
	response := &runtimev1.MediaGenerationResponse{MediaType: strings.ToLower(req.GetMediaType()), TraceId: traceID}
	switch response.MediaType {
	case "image":
		url, err := tools.GenerateImage(ctx, model, tools.ImageGenerationRequest{
			Prompt: req.GetPrompt(), NegativePrompt: req.GetNegativePrompt(), SourceURL: req.GetSourceUrl(), Size: req.GetSize(), Quality: req.GetQuality(),
		})
		if err != nil {
			return nil, log.GRPCError(err, codes.Internal, "Server.GenerateMedia.GenerateImage", "generate image")
		}
		response.MediaUrl, response.MimeType = url, "image/png"
	case "video":
		result, err := tools.GenerateVideo(ctx, model, tools.VideoGenerationRequest{
			Prompt: req.GetPrompt(), SourceURL: req.GetSourceUrl(), Size: req.GetSize(), DurationSeconds: int(req.GetDurationSeconds()),
		})
		if err != nil {
			return nil, log.GRPCError(err, codes.Internal, "Server.GenerateMedia.GenerateVideo", "generate video")
		}
		response.MediaUrl, response.MimeType, response.ProviderJobId = result.URL, "video/mp4", result.ProviderJobID
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unsupported media type %q", response.MediaType)
	}
	return response, nil
}

// HealthCheck reports serving status.
func (s *Server) HealthCheck(ctx context.Context, req *runtimev1.HealthCheckRequest) (*runtimev1.HealthCheckResponse, error) {
	_, traceID := s.bindTrace(ctx, req.GetTraceId())
	return &runtimev1.HealthCheckResponse{
		Status:  runtimev1.HealthCheckResponse_SERVING,
		Version: constant.Version,
		TraceId: traceID,
	}, nil
}

// ListCapabilities returns the provider-independent Runtime ability catalog.
func (s *Server) ListCapabilities(ctx context.Context, req *runtimev1.ListCapabilitiesRequest) (*runtimev1.ListCapabilitiesResponse, error) {
	_, traceID := s.bindTrace(ctx, req.GetTraceId())
	definitions := capability.GlobalRegistry.List()
	items := make([]*runtimev1.CapabilityDefinition, 0, len(definitions))
	for _, definition := range definitions {
		items = append(items, &runtimev1.CapabilityDefinition{
			Id: definition.ID, Description: definition.Description, Input: definition.Input,
			Output: definition.Output, ReadOnly: definition.ReadOnly, Risk: definition.Risk,
			Status: string(definition.Status), Provider: definition.Provider, Reason: definition.Reason,
			Preconditions: worldConditionsToProto(definition.Preconditions), ExpectedEffects: worldEffectsToProto(definition.ExpectedEffects), Postconditions: worldConditionsToProto(definition.Postconditions),
		})
	}
	return &runtimev1.ListCapabilitiesResponse{Capabilities: items, TraceId: traceID}, nil
}

func worldConditionsToProto(values []protocol.WorldCondition) []*runtimev1.WorldCondition {
	result := make([]*runtimev1.WorldCondition, 0, len(values))
	for _, value := range values {
		encoded, _ := json.Marshal(value.Value)
		result = append(result, &runtimev1.WorldCondition{Path: value.Path, Operator: value.Operator, ValueJson: encoded, Required: value.Required})
	}
	return result
}
func worldEffectsToProto(values []protocol.WorldEffect) []*runtimev1.WorldEffect {
	result := make([]*runtimev1.WorldEffect, 0, len(values))
	for _, value := range values {
		encoded, _ := json.Marshal(value.Value)
		result = append(result, &runtimev1.WorldEffect{Operation: value.Operation, Path: value.Path, ValueJson: encoded})
	}
	return result
}

// ---- streaming RPCs ----

func (s *Server) streamCompletion(
	ctx context.Context,
	traceID, prompt string,
	msgs []eino.ChatMessage,
	mc eino.ModelConfig,
	client *eino.Client,
	disp *dispatcher.Dispatcher,
	scope memScope,
	send func(*runtimev1.StreamEvent) error,
) error {
	var seq int64
	var emitMu sync.Mutex
	emit := func(ev *runtimev1.StreamEvent) error {
		emitMu.Lock()
		defer emitMu.Unlock()
		ev.Seq = seq
		ev.TraceId = traceID
		ev.EmittedAt = timestamppb.Now()
		seq++
		return send(ev)
	}

	start := time.Now()
	if err := emit(&runtimev1.StreamEvent{
		Payload: &runtimev1.StreamEvent_Meta{Meta: &runtimev1.MetaEvent{
			StartedAt:      timestamppb.New(start),
			StreamProtocol: constant.StreamProtocolGRPC,
		}},
	}); err != nil {
		return log.WrapError(err, "Server.streamCompletion.emitMeta")
	}

	runCtx, cancelRun := context.WithCancel(actionprotocol.WithScope(ctx, traceID))
	heartbeatDone := make(chan struct{})
	var heartbeatErr error
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if err := emit(&runtimev1.StreamEvent{
					Payload: &runtimev1.StreamEvent_Meta{Meta: &runtimev1.MetaEvent{
						HeartbeatAt: timestamppb.Now(),
					}},
				}); err != nil {
					heartbeatErr = log.WrapError(err, "Server.streamCompletion.emitHeartbeat")
					cancelRun()
					return
				}
			}
		}
	}()

	disp.SetResearchProgressHandler(func(progress research.Progress) error {
		actionID := "research-" + traceID
		payload, payloadErr := structpb.NewStruct(map[string]any{
			"protocol": "athena.research.v3", "type": "PROGRESS", "task_id": traceID,
			"action_id": actionID, "capability": "research.execute", "stage": progress.Stage,
			"message": progress.Message, "progress": progress.Percent, "round": progress.Round,
			"queries": progress.Queries, "sources": progress.Sources, "confidence": progress.Confidence,
			"query_texts":    researchQueryTextsPayload(progress.QueryTexts),
			"valuable_pages": researchPagesPayload(progress.ValuablePages), "completed": progress.Completed,
		})
		if payloadErr != nil {
			return payloadErr
		}
		return emit(&runtimev1.StreamEvent{Payload: &runtimev1.StreamEvent_ToolResult{ToolResult: &runtimev1.ToolResultEvent{
			Id: actionID, Tool: "research.progress", Output: payload, Success: true,
		}}})
	})

	res, err := disp.RunStream(runCtx, prompt, msgs, func(ch eino.StreamChunk) error {
		return emit(&runtimev1.StreamEvent{
			Payload: &runtimev1.StreamEvent_Delta{Delta: &runtimev1.DeltaEvent{
				Text: ch.Text,
				Role: constant.RoleAssistant,
			}},
		})
	}, func(action actionprotocol.Action) error {
		payload, payloadErr := clientActionPayload(action)
		if payloadErr != nil {
			return payloadErr
		}
		return emit(&runtimev1.StreamEvent{Payload: &runtimev1.StreamEvent_ToolResult{ToolResult: &runtimev1.ToolResultEvent{
			Id: action.ActionID, Tool: "client.action", Output: payload, Success: true,
		}}})
	})
	cancelRun()
	<-heartbeatDone
	if err == nil && heartbeatErr != nil {
		err = heartbeatErr
	}
	if err != nil {
		_ = emit(&runtimev1.StreamEvent{
			Payload: &runtimev1.StreamEvent_Error{Error: &runtimev1.ErrorEvent{
				Code:      runtimev1.BusinessErrorCode_BUSINESS_ERROR_CODE_MODEL_CALL_FAILED.String(),
				Message:   err.Error(),
				Retryable: true,
			}},
		})
		return log.GRPCError(err, codes.Unavailable, "Server.streamCompletion.Dispatch", "model stream failed")
	}

	responseMetadata := buildResponseMetadata(client.Name(), time.Since(start).Milliseconds(), res.Usage, eino.UsageCollectorFromContext(runCtx))
	err = emit(&runtimev1.StreamEvent{
		Payload: &runtimev1.StreamEvent_Done{Done: &runtimev1.DoneEvent{
			Content:          res.Content,
			FinishReason:     res.FinishReason,
			FinishedAt:       timestamppb.Now(),
			PromptTokens:     responseMetadata.PromptTokens,
			CompletionTokens: responseMetadata.CompletionTokens,
			TotalTokens:      responseMetadata.TokensUsed,
			Metadata:         responseMetadata,
		}},
	})
	s.reviewMemories(ctx, mc, scope.sessionID, scope.userID, scope.agentID, scope.userInput, res.Content)
	return log.WrapError(err, "Server.streamCompletion.emitDone")
}

func clientActionPayload(action actionprotocol.Action) (*structpb.Struct, error) {
	encoded, err := json.Marshal(action)
	if err != nil {
		return nil, fmt.Errorf("encode client action: %w", err)
	}
	var values map[string]any
	if err := json.Unmarshal(encoded, &values); err != nil {
		return nil, fmt.Errorf("decode client action payload: %w", err)
	}
	return structpb.NewStruct(values)
}

func researchQueryTextsPayload(values []string) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func researchPagesPayload(pages []research.ValuablePage) []any {
	result := make([]any, 0, len(pages))
	for _, page := range pages {
		signals := make([]any, 0, len(page.ValueSignals))
		for _, signal := range page.ValueSignals {
			signals = append(signals, signal)
		}
		result = append(result, map[string]any{
			"id": page.ID, "rank": page.Rank, "title": page.Title, "url": page.URL,
			"domain": page.Domain, "provider": page.Provider, "kind": page.Kind,
			"snippet": page.Snippet, "value_signals": signals,
			"authority": page.Authority, "relevance": page.Relevance,
			"freshness": page.Freshness, "evidence_score": page.EvidenceScore,
			"fetched": page.Fetched, "published_at": page.PublishedAt,
		})
	}
	return result
}

// memScope carries per-request memory identifiers and user input for the
// post-response background review.
type memScope struct {
	sessionID string
	userID    string
	agentID   string
	userInput string
}

// RunStream streams a completion for a configured Run request.
func (s *Server) RunStream(req *runtimev1.RunRequest, stream runtimev1.AgentRuntime_RunStreamServer) error {
	ctx := stream.Context()
	ctx, traceID := s.bindTrace(ctx, req.GetTraceId())
	ctx, _ = eino.WithUsageCollector(ctx)
	log.Infow(ctx, "run_stream", "prompt_len", len(req.GetPrompt()), "messages", len(req.GetMessages()))
	mc, err := s.modelConfig(req.GetModels())
	if err != nil {
		return log.WrapError(err, "Server.RunStream.modelConfig")
	}
	client, err := eino.NewClient(ctx, mc)
	if err != nil {
		return log.GRPCError(err, codes.Internal, "Server.RunStream.NewClient", "init model")
	}
	instruction, sessionID, userID, agentID := s.memoryInstruction(ctx, req.GetContext())
	ctx = provider.WithInvocationScope(ctx, userID, providerTaskID(req.GetContext(), req.GetRequestId(), traceID))
	disp := s.newRunDispatcher(ctx, client, req, instruction)
	return log.WrapError(s.streamCompletion(ctx, traceID, req.GetPrompt(), fromProtoMessages(req.GetMessages()), mc, client, disp,
		memScope{sessionID: sessionID, userID: userID, agentID: agentID, userInput: req.GetPrompt()}, stream.Send), "Server.RunStream")
}

// RunAgentStream streams an autonomous task response.
func (s *Server) RunAgentStream(req *runtimev1.AgentRequest, stream runtimev1.AgentRuntime_RunAgentStreamServer) error {
	ctx := stream.Context()
	ctx, traceID := s.bindTrace(ctx, req.GetTraceId())
	ctx, _ = eino.WithUsageCollector(ctx)
	log.Infow(ctx, "agent_stream", "task_len", len(req.GetTask()))
	if req.GetTask() == "" {
		return status.Error(codes.InvalidArgument, "task is required")
	}
	mc, err := s.modelConfig(req.GetModels())
	if err != nil {
		return log.WrapError(err, "Server.RunAgentStream.modelConfig")
	}
	client, err := eino.NewClient(ctx, mc)
	if err != nil {
		return log.GRPCError(err, codes.Internal, "Server.RunAgentStream.NewClient", "init model")
	}
	instruction, sessionID, userID, agentID := s.memoryInstruction(ctx, req.GetContext())
	ctx = provider.WithInvocationScope(ctx, userID, providerTaskID(req.GetContext(), req.GetRequestId(), traceID))
	disp := s.newAgentDispatcher(ctx, client, req, instruction)
	return log.WrapError(s.streamCompletion(ctx, traceID, req.GetTask(), nil, mc, client, disp,
		memScope{sessionID: sessionID, userID: userID, agentID: agentID, userInput: req.GetTask()}, stream.Send), "Server.RunAgentStream")
}

func buildResponseMetadata(modelName string, latencyMS int64, fallback eino.Usage, collector *eino.UsageCollector) *runtimev1.ResponseMetadata {
	usage := fallback
	var modelUsage []*runtimev1.ModelUsageMetadata
	if collector != nil {
		records := collector.Snapshot()
		if len(records) > 0 {
			usage = collector.Total()
			modelUsage = make([]*runtimev1.ModelUsageMetadata, 0, len(records))
			for _, record := range records {
				modelUsage = append(modelUsage, &runtimev1.ModelUsageMetadata{
					ModelId: record.ModelID, Provider: record.Provider, Model: record.Model,
					PromptTokens: int32(record.PromptTokens), CompletionTokens: int32(record.CompletionTokens),
					TotalTokens: int32(record.TotalTokens), RequestCount: int32(record.RequestCount),
				})
			}
		}
	}
	return &runtimev1.ResponseMetadata{
		Model: modelName, LatencyMs: latencyMS, TokensUsed: int32(usage.TotalTokens),
		PromptTokens: int32(usage.PromptTokens), CompletionTokens: int32(usage.CompletionTokens), ModelUsage: modelUsage,
	}
}

// Resume is not yet implemented (approval/checkpoint engine pending).
func (s *Server) Resume(ctx context.Context, req *runtimev1.ResumeRequest) (*runtimev1.ResumeResponse, error) {
	ctx, _ = s.bindTrace(ctx, req.GetTraceId())
	log.Infow(ctx, "resume", "checkpoint", req.GetCheckpointId())
	return nil, status.Error(codes.Unimplemented, "Resume not implemented in skeleton")
}

// Stop is not yet implemented (run registry pending).
func (s *Server) Stop(ctx context.Context, req *runtimev1.StopRequest) (*runtimev1.StopResponse, error) {
	ctx, _ = s.bindTrace(ctx, req.GetTraceId())
	log.Infow(ctx, "stop", "checkpoint", req.GetCheckpointId(), "session", req.GetSessionId())
	return nil, status.Error(codes.Unimplemented, "Stop not implemented in skeleton")
}
