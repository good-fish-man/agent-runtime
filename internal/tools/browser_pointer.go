package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
)

const BrowserPointerToolName = "BrowserPointer"

type BrowserPointerInput struct {
	SessionID       string   `json:"session_id"`
	Operation       string   `json:"operation"`
	GroundingID     string   `json:"grounding_id"`
	ScreenshotID    string   `json:"screenshot_id"`
	PageRevision    string   `json:"page_revision"`
	CoordinateSpace string   `json:"coordinate_space"`
	X               *float64 `json:"x"`
	Y               *float64 `json:"y"`
	TargetX         *float64 `json:"target_x,omitempty"`
	TargetY         *float64 `json:"target_y,omitempty"`
	Purpose         string   `json:"purpose"`
}

type BrowserPointerTool struct{}

func NewBrowserPointerTool() *BrowserPointerTool { return &BrowserPointerTool{} }

func init() {
	GlobalRegistry.Register(ToolMeta{
		Name:       BrowserPointerToolName,
		Desc:       "Use a short-lived screenshot grounding to control a visual-only browser surface through a bounded pointer action.",
		IsReadOnly: false, MaxResultChars: maxBrowserSnapshot, DefaultRisk: "medium",
		Creator: func(string) interface{} { return NewBrowserPointerTool() },
	})
}

func (t *BrowserPointerTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: BrowserPointerToolName,
		Desc: "Control Canvas, WebGL, video, or another visual-only page surface using a pointer_grounding returned by the latest browser screenshot. Use semantic browser.action refs whenever available. Never invent or reuse grounding IDs, screenshot IDs, page revisions, or coordinates from an older observation. Click and drag are verified by a new screenshot; consequential controls, credentials, challenges, and ordinary semantic DOM controls are rejected by the device runtime.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"session_id":       {Type: schema.String, Desc: "Existing browser session from the screenshot observation", Required: true},
			"operation":        {Type: schema.String, Desc: "move, click, or drag", Required: true},
			"grounding_id":     {Type: schema.String, Desc: "Exact short-lived grounding_id from pointer_grounding", Required: true},
			"screenshot_id":    {Type: schema.String, Desc: "Exact screenshot_id from pointer_grounding", Required: true},
			"page_revision":    {Type: schema.String, Desc: "Exact page_revision from pointer_grounding", Required: true},
			"coordinate_space": {Type: schema.String, Desc: "normalized_1000 or screenshot_pixels, matching pointer_grounding", Required: true},
			"x":                {Type: schema.Number, Desc: "Source/click X coordinate in the declared coordinate space", Required: true},
			"y":                {Type: schema.Number, Desc: "Source/click Y coordinate in the declared coordinate space", Required: true},
			"target_x":         {Type: schema.Number, Desc: "Drag destination X coordinate; required only for drag", Required: false},
			"target_y":         {Type: schema.Number, Desc: "Drag destination Y coordinate; required only for drag", Required: false},
			"purpose":          {Type: schema.String, Desc: "Short description of the visible target and intended effect", Required: true},
		}),
	}, nil
}

func (t *BrowserPointerTool) ValidateInput(_ context.Context, input string) *ValidationResult {
	var in BrowserPointerInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return &ValidationResult{Valid: false, Message: fmt.Sprintf("invalid JSON: %v", err), ErrorCode: 1}
	}
	if err := validateBrowserSessionID(in.SessionID); err != nil {
		return &ValidationResult{Valid: false, Message: err.Error(), ErrorCode: 2}
	}
	operation := strings.ToLower(strings.TrimSpace(in.Operation))
	if operation != "move" && operation != "click" && operation != "drag" {
		return &ValidationResult{Valid: false, Message: "operation must be move, click, or drag", ErrorCode: 3}
	}
	if !strings.HasPrefix(strings.TrimSpace(in.GroundingID), "pointer-grounding-") {
		return &ValidationResult{Valid: false, Message: "a pointer_grounding grounding_id is required", ErrorCode: 4}
	}
	if strings.TrimSpace(in.ScreenshotID) == "" || strings.TrimSpace(in.PageRevision) == "" {
		return &ValidationResult{Valid: false, Message: "screenshot_id and page_revision are required", ErrorCode: 5}
	}
	space := strings.ToLower(strings.TrimSpace(in.CoordinateSpace))
	if space != "normalized_1000" && space != "screenshot_pixels" {
		return &ValidationResult{Valid: false, Message: "coordinate_space must be normalized_1000 or screenshot_pixels", ErrorCode: 6}
	}
	if !validBrowserPointerCoordinate(in.X, space) || !validBrowserPointerCoordinate(in.Y, space) {
		return &ValidationResult{Valid: false, Message: "x and y must be finite coordinates inside the declared coordinate space", ErrorCode: 7}
	}
	if operation == "drag" {
		if !validBrowserPointerCoordinate(in.TargetX, space) || !validBrowserPointerCoordinate(in.TargetY, space) {
			return &ValidationResult{Valid: false, Message: "drag requires finite target_x and target_y coordinates", ErrorCode: 8}
		}
	} else if in.TargetX != nil || in.TargetY != nil {
		return &ValidationResult{Valid: false, Message: "target_x and target_y are only valid for drag", ErrorCode: 9}
	}
	purpose := strings.TrimSpace(in.Purpose)
	if purpose == "" || len([]rune(purpose)) > 300 {
		return &ValidationResult{Valid: false, Message: "purpose is required and must not exceed 300 characters", ErrorCode: 10}
	}
	return &ValidationResult{Valid: true}
}

func validBrowserPointerCoordinate(value *float64, space string) bool {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return false
	}
	if space == "normalized_1000" {
		return *value <= 1000
	}
	return *value <= 100000
}

func (t *BrowserPointerTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var in BrowserPointerInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if validation := t.ValidateInput(ctx, input); !validation.Valid {
		return "", fmt.Errorf("invalid browser pointer request: %s", validation.Message)
	}
	operation := strings.ToLower(strings.TrimSpace(in.Operation))
	arguments := map[string]any{
		"operation": operation, "grounding_id": strings.TrimSpace(in.GroundingID),
		"screenshot_id": strings.TrimSpace(in.ScreenshotID), "page_revision": strings.TrimSpace(in.PageRevision),
		"coordinate_space": strings.ToLower(strings.TrimSpace(in.CoordinateSpace)),
		"x":                *in.X, "y": *in.Y, "purpose": strings.TrimSpace(in.Purpose), "snapshot": true,
	}
	if operation == "drag" {
		arguments["target_x"], arguments["target_y"] = *in.TargetX, *in.TargetY
	}
	risk, decision := actionprotocol.RiskMedium, actionprotocol.Allow
	if operation == "move" {
		risk = actionprotocol.RiskLow
	}
	if operation == "drag" {
		decision = actionprotocol.AskUser
	}
	return browserClientRequest(ctx, strings.TrimSpace(in.SessionID), "pointer", arguments, risk, decision, false,
		"A screenshot-grounded browser pointer action is ready for execution on the user's device.")
}
