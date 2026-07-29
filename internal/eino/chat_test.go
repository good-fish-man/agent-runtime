package eino

import (
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/tools"

	"github.com/cloudwego/eino/schema"
)

func TestIsUserVisibleMessage(t *testing.T) {
	tests := []struct {
		name    string
		message *schema.Message
		want    bool
	}{
		{name: "nil", message: nil, want: false},
		{name: "assistant", message: schema.AssistantMessage("done", nil), want: true},
		{
			name: "generated image",
			message: schema.ToolMessage(
				"![Generated image](https://example.com/image.png)",
				"call-image",
				schema.WithToolName(tools.GenerateImageToolName),
			),
			want: true,
		},
		{
			name:    "other tool",
			message: schema.ToolMessage(`{"private":"result"}`, "call-other", schema.WithToolName("OtherTool")),
			want:    false,
		},
		{name: "user", message: schema.UserMessage("hello"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUserVisibleMessage(tt.message); got != tt.want {
				t.Fatalf("isUserVisibleMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}
