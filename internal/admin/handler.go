package admin

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	runtimeconfig "github.com/good-fish-man/agent-runtime/internal/config"
	"github.com/good-fish-man/agent-runtime/internal/constant"
	"github.com/good-fish-man/agent-runtime/internal/tools"

	"gopkg.in/yaml.v3"
)

type Handler struct {
	configPath string
	skillsPath string
	restart    chan<- struct{}
}

func NewHandler(configPath, skillsPath string, restart chan<- struct{}) *Handler {
	return &Handler{configPath: configPath, skillsPath: skillsPath, restart: restart}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/admin/config/status", h.localOnly(h.status))
	mux.HandleFunc("/admin/config/runtime", h.localOnly(h.runtimeConfig))
	mux.HandleFunc("/admin/config/skills", h.localOnly(h.skillsConfig))
	mux.HandleFunc("/admin/restart", h.localOnly(h.restartService))
	mux.HandleFunc("/admin/local-model/lifecycle", h.localOnly(h.localModelLifecycle))
}

func (h *Handler) localModelLifecycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Mode     string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Provider) == "" || strings.TrimSpace(req.Model) == "" {
		writeError(w, http.StatusBadRequest, "provider and model are required")
		return
	}
	if req.Mode != constant.RuntimeModeAlwaysOn && req.Mode != constant.RuntimeModeOnDemand && req.Mode != constant.RuntimeModeOff {
		writeError(w, http.StatusBadRequest, "mode must be always_on, on_demand, or off")
		return
	}
	if err := tools.ApplyLocalModelRuntimeMode(req.Provider, req.Model, req.Mode); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeOK(w, map[string]any{"mode": req.Mode})
}

func (h *Handler) localOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() {
			writeError(w, http.StatusForbidden, "runtime administration is restricted to localhost")
			return
		}
		next(w, r)
	}
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeOK(w, map[string]any{
		"service": "agent-runtime", "pid": os.Getpid(),
		"runtime_config_file": h.configPath, "skills_config_file": h.skillsPath,
		"restart_supported": h.restart != nil,
	})
}

func (h *Handler) runtimeConfig(w http.ResponseWriter, r *http.Request) {
	h.configFile(w, r, h.configPath, true)
}

func (h *Handler) skillsConfig(w http.ResponseWriter, r *http.Request) {
	h.configFile(w, r, h.skillsPath, false)
}

func (h *Handler) configFile(w http.ResponseWriter, r *http.Request, path string, strict bool) {
	switch r.Method {
	case http.MethodGet:
		content, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w, map[string]any{"content": string(content), "path": path})
	case http.MethodPut:
		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Content) == "" {
			writeError(w, http.StatusBadRequest, "content is required")
			return
		}
		if err := validateYAML(req.Content, strict); err != nil {
			writeError(w, http.StatusBadRequest, "invalid YAML: "+err.Error())
			return
		}
		if err := writeAtomic(path, []byte(req.Content)); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w, map[string]any{"path": path, "restart_required": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) restartService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.restart == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime restart is unavailable")
		return
	}
	writeOK(w, map[string]any{"message": "runtime restart scheduled"})
	time.AfterFunc(250*time.Millisecond, func() {
		select {
		case h.restart <- struct{}{}:
		default:
		}
	})
}

func validateYAML(content string, strict bool) error {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(content), &node); err != nil {
		return err
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return errors.New("root value must be a mapping")
	}
	if !strict {
		return nil
	}
	cfg := runtimeconfig.Default()
	decoder := yaml.NewDecoder(strings.NewReader(content))
	decoder.KnownFields(true)
	return decoder.Decode(&cfg)
}

func writeAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func writeOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": data})
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": status, "message": message})
}
