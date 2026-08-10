package contextcompressor

// Token 阈值常量
const (
	AutoCompactBufferTokens      = 13_000
	WarningThresholdBufferTokens = 20_000
	ErrorThresholdBufferTokens   = 20_000
	ManualCompactBufferTokens    = 3_000
)

// ContextWindowSizes 常见文本模型的上下文窗口大小。
// 这里记录官方 API 的默认窗口；云厂商转售和本地部署的限制可能不同。
var ContextWindowSizes = map[string]int{
	// OpenAI
	"gpt-5.6":                 1_050_000,
	"gpt-5.6-sol":             1_050_000,
	"gpt-5.6-terra":           1_050_000,
	"gpt-5.6-luna":            1_050_000,
	"gpt-5.5":                 1_050_000,
	"gpt-5.5-2026-04-23":      1_050_000,
	"gpt-5.4":                 1_050_000,
	"gpt-5.4-2026-03-05":      1_050_000,
	"gpt-5.4-pro":             1_050_000,
	"gpt-5.4-mini":            400_000,
	"gpt-5.4-mini-2026-03-17": 400_000,
	"gpt-5.4-nano":            400_000,
	"gpt-5.3-codex":           400_000,
	"gpt-5.2":                 400_000,
	"gpt-5.2-2025-12-11":      400_000,
	"gpt-5.2-codex":           400_000,
	"gpt-5.1":                 400_000,
	"gpt-5.1-codex":           400_000,
	"gpt-5.1-codex-max":       400_000,
	"gpt-5":                   400_000,
	"gpt-5-2025-08-07":        400_000,
	"gpt-5-codex":             400_000,
	"gpt-5-mini":              400_000,
	"gpt-5-mini-2025-08-07":   400_000,
	"gpt-5-nano":              400_000,
	"gpt-4.1":                 1_047_576,
	"gpt-4.1-2025-04-14":      1_047_576,
	"gpt-4.1-mini":            1_047_576,
	"gpt-4.1-mini-2025-04-14": 1_047_576,
	"gpt-4.1-nano":            1_047_576,
	"gpt-4.1-nano-2025-04-14": 1_047_576,
	"o3":                      200_000,
	"o3-pro":                  200_000,
	"o4-mini":                 200_000,
	"codex-mini-latest":       200_000,
	"gpt-4o":                  128_000,
	"gpt-4o-mini":             128_000,
	"gpt-4-turbo":             128_000,
	"gpt-3.5-turbo":           16_385,

	// Anthropic
	"claude-fable-5":             1_000_000,
	"claude-mythos-5":            1_000_000,
	"claude-opus-5":              1_000_000,
	"claude-sonnet-5":            1_000_000,
	"claude-opus-4-8":            1_000_000,
	"claude-opus-4-7":            1_000_000,
	"claude-opus-4-6":            1_000_000,
	"claude-sonnet-4-6":          1_000_000,
	"claude-opus-4-5":            200_000,
	"claude-sonnet-4-5":          200_000,
	"claude-sonnet-4-5-20250929": 200_000,
	"claude-haiku-4-5":           200_000,
	"claude-haiku-4-5-20251001":  200_000,
	"claude-sonnet-4-20250514":   200_000,
	"claude-opus-4-20250514":     200_000,
	"claude-3-5-sonnet-20241022": 200_000,
	"claude-3-5-haiku-20241022":  200_000,
	"claude-3-opus-20240229":     200_000,
	"claude-3-sonnet-20240229":   200_000,
	"claude-3-haiku-20240307":    200_000,

	// Google Gemini
	"gemini-3.6-flash":                   1_048_576,
	"gemini-3.5-flash":                   1_048_576,
	"gemini-3.5-flash-lite":              1_048_576,
	"gemini-3.1-pro-preview":             1_048_576,
	"gemini-3.1-pro-preview-customtools": 1_048_576,
	"gemini-2.5-pro":                     1_048_576,
	"gemini-2.5-flash":                   1_048_576,
	"gemini-2.5-flash-lite":              1_048_576,

	// DeepSeek
	"deepseek-v4-pro":   1_000_000,
	"deepseek-v4-flash": 1_000_000,

	// Alibaba Qwen
	"qwen3.8-max-preview":      983_616,
	"qwen3.7-max":              1_000_000,
	"qwen3.7-max-preview":      1_000_000,
	"qwen3.7-max-2026-06-08":   1_000_000,
	"qwen3.7-max-2026-05-20":   1_000_000,
	"qwen3.7-max-2026-05-17":   1_000_000,
	"qwen3.7-plus":             1_000_000,
	"qwen3.7-plus-2026-05-26":  1_000_000,
	"qwen3.6-flash":            1_000_000,
	"qwen3.6-flash-2026-04-16": 1_000_000,
	"qwen3.6-plus":             1_000_000,
	"qwen3.6-plus-2026-04-02":  1_000_000,
	"qwen3-coder-plus":         1_000_000,
	"qwen-long":                10_000_000,

	// xAI
	"grok-4.5":        500_000,
	"grok-4.3":        1_000_000,
	"grok-4.3-latest": 1_000_000,
	"grok-latest":     1_000_000,
	"grok-build-0.1":  256_000,

	// Mistral
	"mistral-medium-3-5": 256_000,
	"mistral-small-2603": 256_000,

	// Other common OpenAI-compatible models
	"glm-5":          200_000,
	"glm-5.2":        1_000_000,
	"kimi-k2.7-code": 256_000,
	"MiniMax-M3":     192_000,
	"MiniMax-M2.7":   204_800,
	"mimo-v2.5-pro":  1_000_000,
}

// GetContextWindowSize 获取模型的上下文窗口大小
func GetContextWindowSize(model string) int {
	if size, ok := ContextWindowSizes[model]; ok {
		return size
	}
	// 默认值
	return 150_000
}

// GetEffectiveContextWindowSize 获取有效的上下文窗口大小（减去摘要输出预留）
func GetEffectiveContextWindowSize(model string) int {
	contextWindow := GetContextWindowSize(model)
	reservedForSummary := DefaultMaxOutputTokens
	if reservedForSummary > contextWindow {
		reservedForSummary = contextWindow
	}
	return contextWindow - reservedForSummary
}

// GetAutoCompactThreshold 获取自动压缩阈值
func GetAutoCompactThreshold(model string) int {
	effectiveWindow := GetEffectiveContextWindowSize(model)
	return effectiveWindow - AutoCompactBufferTokens
}

// ShouldAutoCompact 判断是否应该触发自动压缩
func ShouldAutoCompact(messages []Message, model string, tokenizer Tokenizer) bool {
	tokens := tokenizer.EstimateMessages(messages)
	threshold := GetAutoCompactThreshold(model)
	return tokens >= threshold
}

// TokenWarningState Token 警告状态
type TokenWarningState struct {
	PercentLeft                 int  // 剩余百分比
	IsAboveWarningThreshold     bool // 是否超过警告阈值
	IsAboveErrorThreshold       bool // 是否超过错误阈值
	IsAboveAutoCompactThreshold bool // 是否超过自动压缩阈值
	IsAtBlockingLimit           bool // 是否达到阻塞限制
}

// CalculateTokenWarningState 计算 Token 警告状态
func CalculateTokenWarningState(tokenUsage int, model string, autoCompactEnabled bool) TokenWarningState {
	autoCompactThreshold := GetAutoCompactThreshold(model)
	effectiveWindow := GetEffectiveContextWindowSize(model)

	if !autoCompactEnabled {
		effectiveWindow = GetContextWindowSize(model)
	}

	percentLeft := 0
	if effectiveWindow > 0 {
		percentLeft = max(0, ((effectiveWindow-tokenUsage)*100)/effectiveWindow)
	}

	warningThreshold := effectiveWindow - WarningThresholdBufferTokens
	errorThreshold := effectiveWindow - ErrorThresholdBufferTokens
	blockingLimit := effectiveWindow - ManualCompactBufferTokens

	return TokenWarningState{
		PercentLeft:                 percentLeft,
		IsAboveWarningThreshold:     tokenUsage >= warningThreshold,
		IsAboveErrorThreshold:       tokenUsage >= errorThreshold,
		IsAboveAutoCompactThreshold: autoCompactEnabled && tokenUsage >= autoCompactThreshold,
		IsAtBlockingLimit:           tokenUsage >= blockingLimit,
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
