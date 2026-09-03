package tools

import (
	"context"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
	"github.com/good-fish-man/agent-runtime/internal/effectspec"
	semantics "github.com/good-fish-man/athena-protocol/draft/v0alpha"
)

const (
	BrowserSearchToolName   = "BrowserSearch"
	BrowserTaskToolName     = "BrowserTask"
	BrowserOpenToolName     = "BrowserOpen"
	BrowserNavigateToolName = "BrowserNavigate"
	BrowserObserveToolName  = "BrowserObserve"
	BrowserActionToolName   = "BrowserAction"
	maxBrowserSnapshot      = 80000
)

var browserRefPattern = regexp.MustCompile(`^@e[0-9]+$`)
var browserSnapshotURLPattern = regexp.MustCompile(`https?://[^\s\]\)"']+`)

type BrowserSearchInput struct {
	Query  string `json:"query"`
	Engine string `json:"engine,omitempty"`
	Count  int    `json:"count,omitempty"`
}

type BrowserOpenInput struct {
	Target string `json:"target"`
}

type BrowserTaskInput struct {
	SessionID            string `json:"session_id,omitempty"`
	Goal                 string `json:"goal"`
	Target               string `json:"target,omitempty"`
	Query                string `json:"query,omitempty"`
	ContextualMediaTitle bool   `json:"contextual_media_title,omitempty"`
}

type BrowserNavigateInput struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
}

type BrowserSearchOutput struct {
	Status    string         `json:"status"`
	SessionID string         `json:"session_id,omitempty"`
	Query     string         `json:"query"`
	Engine    string         `json:"engine,omitempty"`
	URL       string         `json:"url,omitempty"`
	Snapshot  string         `json:"snapshot,omitempty"`
	Results   []SearchResult `json:"results,omitempty"`
	Message   string         `json:"message,omitempty"`
}

type BrowserActionInput struct {
	SessionID string `json:"session_id"`
	Action    string `json:"action"`
	Ref       string `json:"ref,omitempty"`
	TargetRef string `json:"target_ref,omitempty"`
	Value     string `json:"value,omitempty"`
}

type BrowserObserveInput struct {
	SessionID string `json:"session_id"`
}

type BrowserActionOutput struct {
	Status    string `json:"status"`
	SessionID string `json:"session_id"`
	URL       string `json:"url,omitempty"`
	Snapshot  string `json:"snapshot,omitempty"`
	Message   string `json:"message,omitempty"`
}

type BrowserSearchTool struct{}
type BrowserTaskTool struct{}
type BrowserOpenTool struct{}
type BrowserNavigateTool struct{}
type BrowserObserveTool struct{}
type BrowserActionTool struct{}

func NewBrowserSearchTool() *BrowserSearchTool     { return &BrowserSearchTool{} }
func NewBrowserTaskTool() *BrowserTaskTool         { return &BrowserTaskTool{} }
func NewBrowserOpenTool() *BrowserOpenTool         { return &BrowserOpenTool{} }
func NewBrowserNavigateTool() *BrowserNavigateTool { return &BrowserNavigateTool{} }
func NewBrowserObserveTool() *BrowserObserveTool   { return &BrowserObserveTool{} }
func NewBrowserActionTool() *BrowserActionTool     { return &BrowserActionTool{} }

func init() {
	GlobalRegistry.Register(ToolMeta{
		Name: BrowserSearchToolName, Desc: "Search the web in a real browser and return result links with an ongoing browser session.",
		IsReadOnly: true, MaxResultChars: maxBrowserSnapshot, DefaultRisk: "low",
		Creator: func(string) interface{} { return NewBrowserSearchTool() },
	})
	GlobalRegistry.Register(ToolMeta{
		Name: BrowserTaskToolName, Desc: "Ask the local Browser System to execute a reversible browser task such as opening a site, searching within a site, or opening a matching result, then return structured observations.",
		IsReadOnly: false, MaxResultChars: maxBrowserSnapshot, DefaultRisk: "medium",
		Creator: func(string) interface{} { return NewBrowserTaskTool() },
	})
	GlobalRegistry.Register(ToolMeta{
		Name: BrowserOpenToolName, Desc: "Open a website in the visible, controllable Athena browser, reusing the existing browser window and creating or switching tabs when needed.",
		IsReadOnly: false, MaxResultChars: maxBrowserSnapshot, DefaultRisk: "medium",
		Creator: func(string) interface{} { return NewBrowserOpenTool() },
	})
	GlobalRegistry.Register(ToolMeta{
		Name: BrowserNavigateToolName, Desc: "Navigate an existing browser session to an exact HTTP(S) URL and return the updated observation.",
		IsReadOnly: false, MaxResultChars: maxBrowserSnapshot, DefaultRisk: "medium",
		Creator: func(string) interface{} { return NewBrowserNavigateTool() },
	})
	GlobalRegistry.Register(ToolMeta{
		Name: BrowserObserveToolName, Desc: "Observe the current URL, title, visible text, and semantic element snapshot for an existing browser session.",
		IsReadOnly: true, MaxResultChars: maxBrowserSnapshot, DefaultRisk: "low",
		Creator: func(string) interface{} { return NewBrowserObserveTool() },
	})
	GlobalRegistry.Register(ToolMeta{
		Name: BrowserActionToolName, Desc: "Perform a limited navigation action in an existing public browser session and return the updated page snapshot.",
		IsReadOnly: false, MaxResultChars: maxBrowserSnapshot, DefaultRisk: "medium",
		Creator: func(string) interface{} { return NewBrowserActionTool() },
	})
}

func (t *BrowserTaskTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: BrowserTaskToolName,
		Desc: "Execute a reversible user-visible browser task through Athena Browser System. Prefer this for commands like opening YouTube, searching inside the current site, opening the first suitable result, or continuing the current page. The Browser System owns tabs, DOM refs, retries, and Observation; do not use it for purchases, bookings, messages, account changes, credentials, or verification codes.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"goal":       {Type: schema.String, Desc: "Natural-language browser task to complete", Required: true},
			"session_id": {Type: schema.String, Desc: "Existing browser session ID when continuing a page", Required: false},
			"target":     {Type: schema.String, Desc: "Optional site, URL, or service name", Required: false},
			"query":      {Type: schema.String, Desc: "Optional search text for the site or page", Required: false},
		}),
	}, nil
}

func (t *BrowserTaskTool) ValidateInput(_ context.Context, input string) *ValidationResult {
	var in BrowserTaskInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid JSON: %v", err), ErrorCode: 1}
	}
	goal := strings.TrimSpace(in.Goal)
	if goal == "" {
		return &ValidationResult{Valid: false, Message: "goal is required", ErrorCode: 2}
	}
	if len([]rune(goal)) > 1200 {
		return &ValidationResult{Valid: false, Message: "goal is too long", ErrorCode: 3}
	}
	if strings.TrimSpace(in.SessionID) != "" {
		if err := validateBrowserSessionID(in.SessionID); err != nil {
			return &ValidationResult{Valid: false, Message: err.Error(), ErrorCode: 4}
		}
	}
	if len([]rune(in.Target)) > 500 || len([]rune(in.Query)) > 500 {
		return &ValidationResult{Valid: false, Message: "target/query is too long", ErrorCode: 5}
	}
	return &ValidationResult{Valid: true}
}

func (t *BrowserTaskTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var in BrowserTaskInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if validation := t.ValidateInput(ctx, input); !validation.Valid {
		return "", fmt.Errorf("invalid browser task: %s", validation.Message)
	}
	sessionID := strings.TrimSpace(in.SessionID)
	if sessionID == "" {
		var err error
		sessionID, err = newBrowserSessionID()
		if err != nil {
			return "", fmt.Errorf("create browser task session: %w", err)
		}
	}
	arguments := map[string]any{
		"goal": strings.TrimSpace(in.Goal), "target": strings.TrimSpace(in.Target), "query": strings.TrimSpace(in.Query),
		"contextual_media_title": in.ContextualMediaTitle,
		"headed":                 true, "snapshot": true,
	}
	trace := effectspec.NewBrowserTrace(in.Goal, in.Target, in.Query, sessionID)
	if err := semantics.PutTrace(arguments, trace); err != nil {
		return "", fmt.Errorf("attach browser outcome semantics: %w", err)
	}
	return browserClientRequest(ctx, sessionID, "task", arguments,
		actionprotocol.RiskMedium, actionprotocol.Allow, false, "Executing a reversible browser task on the user's device.")
}

func (t *BrowserOpenTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: BrowserOpenToolName,
		Desc: "Open an exact HTTP(S) URL in the user's visible Athena browser. Reuses the current browser window and opens or switches a tab. Resolve website names through Search System or use BrowserTask before calling this capability.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"target": {Type: schema.String, Desc: "Exact absolute HTTP(S) URL", Required: true},
		}),
	}, nil
}

func (t *BrowserOpenTool) ValidateInput(_ context.Context, input string) *ValidationResult {
	var in BrowserOpenInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid JSON: %v", err), ErrorCode: 1}
	}
	target := strings.TrimSpace(in.Target)
	if target == "" {
		return &ValidationResult{Valid: false, Message: "target is required", ErrorCode: 2}
	}
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return &ValidationResult{Valid: false, Message: "target must be an exact absolute HTTP(S) URL without credentials; resolve names through Search System", ErrorCode: 3}
	}
	return &ValidationResult{Valid: true}
}

func (t *BrowserOpenTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var in BrowserOpenInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if validation := t.ValidateInput(ctx, input); !validation.Valid {
		return "", fmt.Errorf("invalid browser open request: %s", validation.Message)
	}
	target := strings.TrimSpace(in.Target)
	sessionID, err := newBrowserSessionID()
	if err != nil {
		return "", fmt.Errorf("create browser session: %w", err)
	}
	return browserClientRequest(ctx, sessionID, "open", map[string]any{
		"url": target, "target": target, "headed": true, "snapshot": true,
	}, actionprotocol.RiskMedium, actionprotocol.Allow, false, "Opening a visible, controllable browser session on the user's device.")
}

func (t *BrowserNavigateTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: BrowserNavigateToolName,
		Desc: "Navigate an existing Athena browser session to an exact HTTP(S) URL and return the updated page observation. Use browser.open when there is no current session.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"session_id": {Type: schema.String, Desc: "Existing browser session ID", Required: true},
			"url":        {Type: schema.String, Desc: "Exact absolute HTTP(S) URL without credentials", Required: true},
		}),
	}, nil
}

func (t *BrowserNavigateTool) ValidateInput(_ context.Context, input string) *ValidationResult {
	var in BrowserNavigateInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid JSON: %v", err), ErrorCode: 1}
	}
	if err := validateBrowserSessionID(in.SessionID); err != nil {
		return &ValidationResult{Valid: false, Message: err.Error(), ErrorCode: 2}
	}
	if _, err := validateBrowserURL(in.URL); err != nil {
		return &ValidationResult{Valid: false, Message: err.Error(), ErrorCode: 3}
	}
	return &ValidationResult{Valid: true}
}

func (t *BrowserNavigateTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var in BrowserNavigateInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if validation := t.ValidateInput(ctx, input); !validation.Valid {
		return "", fmt.Errorf("invalid browser navigate request: %s", validation.Message)
	}
	parsed, err := validateBrowserURL(in.URL)
	if err != nil {
		return "", err
	}
	return browserClientRequest(ctx, in.SessionID, "navigate", map[string]any{
		"url": parsed.String(), "snapshot": true,
	}, actionprotocol.RiskMedium, actionprotocol.Allow, false, "Navigating the existing user-side browser session.")
}

func (t *BrowserSearchTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: BrowserSearchToolName,
		Desc: "Use a real browser to discover URLs when WebSearch is blocked, empty, or when the task needs browser navigation. Defaults to Google and automatically falls back to Bing. Returns a session_id and an accessibility snapshot containing links.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query":  {Type: schema.String, Desc: "Focused search query including dates or location when relevant", Required: true},
			"engine": {Type: schema.String, Desc: "google or bing (default google)", Required: false},
			"count":  {Type: schema.Integer, Desc: "Desired result count, 1-10", Required: false},
		}),
	}, nil
}

func (t *BrowserSearchTool) ValidateInput(_ context.Context, input string) *ValidationResult {
	var in BrowserSearchInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid JSON: %v", err), ErrorCode: 1}
	}
	if strings.TrimSpace(in.Query) == "" {
		return &ValidationResult{Valid: false, Message: "query is required", ErrorCode: 2}
	}
	if in.Engine != "" && in.Engine != "google" && in.Engine != "bing" {
		return &ValidationResult{Valid: false, Message: "engine must be google or bing", ErrorCode: 3}
	}
	return &ValidationResult{Valid: true}
}

func (t *BrowserSearchTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var in BrowserSearchInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	in.Query = strings.TrimSpace(in.Query)
	if validation := t.ValidateInput(ctx, input); !validation.Valid {
		return "", fmt.Errorf("invalid browser search: %s", validation.Message)
	}
	count := in.Count
	if count <= 0 || count > 10 {
		count = 8
	}
	engine := in.Engine
	if engine == "" {
		engine = "google"
	}
	sessionID, err := newBrowserSessionID()
	if err != nil {
		return "", fmt.Errorf("create browser search session: %w", err)
	}
	searchURL := publicSearchURL(engine, in.Query, count)
	return browserClientRequest(ctx, sessionID, "navigate", map[string]any{
		"url": searchURL, "query": in.Query, "engine": engine, "snapshot": true,
	}, actionprotocol.RiskLow, actionprotocol.Allow, false, "Opening search results in the user-side browser controller.")
}

func (t *BrowserObserveTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: BrowserObserveToolName,
		Desc: "Observe the current page in an existing Athena browser session. Use after browser.open, browser.action, login takeover, or when a later user command should continue from the current page.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"session_id": {Type: schema.String, Desc: "Session returned by BrowserOpen, BrowserSearch, or BrowserLogin", Required: true},
		}),
	}, nil
}

func (t *BrowserObserveTool) ValidateInput(_ context.Context, input string) *ValidationResult {
	var in BrowserObserveInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid JSON: %v", err), ErrorCode: 1}
	}
	if err := validateBrowserSessionID(in.SessionID); err != nil {
		return &ValidationResult{Valid: false, Message: err.Error(), ErrorCode: 2}
	}
	return &ValidationResult{Valid: true}
}

func (t *BrowserObserveTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var in BrowserObserveInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if validation := t.ValidateInput(ctx, input); !validation.Valid {
		return "", fmt.Errorf("invalid browser observe request: %s", validation.Message)
	}
	return browserClientRequest(ctx, in.SessionID, "extract", map[string]any{
		"snapshot": true,
	}, actionprotocol.RiskLow, actionprotocol.Allow, false, "Observing the current user-side browser session.")
}

func (t *BrowserActionTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: BrowserActionToolName,
		Desc: "Control an existing browser session using refs from its latest snapshot. Supports click, play, pause, type, hover, select, drag, press, scroll, back, forward, refresh, wait, screenshot, and explicit user-requested download, then returns an updated observation. Play and pause require the device to verify the resulting media state. Never use this tool to submit purchases, bookings, messages, account changes, deletion, consent, credentials, or verification codes.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"session_id": {Type: schema.String, Desc: "Session returned by BrowserOpen, BrowserSearch, BrowserObserve, or BrowserLogin", Required: true},
			"action":     {Type: schema.String, Desc: "click, play, pause, type, hover, select, drag, press, scroll, back, forward, refresh, wait, screenshot, or download", Required: true},
			"ref":        {Type: schema.String, Desc: "Accessibility ref such as @e12; required for element interactions", Required: false},
			"target_ref": {Type: schema.String, Desc: "Destination accessibility ref; required only for drag", Required: false},
			"value":      {Type: schema.String, Desc: "Text for type/select, allowed key for press, direction for scroll, milliseconds for wait, or filename for download", Required: false},
		}),
	}, nil
}

func (t *BrowserActionTool) ValidateInput(_ context.Context, input string) *ValidationResult {
	var in BrowserActionInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid JSON: %v", err), ErrorCode: 1}
	}
	if err := validateBrowserSessionID(in.SessionID); err != nil {
		return &ValidationResult{Valid: false, Message: err.Error(), ErrorCode: 2}
	}
	switch in.Action {
	case "play", "pause":
		// Playback is implemented and verified by the trusted client runtime;
		// no model-provided JavaScript or selector is accepted.
	case "click", "hover", "download":
		if !browserRefPattern.MatchString(in.Ref) {
			return &ValidationResult{Valid: false, Message: in.Action + " requires a snapshot ref such as @e12", ErrorCode: 3}
		}
	case "type", "select":
		if !browserRefPattern.MatchString(in.Ref) || strings.TrimSpace(in.Value) == "" {
			return &ValidationResult{Valid: false, Message: in.Action + " requires a snapshot ref and non-empty value", ErrorCode: 4}
		}
		if len([]rune(in.Value)) > 4000 {
			return &ValidationResult{Valid: false, Message: in.Action + " value is too long", ErrorCode: 5}
		}
	case "drag":
		if !browserRefPattern.MatchString(in.Ref) || !browserRefPattern.MatchString(in.TargetRef) || in.Ref == in.TargetRef {
			return &ValidationResult{Valid: false, Message: "drag requires different source and target snapshot refs", ErrorCode: 10}
		}
	case "press":
		if !allowedBrowserKey(in.Value) {
			return &ValidationResult{Valid: false, Message: "press key is not allowed", ErrorCode: 6}
		}
	case "scroll":
		if in.Value != "up" && in.Value != "down" && in.Value != "left" && in.Value != "right" {
			return &ValidationResult{Valid: false, Message: "scroll value must be up, down, left, or right", ErrorCode: 7}
		}
	case "wait":
		if _, err := strconv.Atoi(strings.TrimSpace(in.Value)); err != nil {
			return &ValidationResult{Valid: false, Message: "wait value must be milliseconds", ErrorCode: 8}
		}
	case "screenshot", "back", "forward", "refresh":
		// No extra input required.
	default:
		return &ValidationResult{Valid: false, Message: "unsupported browser action", ErrorCode: 9}
	}
	return &ValidationResult{Valid: true}
}

func (t *BrowserActionTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var in BrowserActionInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if validation := t.ValidateInput(ctx, input); !validation.Valid {
		return "", fmt.Errorf("invalid browser action: %s", validation.Message)
	}
	risk, decision := actionprotocol.RiskMedium, actionprotocol.Allow
	if in.Action == "play" || in.Action == "pause" || in.Action == "hover" || in.Action == "scroll" || in.Action == "back" || in.Action == "forward" || in.Action == "refresh" || in.Action == "wait" || in.Action == "screenshot" {
		risk, decision = actionprotocol.RiskLow, actionprotocol.Allow
	}
	if in.Action == "drag" {
		risk, decision = actionprotocol.RiskMedium, actionprotocol.AskUser
	}
	arguments := map[string]any{"ref": in.Ref, "target_ref": in.TargetRef, "value": in.Value, "snapshot": true}
	if in.Action == "download" && strings.TrimSpace(in.Value) != "" {
		arguments["filename"] = strings.TrimSpace(in.Value)
	}
	return browserClientRequest(ctx, in.SessionID, in.Action, arguments, risk, decision, false, "A browser interaction is ready for execution on the user's device.")
}

func publicSearchURL(engine, query string, count int) string {
	escaped := url.QueryEscape(query)
	if engine == "bing" {
		return "https://www.bing.com/search?q=" + escaped + "&count=" + strconv.Itoa(count)
	}
	return "https://www.google.com/search?q=" + escaped + "&num=" + strconv.Itoa(count)
}

func usefulSearchSnapshot(snapshot string) bool {
	lower := strings.ToLower(snapshot)
	return len(snapshot) > 100 && strings.Contains(lower, "http") &&
		!strings.Contains(lower, "unusual traffic") && !strings.Contains(lower, "verify you are human")
}

func extractBrowserSnapshotResults(snapshot string, limit int) []SearchResult {
	if limit <= 0 {
		limit = 8
	}
	results := make([]SearchResult, 0, limit)
	seen := make(map[string]bool)
	for _, line := range strings.Split(snapshot, "\n") {
		for _, match := range browserSnapshotURLPattern.FindAllString(line, -1) {
			candidate := strings.TrimRight(stdhtml.UnescapeString(match), ".,;")
			parsed, err := url.Parse(candidate)
			if err != nil || parsed.Hostname() == "" {
				continue
			}
			host := strings.ToLower(parsed.Hostname())
			if strings.Contains(host, "google.") {
				if target := parsed.Query().Get("q"); target != "" {
					candidate = target
					parsed, err = url.Parse(candidate)
					if err != nil {
						continue
					}
					host = strings.ToLower(parsed.Hostname())
				}
			}
			if strings.Contains(host, "google.") || strings.Contains(host, "bing.com") || strings.Contains(host, "microsoft.com") || seen[candidate] {
				continue
			}
			seen[candidate] = true
			title := strings.TrimSpace(strings.TrimPrefix(line, "-"))
			if index := strings.Index(title, " [ref="); index > 0 {
				title = strings.Trim(title[:index], " \"")
			}
			results = append(results, SearchResult{Title: title, URL: candidate})
			if len(results) >= limit {
				return results
			}
		}
	}
	return results
}

func allowedBrowserKey(value string) bool {
	switch value {
	case "Enter", "Escape", "Tab", "ArrowUp", "ArrowDown", "PageUp", "PageDown":
		return true
	}
	return false
}

func truncateString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n[truncated]"
}

func marshalBrowserSearch(output BrowserSearchOutput) (string, error) {
	data, err := json.Marshal(output)
	return string(data), err
}

func marshalBrowserAction(output BrowserActionOutput) (string, error) {
	data, err := json.Marshal(output)
	return string(data), err
}
