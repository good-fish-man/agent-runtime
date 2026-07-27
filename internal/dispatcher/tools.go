package dispatcher

import (
	"context"

	"github.com/good-fish-man/agent-runtime/internal/plugins"
	"github.com/good-fish-man/agent-runtime/internal/retriever"
	"github.com/good-fish-man/agent-runtime/internal/subagent"
	"github.com/good-fish-man/agent-runtime/internal/tools"
	"github.com/good-fish-man/agent-runtime/internal/types"
	"github.com/good-fish-man/agent-runtime/log"

	"github.com/cloudwego/eino/components/tool"
)

// buildTools assembles the extra (non built-in) tools for this run and returns
// them along with the full set of enabled tool names (built-in + extra) used to
// render the prompt's "using your tools" section.
func (d *Dispatcher) buildTools(ctx context.Context, relevanceText string) ([]tool.BaseTool, []string) {
	builtinNames := selectBuiltinTools(relevanceText, len(d.req.Files) > 0)
	extra := tools.ToolsByNamesWithBasePath(d.workDir, builtinNames)
	if imageModel, ok := d.req.Models["image"]; ok && imageModel.Name != "" {
		extra = append(extra, tools.NewImageGenerationTool(imageModel))
	}

	// Sub-agent orchestration tools (spawn / delegate / parallel / manage).
	if len(d.req.SubAgents) > 0 {
		mgr := subagent.NewSubAgentManager(d.client.Model())
		mgr.RegisterConfigs(d.mapSubAgents(ctx, d.req.SubAgents))
		extra = append(extra,
			subagent.NewSpawnTool(mgr),
			subagent.NewParallelSpawnTool(mgr),
			subagent.NewDelegateTool(mgr),
			subagent.NewListTasksTool(mgr),
			subagent.NewCancelTaskTool(mgr),
			subagent.NewCollectTaskTool(mgr),
		)
	}

	// Knowledge-base retrieval tool (only when knowledge bases are configured).
	if len(d.req.KnowledgeBases) > 0 {
		extra = append(extra, retriever.CreateRetrievalTool(mapKnowledgeBases(d.req.KnowledgeBases)))
	}
	// Uploaded-file retrieval tool, scoped to the working directory.
	if len(d.req.Files) > 0 {
		extra = append(extra, retriever.CreateFileRetrievalTool(d.workDir))
	}

	// Skill tools (skill execution, load_skill, orchestrate_skills, create_skill).
	extra = append(extra, d.buildSkillTools(ctx)...)

	names := append([]string{}, builtinNames...)
	for _, t := range extra {
		if info, err := t.Info(ctx); err == nil && info != nil {
			names = append(names, info.Name)
		}
	}
	return extra, names
}

// mapSubAgents converts the trimmed types.SubAgentConfig into the subagent
// package's richer config, defaulting the ID to the name when unset.
func (d *Dispatcher) mapSubAgents(ctx context.Context, in []types.SubAgentConfig) []subagent.SubAgentConfig {
	out := make([]subagent.SubAgentConfig, 0, len(in))
	for _, c := range in {
		id := c.ID
		if id == "" {
			id = c.Name
		}
		runtimeTools := tools.ToolsByNamesWithBasePath(d.workDir, c.Tools)
		runtimeTools = append(runtimeTools, d.buildSubAgentSkillTools(ctx, c.Skills)...)
		var modelConfig *subagent.ModelConfig
		if c.Model != nil {
			modelConfig = &subagent.ModelConfig{
				Provider: c.Model.Provider, Name: c.Model.Name, APIKey: c.Model.APIKey, APIBase: c.Model.APIBase,
				Temperature: c.Model.Temperature, MaxTokens: c.Model.MaxTokens, TopP: c.Model.TopP,
			}
		}
		out = append(out, subagent.SubAgentConfig{
			ID:            id,
			Name:          c.Name,
			Description:   c.Description,
			Prompt:        c.Prompt,
			Model:         modelConfig,
			Tools:         c.Tools,
			Skills:        subAgentSkillNames(c.Skills),
			RuntimeTools:  runtimeTools,
			MaxIterations: c.MaxIterations,
			TimeoutMs:     c.TimeoutMs,
		})
	}
	return out
}

func (d *Dispatcher) buildSubAgentSkillTools(ctx context.Context, skills []types.Skill) []tool.BaseTool {
	if len(skills) == 0 {
		return nil
	}
	configPath := d.cfg.SkillsConfigPath
	if configPath == "" {
		configPath = plugins.DefaultSkillConfigPath()
	}
	configMgr, err := plugins.NewSkillConfigManager(configPath)
	if err != nil {
		log.Warnf("[Dispatcher] sub-agent skills: failed to load skill config: %v", err)
		configMgr, _ = plugins.NewSkillConfigManager("")
	}
	runner := plugins.NewSkillRunner(skills, d.skillsDir, d.sandboxConfig(), d.client.Model(), configMgr, d.workDir)
	var out []tool.BaseTool
	if skillTool := runner.BuildSkillTool(); skillTool != nil {
		if info, _ := skillTool.Info(ctx); info != nil {
			out = append(out, skillTool)
		}
	}
	if loadTool := runner.BuildLoadSkillTool(); loadTool != nil {
		if info, _ := loadTool.Info(ctx); info != nil {
			out = append(out, loadTool)
		}
	}
	return out
}

func subAgentSkillNames(skills []types.Skill) []string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		if skill.Name != "" {
			names = append(names, skill.Name)
		}
	}
	return names
}

func mapKnowledgeBases(in []types.KnowledgeBaseConfig) []retriever.KnowledgeBaseConfig {
	out := make([]retriever.KnowledgeBaseConfig, 0, len(in))
	for _, c := range in {
		out = append(out, retriever.KnowledgeBaseConfig{
			ID:           c.ID,
			Name:         c.Name,
			RetrievalURL: c.RetrievalURL,
			Token:        c.Token,
			TopK:         c.TopK,
		})
	}
	return out
}
