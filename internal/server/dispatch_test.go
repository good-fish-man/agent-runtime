package server

import (
	"testing"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"
)

func TestToTypesRunRequestPreservesSubAgentConfig(t *testing.T) {
	req := &runtimev1.RunRequest{Models: map[string]*runtimev1.ModelConfig{
		"default": {Name: "chat-model"},
		"image":   {Name: "image-model", Provider: "OpenAI"},
	}, Capabilities: []*runtimev1.CapabilityConfig{{Id: "internet.search"}}, VisualInputs: []*runtimev1.VisualInput{{
		Id: "image-1", MimeType: "image/png", Data: []byte("image"), Sha256: "abc",
	}}, SubAgents: []*runtimev1.SubAgentConfig{{
		Id: "reviewer", Name: "Reviewer", MaxIterations: 6, TimeoutMs: 30000,
		Model:        &runtimev1.ModelConfig{Name: "small-model"},
		Capabilities: []*runtimev1.CapabilityConfig{{Id: "filesystem.read"}},
		Skills:       []*runtimev1.Skill{{Id: "audit", Name: "Audit"}},
	}}}

	got := toTypesRunRequest(req)
	if got.Models["image"].Name != "image-model" || got.Models["default"].Name != "chat-model" {
		t.Fatalf("model roles lost: %+v", got.Models)
	}
	if len(got.SubAgents) != 1 {
		t.Fatalf("len(SubAgents) = %d, want 1", len(got.SubAgents))
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0].ID != "internet.search" {
		t.Fatalf("run capabilities lost: %+v", got.Capabilities)
	}
	if len(got.VisualInputs) != 1 || got.VisualInputs[0].MIMEType != "image/png" || string(got.VisualInputs[0].Data) != "image" {
		t.Fatalf("visual inputs lost: %+v", got.VisualInputs)
	}
	sub := got.SubAgents[0]
	if sub.ID != "reviewer" || sub.MaxIterations != 6 || sub.TimeoutMs != 30000 {
		t.Fatalf("sub-agent identity or limits lost: %+v", sub)
	}
	if sub.Model == nil || sub.Model.Name != "small-model" || len(sub.Capabilities) != 1 || len(sub.Skills) != 1 {
		t.Fatalf("sub-agent capabilities lost: %+v", sub)
	}
}
