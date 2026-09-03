package server

import (
	"context"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
	protocolv4 "github.com/good-fish-man/athena-protocol/protocol/v4"
)

func TestClientActionPayloadPreservesRequiredProtocolFields(t *testing.T) {
	action := actionprotocol.New(context.Background(), "browser.open", "athena-browser-1", map[string]any{
		"url": "https://example.com/",
	}, actionprotocol.RiskMedium, actionprotocol.Allow)

	payload, err := clientActionPayload(action)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := protocolv4.ActionFromMap(payload.AsMap())
	if err != nil {
		t.Fatalf("transport payload is not a valid action: %v", err)
	}
	if decoded.TaskID != action.TaskID || decoded.StepID != action.StepID || decoded.ActionID != action.ActionID {
		t.Fatalf("action identity changed: got %+v, want %+v", decoded, action)
	}
	if decoded.Revision != action.Revision || decoded.Operation != action.Operation || decoded.IssuedAt != action.IssuedAt {
		t.Fatalf("action protocol fields changed: got %+v, want %+v", decoded, action)
	}
}
