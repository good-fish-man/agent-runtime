package decision

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/research/evidence"
	"github.com/good-fish-man/agent-runtime/internal/research/searchsystem"
)

type fakeAdvisor struct {
	planned  bool
	verified bool
}

func (a *fakeAdvisor) RefinePlan(_ context.Context, _ PlanAdviceRequest) (PlanAdvice, error) {
	a.planned = true
	return PlanAdvice{Queries: []AdvisedQuery{
		{Text: "protocol security official specification", Purpose: "security", Source: "official", Priority: 92},
		{Text: "ignored invalid source", Source: "made-up"},
	}, Usage: AdvisorUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}, nil
}

func (a *fakeAdvisor) VerifyClaims(_ context.Context, request ClaimVerificationRequest) (ClaimVerification, error) {
	a.verified = true
	if len(request.Report.Claims) == 0 || len(request.Report.Items) == 0 {
		return ClaimVerification{}, nil
	}
	return ClaimVerification{Reviews: []ClaimReview{{
		ClaimID: request.Report.Claims[0].ID, Verdict: "supported",
		SourceIDs: []string{request.Report.Items[0].ID, "invented-source"}, Confidence: 0.91,
	}}, Usage: AdvisorUsage{PromptTokens: 20, CompletionTokens: 6, TotalTokens: 26}}, nil
}

func TestV3AdvisorIsBudgetedAndProgressCompletes(t *testing.T) {
	search := &stagedSearch{}
	advisor := &fakeAdvisor{}
	agent := NewAgent(search, evidence.NewPipeline(), nil)
	var progress []Progress
	outcome, err := agent.RunWithOptions(context.Background(), Task{
		Kind: "research", Prompt: "protocol architecture", InitialQueries: []string{"protocol architecture"}, MinSources: 2,
	}, Budget{MaxQueries: 4, MaxPages: 4, MaxRounds: 3, MaxDuration: time.Second}, RunOptions{
		Advisor: advisor, EnableModelPlanning: true, EnableSemanticVerify: true, MaxAdvisorClaims: 2,
		OnProgress: func(event Progress) error { progress = append(progress, event); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !advisor.planned || !advisor.verified {
		t.Fatalf("advisor phases were not executed: planned=%t verified=%t", advisor.planned, advisor.verified)
	}
	if len(outcome.Plan.Queries) > 4 {
		t.Fatalf("advisor exceeded query budget: %d", len(outcome.Plan.Queries))
	}
	for _, query := range outcome.Plan.Queries {
		if query.Text == "ignored invalid source" {
			t.Fatal("query with an unsupported source was accepted")
		}
	}
	if outcome.Result.Usage.AdvisorCalls != 2 || outcome.Result.Usage.TotalTokens != 41 {
		t.Fatalf("advisor usage was not recorded: %+v", outcome.Result.Usage)
	}
	if len(progress) == 0 || progress[len(progress)-1].Stage != "complete" || !progress[len(progress)-1].Completed {
		t.Fatalf("research progress did not complete: %+v", progress)
	}
	finalProgress := progress[len(progress)-1]
	if len(finalProgress.QueryTexts) == 0 || len(finalProgress.ValuablePages) == 0 {
		t.Fatalf("research transparency details were not emitted: %+v", finalProgress)
	}
	page := finalProgress.ValuablePages[0]
	if page.URL == "" || page.Domain == "" || page.Title == "" || !page.Fetched || page.EvidenceScore <= 0 {
		t.Fatalf("valuable page summary is incomplete: %+v", page)
	}
}

func TestValuablePagesAreBoundedAndExcludeRawContent(t *testing.T) {
	report := evidence.Report{Items: []evidence.Item{
		{
			ID: "source-1", Title: "Official specification", URL: "https://example.com/spec",
			CanonicalURL: "https://example.com/spec", Provider: "official", Kind: searchsystem.SourceOfficial,
			Snippet: strings.Repeat("useful evidence ", 40), Content: strings.Repeat("private page body ", 500),
			FetchedAt: time.Now(), Score: evidence.Score{Authority: 0.9, Relevance: 0.8, Overall: 0.85},
		},
		{ID: "invalid", Title: "Invalid", URL: "file:///tmp/secret", CanonicalURL: "file:///tmp/secret"},
	}}
	pages := valuablePages(report, 1)
	if len(pages) != 1 || pages[0].URL != "https://example.com/spec" {
		t.Fatalf("unexpected valuable pages: %+v", pages)
	}
	if strings.Contains(pages[0].Snippet, "private page body") || len([]rune(pages[0].Snippet)) > 283 {
		t.Fatalf("raw or unbounded page content escaped into progress: %+v", pages[0])
	}
}

func TestSemanticVerificationRejectsInventedReferences(t *testing.T) {
	report := evidence.Report{
		Items:  []evidence.Item{{ID: "source-1"}},
		Claims: []evidence.Claim{{ID: "claim-1", Text: "A claim", SourceIDs: []string{"source-1"}, Confidence: 0.5}},
	}
	updated := applySemanticVerification(report, ClaimVerification{Reviews: []ClaimReview{
		{ClaimID: "claim-1", Verdict: "supported", SourceIDs: []string{"invented"}, Confidence: 0.99},
		{ClaimID: "invented-claim", Verdict: "supported", SourceIDs: []string{"source-1"}, Confidence: 0.99},
	}}, 4)
	if updated.Claims[0].Verification != "" || updated.Claims[0].Confidence != 0.5 {
		t.Fatalf("invalid semantic references changed evidence: %+v", updated.Claims[0])
	}

	updated = applySemanticVerification(report, ClaimVerification{Reviews: []ClaimReview{{
		ClaimID: "claim-1", Verdict: "supported", SourceIDs: []string{"source-1", "invented"}, Confidence: 0.9,
	}}}, 4)
	if updated.Claims[0].Verification != "semantic_supported" || len(updated.Claims[0].SourceIDs) != 1 || updated.Claims[0].SourceIDs[0] != "source-1" {
		t.Fatalf("valid semantic review was not safely applied: %+v", updated.Claims[0])
	}
}

func TestMergeAdvisedQueriesRejectsOversizedAndUnknownSources(t *testing.T) {
	plan := Plan{Queries: []searchsystem.Query{{ID: "q-1", Text: "baseline"}}}
	updated := mergeAdvisedQueries(Task{}, plan, PlanAdvice{Queries: []AdvisedQuery{
		{Text: "baseline", Source: "official"},
		{Text: "valid query", Source: "github"},
		{Text: "invalid query", Source: "custom"},
	}}, Budget{MaxQueries: 2})
	if len(updated.Queries) != 2 || updated.Queries[1].Text != "valid query" {
		t.Fatalf("unexpected advised queries: %+v", updated.Queries)
	}
}

func TestMergeAdvisedProcedureQueryCannotDropGoalOrConstraints(t *testing.T) {
	task := Task{
		Kind: "procedure", Goal: "中国驾照换日本驾照",
		Constraints: []string{"申请人 中国人", "当前在日本工作"},
	}
	updated := mergeAdvisedQueries(task, Plan{}, PlanAdvice{Queries: []AdvisedQuery{{
		Text: "外国免許切替 预约和考试", Source: "official", Priority: 90,
	}}}, Budget{MaxQueries: 2})
	if len(updated.Queries) != 1 {
		t.Fatalf("advised procedure query was not accepted: %+v", updated.Queries)
	}
	query := updated.Queries[0].Text
	for _, required := range []string{"中国驾照换日本驾照", "申请人 中国人", "当前在日本工作", "外国免許切替"} {
		if !strings.Contains(query, required) {
			t.Fatalf("advised query lost %q: %q", required, query)
		}
	}
}

func TestV3CacheHitSkipsModelAdvisor(t *testing.T) {
	search := &stagedSearch{}
	cache := evidence.NewResearchCache()
	agent := NewAgent(search, evidence.NewPipeline(), cache)
	task := Task{Kind: "research", Prompt: "cached protocol", InitialQueries: []string{"cached protocol"}, MinSources: 1}
	budget := Budget{MaxQueries: 2, MaxPages: 2, MaxRounds: 2, MaxDuration: time.Second}
	firstAdvisor := &fakeAdvisor{}
	if _, err := agent.RunWithOptions(context.Background(), task, budget, RunOptions{Advisor: firstAdvisor, EnableModelPlanning: true}); err != nil {
		t.Fatal(err)
	}
	secondAdvisor := &fakeAdvisor{}
	outcome, err := agent.RunWithOptions(context.Background(), task, budget, RunOptions{Advisor: secondAdvisor, EnableModelPlanning: true})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.FromCache || secondAdvisor.planned || secondAdvisor.verified {
		t.Fatalf("cache hit invoked model advisor: outcome=%+v advisor=%+v", outcome.Result.Usage, secondAdvisor)
	}
}
