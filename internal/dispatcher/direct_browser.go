package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/intent"
	athenarouter "github.com/good-fish-man/agent-runtime/internal/router"
	"github.com/good-fish-man/agent-runtime/internal/tools"
	log "github.com/good-fish-man/logx"
)

// dispatchDirectBrowserAction turns high-confidence browser commands into a
// device action before model inference. The model still plans ambiguous or
// research-oriented requests, but opening and controlling a page never depends
// on a small model correctly choosing a tool.
func (d *Dispatcher) dispatchDirectBrowserAction(ctx context.Context, userPrompt string, emit func(actionprotocol.Action) error) (*eino.Result, bool, error) {
	if emit == nil || d.routePlan.Primary != athenarouter.RouteBrowser ||
		!d.routePlan.Intent.HasSignal(intent.SignalDirectBrowserControl) ||
		d.routePlan.Intent.Confidence < 0.95 || len([]rune(strings.TrimSpace(userPrompt))) > 1200 {
		return nil, false, nil
	}

	input, err := json.Marshal(tools.BrowserTaskInput{
		SessionID:            strings.TrimSpace(d.contextString("active_browser_session")),
		Goal:                 strings.TrimSpace(userPrompt),
		ContextualMediaTitle: d.routePlan.Intent.HasSignal(intent.SignalContextualMediaTitle),
	})
	if err != nil {
		return nil, true, log.WrapError(err, "dispatcher.directBrowserAction.encode")
	}
	payload, err := tools.NewBrowserTaskTool().InvokableRun(ctx, string(input))
	if err != nil {
		return nil, true, log.WrapError(err, "dispatcher.directBrowserAction.build")
	}
	action, ok := actionprotocol.Parse(payload)
	if !ok {
		return nil, true, fmt.Errorf("dispatcher direct browser action returned an invalid protocol payload")
	}
	if err := emit(action); err != nil {
		return nil, true, log.WrapError(err, "dispatcher.directBrowserAction.emit")
	}
	log.InfowCtx(ctx, "direct browser action dispatched",
		"capability", action.Capability,
		"session_id", action.SessionID,
		"route_reason", d.routePlan.Reason,
	)
	return &eino.Result{FinishReason: "client_action", ActionCount: 1}, true, nil
}

// completeDeviceObservation converts a terminal browser-task observation into
// user-visible text. Suggested actions remain in the Observation event for the
// frontend to render as stable, directly executable buttons.
func (d *Dispatcher) completeDeviceObservation(userPrompt string, emit func(eino.StreamChunk) error) (*eino.Result, bool, error) {
	observation, latestAction, ok := deviceObservationPayload(d.req.Context, userPrompt)
	if !ok {
		return nil, false, nil
	}
	capabilityName, _ := latestAction["capability"].(string)
	state, _ := mapValue(observation["state"])
	_, hasBrowserTask := mapValue(state["browser_task"])
	if !strings.HasPrefix(strings.TrimSpace(capabilityName), "browser.") && !hasBrowserTask {
		return nil, false, nil
	}

	status := strings.ToUpper(strings.TrimSpace(stringValue(observation["status"])))
	plan, hasPlan := mapValue(state["browser_task"])
	completed, _ := plan["completed"].(bool)
	_, continuationRequired := mapValue(state["continuation_required"])
	terminalFailure := status != "" && status != "SUCCEEDED"
	if !terminalFailure && (!hasPlan || (!completed && !continuationRequired)) {
		return nil, false, nil
	}

	content := browserObservationMessage(d.contextString("locale"), status, observation, state, plan)
	if strings.TrimSpace(content) == "" {
		return nil, false, nil
	}
	if emit != nil {
		if err := emit(eino.StreamChunk{Text: content}); err != nil {
			return nil, true, log.WrapError(err, "dispatcher.completeDeviceObservation.emit")
		}
	}
	return &eino.Result{Content: content, FinishReason: "stop"}, true, nil
}

func deviceObservationPayload(values map[string]any, prompt string) (map[string]any, map[string]any, bool) {
	if observation, ok := contextMap(values, "latest_action_observation"); ok {
		latestAction, _ := contextMap(values, "latest_action")
		return observation, latestAction, true
	}

	const marker = "Observation payload:\n```json\n"
	start := strings.LastIndex(prompt, marker)
	if start < 0 {
		return nil, nil, false
	}
	payload := prompt[start+len(marker):]
	if end := strings.LastIndex(payload, "\n```"); end >= 0 {
		payload = payload[:end]
	}
	var envelope map[string]any
	if json.Unmarshal([]byte(payload), &envelope) != nil {
		return nil, nil, false
	}
	observation, ok := mapValue(envelope["observation"])
	if !ok {
		return nil, nil, false
	}
	latestAction, _ := mapValue(envelope["action"])
	return observation, latestAction, true
}

func browserObservationMessage(locale, status string, observation, state, plan map[string]any) string {
	zh := strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "zh")
	if status != "SUCCEEDED" {
		message := strings.TrimSpace(stringValue(observation["error"]))
		if message == "" {
			message = strings.TrimSpace(stringValue(plan["message"]))
		}
		if message == "" {
			message = "browser action did not complete"
		}
		if zh {
			return "浏览器操作未完成：" + message
		}
		return "The browser action did not complete: " + message
	}

	title := strings.TrimSpace(stringValue(state["title"]))
	pageURL := strings.TrimSpace(stringValue(state["url"]))
	message := strings.TrimSpace(stringValue(plan["message"]))
	if playback, ok := mapValue(state["playback"]); ok {
		if playing, _ := playback["playing"].(bool); playing {
			if zh {
				message = "媒体已开始播放。"
			} else {
				message = "Media playback is active."
			}
		}
	}
	if message == "" {
		if zh {
			message = "浏览器任务已完成。"
		} else {
			message = "Browser task completed."
		}
	}

	page := title
	if page == "" {
		page = pageURL
	}
	if page == "" {
		return message
	}
	if pageURL != "" && title != "" {
		page = fmt.Sprintf("[%s](%s)", title, pageURL)
	}
	if zh {
		return message + "\n\n当前页面：" + page
	}
	return message + "\n\nCurrent page: " + page
}

func contextMap(values map[string]any, key string) (map[string]any, bool) {
	if values == nil {
		return nil, false
	}
	return mapValue(values[key])
}

func mapValue(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	default:
		return nil, false
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
