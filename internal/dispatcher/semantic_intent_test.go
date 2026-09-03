package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/intent"
	athenarouter "github.com/good-fish-man/agent-runtime/internal/router"
)

type stubSemanticIntentClassifier struct {
	decision semanticIntentDecision
	err      error
	calls    int
}

func (s *stubSemanticIntentClassifier) Classify(context.Context, intent.Request, intent.Intent) (semanticIntentDecision, error) {
	s.calls++
	return s.decision, s.err
}

type stubIntentCompletionClient struct {
	result *eino.Result
	err    error
	prompt string
	params eino.RunParams
}

func (s *stubIntentCompletionClient) Generate(_ context.Context, prompt string, _ []eino.ChatMessage, params eino.RunParams) (*eino.Result, error) {
	s.prompt, s.params = prompt, params
	return s.result, s.err
}

func semanticTestDispatcher(mode string, classifier semanticIntentClassifier) *Dispatcher {
	return &Dispatcher{
		cfg:        Config{SemanticIntentMode: mode, SemanticIntentConfidence: 0.75},
		intentMode: mode, intentClassifier: classifier,
	}
}

func TestSemanticIntentRecognizesMultilingualFreshWebRequest(t *testing.T) {
	classifier := &stubSemanticIntentClassifier{decision: semanticIntentDecision{
		Mode: intent.ModeResearch, Signals: []intent.Signal{intent.SignalWebAccess},
		RequiresExternalKnowledge: true, RequiresFreshWeb: true, Confidence: 0.96,
	}}
	dispatcher := semanticTestDispatcher(semanticIntentModeHybrid, classifier)

	parsed, source := dispatcher.resolveIntent(context.Background(), intent.Request{Text: "今日の東京の天気を教えて"})

	if source != semanticIntentModeHybrid || !parsed.HasSignal(intent.SignalWebAccess) || parsed.Mode != intent.ModeResearch {
		t.Fatalf("semantic intent = %+v source=%s", parsed, source)
	}
	if route := athenarouter.RouteIntent(parsed); route.Primary != athenarouter.RouteResearch {
		t.Fatalf("semantic route = %+v", route)
	}
}

func TestSemanticIntentCorrectsLexicalWebFalsePositive(t *testing.T) {
	request := intent.Request{Text: "Explain how a web server handles middleware"}
	baseline := intent.Parse(request)
	if !baseline.HasSignal(intent.SignalWebAccess) {
		t.Fatal("test requires the lexical rule baseline to select web access")
	}
	classifier := &stubSemanticIntentClassifier{decision: semanticIntentDecision{
		Mode: intent.ModeChat, Confidence: 0.94, Reason: "stable conceptual explanation",
	}}
	dispatcher := semanticTestDispatcher(semanticIntentModeHybrid, classifier)

	parsed, _ := dispatcher.resolveIntent(context.Background(), request)

	if parsed.HasSignal(intent.SignalWebAccess) || parsed.Mode != intent.ModeChat || !parsed.HasDomain(intent.DomainConversation) {
		t.Fatalf("false positive was not corrected: %+v", parsed)
	}
}

func TestSemanticIntentDoesNotOverrideHardActionSignal(t *testing.T) {
	classifier := &stubSemanticIntentClassifier{decision: semanticIntentDecision{Mode: intent.ModeChat, Confidence: 0.99}}
	dispatcher := semanticTestDispatcher(semanticIntentModeHybrid, classifier)

	parsed, source := dispatcher.resolveIntent(context.Background(), intent.Request{Text: "Open YouTube and play the second video"})

	if source != semanticIntentModeRules || classifier.calls != 0 || !parsed.HasSignal(intent.SignalDirectBrowserControl) {
		t.Fatalf("hard intent changed: %+v source=%s calls=%d", parsed, source, classifier.calls)
	}
}

func TestLocalePackHardActionSkipsSemanticClassifier(t *testing.T) {
	catalog, err := intent.LoadLanguagePacks(filepath.Join("..", "..", "config", "intent-languages"))
	if err != nil {
		t.Fatal(err)
	}
	classifier := &stubSemanticIntentClassifier{decision: semanticIntentDecision{Mode: intent.ModeChat, Confidence: 0.99}}
	dispatcher := semanticTestDispatcher(semanticIntentModeHybrid, classifier)
	dispatcher.intentParser = intent.NewParser(catalog)

	parsed, source := dispatcher.resolveIntent(context.Background(), intent.Request{Text: "ブラウザで動画を再生", Locale: "ja-JP"})

	if source != semanticIntentModeRules || classifier.calls != 0 || !parsed.HasSignal(intent.SignalDirectBrowserControl) {
		t.Fatalf("locale hard intent changed: %+v source=%s calls=%d", parsed, source, classifier.calls)
	}
}

func TestSemanticIntentFailureAndLowConfidenceFallBackToRules(t *testing.T) {
	request := intent.Request{Text: "Find today's technology news"}
	baseline := intent.Parse(request)
	tests := []struct {
		name       string
		classifier *stubSemanticIntentClassifier
		wantSource string
	}{
		{name: "failure", classifier: &stubSemanticIntentClassifier{err: errors.New("model unavailable")}, wantSource: "rules_fallback"},
		{name: "low confidence", classifier: &stubSemanticIntentClassifier{decision: semanticIntentDecision{Mode: intent.ModeChat, Confidence: 0.4}}, wantSource: "rules_low_confidence"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dispatcher := semanticTestDispatcher(semanticIntentModeHybrid, test.classifier)
			parsed, source := dispatcher.resolveIntent(context.Background(), request)
			if source != test.wantSource || !reflect.DeepEqual(parsed, baseline) {
				t.Fatalf("fallback = %+v source=%s, want %+v source=%s", parsed, source, baseline, test.wantSource)
			}
		})
	}
}

func TestSemanticIntentShadowModeOnlyRecordsDecision(t *testing.T) {
	request := intent.Request{Text: "Explain web server architecture"}
	baseline := intent.Parse(request)
	classifier := &stubSemanticIntentClassifier{decision: semanticIntentDecision{Mode: intent.ModeChat, Confidence: 0.95}}
	dispatcher := semanticTestDispatcher(semanticIntentModeShadow, classifier)

	parsed, source := dispatcher.resolveIntent(context.Background(), request)

	if source != semanticIntentModeShadow || !reflect.DeepEqual(parsed, baseline) {
		t.Fatalf("shadow result = %+v source=%s, want baseline %+v", parsed, source, baseline)
	}
}

func TestModelSemanticIntentClassifierUsesBoundedToolFreeJSONCall(t *testing.T) {
	completion := &stubIntentCompletionClient{result: &eino.Result{Content: "```json\n" +
		`{"mode":"research","signals":["web_access"],"requires_external_knowledge":true,"requires_fresh_web":true,"confidence":0.91,"reason":"current weather"}` + "\n```"}}
	classifier := &modelSemanticIntentClassifier{client: completion, timeout: time.Second, maxHistory: 2}
	request := intent.Request{
		Text:                 "今日の東京の天気を教えて",
		PreviousUserMessages: []string{"first", "second", "third"},
		ActiveBrowserSession: true,
	}

	decision, err := classifier.Classify(context.Background(), request, intent.Parse(request))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Mode != intent.ModeResearch || !decision.RequiresFreshWeb || decision.Confidence != 0.91 {
		t.Fatalf("decision = %+v", decision)
	}
	if completion.params.MaxIterations != 1 || !completion.params.DisableBuiltinTools || completion.params.Instruction != semanticIntentInstruction {
		t.Fatalf("unsafe classifier params: %+v", completion.params)
	}
	var payload struct {
		Recent []string `json:"recent_user_messages"`
	}
	if err := json.Unmarshal([]byte(completion.prompt), &payload); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(payload.Recent, []string{"second", "third"}) {
		t.Fatalf("bounded history = %v", payload.Recent)
	}
}

func TestModelSemanticIntentClassifierRejectsUnknownSignals(t *testing.T) {
	completion := &stubIntentCompletionClient{result: &eino.Result{Content: `{"mode":"execute","signals":["purchase_without_approval"],"confidence":0.99}`}}
	classifier := &modelSemanticIntentClassifier{client: completion, timeout: time.Second, maxHistory: 2}

	if _, err := classifier.Classify(context.Background(), intent.Request{Text: "buy it"}, intent.Intent{}); err == nil {
		t.Fatal("unknown semantic signal was accepted")
	}
}

func TestNormalizeSemanticIntentModeFailsClosed(t *testing.T) {
	if got := normalizeSemanticIntentMode("unexpected"); got != semanticIntentModeRules {
		t.Fatalf("invalid mode normalized to %q", got)
	}
}
