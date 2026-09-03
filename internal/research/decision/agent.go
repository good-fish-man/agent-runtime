// Package decision implements the research decision layer. It owns intent,
// query planning, gap detection, follow-up planning, and the bounded research
// loop while delegating retrieval and evidence processing to lower layers.
package decision

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/research/evidence"
	"github.com/good-fish-man/agent-runtime/internal/research/searchsystem"
)

type Task struct {
	Kind           string
	Prompt         string
	Goal           string
	Constraints    []string
	InitialQueries []string
	SeedURLs       []string
	MinSources     int
	MaxSources     int
	Date           string
	Language       string
}

type Budget struct {
	MaxQueries      int
	MaxPages        int
	MaxRounds       int
	ResultsPerQuery int
	MaxContextChars int
	MaxDuration     time.Duration
	CacheTTL        time.Duration
	NewsCacheTTL    time.Duration
}

func DefaultBudget() Budget {
	return Budget{MaxQueries: 6, MaxPages: 8, MaxRounds: 3, ResultsPerQuery: 5, MaxContextChars: 80_000, MaxDuration: 30 * time.Second, CacheTTL: time.Hour, NewsCacheTTL: 5 * time.Minute}
}

type Intent struct {
	Kind            string
	Topic           string
	NeedsFreshness  bool
	NeedsAuthority  bool
	NeedsComparison bool
	PreferredSource []searchsystem.SourceKind
}

type Plan struct {
	Intent  Intent
	Queries []searchsystem.Query
}

type Gap struct {
	Code        string
	Description string
	Priority    int
}

type Usage struct {
	Rounds           int
	Searches         int
	Pages            int
	CacheHits        int
	AdvisorCalls     int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	ElapsedMS        int64
	LimitReached     bool
}

type Source struct {
	ID        string
	Title     string
	URL       string
	Provider  string
	Score     float64
	Authority float64
	Relevance float64
}

type Claim struct {
	Text         string
	SourceIDs    []string
	Verification string
	Confidence   float64
}

type StructuredResult struct {
	Status         string
	StopReason     string
	Confidence     float64
	Claims         []Claim
	Sources        []Source
	Gaps           []Gap
	Contradictions []evidence.Contradiction
	Usage          Usage
}

type Outcome struct {
	Plan         Plan
	Report       evidence.Report
	Result       StructuredResult
	Attempted    []searchsystem.Query
	Observations []searchsystem.Observation
	Failures     []string
	FromCache    bool
}

type IntentAnalyzer interface {
	Analyze(Task) Intent
}

type QueryPlanner interface {
	Plan(Task, Intent, Budget) Plan
}

type GapDetector interface {
	Detect(Task, Plan, evidence.Report) []Gap
}

type FollowUpPlanner interface {
	Next(Task, Plan, []Gap, map[string]bool, int) []searchsystem.Query
}

type KnowledgeSynthesizer interface {
	Synthesize(evidence.Report, []Gap, Usage, string) StructuredResult
}

type SearchSystem interface {
	ExecuteRound(context.Context, []searchsystem.Query, []string, int, int, int) (searchsystem.RoundResult, error)
}

type EvidencePipeline interface {
	Merge(evidence.Request, evidence.Report, searchsystem.RoundResult, int) evidence.Report
}

type ResearchCache interface {
	Get(context.Context, string, time.Duration) (evidence.Report, bool)
	Put(context.Context, string, evidence.Report)
}

type Agent struct {
	intent      IntentAnalyzer
	planner     QueryPlanner
	search      SearchSystem
	evidence    EvidencePipeline
	gaps        GapDetector
	followUp    FollowUpPlanner
	synthesizer KnowledgeSynthesizer
	cache       ResearchCache
}

func NewAgent(search SearchSystem, pipeline EvidencePipeline, cache ResearchCache) *Agent {
	return &Agent{
		intent: DefaultIntentAnalyzer{}, planner: DefaultQueryPlanner{}, search: search,
		evidence: pipeline, gaps: DefaultGapDetector{}, followUp: DefaultFollowUpPlanner{},
		synthesizer: DefaultKnowledgeSynthesizer{}, cache: cache,
	}
}

// Run executes an adaptive search loop. Runtime limits, rather than an LLM,
// are authoritative for cancellation, timeouts, and maximum work.
func (a *Agent) Run(ctx context.Context, task Task, budget Budget) (Outcome, error) {
	return a.RunWithOptions(ctx, task, budget, RunOptions{})
}

// RunWithOptions executes V3 model-assisted research while keeping the
// deterministic planner, evidence pipeline, and runtime budget authoritative.
func (a *Agent) RunWithOptions(ctx context.Context, task Task, budget Budget, options RunOptions) (Outcome, error) {
	started := time.Now()
	budget = normalizeBudget(budget)
	runCtx, cancel := context.WithTimeout(ctx, budget.MaxDuration)
	defer cancel()
	if err := emitProgress(options, Progress{Stage: "intent", Message: "Understanding the research goal", Percent: 5}); err != nil {
		return Outcome{}, err
	}
	intent := a.intent.Analyze(task)
	plan := a.planner.Plan(task, intent, budget)
	outcome := Outcome{Plan: plan}
	usage := Usage{}
	cacheKey := evidence.CacheKey(task.Kind, task.Prompt, task.Date)
	cacheTTL := budget.CacheTTL
	if task.Kind == "news" {
		cacheTTL = budget.NewsCacheTTL
	}
	// A cache hit should not spend model tokens merely to recreate a query plan
	// whose evidence has already been collected for the same task and date.
	if len(plan.Queries) > 0 && a.cache != nil {
		if report, ok := a.cache.Get(runCtx, cacheKey, cacheTTL); ok {
			gaps := a.gaps.Detect(task, plan, report)
			usage.CacheHits = 1
			usage.ElapsedMS = time.Since(started).Milliseconds()
			outcome.Report, outcome.FromCache = report, true
			outcome.Result = a.synthesizer.Synthesize(report, gaps, usage, "cache_hit")
			if err := emitProgress(options, Progress{
				Stage: "complete", Message: "Reused cached research evidence", Percent: 100,
				Queries: len(plan.Queries), QueryTexts: progressQueryTexts(plan.Queries), Sources: len(report.Items),
				ValuablePages: valuablePages(report, 8), Confidence: report.Confidence, Completed: true,
			}); err != nil {
				return outcome, err
			}
			return outcome, nil
		}
	}
	if options.Advisor != nil && options.EnableModelPlanning {
		if err := emitProgress(options, Progress{Stage: "planning", Message: "Refining search queries", Percent: 10, Queries: len(plan.Queries)}); err != nil {
			return outcome, err
		}
		usage.AdvisorCalls++
		advice, err := options.Advisor.RefinePlan(runCtx, PlanAdviceRequest{Task: task, Intent: intent, Baseline: plan, Budget: budget})
		addAdvisorUsage(&usage, advice.Usage)
		if err != nil {
			outcome.Failures = append(outcome.Failures, "model-assisted query planning failed; deterministic plan retained: "+err.Error())
		} else {
			plan = mergeAdvisedQueries(task, plan, advice, budget)
			outcome.Plan = plan
		}
	}
	if err := emitProgress(options, Progress{Stage: "planned", Message: "Search plan ready", Percent: 15, Queries: len(plan.Queries), QueryTexts: progressQueryTexts(plan.Queries)}); err != nil {
		return outcome, err
	}
	if len(plan.Queries) == 0 {
		outcome.Result = a.synthesizer.Synthesize(evidence.Report{}, []Gap{{Code: "no_query", Description: "No usable research query was produced.", Priority: 100}}, usage, "no_query")
		if err := emitProgress(options, Progress{Stage: "complete", Message: "No usable research query was produced", Percent: 100, Completed: true}); err != nil {
			return outcome, err
		}
		return outcome, nil
	}

	attempted := make(map[string]bool)
	pending := append([]searchsystem.Query(nil), plan.Queries...)
	report := evidence.Report{}
	var finalGaps []Gap
	stopReason := "budget_exhausted"
	seedURLs := append([]string(nil), task.SeedURLs...)

	for round := 1; round <= budget.MaxRounds; round++ {
		remainingQueries := budget.MaxQueries - usage.Searches
		remainingPages := budget.MaxPages - usage.Pages
		if remainingQueries <= 0 || remainingPages <= 0 {
			usage.LimitReached = true
			break
		}
		batchSize := 2
		if round == 1 && task.MinSources >= 4 {
			batchSize = 3
		}
		batch := takeQueries(pending, attempted, minInt(batchSize, remainingQueries))
		if len(batch) == 0 {
			batch = a.followUp.Next(task, plan, finalGaps, attempted, minInt(2, remainingQueries))
		}
		if len(batch) == 0 {
			stopReason = "no_useful_follow_up"
			break
		}
		progressPercent := 20 + ((round - 1) * 45 / maxInt(budget.MaxRounds, 1))
		if err := emitProgress(options, Progress{Stage: "searching", Message: fmt.Sprintf("Searching evidence round %d", round), Percent: progressPercent, Round: round, Queries: len(outcome.Attempted) + len(batch), Sources: len(report.Items)}); err != nil {
			return outcome, err
		}
		for _, query := range batch {
			attempted[queryFingerprint(query)] = true
			outcome.Attempted = append(outcome.Attempted, query)
		}
		pageLimit := minInt(remainingPages, maxInt(task.MinSources-len(report.Items), 3))
		if task.MaxSources > 0 {
			pageLimit = minInt(pageLimit, maxInt(task.MaxSources-len(report.Items), 1))
		}
		roundResult, err := a.search.ExecuteRound(runCtx, batch, seedURLs, remainingQueries, budget.ResultsPerQuery, pageLimit)
		seedURLs = nil
		usage.Rounds++
		usage.Searches += countOperations(roundResult.Observations, "search")
		usage.Pages += countOperations(roundResult.Observations, "fetch")
		outcome.Observations = append(outcome.Observations, roundResult.Observations...)
		outcome.Failures = append(outcome.Failures, roundResult.Failures...)
		if err != nil {
			if ctx.Err() != nil {
				return outcome, ctx.Err()
			}
			if runCtx.Err() == context.DeadlineExceeded {
				usage.LimitReached = true
				outcome.Failures = append(outcome.Failures, "research execution reached its time limit; using best available evidence")
				break
			}
			return outcome, err
		}
		report = a.evidence.Merge(evidence.Request{Task: taskResearchScope(task), Kind: task.Kind, Date: task.Date, MinSources: task.MinSources}, report, roundResult, len(outcome.Attempted))
		if err := emitProgress(options, Progress{
			Stage: "ranking", Message: "Ranking and cross-checking evidence", Percent: minInt(progressPercent+15, 78),
			Round: round, Queries: len(outcome.Attempted), QueryTexts: progressQueryTexts(outcome.Attempted),
			Sources: len(report.Items), ValuablePages: valuablePages(report, 8), Confidence: report.Confidence,
		}); err != nil {
			return outcome, err
		}
		finalGaps = a.gaps.Detect(task, plan, report)
		if len(finalGaps) == 0 {
			stopReason = "evidence_sufficient"
			break
		}
		if err := emitProgress(options, Progress{
			Stage: "gap_analysis", Message: fmt.Sprintf("Found %d evidence gap(s); preparing follow-up queries", len(finalGaps)),
			Percent: minInt(progressPercent+20, 82), Round: round, Queries: len(outcome.Attempted),
			QueryTexts: progressQueryTexts(outcome.Attempted), Sources: len(report.Items),
			ValuablePages: valuablePages(report, 8), Confidence: report.Confidence,
		}); err != nil {
			return outcome, err
		}
		pending = append(pending, a.followUp.Next(task, plan, finalGaps, attempted, budget.MaxQueries-usage.Searches)...)
	}

	if len(report.Claims) > 0 && options.Advisor != nil && options.EnableSemanticVerify && runCtx.Err() == nil {
		if err := emitProgress(options, Progress{
			Stage: "verifying", Message: "Semantically verifying key claims", Percent: 88,
			Queries: len(outcome.Attempted), QueryTexts: progressQueryTexts(outcome.Attempted), Sources: len(report.Items),
			ValuablePages: valuablePages(report, 8), Confidence: report.Confidence,
		}); err != nil {
			return outcome, err
		}
		usage.AdvisorCalls++
		verification, err := options.Advisor.VerifyClaims(runCtx, ClaimVerificationRequest{Task: task, Report: report, MaxClaims: options.MaxAdvisorClaims})
		addAdvisorUsage(&usage, verification.Usage)
		if err != nil {
			outcome.Failures = append(outcome.Failures, "semantic claim verification failed; deterministic verification retained: "+err.Error())
		} else {
			report = applySemanticVerification(report, verification, options.MaxAdvisorClaims)
			finalGaps = a.gaps.Detect(task, plan, report)
			if len(finalGaps) > 0 && stopReason == "evidence_sufficient" {
				stopReason = "semantic_verification_gap"
			}
		}
	}

	usage.ElapsedMS = time.Since(started).Milliseconds()
	if usage.Rounds >= budget.MaxRounds && len(finalGaps) > 0 {
		usage.LimitReached = true
	}
	outcome.Report = report
	if err := emitProgress(options, Progress{
		Stage: "synthesizing", Message: "Preparing structured research context", Percent: 95,
		Queries: len(outcome.Attempted), QueryTexts: progressQueryTexts(outcome.Attempted), Sources: len(report.Items),
		ValuablePages: valuablePages(report, 8), Confidence: report.Confidence,
	}); err != nil {
		return outcome, err
	}
	outcome.Result = a.synthesizer.Synthesize(report, finalGaps, usage, stopReason)
	if len(report.Items) > 0 && a.cache != nil {
		a.cache.Put(runCtx, cacheKey, report)
	}
	completionMessage := "Research evidence is ready"
	if len(report.Items) == 0 {
		completionMessage = "Research finished without verifiable pages"
	}
	if err := emitProgress(options, Progress{
		Stage: "complete", Message: completionMessage, Percent: 100, Round: usage.Rounds,
		Queries: len(outcome.Attempted), QueryTexts: progressQueryTexts(outcome.Attempted), Sources: len(report.Items),
		ValuablePages: valuablePages(report, 8), Confidence: report.Confidence, Completed: true,
	}); err != nil {
		return outcome, err
	}
	return outcome, nil
}

func emitProgress(options RunOptions, progress Progress) error {
	if options.OnProgress == nil {
		return nil
	}
	return options.OnProgress(progress)
}

func addAdvisorUsage(usage *Usage, value AdvisorUsage) {
	if usage == nil {
		return
	}
	usage.PromptTokens += value.PromptTokens
	usage.CompletionTokens += value.CompletionTokens
	usage.TotalTokens += value.TotalTokens
}

type DefaultIntentAnalyzer struct{}

func (DefaultIntentAnalyzer) Analyze(task Task) Intent {
	intent := Intent{Kind: task.Kind, Topic: taskResearchScope(task), NeedsAuthority: true, PreferredSource: []searchsystem.SourceKind{searchsystem.SourceGeneral, searchsystem.SourceOfficial}}
	switch task.Kind {
	case "news":
		intent.NeedsFreshness = true
		intent.PreferredSource = []searchsystem.SourceKind{searchsystem.SourceNews, searchsystem.SourceOfficial, searchsystem.SourceGeneral}
	case "comparison":
		intent.NeedsComparison = true
		intent.PreferredSource = []searchsystem.SourceKind{searchsystem.SourceOfficial, searchsystem.SourceGeneral}
		if isTechnicalResearch(task.Prompt) {
			intent.PreferredSource = append(intent.PreferredSource, searchsystem.SourceGitHub)
		}
		if containsAnyFold(task.Prompt, "paper", "study", "论文", "学术", "arxiv", "scientific") {
			intent.PreferredSource = append(intent.PreferredSource, searchsystem.SourceAcademic)
		}
	case "research":
		intent.PreferredSource = []searchsystem.SourceKind{searchsystem.SourceOfficial, searchsystem.SourceGeneral}
		if isTechnicalResearch(task.Prompt) {
			intent.PreferredSource = append(intent.PreferredSource, searchsystem.SourceGitHub)
		}
		if containsAnyFold(task.Prompt, "paper", "research", "study", "论文", "学术", "研究证据", "scientific") {
			intent.PreferredSource = append(intent.PreferredSource, searchsystem.SourceAcademic)
		}
	case "travel":
		intent.NeedsFreshness = true
		intent.PreferredSource = []searchsystem.SourceKind{searchsystem.SourceOfficial, searchsystem.SourceGeneral}
	case "procedure":
		intent.NeedsFreshness = true
		intent.PreferredSource = []searchsystem.SourceKind{searchsystem.SourceOfficial, searchsystem.SourceGeneral}
	}
	return intent
}

type DefaultQueryPlanner struct{}

func (DefaultQueryPlanner) Plan(task Task, intent Intent, budget Budget) Plan {
	queries := make([]searchsystem.Query, 0, budget.MaxQueries)
	add := func(text, purpose string, priority int, source searchsystem.SourceKind) {
		text = strings.TrimSpace(text)
		if text == "" || hasQueryText(queries, text) || len(queries) >= budget.MaxQueries {
			return
		}
		queries = append(queries, searchsystem.Query{ID: fmt.Sprintf("q-%d", len(queries)+1), Text: text, Purpose: purpose, Priority: priority, PreferredSource: []searchsystem.SourceKind{source}})
	}
	base := strings.TrimSpace(task.Goal)
	if base == "" && task.Kind != "procedure" {
		base = coreResearchTopic(task.Prompt)
	}
	if base == "" {
		base = compactResearchQuery(task.Prompt)
	}
	for i, query := range task.InitialQueries {
		if task.Kind == "procedure" {
			add(compactResearchQuery(query), "official_procedure", 100-i, searchsystem.SourceOfficial)
			continue
		}
		source := classifySource(query, intent)
		text := compactResearchQuery(query)
		if task.Kind == "research" || task.Kind == "comparison" {
			text = sourceResearchQuery(base, source)
		}
		add(text, "user_request", 100-i, source)
	}
	switch task.Kind {
	case "news":
		add(base+" "+task.Date, "latest_reporting", 95, searchsystem.SourceNews)
		add(base+" official announcement "+task.Date, "primary_source", 90, searchsystem.SourceOfficial)
		add(base+" independent coverage "+task.Date, "cross_check", 75, searchsystem.SourceGeneral)
	case "comparison":
		add(base+" official specifications", "primary_specs", 90, searchsystem.SourceOfficial)
		if containsSource(intent.PreferredSource, searchsystem.SourceGitHub) {
			add(base+" GitHub repository SDK implementation", "implementation", 85, searchsystem.SourceGitHub)
		}
		add(base+" independent review comparison", "independent_comparison", 80, searchsystem.SourceGeneral)
	case "travel":
		add(base+" official tourism transport", "official_travel_info", 90, searchsystem.SourceOfficial)
		add(base+" current prices opening hours", "current_constraints", 80, searchsystem.SourceGeneral)
	case "procedure":
		scope := taskResearchScope(task)
		add(scope+" official eligibility required documents process", "official_requirements", 95, searchsystem.SourceOfficial)
		add(scope+" local authority appointment assessment exceptions", "local_variations", 75, searchsystem.SourceGeneral)
	default:
		add(base+" official documentation", "primary_source", 90, searchsystem.SourceOfficial)
		if containsSource(intent.PreferredSource, searchsystem.SourceGitHub) {
			add(base+" implementation examples", "implementation", 80, searchsystem.SourceGitHub)
		}
		if containsSource(intent.PreferredSource, searchsystem.SourceAcademic) {
			add(base+" research paper", "academic_evidence", 70, searchsystem.SourceAcademic)
		}
		add(base+" independent analysis", "cross_check", 65, searchsystem.SourceGeneral)
	}
	return Plan{Intent: intent, Queries: queries}
}

type DefaultGapDetector struct{}

func (DefaultGapDetector) Detect(task Task, plan Plan, report evidence.Report) []Gap {
	minimum := task.MinSources
	if minimum <= 0 {
		minimum = 2
	}
	gaps := make([]Gap, 0, 5)
	if len(report.Items) < minimum {
		gaps = append(gaps, Gap{Code: "insufficient_sources", Description: fmt.Sprintf("Need %d source(s), found %d.", minimum, len(report.Items)), Priority: 100})
	}
	if report.DistinctHosts < minInt(minimum, 3) {
		gaps = append(gaps, Gap{Code: "insufficient_diversity", Description: "Evidence does not cover enough independent domains.", Priority: 90})
	}
	if report.AuthoritativeCount == 0 {
		gaps = append(gaps, Gap{Code: "missing_authority", Description: "No authoritative primary source has been collected.", Priority: 95})
	}
	for _, kind := range []searchsystem.SourceKind{searchsystem.SourceGitHub, searchsystem.SourceAcademic} {
		if containsSource(plan.Intent.PreferredSource, kind) && !reportContainsSource(report, kind) {
			gaps = append(gaps, Gap{Code: "missing_" + string(kind), Description: fmt.Sprintf("The task requires at least one %s source.", kind), Priority: 88})
		}
	}
	if len(report.Contradictions) > 0 {
		gaps = append(gaps, Gap{Code: "unresolved_conflict", Description: "Conflicting claims require an additional primary source.", Priority: 98})
	}
	if len(report.Items) > 0 && report.Confidence < 0.48 {
		gaps = append(gaps, Gap{Code: "low_confidence", Description: "Collected evidence has low relevance or authority.", Priority: 80})
	}
	sort.SliceStable(gaps, func(i, j int) bool { return gaps[i].Priority > gaps[j].Priority })
	return gaps
}

type DefaultFollowUpPlanner struct{}

func (DefaultFollowUpPlanner) Next(task Task, plan Plan, gaps []Gap, attempted map[string]bool, limit int) []searchsystem.Query {
	if limit <= 0 {
		return nil
	}
	result := make([]searchsystem.Query, 0, limit)
	for _, query := range plan.Queries {
		if !attempted[queryFingerprint(query)] {
			result = append(result, query)
			if len(result) >= limit {
				return result
			}
		}
	}
	base := taskResearchScope(task)
	for _, gap := range gaps {
		query := searchsystem.Query{ID: "follow-" + gap.Code, Priority: gap.Priority}
		switch gap.Code {
		case "missing_authority", "unresolved_conflict":
			query.Text, query.Purpose, query.PreferredSource = base+" official primary source", "verify_primary_source", []searchsystem.SourceKind{searchsystem.SourceOfficial}
		case "insufficient_diversity":
			query.Text, query.Purpose, query.PreferredSource = base+" independent analysis", "diverse_source", []searchsystem.SourceKind{searchsystem.SourceGeneral}
		case "low_confidence":
			query.Text, query.Purpose, query.PreferredSource = base+" detailed evidence examples", "improve_relevance", []searchsystem.SourceKind{searchsystem.SourceGeneral}
		case "missing_github":
			query.Text, query.Purpose, query.PreferredSource = base+" implementation source code", "technical_implementation", []searchsystem.SourceKind{searchsystem.SourceGitHub}
		case "missing_academic":
			query.Text, query.Purpose, query.PreferredSource = base+" research paper evidence", "academic_evidence", []searchsystem.SourceKind{searchsystem.SourceAcademic}
		default:
			query.Text, query.Purpose, query.PreferredSource = base+" additional reliable sources", "fill_coverage", []searchsystem.SourceKind{searchsystem.SourceGeneral}
		}
		if !attempted[queryFingerprint(query)] {
			result = append(result, query)
		}
		if len(result) >= limit {
			break
		}
	}
	return result
}

type DefaultKnowledgeSynthesizer struct{}

func (DefaultKnowledgeSynthesizer) Synthesize(report evidence.Report, gaps []Gap, usage Usage, stopReason string) StructuredResult {
	status := "complete"
	if len(report.Items) == 0 {
		status = "no_evidence"
	} else if len(gaps) > 0 {
		status = "partial"
	}
	result := StructuredResult{Status: status, StopReason: stopReason, Confidence: report.Confidence, Gaps: gaps, Contradictions: report.Contradictions, Usage: usage}
	for _, item := range report.Items {
		result.Sources = append(result.Sources, Source{ID: item.ID, Title: item.Title, URL: item.URL, Provider: item.Provider, Score: item.Score.Overall, Authority: item.Score.Authority, Relevance: item.Score.Relevance})
	}
	for _, claim := range report.Claims {
		result.Claims = append(result.Claims, Claim{Text: claim.Text, SourceIDs: append([]string(nil), claim.SourceIDs...), Verification: claim.Verification, Confidence: claim.Confidence})
	}
	return result
}

func normalizeBudget(value Budget) Budget {
	defaults := DefaultBudget()
	if value.MaxQueries <= 0 {
		value.MaxQueries = defaults.MaxQueries
	}
	if value.MaxPages <= 0 {
		value.MaxPages = defaults.MaxPages
	}
	if value.MaxRounds <= 0 {
		value.MaxRounds = defaults.MaxRounds
	}
	if value.ResultsPerQuery <= 0 {
		value.ResultsPerQuery = defaults.ResultsPerQuery
	}
	if value.MaxContextChars <= 0 {
		value.MaxContextChars = defaults.MaxContextChars
	}
	if value.MaxDuration <= 0 {
		value.MaxDuration = defaults.MaxDuration
	}
	if value.CacheTTL <= 0 {
		value.CacheTTL = defaults.CacheTTL
	}
	if value.NewsCacheTTL <= 0 {
		value.NewsCacheTTL = defaults.NewsCacheTTL
	}
	return value
}

func takeQueries(values []searchsystem.Query, attempted map[string]bool, limit int) []searchsystem.Query {
	result := make([]searchsystem.Query, 0, limit)
	for _, query := range values {
		if attempted[queryFingerprint(query)] {
			continue
		}
		result = append(result, query)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func queryFingerprint(query searchsystem.Query) string {
	return strings.ToLower(strings.Join(strings.Fields(query.Text), " ")) + "|" + fmt.Sprint(query.PreferredSource)
}

func hasQueryText(queries []searchsystem.Query, text string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
	for _, query := range queries {
		if strings.ToLower(strings.Join(strings.Fields(query.Text), " ")) == normalized {
			return true
		}
	}
	return false
}

func classifySource(query string, intent Intent) searchsystem.SourceKind {
	lower := strings.ToLower(query)
	switch {
	case containsAnyFold(lower, "independent review", "independent coverage", "independent analysis", "独立评测", "独立来源", "独立分析"):
		return searchsystem.SourceGeneral
	case containsAnyFold(lower, "official specification", "official documentation", "official source", "official requirements", "官方", "政府", "主管机关"):
		return searchsystem.SourceOfficial
	case strings.Contains(lower, "paper") || strings.Contains(lower, "论文") || strings.Contains(lower, "arxiv"):
		return searchsystem.SourceAcademic
	case strings.Contains(lower, "github"):
		return searchsystem.SourceGitHub
	case intent.Kind == "news":
		return searchsystem.SourceNews
	default:
		return searchsystem.SourceGeneral
	}
}

func taskResearchScope(task Task) string {
	goal := strings.TrimSpace(task.Goal)
	if goal == "" {
		goal = strings.TrimSpace(task.Prompt)
	}
	parts := []string{goal}
	for _, constraint := range task.Constraints {
		constraint = strings.TrimSpace(constraint)
		if constraint != "" && !strings.Contains(goal, constraint) {
			parts = append(parts, constraint)
		}
	}
	return strings.Join(parts, " ")
}

func isTechnicalResearch(value string) bool {
	return containsAnyFold(value, "mcp", "api", "sdk", "protocol", "协议", "github", "代码", "code", "framework", "框架", "library", "软件", "software", "agent")
}

var researchInstructionPhrases = []string{
	"use and cite reliable official and independent sources", "and cite the valuable pages you used", "cite the valuable pages you used",
	"using multiple reliable sources", "use multiple reliable sources",
	"使用多个可靠来源并给出出处", "使用多个可靠来源", "并给出出处", "给出出处",
	"请深入研究", "深入研究", "帮我研究", "请研究", "research the", "research", "investigate",
	"并比较", "compare the", "compare",
}

func compactResearchQuery(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	for _, phrase := range researchInstructionPhrases {
		value = replaceFold(value, phrase, " ")
	}
	value = strings.NewReplacer(
		",", " ", ".", " ", ";", " ", ":", " ", "?", " ", "!", " ",
		"，", " ", "。", " ", "；", " ", "：", " ", "？", " ", "！", " ",
		"(", " ", ")", " ", "（", " ", "）", " ",
	).Replace(value)
	stopWords := map[string]bool{
		"a": true, "an": true, "and": true, "are": true, "for": true, "from": true,
		"cite": true, "in": true, "independent": true, "into": true, "of": true, "on": true,
		"or": true, "official": true, "please": true, "reliable": true, "source": true, "sources": true,
		"the": true, "to": true, "use": true, "used": true, "using": true, "with": true,
	}
	fields := strings.Fields(value)
	compacted := make([]string, 0, minInt(len(fields), 18))
	for _, field := range fields {
		field = strings.Trim(field, " \t\r\n-_/")
		if field == "" || stopWords[strings.ToLower(field)] {
			continue
		}
		compacted = append(compacted, field)
		if len(compacted) >= 18 {
			break
		}
	}
	result := strings.Join(compacted, " ")
	runes := []rune(result)
	if len(runes) > 140 {
		result = strings.TrimSpace(string(runes[:140]))
	}
	return result
}

func coreResearchTopic(value string) string {
	for _, phrase := range researchInstructionPhrases {
		value = replaceFold(value, phrase, " ")
	}
	if index := strings.IndexAny(value, ",.;:，。；："); index > 0 {
		value = value[:index]
	}
	topic := compactResearchQuery(value)
	for _, suffix := range []string{"的官方架构", "官方架构", "的安全边界", "安全边界", "的架构", "架构"} {
		topic = strings.TrimSpace(strings.TrimSuffix(topic, suffix))
	}
	fields := strings.Fields(topic)
	result := make([]string, 0, minInt(len(fields), 6))
	for _, field := range fields {
		normalized := strings.ToLower(strings.Trim(field, "-_/"))
		if len(result) > 0 && containsString([]string{"architecture", "security", "boundaries", "sdk", "sdks", "implementation", "implementations", "documentation", "repository", "repositories"}, normalized) {
			break
		}
		result = append(result, field)
		if len(result) >= 6 {
			break
		}
	}
	return strings.Join(result, " ")
}

func sourceResearchQuery(topic string, source searchsystem.SourceKind) string {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return ""
	}
	chinese := false
	for _, r := range topic {
		if r >= '\u4e00' && r <= '\u9fff' {
			chinese = true
			break
		}
	}
	if chinese {
		switch source {
		case searchsystem.SourceOfficial:
			return topic + " 官方文档"
		case searchsystem.SourceGitHub:
			return topic + " SDK"
		case searchsystem.SourceAcademic:
			return topic + " 论文"
		default:
			return topic + " 架构 安全"
		}
	}
	switch source {
	case searchsystem.SourceOfficial:
		return topic + " official documentation"
	case searchsystem.SourceGitHub:
		return topic + " SDK"
	case searchsystem.SourceAcademic:
		return topic + " research paper"
	default:
		return topic + " architecture security"
	}
}

func replaceFold(value, old, replacement string) string {
	if old == "" {
		return value
	}
	for {
		index := strings.Index(strings.ToLower(value), strings.ToLower(old))
		if index < 0 {
			return value
		}
		value = value[:index] + replacement + value[index+len(old):]
	}
}

func containsSource(values []searchsystem.SourceKind, expected searchsystem.SourceKind) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func reportContainsSource(report evidence.Report, expected searchsystem.SourceKind) bool {
	for _, item := range report.Items {
		if item.Kind == expected {
			return true
		}
	}
	return false
}

func containsAnyFold(value string, candidates ...string) bool {
	lower := strings.ToLower(value)
	for _, candidate := range candidates {
		if strings.Contains(lower, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

func countOperations(values []searchsystem.Observation, operation string) int {
	count := 0
	for _, value := range values {
		if value.Operation == operation {
			count++
		}
	}
	return count
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
