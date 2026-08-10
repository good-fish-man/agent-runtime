package searchsystem

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIndependentProvidersNormalizeResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/repositories":
			_, _ = w.Write([]byte(`{"items":[{"full_name":"athena/runtime","html_url":"https://github.com/athena/runtime","description":"Agent runtime","language":"Go","stargazers_count":42}]}`))
		case "/w/api.php":
			_, _ = w.Write([]byte(`{"query":{"search":[{"title":"Model Context Protocol","snippet":"An <span>open</span> protocol"}]}}`))
		case "/arxiv":
			w.Header().Set("Content-Type", "application/atom+xml")
			_, _ = w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"><entry><id>https://arxiv.org/abs/1234.5678</id><title>Agent Research</title><summary>Evidence-aware agents.</summary></entry></feed>`))
		case "/gdelt":
			_, _ = w.Write([]byte(`{"articles":[{"url":"https://news.example/story","title":"Protocol released","seendate":"20260807T120000Z","domain":"news.example","sourcecountry":"US","language":"English"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tests := []struct {
		name     string
		provider Provider
		wantURL  string
	}{
		{"github", NewGitHubProvider(server.Client(), server.URL, "token"), "https://github.com/athena/runtime"},
		{"wikipedia", NewWikipediaProvider(server.Client(), server.URL), server.URL + "/wiki/Model_Context_Protocol"},
		{"arxiv", NewArxivProvider(server.Client(), server.URL+"/arxiv"), "https://arxiv.org/abs/1234.5678"},
		{"news", NewGDELTProvider(server.Client(), server.URL+"/gdelt"), "https://news.example/story"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits, err := tt.provider.Search(context.Background(), Query{ID: "q-1", Text: "agent protocol", Priority: 10}, 3)
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) != 1 || hits[0].URL != tt.wantURL || hits[0].Provider != tt.provider.Name() {
				t.Fatalf("unexpected normalized hits: %+v", hits)
			}
		})
	}
}

type alwaysFailProvider struct{ calls atomic.Int32 }

func (*alwaysFailProvider) Name() string     { return "failing" }
func (*alwaysFailProvider) Kind() SourceKind { return SourceGeneral }
func (p *alwaysFailProvider) Search(context.Context, Query, int) ([]Hit, error) {
	p.calls.Add(1)
	return nil, errors.New("provider unavailable")
}

func TestResilientProviderOpensCircuit(t *testing.T) {
	base := &alwaysFailProvider{}
	provider := WithResilience(base, ResilienceConfig{Timeout: time.Second, FailureThreshold: 2, OpenDuration: time.Minute})
	for i := 0; i < 2; i++ {
		if _, err := provider.Search(context.Background(), Query{Text: "test"}, 1); err == nil {
			t.Fatal("expected provider failure")
		}
	}
	_, err := provider.Search(context.Background(), Query{Text: "test"}, 1)
	if !errors.Is(err, ErrProviderCircuitOpen) || !strings.Contains(err.Error(), "failing") {
		t.Fatalf("circuit did not open: %v", err)
	}
	if base.calls.Load() != 2 {
		t.Fatalf("open circuit still called provider: %d", base.calls.Load())
	}
}
