package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
)

func TestBrowserOpenUsesDynamicTargetAndReturnsSession(t *testing.T) {
	result, err := NewBrowserOpenTool().InvokableRun(context.Background(), `{"target":"Acme Portal"}`)
	if err != nil {
		t.Fatal(err)
	}
	var request actionprotocol.Action
	if err := json.Unmarshal([]byte(result), &request); err != nil {
		t.Fatal(err)
	}
	if request.Capability != "browser.open" || request.SessionID == "" || request.Arguments["target"] != "Acme Portal" || request.Arguments["headed"] != true {
		t.Fatalf("unexpected browser open request: %+v", request)
	}
	if destination, _ := request.Arguments["url"].(string); !strings.Contains(destination, "google.com/search") || !strings.Contains(destination, "Acme+Portal") {
		t.Fatalf("website name was not dynamically resolved through search: %q", destination)
	}
}

func TestPublicSearchURL(t *testing.T) {
	got := publicSearchURL("google", "FIFA World Cup 2026", 8)
	if !strings.HasPrefix(got, "https://www.google.com/search?") || !strings.Contains(got, "FIFA+World+Cup+2026") {
		t.Fatalf("unexpected Google search URL: %s", got)
	}
}

func TestBrowserActionOnlyAcceptsNavigationActions(t *testing.T) {
	tool := NewBrowserActionTool()
	for _, input := range []string{
		`{"session_id":"athena-00000000000000000000000000000000","action":"fill","ref":"@e1","value":"secret"}`,
		`{"session_id":"athena-00000000000000000000000000000000","action":"click","ref":"#checkout"}`,
		`{"session_id":"athena-00000000000000000000000000000000","action":"press","value":"Control+L"}`,
	} {
		if tool.ValidateInput(context.Background(), input).Valid {
			t.Fatalf("unsafe browser action accepted: %s", input)
		}
	}
}

func TestBrowserActionAcceptsReferencedTextInput(t *testing.T) {
	validation := NewBrowserActionTool().ValidateInput(context.Background(), `{"session_id":"athena-00000000000000000000000000000000","action":"type","ref":"@e1","value":"search phrase"}`)
	if !validation.Valid {
		t.Fatalf("safe referenced browser input rejected: %s", validation.Message)
	}
}

func TestBrowserActionSupportsWaitScreenshotAndDownload(t *testing.T) {
	tool := NewBrowserActionTool()
	for _, test := range []struct {
		input      string
		capability string
	}{
		{`{"session_id":"athena-00000000000000000000000000000000","action":"wait","value":"1500"}`, "browser.wait"},
		{`{"session_id":"athena-00000000000000000000000000000000","action":"screenshot"}`, "browser.screenshot"},
		{`{"session_id":"athena-00000000000000000000000000000000","action":"download","ref":"@e3","value":"report.pdf"}`, "browser.download"},
	} {
		if validation := tool.ValidateInput(context.Background(), test.input); !validation.Valid {
			t.Fatalf("browser action rejected %s: %s", test.input, validation.Message)
		}
		result, err := tool.InvokableRun(context.Background(), test.input)
		if err != nil {
			t.Fatalf("run %s: %v", test.input, err)
		}
		var request actionprotocol.Action
		if err := json.Unmarshal([]byte(result), &request); err != nil {
			t.Fatal(err)
		}
		if request.Capability != test.capability {
			t.Fatalf("capability=%q want=%q request=%+v", request.Capability, test.capability, request)
		}
	}
}

func TestBrowserObserveDelegatesToClientController(t *testing.T) {
	result, err := NewBrowserObserveTool().InvokableRun(context.Background(), `{"session_id":"athena-00000000000000000000000000000000"}`)
	if err != nil {
		t.Fatal(err)
	}
	var request actionprotocol.Action
	if err := json.Unmarshal([]byte(result), &request); err != nil {
		t.Fatal(err)
	}
	if request.Capability != "browser.observe" || request.SessionID != "athena-00000000000000000000000000000000" || request.Policy.Decision != actionprotocol.Allow {
		t.Fatalf("unexpected browser observe request: %+v", request)
	}
}

func TestBrowserNavigateDelegatesExistingSession(t *testing.T) {
	result, err := NewBrowserNavigateTool().InvokableRun(context.Background(), `{"session_id":"athena-00000000000000000000000000000000","url":"https://www.youtube.com/results?search_query=ai+agent"}`)
	if err != nil {
		t.Fatal(err)
	}
	var request actionprotocol.Action
	if err := json.Unmarshal([]byte(result), &request); err != nil {
		t.Fatal(err)
	}
	if request.Capability != "browser.navigate" || request.SessionID != "athena-00000000000000000000000000000000" || request.Arguments["url"] == "" {
		t.Fatalf("unexpected browser navigate request: %+v", request)
	}
}

func TestUsefulSearchSnapshotRejectsChallengePage(t *testing.T) {
	if usefulSearchSnapshot(strings.Repeat("verify you are human https://google.com ", 10)) {
		t.Fatal("challenge page was accepted as search results")
	}
}

func TestExtractBrowserSnapshotResults(t *testing.T) {
	snapshot := `- link "FIFA" [ref=e1] [url=https://www.fifa.com/tournaments/mens/worldcup]
- link "BBC Sport" [ref=e2] [url=https://www.bbc.com/sport/football]`
	results := extractBrowserSnapshotResults(snapshot, 5)
	if len(results) != 2 || results[0].URL != "https://www.fifa.com/tournaments/mens/worldcup" {
		t.Fatalf("unexpected snapshot results: %+v", results)
	}
}
