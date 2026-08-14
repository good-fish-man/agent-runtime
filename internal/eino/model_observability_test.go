package eino

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	log "github.com/good-fish-man/logx"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type observableFakeModel struct {
	message       *schema.Message
	generateError error
	streamError   error
	stream        []*schema.Message
}

func (m *observableFakeModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return m.message, m.generateError
}

func (m *observableFakeModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if m.streamError != nil {
		return nil, m.streamError
	}
	return schema.StreamReaderFromArray(m.stream), nil
}

func (m *observableFakeModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func TestObservedChatModelGenerateLogsUsageAndPreservesCause(t *testing.T) {
	var output bytes.Buffer
	log.SetOutput(&output)
	defer log.SetOutput(nil)

	cause := errors.New("provider rejected request")
	ctx, collector := WithUsageCollector(context.Background())
	observed := newObservedChatModel(&observableFakeModel{generateError: cause}, ModelIdentity{ModelID: "model-test", Provider: "openai", Model: "gpt-test"})
	_, err := observed.Generate(ctx, []*schema.Message{schema.UserMessage("hello")})
	if !errors.Is(err, cause) {
		t.Fatalf("Generate error does not preserve provider cause: %v", err)
	}
	detail := log.FormatError(err)
	if !strings.Contains(detail, "eino.ChatModel.Generate") || !strings.Contains(detail, "model_observability.go:") {
		t.Fatalf("Generate error is missing source context:\n%s", detail)
	}
	logged := output.String()
	for _, expected := range []string{"model call started", "model call failed", "span_name=model.invoke", "span_id=", "model=gpt-test", "mode=generate", "cost_ms=", "error_chain="} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("model log missing %q:\n%s", expected, logged)
		}
	}
	records := collector.Snapshot()
	if len(records) != 1 || records[0].ModelID != "model-test" || records[0].RequestCount != 1 || records[0].TotalTokens != 0 {
		t.Fatalf("failed model call was not recorded: %+v", records)
	}
}

func TestObservedChatModelStreamLogsFullLifecycle(t *testing.T) {
	var output bytes.Buffer
	log.SetOutput(&output)
	defer log.SetOutput(nil)

	final := &schema.Message{
		Role:    schema.Assistant,
		Content: "hello",
		ResponseMeta: &schema.ResponseMeta{
			FinishReason: "stop",
			Usage: &schema.TokenUsage{
				PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10,
			},
		},
	}
	ctx, collector := WithUsageCollector(context.Background())
	observed := newObservedChatModel(&observableFakeModel{stream: []*schema.Message{final}}, ModelIdentity{ModelID: "model-test", Provider: "openai", Model: "gpt-test"})
	stream, err := observed.Stream(ctx, []*schema.Message{schema.UserMessage("hello")})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	defer stream.Close()

	message, err := stream.Recv()
	if err != nil || message == nil || message.Content != "hello" {
		t.Fatalf("stream chunk = %#v, err = %v", message, err)
	}
	if _, err = stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("stream terminal error = %v, want EOF", err)
	}

	logged := output.String()
	for _, expected := range []string{"model call started", "model call completed", "span_name=model.invoke", "span_id=", "mode=stream", "finish_reason=stop", "total_tokens=10", "chunk_count=1", "cost_ms="} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("stream log missing %q:\n%s", expected, logged)
		}
	}
	records := collector.Snapshot()
	if len(records) != 1 || records[0].ModelID != "model-test" || records[0].PromptTokens != 7 || records[0].CompletionTokens != 3 || records[0].TotalTokens != 10 || records[0].RequestCount != 1 {
		t.Fatalf("stream model usage was not recorded: %+v", records)
	}
}
