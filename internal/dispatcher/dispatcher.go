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
	"github.com/good-fish-man/agent-runtime/internal/prompt"
	"github.com/good-fish-man/agent-runtime/internal/research"
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

	SkillsDir        string // overrides skill discovery directory
	SkillsConfigPath string // skills-config.yaml path
	SkillsGlobalDir  string // additional skills directory to scan

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
	compact            *contextcompressor.IntegrationService
	availableSkills    []types.Skill
	skillsDir          string
	allowSkillCreation bool
	researchExecutor   *research.Executor
	researchContext    string
	researchEvidence   research.Evidence
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
	d := &Dispatcher{
		client:           client,
		req:              req,
		workDir:          workingDir,
		memInstr:         memInstruction,
		cfg:              cfg,
		researchExecutor: research.NewExecutor(),
	}
	d.skillsDir, d.availableSkills = d.discoverSkills()
	d.compact = d.buildCompactService()
	return d
}

// Run performs a non-streaming orchestrated completion.
func (d *Dispatcher) Run(ctx context.Context, userPrompt string, msgs []eino.ChatMessage) (*eino.Result, error) {
	d.prepareCapabilities(ctx, userPrompt, msgs)
	if err := d.prepareResearch(ctx, userPrompt); err != nil {
		return nil, log.WrapError(err, "dispatcher.Run.research")
	}
	instruction := d.buildInstruction(userPrompt)
	msgs = d.maybeCompact(ctx, msgs)
	result, err := d.client.Generate(ctx, userPrompt, msgs, d.runParams(instruction))
	if err != nil {
		return nil, log.WrapError(err, "dispatcher.Run")
	}
	return d.repairResearchResult(ctx, userPrompt, msgs, instruction, result)
}

// RunStream performs a streaming orchestrated completion.
func (d *Dispatcher) RunStream(ctx context.Context, userPrompt string, msgs []eino.ChatMessage, onChunk func(eino.StreamChunk) error, onAction ...func(actionprotocol.Action) error) (*eino.Result, error) {
	d.prepareCapabilities(ctx, userPrompt, msgs)
	if err := d.prepareResearch(ctx, userPrompt); err != nil {
		return nil, log.WrapError(err, "dispatcher.RunStream.research")
	}
	instruction := d.buildInstruction(userPrompt)
	msgs = d.maybeCompact(ctx, msgs)
	if d.researchEvidence.Plan.Kind == research.KindNews {
		result, err := d.client.Generate(ctx, userPrompt, msgs, d.runParams(instruction))
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
		return result, nil
	}
	params := d.runParams(instruction)
	if len(onAction) > 0 {
		params.OnAction = onAction[0]
	}
	result, err := d.client.Stream(ctx, userPrompt, msgs, params, onChunk)
	if err != nil {
		if eino.IsEmptyToolCallStream(err) {
			log.Warnw("stream tool calls produced no visible content; retrying non-streaming", "error", err)
			result, genErr := d.client.Generate(ctx, userPrompt, msgs, d.runParams(instruction))
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
			return result, nil
		}
		return nil, log.WrapError(err, "dispatcher.RunStream")
	}
	return result, nil
}

func (d *Dispatcher) runParams(instruction string) eino.RunParams {
	return eino.RunParams{
		Instruction:         instruction,
		MaxIterations:       d.maxIterations(),
		WorkingDir:          d.workDir,
		ExtraTools:          d.extraTools,
		DisableBuiltinTools: true,
	}
}

func (d *Dispatcher) prepareCapabilities(ctx context.Context, userPrompt string, msgs []eino.ChatMessage) {
	text := capabilityText(userPrompt, msgs)
	d.req.Skills = selectRelevantSkills(d.availableSkills, text, 3)
	d.allowSkillCreation = matchesAny(text, skillCreationKeywords)
	d.extraTools, d.capabilityIDs = d.buildTools(ctx, text)
	log.Infof("[Dispatcher] capabilities: skills_available=%d skills_selected=%v capabilities_selected=%v", len(d.availableSkills), skillNames(d.req.Skills), d.capabilityIDs)
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
	evidence, err := d.researchExecutor.Execute(ctx, plan)
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
		"latency_ms", evidence.Metrics.ElapsedMS,
		"limit_reached", evidence.LimitReached,
	)
	return nil
}

func (d *Dispatcher) disableModelResearchTools(ctx context.Context) {
	removedCapabilities := []string{
		capability.InternetSearch, capability.InternetFetch,
		capability.BrowserSearch, capability.BrowserRead,
		capability.BrowserAction, capability.BrowserClose,
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
	repaired, err := d.client.Generate(ctx, userPrompt, msgs, d.runParams(instruction+"\n\n"+d.researchEvidence.RepairInstruction(reason)))
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
