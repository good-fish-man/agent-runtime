package tools

import (
	"context"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
)

func TestBrowserAutomationCreateEmitsCapabilityAction(t *testing.T) {
	output, err := NewBrowserAutomationTool().InvokableRun(context.Background(), `{
		"operation":"create",
		"session_id":"athena-0123456789abcdef0123456789abcdef",
		"trigger_type":"element_appeared",
		"trigger_name":"Skip Ad",
		"action_type":"click",
		"action_name":"Skip Ad",
		"verification_type":"element_disappeared",
		"verification_name":"Skip Ad",
		"cooldown_ms":5000
	}`)
	if err != nil {
		t.Fatal(err)
	}
	action, ok := actionprotocol.Parse(output)
	if !ok || action.Capability != "browser.automation" || action.SessionID == "" {
		t.Fatalf("action = %#v", action)
	}
	if action.Arguments["trigger_type"] != "element_appeared" || action.Arguments["action_type"] != "click" {
		t.Fatalf("arguments = %#v", action.Arguments)
	}
	if action.Arguments["verification_type"] != "element_disappeared" {
		t.Fatalf("verification arguments = %#v", action.Arguments)
	}
}

func TestBrowserAutomationValidationRejectsUnsafeShape(t *testing.T) {
	tool := NewBrowserAutomationTool()
	validation := tool.ValidateInput(context.Background(), `{
		"operation":"create",
		"session_id":"athena-0123456789abcdef0123456789abcdef",
		"trigger_type":"element_appeared",
		"action_type":"click"
	}`)
	if validation.Valid {
		t.Fatal("targetless click automation was accepted")
	}
	validation = tool.ValidateInput(context.Background(), `{
		"operation":"create",
		"session_id":"athena-0123456789abcdef0123456789abcdef",
		"trigger_type":"element_appeared",
		"trigger_name":"Skip Ad",
		"action_type":"click",
		"action_name":"Skip Ad"
	}`)
	if validation.Valid {
		t.Fatal("unverified click automation was accepted")
	}
	validation = tool.ValidateInput(context.Background(), `{"operation":"delete"}`)
	if validation.Valid {
		t.Fatal("delete without automation_id was accepted")
	}
}
