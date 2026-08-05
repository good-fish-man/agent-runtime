package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/eino"
)

func TestUnit_ExtractorCallLLM_DoesNotSendTemperature(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if _, ok := body["temperature"]; ok {
			t.Fatalf("memory extractor should not send temperature by default: %#v", body["temperature"])
		}
		if got := body["model"]; got != "gpt-test" {
			t.Fatalf("model = %#v", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"memories\":[]}"}}]}`))
	}))
	defer srv.Close()

	extractor := NewExtractor(eino.ModelConfig{Name: "gpt-test", APIKey: "test-key", APIBase: srv.URL})
	if _, err := extractor.callLLM(context.Background(), "test-key", "extract memories"); err != nil {
		t.Fatalf("callLLM returned error: %v", err)
	}
}

func TestUnit_Extractor_DoesNotExpandRuntimeEnvironmentSecrets(t *testing.T) {
	t.Setenv("ATHENA_MEMORY_TEST_API_KEY", "server-side-secret")
	t.Setenv("ATHENA_MEMORY_TEST_API_BASE", "https://server-side.example.test")

	extractor := NewExtractor(eino.ModelConfig{
		Name:    "gpt-test",
		APIKey:  "${ATHENA_MEMORY_TEST_API_KEY}",
		APIBase: " ${ATHENA_MEMORY_TEST_API_BASE} ",
	})

	if got := extractor.model.APIKey; got != "${ATHENA_MEMORY_TEST_API_KEY}" {
		t.Fatalf("APIKey should not be expanded from runtime env, got %q", got)
	}
	if strings.Contains(extractor.apiBase, "server-side.example.test") {
		t.Fatalf("APIBase should not be expanded from runtime env, got %q", extractor.apiBase)
	}
	if got := extractor.apiBase; got != "${ATHENA_MEMORY_TEST_API_BASE}" {
		t.Fatalf("APIBase should only be trimmed, got %q", got)
	}
}
