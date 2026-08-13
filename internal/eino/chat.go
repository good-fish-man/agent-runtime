// Package eino wraps the cloudwego/eino ADK (Agent Development Kit) so the
// Agent Runtime drives completions through an adk.ChatModelAgent + adk.Runner
// event loop, mirroring the reference runner implementation.
package eino

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/tools"
	log "github.com/good-fish-man/logx"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// ModelConfig is the subset of model settings the runtime needs to build a chat model.
type ModelConfig struct {
	Provider    string
	Name        string
	APIKey      string
	APIBase     string
	Temperature float64
	MaxTokens   int
	TopP        float64
	ExtraFields map[string]any
}

// ChatMessage is a transport-neutral message.
type ChatMessage struct {
	Role    string
	Content string
}

type VisualInput struct {
	ID       string
	MIMEType string
	Data     []byte
	SHA256   string
	Purpose  string
	Detail   string
}

// Usage holds token accounting from a completion.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Result is the outcome of a completion.
type Result struct {
	Content      string
	FinishReason string
	Usage        Usage
	ActionCount  int
	StreamStats  StreamStats
	ToolCalls    []schema.ToolCall
}

// StreamStats captures what the model stream actually produced. It helps
// distinguish a real empty model response from tool-call or reasoning-only
// streams that do not surface user-visible text.
type StreamStats struct {
	Events          int
	Chunks          int
	VisibleChunks   int
	EmptyChunks     int
	ToolCallChunks  int
	ReasoningChunks int
	UsageChunks     int
}

// EmptyToolCallStreamError means the provider streamed tool-call deltas, but
// the ADK stream did not continue into a visible tool result or assistant text.
type EmptyToolCallStreamError struct {
	FinishReason string
	Usage        Usage
	Stats        StreamStats
	ToolCalls    []schema.ToolCall
}

func (e *EmptyToolCallStreamError) Error() string {
	if e == nil {
		return "model stream returned empty tool call stream"
	}
	s := e.Stats
	return fmt.Sprintf("model stream returned tool calls without visible content (finish_reason=%s events=%d chunks=%d visible_chunks=%d empty_chunks=%d tool_call_chunks=%d reasoning_chunks=%d usage_chunks=%d prompt_tokens=%d completion_tokens=%d total_tokens=%d)",
		e.FinishReason, s.Events, s.Chunks, s.VisibleChunks, s.EmptyChunks, s.ToolCallChunks, s.ReasoningChunks, s.UsageChunks,
		e.Usage.PromptTokens, e.Usage.CompletionTokens, e.Usage.TotalTokens)
}

func IsEmptyToolCallStream(err error) bool {
	var target *EmptyToolCallStreamError
	return errors.As(err, &target)
}

func EmptyToolCalls(err error) []schema.ToolCall {
	var target *EmptyToolCallStreamError
	if !errors.As(err, &target) || len(target.ToolCalls) == 0 {
		return nil
	}
	return append([]schema.ToolCall(nil), target.ToolCalls...)
}

// StreamChunk is a single streamed delta.
type StreamChunk struct {
	Text string
}

// RunParams controls a single agent run.
type RunParams struct {
	// Instruction is the system prompt for the agent.
	Instruction string
	// MaxIterations bounds ChatModel generation cycles (0 => default).
	MaxIterations int
	// WorkingDir is the base path / working directory bound to filesystem and
	// shell tools (Glob, Grep, Read, Edit, Write, Bash, Task*). Empty => ".".
	WorkingDir string
	// ExtraTools are additional tools (e.g. sub-agent, retriever) appended to
	// the built-in tool set for this run.
	ExtraTools []tool.BaseTool
	// DisableBuiltinTools omits the built-in tool set, using only ExtraTools.
	DisableBuiltinTools bool
	// VisualInputs are trusted device observations attached to the current user
	// turn through Eino's native multimodal message representation.
	VisualInputs []VisualInput
	// OnAction receives device-bound v2 actions. Actions are control events and
	// are never rendered as assistant text.
	OnAction func(actionprotocol.Action) error
}

type toolExecutionResult struct {
	Messages    []adk.Message
	Content     string
	ActionCount int
	Usage       Usage
}

type toolInvocation struct {
	name      string
	callID    string
	arguments string
	tool      tool.InvokableTool
	output    string
	err       error
}

var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv expands ${ENV_VAR} placeholders using the process environment.
func ExpandEnv(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1]
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return m
	})
}

// Client wraps an eino chat model and runs it through the ADK agent/runner.
type Client struct {
	model model.ToolCallingChatModel
	name  string
}

// NewClient builds a chat model from cfg. api_key/api_base support ${ENV_VAR}.
func NewClient(ctx context.Context, cfg ModelConfig) (*Client, error) {
	if cfg.Name == "" {
		return nil, log.NewError("eino.NewClient.validate", "model name is required")
	}
	if err := prepareLocalModelRuntime(ctx, &cfg); err != nil {
		return nil, log.WrapError(err, "eino.NewClient.prepareLocalModelRuntime")
	}
	oc := &openai.ChatModelConfig{
		APIKey:  ExpandEnv(cfg.APIKey),
		Model:   cfg.Name,
		BaseURL: ExpandEnv(cfg.APIBase),
	}
	if cfg.Temperature > 0 {
		t := float32(cfg.Temperature)
		oc.Temperature = &t
	}
	if cfg.MaxTokens > 0 {
		mt := cfg.MaxTokens
		oc.MaxTokens = &mt
	}
	if cfg.TopP > 0 {
		p := float32(cfg.TopP)
		oc.TopP = &p
	}
	cm, err := openai.NewChatModel(ctx, oc)
	if err != nil {
		return nil, log.WrapError(err, "eino.NewClient.createChatModel")
	}
	return &Client{model: newObservedChatModel(cm, modelIdentityFromConfig(cfg)), name: cfg.Name}, nil
}

// Name returns the model name.
func (c *Client) Name() string { return c.name }

// Model returns the underlying tool-calling chat model, for callers (e.g. the
// dispatcher's sub-agent manager and context compressor) that need direct model access.
func (c *Client) Model() model.ToolCallingChatModel { return c.model }

func toADKMessages(prompt string, msgs []ChatMessage, visualInputs []VisualInput) []adk.Message {
	out := make([]adk.Message, 0, len(msgs)+1)
	for _, m := range msgs {
		switch m.Role {
		case constant.RoleSystem:
			out = append(out, schema.SystemMessage(m.Content))
		case constant.RoleAssistant:
			out = append(out, schema.AssistantMessage(m.Content, nil))
		default:
			out = append(out, schema.UserMessage(m.Content))
		}
	}
	if prompt != "" || len(visualInputs) > 0 {
		out = append(out, userMessageWithVisualInputs(prompt, visualInputs))
	}
	return out
}

func userMessageWithVisualInputs(prompt string, visualInputs []VisualInput) *schema.Message {
	if len(visualInputs) == 0 {
		return schema.UserMessage(prompt)
	}
	parts := make([]schema.MessageInputPart, 0, len(visualInputs)+1)
	if prompt != "" {
		parts = append(parts, schema.MessageInputPart{Type: schema.ChatMessagePartTypeText, Text: prompt})
	}
	for _, input := range visualInputs {
		if len(input.Data) == 0 || !strings.HasPrefix(input.MIMEType, "image/") {
			continue
		}
		encoded := base64.StdEncoding.EncodeToString(input.Data)
		detail := schema.ImageURLDetailAuto
		switch strings.ToLower(strings.TrimSpace(input.Detail)) {
		case "low":
			detail = schema.ImageURLDetailLow
		case "high":
			detail = schema.ImageURLDetailHigh
		}
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{
				MessagePartCommon: schema.MessagePartCommon{Base64Data: &encoded, MIMEType: input.MIMEType},
				Detail:            detail,
			},
			Extra: map[string]any{"artifact_id": input.ID, "sha256": input.SHA256, "purpose": input.Purpose},
		})
	}
	if len(parts) == 0 {
		return schema.UserMessage(prompt)
	}
	return &schema.Message{Role: schema.User, UserInputMultiContent: parts}
}

func usageOf(m *schema.Message) Usage {
	if m == nil || m.ResponseMeta == nil || m.ResponseMeta.Usage == nil {
		return Usage{}
	}
	u := m.ResponseMeta.Usage
	return Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
}

func zeroUsage(u Usage) bool {
	return u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0
}

func finishReasonOf(m *schema.Message, fallback string) string {
	if m != nil && m.ResponseMeta != nil && m.ResponseMeta.FinishReason != "" {
		return m.ResponseMeta.FinishReason
	}
	return fallback
}

func isUserVisibleMessage(m *schema.Message) bool {
	if m == nil {
		return false
	}
	return m.Role == schema.Assistant ||
		(m.Role == schema.Tool && isUserVisibleToolName(m.ToolName))
}

func isUserVisibleToolName(name string) bool {
	switch name {
	case tools.GenerateImageToolName,
		tools.GenerateVideoToolName,
		tools.AskUserQuestionToolName,
		capability.ModelName(capability.ImageGenerate),
		capability.ModelName(capability.VideoGenerate),
		capability.ModelName(capability.InteractionAsk):
		return true
	default:
		return false
	}
}

// buildRunner constructs an ADK ChatModelAgent + Runner for this client.
func (c *Client) buildRunner(ctx context.Context, p RunParams, streaming bool) (*adk.Runner, error) {
	maxIter := p.MaxIterations
	if maxIter <= 0 {
		maxIter = constant.DefaultMaxIterations
	}
	agentTools := buildAgentTools(p)
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          constant.AgentName,
		Description:   constant.AgentDescription,
		Instruction:   p.Instruction,
		Model:         c.model,
		MaxIterations: maxIter,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: agentTools,
			},
			ReturnDirectly: map[string]bool{
				capability.ModelName(capability.ImageGenerate):   true,
				capability.ModelName(capability.VideoGenerate):   true,
				capability.ModelName(capability.InteractionAsk):  true,
				capability.ModelName(capability.BrowserSearch):   true,
				capability.ModelName(capability.BrowserTask):     true,
				capability.ModelName(capability.BrowserOpen):     true,
				capability.ModelName(capability.BrowserNavigate): true,
				capability.ModelName(capability.BrowserLogin):    true,
				capability.ModelName(capability.BrowserRead):     true,
				capability.ModelName(capability.BrowserObserve):  true,
				capability.ModelName(capability.BrowserAction):   true,
				capability.ModelName(capability.BrowserClose):    true,
				capability.ModelName(capability.DesktopAction):   true,
			},
		},
	})
	if err != nil {
		return nil, log.WrapError(err, "eino.Client.buildRunner.createAgent")
	}
	return adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: streaming,
	}), nil
}

// Generate performs a non-streaming completion via the ADK event loop.
func (c *Client) Generate(ctx context.Context, prompt string, msgs []ChatMessage, p RunParams) (*Result, error) {
	messages := toADKMessages(prompt, msgs, p.VisualInputs)
	if len(messages) == 0 {
		return nil, log.NewError("eino.Client.Generate.validate", "no input messages")
	}
	runner, err := c.buildRunner(ctx, p, false)
	if err != nil {
		return nil, log.WrapError(err, "eino.Client.Generate.buildRunner")
	}

	events := runner.Run(ctx, messages)
	res := &Result{FinishReason: "stop"}
	for {
		select {
		case <-ctx.Done():
			return nil, log.WrapError(ctx.Err(), "eino.Client.Generate.context")
		default:
		}
		event, ok := events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return nil, log.WrapError(event.Err, "eino.Client.Generate.agentEvent")
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		msg, err := event.Output.MessageOutput.GetMessage()
		if err != nil {
			return nil, log.WrapError(err, "eino.Client.Generate.getMessage")
		}
		if msg == nil {
			continue
		}
		// Assistant text and the safe, direct GenerateImage Markdown are visible.
		// Other tool results may contain raw JSON or command output and stay hidden.
		if action, ok := actionprotocol.Parse(msg.Content); ok {
			return nil, log.NewError("eino.Client.Generate.clientAction", "client action %s requires streaming execution", action.Capability)
		}
		if isUserVisibleMessage(msg) {
			res.Content = msg.Content
		}
		res.FinishReason = finishReasonOf(msg, res.FinishReason)
		if u := usageOf(msg); u.TotalTokens > 0 || u.PromptTokens > 0 || u.CompletionTokens > 0 {
			res.Usage = addUsage(res.Usage, u)
		}
	}
	content, handled, err := executeTextToolMarkup(ctx, res.Content, p.ExtraTools)
	if err != nil {
		return nil, log.WrapError(err, "eino.Client.Generate.textToolCall")
	}
	if handled {
		res.Content = content
	}
	return res, nil
}

// Stream performs a streaming completion via the ADK event loop, invoking
// onChunk for each delta. It returns the aggregated content and final usage.
func (c *Client) Stream(ctx context.Context, prompt string, msgs []ChatMessage, p RunParams, onChunk func(StreamChunk) error) (*Result, error) {
	messages := toADKMessages(prompt, msgs, p.VisualInputs)
	if len(messages) == 0 {
		return nil, log.NewError("eino.Client.Stream.validate", "no input messages")
	}
	maxIter := p.MaxIterations
	if maxIter <= 0 {
		maxIter = constant.DefaultMaxIterations
	}

	total := &Result{FinishReason: "stop"}
	for i := 0; i < maxIter; i++ {
		result, err := c.streamMessages(ctx, messages, p, onChunk)
		mergeResult(total, result)
		if err == nil {
			return total, nil
		}
		if !IsEmptyToolCallStream(err) {
			return nil, err
		}

		toolCalls := EmptyToolCalls(err)
		if len(toolCalls) == 0 {
			return nil, err
		}
		executed, execErr := c.executeToolCalls(ctx, toolCalls, p, onChunk)
		if execErr != nil {
			return nil, log.WrapError(execErr, "eino.Client.Stream.executeToolCalls")
		}
		total.ActionCount += executed.ActionCount
		if !zeroUsage(executed.Usage) {
			total.Usage = addUsage(total.Usage, executed.Usage)
		}
		if strings.TrimSpace(executed.Content) != "" {
			total.Content += executed.Content
			return total, nil
		}
		if executed.ActionCount > 0 {
			total.FinishReason = "client_action"
			return total, nil
		}
		if len(executed.Messages) == 0 {
			return nil, log.WrapError(err, "eino.Client.Stream.toolCallsNoObservation")
		}

		messages = append(messages, schema.AssistantMessage("", toolCalls))
		messages = append(messages, executed.Messages...)
	}
	return nil, log.NewError("eino.Client.Stream.iterationLimit", "stream tool execution reached %d iteration limit", maxIter)
}

func (c *Client) streamMessages(ctx context.Context, messages []adk.Message, p RunParams, onChunk func(StreamChunk) error) (*Result, error) {
	runner, err := c.buildRunner(ctx, p, true)
	if err != nil {
		return nil, log.WrapError(err, "eino.Client.Stream.buildRunner")
	}

	events := runner.Run(ctx, messages)
	res := &Result{FinishReason: "stop"}
	streamFilter := newToolMarkupStreamFilter(onChunk)
	actionCount := 0
	for {
		select {
		case <-ctx.Done():
			return nil, log.WrapError(ctx.Err(), "eino.Client.Stream.context")
		default:
		}
		event, ok := events.Next()
		if !ok {
			break
		}
		res.StreamStats.Events++
		if event.Err != nil {
			return nil, log.WrapError(event.Err, "eino.Client.Stream.agentEvent")
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		mv := event.Output.MessageOutput

		// Non-streaming variant within the event loop (e.g. tool messages).
		if !mv.IsStreaming || mv.MessageStream == nil {
			if mv.Message == nil {
				continue
			}
			if action, ok := actionprotocol.Parse(mv.Message.Content); ok {
				if p.OnAction == nil {
					return nil, log.NewError("eino.Client.Stream.clientAction", "client action %s has no action sink", action.Capability)
				}
				if err := p.OnAction(action); err != nil {
					return nil, log.WrapError(err, "eino.Client.Stream.onAction")
				}
				actionCount++
				continue
			}
			// Only assistant text and safe direct-return media messages are
			// visible. Keep metadata from all other tool messages without emitting
			// their raw results.
			if !isUserVisibleMessage(mv.Message) {
				res.FinishReason = finishReasonOf(mv.Message, res.FinishReason)
				if u := usageOf(mv.Message); u.TotalTokens > 0 || u.PromptTokens > 0 || u.CompletionTokens > 0 {
					res.Usage = addUsage(res.Usage, u)
				}
				continue
			}
			if err := c.emitDelta(mv.Message, res, streamFilter.write); err != nil {
				return nil, log.WrapError(err, "eino.Client.Stream.emitDelta")
			}
			continue
		}

		msg, err := c.consumeStreamingMessage(ctx, mv.MessageStream, res, streamFilter.write)
		if err != nil {
			return nil, log.WrapError(err, "eino.Client.Stream.consumeStream")
		}
		if msg == nil {
			continue
		}
		if len(msg.ToolCalls) > 0 {
			res.FinishReason = finishReasonOf(msg, "tool_calls")
			continue
		}
	}
	content, handled, err := streamFilter.finish(ctx, p.ExtraTools)
	if err != nil {
		return nil, log.WrapError(err, "eino.Client.Stream.textToolCall")
	}
	if handled {
		if action, ok := actionprotocol.Parse(content); ok {
			if p.OnAction == nil {
				return nil, log.NewError("eino.Client.Stream.textClientAction", "client action %s has no action sink", action.Capability)
			}
			if err := p.OnAction(action); err != nil {
				return nil, log.WrapError(err, "eino.Client.Stream.onTextAction")
			}
			actionCount++
			res.Content = ""
		} else {
			res.Content = content
		}
	}
	res.ActionCount = actionCount
	if err := finalizeStreamResult(res); err != nil {
		return res, err
	}
	return res, nil
}

func buildAgentTools(p RunParams) []tool.BaseTool {
	var agentTools []tool.BaseTool
	if !p.DisableBuiltinTools {
		agentTools = append(agentTools, tools.AllToolsWithBasePath(p.WorkingDir)...)
	}
	agentTools = append(agentTools, p.ExtraTools...)
	for i, agentTool := range agentTools {
		agentTools[i] = tools.TraceTool(agentTool)
	}
	return agentTools
}

func mergeResult(dst, src *Result) {
	if dst == nil || src == nil {
		return
	}
	dst.Content += src.Content
	dst.FinishReason = src.FinishReason
	if !zeroUsage(src.Usage) {
		dst.Usage = addUsage(dst.Usage, src.Usage)
	}
	dst.ActionCount += src.ActionCount
	dst.StreamStats.Events += src.StreamStats.Events
	dst.StreamStats.Chunks += src.StreamStats.Chunks
	dst.StreamStats.VisibleChunks += src.StreamStats.VisibleChunks
	dst.StreamStats.EmptyChunks += src.StreamStats.EmptyChunks
	dst.StreamStats.ToolCallChunks += src.StreamStats.ToolCallChunks
	dst.StreamStats.ReasoningChunks += src.StreamStats.ReasoningChunks
	dst.StreamStats.UsageChunks += src.StreamStats.UsageChunks
	dst.ToolCalls = append(dst.ToolCalls, src.ToolCalls...)
}

func addUsage(left, right Usage) Usage {
	return Usage{
		PromptTokens:     left.PromptTokens + right.PromptTokens,
		CompletionTokens: left.CompletionTokens + right.CompletionTokens,
		TotalTokens:      left.TotalTokens + right.TotalTokens,
	}
}

func (c *Client) executeToolCalls(ctx context.Context, toolCalls []schema.ToolCall, p RunParams, onChunk func(StreamChunk) error) (*toolExecutionResult, error) {
	toolByName, err := toolMap(ctx, buildAgentTools(p))
	if err != nil {
		return nil, log.WrapError(err, "eino.Client.executeToolCalls.toolMap")
	}

	invocations := make([]toolInvocation, len(toolCalls))
	for i, call := range toolCalls {
		invocations[i] = resolveToolInvocation(call, i, toolByName)
	}
	executeToolInvocations(ctx, invocations)

	result := &toolExecutionResult{Messages: make([]adk.Message, 0, len(toolCalls))}
	for _, invocation := range invocations {
		if invocation.err != nil {
			result.Messages = append(result.Messages, schema.ToolMessage(toolErrorObservation(invocation.err), invocation.callID, schema.WithToolName(invocation.name)))
			continue
		}
		if action, ok := actionprotocol.Parse(invocation.output); ok {
			if p.OnAction == nil {
				return nil, log.NewError("eino.Client.executeToolCalls.clientAction", "client action %s has no action sink", action.Capability)
			}
			if err := p.OnAction(action); err != nil {
				return nil, log.WrapError(err, "eino.Client.executeToolCalls.onAction")
			}
			result.ActionCount++
			continue
		}
		if isUserVisibleToolName(invocation.name) {
			result.Content += invocation.output
			if err := onChunk(StreamChunk{Text: invocation.output}); err != nil {
				return nil, log.WrapError(err, "eino.Client.executeToolCalls.emitChunk")
			}
			continue
		}
		result.Messages = append(result.Messages, schema.ToolMessage(invocation.output, invocation.callID, schema.WithToolName(invocation.name)))
	}
	return result, nil
}

func resolveToolInvocation(call schema.ToolCall, index int, toolByName map[string]tool.BaseTool) toolInvocation {
	name := strings.TrimSpace(call.Function.Name)
	callID := strings.TrimSpace(call.ID)
	if callID == "" {
		callID = fmt.Sprintf("call_%d", index+1)
	}
	invocation := toolInvocation{name: name, callID: callID, arguments: call.Function.Arguments}
	provider, ok := toolByName[name]
	if !ok {
		invocation.err = log.NewError("eino.Client.resolveToolInvocation", "tool %s is unavailable", name)
		return invocation
	}
	invokable, ok := provider.(tool.InvokableTool)
	if !ok {
		invocation.err = log.NewError("eino.Client.resolveToolInvocation", "tool %s is not invokable", name)
		return invocation
	}
	invocation.tool = invokable
	return invocation
}

// executeToolInvocations preserves model call order while allowing contiguous
// read-only capabilities to run concurrently. Mutating calls remain barriers.
func executeToolInvocations(ctx context.Context, invocations []toolInvocation) {
	for i := 0; i < len(invocations); {
		if !isReadOnlyToolInvocation(invocations[i]) {
			runToolInvocation(ctx, &invocations[i])
			i++
			continue
		}

		end := i + 1
		for end < len(invocations) && isReadOnlyToolInvocation(invocations[end]) {
			end++
		}
		runReadOnlyToolInvocations(ctx, invocations[i:end])
		i = end
	}
}

func isReadOnlyToolInvocation(invocation toolInvocation) bool {
	return invocation.err == nil && invocation.tool != nil && tools.GlobalRegistry.IsReadOnly(invocation.name)
}

func runReadOnlyToolInvocations(ctx context.Context, invocations []toolInvocation) {
	workers := min(len(invocations), constant.DefaultParallelToolWorkers)
	if workers <= 1 {
		if len(invocations) == 1 {
			runToolInvocation(ctx, &invocations[0])
		}
		return
	}

	jobs := make(chan int, len(invocations))
	for i := range invocations {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		log.Go(func() {
			defer wg.Done()
			for index := range jobs {
				runToolInvocation(ctx, &invocations[index])
			}
		})
	}
	wg.Wait()
}

func runToolInvocation(ctx context.Context, invocation *toolInvocation) {
	if invocation == nil || invocation.err != nil {
		return
	}
	callCtx := tools.WithToolCallID(ctx, invocation.callID)
	invocation.output, invocation.err = invocation.tool.InvokableRun(callCtx, invocation.arguments)
}

func toolMap(ctx context.Context, values []tool.BaseTool) (map[string]tool.BaseTool, error) {
	out := make(map[string]tool.BaseTool, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		info, err := value.Info(ctx)
		if err != nil {
			return nil, log.WrapError(err, "eino.Client.toolMap.Info")
		}
		if info == nil || strings.TrimSpace(info.Name) == "" {
			continue
		}
		out[info.Name] = value
	}
	return out, nil
}

func toolErrorObservation(err error) string {
	if err == nil {
		return `{"error":"tool execution failed"}`
	}
	payload, marshalErr := json.Marshal(map[string]string{"error": err.Error()})
	if marshalErr != nil {
		return `{"error":"tool execution failed"}`
	}
	return string(payload)
}

func finalizeStreamResult(res *Result) error {
	if res == nil || strings.TrimSpace(res.Content) != "" {
		return nil
	}
	if res.ActionCount > 0 {
		res.FinishReason = "client_action"
		return nil
	}
	if res.FinishReason == "tool_calls" || res.StreamStats.ToolCallChunks > 0 {
		return &EmptyToolCallStreamError{FinishReason: res.FinishReason, Usage: res.Usage, Stats: res.StreamStats, ToolCalls: res.ToolCalls}
	}
	if zeroUsage(res.Usage) {
		s := res.StreamStats
		return fmt.Errorf("model stream returned empty content without usage or client action (events=%d chunks=%d visible_chunks=%d empty_chunks=%d tool_call_chunks=%d reasoning_chunks=%d usage_chunks=%d)",
			s.Events, s.Chunks, s.VisibleChunks, s.EmptyChunks, s.ToolCallChunks, s.ReasoningChunks, s.UsageChunks)
	}
	s := res.StreamStats
	return fmt.Errorf("model stream returned empty content despite token usage (events=%d chunks=%d visible_chunks=%d empty_chunks=%d tool_call_chunks=%d reasoning_chunks=%d usage_chunks=%d prompt_tokens=%d completion_tokens=%d total_tokens=%d)",
		s.Events, s.Chunks, s.VisibleChunks, s.EmptyChunks, s.ToolCallChunks, s.ReasoningChunks, s.UsageChunks,
		res.Usage.PromptTokens, res.Usage.CompletionTokens, res.Usage.TotalTokens)
}

func (c *Client) emitDelta(msg *schema.Message, res *Result, onChunk func(StreamChunk) error) error {
	res.FinishReason = finishReasonOf(msg, res.FinishReason)
	if u := usageOf(msg); u.TotalTokens > 0 || u.PromptTokens > 0 || u.CompletionTokens > 0 {
		res.Usage = u
	}
	if msg.Content == "" {
		return nil
	}
	res.Content += msg.Content
	return onChunk(StreamChunk{Text: msg.Content})
}

func (c *Client) consumeStream(ctx context.Context, stream *schema.StreamReader[*schema.Message], res *Result, onChunk func(StreamChunk) error) error {
	defer stream.Close()
	for {
		select {
		case <-ctx.Done():
			return log.WrapError(ctx.Err(), "eino.Client.consumeStream.context")
		default:
		}
		chunk, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return log.WrapError(err, "eino.Client.consumeStream.recv")
		}
		recordStreamChunkStats(res, chunk)
		if err := c.emitDelta(chunk, res, onChunk); err != nil {
			return log.WrapError(err, "eino.Client.consumeStream.emitDelta")
		}
	}
}

func (c *Client) consumeStreamingMessage(ctx context.Context, stream *schema.StreamReader[*schema.Message], res *Result, onChunk func(StreamChunk) error) (*schema.Message, error) {
	streams := stream.Copy(2)
	if err := c.consumeStream(ctx, streams[0], res, onChunk); err != nil {
		streams[1].Close()
		return nil, err
	}
	msg, err := schema.ConcatMessageStream(streams[1])
	if err != nil {
		return nil, err
	}
	res.FinishReason = finishReasonOf(msg, res.FinishReason)
	if u := usageOf(msg); u.TotalTokens > 0 || u.PromptTokens > 0 || u.CompletionTokens > 0 {
		res.Usage = u
	}
	if len(msg.ToolCalls) > 0 {
		res.ToolCalls = append(res.ToolCalls, msg.ToolCalls...)
	}
	return msg, nil
}

func recordStreamChunkStats(res *Result, msg *schema.Message) {
	if res == nil {
		return
	}
	res.StreamStats.Chunks++
	if msg == nil {
		res.StreamStats.EmptyChunks++
		return
	}
	if strings.TrimSpace(msg.Content) != "" {
		res.StreamStats.VisibleChunks++
	}
	if len(msg.ToolCalls) > 0 {
		res.StreamStats.ToolCallChunks++
	}
	if strings.TrimSpace(msg.ReasoningContent) != "" {
		res.StreamStats.ReasoningChunks++
	}
	if u := usageOf(msg); !zeroUsage(u) {
		res.StreamStats.UsageChunks++
	}
	if strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 && strings.TrimSpace(msg.ReasoningContent) == "" && zeroUsage(usageOf(msg)) {
		res.StreamStats.EmptyChunks++
	}
}
