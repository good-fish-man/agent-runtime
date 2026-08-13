package dispatcher

import (
	"context"

	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/plugins"
	"github.com/good-fish-man/agent-runtime/internal/retriever"
	athenarouter "github.com/good-fish-man/agent-runtime/internal/router"
	"github.com/good-fish-man/agent-runtime/internal/subagent"
	"github.com/good-fish-man/agent-runtime/internal/tools"
	"github.com/good-fish-man/agent-runtime/internal/types"
	log "github.com/good-fish-man/logx"

	"github.com/cloudwego/eino/components/tool"
)

// buildTools resolves selected capabilities into their internal providers.
func (d *Dispatcher) buildTools(ctx context.Context, plan athenarouter.RoutePlan) ([]tool.BaseTool, []string) {
	capabilityIDs := append([]string(nil), plan.Capabilities...)
	if d.contextString("active_desktop_session") != "" && !containsToolName(capabilityIDs, capability.DesktopAction) {
		capabilityIDs = append(capabilityIDs, capability.DesktopAction)
	}
	for _, configured := range d.req.Capabilities {
		if configured.ID != "" && !containsToolName(capabilityIDs, configured.ID) {
			capabilityIDs = append(capabilityIDs, configured.ID)
		}
	}
	if len(plan.ExcludedCapabilities) > 0 {
		capabilityIDs = withoutToolNames(capabilityIDs, plan.ExcludedCapabilities...)
	}
	if d.isBackgroundMonitor() {
		capabilityIDs = readOnlyMonitorCapabilities(capabilityIDs)
	}
	if !d.contextBool("desktop_bridge") {
		capabilityIDs = withoutToolNames(capabilityIDs, capability.DesktopAction)
	}
	if !d.contextBool("browser_controller") {
		capabilityIDs = withoutToolNames(capabilityIDs,
			capability.BrowserSearch, capability.BrowserTask, capability.BrowserOpen, capability.BrowserLogin, capability.BrowserRead,
			capability.BrowserNavigate, capability.BrowserObserve, capability.BrowserAction, capability.BrowserClose,
		)
	}
	staticCapabilityIDs := withoutToolNames(capabilityIDs, capability.AutomationSchedule, capability.ImageGenerate, capability.VideoGenerate)
	extra, unavailable, err := capability.GlobalRegistry.Resolve(d.workDir, staticCapabilityIDs)
	if err != nil {
		log.WarnwCtx(ctx, "capability resolution failed", "error", err)
	}
	if len(unavailable) > 0 {
		log.WarnwCtx(ctx, "capabilities unavailable", "capabilities", unavailable)
	}
	if containsToolName(capabilityIDs, capability.AutomationSchedule) && !d.isBackgroundMonitor() {
		extra = append(extra, d.wrapDynamicCapability(capability.AutomationSchedule,
			tools.NewScheduledTaskCreateTool(d.contextString("user_id"), d.contextString("agent_id"), d.contextString("session_id"), d.contextString("timezone"))))
	}
	if d.isBackgroundMonitor() {
		return extra, availableCapabilityIDs(capabilityIDs)
	}
	if imageModel, ok := d.req.Models["image"]; ok && imageModel.Name != "" {
		extra = append(extra, d.wrapDynamicCapability(capability.ImageGenerate, tools.NewImageGenerationTool(imageModel)))
		capabilityIDs = append(capabilityIDs, capability.ImageGenerate)
	}
	if videoModel, ok := d.req.Models["video"]; ok && videoModel.Name != "" {
		extra = append(extra, d.wrapDynamicCapability(capability.VideoGenerate, tools.NewVideoGenerationTool(videoModel)))
		capabilityIDs = append(capabilityIDs, capability.VideoGenerate)
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

	names := availableCapabilityIDs(capabilityIDs)
	for _, t := range extra {
		if info, err := t.Info(ctx); err == nil && info != nil {
			name := info.Name
			if definition, ok := capability.GlobalRegistry.FindByModelName(info.Name); ok {
				name = definition.ID
			}
			if !containsToolName(names, name) {
				names = append(names, name)
			}
		}
	}
	return extra, names
}

func (d *Dispatcher) contextString(key string) string {
	if d.req.Context == nil {
		return ""
	}
	value, _ := d.req.Context[key].(string)
	return value
}

func (d *Dispatcher) contextBool(key string) bool {
	if d.req.Context == nil {
		return false
	}
	value, _ := d.req.Context[key].(bool)
	return value
}

func withoutToolNames(names []string, removed ...string) []string {
	remove := make(map[string]bool, len(removed))
	for _, name := range removed {
		remove[name] = true
	}
	result := make([]string, 0, len(names))
	for _, name := range names {
		if !remove[name] {
			result = append(result, name)
		}
	}
	return result
}

func withoutBaseTools(ctx context.Context, values []tool.BaseTool, removed ...string) []tool.BaseTool {
	remove := make(map[string]bool, len(removed))
	for _, name := range removed {
		remove[name] = true
	}
	result := make([]tool.BaseTool, 0, len(values))
	for _, value := range values {
		info, err := value.Info(ctx)
		if err == nil && info != nil && remove[info.Name] {
			continue
		}
		result = append(result, value)
	}
	return result
}

func availableCapabilityIDs(ids []string) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		definition, ok := capability.GlobalRegistry.Get(id)
		if ok && definition.Status == capability.StatusAvailable {
			result = append(result, id)
		}
	}
	return result
}

func (d *Dispatcher) wrapDynamicCapability(id string, provider tool.BaseTool) tool.BaseTool {
	definition, ok := capability.GlobalRegistry.Get(id)
	if !ok {
		return provider
	}
	return capability.Wrap(definition, provider)
}

func (d *Dispatcher) isBackgroundMonitor() bool {
	if d.req.Context == nil {
		return false
	}
	value, _ := d.req.Context["background_monitor"].(bool)
	return value
}
func containsToolName(names []string, wanted string) bool {
	for _, name := range names {
		if name == wanted {
			return true
		}
	}
	return false
}
func readOnlyMonitorCapabilities(names []string) []string {
	allowed := map[string]bool{
		capability.InternetSearch: true,
		capability.InternetFetch:  true,
		capability.BrowserSearch:  true,
		capability.BrowserRead:    true,
		capability.BrowserObserve: true,
		capability.BrowserClose:   true,
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if allowed[name] {
			out = append(out, name)
		}
	}
	if !containsToolName(out, capability.InternetSearch) {
		out = append(out, capability.InternetSearch)
	}
	if !containsToolName(out, capability.InternetFetch) {
		out = append(out, capability.InternetFetch)
	}
	return out
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
		runtimeTools, unavailable, _ := capability.GlobalRegistry.Resolve(d.workDir, c.Capabilities)
		if len(unavailable) > 0 {
			log.WarnwCtx(ctx, "sub-agent capabilities unavailable", "sub_agent", id, "capabilities", unavailable)
		}
		runtimeTools = append(runtimeTools, d.buildSubAgentSkillTools(ctx, c.Skills)...)
		var modelConfig *subagent.ModelConfig
		if c.Model != nil {
			modelConfig = &subagent.ModelConfig{
				Provider: c.Model.Provider, Name: c.Model.Name, APIKey: c.Model.APIKey, APIBase: c.Model.APIBase,
				Temperature: c.Model.Temperature, MaxTokens: c.Model.MaxTokens, TopP: c.Model.TopP, ExtraFields: c.Model.ExtraFields,
			}
		}
		out = append(out, subagent.SubAgentConfig{
			ID:            id,
			Name:          c.Name,
			Description:   c.Description,
			Prompt:        c.Prompt,
			Model:         modelConfig,
			Capabilities:  append([]string{}, c.Capabilities...),
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
