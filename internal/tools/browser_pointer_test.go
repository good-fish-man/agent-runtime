package tools

import (
	"context"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/actionprotocol"
)

func TestBrowserPointerEmitsGroundedCapabilityAction(t *testing.T) {
	output, err := NewBrowserPointerTool().InvokableRun(context.Background(), `{
		"session_id":"athena-0123456789abcdef0123456789abcdef",
		"operation":"click",
		"grounding_id":"pointer-grounding-0123456789abcdef",
		"screenshot_id":"image-0123456789abcdef",
		"page_revision":"page-revision-0123456789abcdef",
		"coordinate_space":"normalized_1000",
		"x":512,
		"y":384,
		"purpose":"Activate the unlabeled canvas play control"
	}`)
	if err != nil {
		t.Fatal(err)
	}
	action, ok := actionprotocol.Parse(output)
	if !ok || action.Capability != "browser.pointer" || action.SessionID == "" {
		t.Fatalf("action = %#v", action)
	}
	if action.Policy.Risk != actionprotocol.RiskMedium || action.Policy.Decision != actionprotocol.Allow {
		t.Fatalf("policy = %#v", action.Policy)
	}
	if action.Arguments["grounding_id"] != "pointer-grounding-0123456789abcdef" || action.Arguments["x"] != float64(512) {
		t.Fatalf("arguments = %#v", action.Arguments)
	}
}

func TestBrowserPointerDragRequiresDestinationAndApproval(t *testing.T) {
	tool := NewBrowserPointerTool()
	invalid := `{
		"session_id":"athena-0123456789abcdef0123456789abcdef",
		"operation":"drag",
		"grounding_id":"pointer-grounding-0123456789abcdef",
		"screenshot_id":"image-0123456789abcdef",
		"page_revision":"page-revision-0123456789abcdef",
		"coordinate_space":"screenshot_pixels",
		"x":20,"y":30,"purpose":"Move a canvas object"
	}`
	if validation := tool.ValidateInput(context.Background(), invalid); validation.Valid {
		t.Fatal("drag without a destination was accepted")
	}
	valid := `{
		"session_id":"athena-0123456789abcdef0123456789abcdef",
		"operation":"drag",
		"grounding_id":"pointer-grounding-0123456789abcdef",
		"screenshot_id":"image-0123456789abcdef",
		"page_revision":"page-revision-0123456789abcdef",
		"coordinate_space":"screenshot_pixels",
		"x":20,"y":30,"target_x":120,"target_y":180,"purpose":"Move a canvas object"
	}`
	output, err := tool.InvokableRun(context.Background(), valid)
	if err != nil {
		t.Fatal(err)
	}
	action, ok := actionprotocol.Parse(output)
	if !ok || action.Policy.Decision != actionprotocol.AskUser {
		t.Fatalf("drag action = %#v", action)
	}
}

func TestBrowserPointerRejectsRawOrUnboundedCoordinates(t *testing.T) {
	tool := NewBrowserPointerTool()
	for _, input := range []string{
		`{"session_id":"athena-0123456789abcdef0123456789abcdef","operation":"click","grounding_id":"raw","screenshot_id":"image-1","page_revision":"revision-1","coordinate_space":"normalized_1000","x":10,"y":10,"purpose":"canvas"}`,
		`{"session_id":"athena-0123456789abcdef0123456789abcdef","operation":"click","grounding_id":"pointer-grounding-1","screenshot_id":"image-1","page_revision":"revision-1","coordinate_space":"screen","x":10,"y":10,"purpose":"canvas"}`,
		`{"session_id":"athena-0123456789abcdef0123456789abcdef","operation":"click","grounding_id":"pointer-grounding-1","screenshot_id":"image-1","page_revision":"revision-1","coordinate_space":"normalized_1000","x":1001,"y":10,"purpose":"canvas"}`,
	} {
		if validation := tool.ValidateInput(context.Background(), input); validation.Valid {
			t.Fatalf("unsafe pointer input was accepted: %s", input)
		}
	}
}
