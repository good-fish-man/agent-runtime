package prompt

import (
	"strings"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/types"
)

func TestBuildDynamicPromptIncludesEffectiveSkills(t *testing.T) {
	req := &types.RunRequest{Skills: []types.Skill{{ID: "pptx", Name: "pptx", Description: "Create presentations"}}}
	prompt := BuildDynamicPrompt(req)
	for _, expected := range []string{"# Available Skills", "**pptx**", "load_skill", "run_skill"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("dynamic prompt missing %q", expected)
		}
	}
}
