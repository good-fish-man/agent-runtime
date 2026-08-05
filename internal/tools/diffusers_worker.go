package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/constant"
)

const diffusersWorkerScript = `import json, sys, torch
from diffusers import DiffusionPipeline
model = sys.argv[1]
has_mps = hasattr(torch.backends, "mps") and torch.backends.mps.is_available()
dtype = torch.float16 if torch.cuda.is_available() or has_mps else torch.float32
pipe = DiffusionPipeline.from_pretrained(model, torch_dtype=dtype, local_files_only=True, use_safetensors=True)
if torch.cuda.is_available(): pipe.to("cuda")
elif has_mps: pipe.to("mps")
pipe.set_progress_bar_config(disable=True)
print(json.dumps({"ready": True}), flush=True)
for line in sys.stdin:
    try:
        request = json.loads(line)
        with torch.inference_mode():
            image = pipe(prompt=request["prompt"], negative_prompt=request.get("negative_prompt") or None).images[0]
        image.save(request["output"])
        print(json.dumps({"ok": True}), flush=True)
    except Exception as error:
        print(json.dumps({"error": str(error)}), flush=True)`

type diffusersWorkerManager struct {
	mu      sync.Mutex
	workers map[string]*diffusersWorker
}

type diffusersWorker struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	stderr bytes.Buffer
	mu     sync.Mutex
}

var sharedDiffusersWorkers = &diffusersWorkerManager{workers: make(map[string]*diffusersWorker)}

func (m *diffusersWorkerManager) generate(ctx context.Context, modelDir, prompt, negativePrompt, output string) error {
	worker, err := m.worker(modelDir)
	if err != nil {
		return fmt.Errorf("start persistent image model: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.generate(prompt, negativePrompt, output) }()
	select {
	case err := <-done:
		if err != nil {
			m.remove(modelDir, worker)
			return fmt.Errorf("persistent image generation failed: %w", err)
		}
		return nil
	case <-ctx.Done():
		m.remove(modelDir, worker)
		return ctx.Err()
	}
}

func (m *diffusersWorkerManager) worker(modelDir string) (*diffusersWorker, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if worker := m.workers[modelDir]; worker != nil {
		return worker, nil
	}
	worker, err := startDiffusersWorker(modelDir)
	if err != nil {
		return nil, err
	}
	m.workers[modelDir] = worker
	return worker, nil
}

func (m *diffusersWorkerManager) remove(modelDir string, worker *diffusersWorker) {
	m.mu.Lock()
	if m.workers[modelDir] == worker {
		delete(m.workers, modelDir)
	}
	m.mu.Unlock()
	worker.stop()
}

func (m *diffusersWorkerManager) stopModel(modelDir string) {
	m.mu.Lock()
	worker := m.workers[modelDir]
	delete(m.workers, modelDir)
	m.mu.Unlock()
	if worker != nil {
		worker.stop()
	}
}

// ApplyLocalModelRuntimeMode applies an administrator lifecycle decision immediately.
func ApplyLocalModelRuntimeMode(provider, model, mode string) error {
	provider = strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(provider))
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch provider {
	case constant.ProviderDiffusers:
		modelDir := diffusersModelDir(model)
		if mode != constant.RuntimeModeAlwaysOn {
			sharedDiffusersWorkers.stopModel(modelDir)
			return nil
		}
		if err := ensureDiffusersWeightAliases(modelDir); err != nil {
			return err
		}
		go func() { _, _ = sharedDiffusersWorkers.worker(modelDir) }()
		return nil
	case constant.ProviderOllama:
		keepAlive := "0"
		if mode == constant.RuntimeModeAlwaysOn {
			keepAlive = "-1"
		}
		payload, _ := json.Marshal(map[string]any{"model": model, "prompt": "", "stream": false, "keep_alive": keepAlive})
		apply := func() error {
			client := &http.Client{Timeout: 10 * time.Minute}
			resp, err := client.Post(constant.DefaultOllamaAPIBase+"/api/generate", "application/json", bytes.NewReader(payload))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				return fmt.Errorf("ollama lifecycle returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
			}
			return nil
		}
		if mode == constant.RuntimeModeAlwaysOn {
			go func() { _ = apply() }()
			return nil
		}
		return apply()
	default:
		return fmt.Errorf("runtime lifecycle is unsupported for provider %s", provider)
	}
}

func startDiffusersWorker(modelDir string) (*diffusersWorker, error) {
	worker := &diffusersWorker{cmd: exec.Command(diffusersPython(), "-u", "-c", diffusersWorkerScript, modelDir)}
	stdin, err := worker.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := worker.cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	worker.stdin = stdin
	worker.stdout = bufio.NewScanner(stdout)
	worker.stdout.Buffer(make([]byte, 1024), 1024*1024)
	worker.cmd.Stderr = &worker.stderr
	if err := worker.cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if !worker.stdout.Scan() {
		worker.stop()
		return nil, fmt.Errorf("worker exited during model loading: %s", worker.stderr.String())
	}
	var ready struct {
		Ready bool   `json:"ready"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(worker.stdout.Bytes(), &ready); err != nil || !ready.Ready {
		worker.stop()
		return nil, fmt.Errorf("worker failed to initialize: %s %s", ready.Error, worker.stderr.String())
	}
	return worker, nil
}

func (w *diffusersWorker) generate(prompt, negativePrompt, output string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	payload, _ := json.Marshal(map[string]string{"prompt": prompt, "negative_prompt": negativePrompt, "output": output})
	if _, err := w.stdin.Write(append(payload, '\n')); err != nil {
		return err
	}
	if !w.stdout.Scan() {
		return fmt.Errorf("worker exited: %s", w.stderr.String())
	}
	var response struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.stdout.Bytes(), &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("%s", response.Error)
	}
	return nil
}

func (w *diffusersWorker) stop() {
	_ = w.stdin.Close()
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
	_ = w.cmd.Wait()
}
