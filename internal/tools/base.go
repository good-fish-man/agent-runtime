package tools

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/good-fish-man/agent-runtime/pkg/errtrace"
)

// ValidationResult 验证结果
type ValidationResult struct {
	Valid     bool
	Message   string
	ErrorCode int
}

// BaseTool 基础工具接口
type BaseTool interface {
	Info(ctx context.Context) (*schema.ToolInfo, error)
	ValidateInput(ctx context.Context, input string) *ValidationResult
	InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error)
}

// OptionalValidateTool 可选验证接口（简化工具可不实现）
type OptionalValidateTool interface {
	ValidateInput(ctx context.Context, input string) *ValidationResult
}

// Adapter 把 BaseTool 适配为 eino tool
type Adapter struct {
	tool      tool.BaseTool
	invokable tool.InvokableTool
}

// TraceTool adds the tool name and call site to execution failures. Wrapping
// at this common boundary avoids duplicate logging in every tool.
func TraceTool(t tool.BaseTool) tool.BaseTool {
	if t == nil {
		return nil
	}
	if _, ok := t.(*Adapter); ok {
		return t
	}
	invokable, ok := t.(tool.InvokableTool)
	if !ok {
		return t
	}
	return &Adapter{tool: t, invokable: invokable}
}

func (a *Adapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return a.tool.Info(ctx)
}

func (a *Adapter) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	if v, ok := a.tool.(OptionalValidateTool); ok {
		if result := v.ValidateInput(ctx, input); !result.Valid {
			return "", &ValidationError{Message: result.Message, Code: result.ErrorCode}
		}
	}
	result, err := a.invokable.InvokableRun(ctx, input, opts...)
	if err == nil {
		return result, nil
	}
	return result, errtrace.Wrap(err, "tool."+a.name(ctx)+".InvokableRun")
}

func (a *Adapter) name(ctx context.Context) string {
	info, err := a.tool.Info(ctx)
	if err == nil && info != nil && info.Name != "" {
		return info.Name
	}
	return fmt.Sprintf("%T", a.tool)
}

// ValidationError 验证错误
type ValidationError struct {
	Message string
	Code    int
}

func (e *ValidationError) Error() string {
	return e.Message
}
