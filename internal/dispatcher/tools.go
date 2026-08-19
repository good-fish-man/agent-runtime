package dispatcher

import (
	"context"

	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/retriever"
	athenarouter "github.com/good-fish-man/agent-runtime/internal/router"
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
	if d.contextBool("persistent_goal_execution") {
		capabilityIDs = withoutToolNames(capabilityIDs, capability.AutomationSchedule, capability.OrchestrationGoal)
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
	staticCapabilityIDs := withoutToolNames(capabilityIDs, capability.AutomationSchedule, capability.OrchestrationGoal, capability.ImageGenerate, capability.VideoGenerate)
	extra, unavailable, err := capability.GlobalRegistry.Resolve(d.workDir, staticCapabilityIDs)
	if err != nil {
		log.WarnwCtx(ctx, "capability resolution failed", "error", err)
	}
	if len(unavailable) > 0 {
		log.WarnwCtx(ctx, "capabilities unavailable", "capabilities", unavailable)
	}
	if containsToolName(capabilityIDs, capability.AutomationSchedule) && !d.isBackgroundMonitor() && !d.contextBool("persistent_goal_execution") {
		extra = append(extra, d.wrapDynamicCapability(capability.AutomationSchedule,
			tools.NewScheduledTaskCreateTool(d.contextString("user_id"), d.contextString("agent_id"), d.contextString("session_id"), d.contextString("timezone"))))
	}
	if containsToolName(capabilityIDs, capability.OrchestrationGoal) && !d.isBackgroundMonitor() && !d.contextBool("persistent_goal_execution") {
		extra = append(extra, d.wrapDynamicCapability(capability.OrchestrationGoal,
			tools.NewPersistentGoalCreateTool(d.contextString("user_id"), d.contextString("agent_id"), d.contextString("session_id"))))
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
