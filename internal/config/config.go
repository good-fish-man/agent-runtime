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
	"strings"

	"github.com/good-fish-man/agent-runtime/internal/constant"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration.
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	DB         DBConfig         `yaml:"db"`
	Memory     MemoryConfig     `yaml:"memory"`
	Intent     IntentConfig     `yaml:"intent"`
	Research   ResearchConfig   `yaml:"research"`
	Sandbox    SandboxConfig    `yaml:"sandbox"`
	Skills     SkillsConfig     `yaml:"skills"`
	Plugins    PluginsConfig    `yaml:"plugins"`
	Operations OperationsConfig `yaml:"operations"`
}

// IntentConfig controls deterministic language packs and the optional
// model-assisted intent classifier.
type IntentConfig struct {
	Mode             string  `yaml:"mode"`
	TimeoutMS        int     `yaml:"timeout_ms"`
	MinConfidence    float64 `yaml:"min_confidence"`
	MaxHistory       int     `yaml:"max_history"`
	LanguagePacksDir string  `yaml:"language_packs_dir"`
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

// ResearchConfig controls the code-owned multi-source research loop.
type ResearchConfig struct {
	Enabled                  bool     `yaml:"enabled"`
	Providers                []string `yaml:"providers"`
	MaxQueries               int      `yaml:"max_queries"`
	MaxPages                 int      `yaml:"max_pages"`
	MaxRounds                int      `yaml:"max_rounds"`
	ResultsPerQuery          int      `yaml:"results_per_query"`
	TimeoutSec               int      `yaml:"timeout_sec"`
	CacheDir                 string   `yaml:"cache_dir"`
	CacheTTLMin              int      `yaml:"cache_ttl_min"`
	NewsCacheTTLMin          int      `yaml:"news_cache_ttl_min"`
	ProviderTimeoutSec       int      `yaml:"provider_timeout_sec"`
	ProviderFailureThreshold int      `yaml:"provider_failure_threshold"`
	CircuitOpenSec           int      `yaml:"circuit_open_sec"`
	ModelPlanning            bool     `yaml:"model_planning"`
	SemanticVerification     bool     `yaml:"semantic_verification"`
	AdvisorTimeoutSec        int      `yaml:"advisor_timeout_sec"`
	MaxAdvisorClaims         int      `yaml:"max_advisor_claims"`
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

// PluginsConfig controls the fail-closed signed Capability Provider loader.
type PluginsConfig struct {
	Enabled          bool   `yaml:"enabled"`
	Dir              string `yaml:"dir"`
	RegistryPath     string `yaml:"registry_path"`
	TrustStorePath   string `yaml:"trust_store_path"`
	AuditPath        string `yaml:"audit_path"`
	RequireSignature bool   `yaml:"require_signature"`
}

// OperationsConfig bounds concurrent work and request lifetime.
type OperationsConfig struct {
	MaxInflight       int `yaml:"max_inflight"`
	MaxQueue          int `yaml:"max_queue"`
	AdmissionWaitMS   int `yaml:"admission_wait_ms"`
	RequestTimeoutSec int `yaml:"request_timeout_sec"`
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
			Enabled:          true,
			AutoMigrate:      true,
			BackgroundReview: true,
			MaxReviewMemory:  constant.DefaultMaxReviewMemory,
			InjectIntoPrompt: true,
		},
		Intent: IntentConfig{
			Mode:          "hybrid",
			TimeoutMS:     4000,
			MinConfidence: 0.75,
			MaxHistory:    4,
		},
		Research: ResearchConfig{
			Enabled:                  true,
			Providers:                []string{"web", "github", "wikipedia", "arxiv", "news"},
			MaxQueries:               constant.DefaultResearchMaxQueries,
			MaxPages:                 constant.DefaultResearchMaxPages,
			MaxRounds:                constant.DefaultResearchMaxRounds,
			ResultsPerQuery:          constant.DefaultResearchResults,
			TimeoutSec:               constant.DefaultResearchTimeoutSec,
			CacheDir:                 defaultResearchCacheDir(),
			CacheTTLMin:              constant.DefaultResearchCacheTTLMin,
			NewsCacheTTLMin:          constant.DefaultNewsCacheTTLMin,
			ProviderTimeoutSec:       constant.DefaultProviderTimeoutSec,
			ProviderFailureThreshold: constant.DefaultProviderFailures,
			CircuitOpenSec:           constant.DefaultCircuitOpenSec,
			ModelPlanning:            true,
			SemanticVerification:     true,
			AdvisorTimeoutSec:        8,
			MaxAdvisorClaims:         8,
		},
		Sandbox: SandboxConfig{
			DefaultImage: constant.DefaultSandboxImage,
			PptxImage:    constant.DefaultPptxImage,
			Workdir:      constant.SandboxWorkdir,
			TimeoutMs:    constant.DefaultSandboxTimeoutMs,
		},
		Skills:     SkillsConfig{Dir: constant.DirSkills, ConfigPath: constant.SkillsConfigRelPath},
		Plugins:    defaultPluginsConfig(),
		Operations: OperationsConfig{MaxInflight: 32, MaxQueue: 64, AdmissionWaitMS: 2000, RequestTimeoutSec: 300},
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
	if err := validateIntentConfig(cfg.Intent); err != nil {
		return cfg, err
	}
	resolvePaths(&cfg, path)
	return cfg, nil
}

func validateIntentConfig(config IntentConfig) error {
	switch strings.ToLower(strings.TrimSpace(config.Mode)) {
	case "rules", "shadow", "hybrid":
	default:
		return fmt.Errorf("intent.mode must be rules, shadow, or hybrid")
	}
	if config.TimeoutMS <= 0 || config.TimeoutMS > 60000 {
		return fmt.Errorf("intent.timeout_ms must be between 1 and 60000")
	}
	if config.MinConfidence <= 0 || config.MinConfidence > 1 {
		return fmt.Errorf("intent.min_confidence must be greater than 0 and at most 1")
	}
	if config.MaxHistory <= 0 || config.MaxHistory > 20 {
		return fmt.Errorf("intent.max_history must be between 1 and 20")
	}
	return nil
}

// resolvePaths makes relative resource paths stable regardless of the process
// working directory. Paths in config are interpreted relative to config.yaml.
func resolvePaths(cfg *Config, configPath string) {
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
	for _, target := range []*string{&cfg.Plugins.Dir, &cfg.Plugins.RegistryPath, &cfg.Plugins.TrustStorePath, &cfg.Plugins.AuditPath} {
		if *target != "" && !filepath.IsAbs(*target) {
			*target = filepath.Clean(filepath.Join(baseDir, *target))
		}
	}
	if cfg.Intent.LanguagePacksDir != "" && !filepath.IsAbs(cfg.Intent.LanguagePacksDir) {
		cfg.Intent.LanguagePacksDir = filepath.Clean(filepath.Join(baseDir, cfg.Intent.LanguagePacksDir))
	}
	if cfg.Research.CacheDir != "" && !filepath.IsAbs(cfg.Research.CacheDir) {
		cfg.Research.CacheDir = filepath.Clean(filepath.Join(baseDir, cfg.Research.CacheDir))
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
	applyBoolEnv(constant.EnvPluginsEnabled, &cfg.Plugins.Enabled)
	applyBoolEnv(constant.EnvPluginRequireSignature, &cfg.Plugins.RequireSignature)
	if v := os.Getenv(constant.EnvPluginsDir); v != "" {
		cfg.Plugins.Dir = v
	}
	if v := os.Getenv(constant.EnvPluginRegistryPath); v != "" {
		cfg.Plugins.RegistryPath = v
	}
	if v := os.Getenv(constant.EnvPluginTrustStorePath); v != "" {
		cfg.Plugins.TrustStorePath = v
	}
	if v := os.Getenv(constant.EnvPluginAuditPath); v != "" {
		cfg.Plugins.AuditPath = v
	}
	applyPositiveIntEnv(constant.EnvOperationsMaxInflight, &cfg.Operations.MaxInflight)
	applyNonNegativeIntEnv(constant.EnvOperationsMaxQueue, &cfg.Operations.MaxQueue)
	applyPositiveIntEnv(constant.EnvOperationsAdmissionWaitMS, &cfg.Operations.AdmissionWaitMS)
	applyPositiveIntEnv(constant.EnvOperationsRequestTimeoutSec, &cfg.Operations.RequestTimeoutSec)
	if v := os.Getenv(constant.EnvResearchCacheDir); v != "" {
		cfg.Research.CacheDir = v
	}
	if v := os.Getenv(constant.EnvResearchProviders); v != "" {
		cfg.Research.Providers = splitCSV(v)
	}
	applyPositiveIntEnv(constant.EnvResearchMaxQueries, &cfg.Research.MaxQueries)
	applyPositiveIntEnv(constant.EnvResearchMaxPages, &cfg.Research.MaxPages)
	applyPositiveIntEnv(constant.EnvResearchMaxRounds, &cfg.Research.MaxRounds)
	applyPositiveIntEnv(constant.EnvResearchTimeoutSec, &cfg.Research.TimeoutSec)
	applyBoolEnv(constant.EnvDBEnabled, &cfg.DB.Enabled)
	if v := os.Getenv(constant.EnvDBType); v != "" {
		cfg.DB.DBType = v
	}
	if v := os.Getenv(constant.EnvDBHost); v != "" {
		cfg.DB.DBHost = v
	}
	applyPositiveIntEnv(constant.EnvDBPort, &cfg.DB.DBPort)
	if v := os.Getenv(constant.EnvDBUser); v != "" {
		cfg.DB.Username = v
	}
	if v := os.Getenv(constant.EnvDBPassword); v != "" {
		cfg.DB.Password = v
	}
	if v := os.Getenv(constant.EnvDBName); v != "" {
		cfg.DB.DBName = v
	}
	if v := os.Getenv(constant.EnvDBSSLMode); v != "" {
		cfg.DB.SSLMode = v
	}
}

func defaultPluginsConfig() PluginsConfig {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = filepath.Join(os.TempDir(), constant.DefaultAthenaTempDirName)
	} else {
		home = filepath.Join(home, constant.DefaultAthenaHomeDirName)
	}
	dir := filepath.Join(home, "plugins")
	return PluginsConfig{
		Enabled: true, Dir: dir, RegistryPath: filepath.Join(dir, "registry.json"),
		TrustStorePath: filepath.Join(dir, "trust-store.json"),
		AuditPath:      filepath.Join(home, "logs", "plugin-invocations.jsonl"), RequireSignature: true,
	}
}

func defaultResearchCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "Athena", "research")
	}
	return filepath.Join(os.TempDir(), "athena", "research-cache")
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func applyPositiveIntEnv(name string, target *int) {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			*target = parsed
		}
	}
}

func applyNonNegativeIntEnv(name string, target *int) {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 {
			*target = parsed
		}
	}
}

func applyBoolEnv(name string, target *bool) {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			*target = parsed
		}
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
