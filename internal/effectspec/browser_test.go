package effectspec

import (
	"testing"

	semantics "github.com/good-fish-man/athena-protocol/draft/v0alpha"
)

func TestNewBrowserTraceModelsOrdinalPlaybackOutcome(t *testing.T) {
	trace := NewBrowserTrace("Open YouTube and play the second video", "YouTube", "", "athena-session")
	if err := trace.Validate(); err != nil {
		t.Fatal(err)
	}
	selector := trace.Outcome.TargetSpec.Selector
	if selector.Type != "ordinal" || selector.Ordinal != 2 || selector.Kind != "video" {
		t.Fatalf("unexpected selector: %+v", selector)
	}
	if got := trace.Outcome.DesiredEffects[0].Predicate; got != "media.playback_state" {
		t.Fatalf("unexpected desired effect %q", got)
	}
	if len(trace.Outcome.MustPreserve) != 2 || len(trace.Outcome.ForbiddenEffects) != 1 || trace.Plan.DefinitionHash == "" {
		t.Fatalf("incomplete trace: %+v", trace)
	}
	if trace.Outcome.MustPreserve[1].Predicate != "browser.authentication_state" {
		t.Fatalf("authentication boundary is not modeled: %+v", trace.Outcome.MustPreserve)
	}
}

func TestNewBrowserTraceModelsCurrentPageSelection(t *testing.T) {
	trace := NewBrowserTrace("Click the Shorts filter", "", "", "athena-session")
	if got := trace.Outcome.DesiredEffects[0].Predicate; got != "target.selection_state" {
		t.Fatalf("unexpected desired effect %q", got)
	}
	if trace.Outcome.ForbiddenEffects[0].Expected != false {
		t.Fatalf("window close guard is not explicit: %+v", trace.Outcome.ForbiddenEffects[0])
	}
	if trace.Schema != semantics.Schema {
		t.Fatalf("unexpected schema %q", trace.Schema)
	}
}
