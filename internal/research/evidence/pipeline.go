// Package evidence turns retrieved documents into ranked, attributable facts.
// It is deliberately model-independent so coverage, conflicts, and stopping
// decisions remain observable and testable.
package evidence

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/good-fish-man/agent-runtime/internal/research/searchsystem"
)

type Request struct {
	Task       string
	Kind       string
	Date       string
	MinSources int
}

type Score struct {
	Authority     float64
	Relevance     float64
	Freshness     float64
	Corroboration float64
	Overall       float64
}

type Item struct {
	ID           string
	QueryIDs     []string
	Provider     string
	Kind         searchsystem.SourceKind
	Title        string
	URL          string
	CanonicalURL string
	Snippet      string
	Content      string
	ContentHash  string
	FetchedAt    time.Time
	PublishedAt  time.Time
	Score        Score
}

type Claim struct {
	ID           string
	Text         string
	SourceIDs    []string
	Verification string
	Confidence   float64
}

type Contradiction struct {
	ClaimIDs []string
	Summary  string
	Severity string
}

type Report struct {
	Items              []Item
	Claims             []Claim
	Contradictions     []Contradiction
	CoveredQueryIDs    []string
	DistinctHosts      int
	AuthoritativeCount int
	Coverage           float64
	Confidence         float64
}

type Aggregator interface {
	Aggregate(existing []Item, documents []searchsystem.Document) []Item
}

type Ranker interface {
	Rank(Request, []Item) []Item
}

type ClaimVerifier interface {
	Verify([]Item) []Claim
}

type ContradictionDetector interface {
	Detect([]Claim) []Contradiction
}

type Pipeline struct {
	aggregator Aggregator
	ranker     Ranker
	verifier   ClaimVerifier
	detector   ContradictionDetector
}

func NewPipeline() *Pipeline {
	return &Pipeline{
		aggregator: DefaultAggregator{},
		ranker:     DefaultRanker{},
		verifier:   DefaultClaimVerifier{MaxClaimsPerSource: 3},
		detector:   DefaultContradictionDetector{},
	}
}

func NewPipelineWith(aggregator Aggregator, ranker Ranker, verifier ClaimVerifier, detector ContradictionDetector) *Pipeline {
	return &Pipeline{aggregator: aggregator, ranker: ranker, verifier: verifier, detector: detector}
}

// Merge preserves evidence from earlier rounds and recalculates scores after
// every follow-up search.
func (p *Pipeline) Merge(request Request, current Report, round searchsystem.RoundResult, totalQueries int) Report {
	items := p.aggregator.Aggregate(current.Items, round.Documents)
	items = p.ranker.Rank(request, items)
	claims := p.verifier.Verify(items)
	contradictions := p.detector.Detect(claims)
	covered, hosts, authoritative := reportDimensions(items)
	coverage := 1.0
	if totalQueries > 0 {
		coverage = minFloat(1, float64(len(covered))/float64(totalQueries))
	}
	confidence := reportConfidence(items, coverage, len(contradictions))
	return Report{
		Items: items, Claims: claims, Contradictions: contradictions,
		CoveredQueryIDs: covered, DistinctHosts: hosts, AuthoritativeCount: authoritative,
		Coverage: coverage, Confidence: confidence,
	}
}

type DefaultAggregator struct{}

func (DefaultAggregator) Aggregate(existing []Item, documents []searchsystem.Document) []Item {
	byURL := make(map[string]Item, len(existing)+len(documents))
	for _, item := range existing {
		byURL[item.CanonicalURL] = item
	}
	for _, document := range documents {
		canonical := document.CanonicalURL
		if canonical == "" {
			canonical = document.URL
		}
		id := stableID("source", canonical)
		item, found := byURL[canonical]
		if !found {
			item = Item{
				ID: id, Provider: document.Provider, Kind: document.Kind,
				Title: document.Title, URL: document.URL, CanonicalURL: canonical,
				Snippet: document.Snippet, Content: document.Content,
				ContentHash: document.ContentHash, FetchedAt: document.FetchedAt,
				PublishedAt: document.PublishedAt,
			}
		}
		item.QueryIDs = appendUnique(item.QueryIDs, document.QueryID)
		if len(document.Content) > len(item.Content) {
			item.Content = document.Content
			item.ContentHash = document.ContentHash
			item.FetchedAt = document.FetchedAt
		}
		if item.PublishedAt.IsZero() && !document.PublishedAt.IsZero() {
			item.PublishedAt = document.PublishedAt
		}
		byURL[canonical] = item
	}
	result := make([]Item, 0, len(byURL))
	for _, item := range byURL {
		if item.CanonicalURL != "" {
			result = append(result, item)
		}
	}
	return result
}

type DefaultRanker struct{}

func (DefaultRanker) Rank(request Request, items []Item) []Item {
	for i := range items {
		items[i].Score.Authority = authorityScore(items[i])
		items[i].Score.Relevance = relevanceScore(request.Task, items[i])
		items[i].Score.Freshness = freshnessScore(request, items[i])
		items[i].Score.Corroboration = corroborationScore(items[i], items)
		items[i].Score.Overall = 0.38*items[i].Score.Authority + 0.38*items[i].Score.Relevance + 0.10*items[i].Score.Freshness + 0.14*items[i].Score.Corroboration
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Score.Overall > items[j].Score.Overall })
	return items
}

type DefaultClaimVerifier struct{ MaxClaimsPerSource int }

func (v DefaultClaimVerifier) Verify(items []Item) []Claim {
	maxClaims := v.MaxClaimsPerSource
	if maxClaims <= 0 {
		maxClaims = 3
	}
	claims := make([]Claim, 0, len(items)*2)
	for _, item := range items {
		statements := extractStatements(item, maxClaims)
		for _, statement := range statements {
			claims = append(claims, Claim{
				ID: stableID("claim", item.ID+statement), Text: statement,
				SourceIDs: []string{item.ID}, Verification: "single_source",
				Confidence: item.Score.Overall,
			})
		}
	}
	for i := range claims {
		for j := range claims {
			if i == j || claims[i].SourceIDs[0] == claims[j].SourceIDs[0] {
				continue
			}
			if negativePolarity(claims[i].Text) == negativePolarity(claims[j].Text) && tokenSimilarity(claims[i].Text, claims[j].Text) >= 0.42 {
				claims[i].SourceIDs = appendUnique(claims[i].SourceIDs, claims[j].SourceIDs[0])
			}
		}
		if len(claims[i].SourceIDs) > 1 {
			claims[i].Verification = "corroborated"
			claims[i].Confidence = minFloat(0.99, claims[i].Confidence+0.15)
		}
	}
	return deduplicateClaims(claims)
}

type DefaultContradictionDetector struct{}

func (DefaultContradictionDetector) Detect(claims []Claim) []Contradiction {
	result := make([]Contradiction, 0)
	for i := 0; i < len(claims); i++ {
		for j := i + 1; j < len(claims); j++ {
			if sharesSource(claims[i], claims[j]) || tokenSimilarity(claims[i].Text, claims[j].Text) < 0.55 {
				continue
			}
			if negativePolarity(claims[i].Text) != negativePolarity(claims[j].Text) {
				result = append(result, Contradiction{
					ClaimIDs: []string{claims[i].ID, claims[j].ID},
					Summary:  "Highly similar claims have opposite polarity and require a primary source.",
					Severity: "medium",
				})
			}
		}
	}
	return result
}

type cacheEntry struct {
	report    Report
	createdAt time.Time
}

// ResearchCache persists complete evidence reports for the process lifetime.
// The interface is intentionally small so a database-backed cache can replace
// it without touching the decision layer.
type ResearchCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
}

func NewResearchCache() *ResearchCache {
	return &ResearchCache{entries: make(map[string]cacheEntry)}
}

func (c *ResearchCache) Get(key string, ttl time.Duration) (Report, bool) {
	if c == nil || key == "" || ttl <= 0 {
		return Report{}, false
	}
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || time.Since(entry.createdAt) >= ttl {
		return Report{}, false
	}
	return entry.report, true
}

func (c *ResearchCache) Put(key string, report Report) {
	if c == nil || key == "" || len(report.Items) == 0 {
		return
	}
	c.mu.Lock()
	c.entries[key] = cacheEntry{report: report, createdAt: time.Now()}
	c.mu.Unlock()
}

func CacheKey(kind, task, date string) string {
	return stableID("research", strings.ToLower(strings.TrimSpace(kind+"|"+task+"|"+date)))
}

func authorityScore(item Item) float64 {
	parsed, err := url.Parse(item.CanonicalURL)
	if err != nil {
		return 0.2
	}
	host := strings.ToLower(parsed.Hostname())
	score := 0.4
	if parsed.Scheme == "https" {
		score += 0.1
	}
	if item.Kind == searchsystem.SourceOfficial {
		score += 0.2
	}
	switch {
	case strings.HasSuffix(host, ".gov") || strings.Contains(host, ".gov."):
		score += 0.35
	case strings.HasSuffix(host, ".edu") || strings.Contains(host, ".edu.") || strings.HasSuffix(host, ".ac.jp"):
		score += 0.25
	case reliableHost(host):
		score += 0.22
	case item.Kind == searchsystem.SourceGitHub && (host == "github.com" || strings.HasSuffix(host, ".github.com")):
		score += 0.18
	}
	return minFloat(0.99, score)
}

func reliableHost(host string) bool {
	for _, suffix := range []string{"reuters.com", "apnews.com", "bbc.com", "bbc.co.uk", "who.int", "un.org", "europa.eu", "nature.com", "science.org", "arxiv.org", "github.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func relevanceScore(task string, item Item) float64 {
	needle := tokenSet(task)
	if len(needle) == 0 {
		return 0.5
	}
	haystack := tokenSet(item.Title + " " + item.Snippet + " " + prefix(item.Content, 3000))
	matches := 0
	for token := range needle {
		if haystack[token] {
			matches++
		}
	}
	return minFloat(1, 0.2+0.8*float64(matches)/float64(len(needle)))
}

func freshnessScore(request Request, item Item) float64 {
	if !item.PublishedAt.IsZero() {
		if target, err := time.Parse("2006-01-02", request.Date); err == nil {
			days := item.PublishedAt.Sub(target).Hours() / 24
			if days < 0 {
				days = -days
			}
			switch {
			case days <= 1:
				return 0.98
			case days <= 3:
				return 0.82
			case days <= 7:
				return 0.65
			default:
				return 0.35
			}
		}
		return 0.75
	}
	if request.Date != "" && strings.Contains(item.Title+" "+item.Snippet+" "+prefix(item.Content, 1000), request.Date) {
		return 0.95
	}
	if request.Kind == "news" {
		return 0.45 // Publication time is unknown; do not confuse fetch time with freshness.
	}
	return 0.6
}

func corroborationScore(item Item, all []Item) float64 {
	base := item.Title + " " + item.Snippet
	corroborators := 0
	for _, candidate := range all {
		if candidate.ID != item.ID && tokenSimilarity(base, candidate.Title+" "+candidate.Snippet) >= 0.35 {
			corroborators++
		}
	}
	return minFloat(1, float64(corroborators)/2)
}

func extractStatements(item Item, limit int) []string {
	text := item.Content
	if text == "" {
		text = item.Snippet
	}
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == '。' || r == '！' || r == '？' || r == '\n'
	})
	result := make([]string, 0, limit)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len([]rune(part)) < 20 || len([]rune(part)) > 420 {
			continue
		}
		result = append(result, part)
		if len(result) >= limit {
			break
		}
	}
	if len(result) == 0 && strings.TrimSpace(item.Snippet) != "" {
		result = append(result, strings.TrimSpace(item.Snippet))
	}
	return result
}

func reportDimensions(items []Item) ([]string, int, int) {
	queries := make(map[string]bool)
	hosts := make(map[string]bool)
	authoritative := 0
	for _, item := range items {
		for _, id := range item.QueryIDs {
			if id != "" && id != "seed" {
				queries[id] = true
			}
		}
		if parsed, err := url.Parse(item.CanonicalURL); err == nil {
			hosts[strings.ToLower(parsed.Hostname())] = true
		}
		if item.Score.Authority >= 0.72 {
			authoritative++
		}
	}
	queryIDs := make([]string, 0, len(queries))
	for id := range queries {
		queryIDs = append(queryIDs, id)
	}
	sort.Strings(queryIDs)
	return queryIDs, len(hosts), authoritative
}

func reportConfidence(items []Item, coverage float64, contradictions int) float64 {
	if len(items) == 0 {
		return 0
	}
	limit := len(items)
	if limit > 5 {
		limit = 5
	}
	total := 0.0
	for i := 0; i < limit; i++ {
		total += items[i].Score.Overall
	}
	confidence := 0.7*(total/float64(limit)) + 0.3*coverage - float64(contradictions)*0.08
	return maxFloat(0, minFloat(0.99, confidence))
}

func deduplicateClaims(values []Claim) []Claim {
	result := make([]Claim, 0, len(values))
	for _, value := range values {
		duplicate := -1
		for i := range result {
			if tokenSimilarity(value.Text, result[i].Text) >= 0.82 {
				duplicate = i
				break
			}
		}
		if duplicate < 0 {
			result = append(result, value)
			continue
		}
		for _, sourceID := range value.SourceIDs {
			result[duplicate].SourceIDs = appendUnique(result[duplicate].SourceIDs, sourceID)
		}
		if len(result[duplicate].SourceIDs) > 1 {
			result[duplicate].Verification = "corroborated"
			result[duplicate].Confidence = minFloat(0.99, maxFloat(result[duplicate].Confidence, value.Confidence)+0.15)
		}
	}
	return result
}

func tokenSimilarity(a, b string) float64 {
	aTokens, bTokens := tokenSet(a), tokenSet(b)
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return 0
	}
	intersection := 0
	union := make(map[string]bool, len(aTokens)+len(bTokens))
	for token := range aTokens {
		union[token] = true
	}
	for token := range bTokens {
		if aTokens[token] {
			intersection++
		}
		union[token] = true
	}
	return float64(intersection) / float64(len(union))
}

func tokenSet(value string) map[string]bool {
	result := make(map[string]bool)
	var current strings.Builder
	flush := func() {
		if current.Len() >= 2 {
			result[current.String()] = true
		}
		current.Reset()
	}
	for _, r := range strings.ToLower(value) {
		if unicode.Is(unicode.Han, r) {
			flush()
			result[string(r)] = true
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return result
}

func negativePolarity(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{" not ", " no ", "never", "without", "didn't", "doesn't", "不是", "没有", "不会", "未", "否认"} {
		if strings.Contains(" "+lower+" ", marker) {
			return true
		}
	}
	return false
}

func sharesSource(a, b Claim) bool {
	for _, left := range a.SourceIDs {
		for _, right := range b.SourceIDs {
			if left == right {
				return true
			}
		}
	}
	return false
}

func stableID(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%s-%x", prefix, sum[:8])
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func prefix(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
