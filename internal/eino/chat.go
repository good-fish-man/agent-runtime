// Package eino wraps the cloudwego/eino ADK (Agent Development Kit) so the
// Agent Runtime drives completions through an adk.ChatModelAgent + adk.Runner
// event loop, mirroring the reference runner implementation.
package eino

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/tools"
	"github.com/good-fish-man/agent-runtime/log"

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
	model *openai.ChatModel
	name  string
}

// NewClient builds a chat model from cfg. api_key/api_base support ${ENV_VAR}.
func NewClient(ctx context.Context, cfg ModelConfig) (*Client, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("model name is required")
	}
	if err := prepareLocalModelRuntime(ctx, &cfg); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("create chat model %q: %w", cfg.Name, err)
	}
	return &Client{model: cm, name: cfg.Name}, nil
}

// Name returns the model name.
func (c *Client) Name() string { return c.name }

// Model returns the underlying tool-calling chat model, for callers (e.g. the
// dispatcher's sub-agent manager and context compressor) that need direct model access.
func (c *Client) Model() model.ToolCallingChatModel { return c.model }

func toADKMessages(prompt string, msgs []ChatMessage) []adk.Message {
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
	if prompt != "" {
		out = append(out, schema.UserMessage(prompt))
	}
	return out
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
		(m.Role == schema.Tool && (m.ToolName == tools.GenerateImageToolName || m.ToolName == tools.GenerateVideoToolName || m.ToolName == tools.AskUserQuestionToolName))
}

// buildRunner constructs an ADK ChatModelAgent + Runner for this client.
func (c *Client) buildRunner(ctx context.Context, p RunParams, streaming bool) (*adk.Runner, error) {
	maxIter := p.MaxIterations
	if maxIter <= 0 {
		maxIter = constant.DefaultMaxIterations
	}
	var agentTools []tool.BaseTool
	if !p.DisableBuiltinTools {
		agentTools = append(agentTools, tools.AllToolsWithBasePath(p.WorkingDir)...)
	}
	agentTools = append(agentTools, p.ExtraTools...)
	for i, agentTool := range agentTools {
		agentTools[i] = tools.TraceTool(agentTool)
	}
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
				tools.GenerateImageToolName:   true,
				tools.GenerateVideoToolName:   true,
				tools.AskUserQuestionToolName: true,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	return adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: streaming,
	}), nil
}

// Generate performs a non-streaming completion via the ADK event loop.
func (c *Client) Generate(ctx context.Context, prompt string, msgs []ChatMessage, p RunParams) (*Result, error) {
	messages := toADKMessages(prompt, msgs)
	if len(messages) == 0 {
		return nil, fmt.Errorf("no input messages")
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
		if isUserVisibleMessage(msg) {
			res.Content = msg.Content
		}
		res.FinishReason = finishReasonOf(msg, res.FinishReason)
		if u := usageOf(msg); u.TotalTokens > 0 || u.PromptTokens > 0 || u.CompletionTokens > 0 {
			res.Usage = u
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
	messages := toADKMessages(prompt, msgs)
	if len(messages) == 0 {
		return nil, fmt.Errorf("no input messages")
	}
	runner, err := c.buildRunner(ctx, p, true)
	if err != nil {
		return nil, log.WrapError(err, "eino.Client.Stream.buildRunner")
	}

	events := runner.Run(ctx, messages)
	res := &Result{FinishReason: "stop"}
	streamFilter := newToolMarkupStreamFilter(onChunk)
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
			// Only assistant text and the safe, direct GenerateImage Markdown are
			// visible. Keep metadata from all other tool messages without emitting
			// their raw results.
			if !isUserVisibleMessage(mv.Message) {
				res.FinishReason = finishReasonOf(mv.Message, res.FinishReason)
				if u := usageOf(mv.Message); u.TotalTokens > 0 || u.PromptTokens > 0 || u.CompletionTokens > 0 {
					res.Usage = u
				}
				continue
			}
			if err := c.emitDelta(mv.Message, res, streamFilter.write); err != nil {
				return nil, log.WrapError(err, "eino.Client.Stream.emitDelta")
			}
			continue
		}

		if err := c.consumeStream(ctx, mv.MessageStream, res, streamFilter.write); err != nil {
			return nil, log.WrapError(err, "eino.Client.Stream.consumeStream")
		}
	}
	content, handled, err := streamFilter.finish(ctx, p.ExtraTools)
	if err != nil {
		return nil, log.WrapError(err, "eino.Client.Stream.textToolCall")
	}
	if handled {
		res.Content = content
	}
	return res, nil
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
		if err := c.emitDelta(chunk, res, onChunk); err != nil {
			return log.WrapError(err, "eino.Client.consumeStream.emitDelta")
		}
	}
}
