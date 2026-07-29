package tools

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/types"
)

func TestGenerateVideoCompletesOpenAIJobAndStoresContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "provider.test" && request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		status, body, contentType := http.StatusOK, "", "application/json"
		switch request.URL.Path {
		case "/reference.png":
			body, contentType = "fake-png", "image/png"
		case "/v1/videos":
			if request.Method != http.MethodPost || !strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/form-data;") {
				t.Fatalf("method/content-type = %s %q", request.Method, request.Header.Get("Content-Type"))
			}
			if err := request.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			file, _, err := request.FormFile("input_reference")
			if err != nil {
				t.Fatalf("input_reference: %v", err)
			}
			data, err := io.ReadAll(file)
			_ = file.Close()
			if err != nil || string(data) != "fake-png" {
				t.Fatalf("reference data = %q, err = %v", data, err)
			}
			body = `{"id":"video-1","status":"completed"}`
		case "/v1/videos/video-1/content":
			body, contentType = "fake-mp4", "video/mp4"
		default:
			status, body = http.StatusNotFound, "not found"
		}
		return &http.Response{
			StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}},
			Body: io.NopCloser(strings.NewReader(body)), Request: request,
		}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = originalTransport })

	t.Setenv("AGENT_RUNTIME_PUBLIC_URL", "https://runtime.test")
	result, err := GenerateVideo(context.Background(), types.ModelConfig{
		Provider: "OpenAI", Name: "sora-test", APIKey: "test-key", APIBase: "https://provider.test/v1",
	}, VideoGenerationRequest{
		Prompt: "ocean sunrise", SourceURL: "https://assets.test/reference.png", Size: "1280x720", DurationSeconds: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProviderJobID != "video-1" || !strings.Contains(result.URL, "/generated/") {
		t.Fatalf("result = %+v", result)
	}
	filename := result.URL[strings.LastIndex(result.URL, "/")+1:]
	t.Cleanup(func() { _ = os.Remove(filepathJoinGenerated(filename)) })
}

func filepathJoinGenerated(filename string) string {
	return GeneratedImagesDir() + string(os.PathSeparator) + filename
}

func TestVideoDimensions(t *testing.T) {
	if width, height := videoDimensions("1280x720", "cerspense/zeroscope_v2_576w"); width != 576 || height != 320 {
		t.Fatalf("zeroscope dimensions = %dx%d", width, height)
	}
	if width, height := videoDimensions("720x480", "custom/video"); width != 720 || height != 480 {
		t.Fatalf("custom dimensions = %dx%d", width, height)
	}
}
