package dispatcher

import (
	"context"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/research"
	"github.com/good-fish-man/agent-runtime/internal/types"
)

func TestResearchRunCapsPlannerIterations(t *testing.T) {
	d := &Dispatcher{
		req:              &types.RunRequest{Options: &types.RunOptions{MaxIterations: 20}},
		researchEvidence: research.Evidence{Plan: research.Plan{Kind: research.KindResearch}},
	}
	if got := d.maxIterations(); got != research.DefaultProtocol().MaxPlannerIterations {
		t.Fatalf("maxIterations() = %d, want protocol limit %d", got, research.DefaultProtocol().MaxPlannerIterations)
	}
}

func TestResearchRunRemovesModelWebTools(t *testing.T) {
	providers, _, err := capability.GlobalRegistry.Resolve(".", []string{capability.InternetSearch, capability.InternetFetch, capability.FilesystemRead})
	if err != nil {
		t.Fatal(err)
	}
	d := &Dispatcher{
		capabilityIDs: []string{capability.InternetSearch, capability.InternetFetch, capability.BrowserTask, capability.BrowserNavigate, capability.FilesystemRead},
		extraTools:    providers,
	}
	d.disableModelResearchTools(context.Background())
	if containsToolName(d.capabilityIDs, capability.InternetSearch) || containsToolName(d.capabilityIDs, capability.InternetFetch) || containsToolName(d.capabilityIDs, capability.BrowserTask) || containsToolName(d.capabilityIDs, capability.BrowserNavigate) {
		t.Fatalf("research capabilities remain selected: %v", d.capabilityIDs)
	}
	if len(d.extraTools) != 1 {
		t.Fatalf("extra tools = %d, want only the non-web tool", len(d.extraTools))
	}
}
