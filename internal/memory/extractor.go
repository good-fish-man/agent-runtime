package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/observability"
	log "github.com/good-fish-man/logx"
)

// Extractor extracts structured memories from a conversation turn using an
// OpenAI-compatible chat completions endpoint.
type Extractor struct {
	model   eino.ModelConfig
	apiBase string
	client  *http.Client
}

// NewExtractor creates an Extractor from a model config.
func NewExtractor(model eino.ModelConfig) *Extractor {
	model.Name = strings.TrimSpace(model.Name)
	model.APIKey = strings.TrimSpace(model.APIKey)
	apiBase := strings.TrimSpace(model.APIBase)
	if apiBase == "" {
		apiBase = constant.DefaultOpenAIAPIBase
	}
	apiBase = strings.TrimRight(apiBase, "/")
	return &Extractor{
		model:   model,
		apiBase: apiBase,
		client:  &http.Client{},
	}
}

// Extract returns memories mined from the given user/assistant exchange.
func (e *Extractor) Extract(ctx context.Context, userInput, assistantOutput string) ([]ExtractedMemory, error) {
	apiKey := strings.TrimSpace(e.model.APIKey)
	if apiKey == "" || e.model.Name == "" {
		return nil, nil
	}

	prompt := buildExtractionPrompt(userInput, assistantOutput)
	raw, err := e.callLLM(ctx, apiKey, prompt)
	if err != nil {
		return nil, log.WrapError(err, "memory.Extractor.Extract.callLLM")
	}

	var parsed struct {
		Memories []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Type        string `json:"type"`
			Content     string `json:"content"`
		} `json:"memories"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, log.WrapError(err, "memory.Extractor.Extract.parseResponse")
	}

	out := make([]ExtractedMemory, 0, len(parsed.Memories))
	for _, m := range parsed.Memories {
		if m.Name == "" || m.Content == "" {
			continue
		}
		out = append(out, ExtractedMemory{
			Name:        m.Name,
			Description: m.Description,
			Type:        m.Type,
			Content:     m.Content,
			Importance:  2,
		})
	}
	return out, nil
}

func (e *Extractor) callLLM(ctx context.Context, apiKey, prompt string) (result string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	span := observability.Begin(ctx, "model", e.model.Name, "",
		"provider", e.model.Provider,
		"mode", "memory_extract",
		"message_count", 1,
		"bound_tool_count", 0,
	)
	var finishReason string
	var promptTokens, completionTokens, totalTokens, responseBytes int
	defer func() {
		span.End(err,
			"finish_reason", finishReason,
			"prompt_tokens", promptTokens,
			"completion_tokens", completionTokens,
			"total_tokens", totalTokens,
			"response_bytes", responseBytes,
		)
	}()

	reqBody := map[string]any{
		"model":           e.model.Name,
		"messages":        []map[string]any{{"role": "user", "content": prompt}},
		"response_format": map[string]string{"type": "json_object"},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", log.WrapError(err, "memory.Extractor.callLLM.marshalRequest")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.apiBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", log.WrapError(err, "memory.Extractor.callLLM.createRequest")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return "", log.WrapError(err, "memory.Extractor.callLLM.request")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", log.WrapError(err, "memory.Extractor.callLLM.readResponse")
	}
	responseBytes = len(respBody)
	if resp.StatusCode != http.StatusOK {
		return "", log.NewError("memory.Extractor.callLLM.status", "LLM returned status %d: %s", resp.StatusCode, truncateModelError(respBody))
	}

	var openAIResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		return "", log.WrapError(err, "memory.Extractor.callLLM.parseResponse")
	}
	if len(openAIResp.Choices) == 0 {
		return "", log.NewError("memory.Extractor.callLLM.validateResponse", "LLM returned no choices")
	}
	finishReason = openAIResp.Choices[0].FinishReason
	promptTokens = openAIResp.Usage.PromptTokens
	completionTokens = openAIResp.Usage.CompletionTokens
	totalTokens = openAIResp.Usage.TotalTokens
	return cleanJSONBlock(openAIResp.Choices[0].Message.Content), nil
}

func truncateModelError(body []byte) string {
	const maxErrorBytes = 4096
	if len(body) <= maxErrorBytes {
		return strings.TrimSpace(string(body))
	}
	return strings.TrimSpace(string(body[:maxErrorBytes])) + "..."
}

// cleanJSONBlock strips markdown code fences around a JSON payload.
func cleanJSONBlock(content string) string {
	content = strings.TrimSpace(content)
	content = strings.Replace(content, "```json\n", "", 1)
	content = strings.Replace(content, "```\n", "", 1)
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func buildExtractionPrompt(userInput, assistantOutput string) string {
	return `你是一个记忆提取专家。从以下对话中提取关键信息并以 JSON 格式返回。

对话:
用户: ` + userInput + `
助手: ` + assistantOutput + `

记忆类型说明:
- user: 用户角色、偏好、知识 (如 "用户是数据科学家")
- feedback: 用户指导 (如 "用户说不要用 mock 测试")
- project: 项目上下文 (如 "项目截止日期是 3 月 15 日")
- reference: 外部系统指针 (如 "bug 在 Linear 的 INGEST 项目跟踪")

提取规则:
1. 只提取对话中明确提到的信息，不要推测
2. 每条记忆需要有: name(英文简短带下划线), description(描述), type(类型), content(完整内容)
3. 优先提取 feedback 类型（用户明确给过指导的）
4. 最多提取 5 条记忆
5. 如果没有值得记忆的信息，返回空数组 []

返回格式:
{
  "memories": [
    {"name": "user_role", "description": "用户是数据科学家", "type": "user", "content": "..."}
  ]
}`
}
