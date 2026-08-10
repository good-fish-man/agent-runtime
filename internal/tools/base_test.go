package tools

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	log "github.com/good-fish-man/logx"
)

type failingTool struct{ cause error }

func (t *failingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "FailingTool"}, nil
}

func (t *failingTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "", t.cause
}

type successfulTool struct{}

func (t *successfulTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "SuccessfulTool"}, nil
}

func (t *successfulTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "ok", nil
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

func TestTraceToolLogsStartEndDurationAndProviderCallID(t *testing.T) {
	var output bytes.Buffer
	log.SetOutput(&output)
	defer log.SetOutput(nil)

	traced := TraceTool(&successfulTool{})
	invokable := traced.(tool.InvokableTool)
	result, err := invokable.InvokableRun(WithToolCallID(context.Background(), "call-provider-123"), `{"value":1}`)
	if err != nil || result != "ok" {
		t.Fatalf("InvokableRun result = %q, err = %v", result, err)
	}

	logged := output.String()
	for _, expected := range []string{
		"tool call started",
		"tool call completed",
		"call_id=call-provider-123",
		"tool=SuccessfulTool",
		"arguments_bytes=11",
		"output_bytes=2",
		"cost_ms=",
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("tool log missing %q:\n%s", expected, logged)
		}
	}
}
