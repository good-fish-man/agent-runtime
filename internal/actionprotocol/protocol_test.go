package actionprotocol

import (
	"context"
	"testing"
)

func TestNewActionUsesScopedTaskAndMonotonicSequence(t *testing.T) {
	ctx := WithScope(context.Background(), "task-1")
	first := New(ctx, "browser.open", "", nil, RiskMedium, Allow)
	second := New(ctx, "browser.observe", "session-1", nil, RiskLow, Allow)
	if first.TaskID != "task-1" || first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("unexpected correlation: first=%+v second=%+v", first, second)
	}
	if first.Policy.Risk != RiskMedium || first.Policy.Decision != Allow {
		t.Fatalf("unexpected policy: %+v", first.Policy)
	}
}

func TestMarshalRejectsUnknownPolicy(t *testing.T) {
	action := New(WithScope(context.Background(), "task-1"), "browser.open", "", nil, RiskLow, "LEGACY_ALLOW")
	if _, err := Marshal(action); err == nil {
		t.Fatal("unknown policy was accepted")
	}
}

func TestParseRejectsLegacyPayload(t *testing.T) {
	legacyType := "browser_" + "action_request"
	if _, ok := Parse(`{"type":"` + legacyType + `"}`); ok {
		t.Fatal("legacy browser action payload was accepted")
	}
}
