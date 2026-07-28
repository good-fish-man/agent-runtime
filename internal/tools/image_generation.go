package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/types"
	"github.com/good-fish-man/agent-runtime/log"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type ImageGenerationTool struct{ model types.ModelConfig }

type imageGenerationInput struct {
	Prompt         string `json:"prompt"`
	NegativePrompt string `json:"negative_prompt,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
}

func NewImageGenerationTool(model types.ModelConfig) *ImageGenerationTool {
	return &ImageGenerationTool{model: model}
}

func (t *ImageGenerationTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "GenerateImage",
		Desc: "Generate an image with the Agent's bound image model. Use this whenever the user asks to draw, design, render, illustrate, or generate an image. Return the markdown field from the tool result to the user unchanged.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"prompt":          {Type: schema.String, Desc: "Detailed image generation prompt", Required: true},
			"negative_prompt": {Type: schema.String, Desc: "Elements to avoid"},
			"size":            {Type: schema.String, Desc: "Image size such as 1024x1024"},
			"quality":         {Type: schema.String, Desc: "Quality such as standard or high"},
		}),
	}, nil
}

func (t *ImageGenerationTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	var request imageGenerationInput
	if err := json.Unmarshal([]byte(input), &request); err != nil {
		return "", fmt.Errorf("invalid image request: %w", err)
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if request.Size == "" {
		request.Size = "1024x1024"
	}
	if modelRuntimeMode(t.model) == constant.RuntimeModeOff {
		return "", fmt.Errorf("local model is disabled by the administrator")
	}
	provider := strings.ToLower(strings.ReplaceAll(t.model.Provider, " ", ""))
	var imageURL string
	var err error
	switch provider {
	case constant.ProviderDiffusers:
		imageURL, err = t.generateDiffusers(ctx, request)
	case "stabilityai", "stability":
		imageURL, err = t.generateStability(ctx, request)
	default:
		imageURL, err = t.generateOpenAI(ctx, request)
	}
	if err != nil {
		return "", log.WrapError(err, "ImageGenerationTool.InvokableRun.generate")
	}
	result, _ := json.Marshal(map[string]any{
		"image_url": imageURL,
		"markdown":  fmt.Sprintf("![Generated image](%s)", imageURL),
		"prompt":    request.Prompt,
	})
	return string(result), nil
}

func (t *ImageGenerationTool) generateOpenAI(ctx context.Context, input imageGenerationInput) (string, error) {
	endpoint := strings.TrimRight(t.model.APIBase, "/")
	if !strings.HasSuffix(endpoint, "/images/generations") {
		endpoint += "/images/generations"
	}
	requestBody := map[string]any{"model": t.model.Name, "prompt": input.Prompt, "size": input.Size}
	if input.Quality != "" {
		requestBody["quality"] = input.Quality
	}
	payload, _ := json.Marshal(requestBody)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if t.model.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.model.APIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", log.WrapError(err, "ImageGenerationTool.generateOpenAI.request")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("image API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var result struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil || len(result.Data) == 0 {
		return "", fmt.Errorf("image API returned no image")
	}
	if result.Data[0].URL != "" {
		return result.Data[0].URL, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(result.Data[0].B64JSON)
	if err != nil {
		return "", fmt.Errorf("decode generated image: %w", err)
	}
	return saveGeneratedImage(decoded, ".png")
}

func (t *ImageGenerationTool) generateStability(ctx context.Context, input imageGenerationInput) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("prompt", input.Prompt)
	_ = writer.WriteField("negative_prompt", input.NegativePrompt)
	_ = writer.WriteField("output_format", "png")
	_ = writer.Close()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, t.model.APIBase, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "image/*")
	req.Header.Set("Authorization", "Bearer "+t.model.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", log.WrapError(err, "ImageGenerationTool.generateStability.request")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Stability API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return saveGeneratedImage(data, ".png")
}

func (t *ImageGenerationTool) generateDiffusers(ctx context.Context, input imageGenerationInput) (string, error) {
	python := diffusersPython()
	modelDir := filepath.Join(athenaDir(), "models", "diffusers", strings.ReplaceAll(t.model.Name, "/", "--"))
	if _, err := os.Stat(filepath.Join(modelDir, ".athena_complete")); err != nil {
		return "", fmt.Errorf("local image model is not downloaded: %s", t.model.Name)
	}
	if err := ensureDiffusersWeightAliases(modelDir); err != nil {
		return "", fmt.Errorf("prepare local image model weights: %w", err)
	}
	filename := generatedFilename(".png")
	output := filepath.Join(GeneratedImagesDir(), filename)
	if err := os.MkdirAll(GeneratedImagesDir(), 0o755); err != nil {
		return "", log.WrapError(err, "ImageGenerationTool.generateDiffusers.createOutputDir")
	}
	if modelRuntimeMode(t.model) == constant.RuntimeModeAlwaysOn {
		if err := sharedDiffusersWorkers.generate(ctx, modelDir, input.Prompt, input.NegativePrompt, output); err != nil {
			return "", log.WrapError(err, "ImageGenerationTool.generateDiffusers.worker")
		}
		return generatedPublicURL(filename), nil
	}
	script := `import sys, torch
from diffusers import DiffusionPipeline
model, prompt, negative, output = sys.argv[1:5]
has_mps = hasattr(torch.backends, "mps") and torch.backends.mps.is_available()
dtype = torch.float16 if torch.cuda.is_available() or has_mps else torch.float32
load_options = {"torch_dtype": dtype, "local_files_only": True, "use_safetensors": True}
pipe = DiffusionPipeline.from_pretrained(model, **load_options)
if torch.cuda.is_available(): pipe.to("cuda")
elif has_mps: pipe.to("mps")
result = pipe(prompt=prompt, negative_prompt=negative or None).images[0]
result.save(output)`
	command := exec.CommandContext(ctx, python, "-c", script, modelDir, input.Prompt, input.NegativePrompt, output)
	if data, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("local image generation failed: %v: %s", err, strings.TrimSpace(string(data)))
	}
	return generatedPublicURL(filename), nil
}

func modelRuntimeMode(model types.ModelConfig) string {
	if model.ExtraFields != nil {
		if value, ok := model.ExtraFields["runtime_mode"].(string); ok {
			switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
			case constant.RuntimeModeAlwaysOn, constant.RuntimeModeOnDemand, constant.RuntimeModeOff:
				return normalized
			}
		}
	}
	return constant.RuntimeModeOnDemand
}

func ensureDiffusersWeightAliases(modelDir string) error {
	return filepath.Walk(modelDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !strings.HasSuffix(strings.ToLower(info.Name()), ".fp16.safetensors") {
			return err
		}
		alias := strings.TrimSuffix(path, ".fp16.safetensors") + ".safetensors"
		if _, err := os.Stat(alias); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Link(path, alias); err == nil {
			return nil
		}

		// Some filesystems do not support hard links. Copy only as a portability fallback.
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer source.Close()
		target, err := os.OpenFile(alias, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err = io.Copy(target, source); err != nil {
			_ = target.Close()
			_ = os.Remove(alias)
			return err
		}
		return target.Close()
	})
}

func athenaDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "athena")
	}
	return filepath.Join(home, ".athena")
}

func diffusersPython() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(athenaDir(), "image-runtime", "venv", "Scripts", "python.exe")
	}
	return filepath.Join(athenaDir(), "image-runtime", "venv", "bin", "python")
}

func GeneratedImagesDir() string { return filepath.Join(athenaDir(), "generated-images") }

// GeneratedImageHandler serves only random generated files and never lists the directory.
func GeneratedImageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := filepath.Base(strings.TrimPrefix(r.URL.Path, "/generated/"))
	if name == "." || name == "" || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, filepath.Join(GeneratedImagesDir(), name))
}

func generatedFilename(ext string) string {
	random := make([]byte, 8)
	_, _ = rand.Read(random)
	return fmt.Sprintf("%d-%s%s", time.Now().UnixMilli(), hex.EncodeToString(random), ext)
}

func generatedPublicURL(filename string) string {
	base := strings.TrimRight(os.Getenv("AGENT_RUNTIME_PUBLIC_URL"), "/")
	if base == "" {
		base = "http://127.0.0.1:18081"
	}
	return base + "/generated/" + filename
}

func saveGeneratedImage(data []byte, ext string) (string, error) {
	if err := os.MkdirAll(GeneratedImagesDir(), 0o755); err != nil {
		return "", err
	}
	filename := generatedFilename(ext)
	if err := os.WriteFile(filepath.Join(GeneratedImagesDir(), filename), data, 0o600); err != nil {
		return "", err
	}
	return generatedPublicURL(filename), nil
}
