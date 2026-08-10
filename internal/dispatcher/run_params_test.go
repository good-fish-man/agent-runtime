package dispatcher

import (
	"context"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/types"

	"github.com/cloudwego/eino/components/tool"
)

func toolNames(t *testing.T, tools []tool.BaseTool) map[string]bool {
	t.Helper()
	names := make(map[string]bool, len(tools))
	for _, bt := range tools {
		info, err := bt.Info(context.Background())
		if err != nil || info == nil {
			t.Fatalf("tool info: %v", err)
		}
		names[info.Name] = true
	}
	return names
}

func TestNonStreamingRunParamsStripsClientBoundTools(t *testing.T) {
	resolved, unavailable, err := capability.GlobalRegistry.Resolve(".",
		[]string{capability.InternetSearch, capability.BrowserTask, capability.DesktopAction})
	if err != nil {
		t.Fatalf("resolve capabilities: %v", err)
	}
	if len(unavailable) != 0 {
		t.Fatalf("unexpected unavailable capabilities: %v", unavailable)
	}

	d := &Dispatcher{req: &types.RunRequest{}, extraTools: resolved}

	streaming := toolNames(t, d.runParams("instr").ExtraTools)
	if !streaming[capability.ModelName(capability.BrowserTask)] {
		t.Fatalf("streaming params should keep browser tool: %v", streaming)
	}
	if !streaming[capability.ModelName(capability.DesktopAction)] {
		t.Fatalf("streaming params should keep desktop tool: %v", streaming)
	}

	nonStreaming := toolNames(t, d.nonStreamingRunParams(context.Background(), "instr").ExtraTools)
	if nonStreaming[capability.ModelName(capability.BrowserTask)] {
		t.Fatalf("non-streaming params must drop browser tool: %v", nonStreaming)
	}
	if nonStreaming[capability.ModelName(capability.DesktopAction)] {
		t.Fatalf("non-streaming params must drop desktop tool: %v", nonStreaming)
	}
	if !nonStreaming[capability.ModelName(capability.InternetSearch)] {
		t.Fatalf("non-streaming params must keep server tool: %v", nonStreaming)
	}

	if len(d.extraTools) != 3 {
		t.Fatalf("d.extraTools must not be mutated, got %d tools", len(d.extraTools))
	}
}
