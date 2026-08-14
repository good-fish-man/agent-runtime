// Package dispatcher provides a streamlined orchestration layer that wires the
// migrated building blocks — prompt construction, built-in tools, sub-agents,
// context compression and knowledge retrieval — on top of the eino.Client
// agent/runner event loop.
//
// It is a deliberately trimmed re-implementation of the reference runner's
// dispatcher: it orchestrates only the components that have been migrated into
// agent-runtime and omits the heavier, not-yet-migrated features (deep agent,
// MCP, A2A, approval, checkpoints). When a request carries no skills,
// knowledge bases or sub-agents, behaviour is equivalent to calling the
// eino.Client directly with the built-in tool set.
package dispatcher

import (
	"context"
	"strings"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/contextcompressor"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/intent"
	"github.com/good-fish-man/agent-runtime/internal/prompt"
	"github.com/good-fish-man/agent-runtime/internal/research"
	athenarouter "github.com/good-fish-man/agent-runtime/internal/router"
	"github.com/good-fish-man/agent-runtime/internal/types"
	log "github.com/good-fish-man/logx"

	"github.com/cloudwego/eino/components/tool"
)

// Config carries operator-level defaults (from the server configuration) into a
// dispatch run. Zero-value fields fall back to package/env defaults.
type Config struct {
	SandboxImage     string // default docker image when a request enables sandbox without one
	SandboxPptxImage string // default image for pptx rendering
	SandboxWorkdir   string // default container workdir
	SandboxTimeoutMs int    // default sandbox exec timeout

	SkillsDir                string // overrides skill discovery directory
	SkillsConfigPath         string // skills-config.yaml path
	SkillsGlobalDir          string // additional skills directory to scan
	ResearchRunner           research.Runner
	DisableResearch          bool
	ResearchModelPlanning    bool
	ResearchSemanticVerify   bool
	ResearchAdvisorTimeout   time.Duration
	ResearchMaxAdvisorClaims int
}

// Dispatcher orchestrates a single logical agent run.
type Dispatcher struct {
	client   *eino.Client
	req      *types.RunRequest
	workDir  string
	memInstr string
	cfg      Config

	extraTools         []tool.BaseTool
	capabilityIDs      []string
	routePlan          athenarouter.RoutePlan
	compact            *contextcompressor.IntegrationService
	availableSkills    []types.Skill
	skillsDir          string
	allowSkillCreation bool
	researchExecutor   research.Runner
	researchContext    string
	researchEvidence   research.Evidence
	researchProgress   func(research.Progress) error
}

// New builds a Dispatcher for req. memInstruction is an optional memory block
// appended to the system instruction. workingDir binds filesystem/shell tools.
// cfg supplies operator-level defaults (sandbox image, skills dir, ...).
func New(client *eino.Client, req *types.RunRequest, workingDir, memInstruction string, cfg Config) *Dispatcher {
	if req == nil {
		req = &types.RunRequest{}
	}
	if workingDir == "" {
		workingDir = "."
	}
	researchRunner := cfg.ResearchRunner
	if researchRunner == nil && !cfg.DisableResearch {
		researchRunner = research.NewResearchAgent()
	}
	d := &Dispatcher{
		client:           client,
		req:              req,
		workDir:          workingDir,
		memInstr:         memInstruction,
		cfg:              cfg,
		researchExecutor: researchRunner,
	}
	d.skillsDir, d.availableSkills = d.discoverSkills()
	d.compact = d.buildCompactService()
	return d
}

// SetResearchProgressHandler attaches the request-scoped V3 progress sink.
// It must be set before Run or RunStream starts.
func (d *Dispatcher) SetResearchProgressHandler(handler func(research.Progress) error) {
	d.researchProgress = handler
}

// Run performs a non-streaming orchestrated completion.
func (d *Dispatcher) Run(ctx context.Context, userPrompt string, msgs []eino.ChatMessage) (*eino.Result, error) {
	d.prepareCapabilities(ctx, userPrompt, msgs)
	if err := d.prepareResearch(ctx, userPrompt); err != nil {
		return nil, log.WrapError(err, "dispatcher.Run.research")
	}
	if d.researchEvidence.Plan.Kind != research.KindNone && len(d.researchEvidence.Sources) == 0 {
		return d.addResearchUsage(&eino.Result{Content: d.researchEvidence.FallbackAnswer(), FinishReason: "stop"}), nil
	}
	instruction := d.buildInstruction(userPrompt)
	msgs = d.maybeCompact(ctx, msgs)
	result, err := d.client.Generate(ctx, userPrompt, msgs, d.nonStreamingRunParams(ctx, instruction))
	if err != nil {
		return nil, log.WrapError(err, "dispatcher.Run")
	}
	result, err = d.repairResearchResult(ctx, userPrompt, msgs, instruction, result)
	return d.addResearchUsage(result), err
}

// RunStream performs a streaming orchestrated completion.
func (d *Dispatcher) RunStream(ctx context.Context, userPrompt string, msgs []eino.ChatMessage, onChunk func(eino.StreamChunk) error, onAction ...func(actionprotocol.Action) error) (*eino.Result, error) {
	if len(onAction) > 0 {
		if result, handled, err := d.dispatchCapabilityHandoff(ctx, userPrompt, onChunk, onAction[0]); handled {
			return result, err
		}
	}
	if result, handled, err := d.completeDeviceObservation(userPrompt, onChunk); handled {
		return result, err
	}
	d.prepareCapabilities(ctx, userPrompt, msgs)
	if len(onAction) > 0 {
		if result, handled, err := d.dispatchDirectBrowserAction(ctx, userPrompt, onAction[0]); handled {
			return result, err
		}
	}
	if err := d.prepareResearch(ctx, userPrompt); err != nil {
		return nil, log.WrapError(err, "dispatcher.RunStream.research")
	}
	instruction := d.buildInstruction(userPrompt)
	msgs = d.maybeCompact(ctx, msgs)
	if d.researchEvidence.Plan.Kind != research.KindNone {
		if len(d.researchEvidence.Sources) == 0 {
			result := &eino.Result{Content: d.researchEvidence.FallbackAnswer(), FinishReason: "stop"}
			if err := onChunk(eino.StreamChunk{Text: result.Content}); err != nil {
				return nil, log.WrapError(err, "dispatcher.RunStream.researchFallbackEmit")
			}
			return d.addResearchUsage(result), nil
		}
		result, err := d.client.Generate(ctx, userPrompt, msgs, d.nonStreamingRunParams(ctx, instruction))
		if err != nil {
			return nil, log.WrapError(err, "dispatcher.RunStream.researchGenerate")
		}
		result, err = d.repairResearchResult(ctx, userPrompt, msgs, instruction, result)
		if err != nil {
			return nil, err
		}
		if result.Content != "" {
			if err := onChunk(eino.StreamChunk{Text: result.Content}); err != nil {
				return nil, log.WrapError(err, "dispatcher.RunStream.researchEmit")
			}
		}
		return d.addResearchUsage(result), nil
	}
	params := d.runParams(instruction)
	if len(onAction) > 0 {
		params.OnAction = onAction[0]
	}
	result, err := d.client.Stream(ctx, userPrompt, msgs, params, onChunk)
	if err != nil {
		if eino.IsEmptyToolCallStream(err) {
			log.Warnw("stream tool calls produced no visible content; retrying non-streaming", "error", err)
			result, genErr := d.client.Generate(ctx, userPrompt, msgs, d.nonStreamingRunParams(ctx, instruction))
			if genErr != nil {
				return nil, log.WrapError(genErr, "dispatcher.RunStream.toolCallFallback")
			}
			result, genErr = d.repairResearchResult(ctx, userPrompt, msgs, instruction, result)
			if genErr != nil {
				return nil, genErr
			}
			if strings.TrimSpace(result.Content) != "" {
				if emitErr := onChunk(eino.StreamChunk{Text: result.Content}); emitErr != nil {
					return nil, log.WrapError(emitErr, "dispatcher.RunStream.toolCallFallbackEmit")
				}
			} else {
				return nil, log.WrapError(err, "dispatcher.RunStream.toolCallFallback.empty")
			}
			return d.addResearchUsage(result), nil
		}
		return nil, log.WrapError(err, "dispatcher.RunStream")
	}
	return d.addResearchUsage(result), nil
}

func (d *Dispatcher) addResearchUsage(result *eino.Result) *eino.Result {
	if result == nil || d.researchEvidence.Metrics.TotalTokens <= 0 {
		return result
	}
	result.Usage.PromptTokens += d.researchEvidence.Metrics.PromptTokens
	result.Usage.CompletionTokens += d.researchEvidence.Metrics.CompletionTokens
	result.Usage.TotalTokens += d.researchEvidence.Metrics.TotalTokens
	return result
}

func (d *Dispatcher) runParams(instruction string) eino.RunParams {
	params := eino.RunParams{
		Instruction:         instruction,
		MaxIterations:       d.maxIterations(),
		WorkingDir:          d.workDir,
		ExtraTools:          d.extraTools,
		DisableBuiltinTools: true,
	}
	for _, input := range d.req.VisualInputs {
		params.VisualInputs = append(params.VisualInputs, eino.VisualInput{
			ID: input.ID, MIMEType: input.MIMEType, Data: append([]byte(nil), input.Data...),
			SHA256: input.SHA256, Purpose: input.Purpose, Detail: input.Detail,
		})
	}
	return params
}

// nonStreamingRunParams builds run params for the non-streaming Generate path.
// Client-bound tools (browser.*/desktop.*) are stripped because their actions
// require an OnAction sink that only the streaming path provides; offering them
// here would let the model emit an action that cannot be fulfilled.
func (d *Dispatcher) nonStreamingRunParams(ctx context.Context, instruction string) eino.RunParams {
	params := d.runParams(instruction)
	params.ExtraTools = withoutBaseTools(ctx, params.ExtraTools, capability.ClientBoundModelNames()...)
	return params
}

func (d *Dispatcher) prepareCapabilities(ctx context.Context, userPrompt string, msgs []eino.ChatMessage) {
	text := capabilityText(userPrompt, msgs)
	intentSpan := log.StartSpan(ctx, "intent.parse", "message_count", len(msgs), "has_files", len(d.req.Files) > 0)
	parsedIntent := intent.Parse(intent.Request{
		Text:                 userPrompt,
		HasFiles:             len(d.req.Files) > 0,
		ActiveBrowserSession: d.contextString("active_browser_session") != "",
		ActiveDesktopSession: d.contextString("active_desktop_session") != "",
		PreviousUserMessages: d.previousUserMessages(msgs),
	})
	intentSpan.End(nil,
		"intent_mode", parsedIntent.Mode,
		"confidence", parsedIntent.Confidence,
	)
	routeSpan := log.StartSpan(ctx, "route.plan", "intent_mode", parsedIntent.Mode)
	d.routePlan = athenarouter.RouteIntent(parsedIntent)
	routeSpan.End(nil,
		"primary", d.routePlan.Primary,
		"fallback_count", len(d.routePlan.Fallbacks),
		"capability_count", len(d.routePlan.Capabilities),
	)
	d.req.Skills = selectRelevantSkills(d.availableSkills, text, 3)
	// Browser control is implemented by the Action/Observation protocol. Do not
	// expose the legacy agent-browser skill in the same run: its standalone CLI
	// guidance (including CDP setup) conflicts with the device browser runtime.
	if d.usesBrowserCapabilities(d.routePlan) {
		d.req.Skills = withoutSkills(d.req.Skills, "agent-browser")
	}
	d.allowSkillCreation = matchesAny(text, skillCreationKeywords)
	d.extraTools, d.capabilityIDs = d.buildTools(ctx, d.routePlan)
	log.Infof("[Dispatcher] route: primary=%s reason=%s confidence=%.2f fallbacks=%v skills_available=%d skills_selected=%v capabilities_selected=%v",
		d.routePlan.Primary, d.routePlan.Reason, d.routePlan.Intent.Confidence, d.routePlan.Fallbacks,
		len(d.availableSkills), skillNames(d.req.Skills), d.capabilityIDs)
}

func (d *Dispatcher) usesBrowserCapabilities(plan athenarouter.RoutePlan) bool {
	ids := append([]string(nil), plan.Capabilities...)
	for _, configured := range d.req.Capabilities {
		ids = append(ids, configured.ID)
	}
	for _, id := range ids {
		if strings.HasPrefix(id, "browser.") {
			return true
		}
	}
	return false
}

func withoutSkills(skills []types.Skill, removedIDs ...string) []types.Skill {
	removed := make(map[string]bool, len(removedIDs))
	for _, id := range removedIDs {
		removed[strings.ToLower(strings.TrimSpace(id))] = true
	}
	result := make([]types.Skill, 0, len(skills))
	for _, skill := range skills {
		if removed[strings.ToLower(strings.TrimSpace(skill.ID))] {
			continue
		}
		result = append(result, skill)
	}
	return result
}

func (d *Dispatcher) maxIterations() int {
	protocolLimit := 0
	if d.researchEvidence.Plan.Kind != research.KindNone {
		protocolLimit = research.DefaultProtocol().MaxPlannerIterations
	}
	if d.req.Options != nil && d.req.Options.MaxIterations > 0 {
		if protocolLimit > 0 && d.req.Options.MaxIterations > protocolLimit {
			return protocolLimit
		}
		return d.req.Options.MaxIterations
	}
	if protocolLimit > 0 {
		return protocolLimit
	}
	return 0
}

// buildInstruction assembles the system prompt: static sections (keyed by the
// selected capability set) + per-request dynamic sections + optional memory block.
func (d *Dispatcher) buildInstruction(userPrompt string) string {
	parts := make([]string, 0, 5)
	if s := prompt.BuildStaticPrompt(d.capabilityIDs); s != "" {
		parts = append(parts, s)
	}
	if s := prompt.BuildDynamicPrompt(d.req); s != "" {
		parts = append(parts, s)
	}
	parts = append(parts, prompt.GetResponseLanguageSection(d.contextString("locale"), userPrompt))
	parts = append(parts, prompt.GetRuntimeContextSection(time.Now(), d.req.Context))
	if d.researchContext != "" {
		parts = append(parts, d.researchContext)
	}
	if d.memInstr != "" {
		parts = append(parts, d.memInstr)
	}
	return joinNonEmpty(parts, "\n\n")
}

func (d *Dispatcher) prepareResearch(ctx context.Context, userPrompt string) error {
	d.researchContext = ""
	d.researchEvidence = research.Evidence{}
	if d.researchExecutor == nil {
		return nil
	}
	routePlan := d.routePlan
	if routePlan.Primary == "" {
		intentSpan := log.StartSpan(ctx, "intent.parse", "phase", "research_fallback")
		parsed := intent.Parse(intent.Request{
			Text:                 userPrompt,
			HasFiles:             len(d.req.Files) > 0,
			ActiveBrowserSession: d.contextString("active_browser_session") != "",
			ActiveDesktopSession: d.contextString("active_desktop_session") != "",
			PreviousUserMessages: d.previousUserMessages(nil),
		})
		intentSpan.End(nil, "intent_mode", parsed.Mode, "confidence", parsed.Confidence)
		routeSpan := log.StartSpan(ctx, "route.plan", "phase", "research_fallback", "intent_mode", parsed.Mode)
		routePlan = athenarouter.RouteIntent(parsed)
		routeSpan.End(nil,
			"primary", routePlan.Primary,
			"fallback_count", len(routePlan.Fallbacks),
			"capability_count", len(routePlan.Capabilities),
		)
	}
	if routePlan.Primary != athenarouter.RouteResearch {
		return nil
	}
	previousUserPrompts := make([]string, 0, len(d.req.Messages))
	for _, message := range d.req.Messages {
		if message.Role == "user" {
			previousUserPrompts = append(previousUserPrompts, message.Content)
		}
	}
	plan := research.AnalyzeConversation(userPrompt, previousUserPrompts, d.req.Context, time.Now())
	if plan.Kind == research.KindNone {
		return nil
	}
	log.InfowCtx(ctx, "research execution started", "kind", plan.Kind, "queries", len(plan.Queries), "source_limit", plan.MaxSources)
	var evidence research.Evidence
	var err error
	if advanced, ok := d.researchExecutor.(research.AdvancedRunner); ok {
		options := research.ExecuteOptions{
			EnableModelPlanning:  d.cfg.ResearchModelPlanning,
			EnableSemanticVerify: d.cfg.ResearchSemanticVerify,
			MaxAdvisorClaims:     d.cfg.ResearchMaxAdvisorClaims,
			OnProgress:           d.researchProgress,
		}
		if options.EnableModelPlanning || options.EnableSemanticVerify {
			options.Advisor = newResearchModelAdvisor(d.client, d.cfg.ResearchAdvisorTimeout)
		}
		evidence, err = advanced.ExecuteWithOptions(ctx, plan, options)
	} else {
		evidence, err = d.researchExecutor.Execute(ctx, plan)
	}
	if err != nil {
		return err
	}
	for _, failure := range evidence.Failures {
		log.WarnwCtx(ctx, "research source failed", "kind", plan.Kind, "error", failure)
	}
	d.researchContext = evidence.ContextSection()
	d.researchEvidence = evidence
	d.disableModelResearchTools(ctx)
	log.InfowCtx(ctx, "research execution completed",
		"kind", plan.Kind,
		"sources", len(evidence.Sources),
		"failures", len(evidence.Failures),
		"planner_iterations", evidence.Metrics.PlannerIterations,
		"tool_calls", evidence.Metrics.ToolCalls,
		"search_calls", evidence.Metrics.SearchCalls,
		"fetch_calls", evidence.Metrics.FetchCalls,
		"cache_hits", evidence.Metrics.CacheHits,
		"advisor_calls", evidence.Metrics.AdvisorCalls,
		"advisor_tokens", evidence.Metrics.TotalTokens,
		"latency_ms", evidence.Metrics.ElapsedMS,
		"limit_reached", evidence.LimitReached,
		"stop_reason", evidence.StopReason,
		"confidence", evidence.Confidence,
		"remaining_gaps", len(evidence.Gaps),
		"contradictions", len(evidence.Contradictions),
	)
	return nil
}

func (d *Dispatcher) previousUserMessages(msgs []eino.ChatMessage) []string {
	previous := make([]string, 0, len(d.req.Messages)+len(msgs))
	seen := make(map[string]bool)
	add := func(role, content string) {
		content = strings.TrimSpace(content)
		if role == "user" && content != "" && !seen[content] {
			previous = append(previous, content)
			seen[content] = true
		}
	}
	for _, message := range d.req.Messages {
		add(message.Role, message.Content)
	}
	for _, message := range msgs {
		add(message.Role, message.Content)
	}
	return previous
}

func (d *Dispatcher) disableModelResearchTools(ctx context.Context) {
	removedCapabilities := []string{
		capability.InternetSearch, capability.InternetFetch,
		capability.BrowserSearch, capability.BrowserTask, capability.BrowserOpen,
		capability.BrowserNavigate, capability.BrowserLogin, capability.BrowserRead,
		capability.BrowserObserve, capability.BrowserAction, capability.BrowserWait,
		capability.BrowserDownload, capability.BrowserScreenshot, capability.BrowserClose,
	}
	d.capabilityIDs = withoutToolNames(d.capabilityIDs, removedCapabilities...)
	removedModelNames := make([]string, 0, len(removedCapabilities))
	for _, id := range removedCapabilities {
		removedModelNames = append(removedModelNames, capability.ModelName(id))
	}
	d.extraTools = withoutBaseTools(ctx, d.extraTools, removedModelNames...)
}

func (d *Dispatcher) repairResearchResult(ctx context.Context, userPrompt string, msgs []eino.ChatMessage, instruction string, result *eino.Result) (*eino.Result, error) {
	needsRepair, reason := d.researchEvidence.NeedsRepair(result.Content)
	if !needsRepair {
		return result, nil
	}
	log.WarnwCtx(ctx, "research answer rejected", "kind", d.researchEvidence.Plan.Kind, "reason", reason)
	repaired, err := d.client.Generate(ctx, userPrompt, msgs, d.nonStreamingRunParams(ctx, instruction+"\n\n"+d.researchEvidence.RepairInstruction(reason)))
	if err != nil {
		return nil, log.WrapError(err, "dispatcher.repairResearchResult.generate")
	}
	repaired.Usage.PromptTokens += result.Usage.PromptTokens
	repaired.Usage.CompletionTokens += result.Usage.CompletionTokens
	repaired.Usage.TotalTokens += result.Usage.TotalTokens
	if invalid, retryReason := d.researchEvidence.NeedsRepair(repaired.Content); invalid {
		log.WarnwCtx(ctx, "research answer repair rejected", "kind", d.researchEvidence.Plan.Kind, "reason", retryReason)
		repaired.Content = d.researchEvidence.FallbackAnswer()
		repaired.FinishReason = "stop"
	}
	return repaired, nil
}

func joinNonEmpty(parts []string, sep string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += p
	}
	return out
}
