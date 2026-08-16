package eino

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

func TestBuildOpenAIChatModelConfigUsesGPT56ToolCompatibility(t *testing.T) {
	config := buildOpenAIChatModelConfig(ModelConfig{
		Provider: "openai", Name: "gpt-5.6-sol", APIKey: "key", APIBase: "https://api.openai.com/v1",
		Temperature: 0.7, TopP: 0.9, MaxTokens: 4096,
		ExtraFields: map[string]any{"reasoning_effort": "high"},
	})

	if string(config.ReasoningEffort) != "none" {
		t.Fatalf("reasoning effort = %q, want none", config.ReasoningEffort)
	}
	if config.MaxCompletionTokens == nil || *config.MaxCompletionTokens != 4096 || config.MaxTokens != nil {
		t.Fatalf("unexpected token fields: max_completion_tokens=%v max_tokens=%v", config.MaxCompletionTokens, config.MaxTokens)
	}
	if config.Temperature != nil || config.TopP != nil {
		t.Fatal("GPT-5.6 must omit sampling controls unsupported by the current adapter")
	}
}

func TestBuildOpenAIChatModelConfigKeepsGPT55DefaultsCompatible(t *testing.T) {
	config := buildOpenAIChatModelConfig(ModelConfig{
		Provider: "openai", Name: "gpt-5.5", Temperature: 0.7, TopP: 0.9, MaxTokens: 2048,
	})

	if config.ReasoningEffort != "" {
		t.Fatalf("reasoning effort = %q, want provider default", config.ReasoningEffort)
	}
	if config.Temperature != nil || config.TopP != nil {
		t.Fatal("GPT-5.5 default reasoning must not send unsupported sampling controls")
	}
	if config.MaxCompletionTokens == nil || *config.MaxCompletionTokens != 2048 || config.MaxTokens != nil {
		t.Fatalf("unexpected token fields: max_completion_tokens=%v max_tokens=%v", config.MaxCompletionTokens, config.MaxTokens)
	}
}

func TestBuildOpenAIChatModelConfigOmitsGPT55SamplingWithReasoningNone(t *testing.T) {
	config := buildOpenAIChatModelConfig(ModelConfig{
		Name: "gpt-5.5", Temperature: 0.5, TopP: 0.8,
		ExtraFields: map[string]any{"reasoning_effort": "none"},
	})

	if string(config.ReasoningEffort) != "none" || config.Temperature != nil || config.TopP != nil {
		t.Fatalf("reasoning model sampling controls were not omitted: %+v", config)
	}
}

func TestBuildOpenAIChatModelConfigPreservesLegacyFields(t *testing.T) {
	config := buildOpenAIChatModelConfig(ModelConfig{Name: "gpt-4o", Temperature: 0.4, TopP: 0.8, MaxTokens: 1024})
	if config.MaxTokens == nil || *config.MaxTokens != 1024 || config.MaxCompletionTokens != nil {
		t.Fatalf("unexpected legacy token fields: max_tokens=%v max_completion_tokens=%v", config.MaxTokens, config.MaxCompletionTokens)
	}
	if config.Temperature == nil || config.TopP == nil {
		t.Fatal("legacy model sampling controls were removed")
	}
}

func TestGPT56ToolRequestSerializesCompatibleParameters(t *testing.T) {
	requestBody := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		requestBody <- body
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"id":"test","object":"chat.completion","created":1,"model":"gpt-5.6-sol","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	config := buildOpenAIChatModelConfig(ModelConfig{
		Name: "gpt-5.6-sol", APIKey: "test", APIBase: server.URL,
		Temperature: 0.6, MaxTokens: 512,
	})
	chatModel, err := openai.NewChatModel(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	if err := chatModel.BindTools([]*schema.ToolInfo{{
		Name: "lookup",
		Desc: "Lookup a value",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {Type: schema.String, Required: true},
		}),
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := chatModel.Generate(t.Context(), []*schema.Message{schema.UserMessage("hello")}); err != nil {
		t.Fatal(err)
	}

	body := <-requestBody
	if body["reasoning_effort"] != "none" {
		t.Fatalf("serialized reasoning_effort = %v", body["reasoning_effort"])
	}
	if body["max_completion_tokens"] != float64(512) {
		t.Fatalf("serialized max_completion_tokens = %v", body["max_completion_tokens"])
	}
	if _, exists := body["max_tokens"]; exists {
		t.Fatalf("deprecated max_tokens was serialized: %v", body["max_tokens"])
	}
	if _, exists := body["temperature"]; exists {
		t.Fatalf("unsupported temperature was serialized: %v", body["temperature"])
	}
	if _, exists := body["top_p"]; exists {
		t.Fatalf("unsupported top_p was serialized: %v", body["top_p"])
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("serialized tools = %#v", body["tools"])
	}
}
