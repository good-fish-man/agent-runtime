package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/good-fish-man/agent-runtime/internal/constant"
	xhtml "golang.org/x/net/html"
)

// ========== WebSearchTool ==========

// WebSearchInput for web search tool
type WebSearchInput struct {
	Query string `json:"query"`           // Search query
	Count int    `json:"count,omitempty"` // Number of results (default 10)
}

// WebSearchOutput for web search result
type WebSearchOutput struct {
	Results []SearchResult `json:"results"`
	Query   string         `json:"query"`
	Status  string         `json:"status"`
	Message string         `json:"message,omitempty"`
}

// SearchResult represents a single search result
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// WebSearchTool searches the web
type WebSearchTool struct {
	client *http.Client
}

func NewWebSearchTool() *WebSearchTool {
	return &WebSearchTool{
		client: &http.Client{
			Timeout: constant.DefaultWebRequestTimeoutSec * time.Second,
		},
	}
}

func init() {
	GlobalRegistry.Register(ToolMeta{
		Name:           "WebSearch",
		Desc:           "Search the public web. Use before answering current, unstable, recent, recommended, explicitly verified, or source-dependent questions. Returns titles, direct URLs, and snippets; fetch important sources with WebFetch.",
		IsReadOnly:     true,
		MaxResultChars: 50000,
		DefaultRisk:    "low",
		Creator: func(basePath string) interface{} {
			return NewWebSearchTool()
		},
	})
}

func (t *WebSearchTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "WebSearch",
		Desc: "Search the public web. Use before answering current, unstable, recent, recommended, explicitly verified, or source-dependent questions. Returns titles, direct URLs, and snippets; fetch important sources with WebFetch.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The search query",
				Required: true,
			},
			"count": {
				Type:     schema.Integer,
				Desc:     "Number of results to return (default 10)",
				Required: false,
			},
		}),
	}, nil
}

func (t *WebSearchTool) ValidateInput(ctx context.Context, input string) *ValidationResult {
	var searchInput WebSearchInput
	if err := json.Unmarshal([]byte(input), &searchInput); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid JSON: %v", err), ErrorCode: 1}
	}
	if strings.TrimSpace(searchInput.Query) == "" {
		return &ValidationResult{Valid: false, Message: "query is required", ErrorCode: 2}
	}
	return &ValidationResult{Valid: true}
}

func (t *WebSearchTool) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	var searchInput WebSearchInput
	if err := json.Unmarshal([]byte(input), &searchInput); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	searchInput.Query = strings.TrimSpace(searchInput.Query)
	if searchInput.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	count := searchInput.Count
	if count <= 0 || count > 20 {
		count = 10
	}

	// Try both public DuckDuckGo frontends. Either endpoint may independently
	// reject automated traffic, so provider failures are recoverable results.
	query := url.QueryEscape(searchInput.Query)
	searchURLs := []string{
		fmt.Sprintf(constant.DuckDuckGoHTMLSearchURL, query),
		fmt.Sprintf("https://lite.duckduckgo.com/lite/?q=%s", query),
	}
	var results []SearchResult
	failures := make([]string, 0, len(searchURLs))
	for _, searchURL := range searchURLs {
		body, status, err := t.fetchSearchPage(ctx, searchURL)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			failures = append(failures, err.Error())
			continue
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			failures = append(failures, fmt.Sprintf("HTTP %d", status))
			continue
		}
		results = parseDuckDuckGoResults(string(body), count)
		if len(results) > 0 {
			break
		}
	}
	if len(results) == 0 && len(failures) == len(searchURLs) {
		return marshalWebSearchOutput(WebSearchOutput{
			Results: []SearchResult{}, Query: searchInput.Query, Status: "search_unavailable",
			Message: "The public search provider is temporarily unavailable or rejected the request (" + strings.Join(failures, "; ") + "). Do not repeat the same query immediately. Continue with other available browsing tools or explain the temporary search limitation.",
		})
	}
	if len(results) == 0 {
		return marshalWebSearchOutput(WebSearchOutput{
			Results: []SearchResult{},
			Query:   searchInput.Query,
			Status:  "no_results",
			Message: "No results were found. Do not repeat the same query. Retry once with a shorter or more specific query; if a required detail such as location is missing, ask the user for it.",
		})
	}

	output := WebSearchOutput{
		Results: results,
		Query:   searchInput.Query,
		Status:  "ok",
	}
	return marshalWebSearchOutput(output)
}

func (t *WebSearchTool) fetchSearchPage(ctx context.Context, searchURL string) ([]byte, int, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid search URL: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return body, resp.StatusCode, nil
}

func marshalWebSearchOutput(output WebSearchOutput) (string, error) {
	result, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("encode search results: %w", err)
	}
	return string(result), nil
}

func parseDuckDuckGoResults(html string, count int) []SearchResult {
	if results := parseDuckDuckGoDocument(html, count); len(results) > 0 {
		return results
	}
	var results []SearchResult

	// Simple HTML parsing for DuckDuckGo results
	// Each result is in a div with class "result"
	lines := strings.Split(html, "\n")
	var currentResult *SearchResult

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Look for result headers
		if strings.Contains(line, "<a class=\"result__a\" href=\"") {
			// Extract URL
			hrefStart := strings.Index(line, "href=\"") + 6
			hrefEnd := strings.Index(line[hrefStart:], "\"")
			if hrefEnd > 0 {
				resultURL := normalizeSearchResultURL(line[hrefStart : hrefStart+hrefEnd])
				currentResult = &SearchResult{URL: resultURL}
			}

			// Extract title (between > and </a>)
			titleStart := strings.Index(line, "\">") + 2
			titleEnd := strings.Index(line, "</a>")
			if titleEnd > titleStart && currentResult != nil {
				currentResult.Title = stripHTML(line[titleStart:titleEnd])
			}
		}

		// Look for snippet
		if strings.Contains(line, "<a class=\"result__snippet\"") && currentResult != nil {
			// Extract snippet
			snippetStart := strings.Index(line, "\">") + 2
			snippetEnd := strings.Index(line, "</a>")
			if snippetEnd > snippetStart {
				currentResult.Snippet = stripHTML(line[snippetStart:snippetEnd])
			}

			results = append(results, *currentResult)
			currentResult = nil

			if len(results) >= count {
				break
			}
		}
	}

	return results
}

func parseDuckDuckGoDocument(document string, count int) []SearchResult {
	root, err := xhtml.Parse(strings.NewReader(document))
	if err != nil {
		return nil
	}
	results := make([]SearchResult, 0, count)
	var current *SearchResult
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if len(results) >= count {
			return
		}
		if node.Type == xhtml.ElementNode {
			className := htmlAttribute(node, "class")
			if node.Data == "a" && (hasHTMLClass(className, "result__a") || hasHTMLClass(className, "result-link")) {
				if current != nil && current.URL != "" {
					results = append(results, *current)
				}
				current = &SearchResult{Title: strings.TrimSpace(htmlNodeText(node)), URL: normalizeSearchResultURL(htmlAttribute(node, "href"))}
			} else if current != nil && (hasHTMLClass(className, "result__snippet") || hasHTMLClass(className, "result-snippet")) {
				current.Snippet = strings.TrimSpace(htmlNodeText(node))
				results = append(results, *current)
				current = nil
			}
		}
		for child := node.FirstChild; child != nil && len(results) < count; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if current != nil && current.URL != "" && len(results) < count {
		results = append(results, *current)
	}
	return results
}

func htmlAttribute(node *xhtml.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

func hasHTMLClass(value, wanted string) bool {
	for _, className := range strings.Fields(value) {
		if className == wanted {
			return true
		}
	}
	return false
}

func htmlNodeText(node *xhtml.Node) string {
	if node.Type == xhtml.TextNode {
		return node.Data
	}
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		builder.WriteString(htmlNodeText(child))
	}
	return builder.String()
}

func normalizeSearchResultURL(value string) string {
	value = html.UnescapeString(strings.TrimSpace(value))
	if strings.HasPrefix(value, "//") {
		value = "https:" + value
	} else if strings.HasPrefix(value, "/") {
		value = "https://duckduckgo.com" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	if strings.HasSuffix(strings.ToLower(parsed.Hostname()), "duckduckgo.com") {
		if target := parsed.Query().Get("uddg"); target != "" {
			return target
		}
	}
	return value
}
