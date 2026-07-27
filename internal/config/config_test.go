package config

import (
	"os"
	"path/filepath"
	"testing"
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

func TestDefaultSkillsDirIsConfigured(t *testing.T) {
	if Default().Skills.Dir != "skills" {
		t.Fatalf("default skills dir must point at bundled skills")
	}
	if Default().Skills.ConfigPath != "config/skills-config.yaml" {
		t.Fatalf("default skills config must point at the runtime config directory")
	}
}
