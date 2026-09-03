package tools

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/good-fish-man/agent-runtime/internal/observability"
	log "github.com/good-fish-man/logx"
)

type toolCallIDContextKey struct{}

// WithToolCallID preserves the provider's tool_call_id through the common
// execution boundary so model, tool, and observation logs can be correlated.
func WithToolCallID(ctx context.Context, callID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if callID == "" {
		return ctx
	}
	return context.WithValue(ctx, toolCallIDContextKey{}, callID)
}

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
	info, err := a.tool.Info(ctx)
	return info, log.WrapError(err, "tool.Adapter.Info")
}

func (a *Adapter) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (result string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	name := a.name(ctx)
	operation := "tool." + name + ".InvokableRun"
	callID, _ := ctx.Value(toolCallIDContextKey{}).(string)
	span := observability.Begin(ctx, "tool", name, callID,
		"arguments_bytes", len(input),
		"read_only", GlobalRegistry.IsReadOnly(name),
	)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = log.NewError(operation, "panic: %v", recovered)
			log.Errorf(ctx, "tool call panic tool=%s error=%v\n%s", name, recovered, debug.Stack())
		}
		span.End(err, "output_bytes", len(result))
	}()

	if contextErr := ctx.Err(); contextErr != nil {
		return "", log.WrapError(contextErr, operation)
	}
	if v, ok := a.tool.(OptionalValidateTool); ok {
		if validation := v.ValidateInput(ctx, input); validation != nil && !validation.Valid {
			return "", log.WrapError(&ValidationError{Message: validation.Message, Code: validation.ErrorCode}, "tool."+name+".ValidateInput")
		}
	}
	result, err = a.invokable.InvokableRun(ctx, input, opts...)
	if err != nil {
		err = log.WrapError(err, operation)
	}
	return result, err
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
