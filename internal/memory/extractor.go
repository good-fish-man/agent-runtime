package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/eino"
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
	apiBase := eino.ExpandEnv(model.APIBase)
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
	apiKey := eino.ExpandEnv(e.model.APIKey)
	if apiKey == "" || e.model.Name == "" {
		return nil, nil
	}

	prompt := buildExtractionPrompt(userInput, assistantOutput)
	raw, err := e.callLLM(ctx, apiKey, prompt)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("parse extraction result: %w", err)
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

func (e *Extractor) callLLM(ctx context.Context, apiKey, prompt string) (string, error) {
	reqBody := map[string]any{
		"model":           e.model.Name,
		"messages":        []map[string]any{{"role": "user", "content": prompt}},
		"temperature":     0.3,
		"response_format": map[string]string{"type": "json_object"},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.apiBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call LLM: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var openAIResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		return "", fmt.Errorf("parse LLM response: %w", err)
	}
	if len(openAIResp.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices")
	}
	return cleanJSONBlock(openAIResp.Choices[0].Message.Content), nil
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
