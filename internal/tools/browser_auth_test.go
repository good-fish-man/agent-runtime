package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
)

func TestBrowserLoginValidation(t *testing.T) {
	tool := NewBrowserLoginTool()
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{name: "https", input: `{"url":"https://example.com/login"}`, valid: true},
		{name: "localhost", input: `{"url":"http://localhost:3000/login"}`, valid: true},
		{name: "file scheme", input: `{"url":"file:///etc/passwd"}`, valid: false},
		{name: "embedded credentials", input: `{"url":"https://user:secret@example.com"}`, valid: false},
		{name: "relative URL", input: `{"url":"/login"}`, valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.ValidateInput(context.Background(), tt.input)
			if result.Valid != tt.valid {
				t.Fatalf("ValidateInput() valid = %v, want %v: %s", result.Valid, tt.valid, result.Message)
			}
		})
	}
}

func TestBrowserSessionIDIsOpaqueAndValidated(t *testing.T) {
	id, err := newBrowserSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateBrowserSessionID(id); err != nil {
		t.Fatalf("generated session ID rejected: %v", err)
	}
	if err := validateBrowserSessionID("../../browser-profile"); err == nil {
		t.Fatal("path-like session ID was accepted")
	}
}

func TestBrowserReadDelegatesSessionToClientController(t *testing.T) {
	result, err := NewBrowserReadTool().InvokableRun(context.Background(), `{"session_id":"athena-00000000000000000000000000000000"}`)
	if err != nil {
		t.Fatal(err)
	}
	var request actionprotocol.Action
	if err := json.Unmarshal([]byte(result), &request); err != nil {
		t.Fatal(err)
	}
	if request.Protocol != actionprotocol.Protocol || request.Capability != "browser.observe" || request.Policy.Decision != actionprotocol.Allow {
		t.Fatalf("unexpected browser request: %+v", request)
	}
}

func TestTruncateBrowserContent(t *testing.T) {
	content := strings.Repeat("a", maxBrowserContentChars+10)
	got := truncateBrowserContent(content)
	if len(got) <= maxBrowserContentChars || !strings.HasSuffix(got, "[content truncated]") {
		t.Fatal("content was not marked as truncated")
	}
}
