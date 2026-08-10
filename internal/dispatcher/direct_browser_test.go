package dispatcher

import (
	"context"
	"strings"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
	"github.com/good-fish-man/agent-runtime/internal/eino"
	"github.com/good-fish-man/agent-runtime/internal/intent"
	athenarouter "github.com/good-fish-man/agent-runtime/internal/router"
	"github.com/good-fish-man/agent-runtime/internal/types"
)

func TestDispatchDirectBrowserActionBypassesModel(t *testing.T) {
	d := &Dispatcher{
		req: &types.RunRequest{Context: map[string]any{"active_browser_session": "athena-11111111111111111111111111111111"}},
		routePlan: athenarouter.RoutePlan{
			Primary: athenarouter.RouteBrowser,
			Reason:  "direct_browser_interaction",
			Intent: intent.Intent{
				Confidence: 0.98,
				Signals:    []intent.Signal{intent.SignalDirectBrowserControl},
			},
		},
	}
	var emitted actionprotocol.Action
	result, handled, err := d.dispatchDirectBrowserAction(
		actionprotocol.WithScope(context.Background(), "request-1"),
		"Open YouTube home page",
		func(action actionprotocol.Action) error { emitted = action; return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result == nil || result.ActionCount != 1 || result.FinishReason != "client_action" {
		t.Fatalf("unexpected result: handled=%v result=%+v", handled, result)
	}
	if emitted.Capability != "browser.task" || emitted.SessionID != "athena-11111111111111111111111111111111" {
		t.Fatalf("unexpected action: %+v", emitted)
	}
	if emitted.Arguments["goal"] != "Open YouTube home page" {
		t.Fatalf("goal = %#v", emitted.Arguments["goal"])
	}
}

func TestDispatchContextualMediaTitleMarksBrowserTask(t *testing.T) {
	d := &Dispatcher{
		req: &types.RunRequest{Context: map[string]any{"active_browser_session": "athena-11111111111111111111111111111111"}},
		routePlan: athenarouter.RoutePlan{
			Primary: athenarouter.RouteBrowser,
			Reason:  "direct_browser_interaction",
			Intent: intent.Intent{
				Confidence: 0.98,
				Signals:    []intent.Signal{intent.SignalDirectBrowserControl, intent.SignalContextualMediaTitle},
			},
		},
	}
	var emitted actionprotocol.Action
	_, handled, err := d.dispatchDirectBrowserAction(
		actionprotocol.WithScope(context.Background(), "request-2"),
		"Adele Hello",
		func(action actionprotocol.Action) error { emitted = action; return nil },
	)
	if err != nil || !handled {
		t.Fatalf("dispatch failed: handled=%v err=%v", handled, err)
	}
	if contextual, _ := emitted.Arguments["contextual_media_title"].(bool); !contextual {
		t.Fatalf("contextual media marker missing: %+v", emitted.Arguments)
	}
}

func TestCompleteDeviceObservationKeepsSuggestedActionsInObservation(t *testing.T) {
	d := &Dispatcher{req: &types.RunRequest{Context: map[string]any{
		"locale":        "en",
		"latest_action": map[string]any{"capability": "browser.task"},
		"latest_action_observation": map[string]any{
			"status": "SUCCEEDED",
			"state": map[string]any{
				"title": "YouTube",
				"url":   "https://www.youtube.com/",
				"browser_task": map[string]any{
					"completed": true,
					"message":   "Browser task completed.",
				},
				"suggested_actions": []any{map[string]any{"id": "option-1"}},
			},
		},
	}}}
	var visible string
	result, handled, err := d.completeDeviceObservation("", func(chunk eino.StreamChunk) error {
		visible += chunk.Text
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result == nil || !strings.Contains(visible, "[YouTube](https://www.youtube.com/)") {
		t.Fatalf("unexpected completion: handled=%v result=%+v visible=%q", handled, result, visible)
	}
	state := d.req.Context["latest_action_observation"].(map[string]any)["state"].(map[string]any)
	if len(state["suggested_actions"].([]any)) != 1 {
		t.Fatalf("suggested actions were mutated: %#v", state["suggested_actions"])
	}
}

func TestCompleteDeviceObservationStopsForRequiredPageSelection(t *testing.T) {
	d := &Dispatcher{req: &types.RunRequest{Context: map[string]any{
		"locale":        "en",
		"latest_action": map[string]any{"capability": "browser.task"},
		"latest_action_observation": map[string]any{
			"status": "SUCCEEDED",
			"state": map[string]any{
				"title": "Netflix",
				"url":   "https://www.netflix.com/browse",
				"browser_task": map[string]any{
					"completed": false,
					"message":   "Choose a visible profile or account to continue this browser task.",
				},
				"continuation_required": map[string]any{
					"kind":   "page_selection",
					"reason": "visible_choice_required",
				},
				"suggested_actions": []any{map[string]any{"id": "profile-yufu"}},
			},
		},
	}}}
	var visible string
	result, handled, err := d.completeDeviceObservation("", func(chunk eino.StreamChunk) error {
		visible += chunk.Text
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result == nil || result.FinishReason != "stop" {
		t.Fatalf("required page selection did not stop the model loop: handled=%v result=%+v", handled, result)
	}
	if !strings.Contains(visible, "Choose a visible profile") {
		t.Fatalf("visible completion = %q", visible)
	}
}

func TestCompleteDeviceObservationReportsFailure(t *testing.T) {
	d := &Dispatcher{req: &types.RunRequest{Context: map[string]any{
		"locale":        "zh-CN",
		"latest_action": map[string]any{"capability": "browser.task"},
		"latest_action_observation": map[string]any{
			"status": "FAILED",
			"error":  "playback did not start",
			"state":  map[string]any{},
		},
	}}}
	result, handled, err := d.completeDeviceObservation("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result == nil || result.Content != "浏览器操作未完成：playback did not start" {
		t.Fatalf("unexpected failure completion: handled=%v result=%+v", handled, result)
	}
}

func TestCompleteDeviceObservationRecoversFromPrompt(t *testing.T) {
	d := &Dispatcher{req: &types.RunRequest{}}
	prompt := "A real Athena Desktop/Browser action has finished.\n\nObservation payload:\n```json\n" +
		`{"action":{"capability":"browser.task"},"observation":{"status":"SUCCEEDED","state":{"url":"https://www.youtube.com/","title":"YouTube","browser_task":{"completed":true,"message":"Browser task completed."}}}}` +
		"\n```"
	result, handled, err := d.completeDeviceObservation(prompt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result == nil || !strings.Contains(result.Content, "YouTube") {
		t.Fatalf("unexpected recovered completion: handled=%v result=%+v", handled, result)
	}
}
