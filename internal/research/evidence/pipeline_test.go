package evidence

import (
	"context"
	"testing"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/research/searchsystem"
)

func TestPipelineRanksAuthorityAndDetectsContradictions(t *testing.T) {
	round := searchsystem.RoundResult{Documents: []searchsystem.Document{
		{
			Hit:          searchsystem.Hit{QueryID: "q-1", Provider: "official", Kind: searchsystem.SourceOfficial, Title: "Official protocol specification", URL: "https://data.gov/protocol"},
			CanonicalURL: "https://data.gov/protocol", Content: "The protocol supports streaming responses for connected clients.", ContentHash: "a", FetchedAt: time.Now(),
		},
		{
			Hit:          searchsystem.Hit{QueryID: "q-2", Provider: "public", Kind: searchsystem.SourceGeneral, Title: "Protocol analysis", URL: "https://analysis.example/protocol"},
			CanonicalURL: "https://analysis.example/protocol", Content: "The protocol does not support streaming responses for connected clients.", ContentHash: "b", FetchedAt: time.Now(),
		},
	}}
	report := NewPipeline().Merge(Request{Task: "protocol streaming responses", Kind: "research", MinSources: 2}, Report{}, round, 2)
	if len(report.Items) != 2 || report.Items[0].CanonicalURL != "https://data.gov/protocol" {
		t.Fatalf("authority ranking failed: %+v", report.Items)
	}
	if report.AuthoritativeCount == 0 {
		t.Fatalf("authoritative source was not recognized: %+v", report)
	}
	if len(report.Contradictions) == 0 {
		t.Fatalf("opposite claims were not detected: %+v", report.Claims)
	}
}

func TestResearchCacheExpires(t *testing.T) {
	cache := NewResearchCache()
	cache.Put(context.Background(), "key", Report{Items: []Item{{ID: "source"}}})
	if _, ok := cache.Get(context.Background(), "key", time.Minute); !ok {
		t.Fatal("fresh cache entry was not returned")
	}
	if _, ok := cache.Get(context.Background(), "key", time.Nanosecond); ok {
		t.Fatal("expired cache entry was returned")
	}
}

func TestLayeredResearchCacheSurvivesNewInstance(t *testing.T) {
	dir := t.TempDir()
	first := NewLayeredResearchCache(dir)
	report := Report{Items: []Item{{ID: "source", URL: "https://example.com", Content: "public evidence"}}}
	first.Put(context.Background(), "research-key", report)

	second := NewLayeredResearchCache(dir)
	got, ok := second.Get(context.Background(), "research-key", time.Hour)
	if !ok || len(got.Items) != 1 || got.Items[0].Content != "public evidence" {
		t.Fatalf("disk cache was not restored: ok=%t report=%+v", ok, got)
	}
}
