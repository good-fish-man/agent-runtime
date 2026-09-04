package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/tools"
	"github.com/good-fish-man/athena-protocol/sdk/safety"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type fakeObservationTool struct {
	name   string
	input  string
	output string
}

type concurrentObservationTool struct {
	active atomic.Int32
	peak   atomic.Int32
}

func (t *concurrentObservationTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "Read"}, nil
}

func (t *concurrentObservationTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	current := t.active.Add(1)
	defer t.active.Add(-1)
	for {
		observed := t.peak.Load()
		if current <= observed || t.peak.CompareAndSwap(observed, current) {
			break
		}
	}
	select {
	case <-time.After(20 * time.Millisecond):
		return input, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (f *fakeObservationTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: f.name}, nil
}

func (f *fakeObservationTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	f.input = input
	return f.output, nil
}

func TestIsUserVisibleMessage(t *testing.T) {
	tests := []struct {
		name    string
		message *schema.Message
		want    bool
	}{
		{name: "nil", message: nil, want: false},
		{name: "assistant", message: schema.AssistantMessage("done", nil), want: true},
		{
			name: "generated image",
			message: schema.ToolMessage(
				"![Generated image](https://example.com/image.png)",
				"call-image",
				schema.WithToolName(tools.GenerateImageToolName),
			),
			want: true,
		},
		{
			name:    "other tool",
			message: schema.ToolMessage(`{"private":"result"}`, "call-other", schema.WithToolName("OtherTool")),
			want:    false,
		},
		{
			name: "clarification question",
			message: schema.ToolMessage(
				`{"questions":[{"question":"Drive?","options":[{"label":"Yes"},{"label":"No"}]}]}`,
				"call-question",
				schema.WithToolName(tools.AskUserQuestionToolName),
			),
			want: true,
		},
		{
			name: "capability clarification question",
			message: schema.ToolMessage(
				`{"questions":[{"question":"Drive?","options":[{"label":"Yes"},{"label":"No"}]}]}`,
				"call-question",
				schema.WithToolName("interaction_ask"),
			),
			want: true,
		},
		{
			name: "browser authentication",
			message: schema.ToolMessage(
				`{"type":"browser_authentication","status":"authentication_required"}`,
				"call-browser-login",
				schema.WithToolName(tools.BrowserLoginToolName),
			),
			want: false,
		},
		{name: "user", message: schema.UserMessage("hello"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUserVisibleMessage(tt.message); got != tt.want {
				t.Fatalf("isUserVisibleMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFinalizeStreamResultMarksClientAction(t *testing.T) {
	res := &Result{FinishReason: "stop", ActionCount: 1}
	if err := finalizeStreamResult(res); err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != "client_action" {
		t.Fatalf("finish reason = %q", res.FinishReason)
	}
}

func TestFinalizeStreamResultRejectsEmptyNoUsageNoAction(t *testing.T) {
	err := finalizeStreamResult(&Result{FinishReason: "stop", StreamStats: StreamStats{Events: 1, Chunks: 2, EmptyChunks: 2}})
	if err == nil || !strings.Contains(err.Error(), "empty content") || !strings.Contains(err.Error(), "chunks=2") {
		t.Fatalf("expected empty stream error, got %v", err)
	}
}

func TestFinalizeStreamResultRejectsEmptyWithUsage(t *testing.T) {
	err := finalizeStreamResult(&Result{FinishReason: "stop", Usage: Usage{PromptTokens: 10, TotalTokens: 10}})
	if err == nil || !strings.Contains(err.Error(), "despite token usage") || !strings.Contains(err.Error(), "total_tokens=10") {
		t.Fatalf("expected diagnostic empty response error, got %v", err)
	}
}

func TestFinalizeStreamResultRejectsEmptyToolCallsWithUsage(t *testing.T) {
	idx := 0
	toolCalls := []schema.ToolCall{{
		Index: &idx,
		ID:    "call-1",
		Type:  "function",
		Function: schema.FunctionCall{
			Name:      "interaction_ask",
			Arguments: `{"question":"Continue?"}`,
		},
	}}
	err := finalizeStreamResult(&Result{
		FinishReason: "tool_calls",
		Usage:        Usage{PromptTokens: 2810, CompletionTokens: 332, TotalTokens: 3142},
		StreamStats:  StreamStats{Events: 2, Chunks: 122, ToolCallChunks: 121, UsageChunks: 1},
		ToolCalls:    toolCalls,
	})
	if !IsEmptyToolCallStream(err) {
		t.Fatalf("expected EmptyToolCallStreamError, got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "tool_call_chunks=121") || !strings.Contains(err.Error(), "total_tokens=3142") {
		t.Fatalf("missing diagnostic stats: %v", err)
	}
	got := EmptyToolCalls(err)
	if len(got) != 1 || got[0].Function.Arguments != toolCalls[0].Function.Arguments {
		t.Fatalf("tool calls = %+v", got)
	}
}

func TestRecordStreamChunkStats(t *testing.T) {
	res := &Result{}
	recordStreamChunkStats(res, nil)
	recordStreamChunkStats(res, schema.AssistantMessage("hello", nil))
	recordStreamChunkStats(res, &schema.Message{
		Role: schema.Assistant,
		ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{
			PromptTokens: 4,
			TotalTokens:  4,
		}},
	})

	if res.StreamStats.Chunks != 3 {
		t.Fatalf("chunks = %d", res.StreamStats.Chunks)
	}
	if res.StreamStats.EmptyChunks != 1 {
		t.Fatalf("empty chunks = %d", res.StreamStats.EmptyChunks)
	}
	if res.StreamStats.VisibleChunks != 1 {
		t.Fatalf("visible chunks = %d", res.StreamStats.VisibleChunks)
	}
	if res.StreamStats.UsageChunks != 1 {
		t.Fatalf("usage chunks = %d", res.StreamStats.UsageChunks)
	}
}

func TestConsumeStreamingMessageAggregatesToolCalls(t *testing.T) {
	idx := 0
	stream := schema.StreamReaderFromArray([]*schema.Message{
		{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				Index: &idx,
				ID:    "call-1",
				Type:  "function",
				Function: schema.FunctionCall{
					Name:      "interaction_ask",
					Arguments: `{"question":"`,
				},
			}},
		},
		{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				Index: &idx,
				Function: schema.FunctionCall{
					Arguments: `Continue?"}`,
				},
			}},
			ResponseMeta: &schema.ResponseMeta{
				FinishReason: "tool_calls",
				Usage: &schema.TokenUsage{
					PromptTokens:     10,
					CompletionTokens: 5,
					TotalTokens:      15,
				},
			},
		},
	})

	res := &Result{}
	var emitted []string
	msg, err := (&Client{}).consumeStreamingMessage(nilContext(), stream, res, func(chunk StreamChunk) error {
		emitted = append(emitted, chunk.Text)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(emitted) != 0 {
		t.Fatalf("emitted = %v, want no visible tool-call chunks", emitted)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d", len(msg.ToolCalls))
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("result tool calls = %d", len(res.ToolCalls))
	}
	if got := msg.ToolCalls[0].Function.Arguments; got != `{"question":"Continue?"}` {
		t.Fatalf("arguments = %q", got)
	}
	if res.FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q", res.FinishReason)
	}
	if res.Usage.TotalTokens != 15 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if res.StreamStats.ToolCallChunks != 2 {
		t.Fatalf("tool call chunks = %d", res.StreamStats.ToolCallChunks)
	}
}

func nilContext() context.Context {
	return context.Background()
}

func TestOpenAIResponsesModelConfigGPT56Compatibility(t *testing.T) {
	configured, err := openAIResponsesModelConfig(ModelConfig{
		Name: "gpt-5.6", Temperature: 0.7, MaxTokens: 4096, TopP: 0.8,
		ExtraFields: map[string]any{"reasoning_effort": "xhigh"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.MaxTokens == nil || *configured.MaxTokens != 4096 {
		t.Fatalf("max output tokens = %v", configured.MaxTokens)
	}
	if configured.Temperature != nil || configured.TopP != nil {
		t.Fatalf("GPT-5.6 sampling fields must be omitted: temperature=%v top_p=%v", configured.Temperature, configured.TopP)
	}
	if configured.Reasoning == nil || string(configured.Reasoning.Effort) != "xhigh" {
		got := ""
		if configured.Reasoning != nil {
			got = string(configured.Reasoning.Effort)
		}
		t.Fatalf("reasoning effort = %q", got)
	}
	if configured.Store == nil || *configured.Store {
		t.Fatalf("responses must be stateless: store=%v", configured.Store)
	}
	if len(configured.Include) != 1 || string(configured.Include[0]) != "reasoning.encrypted_content" {
		t.Fatalf("responses include = %v", configured.Include)
	}
}

func TestOpenAIResponsesModelConfigGPT56DefaultsReasoning(t *testing.T) {
	configured, err := openAIResponsesModelConfig(ModelConfig{Name: "gpt-5.6-sol"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(configured.Reasoning.Effort); got != "medium" {
		t.Fatalf("reasoning effort = %q, want medium", got)
	}
}

func TestOpenAIChatModelConfigKeepsLegacyModelParameters(t *testing.T) {
	configured, err := openAIChatModelConfig(ModelConfig{
		Name: "gpt-4o", Temperature: 0.7, MaxTokens: 2048, TopP: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.MaxTokens == nil || *configured.MaxTokens != 2048 || configured.MaxCompletionTokens != nil {
		t.Fatalf("token fields = max_tokens:%v max_completion_tokens:%v", configured.MaxTokens, configured.MaxCompletionTokens)
	}
	if configured.Temperature == nil || configured.TopP == nil {
		t.Fatalf("legacy sampling fields were not preserved: temperature=%v top_p=%v", configured.Temperature, configured.TopP)
	}
}

func TestOpenAIChatModelConfigRejectsInvalidGPT56ReasoningEffort(t *testing.T) {
	_, err := openAIResponsesModelConfig(ModelConfig{
		Name: "gpt-5.6-terra", ExtraFields: map[string]any{"reasoning_effort": "ultra"},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported reasoning_effort") {
		t.Fatalf("error = %v", err)
	}
}

func TestGPT56ResponsesRequestContract(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"resp-test","object":"response","created_at":1,"status":"completed","model":"gpt-5.6","output":[{"id":"msg-test","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"reasoning":{"effort":"medium"},"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	client, err := NewClient(context.Background(), ModelConfig{
		Name: "gpt-5.6", APIKey: "test-key", APIBase: server.URL + "/v1",
		Temperature: 0.7, MaxTokens: 4096, TopP: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	toolInfos := []*schema.ToolInfo{{
		Name: "lookup", Desc: "Look up a value",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {Type: schema.String, Required: true},
		}),
	}}
	message, err := client.Model().Generate(context.Background(), []*schema.Message{schema.UserMessage("hello")}, model.WithTools(toolInfos))
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "ok" {
		t.Fatalf("response content = %q", message.Content)
	}
	reasoning, _ := requestBody["reasoning"].(map[string]any)
	if requestBody["model"] != "gpt-5.6" || reasoning["effort"] != "medium" || requestBody["max_output_tokens"] != float64(4096) {
		t.Fatalf("request body = %+v", requestBody)
	}
	if requestBody["store"] != false {
		t.Fatalf("responses request must disable storage: %+v", requestBody)
	}
	include, _ := requestBody["include"].([]any)
	if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("responses include = %#v", requestBody["include"])
	}
	if tools, ok := requestBody["tools"].([]any); !ok || len(tools) != 1 {
		t.Fatalf("responses tools = %#v", requestBody["tools"])
	}
}

func TestGPT56ResponsesStreamRequestContract(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		response := `{"id":"resp-stream","object":"response","created_at":1,"status":"completed","model":"gpt-5.6","output":[{"id":"msg-stream","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"reasoning":{"effort":"high"},"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`
		_, _ = fmt.Fprintf(writer, "event: response.created\ndata: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":%s}\n\n", response)
		_, _ = fmt.Fprint(writer, "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"sequence_number\":1,\"output_index\":0,\"item\":{\"id\":\"msg-stream\",\"type\":\"message\",\"status\":\"in_progress\",\"role\":\"assistant\",\"content\":[]}}\n\n")
		_, _ = fmt.Fprint(writer, "event: response.content_part.added\ndata: {\"type\":\"response.content_part.added\",\"sequence_number\":2,\"item_id\":\"msg-stream\",\"output_index\":0,\"content_index\":0,\"part\":{\"type\":\"output_text\",\"text\":\"\",\"annotations\":[]}}\n\n")
		_, _ = fmt.Fprint(writer, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":3,\"item_id\":\"msg-stream\",\"output_index\":0,\"content_index\":0,\"delta\":\"ok\"}\n\n")
		_, _ = fmt.Fprint(writer, "event: response.output_text.done\ndata: {\"type\":\"response.output_text.done\",\"sequence_number\":4,\"item_id\":\"msg-stream\",\"output_index\":0,\"content_index\":0,\"text\":\"ok\"}\n\n")
		_, _ = fmt.Fprint(writer, "event: response.content_part.done\ndata: {\"type\":\"response.content_part.done\",\"sequence_number\":5,\"item_id\":\"msg-stream\",\"output_index\":0,\"content_index\":0,\"part\":{\"type\":\"output_text\",\"text\":\"ok\",\"annotations\":[]}}\n\n")
		_, _ = fmt.Fprint(writer, "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"sequence_number\":6,\"output_index\":0,\"item\":{\"id\":\"msg-stream\",\"type\":\"message\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\",\"annotations\":[]}]}}\n\n")
		_, _ = fmt.Fprintf(writer, "event: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":7,\"response\":%s}\n\n", response)
	}))
	defer server.Close()

	client, err := NewClient(context.Background(), ModelConfig{
		Name: "gpt-5.6", APIKey: "test-key", APIBase: server.URL + "/v1",
		ExtraFields: map[string]any{"reasoning_effort": "high"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Model().Stream(context.Background(), []*schema.Message{schema.UserMessage("hello")})
	if err != nil {
		t.Fatal(err)
	}
	message, err := schema.ConcatMessageStream(stream)
	if err != nil {
		t.Fatal(err)
	}
	if message.Content != "ok" {
		t.Fatalf("stream content = %q", message.Content)
	}
	if requestBody["stream"] != true {
		t.Fatalf("stream request body = %+v", requestBody)
	}
	reasoning, _ := requestBody["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Fatalf("stream reasoning = %#v", requestBody["reasoning"])
	}
}

func TestGPT56ResponsesWithToolsPreservesReasoning(t *testing.T) {
	requests := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		var requestBody map[string]any
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		requests <- requestBody
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"resp-test","object":"response","created_at":1,"status":"completed","model":"gpt-5.6","output":[{"id":"msg-test","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"reasoning":{"effort":"xhigh"},"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	client, err := NewClient(context.Background(), ModelConfig{
		Name: "gpt-5.6", APIKey: "test-key", APIBase: server.URL + "/v1",
		MaxTokens: 4096, ExtraFields: map[string]any{"reasoning_effort": "xhigh"},
	})
	if err != nil {
		t.Fatal(err)
	}
	toolInfos := []*schema.ToolInfo{{
		Name: "lookup",
		Desc: "Look up a value",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {Type: schema.String, Required: true},
		}),
	}}
	messages := []*schema.Message{schema.UserMessage("hello")}

	if _, err := client.Model().Generate(context.Background(), messages, model.WithTools(toolInfos)); err != nil {
		t.Fatal(err)
	}

	bound, err := client.Model().WithTools(toolInfos)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Generate(context.Background(), messages); err != nil {
		t.Fatal(err)
	}

	for requestNumber := 1; requestNumber <= 2; requestNumber++ {
		requestBody := <-requests
		reasoning, _ := requestBody["reasoning"].(map[string]any)
		if reasoning["effort"] != "xhigh" {
			t.Fatalf("request %d reasoning = %v, body = %+v", requestNumber, requestBody["reasoning"], requestBody)
		}
		if tools, ok := requestBody["tools"].([]any); !ok || len(tools) != 1 {
			t.Fatalf("request %d tools = %#v", requestNumber, requestBody["tools"])
		}
		if requestBody["max_output_tokens"] != float64(4096) {
			t.Fatalf("request %d max_output_tokens = %v", requestNumber, requestBody["max_output_tokens"])
		}
	}
}

func TestMergeResultAccumulatesModelUsageAcrossToolIterations(t *testing.T) {
	total := &Result{Usage: Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}}
	mergeResult(total, &Result{Usage: Usage{PromptTokens: 180, CompletionTokens: 30, TotalTokens: 210}})

	if total.Usage.PromptTokens != 280 || total.Usage.CompletionTokens != 50 || total.Usage.TotalTokens != 330 {
		t.Fatalf("usage was not accumulated: %+v", total.Usage)
	}
}

func TestExecuteToolCallsAppendsObservationMessages(t *testing.T) {
	fakeTool := &fakeObservationTool{name: "internet_search", output: `{"results":[{"title":"Athena"}]}`}
	calls := []schema.ToolCall{{
		ID:   "call-search",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      fakeTool.name,
			Arguments: `{"query":"Athena"}`,
		},
	}}

	result, err := (&Client{}).executeToolCalls(context.Background(), calls, RunParams{
		ExtraTools:          []tool.BaseTool{fakeTool},
		DisableBuiltinTools: true,
	}, func(StreamChunk) error {
		t.Fatal("non-visible tool should not emit chunks")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fakeTool.input != `{"query":"Athena"}` {
		t.Fatalf("tool input = %s", fakeTool.input)
	}
	if result.Content != "" || result.ActionCount != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("messages = %d", len(result.Messages))
	}
	msg := result.Messages[0]
	if msg.Role != schema.Tool || msg.ToolName != fakeTool.name || msg.ToolCallID != "call-search" {
		t.Fatalf("tool message metadata = %+v", msg)
	}
	if payload := toolEnvelopeData(t, msg.Content); payload != fakeTool.output {
		t.Fatalf("tool message data = %q, envelope = %q", payload, msg.Content)
	}
}

func TestExecuteToolCallsEmitsVisibleToolResult(t *testing.T) {
	imageTool := &fakeImageTool{}
	calls := []schema.ToolCall{{
		ID:   "call-image",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      tools.GenerateImageToolName,
			Arguments: `{"prompt":"cat"}`,
		},
	}}

	var emitted strings.Builder
	result, err := (&Client{}).executeToolCalls(context.Background(), calls, RunParams{
		ExtraTools:          []tool.BaseTool{imageTool},
		DisableBuiltinTools: true,
	}, func(chunk StreamChunk) error {
		emitted.WriteString(chunk.Text)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 0 {
		t.Fatalf("messages = %d", len(result.Messages))
	}
	if !strings.HasPrefix(result.Content, "![Generated image]") || emitted.String() != result.Content {
		t.Fatalf("content = %q emitted = %q", result.Content, emitted.String())
	}
}

func TestExecuteToolCallsRecordsUnavailableToolAsObservation(t *testing.T) {
	calls := []schema.ToolCall{{
		ID:   "call-missing",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      "missing_tool",
			Arguments: `{}`,
		},
	}}

	result, err := (&Client{}).executeToolCalls(context.Background(), calls, RunParams{DisableBuiltinTools: true}, func(StreamChunk) error {
		t.Fatal("missing tool observation should not emit chunks")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("messages = %d", len(result.Messages))
	}
	msg := result.Messages[0]
	if msg.ToolName != "missing_tool" || !strings.Contains(msg.Content, "unavailable") {
		t.Fatalf("tool message = %+v", msg)
	}
}

func TestExecuteToolCallsRunsReadOnlyCapabilitiesWithBoundedConcurrency(t *testing.T) {
	readTool := &concurrentObservationTool{}
	calls := make([]schema.ToolCall, constant.DefaultParallelToolWorkers+4)
	for i := range calls {
		calls[i] = schema.ToolCall{
			ID:   fmt.Sprintf("call-read-%d", i),
			Type: "function",
			Function: schema.FunctionCall{
				Name:      "Read",
				Arguments: fmt.Sprintf(`{"index":%d}`, i),
			},
		}
	}

	result, err := (&Client{}).executeToolCalls(context.Background(), calls, RunParams{
		ExtraTools:          []tool.BaseTool{readTool},
		DisableBuiltinTools: true,
	}, func(StreamChunk) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != len(calls) {
		t.Fatalf("messages = %d, want %d", len(result.Messages), len(calls))
	}
	if peak := readTool.peak.Load(); peak <= 1 || peak > constant.DefaultParallelToolWorkers {
		t.Fatalf("peak concurrency = %d, want 2..%d", peak, constant.DefaultParallelToolWorkers)
	}
	for i, message := range result.Messages {
		if message.ToolCallID != calls[i].ID || toolEnvelopeData(t, message.Content) != calls[i].Function.Arguments {
			t.Fatalf("message[%d] = %+v, want call %s", i, message, calls[i].ID)
		}
	}
}

func toolEnvelopeData(t *testing.T, content string) string {
	t.Helper()
	var envelope safety.Envelope
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		t.Fatalf("decode tool trust envelope: %v", err)
	}
	if envelope.Schema != safety.EnvelopeSchema || envelope.Trust != safety.TrustExternal {
		t.Fatalf("invalid tool trust envelope: %+v", envelope)
	}
	encoded, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("encode tool envelope data: %v", err)
	}
	return string(encoded)
}
