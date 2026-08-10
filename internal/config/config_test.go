package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/constant"
)

func TestLoadResolvesSkillsRelativeToConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("skills:\n  dir: bundled-skills\n  config_path: skill-config.yaml\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "bundled-skills"); cfg.Skills.Dir != want {
		t.Fatalf("skills dir = %q, want %q", cfg.Skills.Dir, want)
	}
	if want := filepath.Join(dir, "skill-config.yaml"); cfg.Skills.ConfigPath != want {
		t.Fatalf("skills config = %q, want %q", cfg.Skills.ConfigPath, want)
	}
}

func TestResearchDefaultsAndEnvironmentOverrides(t *testing.T) {
	defaults := Default().Research
	if !defaults.Enabled || defaults.MaxQueries <= 0 || defaults.MaxPages <= 0 || len(defaults.Providers) < 3 || !filepath.IsAbs(defaults.CacheDir) {
		t.Fatalf("invalid research defaults: %+v", defaults)
	}
	t.Setenv(constant.EnvResearchProviders, "web,github")
	t.Setenv(constant.EnvResearchMaxQueries, "9")
	t.Setenv(constant.EnvResearchCacheDir, "research-cache")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Research.MaxQueries != 9 || len(cfg.Research.Providers) != 2 {
		t.Fatalf("research env overrides were not applied: %+v", cfg.Research)
	}
	if cfg.Research.CacheDir != filepath.Join(filepath.Dir(configPath), "research-cache") {
		t.Fatalf("relative research cache path was not resolved: %s", cfg.Research.CacheDir)
	}
}

func TestDefaultSkillsDirIsConfigured(t *testing.T) {
	if Default().Skills.Dir != "skills" {
		t.Fatalf("default skills dir must point at bundled skills")
	}
	if Default().Skills.ConfigPath != "config/skills-config.yaml" {
		t.Fatalf("default skills config must point at the runtime config directory")
	}
}
