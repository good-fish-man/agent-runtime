package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
	semantics "github.com/good-fish-man/athena-protocol/draft/v0alpha"
)

func TestBrowserOpenUsesExactTargetAndReturnsSession(t *testing.T) {
	result, err := NewBrowserOpenTool().InvokableRun(context.Background(), `{"target":"https://portal.example.com/"}`)
	if err != nil {
		t.Fatal(err)
	}
	var request actionprotocol.Action
	if err := json.Unmarshal([]byte(result), &request); err != nil {
		t.Fatal(err)
	}
	if request.Capability != "browser.open" || request.SessionID == "" || request.Arguments["target"] != "https://portal.example.com/" || request.Arguments["headed"] != true {
		t.Fatalf("unexpected browser open request: %+v", request)
	}
	if destination, _ := request.Arguments["url"].(string); destination != "https://portal.example.com/" {
		t.Fatalf("exact browser destination = %q", destination)
	}
}

func TestBrowserOpenRejectsUnresolvedWebsiteName(t *testing.T) {
	validation := NewBrowserOpenTool().ValidateInput(context.Background(), `{"target":"Acme Portal"}`)
	if validation.Valid || !strings.Contains(validation.Message, "Search System") {
		t.Fatalf("validation = %#v", validation)
	}
}

func TestBrowserTaskEmitsTaskCapability(t *testing.T) {
	result, err := NewBrowserTaskTool().InvokableRun(context.Background(), `{"goal":"Open YouTube and search AI Agent tutorials","target":"YouTube","query":"AI Agent tutorials"}`)
	if err != nil {
		t.Fatal(err)
	}
	var request actionprotocol.Action
	if err := json.Unmarshal([]byte(result), &request); err != nil {
		t.Fatal(err)
	}
	if request.Capability != "browser.task" || request.SessionID == "" || request.Arguments["goal"] == "" || request.Arguments["headed"] != true {
		t.Fatalf("unexpected browser task request: %+v", request)
	}
	trace, err := semantics.TraceFromArguments(request.Arguments)
	if err != nil {
		t.Fatal(err)
	}
	if trace == nil || trace.Outcome.Goal == "" || trace.Plan.DefinitionHash == "" {
		t.Fatalf("browser task has no immutable effect plan: %#v", trace)
	}
	if trace.Outcome.TargetSpec.Selector.Type != "query" || trace.Outcome.TargetSpec.Selector.Value != "AI Agent tutorials" {
		t.Fatalf("browser target was not modeled: %#v", trace.Outcome.TargetSpec)
	}
	if len(trace.Outcome.DesiredEffects) != 1 || len(trace.Outcome.MustPreserve) != 2 || len(trace.Outcome.ForbiddenEffects) != 1 {
		t.Fatalf("browser outcome constraints are incomplete: %#v", trace.Outcome)
	}
}

func TestBrowserTaskCanContinueExistingSession(t *testing.T) {
	result, err := NewBrowserTaskTool().InvokableRun(context.Background(), `{"session_id":"athena-00000000000000000000000000000000","goal":"Search this page for AI Agent tutorials"}`)
	if err != nil {
		t.Fatal(err)
	}
	var request actionprotocol.Action
	if err := json.Unmarshal([]byte(result), &request); err != nil {
		t.Fatal(err)
	}
	if request.Capability != "browser.task" || request.SessionID != "athena-00000000000000000000000000000000" {
		t.Fatalf("existing session was not preserved: %+v", request)
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

func TestBrowserActionSupportsSemanticInteractionSet(t *testing.T) {
	tool := NewBrowserActionTool()
	for _, input := range []string{
		`{"session_id":"athena-00000000000000000000000000000000","action":"hover","ref":"@e1"}`,
		`{"session_id":"athena-00000000000000000000000000000000","action":"select","ref":"@e2","value":"Tokyo"}`,
		`{"session_id":"athena-00000000000000000000000000000000","action":"drag","ref":"@e3","target_ref":"@e4"}`,
		`{"session_id":"athena-00000000000000000000000000000000","action":"back"}`,
		`{"session_id":"athena-00000000000000000000000000000000","action":"forward"}`,
		`{"session_id":"athena-00000000000000000000000000000000","action":"refresh"}`,
	} {
		if validation := tool.ValidateInput(context.Background(), input); !validation.Valid {
			t.Fatalf("browser action rejected %s: %s", input, validation.Message)
		}
	}
	if tool.ValidateInput(context.Background(), `{"session_id":"athena-00000000000000000000000000000000","action":"drag","ref":"@e3","target_ref":"@e3"}`).Valid {
		t.Fatal("drag accepted the same source and target ref")
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

func TestBrowserActionSupportsVerifiedPlayback(t *testing.T) {
	tool := NewBrowserActionTool()
	input := `{"session_id":"athena-00000000000000000000000000000000","action":"play"}`
	if validation := tool.ValidateInput(context.Background(), input); !validation.Valid {
		t.Fatalf("play action rejected: %#v", validation)
	}
	result, err := tool.InvokableRun(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var request actionprotocol.Action
	if err := json.Unmarshal([]byte(result), &request); err != nil {
		t.Fatal(err)
	}
	if request.Capability != "browser.play" || request.SessionID == "" {
		t.Fatalf("unexpected playback request: %#v", request)
	}
}

func TestBrowserActionSupportsPause(t *testing.T) {
	tool := NewBrowserActionTool()
	input := `{"session_id":"athena-00000000000000000000000000000000","action":"pause"}`
	if validation := tool.ValidateInput(context.Background(), input); !validation.Valid {
		t.Fatalf("pause action rejected: %#v", validation)
	}
	result, err := tool.InvokableRun(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var request actionprotocol.Action
	if err := json.Unmarshal([]byte(result), &request); err != nil {
		t.Fatal(err)
	}
	if request.Capability != "browser.pause" || request.SessionID == "" {
		t.Fatalf("unexpected pause request: %#v", request)
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
