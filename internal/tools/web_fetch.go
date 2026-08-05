package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/good-fish-man/agent-runtime/internal/constant"
	log "github.com/good-fish-man/logx"
)

// ========== WebFetchTool ==========

// WebFetchInput for web fetch tool
type WebFetchInput struct {
	URL    string `json:"url"`             // URL to fetch
	Prompt string `json:"prompt"`          // What to extract from the page
	Cache  bool   `json:"cache,omitempty"` // Use cached result (default true)
}

// WebFetchOutput for web fetch result
type WebFetchOutput struct {
	Content    string `json:"content"`         // Fetched content
	Title      string `json:"title,omitempty"` // Page title
	StatusCode int    `json:"status_code"`     // HTTP status code
	URL        string `json:"url"`             // Final URL (after redirects)
	Status     string `json:"status"`          // ok, http_error, or fetch_error
	Message    string `json:"message,omitempty"`
}

// WebFetchTool fetches and analyzes web content
type WebFetchTool struct {
	client   *http.Client
	cache    map[string]*cachedFetch
	cacheMu  sync.RWMutex
	cacheTTL time.Duration
}

type cachedFetch struct {
	content   string
	title     string
	fetchedAt time.Time
}

func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{
		client: &http.Client{
			Timeout: constant.DefaultWebRequestTimeoutSec * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				return nil
			},
		},
		cache:    make(map[string]*cachedFetch),
		cacheTTL: constant.DefaultWebFetchCacheTTLMin * time.Minute,
	}
}

func init() {
	GlobalRegistry.Register(ToolMeta{
		Name:           "WebFetch",
		Desc:           "Fetch an exact public page URL supplied by the user or returned by WebSearch. Never invent a hostname or construct a URL from a topic. Returns a recoverable status when a page is unavailable.",
		IsReadOnly:     true,
		MaxResultChars: 500000,
		DefaultRisk:    "low",
		Creator: func(basePath string) interface{} {
			return NewWebFetchTool()
		},
	})
}

func (t *WebFetchTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "WebFetch",
		Desc: "Fetch an exact public page URL supplied by the user or returned by WebSearch. Never invent a hostname or construct a URL from a topic. Returns a recoverable status when a page is unavailable.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"url": {
				Type:     schema.String,
				Desc:     "URL to fetch content from",
				Required: true,
			},
			"prompt": {
				Type:     schema.String,
				Desc:     "Description of what to extract from the page",
				Required: false,
			},
			"cache": {
				Type:     schema.Boolean,
				Desc:     "Use cached result if available (default true)",
				Required: false,
			},
		}),
	}, nil
}

func (t *WebFetchTool) ValidateInput(ctx context.Context, input string) *ValidationResult {
	var fetchInput WebFetchInput
	if err := json.Unmarshal([]byte(input), &fetchInput); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid JSON: %v", err), ErrorCode: 1}
	}
	if fetchInput.URL == "" {
		return &ValidationResult{Valid: false, Message: "url is required", ErrorCode: 2}
	}
	// Validate URL format
	if _, err := url.Parse(fetchInput.URL); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid URL: %v", err), ErrorCode: 3}
	}
	return &ValidationResult{Valid: true}
}

func (t *WebFetchTool) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	var fetchInput WebFetchInput
	if err := json.Unmarshal([]byte(input), &fetchInput); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Check cache first
	useCache := fetchInput.Cache != false // default true
	if useCache {
		t.cacheMu.RLock()
		cached, ok := t.cache[fetchInput.URL]
		t.cacheMu.RUnlock()
		if ok {
			if time.Since(cached.fetchedAt) < t.cacheTTL {
				output := WebFetchOutput{
					Content:    cached.content,
					Title:      cached.title,
					URL:        fetchInput.URL,
					StatusCode: 200,
					Status:     "ok",
				}
				return marshalWebFetchOutput(output)
			}
		}
	}

	// Fetch URL
	req, err := http.NewRequestWithContext(ctx, "GET", fetchInput.URL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; RunnerBot/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := t.client.Do(req)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return "", fmt.Errorf("fetch canceled: %w", err)
		}
		log.WarnwCtx(ctx, "WebFetch could not reach page", "url", fetchInput.URL, "error", err)
		return marshalWebFetchOutput(WebFetchOutput{
			Content: "",
			URL:     fetchInput.URL,
			Status:  "fetch_error",
			Message: "The page could not be reached. Do not retry the same URL and do not guess a replacement URL. Use WebSearch to find a different real, authoritative source.",
		})
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL.String()

	// Read body with size limit (1MB)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return "", fmt.Errorf("read body canceled: %w", err)
		}
		log.WarnwCtx(ctx, "WebFetch could not read page", "url", finalURL, "error", err)
		return marshalWebFetchOutput(WebFetchOutput{
			Content:    "",
			URL:        finalURL,
			StatusCode: resp.StatusCode,
			Status:     "fetch_error",
			Message:    "The page response could not be read. Do not retry the same URL; use WebSearch to find another authoritative source.",
		})
	}

	content := string(body)

	// Extract title from HTML if present
	title := extractHTMLTitle(content)

	// Simple content extraction (remove HTML tags for plain text)
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		content = stripHTML(content)
	}

	status := "ok"
	message := ""
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		status = "http_error"
		message = fmt.Sprintf("The page returned HTTP %d. Do not retry the same URL; use WebSearch to find another authoritative source.", resp.StatusCode)
	}

	// Cache only successful responses.
	if useCache && status == "ok" {
		t.cacheMu.Lock()
		t.cache[fetchInput.URL] = &cachedFetch{
			content:   content,
			title:     title,
			fetchedAt: time.Now(),
		}
		t.cacheMu.Unlock()
	}

	output := WebFetchOutput{
		Content:    content,
		Title:      title,
		StatusCode: resp.StatusCode,
		URL:        finalURL,
		Status:     status,
		Message:    message,
	}
	return marshalWebFetchOutput(output)
}

func marshalWebFetchOutput(output WebFetchOutput) (string, error) {
	result, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("encode fetch result: %w", err)
	}
	return string(result), nil
}

func extractHTMLTitle(html string) string {
	const start = "<title>"
	const end = "</title>"
	if i := strings.Index(strings.ToLower(html), start); i >= 0 {
		startIdx := i + len(start)
		if j := strings.Index(strings.ToLower(html[startIdx:]), end); j >= 0 {
			return strings.TrimSpace(html[startIdx : startIdx+j])
		}
	}
	return ""
}

func stripHTML(html string) string {
	// Simple HTML tag stripper
	var result strings.Builder
	inTag := false
	for _, r := range html {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return strings.TrimSpace(result.String())
}
