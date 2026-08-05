package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
)

func TestDesktopActionReturnsRequestWithoutExecutingHostOperation(t *testing.T) {
	result, err := NewDesktopActionTool().InvokableRun(context.Background(), `{"action":"search_files","query":"report","extensions":["pdf"]}`)
	if err != nil {
		t.Fatal(err)
	}
	var request actionprotocol.Action
	if err := json.Unmarshal([]byte(result), &request); err != nil {
		t.Fatal(err)
	}
	if request.Protocol != actionprotocol.Protocol || request.Capability != "file.search" || request.ActionID == "" {
		t.Fatalf("unexpected request: %+v", request)
	}
}

func TestDesktopActionRejectsUnsupportedActions(t *testing.T) {
	validation := NewDesktopActionTool().ValidateInput(context.Background(), `{"action":"run_shell","application":"rm"}`)
	if validation.Valid {
		t.Fatal("unsupported desktop action was accepted")
	}
}

func TestDesktopOpenReturnsPersistentSession(t *testing.T) {
	result, err := NewDesktopActionTool().InvokableRun(context.Background(), `{"action":"open_application","application":"Example Player"}`)
	if err != nil {
		t.Fatal(err)
	}
	var request actionprotocol.Action
	if err := json.Unmarshal([]byte(result), &request); err != nil {
		t.Fatal(err)
	}
	if !desktopSessionIDPattern.MatchString(request.SessionID) || request.Capability != "app.open" {
		t.Fatalf("desktop session was not returned: %+v", request)
	}
}

func TestDesktopControlRequiresSession(t *testing.T) {
	validation := NewDesktopActionTool().ValidateInput(context.Background(), `{"action":"observe"}`)
	if validation.Valid {
		t.Fatal("desktop control without a session was accepted")
	}
}
