package router

import "github.com/good-fish-man/agent-runtime/internal/intent"

type Route string

const (
	RouteConversation Route = "conversation"
	RouteResearch     Route = "research"
	RouteBrowser      Route = "browser"
	RouteFile         Route = "file"
	RouteDesktop      Route = "desktop"
	RouteAutomation   Route = "automation"
	RoutePlanning     Route = "planning"
	RouteTask         Route = "task"
)

type RoutePlan struct {
	Primary              Route         `json:"primary"`
	Capabilities         []string      `json:"capabilities"`
	Fallbacks            []Route       `json:"fallbacks,omitempty"`
	ExcludedCapabilities []string      `json:"excluded_capabilities,omitempty"`
	Reason               string        `json:"reason"`
	Intent               intent.Intent `json:"intent"`
}

func (p RoutePlan) UsesCapability(wanted string) bool {
	for _, capability := range p.Capabilities {
		if capability == wanted {
			return true
		}
	}
	return false
}
