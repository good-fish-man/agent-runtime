package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/tools"
	log "github.com/good-fish-man/logx"

	"github.com/cloudwego/eino/components/tool"
)

const (
	toolMarkupOpen  = "<tools>"
	toolMarkupClose = "</tools>"
)

type textToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// executeTextToolMarkup is a narrow compatibility path for small local models
// that print a supported direct-return tool call instead of returning native tool_calls.
func executeTextToolMarkup(ctx context.Context, content string, available []tool.BaseTool) (string, bool, error) {
	call, matched, err := parseTextToolCall(content)
	if !matched {
		return content, false, nil
	}
	if err != nil {
		log.WarnfCtx(ctx, "[ToolMarkup] ignored malformed text tool call: %v", err)
		return "图片生成请求格式无效，请重试。", true, nil
	}
	if !supportedTextToolCall(call.Name) {
		log.WarnfCtx(ctx, "[ToolMarkup] blocked unsupported text tool call: %s", call.Name)
		return "模型返回了不受支持的工具调用，请重试。", true, nil
	}
	log.WarnfCtx(ctx, "[ToolMarkup] converting model text output to native tool call: %s", call.Name)

	for _, candidate := range available {
		if candidate == nil {
			continue
		}
		info, infoErr := candidate.Info(ctx)
		if infoErr != nil || info == nil || info.Name != call.Name {
			continue
		}
		invokable, ok := tools.TraceTool(candidate).(tool.InvokableTool)
		if !ok {
			return "", true, fmt.Errorf("tool %s is not invokable", call.Name)
		}
		result, runErr := invokable.InvokableRun(ctx, string(call.Arguments))
		if runErr != nil {
			return "", true, log.WrapError(runErr, "eino.executeTextToolMarkup")
		}
		return result, true, nil
	}

	log.WarnfCtx(ctx, "[ToolMarkup] requested tool is unavailable: %s", call.Name)
	if call.Name == tools.GenerateVideoToolName {
		return "当前 Agent 未绑定视频生成模型，请先在 Agent 设置中选择视频模型。", true, nil
	}
	return "当前 Agent 未绑定图片生成模型，请先在 Agent 设置中选择图片模型。", true, nil
}

func supportedTextToolCall(name string) bool {
	switch name {
	case tools.GenerateImageToolName, tools.GenerateVideoToolName,
		capability.ModelName(capability.ImageGenerate), capability.ModelName(capability.VideoGenerate):
		return true
	default:
		return false
	}
}

func parseTextToolCall(content string) (*textToolCall, bool, error) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, toolMarkupOpen) {
		return nil, false, nil
	}
	if !strings.HasSuffix(trimmed, toolMarkupClose) {
		return nil, true, fmt.Errorf("incomplete text tool call")
	}
	payload := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, toolMarkupOpen), toolMarkupClose))
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	var call textToolCall
	if err := decoder.Decode(&call); err != nil {
		return nil, true, fmt.Errorf("invalid text tool call: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, true, fmt.Errorf("invalid trailing text tool call data")
	}
	if call.Name == "" || len(call.Arguments) == 0 || string(call.Arguments) == "null" {
		return nil, true, fmt.Errorf("text tool call requires name and arguments")
	}
	var arguments map[string]any
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil || arguments == nil {
		return nil, true, fmt.Errorf("text tool call arguments must be a JSON object")
	}
	return &call, true, nil
}

type toolMarkupStreamFilter struct {
	emit        func(StreamChunk) error
	buffer      strings.Builder
	passthrough bool
}

func newToolMarkupStreamFilter(emit func(StreamChunk) error) *toolMarkupStreamFilter {
	return &toolMarkupStreamFilter{emit: emit}
}

func (f *toolMarkupStreamFilter) write(chunk StreamChunk) error {
	if f.passthrough {
		return f.emit(chunk)
	}
	f.buffer.WriteString(chunk.Text)
	trimmed := strings.TrimLeftFunc(f.buffer.String(), unicode.IsSpace)
	if trimmed == "" || strings.HasPrefix(toolMarkupOpen, trimmed) || strings.HasPrefix(trimmed, toolMarkupOpen) {
		return nil
	}
	f.passthrough = true
	text := f.buffer.String()
	f.buffer.Reset()
	return f.emit(StreamChunk{Text: text})
}

func (f *toolMarkupStreamFilter) finish(ctx context.Context, available []tool.BaseTool) (string, bool, error) {
	if f.passthrough {
		return "", false, nil
	}
	content := f.buffer.String()
	result, handled, err := executeTextToolMarkup(ctx, content, available)
	if err != nil {
		return "", handled, err
	}
	if !handled {
		result = content
	}
	if result != "" {
		if err := f.emit(StreamChunk{Text: result}); err != nil {
			return "", handled, err
		}
	}
	return result, handled, nil
}
