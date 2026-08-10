// Package searchsystem implements the capability layer of the research stack.
// It discovers public sources and retrieves bounded page content, but it does
// not decide whether evidence is sufficient or compose a user-facing answer.
package searchsystem

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/tools"
)

// SourceKind describes the type of source a query should prioritize.
type SourceKind string

const (
	SourceGeneral  SourceKind = "general"
	SourceOfficial SourceKind = "official"
	SourceGitHub   SourceKind = "github"
	SourceAcademic SourceKind = "academic"
	SourceNews     SourceKind = "news"
	SourceVideo    SourceKind = "video"
)

// Query is a provider-independent search request produced by the decision layer.
type Query struct {
	ID              string
	Text            string
	Purpose         string
	Priority        int
	PreferredSource []SourceKind
}

// Hit is a lightweight search result. Page content is fetched only after the
// complete result set has been deduplicated and prioritized.
type Hit struct {
	QueryID      string
	Provider     string
	Kind         SourceKind
	Title        string
	URL          string
	Snippet      string
	Priority     int
	Seed         bool
	SearchRank   int
	ProviderRank int
	PublishedAt  time.Time
}

// Document is normalized content returned by the fetch and extraction stages.
type Document struct {
	Hit
	CanonicalURL string
	Content      string
	ContentHash  string
	FetchedAt    time.Time
}

// Observation records one bounded capability operation without retaining raw
// provider payloads.
type Observation struct {
	Operation string
	Target    string
	Provider  string
	Status    string
	ErrorCode string
	Summary   string
	ElapsedMS int64
}

// RoundResult contains only capability-layer facts; evidence quality is added
// by the evidence layer.
type RoundResult struct {
	Hits         []Hit
	Documents    []Document
	Observations []Observation
	Failures     []string
}

// Provider is an independently replaceable search backend.
type Provider interface {
	Name() string
	Kind() SourceKind
	Search(context.Context, Query, int) ([]Hit, error)
}

// Fetcher retrieves one selected public page.
type Fetcher interface {
	Fetch(context.Context, Hit) (Document, error)
}

// Extractor bounds and normalizes fetched content before it leaves the
// capability layer.
type Extractor interface {
	Extract(Document) Document
}

// SourceRouter chooses providers from explicit source preferences.
type SourceRouter interface {
	Route(Query) []Provider
}

type Router struct {
	providers map[SourceKind][]Provider
}

func NewRouter(providers ...Provider) *Router {
	r := &Router{providers: make(map[SourceKind][]Provider)}
	for _, provider := range providers {
		if provider != nil {
			r.providers[provider.Kind()] = append(r.providers[provider.Kind()], provider)
		}
	}
	return r
}

func (r *Router) Route(query Query) []Provider {
	kinds := query.PreferredSource
	if len(kinds) == 0 {
		kinds = []SourceKind{SourceGeneral}
	}
	seen := make(map[string]bool)
	result := make([]Provider, 0, len(kinds))
	for _, kind := range kinds {
		for _, provider := range r.providers[kind] {
			if !seen[provider.Name()] {
				seen[provider.Name()] = true
				result = append(result, provider)
			}
		}
	}
	if len(result) == 0 {
		result = append(result, r.providers[SourceGeneral]...)
	}
	return result
}

type System struct {
	router    SourceRouter
	fetcher   Fetcher
	extractor Extractor
}

func New(router SourceRouter, fetcher Fetcher, extractor Extractor) *System {
	if extractor == nil {
		extractor = DefaultExtractor{MaxContentChars: 12_000}
	}
	return &System{router: router, fetcher: fetcher, extractor: extractor}
}

// ExecuteRound searches first, then fetches only the highest-value unique URLs.
func (s *System) ExecuteRound(ctx context.Context, queries []Query, seedURLs []string, maxSearches, resultsPerSearch, maxPages int) (RoundResult, error) {
	if maxSearches <= 0 || maxPages <= 0 || s == nil || s.router == nil || s.fetcher == nil {
		return RoundResult{}, nil
	}
	if resultsPerSearch <= 0 {
		resultsPerSearch = 5
	}
	type searchTask struct {
		query    Query
		provider Provider
		order    int
	}
	tasks := make([]searchTask, 0, maxSearches)
	sort.SliceStable(queries, func(i, j int) bool { return queries[i].Priority > queries[j].Priority })
	for _, query := range queries {
		for _, provider := range s.router.Route(query) {
			if len(tasks) >= maxSearches {
				break
			}
			tasks = append(tasks, searchTask{query: query, provider: provider, order: len(tasks)})
		}
		if len(tasks) >= maxSearches {
			break
		}
	}

	var result RoundResult
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, task := range tasks {
		task := task
		wg.Add(1)
		go func() {
			defer wg.Done()
			started := time.Now()
			hits, err := task.provider.Search(ctx, task.query, resultsPerSearch)
			for i := range hits {
				hits[i].ProviderRank = task.order
			}
			observation := Observation{Operation: "search", Target: task.query.Text, Provider: task.provider.Name(), ElapsedMS: time.Since(started).Milliseconds()}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				observation.Status, observation.ErrorCode, observation.Summary = "error", classifyError(err), "Search provider failed."
				result.Failures = append(result.Failures, fmt.Sprintf("search %q via %s: %v", task.query.Text, task.provider.Name(), err))
			} else {
				observation.Status = "success"
				observation.Summary = fmt.Sprintf("Search returned %d result(s).", len(hits))
				result.Hits = append(result.Hits, hits...)
			}
			result.Observations = append(result.Observations, observation)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return result, err
	}

	for i, rawURL := range seedURLs {
		if validPublicURL(rawURL) {
			result.Hits = append(result.Hits, Hit{QueryID: "seed", Provider: "user", Kind: SourceOfficial, Title: rawURL, URL: rawURL, Priority: 1000 - i, Seed: true})
		}
	}
	result.Hits = selectHits(result.Hits, maxPages)
	documents := make([]Document, len(result.Hits))
	fetchFailures := make([]string, len(result.Hits))
	fetchObservations := make([]Observation, len(result.Hits))
	for i, hit := range result.Hits {
		i, hit := i, hit
		wg.Add(1)
		go func() {
			defer wg.Done()
			started := time.Now()
			document, err := s.fetcher.Fetch(ctx, hit)
			observation := Observation{Operation: "fetch", Target: hit.URL, Provider: hit.Provider, ElapsedMS: time.Since(started).Milliseconds()}
			if err != nil {
				fetchFailures[i] = fmt.Sprintf("fetch %s: %v", hit.URL, err)
				observation.Status, observation.ErrorCode, observation.Summary = "error", classifyError(err), "Page fetch failed."
			} else {
				document = s.extractor.Extract(document)
				documents[i] = document
				observation.Status = "success"
				observation.Summary = fmt.Sprintf("Extracted %d character(s).", len(document.Content))
			}
			fetchObservations[i] = observation
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for i, document := range documents {
		if document.URL != "" && (document.Content != "" || document.Snippet != "") {
			result.Documents = append(result.Documents, document)
		}
		if fetchFailures[i] != "" {
			result.Failures = append(result.Failures, fetchFailures[i])
		}
		if fetchObservations[i].Operation != "" {
			result.Observations = append(result.Observations, fetchObservations[i])
		}
	}
	return result, nil
}

type toolProvider struct {
	name      string
	kind      SourceKind
	transform func(string) string
	tool      *tools.WebSearchTool
}

func (p *toolProvider) Name() string     { return p.name }
func (p *toolProvider) Kind() SourceKind { return p.kind }

func (p *toolProvider) Search(ctx context.Context, query Query, count int) ([]Hit, error) {
	text := query.Text
	if p.transform != nil {
		text = p.transform(text)
	}
	input, _ := json.Marshal(tools.WebSearchInput{Query: text, Count: count})
	raw, err := p.tool.InvokableRun(ctx, string(input))
	if err != nil {
		return nil, err
	}
	var output tools.WebSearchOutput
	if err = json.Unmarshal([]byte(raw), &output); err != nil {
		return nil, err
	}
	if output.Status == "no_results" {
		return nil, nil
	}
	if output.Status != "ok" {
		return nil, fmt.Errorf("%s: %s", output.Status, output.Message)
	}
	hits := make([]Hit, 0, len(output.Results))
	for i, item := range output.Results {
		hits = append(hits, Hit{QueryID: query.ID, Provider: p.name, Kind: p.kind, Title: item.Title, URL: item.URL, Snippet: item.Snippet, Priority: query.Priority, SearchRank: i + 1})
	}
	return hits, nil
}

type toolFetcher struct{ tool *tools.WebFetchTool }

func (f *toolFetcher) Fetch(ctx context.Context, hit Hit) (Document, error) {
	input, _ := json.Marshal(tools.WebFetchInput{URL: hit.URL, Prompt: "Extract factual content relevant to the research task", Cache: true})
	raw, err := f.tool.InvokableRun(ctx, string(input))
	if err != nil {
		return Document{}, err
	}
	var output tools.WebFetchOutput
	if err = json.Unmarshal([]byte(raw), &output); err != nil {
		return Document{}, err
	}
	if output.Status != "ok" {
		return Document{}, fmt.Errorf("%s: %s", output.Status, output.Message)
	}
	if output.Title != "" {
		hit.Title = output.Title
	}
	return Document{Hit: hit, CanonicalURL: canonicalURL(output.URL), Content: output.Content, FetchedAt: time.Now()}, nil
}

type ProviderConfig struct {
	Enabled     []string
	GitHubToken string
	Resilience  ResilienceConfig
}

func DefaultProviderConfig() ProviderConfig {
	return ProviderConfig{Enabled: []string{"web", "github", "wikipedia", "arxiv", "news"}, Resilience: DefaultResilienceConfig()}
}

func NewDefault() *System { return NewDefaultWithConfig(DefaultProviderConfig()) }

// NewDefaultWithConfig builds a registry of genuinely independent source
// providers plus scoped public-web fallbacks.
func NewDefaultWithConfig(config ProviderConfig) *System {
	if len(config.Enabled) == 0 {
		config.Enabled = DefaultProviderConfig().Enabled
	}
	if config.GitHubToken == "" {
		config.GitHubToken = os.Getenv(constant.EnvResearchGitHubToken)
		if config.GitHubToken == "" {
			config.GitHubToken = os.Getenv(constant.EnvGitHubToken)
		}
	}
	searchTool := tools.NewWebSearchTool()
	provider := func(name string, kind SourceKind, transform func(string) string) Provider {
		return WithResilience(&toolProvider{name: name, kind: kind, transform: transform, tool: searchTool}, config.Resilience)
	}
	providers := make([]Provider, 0, 10)
	if providerEnabled(config.Enabled, "github") {
		providers = append(providers, WithResilience(NewGitHubProvider(nil, "", config.GitHubToken), config.Resilience))
	}
	if providerEnabled(config.Enabled, "wikipedia") {
		providers = append(providers, WithResilience(NewWikipediaProvider(nil, ""), config.Resilience))
	}
	if providerEnabled(config.Enabled, "arxiv") {
		providers = append(providers, WithResilience(NewArxivProvider(nil, ""), config.Resilience))
	}
	if providerEnabled(config.Enabled, "news") {
		providers = append(providers, WithResilience(NewGDELTProvider(nil, ""), config.Resilience))
	}
	if providerEnabled(config.Enabled, "web") {
		providers = append(providers,
			provider("public-web", SourceGeneral, nil),
			provider("official-web", SourceOfficial, func(q string) string { return q + " official documentation" }),
			provider("github-web-fallback", SourceGitHub, func(q string) string { return q + " site:github.com" }),
			provider("academic-web-fallback", SourceAcademic, func(q string) string { return q + " research paper" }),
			provider("news-web-fallback", SourceNews, func(q string) string { return q + " latest news" }),
			provider("video-web", SourceVideo, func(q string) string { return q + " site:youtube.com" }),
		)
	}
	if len(providers) == 0 {
		providers = append(providers, provider("public-web", SourceGeneral, nil))
	}
	router := NewRouter(providers...)
	return New(router, &toolFetcher{tool: tools.NewWebFetchTool()}, DefaultExtractor{MaxContentChars: 12_000})
}

func providerEnabled(values []string, name string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), name) {
			return true
		}
	}
	return false
}

type DefaultExtractor struct{ MaxContentChars int }

func (e DefaultExtractor) Extract(document Document) Document {
	document.CanonicalURL = canonicalURL(document.CanonicalURL)
	if document.CanonicalURL == "" {
		document.CanonicalURL = canonicalURL(document.URL)
	}
	document.Content = strings.Join(strings.Fields(document.Content), " ")
	document.Content = truncate(document.Content, e.MaxContentChars)
	document.ContentHash = fmt.Sprintf("%x", sha256.Sum256([]byte(document.Content)))
	return document
}

func selectHits(values []Hit, limit int) []Hit {
	seenURL := make(map[string]bool)
	hostCount := make(map[string]int)
	result := make([]Hit, 0, limit)
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].Seed != values[j].Seed {
			return values[i].Seed
		}
		if values[i].Priority != values[j].Priority {
			return values[i].Priority > values[j].Priority
		}
		if values[i].ProviderRank != values[j].ProviderRank {
			return values[i].ProviderRank < values[j].ProviderRank
		}
		return values[i].SearchRank < values[j].SearchRank
	})
	for _, hit := range values {
		canonical := canonicalURL(hit.URL)
		if canonical == "" || seenURL[canonical] {
			continue
		}
		parsed, _ := url.Parse(canonical)
		host := strings.ToLower(parsed.Hostname())
		if hostCount[host] >= 2 {
			continue
		}
		seenURL[canonical] = true
		hostCount[host]++
		hit.URL = canonical
		result = append(result, hit)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func canonicalURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return ""
	}
	parsed.Fragment = ""
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "fbclid" || lower == "gclid" {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func validPublicURL(raw string) bool { return canonicalURL(raw) != "" }

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.ValidString(value[:limit]) {
		limit--
	}
	return value[:limit] + "..."
}

func classifyError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "TIMEOUT"
	}
	if errors.Is(err, ErrProviderCircuitOpen) {
		return "PROVIDER_CIRCUIT_OPEN"
	}
	var providerErr *ProviderError
	if errors.As(err, &providerErr) && providerErr.Code != "" {
		return strings.ToUpper(providerErr.Code)
	}
	return "PROVIDER_ERROR"
}
