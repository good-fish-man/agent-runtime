package eino

import (
	"context"
	"errors"
	"io"
	"runtime/debug"
	"strings"

	"github.com/good-fish-man/agent-runtime/internal/observability"
	log "github.com/good-fish-man/logx"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const modelStreamBuffer = 16

type observedChatModel struct {
	inner     model.ToolCallingChatModel
	modelID   string
	provider  string
	name      string
	toolNames []string
}

func newObservedChatModel(inner model.ToolCallingChatModel, identity ModelIdentity) model.ToolCallingChatModel {
	return &observedChatModel{
		inner:    inner,
		modelID:  strings.TrimSpace(identity.ModelID),
		provider: strings.TrimSpace(identity.Provider),
		name:     strings.TrimSpace(identity.Model),
	}
}

func (m *observedChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (message *schema.Message, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fields := []any{
		"provider", m.provider,
		"mode", "generate",
		"message_count", len(input),
	}
	fields = append(fields, m.toolFields(opts...)...)
	span := observability.Begin(ctx, "model", m.name, "", fields...)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = log.NewError("eino.ChatModel.Generate", "panic: %v", recovered)
			log.Errorf(ctx, "model call panic model=%s mode=generate error=%v\n%s", m.name, recovered, debug.Stack())
		}
		recordModelUsage(ctx, m.identity(), usageOf(message))
		span.End(err, modelMessageFields(message)...)
	}()

	message, err = m.inner.Generate(ctx, input, opts...)
	if err != nil {
		err = log.WrapError(err, "eino.ChatModel.Generate")
	}
	return message, err
}

func (m *observedChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (reader *schema.StreamReader[*schema.Message], err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fields := []any{
		"provider", m.provider,
		"mode", "stream",
		"message_count", len(input),
	}
	fields = append(fields, m.toolFields(opts...)...)
	span := observability.Begin(ctx, "model", m.name, "", fields...)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = log.NewError("eino.ChatModel.Stream", "panic: %v", recovered)
			log.Errorf(ctx, "model call panic model=%s mode=stream error=%v\n%s", m.name, recovered, debug.Stack())
			span.End(err)
		}
	}()

	source, err := m.inner.Stream(ctx, input, opts...)
	if err != nil {
		err = log.WrapError(err, "eino.ChatModel.Stream")
		recordModelUsage(ctx, m.identity(), Usage{})
		span.End(err)
		return nil, err
	}
	if source == nil {
		err = log.NewError("eino.ChatModel.Stream", "model returned a nil stream")
		recordModelUsage(ctx, m.identity(), Usage{})
		span.End(err)
		return nil, err
	}

	reader, writer := schema.Pipe[*schema.Message](modelStreamBuffer)
	go m.forwardStream(ctx, source, writer, span)
	return reader, nil
}

func (m *observedChatModel) WithTools(toolInfos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	bound, err := m.inner.WithTools(toolInfos)
	if err != nil {
		return nil, log.WrapError(err, "eino.ChatModel.WithTools")
	}
	if bound == nil {
		return nil, log.NewError("eino.ChatModel.WithTools", "model returned nil after binding tools")
	}
	return &observedChatModel{
		inner:     bound,
		modelID:   m.modelID,
		provider:  m.provider,
		name:      m.name,
		toolNames: toolInfoNames(toolInfos),
	}, nil
}

func (m *observedChatModel) identity() ModelIdentity {
	return ModelIdentity{ModelID: m.modelID, Provider: m.provider, Model: m.name}
}

func (m *observedChatModel) toolFields(opts ...model.Option) []any {
	names := append([]string(nil), m.toolNames...)
	if options := model.GetCommonOptions(nil, opts...); options.Tools != nil {
		names = toolInfoNames(options.Tools)
	}
	return []any{"tool_count", len(names), "available_tools", names}
}

func toolInfoNames(toolInfos []*schema.ToolInfo) []string {
	names := make([]string, 0, len(toolInfos))
	for _, info := range toolInfos {
		if info != nil && strings.TrimSpace(info.Name) != "" {
			names = append(names, strings.TrimSpace(info.Name))
		}
	}
	return names
}

func (m *observedChatModel) forwardStream(
	ctx context.Context,
	source *schema.StreamReader[*schema.Message],
	writer *schema.StreamWriter[*schema.Message],
	span *observability.Invocation,
) {
	stats := modelStreamObservation{}
	defer func() {
		if recovered := recover(); recovered != nil {
			err := log.NewError("eino.ChatModel.Stream.recv", "panic: %v", recovered)
			log.Errorf(ctx, "model stream panic model=%s error=%v\n%s", m.name, recovered, debug.Stack())
			writer.Send(nil, err)
			span.End(err, stats.fields()...)
		}
		recordModelUsage(ctx, m.identity(), stats.usage)
		source.Close()
		writer.Close()
	}()

	for {
		chunk, err := source.Recv()
		if errors.Is(err, io.EOF) {
			span.End(nil, stats.fields()...)
			return
		}
		if err != nil {
			wrapped := log.WrapError(err, "eino.ChatModel.Stream.recv")
			writer.Send(nil, wrapped)
			span.End(wrapped, stats.fields()...)
			return
		}

		stats.observe(chunk)
		if writer.Send(chunk, nil) {
			span.End(nil, append(stats.fields(), "status", "consumer_closed")...)
			return
		}
	}
}

type modelStreamObservation struct {
	chunks         int
	contentBytes   int
	toolCallChunks int
	finishReason   string
	usage          Usage
}

func (s *modelStreamObservation) observe(message *schema.Message) {
	s.chunks++
	if message == nil {
		return
	}
	s.contentBytes += len(message.Content)
	if len(message.ToolCalls) > 0 {
		s.toolCallChunks++
	}
	s.finishReason = finishReasonOf(message, s.finishReason)
	if usage := usageOf(message); !zeroUsage(usage) {
		s.usage = usage
	}
}

func (s modelStreamObservation) fields() []any {
	return []any{
		"finish_reason", s.finishReason,
		"prompt_tokens", s.usage.PromptTokens,
		"completion_tokens", s.usage.CompletionTokens,
		"total_tokens", s.usage.TotalTokens,
		"chunk_count", s.chunks,
		"content_bytes", s.contentBytes,
		"tool_call_chunks", s.toolCallChunks,
	}
}

func modelMessageFields(message *schema.Message) []any {
	usage := usageOf(message)
	return []any{
		"finish_reason", finishReasonOf(message, ""),
		"prompt_tokens", usage.PromptTokens,
		"completion_tokens", usage.CompletionTokens,
		"total_tokens", usage.TotalTokens,
		"content_bytes", messageContentBytes(message),
		"tool_call_count", messageToolCallCount(message),
	}
}

func messageContentBytes(message *schema.Message) int {
	if message == nil {
		return 0
	}
	return len(message.Content)
}

func messageToolCallCount(message *schema.Message) int {
	if message == nil {
		return 0
	}
	return len(message.ToolCalls)
}
