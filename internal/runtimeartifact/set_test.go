package runtimeartifact

import (
	"testing"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/capability"
	artifactv1 "github.com/good-fish-man/athena-protocol/draft/runtimeartifact"
	learningv2 "github.com/good-fish-man/athena-protocol/protocol/learning/v2"
)

func TestParseSelectAndRender(t *testing.T) {
	skill := runtimeSkill()
	checksum, err := artifactv1.Checksum(skill)
	if err != nil {
		t.Fatal(err)
	}
	strategy := learningv2.StrategyDefinition{
		ID: "learned.browser.strategy", Version: "0.1.0", Description: "Prefer the reviewed browser plan.",
		Condition:      []learningv2.Predicate{{Field: "context.capabilities", Operator: "contains", Value: "browser.open"}},
		PreferredSkill: skill.ID, OwnerID: "user-1", Visibility: learningv2.VisibilityPrivate, LifecycleState: learningv2.LifecycleApproved,
	}
	strategyChecksum, err := artifactv1.Checksum(strategy)
	if err != nil {
		t.Fatal(err)
	}
	bundle := artifactv1.Bundle{
		Schema: artifactv1.Schema, OwnerID: "user-1", AgentID: "agent-1", BuildID: "build-1",
		BuildChecksum: checksum, ManifestID: "manifest-1", ResolvedAt: time.Now().UTC(),
		Skills:     []artifactv1.SkillArtifact{{Reference: artifactv1.Reference{Kind: artifactv1.KindSkill, ArtifactID: skill.ID, Version: skill.Version, VersionID: "version-1", CandidateID: "candidate-1", Checksum: checksum}, Definition: skill}},
		Strategies: []artifactv1.StrategyArtifact{{Reference: artifactv1.Reference{Kind: artifactv1.KindStrategy, ArtifactID: strategy.ID, Version: strategy.Version, VersionID: "version-2", CandidateID: "candidate-2", Checksum: strategyChecksum}, Definition: strategy}},
	}
	raw, err := bundle.ContextValue()
	if err != nil {
		t.Fatal(err)
	}
	contextValues := map[string]any{artifactv1.ContextKey: raw, "agent_build_id": "build-1", "run_manifest_id": "manifest-1", "environment_fingerprint": "env-1"}
	set, err := ParseContext(contextValues)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := contextValues[artifactv1.ContextKey]; exists {
		t.Fatal("reserved artifact context was not consumed")
	}
	selection := set.Select(contextValues, []string{capability.BrowserTask})
	if len(selection.Skills) != 1 || len(selection.Strategies) != 1 || selection.Bindings[capability.BrowserOpen] != capability.BrowserTask {
		t.Fatalf("selection = %+v", selection)
	}
	if instruction := set.Instruction(selection); !containsText(instruction, "browser.open.open via browser.task") {
		t.Fatalf("instruction = %q", instruction)
	}
}

func TestSelectDoesNotGrantMissingCapability(t *testing.T) {
	skill := runtimeSkill()
	checksum, _ := artifactv1.Checksum(skill)
	set := &Set{bundle: artifactv1.Bundle{Skills: []artifactv1.SkillArtifact{{Reference: artifactv1.Reference{ArtifactID: skill.ID}, Definition: skill}}}}
	selection := set.Select(map[string]any{"environment_fingerprint": "env-1"}, nil)
	if len(selection.Skills) != 0 || len(selection.UnavailableSkill[skill.ID]) != 1 {
		t.Fatalf("selection = %+v, checksum=%s", selection, checksum)
	}
}

func runtimeSkill() learningv2.SkillDefinition {
	return learningv2.SkillDefinition{
		ID: "learned.browser.open", Version: "0.1.0", Description: "Open a requested page.",
		Preconditions:        []learningv2.Predicate{{Field: "context.environment_fingerprint", Operator: "exists"}, {Field: "context.capabilities", Operator: "contains_all", Value: []string{"browser.open"}}},
		RequiredCapabilities: []string{"browser.open"}, TaskGraphTemplate: learningv2.TaskGraphTemplate{Steps: []learningv2.TaskStep{{ID: "step-1", Capability: "browser.open", Operation: "open"}}},
		VerificationRules: []learningv2.VerificationRule{{Field: "task.status", Operator: "equals", Expected: "COMPLETED", EvidenceRequired: true}},
		OwnerID:           "user-1", Visibility: learningv2.VisibilityPrivate, LifecycleState: learningv2.LifecycleApproved,
	}
}

func containsText(value, expected string) bool {
	for index := 0; index+len(expected) <= len(value); index++ {
		if value[index:index+len(expected)] == expected {
			return true
		}
	}
	return false
}
