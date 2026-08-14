package router

import "github.com/good-fish-man/agent-runtime/internal/intent"

type Decision struct {
	Primary   Route
	Fallbacks []Route
	Reason    string
}

type Policy struct{}

func (Policy) Decide(parsed intent.Intent) Decision {
	switch {
	case parsed.HasSignal(intent.SignalDirectBrowserControl):
		return Decision{Primary: RouteBrowser, Reason: "direct_browser_interaction"}
	case parsed.HasSignal(intent.SignalBrowserAuthentication):
		return Decision{Primary: RouteBrowser, Reason: "authenticated_browser_interaction"}
	case parsed.HasSignal(intent.SignalBrowserDownload):
		return Decision{Primary: RouteBrowser, Reason: "browser_download"}
	case parsed.HasSignal(intent.SignalBrowserScreenshot):
		return Decision{Primary: RouteBrowser, Reason: "browser_screenshot"}
	case parsed.HasSignal(intent.SignalBrowserClose):
		return Decision{Primary: RouteBrowser, Reason: "browser_close"}
	case parsed.HasSignal(intent.SignalPersistentGoal):
		return Decision{Primary: RouteOrchestration, Reason: "persistent_goal_requested"}
	case parsed.HasSignal(intent.SignalScheduled):
		return Decision{Primary: RouteAutomation, Fallbacks: []Route{RouteResearch}, Reason: "scheduled_operation"}
	case parsed.HasSignal(intent.SignalLocalDeviceFile), parsed.HasSignal(intent.SignalUploadedFile):
		return Decision{Primary: RouteFile, Reason: "local_device_file_operation"}
	case parsed.HasSignal(intent.SignalExplicitDesktop):
		return Decision{Primary: RouteDesktop, Reason: "explicit_desktop_application"}
	case parsed.HasSignal(intent.SignalContextualResearch):
		return Decision{Primary: RouteResearch, Reason: "research_conversation_refinement"}
	case parsed.HasSignal(intent.SignalWebAccess):
		var fallbacks []Route
		if parsed.HasSignal(intent.SignalWorkspaceWrite) || parsed.HasSignal(intent.SignalWorkspaceRead) || parsed.HasSignal(intent.SignalCommand) {
			fallbacks = append(fallbacks, RouteFile)
		}
		return Decision{Primary: RouteResearch, Fallbacks: fallbacks, Reason: "external_knowledge_required"}
	case parsed.HasSignal(intent.SignalWorkspaceWrite), parsed.HasSignal(intent.SignalWorkspaceRead), parsed.HasSignal(intent.SignalCommand):
		return Decision{Primary: RouteFile, Reason: "workspace_operation"}
	case parsed.HasSignal(intent.SignalOpenTarget):
		return Decision{Primary: RouteBrowser, Fallbacks: []Route{RouteDesktop}, Reason: "open_target_prefers_browser"}
	case parsed.HasSignal(intent.SignalPlanning):
		return Decision{Primary: RoutePlanning, Reason: "planning_request"}
	case parsed.HasSignal(intent.SignalTaskManagement):
		return Decision{Primary: RouteTask, Reason: "task_management"}
	default:
		return Decision{Primary: RouteConversation, Reason: "conversation_default"}
	}
}
