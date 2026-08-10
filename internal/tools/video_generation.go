package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/observability"
	"github.com/good-fish-man/agent-runtime/internal/types"
	log "github.com/good-fish-man/logx"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const GenerateVideoToolName = "GenerateVideo"

type VideoGenerationTool struct{ model types.ModelConfig }

type videoGenerationToolInput struct {
	Prompt          string `json:"prompt"`
	SourceURL       string `json:"source_url,omitempty"`
	Size            string `json:"size,omitempty"`
	DurationSeconds int    `json:"duration_seconds,omitempty"`
}

func NewVideoGenerationTool(model types.ModelConfig) *VideoGenerationTool {
	return &VideoGenerationTool{model: model}
}

func (t *VideoGenerationTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: GenerateVideoToolName,
		Desc: "Generate a video with the Agent's bound video model. The successful result is returned directly to the user.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"prompt":           {Type: schema.String, Desc: "Detailed video prompt", Required: true},
			"source_url":       {Type: schema.String, Desc: "Optional reference image URL"},
			"size":             {Type: schema.String, Desc: "Video dimensions such as 1280x720"},
			"duration_seconds": {Type: schema.Integer, Desc: "Video duration in seconds"},
		}),
	}, nil
}

func (t *VideoGenerationTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var request videoGenerationToolInput
	if err := json.Unmarshal([]byte(input), &request); err != nil {
		return "", fmt.Errorf("invalid video request: %w", err)
	}
	result, err := GenerateVideo(ctx, t.model, VideoGenerationRequest{
		Prompt: request.Prompt, SourceURL: request.SourceURL, Size: request.Size, DurationSeconds: request.DurationSeconds,
	})
	if err != nil {
		return "", log.WrapError(err, "VideoGenerationTool.InvokableRun")
	}
	return fmt.Sprintf("[Generated video](%s)", result.URL), nil
}

type VideoGenerationRequest struct {
	Prompt          string
	SourceURL       string
	Size            string
	DurationSeconds int
}

type VideoGenerationResult struct {
	URL           string
	ProviderJobID string
}

type videoJob struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	URL      string `json:"url"`
	VideoURL string `json:"video_url"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// GenerateVideo calls an OpenAI-compatible asynchronous video endpoint.
// Provider adapters can be added here without changing the public RPC.
func GenerateVideo(ctx context.Context, model types.ModelConfig, input VideoGenerationRequest) (result *VideoGenerationResult, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	span := observability.Begin(ctx, "model", model.Name, "",
		"provider", model.Provider,
		"mode", "video_generate",
		"has_source_image", strings.TrimSpace(input.SourceURL) != "",
	)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = log.NewError("GenerateVideo", "panic: %v", recovered)
			log.ErrorfCtx(ctx, "model call panic model=%s mode=video_generate error=%v\n%s", model.Name, recovered, debug.Stack())
		} else if err != nil {
			err = log.WrapError(err, "GenerateVideo")
		}
		span.End(err, "output_present", result != nil && result.URL != "")
	}()

	if strings.TrimSpace(input.Prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if input.DurationSeconds <= 0 {
		input.DurationSeconds = 4
	}
	if input.Size == "" {
		input.Size = "1280x720"
	}
	if modelRuntimeMode(model) == constant.RuntimeModeOff {
		return nil, fmt.Errorf("local model is disabled by the administrator")
	}
	provider := strings.ToLower(strings.ReplaceAll(model.Provider, " ", ""))
	if provider == constant.ProviderDiffusers {
		return generateDiffusersVideo(ctx, model, input)
	}
	endpoint := mediaEndpoint(model.APIBase, "videos")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", model.Name)
	_ = writer.WriteField("prompt", input.Prompt)
	_ = writer.WriteField("seconds", strconv.Itoa(input.DurationSeconds))
	_ = writer.WriteField("size", input.Size)
	if input.SourceURL != "" {
		reference, err := fetchSourceImage(ctx, input.SourceURL)
		if err != nil {
			return nil, log.WrapError(err, "GenerateVideo.reference")
		}
		part, err := writer.CreateFormFile("input_reference", "reference.png")
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(reference); err != nil {
			return nil, err
		}
	}
	_ = writer.Close()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	setBearer(request, model.APIKey)
	job, err := doVideoJobRequest(request)
	if err != nil {
		return nil, log.WrapError(err, "GenerateVideo.create")
	}
	if directURL := firstNonEmpty(job.VideoURL, job.URL); directURL != "" {
		url, err := downloadGeneratedMedia(ctx, directURL, model.APIKey, ".mp4")
		return &VideoGenerationResult{URL: url, ProviderJobID: job.ID}, err
	}
	if job.ID == "" {
		return nil, fmt.Errorf("video API returned no job id")
	}

	jobURL := strings.TrimRight(endpoint, "/") + "/" + job.ID
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch strings.ToLower(job.Status) {
		case "completed", "succeeded", "success":
			contentURL := jobURL + "/content"
			url, err := downloadGeneratedMedia(ctx, contentURL, model.APIKey, ".mp4")
			return &VideoGenerationResult{URL: url, ProviderJobID: job.ID}, err
		case "failed", "cancelled", "canceled":
			message := "video generation failed"
			if job.Error != nil && job.Error.Message != "" {
				message = job.Error.Message
			}
			return nil, fmt.Errorf("%s", message)
		}

		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		poll, err := http.NewRequestWithContext(ctx, http.MethodGet, jobURL, nil)
		if err != nil {
			return nil, err
		}
		setBearer(poll, model.APIKey)
		job, err = doVideoJobRequest(poll)
		if err != nil {
			return nil, log.WrapError(err, "GenerateVideo.poll")
		}
	}
}

func generateDiffusersVideo(ctx context.Context, model types.ModelConfig, input VideoGenerationRequest) (*VideoGenerationResult, error) {
	if input.SourceURL != "" {
		return nil, fmt.Errorf("local video model %s does not support image-to-video", model.Name)
	}
	modelDir := diffusersModelDir(model.Name)
	if _, err := os.Stat(filepath.Join(modelDir, constant.DiffusersCompleteFileName)); err != nil {
		return nil, fmt.Errorf("local video model is not downloaded: %s", model.Name)
	}
	if err := os.MkdirAll(GeneratedImagesDir(), 0o755); err != nil {
		return nil, err
	}
	width, height := videoDimensions(input.Size, model.Name)
	frames := input.DurationSeconds * 8
	if frames < 16 {
		frames = 16
	}
	if frames > 48 {
		frames = 48
	}
	filename := generatedFilename(".mp4")
	output := filepath.Join(GeneratedImagesDir(), filename)
	script := `import sys, torch
from diffusers import DiffusionPipeline
from diffusers.utils import export_to_video
model, prompt, output, width, height, frames = sys.argv[1:7]
has_mps = hasattr(torch.backends, "mps") and torch.backends.mps.is_available()
dtype = torch.float16 if torch.cuda.is_available() or has_mps else torch.float32
pipe = DiffusionPipeline.from_pretrained(model, torch_dtype=dtype, local_files_only=True)
if torch.cuda.is_available(): pipe.enable_model_cpu_offload()
elif has_mps: pipe.to("mps")
result = pipe(prompt=prompt, width=int(width), height=int(height), num_frames=int(frames))
video_frames = result.frames[0]
export_to_video(video_frames, output, fps=8)`
	command := exec.CommandContext(ctx, diffusersPython(), "-c", script, modelDir, input.Prompt, output, strconv.Itoa(width), strconv.Itoa(height), strconv.Itoa(frames))
	command.Env = append(os.Environ(), "PYTORCH_ENABLE_MPS_FALLBACK=1")
	data, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("local video generation failed: %v: %s", err, strings.TrimSpace(string(data)))
	}
	return &VideoGenerationResult{URL: generatedPublicURL(filename)}, nil
}

func videoDimensions(size, model string) (int, int) {
	if strings.Contains(strings.ToLower(model), "zeroscope") {
		return 576, 320
	}
	parts := strings.Split(strings.ToLower(strings.TrimSpace(size)), "x")
	if len(parts) == 2 {
		width, widthErr := strconv.Atoi(parts[0])
		height, heightErr := strconv.Atoi(parts[1])
		if widthErr == nil && heightErr == nil && width > 0 && height > 0 {
			return width, height
		}
	}
	if runtime.GOARCH == "arm64" {
		return 576, 320
	}
	return 720, 480
}

func mediaEndpoint(base, resource string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if strings.HasSuffix(base, "/"+resource) {
		return base
	}
	return base + "/" + resource
}

func doVideoJobRequest(request *http.Request) (videoJob, error) {
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return videoJob{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return videoJob{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return videoJob{}, fmt.Errorf("video API returned %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	var job videoJob
	if err := json.Unmarshal(data, &job); err != nil {
		return videoJob{}, fmt.Errorf("decode video API response: %w", err)
	}
	return job, nil
}

func downloadGeneratedMedia(ctx context.Context, sourceURL, apiKey, extension string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", err
	}
	setBearer(request, apiKey)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return "", fmt.Errorf("download generated media returned %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 512<<20))
	if err != nil {
		return "", err
	}
	return saveGeneratedImage(data, extension)
}

func setBearer(request *http.Request, apiKey string) {
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
