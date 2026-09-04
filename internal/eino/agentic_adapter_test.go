package eino

import (
	"context"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type scriptedAgenticModel struct {
	mu          sync.Mutex
	inputs      [][]*schema.AgenticMessage
	toolCounts  []int
	streamCalls int
}

func (m *scriptedAgenticModel) Generate(_ context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	m.record(input, opts...)
	return &schema.AgenticMessage{
		Role:          schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlock(&schema.AssistantGenText{Text: "done"})},
		ResponseMeta:  &schema.AgenticResponseMeta{TokenUsage: &schema.TokenUsage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}},
	}, nil
}

func (m *scriptedAgenticModel) Stream(_ context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	m.record(input, opts...)
	m.mu.Lock()
	call := m.streamCalls
	m.streamCalls++
	m.mu.Unlock()

	if call == 0 {
		return schema.StreamReaderFromArray([]*schema.AgenticMessage{
			{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{schema.NewContentBlockChunk(&schema.Reasoning{
					Text: "checking", Signature: "encrypted-reasoning",
				}, &schema.StreamingMeta{Index: 0})},
			},
			{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{schema.NewContentBlockChunk(&schema.FunctionToolCall{
					CallID: "call-lookup", Name: "lookup", Arguments: "{}",
				}, &schema.StreamingMeta{Index: 1})},
				ResponseMeta: &schema.AgenticResponseMeta{TokenUsage: &schema.TokenUsage{
					PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5,
				}},
			},
		}), nil
	}

	return schema.StreamReaderFromArray([]*schema.AgenticMessage{{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{schema.NewContentBlockChunk(
			&schema.AssistantGenText{Text: "done"}, &schema.StreamingMeta{Index: 0},
		)},
		ResponseMeta: &schema.AgenticResponseMeta{TokenUsage: &schema.TokenUsage{
			PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6,
		}},
	}}), nil
}

func (m *scriptedAgenticModel) record(input []*schema.AgenticMessage, opts ...model.Option) {
	common := model.GetCommonOptions(nil, opts...)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputs = append(m.inputs, append([]*schema.AgenticMessage(nil), input...))
	m.toolCounts = append(m.toolCounts, len(common.Tools))
}

func TestAgenticChatModelAdapterGenerateRoundTrip(t *testing.T) {
	inner := &scriptedAgenticModel{}
	adapter := newAgenticChatModelAdapter(inner)
	original := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.Reasoning{Text: "summary", Signature: "encrypted"}),
			schema.NewContentBlock(&schema.FunctionToolCall{CallID: "call-1", Name: "lookup", Arguments: `{"query":"athena"}`}),
		},
	}
	legacy := agenticMessageToLegacy(original, true)
	messages := []*schema.Message{
		schema.UserMessage("find it"),
		legacy,
		schema.ToolMessage("found", "call-1", schema.WithToolName("lookup")),
	}

	result, err := adapter.Generate(context.Background(), messages)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "done" {
		t.Fatalf("content = %q", result.Content)
	}
	if len(inner.inputs) != 1 || len(inner.inputs[0]) != 3 {
		t.Fatalf("captured inputs = %#v", inner.inputs)
	}
	if inner.inputs[0][1] != original {
		t.Fatal("provider-native assistant message was flattened between tool turns")
	}
	toolResult := inner.inputs[0][2].ContentBlocks[0].FunctionToolResult
	if toolResult == nil || toolResult.CallID != "call-1" || toolResult.Name != "lookup" || toolResult.Content[0].Text.Text != "found" {
		t.Fatalf("tool result = %#v", toolResult)
	}
}

func TestAgenticChatModelAdapterStreamRetainsConcatenatedMessage(t *testing.T) {
	inner := &scriptedAgenticModel{}
	adapter := newAgenticChatModelAdapter(inner)
	stream, err := adapter.Stream(context.Background(), []*schema.Message{schema.UserMessage("find it")})
	if err != nil {
		t.Fatal(err)
	}
	message, err := schema.ConcatMessageStream(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Name != "lookup" {
		t.Fatalf("tool calls = %#v", message.ToolCalls)
	}
	original, ok := message.Extra[agenticMessageExtraKey].(*schema.AgenticMessage)
	if !ok || original == nil {
		t.Fatalf("retained message = %#v", message.Extra)
	}
	if len(original.ContentBlocks) != 2 || original.ContentBlocks[0].Reasoning.Signature != "encrypted-reasoning" {
		t.Fatalf("agentic blocks = %#v", original.ContentBlocks)
	}
	if original.ContentBlocks[1].FunctionToolCall.Arguments != "{}" {
		t.Fatalf("function arguments = %q", original.ContentBlocks[1].FunctionToolCall.Arguments)
	}
}

func TestClientStreamExecutesResponsesToolLoop(t *testing.T) {
	inner := &scriptedAgenticModel{}
	client := &Client{model: newAgenticChatModelAdapter(inner), name: "gpt-5.6"}
	lookup := &fakeObservationTool{name: "lookup", output: `{"value":"Athena"}`}
	var chunks []string

	result, err := client.Stream(context.Background(), "find Athena", nil, RunParams{
		DisableBuiltinTools: true,
		ExtraTools:          []tool.BaseTool{lookup},
	}, func(chunk StreamChunk) error {
		chunks = append(chunks, chunk.Text)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "done" || len(chunks) != 1 || chunks[0] != "done" {
		t.Fatalf("result = %#v chunks = %#v", result, chunks)
	}
	if lookup.input != "{}" {
		t.Fatalf("tool input = %q", lookup.input)
	}
	if inner.streamCalls != 2 || len(inner.inputs) != 2 {
		t.Fatalf("stream calls = %d inputs = %d", inner.streamCalls, len(inner.inputs))
	}
	if inner.toolCounts[0] != 1 || inner.toolCounts[1] != 1 {
		t.Fatalf("tool counts = %v", inner.toolCounts)
	}
	if !containsFunctionResult(inner.inputs[1], "call-lookup") {
		t.Fatalf("second request does not contain the tool result: %#v", inner.inputs[1])
	}
}

func containsFunctionResult(messages []*schema.AgenticMessage, callID string) bool {
	for _, message := range messages {
		for _, block := range message.ContentBlocks {
			if block != nil && block.FunctionToolResult != nil && block.FunctionToolResult.CallID == callID {
				return true
			}
		}
	}
	return false
}
