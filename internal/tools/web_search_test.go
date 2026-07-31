package tools

import "testing"

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
