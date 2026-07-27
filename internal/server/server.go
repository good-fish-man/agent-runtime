// Package server implements the AgentRuntime gRPC service defined in
// proto/agent/runtime/v1. This is a runnable skeleton: Run / RunStream /
// RunAgent / RunAgentStream / HealthCheck are backed by an eino chat model,
// while Resume / Stop return Unimplemented until the full engine lands.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"
	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/dispatcher"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/memory"
	"github.com/good-fish-man/agent-runtime/log"

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
	if ctx != nil {
		if v := ctx.Value(log.ReqIDKey); v != nil {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
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

// bindTrace resolves the request trace_id, injects it into ctx, and binds it to
// the current goroutine so every log line emitted while handling the request
// carries [trace_id]. The returned ctx must be used downstream (so ctx-based
// logging and log.Go inherit the id); release() must be deferred to unbind.
func (s *Server) bindTrace(ctx context.Context, fromRequest string) (context.Context, string, func()) {
	traceID := resolveTraceID(ctx, fromRequest)
	ctx = log.WithReqID(ctx, traceID)
	log.SetReqId(traceID)
	return ctx, traceID, log.ClearReqId
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
func (s *Server) reviewMemories(model eino.ModelConfig, sessionID, userID, agentID, userInput, assistantOutput string) {
	if s.cfg.Reviewer == nil {
		return
	}
	s.cfg.Reviewer.ReviewIfNeeded(model, sessionID, userID, agentID, userInput, assistantOutput)
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
	ctx, traceID, release := s.bindTrace(ctx, req.GetTraceId())
	defer release()
	log.Infow("run", "prompt_len", len(req.GetPrompt()), "messages", len(req.GetMessages()), "stream", false)
	mc, err := s.modelConfig(req.GetModels())
	if err != nil {
		return nil, err
	}
	client, err := eino.NewClient(ctx, mc)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "init model: %v", err)
	}
	instruction, sessionID, userID, agentID := s.memoryInstruction(ctx, req.GetContext())
	disp := s.newRunDispatcher(client, req, instruction)
	start := time.Now()
	res, err := disp.Run(ctx, req.GetPrompt(), fromProtoMessages(req.GetMessages()))
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "model call failed: %v", err)
	}
	s.reviewMemories(mc, sessionID, userID, agentID, req.GetPrompt(), res.Content)
	return &runtimev1.RunResponse{
		Content:      res.Content,
		FinishReason: res.FinishReason,
		TokensUsed:   int32(res.Usage.TotalTokens),
		TraceId:      traceID,
		Memories:     s.currentMemories(ctx, sessionID),
		Metadata: &runtimev1.ResponseMetadata{
			Model:            client.Name(),
			LatencyMs:        time.Since(start).Milliseconds(),
			TokensUsed:       int32(res.Usage.TotalTokens),
			PromptTokens:     int32(res.Usage.PromptTokens),
			CompletionTokens: int32(res.Usage.CompletionTokens),
		},
	}, nil
}

// RunAgent performs a non-streaming autonomous task (natural language input).
func (s *Server) RunAgent(ctx context.Context, req *runtimev1.AgentRequest) (*runtimev1.AgentResponse, error) {
	ctx, traceID, release := s.bindTrace(ctx, req.GetTraceId())
	defer release()
	log.Infow("agent", "task_len", len(req.GetTask()), "stream", false)
	if req.GetTask() == "" {
		return nil, status.Error(codes.InvalidArgument, "task is required")
	}
	mc, err := s.modelConfig(req.GetModels())
	if err != nil {
		return nil, err
	}
	client, err := eino.NewClient(ctx, mc)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "init model: %v", err)
	}
	instruction, sessionID, userID, agentID := s.memoryInstruction(ctx, req.GetContext())
	disp := s.newAgentDispatcher(client, req, instruction)
	start := time.Now()
	res, err := disp.Run(ctx, req.GetTask(), nil)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "model call failed: %v", err)
	}
	s.reviewMemories(mc, sessionID, userID, agentID, req.GetTask(), res.Content)
	return &runtimev1.AgentResponse{
		Content:      res.Content,
		FinishReason: res.FinishReason,
		TokensUsed:   int32(res.Usage.TotalTokens),
		TraceId:      traceID,
		Metadata: &runtimev1.ResponseMetadata{
			Model:            client.Name(),
			LatencyMs:        time.Since(start).Milliseconds(),
			TokensUsed:       int32(res.Usage.TotalTokens),
			PromptTokens:     int32(res.Usage.PromptTokens),
			CompletionTokens: int32(res.Usage.CompletionTokens),
		},
	}, nil
}

// HealthCheck reports serving status.
func (s *Server) HealthCheck(ctx context.Context, req *runtimev1.HealthCheckRequest) (*runtimev1.HealthCheckResponse, error) {
	_, traceID, release := s.bindTrace(ctx, req.GetTraceId())
	defer release()
	return &runtimev1.HealthCheckResponse{
		Status:  runtimev1.HealthCheckResponse_SERVING,
		Version: constant.Version,
		TraceId: traceID,
	}, nil
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
	emit := func(ev *runtimev1.StreamEvent) error {
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
		return err
	}

	res, err := disp.RunStream(ctx, prompt, msgs, func(ch eino.StreamChunk) error {
		return emit(&runtimev1.StreamEvent{
			Payload: &runtimev1.StreamEvent_Delta{Delta: &runtimev1.DeltaEvent{
				Text: ch.Text,
				Role: constant.RoleAssistant,
			}},
		})
	})
	if err != nil {
		_ = emit(&runtimev1.StreamEvent{
			Payload: &runtimev1.StreamEvent_Error{Error: &runtimev1.ErrorEvent{
				Code:      runtimev1.BusinessErrorCode_BUSINESS_ERROR_CODE_MODEL_CALL_FAILED.String(),
				Message:   err.Error(),
				Retryable: true,
			}},
		})
		return status.Errorf(codes.Unavailable, "model stream failed: %v", err)
	}

	err = emit(&runtimev1.StreamEvent{
		Payload: &runtimev1.StreamEvent_Done{Done: &runtimev1.DoneEvent{
			Content:          res.Content,
			FinishReason:     res.FinishReason,
			FinishedAt:       timestamppb.Now(),
			PromptTokens:     int32(res.Usage.PromptTokens),
			CompletionTokens: int32(res.Usage.CompletionTokens),
			TotalTokens:      int32(res.Usage.TotalTokens),
			Metadata: &runtimev1.ResponseMetadata{
				Model:            client.Name(),
				LatencyMs:        time.Since(start).Milliseconds(),
				TokensUsed:       int32(res.Usage.TotalTokens),
				PromptTokens:     int32(res.Usage.PromptTokens),
				CompletionTokens: int32(res.Usage.CompletionTokens),
			},
		}},
	})
	s.reviewMemories(mc, scope.sessionID, scope.userID, scope.agentID, scope.userInput, res.Content)
	return err
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
	ctx, traceID, release := s.bindTrace(ctx, req.GetTraceId())
	defer release()
	log.Infow("run_stream", "prompt_len", len(req.GetPrompt()), "messages", len(req.GetMessages()))
	mc, err := s.modelConfig(req.GetModels())
	if err != nil {
		return err
	}
	client, err := eino.NewClient(ctx, mc)
	if err != nil {
		return status.Errorf(codes.Internal, "init model: %v", err)
	}
	instruction, sessionID, userID, agentID := s.memoryInstruction(ctx, req.GetContext())
	disp := s.newRunDispatcher(client, req, instruction)
	return s.streamCompletion(ctx, traceID, req.GetPrompt(), fromProtoMessages(req.GetMessages()), mc, client, disp,
		memScope{sessionID: sessionID, userID: userID, agentID: agentID, userInput: req.GetPrompt()}, stream.Send)
}

// RunAgentStream streams an autonomous task response.
func (s *Server) RunAgentStream(req *runtimev1.AgentRequest, stream runtimev1.AgentRuntime_RunAgentStreamServer) error {
	ctx := stream.Context()
	ctx, traceID, release := s.bindTrace(ctx, req.GetTraceId())
	defer release()
	log.Infow("agent_stream", "task_len", len(req.GetTask()))
	if req.GetTask() == "" {
		return status.Error(codes.InvalidArgument, "task is required")
	}
	mc, err := s.modelConfig(req.GetModels())
	if err != nil {
		return err
	}
	client, err := eino.NewClient(ctx, mc)
	if err != nil {
		return status.Errorf(codes.Internal, "init model: %v", err)
	}
	instruction, sessionID, userID, agentID := s.memoryInstruction(ctx, req.GetContext())
	disp := s.newAgentDispatcher(client, req, instruction)
	return s.streamCompletion(ctx, traceID, req.GetTask(), nil, mc, client, disp,
		memScope{sessionID: sessionID, userID: userID, agentID: agentID, userInput: req.GetTask()}, stream.Send)
}

// Resume is not yet implemented (approval/checkpoint engine pending).
func (s *Server) Resume(ctx context.Context, req *runtimev1.ResumeRequest) (*runtimev1.ResumeResponse, error) {
	_, _, release := s.bindTrace(ctx, req.GetTraceId())
	defer release()
	log.Infow("resume", "checkpoint", req.GetCheckpointId())
	return nil, status.Error(codes.Unimplemented, "Resume not implemented in skeleton")
}

// Stop is not yet implemented (run registry pending).
func (s *Server) Stop(ctx context.Context, req *runtimev1.StopRequest) (*runtimev1.StopResponse, error) {
	_, _, release := s.bindTrace(ctx, req.GetTraceId())
	defer release()
	log.Infow("stop", "checkpoint", req.GetCheckpointId(), "session", req.GetSessionId())
	return nil, status.Error(codes.Unimplemented, "Stop not implemented in skeleton")
}
