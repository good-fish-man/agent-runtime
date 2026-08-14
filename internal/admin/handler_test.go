package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginReloadIsLocalAndObservable(t *testing.T) {
	mux := http.NewServeMux()
	called := false
	NewHandler("config.yaml", "skills.yaml", make(chan struct{}, 1)).WithPluginReload(func(context.Context) (any, error) {
		called = true
		return map[string]any{"loaded": []string{"com.example.echo@0.8.0"}}, nil
	}).Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/admin/plugins/reload", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !called || !strings.Contains(res.Body.String(), "com.example.echo@0.8.0") {
		t.Fatalf("reload response=%d called=%v body=%s", res.Code, called, res.Body.String())
	}
}

func TestStatusReportsRuntimeOwnedPaths(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	skillsPath := filepath.Join(dir, "config", "skills-config.yaml")
	mux := http.NewServeMux()
	NewHandler(configPath, skillsPath, make(chan struct{}, 1)).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/config/status", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", res.Code, http.StatusOK)
	}
	body := res.Body.String()
	if !containsAll(body, configPath, skillsPath) {
		t.Fatalf("status response does not contain runtime paths: %s", body)
	}
}

func TestAdminRejectsNonLoopbackRequests(t *testing.T) {
	mux := http.NewServeMux()
	NewHandler("config.yaml", "skills.yaml", make(chan struct{}, 1)).Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/admin/config/status", nil)
	req.RemoteAddr = "192.0.2.10:12345"
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status code = %d, want %d", res.Code, http.StatusForbidden)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
