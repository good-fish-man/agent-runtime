package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type webSearchRoundTripFunc func(*http.Request) (*http.Response, error)

func (f webSearchRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNormalizeSearchResultURL(t *testing.T) {
	tests := map[string]string{
		"https://example.com/docs": "https://example.com/docs",
		"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Frelease%3Fa%3D1": "https://example.com/release?a=1",
		"/l/?uddg=https%3A%2F%2Fexample.org%2Fnews&amp;rut=ignored":            "https://example.org/news",
	}
	for input, want := range tests {
		if got := normalizeSearchResultURL(input); got != want {
			t.Errorf("normalizeSearchResultURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseDuckDuckGoResultsReturnsDirectURL(t *testing.T) {
	html := `<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fdocs">Example Docs</a>
<a class="result__snippet" href="//duckduckgo.com/l/">Official documentation</a>`
	results := parseDuckDuckGoResults(html, 1)
	if len(results) != 1 {
		t.Fatalf("parseDuckDuckGoResults() returned %d results", len(results))
	}
	if results[0].URL != "https://example.com/docs" || results[0].Title != "Example Docs" {
		t.Fatalf("unexpected result: %+v", results[0])
	}
}

func TestWebSearchEmptyResultsAreRecoverable(t *testing.T) {
	searchTool := NewWebSearchTool()
	searchTool.client.Transport = webSearchRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("<html><body>No results.</body></html>")),
			Header:     make(http.Header),
		}, nil
	})

	result, err := searchTool.InvokableRun(context.Background(), `{"query":"today weather"}`)
	if err != nil {
		t.Fatalf("empty search result must not fail the tool node: %v", err)
	}
	var output WebSearchOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if output.Status != "no_results" || len(output.Results) != 0 {
		t.Fatalf("unexpected empty result output: %+v", output)
	}
	if !strings.Contains(output.Message, "Do not repeat") {
		t.Fatalf("empty result does not guide model recovery: %+v", output)
	}
}

func TestWebSearchHTTPFailuresAreRecoverable(t *testing.T) {
	searchTool := NewWebSearchTool()
	searchTool.client.Transport = webSearchRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("forbidden")), Header: make(http.Header)}, nil
	})
	result, err := searchTool.InvokableRun(context.Background(), `{"query":"today news"}`)
	if err != nil {
		t.Fatalf("provider HTTP failures must not fail the tool node: %v", err)
	}
	var output WebSearchOutput
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != "search_unavailable" || !strings.Contains(output.Message, "HTTP 403") {
		t.Fatalf("unexpected provider failure output: %+v", output)
	}
}

func TestParseDuckDuckGoLiteResults(t *testing.T) {
	document := `<table><tr><td><a rel="nofollow" href="https://example.com/news" class="result-link">Example News</a></td></tr><tr><td class="result-snippet">Current details</td></tr></table>`
	results := parseDuckDuckGoResults(document, 1)
	if len(results) != 1 || results[0].URL != "https://example.com/news" || results[0].Snippet != "Current details" {
		t.Fatalf("unexpected lite results: %+v", results)
	}
}
