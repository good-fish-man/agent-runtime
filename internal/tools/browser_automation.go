package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
)

const BrowserAutomationToolName = "BrowserAutomation"

type BrowserAutomationInput struct {
	Operation               string `json:"operation"`
	SessionID               string `json:"session_id,omitempty"`
	AutomationID            string `json:"automation_id,omitempty"`
	TriggerType             string `json:"trigger_type,omitempty"`
	TriggerRole             string `json:"trigger_role,omitempty"`
	TriggerName             string `json:"trigger_name,omitempty"`
	TriggerKind             string `json:"trigger_kind,omitempty"`
	TriggerURLContains      string `json:"trigger_url_contains,omitempty"`
	ActionType              string `json:"action_type,omitempty"`
	ActionRole              string `json:"action_role,omitempty"`
	ActionName              string `json:"action_name,omitempty"`
	ActionKind              string `json:"action_kind,omitempty"`
	ActionURLContains       string `json:"action_url_contains,omitempty"`
	ActionValue             string `json:"action_value,omitempty"`
	ActionURL               string `json:"action_url,omitempty"`
	VerificationType        string `json:"verification_type,omitempty"`
	VerificationName        string `json:"verification_name,omitempty"`
	VerificationRole        string `json:"verification_role,omitempty"`
	VerificationKind        string `json:"verification_kind,omitempty"`
	VerificationURLContains string `json:"verification_url_contains,omitempty"`
	CooldownMS              int    `json:"cooldown_ms,omitempty"`
}

type BrowserAutomationTool struct{}

func NewBrowserAutomationTool() *BrowserAutomationTool { return &BrowserAutomationTool{} }

func init() {
	GlobalRegistry.Register(ToolMeta{
		Name:       BrowserAutomationToolName,
		Desc:       "Create and manage safe event-driven browser watch rules on an existing Athena browser session.",
		IsReadOnly: false, MaxResultChars: maxBrowserSnapshot, DefaultRisk: "medium",
		Creator: func(string) interface{} { return NewBrowserAutomationTool() },
	})
}

func (t *BrowserAutomationTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: BrowserAutomationToolName,
		Desc: "Manage event-driven browser automation without repeatedly calling the model. Use create for safe reversible rules such as clicking a visible Skip Ad button or reacting when media ends; use list/get/pause/resume/delete to manage existing rules. Purchases, messages, credentials, account changes, downloads, and destructive actions are not allowed in unattended rules.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"operation":                 {Type: schema.String, Desc: "create, list, get, pause, resume, or delete", Required: true},
			"session_id":                {Type: schema.String, Desc: "Existing browser session; required for create and optional for filtered list", Required: false},
			"automation_id":             {Type: schema.String, Desc: "Rule ID; required for get, pause, resume, and delete", Required: false},
			"trigger_type":              {Type: schema.String, Desc: "page_loaded, page_changed, element_appeared, element_disappeared, video_started, video_paused, video_ended, or login_required", Required: false},
			"trigger_name":              {Type: schema.String, Desc: "Optional semantic trigger element name, such as Skip Ad", Required: false},
			"trigger_role":              {Type: schema.String, Desc: "Optional semantic trigger role", Required: false},
			"trigger_kind":              {Type: schema.String, Desc: "Optional trigger entity kind", Required: false},
			"trigger_url_contains":      {Type: schema.String, Desc: "Optional URL fragment limiting the trigger", Required: false},
			"action_type":               {Type: schema.String, Desc: "click, play, press, scroll, or navigate", Required: false},
			"action_name":               {Type: schema.String, Desc: "Semantic target name for click or play", Required: false},
			"action_role":               {Type: schema.String, Desc: "Optional semantic action target role", Required: false},
			"action_kind":               {Type: schema.String, Desc: "Optional semantic action target kind", Required: false},
			"action_value":              {Type: schema.String, Desc: "Allowed key for press or up/down for scroll", Required: false},
			"action_url":                {Type: schema.String, Desc: "Exact HTTP(S) URL for navigate", Required: false},
			"verification_type":         {Type: schema.String, Desc: "Required for click/play/navigate: element_appeared, element_disappeared, page_changed, url_contains, video_started, or media_playing", Required: false},
			"verification_name":         {Type: schema.String, Desc: "Semantic element name used by appeared/disappeared verification", Required: false},
			"verification_role":         {Type: schema.String, Desc: "Optional semantic role used by verification", Required: false},
			"verification_kind":         {Type: schema.String, Desc: "Optional semantic kind used by verification", Required: false},
			"verification_url_contains": {Type: schema.String, Desc: "Required URL fragment for url_contains verification", Required: false},
			"cooldown_ms":               {Type: schema.Integer, Desc: "Rule cooldown from 500 to 300000 milliseconds", Required: false},
		}),
	}, nil
}

func (t *BrowserAutomationTool) ValidateInput(_ context.Context, input string) *ValidationResult {
	var in BrowserAutomationInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid JSON: %v", err), ErrorCode: 1}
	}
	in.Operation = strings.ToLower(strings.TrimSpace(in.Operation))
	switch in.Operation {
	case "create":
		actionType := strings.ToLower(strings.TrimSpace(in.ActionType))
		verificationType := strings.ToLower(strings.TrimSpace(in.VerificationType))
		if err := validateBrowserSessionID(in.SessionID); err != nil {
			return &ValidationResult{Valid: false, Message: err.Error(), ErrorCode: 2}
		}
		if !allowedBrowserAutomationTrigger(in.TriggerType) {
			return &ValidationResult{Valid: false, Message: "unsupported trigger_type", ErrorCode: 3}
		}
		if !allowedBrowserAutomationAction(actionType) {
			return &ValidationResult{Valid: false, Message: "unsupported action_type", ErrorCode: 4}
		}
		if (actionType == "click" || actionType == "play") && strings.TrimSpace(in.ActionName+in.ActionRole+in.ActionKind+in.ActionURLContains) == "" {
			return &ValidationResult{Valid: false, Message: actionType + " requires a semantic action target", ErrorCode: 5}
		}
		if !allowedBrowserAutomationVerification(verificationType) {
			return &ValidationResult{Valid: false, Message: "unsupported verification_type", ErrorCode: 10}
		}
		if (actionType == "click" || actionType == "play" || actionType == "navigate") && verificationType == "" {
			return &ValidationResult{Valid: false, Message: actionType + " requires verification_type", ErrorCode: 11}
		}
		if (verificationType == "element_appeared" || verificationType == "element_disappeared") &&
			strings.TrimSpace(in.VerificationName+in.VerificationRole+in.VerificationKind+in.VerificationURLContains) == "" {
			return &ValidationResult{Valid: false, Message: verificationType + " requires a semantic verification target", ErrorCode: 12}
		}
		if verificationType == "url_contains" && strings.TrimSpace(in.VerificationURLContains) == "" {
			return &ValidationResult{Valid: false, Message: "url_contains requires verification_url_contains", ErrorCode: 13}
		}
		if in.CooldownMS != 0 && (in.CooldownMS < 500 || in.CooldownMS > 300000) {
			return &ValidationResult{Valid: false, Message: "cooldown_ms must be between 500 and 300000", ErrorCode: 6}
		}
	case "list":
		if strings.TrimSpace(in.SessionID) != "" {
			if err := validateBrowserSessionID(in.SessionID); err != nil {
				return &ValidationResult{Valid: false, Message: err.Error(), ErrorCode: 7}
			}
		}
	case "get", "pause", "resume", "delete":
		if !strings.HasPrefix(strings.TrimSpace(in.AutomationID), "automation-") {
			return &ValidationResult{Valid: false, Message: "automation_id is required", ErrorCode: 8}
		}
	default:
		return &ValidationResult{Valid: false, Message: "operation must be create, list, get, pause, resume, or delete", ErrorCode: 9}
	}
	return &ValidationResult{Valid: true}
}

func (t *BrowserAutomationTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var in BrowserAutomationInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if validation := t.ValidateInput(ctx, input); !validation.Valid {
		return "", fmt.Errorf("invalid browser automation request: %s", validation.Message)
	}
	arguments := map[string]any{
		"operation": in.Operation, "automation_id": strings.TrimSpace(in.AutomationID),
		"trigger_type": strings.ToLower(strings.TrimSpace(in.TriggerType)), "trigger_role": strings.TrimSpace(in.TriggerRole),
		"trigger_name": strings.TrimSpace(in.TriggerName), "trigger_kind": strings.TrimSpace(in.TriggerKind),
		"trigger_url_contains": strings.TrimSpace(in.TriggerURLContains),
		"action_type":          strings.ToLower(strings.TrimSpace(in.ActionType)), "action_role": strings.TrimSpace(in.ActionRole),
		"action_name": strings.TrimSpace(in.ActionName), "action_kind": strings.TrimSpace(in.ActionKind),
		"action_url_contains": strings.TrimSpace(in.ActionURLContains), "action_value": strings.TrimSpace(in.ActionValue),
		"action_url": strings.TrimSpace(in.ActionURL), "verification_type": strings.ToLower(strings.TrimSpace(in.VerificationType)),
		"verification_name": strings.TrimSpace(in.VerificationName), "verification_role": strings.TrimSpace(in.VerificationRole),
		"verification_kind": strings.TrimSpace(in.VerificationKind), "verification_url_contains": strings.TrimSpace(in.VerificationURLContains),
		"cooldown_ms": in.CooldownMS,
	}
	risk := actionprotocol.RiskMedium
	if in.Operation == "list" || in.Operation == "get" {
		risk = actionprotocol.RiskLow
	}
	return browserClientRequest(ctx, strings.TrimSpace(in.SessionID), "automation", arguments, risk, actionprotocol.Allow, false,
		"Managing an event-driven browser watch rule on the user's device.")
}

func allowedBrowserAutomationTrigger(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "page_loaded", "page_changed", "element_appeared", "element_disappeared", "video_started", "video_paused", "video_ended", "login_required":
		return true
	}
	return false
}

func allowedBrowserAutomationAction(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "click", "play", "press", "scroll", "navigate":
		return true
	}
	return false
}

func allowedBrowserAutomationVerification(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "element_appeared", "element_disappeared", "page_changed", "url_contains", "video_started", "media_playing":
		return true
	}
	return false
}
