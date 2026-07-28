package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/good-fish-man/agent-runtime/log"
)

type failingTool struct{ cause error }

func (t *failingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "FailingTool"}, nil
}

func (t *failingTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "", t.cause
}

func TestTraceToolPreservesCauseAndAddsToolName(t *testing.T) {
	cause := errors.New("execution failed")
	traced := TraceTool(&failingTool{cause: cause})
	invokable, ok := traced.(tool.InvokableTool)
	if !ok {
		t.Fatal("TraceTool removed InvokableTool support")
	}
	_, err := invokable.InvokableRun(context.Background(), `{}`)
	if !errors.Is(err, cause) {
		t.Fatal("tool error cause was not preserved")
	}
	detail := log.FormatError(err)
	if !strings.Contains(detail, "tool.FailingTool.InvokableRun") || !strings.Contains(detail, "base.go:") {
		t.Fatalf("unexpected trace:\n%s", detail)
	}
}
