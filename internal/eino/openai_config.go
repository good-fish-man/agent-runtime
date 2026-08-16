package eino

import (
	"strings"

	"github.com/cloudwego/eino-ext/components/model/openai"
)

type openAIChatModelProfile struct {
	reasoningModel           bool
	requireToolReasoningNone bool
}

func buildOpenAIChatModelConfig(cfg ModelConfig) *openai.ChatModelConfig {
	profile := openAIChatProfile(cfg.Name)
	reasoningEffort := configuredReasoningEffort(cfg.ExtraFields)
	if profile.requireToolReasoningNone {
		// GPT-5.6 function tools on Chat Completions require effective reasoning none.
		reasoningEffort = "none"
	}

	config := &openai.ChatModelConfig{
		APIKey:  ExpandEnv(cfg.APIKey),
		Model:   cfg.Name,
		BaseURL: ExpandEnv(cfg.APIBase),
	}
	if reasoningEffort != "" {
		config.ReasoningEffort = openai.ReasoningEffortLevel(reasoningEffort)
	}

	// The current Eino OpenAI adapter rejects sampling controls for reasoning
	// models before sending the request, including when reasoning is none.
	if !profile.reasoningModel {
		if cfg.Temperature > 0 {
			temperature := float32(cfg.Temperature)
			config.Temperature = &temperature
		}
		if cfg.TopP > 0 {
			topP := float32(cfg.TopP)
			config.TopP = &topP
		}
	}
	if cfg.MaxTokens > 0 {
		maxTokens := cfg.MaxTokens
		if profile.reasoningModel {
			config.MaxCompletionTokens = &maxTokens
		} else {
			config.MaxTokens = &maxTokens
		}
	}
	return config
}

func openAIChatProfile(modelName string) openAIChatModelProfile {
	name := strings.ToLower(strings.TrimSpace(modelName))
	reasoningModel := hasModelFamily(name, "gpt-5") || hasModelFamily(name, "o1") || hasModelFamily(name, "o3") || hasModelFamily(name, "o4")
	return openAIChatModelProfile{
		reasoningModel:           reasoningModel,
		requireToolReasoningNone: hasModelFamily(name, "gpt-5.6"),
	}
}

func hasModelFamily(modelName, family string) bool {
	return modelName == family || strings.HasPrefix(modelName, family+"-") || strings.HasPrefix(modelName, family+".")
}

func configuredReasoningEffort(extraFields map[string]any) string {
	value, _ := extraFields["reasoning_effort"].(string)
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return value
	default:
		return ""
	}
}
