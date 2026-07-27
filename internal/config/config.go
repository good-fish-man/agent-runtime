// Package config loads the Agent Runtime configuration from a YAML file
// (server / db / memory sections) with environment-variable overrides. The
// YAML layout mirrors the XiaoQinglong agent-frame config so operators can
// reuse familiar conventions.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/good-fish-man/agent-runtime/internal/constant"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration.
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	DB      DBConfig      `yaml:"db"`
	Memory  MemoryConfig  `yaml:"memory"`
	Sandbox SandboxConfig `yaml:"sandbox"`
	Skills  SkillsConfig  `yaml:"skills"`
}

// ServerConfig holds listen addresses and default model settings.
type ServerConfig struct {
	GRPCAddr string      `yaml:"grpc_addr"`
	HTTPAddr string      `yaml:"http_addr"`
	Model    ModelConfig `yaml:"default_model"`
}

// ModelConfig is the default model used when a request omits models.
type ModelConfig struct {
	Provider string `yaml:"provider"`
	Name     string `yaml:"name"`
	APIKey   string `yaml:"api_key"`
	APIBase  string `yaml:"api_base"`
}

// DBConfig configures the PostgreSQL connection (gorm).
type DBConfig struct {
	Enabled         bool   `yaml:"enabled"`
	DBType          string `yaml:"db_type"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	DBHost          string `yaml:"db_host"`
	DBPort          int    `yaml:"db_port"`
	DBName          string `yaml:"db_name"`
	SSLMode         string `yaml:"ssl_mode"`
	MaxOpenConn     int    `yaml:"max_open_conn"`
	MaxIdleConn     int    `yaml:"max_idle_conn"`
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"` // seconds
	LogMode         int    `yaml:"log_mode"`
}

// MemoryConfig configures the memory module behaviour.
type MemoryConfig struct {
	Enabled          bool `yaml:"enabled"`
	AutoMigrate      bool `yaml:"auto_migrate"`
	BackgroundReview bool `yaml:"background_review"`
	MaxReviewMemory  int  `yaml:"max_review_memory"`
	InjectIntoPrompt bool `yaml:"inject_into_prompt"`
}

// SandboxConfig holds operator-tunable defaults for skill sandbox execution.
// Per-request Sandbox settings still take precedence when provided.
type SandboxConfig struct {
	DefaultImage string `yaml:"default_image"`
	PptxImage    string `yaml:"pptx_image"`
	Workdir      string `yaml:"workdir"`
	TimeoutMs    int    `yaml:"timeout_ms"`
}

// SkillsConfig configures where skills and skill config are located.
type SkillsConfig struct {
	Dir        string `yaml:"dir"`         // overrides skill discovery directory
	ConfigPath string `yaml:"config_path"` // skills-config.yaml path
	GlobalDir  string `yaml:"global_dir"`  // additional skills directory to scan
}

// Default returns a Config populated with sane defaults.
func Default() Config {
	return Config{
		Server: ServerConfig{
			GRPCAddr: constant.DefaultGRPCAddr,
			HTTPAddr: constant.DefaultHTTPAddr,
		},
		DB: DBConfig{
			Enabled:         false,
			DBType:          "postgres",
			DBHost:          "localhost",
			DBPort:          5432,
			DBName:          "agent_runtime",
			SSLMode:         "disable",
			MaxOpenConn:     50,
			MaxIdleConn:     10,
			ConnMaxLifetime: 500,
			LogMode:         4,
		},
		Memory: MemoryConfig{
			Enabled:          false,
			AutoMigrate:      true,
			BackgroundReview: true,
			MaxReviewMemory:  constant.DefaultMaxReviewMemory,
			InjectIntoPrompt: true,
		},
		Sandbox: SandboxConfig{
			DefaultImage: constant.DefaultSandboxImage,
			PptxImage:    constant.DefaultPptxImage,
			Workdir:      constant.SandboxWorkdir,
			TimeoutMs:    constant.DefaultSandboxTimeoutMs,
		},
		Skills: SkillsConfig{Dir: constant.DirSkills, ConfigPath: constant.SkillsConfigRelPath},
	}
}

// ResolvePath returns the exact configuration path used by Load.
func ResolvePath(path string) string {
	if path == "" {
		path = getenv(constant.EnvConfigPath, constant.DefaultConfigPath)
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// Load reads configuration from path (if it exists), then applies environment
// overrides. A missing file is not an error: defaults + env are used instead.
func Load(path string) (Config, error) {
	cfg := Default()
	path = ResolvePath(path)

	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %q: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return cfg, fmt.Errorf("read config %q: %w", path, err)
	}

	applyEnvOverrides(&cfg)
	resolveSkillPaths(&cfg, path)
	return cfg, nil
}

// resolveSkillPaths makes relative skill paths stable regardless of the process
// working directory. Paths in config are interpreted relative to config.yaml.
func resolveSkillPaths(cfg *Config, configPath string) {
	baseDir := "."
	if configPath != "" {
		if abs, err := filepath.Abs(configPath); err == nil {
			baseDir = filepath.Dir(abs)
		}
	}
	for _, target := range []*string{&cfg.Skills.Dir, &cfg.Skills.ConfigPath, &cfg.Skills.GlobalDir} {
		if *target != "" && !filepath.IsAbs(*target) {
			*target = filepath.Clean(filepath.Join(baseDir, *target))
		}
	}
}

// applyEnvOverrides lets environment variables win over the YAML file so
// deployments can inject secrets without editing files.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv(constant.EnvGRPCAddr); v != "" {
		cfg.Server.GRPCAddr = v
	}
	if v := os.Getenv(constant.EnvHTTPAddr); v != "" {
		cfg.Server.HTTPAddr = v
	}
	if v := os.Getenv(constant.EnvDefaultModel); v != "" {
		cfg.Server.Model.Name = v
	}
	if v := os.Getenv(constant.EnvDefaultAPIKey); v != "" {
		cfg.Server.Model.APIKey = v
	}
	if v := os.Getenv(constant.EnvDefaultAPIBase); v != "" {
		cfg.Server.Model.APIBase = v
	}
	if v := os.Getenv(constant.EnvSandboxImage); v != "" {
		cfg.Sandbox.DefaultImage = v
	}
	if v := os.Getenv(constant.EnvSkillsDir); v != "" {
		cfg.Skills.Dir = v
	}
	if v := os.Getenv(constant.EnvSkillsConfigPath); v != "" {
		cfg.Skills.ConfigPath = v
	}
	if v := os.Getenv(constant.EnvGlobalSkillsDir); v != "" {
		cfg.Skills.GlobalDir = v
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// DSN builds a PostgreSQL DSN from the DB config.
func (d DBConfig) DSN() string {
	sslMode := d.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		d.DBHost, strconv.Itoa(d.DBPort), d.Username, d.Password, d.DBName, sslMode,
	)
}
