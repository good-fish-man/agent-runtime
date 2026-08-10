package searchsystem

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type fakeProvider struct {
	name string
	kind SourceKind
	urls []string
}

func (p fakeProvider) Name() string     { return p.name }
func (p fakeProvider) Kind() SourceKind { return p.kind }
func (p fakeProvider) Search(_ context.Context, query Query, _ int) ([]Hit, error) {
	result := make([]Hit, 0, len(p.urls))
	for i, pageURL := range p.urls {
		result = append(result, Hit{QueryID: query.ID, Provider: p.name, Kind: p.kind, Title: fmt.Sprintf("result-%d", i), URL: pageURL, Priority: query.Priority, SearchRank: i + 1})
	}
	return result, nil
}

type fakeFetcher struct{}

func (fakeFetcher) Fetch(_ context.Context, hit Hit) (Document, error) {
	return Document{Hit: hit, CanonicalURL: hit.URL, Content: "A sufficiently detailed factual page used by the research pipeline.", FetchedAt: time.Now()}, nil
}

func TestSystemRoutesSourcesDeduplicatesAndBoundsFetches(t *testing.T) {
	router := NewRouter(
		fakeProvider{name: "general", kind: SourceGeneral, urls: []string{"https://general.example/a"}},
		fakeProvider{name: "official", kind: SourceOfficial, urls: []string{
			"https://docs.example/a?utm_source=test", "https://docs.example/a", "https://second.example/b",
		}},
	)
	system := New(router, fakeFetcher{}, DefaultExtractor{MaxContentChars: 1000})
	result, err := system.ExecuteRound(context.Background(), []Query{{
		ID: "q-1", Text: "protocol official docs", Priority: 10, PreferredSource: []SourceKind{SourceOfficial},
	}}, nil, 1, 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 2 {
		t.Fatalf("documents=%d, want 2: %+v", len(result.Documents), result.Documents)
	}
	for _, document := range result.Documents {
		if document.Provider != "official" {
			t.Fatalf("unexpected provider routing: %+v", document)
		}
		if document.ContentHash == "" {
			t.Fatalf("content was not normalized: %+v", document)
		}
	}
}
