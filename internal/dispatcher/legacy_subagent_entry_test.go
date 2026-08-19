package dispatcher

import (
	"os"
	"strings"
	"testing"
)

func TestProductionDispatcherHasNoLegacySubagentManagerEntry(t *testing.T) {
	content, err := os.ReadFile("tools.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"NewSubAgentManager", "NewSpawnTool", "NewDelegateTool", "NewParallelSpawnTool"} {
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("legacy request-scoped subagent entry %q remains in production dispatcher", forbidden)
		}
	}
}
