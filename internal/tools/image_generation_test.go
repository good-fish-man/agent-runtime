package tools

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/types"
)

func TestImageGenerationToolOpenAIURL(t *testing.T) {
	originalTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"url":"https://example.com/generated.png"}]}`)),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = originalTransport })

	imageTool := NewImageGenerationTool(types.ModelConfig{Provider: "OpenAI", Name: "image-model", APIKey: "test-key", APIBase: "https://unit.test/v1"})
	result, err := imageTool.InvokableRun(context.Background(), `{"prompt":"a bronze owl"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result != "![Generated image](https://example.com/generated.png)" {
		t.Fatalf("result = %q", result)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestEnsureDiffusersWeightAliases(t *testing.T) {
	modelDir := t.TempDir()
	fp16 := filepath.Join(modelDir, "vae", "diffusion_pytorch_model.fp16.safetensors")
	if err := os.MkdirAll(filepath.Dir(fp16), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fp16, []byte("weights"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ensureDiffusersWeightAliases(modelDir); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(modelDir, "vae", "diffusion_pytorch_model.safetensors")
	data, err := os.ReadFile(alias)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "weights" {
		t.Fatalf("alias contents = %q", data)
	}

	if err := ensureDiffusersWeightAliases(modelDir); err != nil {
		t.Fatalf("second alias preparation should be idempotent: %v", err)
	}
}

func TestModelRuntimeMode(t *testing.T) {
	if got := modelRuntimeMode(types.ModelConfig{}); got != "on_demand" {
		t.Fatalf("default mode = %q", got)
	}
	if got := modelRuntimeMode(types.ModelConfig{ExtraFields: map[string]any{"runtime_mode": "always_on"}}); got != "always_on" {
		t.Fatalf("configured mode = %q", got)
	}
	if got := modelRuntimeMode(types.ModelConfig{ExtraFields: map[string]any{"runtime_mode": "invalid"}}); got != "on_demand" {
		t.Fatalf("invalid mode = %q", got)
	}
}
