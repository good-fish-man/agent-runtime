package decision

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/research/evidence"
	"github.com/good-fish-man/agent-runtime/internal/research/searchsystem"
)

type stagedSearch struct{ rounds int }

func (s *stagedSearch) ExecuteRound(_ context.Context, queries []searchsystem.Query, _ []string, _, _, _ int) (searchsystem.RoundResult, error) {
	s.rounds++
	result := searchsystem.RoundResult{}
	for i, query := range queries {
		kind := query.PreferredSource[0]
		provider, title, pageURL := "public", "Independent protocol analysis", fmt.Sprintf("https://analysis-%d.example/protocol", i)
		switch kind {
		case searchsystem.SourceOfficial:
			provider, title, pageURL = "official", "Official protocol documentation", "https://data.gov/protocol"
		case searchsystem.SourceGitHub:
			provider, title, pageURL = "github-api", "Protocol implementation", "https://github.com/example/protocol"
		case searchsystem.SourceAcademic:
			provider, title, pageURL = "arxiv", "Protocol research paper", "https://arxiv.org/abs/1234.5678"
		}
		hit := searchsystem.Hit{QueryID: query.ID, Provider: provider, Kind: kind, Title: title, URL: pageURL}
		result.Documents = append(result.Documents, searchsystem.Document{Hit: hit, CanonicalURL: hit.URL, Content: fmt.Sprintf("Protocol evidence from research round %d confirms the documented architecture and implementation.", s.rounds), ContentHash: fmt.Sprintf("%d-%d", s.rounds, i), FetchedAt: time.Now()})
		result.Observations = append(result.Observations, searchsystem.Observation{Operation: "search", Status: "success"}, searchsystem.Observation{Operation: "fetch", Status: "success"})
	}
	return result, nil
}

func TestAgentFollowsEvidenceGapsAndStopsWhenSufficient(t *testing.T) {
	search := &stagedSearch{}
	cache := evidence.NewResearchCache()
	agent := NewAgent(search, evidence.NewPipeline(), cache)
	task := Task{Kind: "research", Prompt: "research protocol architecture", InitialQueries: []string{"protocol architecture"}, MinSources: 2}
	outcome, err := agent.Run(context.Background(), task, Budget{MaxQueries: 4, MaxPages: 4, MaxRounds: 3, MaxDuration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if search.rounds < 2 {
		t.Fatalf("agent stopped before filling evidence gaps: %+v", outcome.Result)
	}
	if outcome.Result.StopReason != "evidence_sufficient" || outcome.Result.Status != "complete" {
		t.Fatalf("unexpected result: %+v", outcome.Result)
	}
	if len(outcome.Report.Items) < 4 || outcome.Report.AuthoritativeCount == 0 {
		t.Fatalf("evidence was not accumulated across rounds: %+v", outcome.Report)
	}

	cached, err := agent.Run(context.Background(), task, Budget{MaxQueries: 4, MaxPages: 4, MaxRounds: 3, MaxDuration: time.Second})
	if err != nil || !cached.FromCache || search.rounds != 2 {
		t.Fatalf("research cache was not reused: cached=%+v rounds=%d err=%v", cached, search.rounds, err)
	}
}

func TestTechnicalResearchPlansSpecializedSources(t *testing.T) {
	task := Task{Kind: "research", Prompt: "帮我了解 MCP protocol architecture", InitialQueries: []string{"MCP protocol"}}
	intent := (DefaultIntentAnalyzer{}).Analyze(task)
	plan := (DefaultQueryPlanner{}).Plan(task, intent, DefaultBudget())
	var github, official bool
	for _, query := range plan.Queries {
		github = github || containsSource(query.PreferredSource, searchsystem.SourceGitHub)
		official = official || containsSource(query.PreferredSource, searchsystem.SourceOfficial)
	}
	if !github || !official {
		t.Fatalf("technical source plan is incomplete: %+v", plan.Queries)
	}
}

func TestComparisonPlannerUsesCompactIndependentSourceQueries(t *testing.T) {
	prompt := "Research the official MCP architecture, security boundaries, and major SDKs. Compare official documentation with GitHub implementations and cite the valuable pages you used."
	task := Task{Kind: "comparison", Prompt: prompt, InitialQueries: []string{prompt + " 2026-08-10", prompt + " official specifications", prompt + " independent reviews"}}
	plan := (DefaultQueryPlanner{}).Plan(task, (DefaultIntentAnalyzer{}).Analyze(task), DefaultBudget())
	seen := map[searchsystem.SourceKind]bool{}
	for _, query := range plan.Queries {
		if len(strings.Fields(query.Text)) > 8 || len([]rune(query.Text)) > 100 || strings.Contains(strings.ToLower(query.Text), "cite the valuable pages") {
			t.Fatalf("query was not compacted: %q", query.Text)
		}
		for _, kind := range query.PreferredSource {
			seen[kind] = true
		}
	}
	if topic := coreResearchTopic(prompt); topic != "MCP" {
		t.Fatalf("core topic = %q, want MCP", topic)
	}
	if topic := coreResearchTopic("Investigate Model Context Protocol architecture, security boundaries, and SDKs."); topic != "Model Context Protocol" {
		t.Fatalf("expanded core topic = %q, want Model Context Protocol", topic)
	}
	for _, kind := range []searchsystem.SourceKind{searchsystem.SourceOfficial, searchsystem.SourceGeneral, searchsystem.SourceGitHub} {
		if !seen[kind] {
			t.Fatalf("comparison plan missing %s source: %+v", kind, plan.Queries)
		}
	}
}
