package research

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/tools"
)

func TestExecutorCollectsDiverseSourcesAndDegradesFailures(t *testing.T) {
	executor := &Executor{
		protocol: Protocol{MaxFetchRetries: -1},
		search: func(_ context.Context, query string, _ int) (tools.WebSearchOutput, error) {
			if query == "broken" {
				return tools.WebSearchOutput{}, errors.New("temporary search failure")
			}
			return tools.WebSearchOutput{Status: "ok", Results: []tools.SearchResult{
				{Title: "A", URL: "https://a.example/news", Snippet: "a snippet"},
				{Title: "B", URL: "https://b.example/news", Snippet: "b snippet"},
			}}, nil
		},
		fetch: func(_ context.Context, pageURL string) (tools.WebFetchOutput, error) {
			if strings.Contains(pageURL, "b.example") {
				return tools.WebFetchOutput{URL: pageURL, Status: "fetch_error"}, nil
			}
			return tools.WebFetchOutput{URL: pageURL, Title: "Fetched A", Status: "ok", Content: "verified facts"}, nil
		},
	}

	evidence, err := executor.Execute(context.Background(), Plan{
		Kind: KindNews, Queries: []string{"working", "broken"}, MinSources: 2, MaxSources: 4, Date: "2026-07-31",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Sources) != 1 {
		t.Fatalf("sources = %+v, want one successful source", evidence.Sources)
	}
	if len(evidence.Failures) != 2 {
		t.Fatalf("failures = %v, want search and fetch failures", evidence.Failures)
	}
	section := evidence.ContextSection()
	for _, want := range []string{"Coverage warning", "https://a.example/news", "verified facts", "untrusted evidence"} {
		if !strings.Contains(section, want) {
			t.Fatalf("context section missing %q:\n%s", want, section)
		}
	}
}

func TestExecutorCancellationIsFatal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	executor := &Executor{
		search: func(ctx context.Context, _ string, _ int) (tools.WebSearchOutput, error) {
			cancel()
			return tools.WebSearchOutput{}, ctx.Err()
		},
		fetch: func(context.Context, string) (tools.WebFetchOutput, error) {
			t.Fatal("fetch must not run after cancellation")
			return tools.WebFetchOutput{}, nil
		},
	}

	_, err := executor.Execute(ctx, Plan{Kind: KindResearch, Queries: []string{"query"}, MaxSources: 2})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestExecutorEnforcesProtocolLimitsAndBuildsObservations(t *testing.T) {
	var searches atomic.Int32
	var fetches atomic.Int32
	executor := &Executor{
		protocol: Protocol{MaxSearches: 2, MaxFetches: 3, MaxExecutionTime: time.Second},
		search: func(_ context.Context, query string, _ int) (tools.WebSearchOutput, error) {
			searches.Add(1)
			results := make([]tools.SearchResult, 0, 3)
			for i := 0; i < 3; i++ {
				results = append(results, tools.SearchResult{Title: query, URL: fmt.Sprintf("https://source-%s-%d.example/item", query, i), Snippet: "fact"})
			}
			return tools.WebSearchOutput{Status: "ok", Results: results}, nil
		},
		fetch: func(_ context.Context, pageURL string) (tools.WebFetchOutput, error) {
			fetches.Add(1)
			return tools.WebFetchOutput{Status: "ok", URL: pageURL, Content: "verified"}, nil
		},
	}

	evidence, err := executor.Execute(context.Background(), Plan{
		Kind: KindResearch, Queries: []string{"one", "two", "three"}, MaxSources: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if searches.Load() != 2 || fetches.Load() != 3 {
		t.Fatalf("calls search=%d fetch=%d, want 2 and 3", searches.Load(), fetches.Load())
	}
	if evidence.Metrics.ToolCalls != 5 || len(evidence.Observations) != 5 {
		t.Fatalf("metrics=%+v observations=%+v", evidence.Metrics, evidence.Observations)
	}
}

func TestExecutorProtocolTimeoutReturnsBestEvidence(t *testing.T) {
	executor := &Executor{
		protocol: Protocol{MaxExecutionTime: 10 * time.Millisecond},
		search: func(ctx context.Context, _ string, _ int) (tools.WebSearchOutput, error) {
			<-ctx.Done()
			return tools.WebSearchOutput{}, ctx.Err()
		},
		fetch: func(context.Context, string) (tools.WebFetchOutput, error) {
			return tools.WebFetchOutput{}, nil
		},
	}

	evidence, err := executor.Execute(context.Background(), Plan{Kind: KindResearch, Queries: []string{"slow"}})
	if err != nil {
		t.Fatalf("protocol timeout must degrade instead of failing: %v", err)
	}
	if !evidence.LimitReached || len(evidence.Failures) == 0 {
		t.Fatalf("timeout was not recorded: %+v", evidence)
	}
}

func TestExecutorCachesSearchAndFetchResponses(t *testing.T) {
	var searches atomic.Int32
	var fetches atomic.Int32
	executor := &Executor{
		protocol: DefaultProtocol(),
		cache:    &responseCache{searches: make(map[string]cachedSearch), fetches: make(map[string]cachedFetchResult)},
		search: func(context.Context, string, int) (tools.WebSearchOutput, error) {
			searches.Add(1)
			return tools.WebSearchOutput{Status: "ok", Results: []tools.SearchResult{{URL: "https://example.com/fact", Snippet: "fact"}}}, nil
		},
		fetch: func(_ context.Context, pageURL string) (tools.WebFetchOutput, error) {
			fetches.Add(1)
			return tools.WebFetchOutput{Status: "ok", URL: pageURL, Content: "verified"}, nil
		},
	}
	plan := Plan{Kind: KindResearch, Queries: []string{"cached"}, MaxSources: 1}
	if _, err := executor.Execute(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	second, err := executor.Execute(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if searches.Load() != 1 || fetches.Load() != 1 || second.Metrics.CacheHits != 2 {
		t.Fatalf("cache calls search=%d fetch=%d metrics=%+v", searches.Load(), fetches.Load(), second.Metrics)
	}
}

func TestExecutorRetriesTransientFetchErrors(t *testing.T) {
	var calls atomic.Int32
	executor := &Executor{
		protocol: Protocol{MaxFetchRetries: 2, RetryBackoff: time.Millisecond},
		search: func(context.Context, string, int) (tools.WebSearchOutput, error) {
			return tools.WebSearchOutput{Status: "ok", Results: []tools.SearchResult{{URL: "https://example.com/fact"}}}, nil
		},
		fetch: func(_ context.Context, pageURL string) (tools.WebFetchOutput, error) {
			if calls.Add(1) < 3 {
				return tools.WebFetchOutput{Status: "fetch_error", URL: pageURL}, nil
			}
			return tools.WebFetchOutput{Status: "ok", URL: pageURL, Content: "verified"}, nil
		},
	}
	evidence, err := executor.Execute(context.Background(), Plan{Kind: KindResearch, Queries: []string{"retry"}, MaxSources: 1})
	if err != nil || len(evidence.Sources) != 1 || calls.Load() != 3 {
		t.Fatalf("retry result calls=%d evidence=%+v err=%v", calls.Load(), evidence, err)
	}
}

func TestEvidenceContextHonorsProtocolBudget(t *testing.T) {
	evidence := Evidence{
		Plan: Plan{Kind: KindResearch}, ContextLimit: 200,
		Sources: []Source{{Title: "Large", URL: "https://example.com", Content: strings.Repeat("x", 1000)}},
	}
	if section := evidence.ContextSection(); len(section) > 203 { // truncate adds "..."
		t.Fatalf("context length = %d, want bounded context", len(section))
	}
}

func TestSelectCandidatesRoundRobinsAndDeduplicatesHosts(t *testing.T) {
	groups := [][]tools.SearchResult{
		{{Title: "A1", URL: "https://a.example/1"}, {Title: "A2", URL: "https://a.example/2"}},
		{{Title: "B1", URL: "https://b.example/1"}, {Title: "C1", URL: "https://c.example/1"}},
	}
	got := selectCandidates([]string{"https://a.example/1"}, groups, 4)
	if len(got) != 4 {
		t.Fatalf("candidates = %+v, want 4", got)
	}
	if got[1].URL != "https://b.example/1" {
		t.Fatalf("round-robin order = %+v", got)
	}
}

func TestNewsQualityGateRejectsAlreadyAnsweredClarification(t *testing.T) {
	evidence := Evidence{Plan: Plan{Kind: KindNews, Date: "2026-07-31", ResponseLanguage: "Chinese"}}
	invalid := []string{
		"Could you specify the time or day for Tokyo's current news?",
		"Would you like news or weather?",
		"请指定具体时间。",
	}
	for _, answer := range invalid {
		if rejected, _ := evidence.NeedsRepair(answer); !rejected {
			t.Errorf("answer was not rejected: %q", answer)
		}
	}
	if rejected, reason := evidence.NeedsRepair("这是今天的新闻整理。\n来源：https://example.com/news"); rejected {
		t.Fatalf("valid answer rejected: %s", reason)
	}
}
