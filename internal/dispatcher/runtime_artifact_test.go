package dispatcher

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/types"
	artifactv1 "github.com/good-fish-man/athena-protocol/draft/runtimeartifact"
	learningv2 "github.com/good-fish-man/athena-protocol/protocol/learning/v2"
)

func TestDispatcherAppliesReviewedRuntimeArtifact(t *testing.T) {
	skill := learningv2.SkillDefinition{
		ID: "learned.browser.open", Version: "0.1.0", Description: "Open a requested web page.",
		Preconditions:        []learningv2.Predicate{{Field: "context.environment_fingerprint", Operator: "exists"}, {Field: "context.capabilities", Operator: "contains_all", Value: []string{"browser.open"}}},
		RequiredCapabilities: []string{"browser.open"},
		TaskGraphTemplate:    learningv2.TaskGraphTemplate{Steps: []learningv2.TaskStep{{ID: "step-1", Capability: "browser.open", Operation: "open"}}},
		VerificationRules:    []learningv2.VerificationRule{{Field: "task.status", Operator: "equals", Expected: "COMPLETED", EvidenceRequired: true}},
		OwnerID:              "user-1", Visibility: learningv2.VisibilityPrivate, LifecycleState: learningv2.LifecycleApproved,
	}
	checksum, err := artifactv1.Checksum(skill)
	if err != nil {
		t.Fatal(err)
	}
	bundle := artifactv1.Bundle{
		Schema: artifactv1.Schema, OwnerID: "user-1", AgentID: "agent-1", BuildID: "build-1", BuildChecksum: checksum,
		ManifestID: "manifest-1", ResolvedAt: time.Now().UTC(),
		Skills: []artifactv1.SkillArtifact{{Reference: artifactv1.Reference{Kind: artifactv1.KindSkill, ArtifactID: skill.ID, Version: skill.Version, VersionID: "version-1", CandidateID: "candidate-1", Checksum: checksum}, Definition: skill}},
	}
	raw, err := bundle.ContextValue()
	if err != nil {
		t.Fatal(err)
	}
	req := &types.RunRequest{Context: map[string]any{
		artifactv1.ContextKey: raw, "agent_build_id": "build-1", "run_manifest_id": "manifest-1", "environment_fingerprint": "env-1",
		"browser_controller": true,
	}}
	dispatcher := New(context.Background(), nil, req, t.TempDir(), "", Config{DisableResearch: true})
	dispatcher.prepareCapabilities(context.Background(), "Open https://example.com in the browser", nil)
	instruction := dispatcher.buildInstruction("Open https://example.com in the browser")
	if !strings.Contains(instruction, "Reviewed Runtime Artifacts") || !strings.Contains(instruction, "learned.browser.open@0.1.0") {
		t.Fatalf("runtime artifact instruction missing: %s", instruction)
	}
	if _, exists := req.Context[artifactv1.ContextKey]; exists {
		t.Fatal("raw runtime artifact bundle leaked into generic request context")
	}
}

func TestDispatcherRejectsMismatchedRuntimeManifest(t *testing.T) {
	req := &types.RunRequest{Context: map[string]any{
		artifactv1.ContextKey: map[string]any{"schema": artifactv1.Schema},
		"agent_build_id":      "build-1", "run_manifest_id": "manifest-1",
	}}
	dispatcher := New(context.Background(), nil, req, t.TempDir(), "", Config{DisableResearch: true})
	if _, err := dispatcher.Run(context.Background(), "hello", nil); err == nil || !strings.Contains(err.Error(), "runtimeArtifacts") {
		t.Fatalf("Run() error = %v", err)
	}
}
