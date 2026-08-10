package decision

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/good-fish-man/agent-runtime/internal/research/evidence"
	"github.com/good-fish-man/agent-runtime/internal/research/searchsystem"
)

// Advisor adds model-assisted planning and verification to the deterministic
// research loop. Its output is always validated against runtime budgets and
// collected source IDs before it can affect execution.
type Advisor interface {
	RefinePlan(context.Context, PlanAdviceRequest) (PlanAdvice, error)
	VerifyClaims(context.Context, ClaimVerificationRequest) (ClaimVerification, error)
}

type AdvisorUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type PlanAdviceRequest struct {
	Task     Task
	Intent   Intent
	Baseline Plan
	Budget   Budget
}

type AdvisedQuery struct {
	Text     string `json:"text"`
	Purpose  string `json:"purpose"`
	Source   string `json:"source"`
	Priority int    `json:"priority"`
}

type PlanAdvice struct {
	Queries []AdvisedQuery `json:"queries"`
	Usage   AdvisorUsage   `json:"-"`
}

type ClaimVerificationRequest struct {
	Task      Task
	Report    evidence.Report
	MaxClaims int
}

type ClaimReview struct {
	ClaimID    string   `json:"claim_id"`
	Verdict    string   `json:"verdict"`
	SourceIDs  []string `json:"source_ids"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
}

type ClaimVerification struct {
	Reviews []ClaimReview `json:"reviews"`
	Usage   AdvisorUsage  `json:"-"`
}

type Progress struct {
	Stage         string
	Message       string
	Percent       int
	Round         int
	Queries       int
	QueryTexts    []string
	Sources       int
	ValuablePages []ValuablePage
	Confidence    float64
	Completed     bool
}

// ValuablePage is the bounded, UI-safe description of a page that contributed
// evidence. Raw page content never crosses the progress protocol boundary.
type ValuablePage struct {
	ID            string
	Rank          int
	Title         string
	URL           string
	Domain        string
	Provider      string
	Kind          string
	Snippet       string
	ValueSignals  []string
	Authority     float64
	Relevance     float64
	Freshness     float64
	EvidenceScore float64
	Fetched       bool
	PublishedAt   string
}

type RunOptions struct {
	Advisor              Advisor
	EnableModelPlanning  bool
	EnableSemanticVerify bool
	MaxAdvisorClaims     int
	OnProgress           func(Progress) error
}

func mergeAdvisedQueries(plan Plan, advice PlanAdvice, budget Budget) Plan {
	for _, candidate := range advice.Queries {
		if len(plan.Queries) >= budget.MaxQueries {
			break
		}
		text := strings.TrimSpace(candidate.Text)
		if text == "" || len([]rune(text)) > 240 || hasQueryText(plan.Queries, text) {
			continue
		}
		source, ok := validSourceKind(candidate.Source)
		if !ok {
			continue
		}
		priority := candidate.Priority
		if priority < 1 {
			priority = 50
		} else if priority > 100 {
			priority = 100
		}
		plan.Queries = append(plan.Queries, searchsystem.Query{
			ID:              fmt.Sprintf("advisor-%d", len(plan.Queries)+1),
			Text:            text,
			Purpose:         strings.TrimSpace(candidate.Purpose),
			Priority:        priority,
			PreferredSource: []searchsystem.SourceKind{source},
		})
	}
	return plan
}

func validSourceKind(raw string) (searchsystem.SourceKind, bool) {
	switch searchsystem.SourceKind(strings.ToLower(strings.TrimSpace(raw))) {
	case searchsystem.SourceGeneral:
		return searchsystem.SourceGeneral, true
	case searchsystem.SourceOfficial:
		return searchsystem.SourceOfficial, true
	case searchsystem.SourceGitHub:
		return searchsystem.SourceGitHub, true
	case searchsystem.SourceAcademic:
		return searchsystem.SourceAcademic, true
	case searchsystem.SourceNews:
		return searchsystem.SourceNews, true
	default:
		return "", false
	}
}

func applySemanticVerification(report evidence.Report, verification ClaimVerification, maxClaims int) evidence.Report {
	if maxClaims <= 0 {
		maxClaims = 8
	}
	validSources := make(map[string]bool, len(report.Items))
	for _, item := range report.Items {
		validSources[item.ID] = true
	}
	claimIndex := make(map[string]int, len(report.Claims))
	for i, claim := range report.Claims {
		claimIndex[claim.ID] = i
	}
	seen := make(map[string]bool)
	contradicted := 0
	for _, review := range verification.Reviews {
		index, ok := claimIndex[strings.TrimSpace(review.ClaimID)]
		if !ok || seen[review.ClaimID] || len(seen) >= maxClaims {
			continue
		}
		verdict := strings.ToLower(strings.TrimSpace(review.Verdict))
		if verdict != "supported" && verdict != "contradicted" && verdict != "insufficient" {
			continue
		}
		sourceIDs := make([]string, 0, len(review.SourceIDs))
		for _, sourceID := range review.SourceIDs {
			if validSources[sourceID] && !containsString(sourceIDs, sourceID) {
				sourceIDs = append(sourceIDs, sourceID)
			}
		}
		if len(sourceIDs) == 0 {
			continue
		}
		seen[review.ClaimID] = true
		claim := &report.Claims[index]
		claim.SourceIDs = sourceIDs
		claim.Verification = "semantic_" + verdict
		claim.Confidence = clampConfidence(review.Confidence, claim.Confidence)
		if verdict == "contradicted" {
			contradicted++
			report.Contradictions = append(report.Contradictions, evidence.Contradiction{
				ClaimIDs: []string{claim.ID}, Summary: strings.TrimSpace(review.Reason), Severity: "medium",
			})
		}
	}
	if contradicted > 0 {
		report.Confidence = clampConfidence(report.Confidence*0.85, report.Confidence)
	}
	return report
}

func clampConfidence(value, fallback float64) float64 {
	if value <= 0 {
		return fallback
	}
	if value > 0.99 {
		return 0.99
	}
	return value
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func valuablePages(report evidence.Report, limit int) []ValuablePage {
	if limit <= 0 {
		limit = 8
	}
	pages := make([]ValuablePage, 0, minInt(limit, len(report.Items)))
	seen := make(map[string]bool, len(report.Items))
	for _, item := range report.Items {
		if len(pages) >= limit {
			break
		}
		rawURL := strings.TrimSpace(item.CanonicalURL)
		if rawURL == "" {
			rawURL = strings.TrimSpace(item.URL)
		}
		parsed, err := url.Parse(rawURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || seen[rawURL] {
			continue
		}
		seen[rawURL] = true
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = parsed.Hostname()
		}
		page := ValuablePage{
			ID: item.ID, Rank: len(pages) + 1, Title: title, URL: rawURL,
			Domain: parsed.Hostname(), Provider: item.Provider, Kind: string(item.Kind),
			Snippet:   truncateProgressText(item.Snippet, 280),
			Authority: item.Score.Authority, Relevance: item.Score.Relevance,
			Freshness: item.Score.Freshness, EvidenceScore: item.Score.Overall,
			Fetched: !item.FetchedAt.IsZero() || strings.TrimSpace(item.Content) != "",
		}
		if !item.PublishedAt.IsZero() {
			page.PublishedAt = item.PublishedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		if page.Fetched {
			page.ValueSignals = append(page.ValueSignals, "opened")
		}
		if page.Authority >= 0.75 {
			page.ValueSignals = append(page.ValueSignals, "authoritative")
		}
		if page.Relevance >= 0.65 {
			page.ValueSignals = append(page.ValueSignals, "high_relevance")
		}
		if page.Freshness >= 0.70 {
			page.ValueSignals = append(page.ValueSignals, "recent")
		}
		if item.Score.Corroboration >= 0.45 {
			page.ValueSignals = append(page.ValueSignals, "corroborated")
		}
		pages = append(pages, page)
	}
	return pages
}

func progressQueryTexts(queries []searchsystem.Query) []string {
	values := make([]string, 0, len(queries))
	for _, query := range queries {
		text := strings.TrimSpace(query.Text)
		if text != "" && !containsString(values, text) {
			values = append(values, text)
		}
	}
	return values
}

func truncateProgressText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
