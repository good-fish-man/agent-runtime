package eino

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/constant"
	log "github.com/good-fish-man/logx"
)

const ollamaStartupTimeout = 20 * time.Second

var ollamaStartupMu sync.Mutex

func prepareLocalModelRuntime(ctx context.Context, cfg *ModelConfig) error {
	if cfg == nil || normalizeProvider(cfg.Provider) != constant.ProviderOllama {
		return nil
	}
	if modelRuntimeMode(cfg.ExtraFields) == constant.RuntimeModeOff {
		return fmt.Errorf("local Ollama model is disabled by the administrator")
	}
	cfg.APIBase = normalizeOllamaAPIBase(cfg.APIBase)
	if err := ensureOllamaRunning(ctx); err != nil {
		return fmt.Errorf("prepare Ollama runtime: %w", err)
	}
	return nil
}

func modelRuntimeMode(extraFields map[string]any) string {
	if value, ok := extraFields["runtime_mode"].(string); ok {
		mode := strings.ToLower(strings.TrimSpace(value))
		if mode != "" {
			return mode
		}
	}
	return constant.RuntimeModeOnDemand
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(provider))
}

func normalizeOllamaAPIBase(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return constant.DefaultOllamaAPIBase + "/v1"
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "localhost") {
		return raw
	}
	port := parsed.Port()
	parsed.Host = net.JoinHostPort("127.0.0.1", port)
	if port == "" {
		parsed.Host = "127.0.0.1"
	}
	return parsed.String()
}

func ensureOllamaRunning(ctx context.Context) error {
	if ollamaHealthy(ctx) {
		return nil
	}
	ollamaStartupMu.Lock()
	defer ollamaStartupMu.Unlock()
	if ollamaHealthy(ctx) {
		return nil
	}

	binary, err := findOllamaBinary()
	if err != nil {
		return err
	}
	logPath := filepath.Join(os.TempDir(), constant.OllamaStartupLogFileName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open Ollama log: %w", err)
	}
	command := exec.Command(binary, "serve")
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start Ollama: %w", err)
	}
	_ = logFile.Close()
	go func() { _ = command.Wait() }()
	log.Infof(ctx, "Ollama was not running; started %s serve (pid=%d)", binary, command.Process.Pid)

	deadline := time.NewTimer(ollamaStartupTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("Ollama did not become ready within %s; check %s", ollamaStartupTimeout, logPath)
		case <-ticker.C:
			if ollamaHealthy(ctx) {
				return nil
			}
		}
	}
}

func ollamaHealthy(ctx context.Context) bool {
	requestCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, constant.DefaultOllamaAPIBase+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func findOllamaBinary() (string, error) {
	if binary, err := exec.LookPath("ollama"); err == nil {
		return binary, nil
	}
	candidates := []string{"/opt/homebrew/bin/ollama", "/usr/local/bin/ollama"}
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			candidates = append(candidates, filepath.Join(localAppData, "Programs", "Ollama", "ollama.exe"))
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("Ollama is not installed or is not available in PATH")
}
