package research

import (
	"net/url"
	"strings"
	"time"
)

// Protocol defines the deterministic limits around the model-independent
// research phase. The zero value is normalized to DefaultProtocol.
type Protocol struct {
	MaxSearches          int
	MaxFetches           int
	MaxResearchRounds    int
	ResultsPerSearch     int
	MaxPlannerIterations int
	MaxExecutionTime     time.Duration
	SearchCacheTTL       time.Duration
	FetchCacheTTL        time.Duration
	ResearchCacheTTL     time.Duration
	NewsCacheTTL         time.Duration
	MaxContextChars      int
	SearchInterval       time.Duration
	FetchInterval        time.Duration
	PerDomainInterval    time.Duration
	MaxFetchRetries      int
	RetryBackoff         time.Duration
}

// DefaultProtocol implements Agent Protocol v1.0 limits.
func DefaultProtocol() Protocol {
	return Protocol{
		MaxSearches:          6,
		MaxFetches:           8,
		MaxResearchRounds:    3,
		ResultsPerSearch:     5,
		MaxPlannerIterations: 6,
		MaxExecutionTime:     30 * time.Second,
		SearchCacheTTL:       5 * time.Minute,
		FetchCacheTTL:        time.Hour,
		ResearchCacheTTL:     time.Hour,
		NewsCacheTTL:         5 * time.Minute,
		MaxContextChars:      80_000, // Approximately 20k tokens.
		SearchInterval:       200 * time.Millisecond,
		FetchInterval:        500 * time.Millisecond,
		PerDomainInterval:    time.Second,
		MaxFetchRetries:      2,
		RetryBackoff:         500 * time.Millisecond,
	}
}

func (p Protocol) normalized() Protocol {
	defaults := DefaultProtocol()
	if p.MaxSearches <= 0 {
		p.MaxSearches = defaults.MaxSearches
	}
	if p.MaxFetches <= 0 {
		p.MaxFetches = defaults.MaxFetches
	}
	if p.MaxResearchRounds <= 0 {
		p.MaxResearchRounds = defaults.MaxResearchRounds
	}
	if p.ResultsPerSearch <= 0 {
		p.ResultsPerSearch = defaults.ResultsPerSearch
	}
	if p.MaxPlannerIterations <= 0 {
		p.MaxPlannerIterations = defaults.MaxPlannerIterations
	}
	if p.MaxExecutionTime <= 0 {
		p.MaxExecutionTime = defaults.MaxExecutionTime
	}
	if p.SearchCacheTTL <= 0 {
		p.SearchCacheTTL = defaults.SearchCacheTTL
	}
	if p.FetchCacheTTL <= 0 {
		p.FetchCacheTTL = defaults.FetchCacheTTL
	}
	if p.ResearchCacheTTL <= 0 {
		p.ResearchCacheTTL = defaults.ResearchCacheTTL
	}
	if p.NewsCacheTTL <= 0 {
		p.NewsCacheTTL = defaults.NewsCacheTTL
	}
	if p.MaxContextChars <= 0 {
		p.MaxContextChars = defaults.MaxContextChars
	}
	if p.SearchInterval <= 0 {
		p.SearchInterval = defaults.SearchInterval
	}
	if p.FetchInterval <= 0 {
		p.FetchInterval = defaults.FetchInterval
	}
	if p.PerDomainInterval <= 0 {
		p.PerDomainInterval = defaults.PerDomainInterval
	}
	if p.MaxFetchRetries == 0 {
		p.MaxFetchRetries = defaults.MaxFetchRetries
	} else if p.MaxFetchRetries < 0 {
		p.MaxFetchRetries = 0
	}
	if p.RetryBackoff <= 0 {
		p.RetryBackoff = defaults.RetryBackoff
	}
	return p
}

// Observation is the compact, provider-independent result of one tool call.
// Raw HTML and large provider payloads are deliberately excluded.
type Observation struct {
	Tool       string
	Status     string
	Summary    string
	ErrorCode  string
	ElapsedMS  int64
	Confidence float64
	CacheHit   bool
	Target     string
}

// Metrics summarizes a complete deterministic research pass.
type Metrics struct {
	PlannerIterations int
	ToolCalls         int
	SearchCalls       int
	FetchCalls        int
	CacheHits         int
	AdvisorCalls      int
	PromptTokens      int
	CompletionTokens  int
	TotalTokens       int
	ElapsedMS         int64
}

func sourceTrust(rawURL, title string) (float64, string) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return 0, "low"
	}
	host := strings.ToLower(parsed.Hostname())
	score := 0.35
	if parsed.Scheme == "https" {
		score += 0.15
	}
	if strings.HasSuffix(host, ".gov") || strings.Contains(host, ".gov.") {
		score += 0.35
	} else if strings.HasSuffix(host, ".edu") || strings.Contains(host, ".edu.") || strings.HasSuffix(host, ".ac.jp") {
		score += 0.25
	} else if knownReliableHost(host) {
		score += 0.2
	}
	lowerTitle := strings.ToLower(title)
	if strings.Contains(lowerTitle, "official") || strings.Contains(lowerTitle, "官方") {
		score += 0.1
	}
	if score > 0.99 {
		score = 0.99
	}
	level := "low"
	if score >= 0.8 {
		level = "high"
	} else if score >= 0.55 {
		level = "medium"
	}
	return score, level
}

func knownReliableHost(host string) bool {
	for _, suffix := range []string{
		"reuters.com", "apnews.com", "bbc.com", "bbc.co.uk", "who.int",
		"un.org", "europa.eu", "nature.com", "science.org",
	} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
