package router

import (
	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/intent"
)

func RouteIntent(parsed intent.Intent) RoutePlan {
	decision := (Policy{}).Decide(parsed)
	plan := RoutePlan{
		Primary:   decision.Primary,
		Fallbacks: append([]Route(nil), decision.Fallbacks...),
		Reason:    decision.Reason,
		Intent:    parsed,
	}
	seen := make(map[string]bool)
	add := func(names ...string) {
		for _, name := range names {
			if name != "" && !seen[name] {
				plan.Capabilities = append(plan.Capabilities, name)
				seen[name] = true
			}
		}
	}

	add(capability.InteractionAsk)
	if parsed.HasSignal(intent.SignalLocalDeviceFile) {
		add(capability.DesktopAction)
	} else {
		if parsed.HasSignal(intent.SignalUploadedFile) || parsed.HasSignal(intent.SignalWorkspaceRead) {
			add(capability.FilesystemList, capability.FilesystemSearch, capability.FilesystemRead)
		}
		if parsed.HasSignal(intent.SignalWorkspaceWrite) {
			add(capability.FilesystemList, capability.FilesystemSearch, capability.FilesystemRead, capability.FilesystemEdit, capability.FilesystemWrite)
		}
	}
	if parsed.HasSignal(intent.SignalCommand) {
		add(capability.SystemShell)
	}

	directBrowser := parsed.HasSignal(intent.SignalDirectBrowserControl)
	if directBrowser {
		addBrowserTaskCapabilities(add)
	} else if parsed.HasSignal(intent.SignalOpenTarget) && !parsed.HasSignal(intent.SignalExplicitDesktop) && !parsed.HasSignal(intent.SignalWebAccess) {
		addBrowserExecutionCapabilities(add)
	}
	if parsed.HasSignal(intent.SignalWebAccess) && !directBrowser {
		add(capability.InternetSearch, capability.InternetFetch)
	}
	if parsed.HasSignal(intent.SignalBrowserAuthentication) {
		add(capability.BrowserSearch, capability.BrowserTask, capability.BrowserNavigate, capability.BrowserLogin, capability.BrowserRead, capability.BrowserObserve, capability.BrowserAction, capability.BrowserPointer, capability.BrowserWait, capability.BrowserScreenshot, capability.BrowserClose)
	}
	if parsed.HasSignal(intent.SignalBrowserDownload) {
		add(capability.BrowserTask, capability.BrowserOpen, capability.BrowserObserve, capability.BrowserAction, capability.BrowserDownload)
	}
	if parsed.HasSignal(intent.SignalBrowserScreenshot) {
		add(capability.BrowserObserve, capability.BrowserAction, capability.BrowserPointer, capability.BrowserScreenshot)
	}
	if parsed.HasSignal(intent.SignalBrowserClose) {
		add(capability.BrowserClose)
	}
	if parsed.HasSignal(intent.SignalExplicitDesktop) || (parsed.HasSignal(intent.SignalOpenTarget) && !directBrowser && !parsed.HasSignal(intent.SignalWebAccess)) {
		add(capability.DesktopAction)
	}
	if parsed.HasSignal(intent.SignalPlanning) {
		add(capability.PlanningTodo, capability.PlanningEnter, capability.PlanningExit)
	}
	if parsed.HasSignal(intent.SignalTaskManagement) {
		add(capability.TaskCreate, capability.TaskGet, capability.TaskList, capability.TaskUpdate)
	}
	if parsed.HasSignal(intent.SignalWait) {
		add(capability.SystemWait)
	}
	if parsed.HasSignal(intent.SignalScheduled) {
		add(capability.AutomationSchedule)
	}
	if parsed.HasSignal(intent.SignalPersistentGoal) {
		add(capability.OrchestrationGoal)
	}
	if directBrowser {
		plan.ExcludedCapabilities = []string{
			capability.InternetSearch,
			capability.InternetFetch,
			capability.BrowserSearch,
			capability.BrowserOpen,
			capability.BrowserNavigate,
			capability.BrowserRead,
			capability.DesktopAction,
		}
	} else if plan.Primary == RouteResearch {
		// Research runs on server-side search/fetch. An open desktop session must
		// never turn an informational request into a visible device action.
		plan.ExcludedCapabilities = append(allBrowserCapabilities(), capability.DesktopAction)
	} else if plan.Primary != RouteBrowser {
		// Configured capabilities describe what an agent may use, not what every
		// turn should expose. Browser tools are available only to browser routes.
		plan.ExcludedCapabilities = allBrowserCapabilities()
	}
	return plan
}

func addBrowserTaskCapabilities(add func(...string)) {
	add(capability.BrowserTask, capability.BrowserObserve, capability.BrowserAction, capability.BrowserPointer, capability.BrowserAutomation, capability.BrowserWait, capability.BrowserScreenshot)
}

func addBrowserExecutionCapabilities(add func(...string)) {
	add(capability.BrowserTask, capability.BrowserOpen, capability.BrowserNavigate, capability.BrowserRead, capability.BrowserObserve, capability.BrowserAction, capability.BrowserPointer, capability.BrowserAutomation, capability.BrowserWait, capability.BrowserScreenshot)
}

func allBrowserCapabilities() []string {
	return capability.BrowserIDs()
}
