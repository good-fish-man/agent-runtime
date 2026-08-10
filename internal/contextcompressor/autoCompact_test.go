package contextcompressor

import "testing"

func TestGetContextWindowSize(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{model: "gpt-5.6", want: 1_050_000},
		{model: "claude-sonnet-5", want: 1_000_000},
		{model: "claude-haiku-4-5", want: 200_000},
		{model: "gemini-3.6-flash", want: 1_048_576},
		{model: "deepseek-v4-pro", want: 1_000_000},
		{model: "qwen3.8-max-preview", want: 983_616},
		{model: "grok-4.5", want: 500_000},
		{model: "mistral-medium-3-5", want: 256_000},
		{model: "MiniMax-M2.7", want: 204_800},
		{model: "unknown-model", want: 150_000},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := GetContextWindowSize(tt.model); got != tt.want {
				t.Fatalf("GetContextWindowSize(%q) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}
