package dispatcher

import (
	"context"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/research"
	"github.com/good-fish-man/agent-runtime/internal/types"
)

type browserHandoffResearchRunner struct {
	plan     research.Plan
	evidence research.Evidence
	err      error
}

func (r *browserHandoffResearchRunner) Execute(_ context.Context, plan research.Plan) (research.Evidence, error) {
	r.plan = plan
	return r.evidence, r.err
}

func TestDispatchCapabilityHandoffSearchesThenResumesSameBrowserSession(t *testing.T) {
	runner := &browserHandoffResearchRunner{evidence: research.Evidence{Sources: []research.Source{
		{Title: "Google results", URL: "https://www.google.com/search?q=vimeo", EvidenceScore: 0.99, TrustScore: 0.9},
		{Title: "Vimeo - Official", URL: "https://vimeo.com/features/video-player", EvidenceScore: 0.92, TrustScore: 0.85, RelevanceScore: 0.94},
	}}}
	const sessionID = "athena-11111111111111111111111111111111"
	d := &Dispatcher{
		req: &types.RunRequest{Context: map[string]any{
			"latest_action_observation": map[string]any{
				"status": "SUCCEEDED",
				"state": map[string]any{"capability_handoff": map[string]any{
					"schema": capabilityHandoffSchema, "from": "browser.task", "to": "internet.search",
					"query": "Vimeo official website", "session_id": sessionID,
					"resume": map[string]any{
						"capability": "browser.task", "goal": "Open Vimeo home page and play the first video",
						"target": "Vimeo", "query": "", "contextual_media_title": false,
					},
				}},
			},
		}},
		researchExecutor: runner,
	}
	var emitted actionprotocol.Action
	result, handled, err := d.dispatchCapabilityHandoff(
		actionprotocol.WithScope(context.Background(), "handoff-request"), "", func(eino.StreamChunk) error { return nil },
		func(action actionprotocol.Action) error { emitted = action; return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result == nil || result.FinishReason != "client_action" {
		t.Fatalf("result = %#v, handled=%v", result, handled)
	}
	if emitted.Capability != "browser.task" || emitted.SessionID != sessionID {
		t.Fatalf("action = %#v", emitted)
	}
	if emitted.Arguments["target"] != "https://vimeo.com/" || emitted.Arguments["goal"] != "Open Vimeo home page and play the first video" {
		t.Fatalf("arguments = %#v", emitted.Arguments)
	}
	if len(runner.plan.Queries) != 1 || runner.plan.Queries[0] != "Vimeo official website" {
		t.Fatalf("research plan = %#v", runner.plan)
	}
}

func TestSelectBrowserHandoffSourceRejectsSearchProvider(t *testing.T) {
	selected, ok := selectBrowserHandoffSource([]research.Source{
		{Title: "Search", URL: "https://www.google.com/search?q=example", EvidenceScore: 1},
		{Title: "Example official site", URL: "https://example.com/about", EvidenceScore: 0.7},
	}, "Example official website")
	if !ok || selected.URL != "https://example.com/about" {
		t.Fatalf("selected = %#v, ok=%v", selected, ok)
	}
}
