package research

import (
	"context"

	"github.com/good-fish-man/agent-runtime/internal/research/decision"
	researchevidence "github.com/good-fish-man/agent-runtime/internal/research/evidence"
	"github.com/good-fish-man/agent-runtime/internal/research/searchsystem"
)

// Runner is the stable dispatcher-facing research contract.
type Runner interface {
	Execute(context.Context, Plan) (Evidence, error)
}

// AdvancedRunner is the optional V3 contract. Dispatcher uses it when model
// advice or live research progress is enabled, while older Runner
// implementations continue to work unchanged.
type AdvancedRunner interface {
	Runner
	ExecuteWithOptions(context.Context, Plan, ExecuteOptions) (Evidence, error)
}

type Advisor = decision.Advisor
type Progress = decision.Progress
type ValuablePage = decision.ValuablePage

type ExecuteOptions struct {
	Advisor              Advisor
	EnableModelPlanning  bool
	EnableSemanticVerify bool
	MaxAdvisorClaims     int
	OnProgress           func(Progress) error
}

// AgentExecutor adapts the layered Research Agent back to the existing
// dispatcher contract.
type AgentExecutor struct {
	agent    *decision.Agent
	protocol Protocol
}

var sharedResearchCache = researchevidence.NewResearchCache()

func NewResearchAgent() *AgentExecutor {
	return NewResearchAgentWithConfig(AgentConfig{})
}

type AgentConfig struct {
	Protocol    Protocol
	CacheDir    string
	Providers   []string
	GitHubToken string
	Resilience  searchsystem.ResilienceConfig
}

func NewResearchAgentWithConfig(config AgentConfig) *AgentExecutor {
	providerConfig := searchsystem.DefaultProviderConfig()
	if len(config.Providers) > 0 {
		providerConfig.Enabled = append([]string(nil), config.Providers...)
	}
	providerConfig.GitHubToken = config.GitHubToken
	providerConfig.Resilience = config.Resilience
	search := searchsystem.NewDefaultWithConfig(providerConfig)
	pipeline := researchevidence.NewPipeline()
	var cache decision.ResearchCache = sharedResearchCache
	if config.CacheDir != "" {
		cache = researchevidence.NewLayeredResearchCache(config.CacheDir)
	}
	protocol := config.Protocol
	protocol = protocol.normalized()
	return &AgentExecutor{
		agent:    decision.NewAgent(search, pipeline, cache),
		protocol: protocol,
	}
}

func (e *AgentExecutor) Execute(ctx context.Context, plan Plan) (Evidence, error) {
	return e.ExecuteWithOptions(ctx, plan, ExecuteOptions{})
}

func (e *AgentExecutor) ExecuteWithOptions(ctx context.Context, plan Plan, options ExecuteOptions) (Evidence, error) {
	if plan.Kind == KindNone {
		return Evidence{}, nil
	}
	protocol := e.protocol.normalized()
	outcome, err := e.agent.RunWithOptions(ctx, decision.Task{
		Kind:           string(plan.Kind),
		Prompt:         plan.ResolvedRequest,
		InitialQueries: append([]string(nil), plan.Queries...),
		SeedURLs:       append([]string(nil), plan.SeedURLs...),
		MinSources:     plan.MinSources,
		MaxSources:     plan.MaxSources,
		Date:           plan.Date,
		Language:       plan.ResponseLanguage,
	}, decision.Budget{
		MaxQueries:      protocol.MaxSearches,
		MaxPages:        protocol.MaxFetches,
		MaxRounds:       protocol.MaxResearchRounds,
		ResultsPerQuery: protocol.ResultsPerSearch,
		MaxContextChars: protocol.MaxContextChars,
		MaxDuration:     protocol.MaxExecutionTime,
		CacheTTL:        protocol.ResearchCacheTTL,
		NewsCacheTTL:    protocol.NewsCacheTTL,
	}, decision.RunOptions{
		Advisor: options.Advisor, EnableModelPlanning: options.EnableModelPlanning,
		EnableSemanticVerify: options.EnableSemanticVerify, MaxAdvisorClaims: options.MaxAdvisorClaims,
		OnProgress: options.OnProgress,
	})
	if err != nil {
		return Evidence{}, err
	}

	result := Evidence{
		Plan: plan, Failures: append([]string(nil), outcome.Failures...),
		ContextLimit: protocol.MaxContextChars, LimitReached: outcome.Result.Usage.LimitReached,
		StopReason: outcome.Result.StopReason, Confidence: outcome.Result.Confidence,
		Metrics: Metrics{
			PlannerIterations: outcome.Result.Usage.Rounds,
			SearchCalls:       outcome.Result.Usage.Searches,
			FetchCalls:        outcome.Result.Usage.Pages,
			CacheHits:         outcome.Result.Usage.CacheHits,
			AdvisorCalls:      outcome.Result.Usage.AdvisorCalls,
			PromptTokens:      outcome.Result.Usage.PromptTokens,
			CompletionTokens:  outcome.Result.Usage.CompletionTokens,
			TotalTokens:       outcome.Result.Usage.TotalTokens,
			ElapsedMS:         outcome.Result.Usage.ElapsedMS,
		},
	}
	result.Metrics.ToolCalls = result.Metrics.SearchCalls + result.Metrics.FetchCalls
	for _, query := range outcome.Attempted {
		result.AttemptedQuery = append(result.AttemptedQuery, query.Text)
	}
	for _, observation := range outcome.Observations {
		result.Observations = append(result.Observations, Observation{
			Tool: observation.Operation, Status: observation.Status,
			Summary: observation.Summary, ErrorCode: observation.ErrorCode,
			ElapsedMS: observation.ElapsedMS, Target: observation.Target,
		})
	}
	if outcome.FromCache {
		result.Observations = append(result.Observations, Observation{Tool: "research_cache", Status: "success", Summary: "Reused a recent evidence report.", CacheHit: true, Confidence: outcome.Result.Confidence})
	}
	for _, item := range outcome.Report.Items {
		result.Sources = append(result.Sources, Source{
			ID: item.ID, Title: item.Title, URL: item.URL, Snippet: item.Snippet,
			Content: item.Content, Provider: item.Provider,
			TrustScore: item.Score.Authority, TrustLevel: trustLevel(item.Score.Authority),
			RelevanceScore: item.Score.Relevance, FreshnessScore: item.Score.Freshness,
			EvidenceScore: item.Score.Overall, PublishedAt: item.PublishedAt,
		})
	}
	for _, claim := range outcome.Result.Claims {
		result.Claims = append(result.Claims, Claim{Text: claim.Text, SourceIDs: claim.SourceIDs, Verification: claim.Verification, Confidence: claim.Confidence})
	}
	for _, conflict := range outcome.Result.Contradictions {
		result.Contradictions = append(result.Contradictions, Contradiction{ClaimIDs: conflict.ClaimIDs, Summary: conflict.Summary, Severity: conflict.Severity})
	}
	for _, gap := range outcome.Result.Gaps {
		result.Gaps = append(result.Gaps, ResearchGap{Code: gap.Code, Description: gap.Description, Priority: gap.Priority})
	}
	if len(result.AttemptedQuery) == 0 {
		result.AttemptedQuery = append([]string(nil), plan.Queries...)
	}
	return result, nil
}

func trustLevel(score float64) string {
	switch {
	case score >= 0.8:
		return "high"
	case score >= 0.55:
		return "medium"
	default:
		return "low"
	}
}
