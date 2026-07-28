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

	"github.com/good-fish-man/agent-runtime/internal/contextcompressor"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/prompt"
	"github.com/good-fish-man/agent-runtime/internal/types"
	"github.com/good-fish-man/agent-runtime/log"

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
	toolNames          []string
	compact            *contextcompressor.IntegrationService
	availableSkills    []types.Skill
	skillsDir          string
	allowSkillCreation bool
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
		client:   client,
		req:      req,
		workDir:  workingDir,
		memInstr: memInstruction,
		cfg:      cfg,
	}
	d.skillsDir, d.availableSkills = d.discoverSkills()
	d.compact = d.buildCompactService()
	return d
}

// Run performs a non-streaming orchestrated completion.
func (d *Dispatcher) Run(ctx context.Context, userPrompt string, msgs []eino.ChatMessage) (*eino.Result, error) {
	d.prepareCapabilities(ctx, userPrompt, msgs)
	instruction := d.buildInstruction()
	msgs = d.maybeCompact(ctx, msgs)
	result, err := d.client.Generate(ctx, userPrompt, msgs, d.runParams(instruction))
	if err != nil {
		return nil, log.WrapError(err, "dispatcher.Run")
	}
	return result, nil
}

// RunStream performs a streaming orchestrated completion.
func (d *Dispatcher) RunStream(ctx context.Context, userPrompt string, msgs []eino.ChatMessage, onChunk func(eino.StreamChunk) error) (*eino.Result, error) {
	d.prepareCapabilities(ctx, userPrompt, msgs)
	instruction := d.buildInstruction()
	msgs = d.maybeCompact(ctx, msgs)
	result, err := d.client.Stream(ctx, userPrompt, msgs, d.runParams(instruction), onChunk)
	if err != nil {
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
	d.extraTools, d.toolNames = d.buildTools(ctx, text)
	log.Infof("[Dispatcher] capabilities: skills_available=%d skills_selected=%v tools_selected=%v", len(d.availableSkills), skillNames(d.req.Skills), d.toolNames)
}

func (d *Dispatcher) maxIterations() int {
	if d.req.Options != nil && d.req.Options.MaxIterations > 0 {
		return d.req.Options.MaxIterations
	}
	return 0
}

// buildInstruction assembles the system prompt: static sections (keyed by the
// enabled tool set) + per-request dynamic sections + optional memory block.
func (d *Dispatcher) buildInstruction() string {
	parts := make([]string, 0, 4)
	if s := prompt.BuildStaticPrompt(d.toolNames); s != "" {
		parts = append(parts, s)
	}
	if s := prompt.BuildDynamicPrompt(d.req); s != "" {
		parts = append(parts, s)
	}
	if d.memInstr != "" {
		parts = append(parts, d.memInstr)
	}
	return joinNonEmpty(parts, "\n\n")
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
