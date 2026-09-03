package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
)

const DesktopActionToolName = "DesktopAction"

var desktopSessionIDPattern = regexp.MustCompile(`^desktop-[a-f0-9]{24}$`)

type DesktopActionInput struct {
	Action        string   `json:"action"`
	Query         string   `json:"query,omitempty"`
	Mode          string   `json:"mode,omitempty"`
	Extensions    []string `json:"extensions,omitempty"`
	MaxResults    int      `json:"max_results,omitempty"`
	IncludeHidden bool     `json:"include_hidden,omitempty"`
	Application   string   `json:"application,omitempty"`
	SessionID     string   `json:"session_id,omitempty"`
	Value         string   `json:"value,omitempty"`
}

type DesktopActionTool struct{}

func NewDesktopActionTool() *DesktopActionTool { return &DesktopActionTool{} }

func init() {
	GlobalRegistry.Register(ToolMeta{
		Name: DesktopActionToolName, Desc: "Request a user-authorized action from the Athena desktop app.",
		IsReadOnly: false, MaxResultChars: 20000, DefaultRisk: "medium",
		Creator: func(string) interface{} { return NewDesktopActionTool() },
	})
}

func (t *DesktopActionTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: DesktopActionToolName,
		Desc: "Ask the user's Athena desktop app to search authorized folders or control an installed application through a persistent desktop session. Never pass commands, paths, or process IDs.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action":         {Type: schema.String, Desc: "search_files, open_application, observe, activate, press, type_text, or close_application", Required: true},
			"query":          {Type: schema.String, Desc: "File name or content text to find", Required: false},
			"mode":           {Type: schema.String, Desc: "name, content, or both", Required: false},
			"extensions":     {Type: schema.Array, ElemInfo: &schema.ParameterInfo{Type: schema.String}, Desc: "Optional file extensions", Required: false},
			"max_results":    {Type: schema.Integer, Desc: "Maximum file results, up to 200", Required: false},
			"include_hidden": {Type: schema.Boolean, Desc: "Include hidden files and folders", Required: false},
			"application":    {Type: schema.String, Desc: "Exact installed application name", Required: false},
			"session_id":     {Type: schema.String, Desc: "Desktop session returned by open_application", Required: false},
			"value":          {Type: schema.String, Desc: "Key name for press or text for type_text", Required: false},
		}),
	}, nil
}

func (t *DesktopActionTool) ValidateInput(_ context.Context, input string) *ValidationResult {
	var in DesktopActionInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid JSON: %v", err), ErrorCode: 1}
	}
	switch in.Action {
	case "search_files":
		if strings.TrimSpace(in.Query) == "" {
			return &ValidationResult{Valid: false, Message: "query is required for search_files", ErrorCode: 2}
		}
		if in.Mode != "" && in.Mode != "name" && in.Mode != "content" && in.Mode != "both" {
			return &ValidationResult{Valid: false, Message: "mode must be name, content, or both", ErrorCode: 3}
		}
	case "open_application":
		if strings.TrimSpace(in.Application) == "" {
			return &ValidationResult{Valid: false, Message: "application is required for open_application", ErrorCode: 4}
		}
	case "observe", "activate", "close_application":
		if !desktopSessionIDPattern.MatchString(strings.TrimSpace(in.SessionID)) {
			return &ValidationResult{Valid: false, Message: "a valid desktop session_id is required", ErrorCode: 5}
		}
	case "press", "type_text":
		if !desktopSessionIDPattern.MatchString(strings.TrimSpace(in.SessionID)) {
			return &ValidationResult{Valid: false, Message: "a valid desktop session_id is required", ErrorCode: 6}
		}
		if strings.TrimSpace(in.Value) == "" {
			return &ValidationResult{Valid: false, Message: "value is required", ErrorCode: 7}
		}
	default:
		return &ValidationResult{Valid: false, Message: "unsupported desktop action", ErrorCode: 8}
	}
	return &ValidationResult{Valid: true}
}

func (t *DesktopActionTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var in DesktopActionInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if validation := t.ValidateInput(ctx, input); !validation.Valid {
		return "", fmt.Errorf("invalid desktop action: %s", validation.Message)
	}
	in.Query = strings.TrimSpace(in.Query)
	in.Application = strings.TrimSpace(in.Application)
	in.SessionID = strings.TrimSpace(in.SessionID)
	if in.Action == "open_application" {
		in.SessionID = "desktop-" + strings.TrimPrefix(desktopRequestID(), "desktop-")
	}
	risk, decision := actionprotocol.RiskLow, actionprotocol.Allow
	if in.Action == "open_application" {
		risk = actionprotocol.RiskMedium
	}
	if in.Action == "press" || in.Action == "type_text" || in.Action == "close_application" {
		risk, decision = actionprotocol.RiskMedium, actionprotocol.AskUser
	}
	capability := map[string]string{
		"search_files": "file.search", "open_application": "app.open", "observe": "app.observe",
		"activate": "app.activate", "press": "app.press", "type_text": "app.type", "close_application": "app.close",
	}[in.Action]
	arguments := map[string]any{
		"query": in.Query, "mode": in.Mode, "extensions": in.Extensions, "max_results": in.MaxResults,
		"include_hidden": in.IncludeHidden, "application": in.Application, "value": in.Value,
	}
	return actionprotocol.Marshal(actionprotocol.New(ctx, capability, in.SessionID, arguments, risk, decision))
}

func desktopRequestID() string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "desktop-request"
	}
	return "desktop-" + hex.EncodeToString(bytes[:])
}
