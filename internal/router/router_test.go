package router

import (
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/capability"
	"github.com/good-fish-man/agent-runtime/internal/intent"
)

func TestRouteDirectBrowserInteraction(t *testing.T) {
	plan := RouteIntent(intent.Parse(intent.Request{Text: "Open youtub home page and play the second vido"}))
	if plan.Primary != RouteBrowser || plan.Reason != "direct_browser_interaction" {
		t.Fatalf("unexpected route: %+v", plan)
	}
	for _, wanted := range []string{capability.BrowserTask, capability.BrowserObserve, capability.BrowserAction} {
		if !plan.UsesCapability(wanted) {
			t.Fatalf("route missing %s: %+v", wanted, plan)
		}
	}
	for _, unwanted := range []string{capability.InternetSearch, capability.InternetFetch, capability.BrowserSearch, capability.BrowserOpen, capability.BrowserNavigate, capability.BrowserRead, capability.DesktopAction} {
		if plan.UsesCapability(unwanted) {
			t.Fatalf("direct browser route included %s: %+v", unwanted, plan)
		}
	}
}

func TestRouteResearch(t *testing.T) {
	plan := RouteIntent(intent.Parse(intent.Request{Text: "帮我查询今天的科技新闻"}))
	if plan.Primary != RouteResearch || plan.Reason != "external_knowledge_required" {
		t.Fatalf("unexpected route: %+v", plan)
	}
	for _, wanted := range []string{capability.InternetSearch, capability.InternetFetch} {
		if !plan.UsesCapability(wanted) {
			t.Fatalf("research route missing %s: %+v", wanted, plan)
		}
	}
	for _, unwanted := range allBrowserCapabilities() {
		if plan.UsesCapability(unwanted) {
			t.Fatalf("research route can control the local browser through %s: %+v", unwanted, plan)
		}
	}
}

func TestRouteEnglishInvestigation(t *testing.T) {
	plan := RouteIntent(intent.Parse(intent.Request{Text: "Investigate Model Context Protocol architecture and cite reliable official and independent sources."}))
	if plan.Primary != RouteResearch || plan.Reason != "external_knowledge_required" {
		t.Fatalf("unexpected route: %+v", plan)
	}
}

func TestRouteResearchWinsOverAmbiguousImplementationKeywords(t *testing.T) {
	plan := RouteIntent(intent.Parse(intent.Request{Text: "请深入研究 MCP 协议的官方架构、安全边界和主要 SDK，并比较官方文档与 GitHub 实现；使用多个可靠来源并给出出处。"}))
	if plan.Primary != RouteResearch || plan.Reason != "external_knowledge_required" {
		t.Fatalf("research request was captured by broad workspace keywords: %+v", plan)
	}
	if !plan.UsesCapability(capability.InternetSearch) || !plan.UsesCapability(capability.InternetFetch) {
		t.Fatalf("research route lacks internet capabilities: %+v", plan)
	}
	if len(plan.Fallbacks) != 1 || plan.Fallbacks[0] != RouteFile {
		t.Fatalf("mixed research request lost safe fallbacks: %+v", plan)
	}
}

func TestRouteOfficialProcedureDoesNotUseActiveBrowser(t *testing.T) {
	plan := RouteIntent(intent.Parse(intent.Request{
		Text:                 "想切换驾照，我应该怎么做",
		ActiveBrowserSession: true,
		PreviousUserMessages: []string{"Open YouTube and play a music video"},
	}))
	if plan.Primary != RouteResearch || plan.Reason != "external_knowledge_required" {
		t.Fatalf("unexpected route: %+v", plan)
	}
	for _, unwanted := range append(allBrowserCapabilities(), capability.DesktopAction) {
		if plan.UsesCapability(unwanted) {
			t.Fatalf("procedure research exposed device capability %s: %+v", unwanted, plan)
		}
	}
}

func TestRouteDetailedOfficialProcedureKeepsSingleResearchRoute(t *testing.T) {
	plan := RouteIntent(intent.Parse(intent.Request{
		Text:                 "我是中国人，在日本工作，想把中国驾照换成日本驾照，我应该怎么做",
		ActiveBrowserSession: true,
		PreviousUserMessages: []string{"Open YouTube and play a music video"},
	}))
	if plan.Primary != RouteResearch || plan.Reason != "external_knowledge_required" {
		t.Fatalf("procedure did not keep one research route: %+v", plan)
	}
	if !plan.UsesCapability(capability.InternetSearch) || !plan.UsesCapability(capability.InternetFetch) {
		t.Fatalf("procedure route lacks research capabilities: %+v", plan)
	}
	for _, unwanted := range append(allBrowserCapabilities(), capability.DesktopAction) {
		if plan.UsesCapability(unwanted) {
			t.Fatalf("procedure route exposed unrelated capability %s: %+v", unwanted, plan)
		}
	}
}

func TestRouteWorkspaceOperation(t *testing.T) {
	plan := RouteIntent(intent.Parse(intent.Request{Text: "帮我修改 main.go 并运行测试"}))
	if plan.Primary != RouteFile {
		t.Fatalf("unexpected route: %+v", plan)
	}
	for _, wanted := range []string{capability.FilesystemRead, capability.FilesystemEdit, capability.SystemShell} {
		if !plan.UsesCapability(wanted) {
			t.Fatalf("file route missing %s: %+v", wanted, plan)
		}
	}
}

func TestRouteGenericOpenTargetUsesDesktopFallback(t *testing.T) {
	plan := RouteIntent(intent.Parse(intent.Request{Text: "帮我打开 Acme Portal"}))
	if plan.Primary != RouteBrowser || len(plan.Fallbacks) != 1 || plan.Fallbacks[0] != RouteDesktop {
		t.Fatalf("unexpected generic target route: %+v", plan)
	}
	if !plan.UsesCapability(capability.BrowserTask) || !plan.UsesCapability(capability.DesktopAction) {
		t.Fatalf("generic target route lacks fallback capabilities: %+v", plan)
	}
}

func TestRouteScheduledOperationBeforeResearch(t *testing.T) {
	plan := RouteIntent(intent.Parse(intent.Request{Text: "每五分钟查询一次演唱会余票"}))
	if plan.Primary != RouteAutomation || !plan.UsesCapability(capability.AutomationSchedule) {
		t.Fatalf("unexpected scheduled route: %+v", plan)
	}
}

func TestRoutePersistentGoal(t *testing.T) {
	plan := RouteIntent(intent.Parse(intent.Request{Text: "Create a persistent goal that can resume after restart"}))
	if plan.Primary != RouteOrchestration || plan.Reason != "persistent_goal_requested" || !plan.UsesCapability(capability.OrchestrationGoal) {
		t.Fatalf("unexpected persistent goal route: %+v", plan)
	}
}

func TestRouteActiveBrowserFollowUp(t *testing.T) {
	plan := RouteIntent(intent.Parse(intent.Request{Text: "play the second one", ActiveBrowserSession: true}))
	if plan.Primary != RouteBrowser || plan.Reason != "direct_browser_interaction" {
		t.Fatalf("unexpected active browser route: %+v", plan)
	}
}

func TestRouteContextualMediaTitle(t *testing.T) {
	plan := RouteIntent(intent.Parse(intent.Request{
		Text:                 "Adele Hello",
		ActiveBrowserSession: true,
		PreviousUserMessages: []string{"Open YouTube and play a music video"},
	}))
	if plan.Primary != RouteBrowser || plan.Reason != "direct_browser_interaction" || !plan.UsesCapability(capability.BrowserTask) {
		t.Fatalf("unexpected contextual media route: %+v", plan)
	}
}
