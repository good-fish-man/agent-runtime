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

func TestLoadResolvesIntentLanguagePacksRelativeToConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("intent:\n  language_packs_dir: locale-packs\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "locale-packs"); cfg.Intent.LanguagePacksDir != want {
		t.Fatalf("intent language packs dir = %q, want %q", cfg.Intent.LanguagePacksDir, want)
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

func TestSemanticIntentDefaultsAreBounded(t *testing.T) {
	configured := Default().Intent
	if configured.Mode != "hybrid" || configured.TimeoutMS <= 0 || configured.TimeoutMS > 10000 {
		t.Fatalf("invalid semantic intent defaults: %+v", configured)
	}
	if configured.MinConfidence <= 0.5 || configured.MinConfidence > 1 || configured.MaxHistory <= 0 || configured.MaxHistory > 8 {
		t.Fatalf("unbounded semantic intent defaults: %+v", configured)
	}
}

func TestLoadRejectsInvalidSemanticIntentConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("intent:\n  mode: guess\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("invalid semantic intent mode was accepted")
	}
}

func TestDatabaseEnvironmentOverridesKeepCredentialsOutOfConfig(t *testing.T) {
	t.Setenv(constant.EnvDBEnabled, "true")
	t.Setenv(constant.EnvDBType, "postgres")
	t.Setenv(constant.EnvDBHost, "database.internal")
	t.Setenv(constant.EnvDBPort, "5544")
	t.Setenv(constant.EnvDBUser, "athena-runtime")
	t.Setenv(constant.EnvDBPassword, "runtime-secret-from-environment")
	t.Setenv(constant.EnvDBName, "athena")
	t.Setenv(constant.EnvDBSSLMode, "require")
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DB.Enabled || cfg.DB.DBType != "postgres" || cfg.DB.DBHost != "database.internal" || cfg.DB.DBPort != 5544 {
		t.Fatalf("database endpoint overrides = %+v", cfg.DB)
	}
	if cfg.DB.Username != "athena-runtime" || cfg.DB.Password != "runtime-secret-from-environment" || cfg.DB.DBName != "athena" || cfg.DB.SSLMode != "require" {
		t.Fatalf("database credential overrides = %+v", cfg.DB)
	}
}

func TestPluginDefaultsAreSignedAndAbsolute(t *testing.T) {
	plugins := Default().Plugins
	if !plugins.Enabled || !plugins.RequireSignature {
		t.Fatalf("plugins must default to enabled and signed: %+v", plugins)
	}
	for _, path := range []string{plugins.Dir, plugins.RegistryPath, plugins.TrustStorePath, plugins.AuditPath} {
		if !filepath.IsAbs(path) {
			t.Fatalf("plugin path must be absolute: %s", path)
		}
	}
}
