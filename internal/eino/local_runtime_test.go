package eino

import (
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/constant"
)

func TestNormalizeOllamaAPIBaseUsesIPv4Loopback(t *testing.T) {
	if got := normalizeOllamaAPIBase("http://localhost:11434/v1"); got != "http://127.0.0.1:11434/v1" {
		t.Fatalf("normalized API base = %q", got)
	}
	if got := normalizeOllamaAPIBase(""); got != "http://127.0.0.1:11434/v1" {
		t.Fatalf("default API base = %q", got)
	}
}

func TestPrepareLocalModelRuntimeRejectsOffModeBeforeStarting(t *testing.T) {
	cfg := &ModelConfig{
		Provider:    "Ollama",
		APIBase:     "http://localhost:11434/v1",
		ExtraFields: map[string]any{"runtime_mode": constant.RuntimeModeOff},
	}
	if err := prepareLocalModelRuntime(t.Context(), cfg); err == nil {
		t.Fatal("off runtime mode was accepted")
	}
}
