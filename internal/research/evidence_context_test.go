package research

import (
	"strings"
	"testing"
)

func TestEvidenceContextHonorsProtocolBudget(t *testing.T) {
	evidence := Evidence{
		Plan: Plan{Kind: KindResearch}, ContextLimit: 200,
		Sources: []Source{{Title: "Large", URL: "https://example.com", Content: strings.Repeat("x", 1000)}},
	}
	if section := evidence.ContextSection(); len(section) > 203 {
		t.Fatalf("context length = %d, want bounded context", len(section))
	}
}

func TestEvidenceContextContainsStructuredResearchState(t *testing.T) {
	evidence := Evidence{
		Plan:       Plan{Kind: KindResearch, MinSources: 2, ResolvedRequest: "protocol research"},
		StopReason: "budget_exhausted", Confidence: 0.61,
		Sources: []Source{{ID: "source-1", Title: "Spec", URL: "https://example.com/spec", Provider: "official", TrustScore: 0.9, RelevanceScore: 0.8, EvidenceScore: 0.85}},
		Claims:  []Claim{{Text: "The protocol is documented.", SourceIDs: []string{"source-1"}, Verification: "single_source", Confidence: 0.8}},
		Gaps:    []ResearchGap{{Code: "insufficient_diversity", Description: "Need another domain.", Priority: 90}},
	}
	section := evidence.ContextSection()
	for _, expected := range []string{"budget_exhausted", "insufficient_diversity", "source-1", "Extracted claims", "untrusted evidence"} {
		if !strings.Contains(section, expected) {
			t.Fatalf("context missing %q:\n%s", expected, section)
		}
	}
}

func TestNewsQualityGateRejectsAlreadyAnsweredClarification(t *testing.T) {
	evidence := Evidence{Plan: Plan{Kind: KindNews, Date: "2026-07-31", ResponseLanguage: "Chinese"}}
	for _, answer := range []string{
		"Could you specify the time or day for Tokyo's current news?",
		"Would you like news or weather?",
		"请指定具体时间。",
	} {
		if rejected, _ := evidence.NeedsRepair(answer); !rejected {
			t.Errorf("answer was not rejected: %q", answer)
		}
	}
	if rejected, reason := evidence.NeedsRepair("这是今天的新闻整理。\n来源：https://example.com/news"); rejected {
		t.Fatalf("valid answer rejected: %s", reason)
	}
}

func TestResearchQualityGateRejectsPseudoToolsAndMissingCitations(t *testing.T) {
	evidence := Evidence{
		Plan:    Plan{Kind: KindResearch, ResponseLanguage: "English"},
		Sources: []Source{{Title: "Specification", URL: "https://example.com/spec"}},
	}
	for _, answer := range []string{
		`<browser.navigate url="https://example.com"/>`,
		"Please provide the following sources.",
		"The specification defines the protocol.",
	} {
		if rejected, _ := evidence.NeedsRepair(answer); !rejected {
			t.Errorf("answer was not rejected: %q", answer)
		}
	}
	if rejected, reason := evidence.NeedsRepair("The specification defines the protocol: https://example.com/spec"); rejected {
		t.Fatalf("valid research answer rejected: %s", reason)
	}
}
