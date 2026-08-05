package tools

import (
	"context"
	"strings"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
)

func browserClientRequest(ctx context.Context, sessionID, action string, arguments map[string]any, risk, decision string, takeover bool, message string) (string, error) {
	capability := "browser." + strings.TrimSpace(action)
	if action == "extract" {
		capability = "browser.observe"
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	if takeover {
		arguments["user_takeover"] = true
	}
	if message != "" {
		arguments["message"] = message
	}
	return actionprotocol.Marshal(actionprotocol.New(ctx, capability, sessionID, arguments, risk, decision))
}
