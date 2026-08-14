// Package constant centralizes string keys and default values used across the
// Agent Runtime so they are declared once instead of scattered as literals.
package constant

// gRPC metadata (header) keys used to resolve trace/auth context.
const (
	MetaKeyTraceID       = "x-trace-id"
	MetaKeyTraceparent   = "traceparent"
	MetaKeyAPIKey        = "x-api-key"
	MetaKeyAuthorization = "authorization"
)

// Chat message roles.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Model routing role keys (map keys in RunRequest.models).
const (
	ModelRoleDefault = "default"
)

// Stream protocol identifiers reported in MetaEvent.stream_protocol.
const (
	StreamProtocolGRPC = "grpc"
	StreamProtocolSSE  = "sse"
)

// SSE event names emitted by the HTTP gateway (mirror StreamEvent oneof).
const (
	EventMeta        = "meta"
	EventDelta       = "delta"
	EventToolCall    = "tool_call"
	EventTool        = "tool"
	EventInterrupted = "interrupted"
	EventError       = "error"
	EventDone        = "done"
	EventMessage     = "message"
)

// Environment variable names for server configuration.
const (
	EnvGRPCAddr                    = "GRPC_ADDR"
	EnvHTTPAddr                    = "HTTP_ADDR"
	EnvDefaultModel                = "DEFAULT_MODEL"
	EnvDefaultAPIKey               = "DEFAULT_API_KEY"
	EnvDefaultAPIBase              = "DEFAULT_API_BASE"
	EnvRuntimePublicURL            = "AGENT_RUNTIME_PUBLIC_URL"
	EnvRuntimeClientInternalURL    = "ATHENA_RUNTIME_CLIENT_INTERNAL_URL"
	EnvRuntimeClientGoalURL        = "ATHENA_RUNTIME_CLIENT_GOAL_URL"
	EnvInternalServiceToken        = "ATHENA_INTERNAL_SERVICE_TOKEN"
	EnvResearchCacheDir            = "ATHENA_RESEARCH_CACHE_DIR"
	EnvResearchProviders           = "ATHENA_RESEARCH_PROVIDERS"
	EnvResearchMaxQueries          = "ATHENA_RESEARCH_MAX_QUERIES"
	EnvResearchMaxPages            = "ATHENA_RESEARCH_MAX_PAGES"
	EnvResearchMaxRounds           = "ATHENA_RESEARCH_MAX_ROUNDS"
	EnvResearchTimeoutSec          = "ATHENA_RESEARCH_TIMEOUT_SEC"
	EnvResearchGitHubToken         = "ATHENA_GITHUB_TOKEN"
	EnvGitHubToken                 = "GITHUB_TOKEN"
	EnvPluginsEnabled              = "ATHENA_PLUGINS_ENABLED"
	EnvPluginsDir                  = "ATHENA_PLUGINS_DIR"
	EnvPluginRegistryPath          = "ATHENA_PLUGIN_REGISTRY_PATH"
	EnvPluginTrustStorePath        = "ATHENA_PLUGIN_TRUST_STORE_PATH"
	EnvPluginAuditPath             = "ATHENA_PLUGIN_AUDIT_PATH"
	EnvPluginRequireSignature      = "ATHENA_PLUGIN_REQUIRE_SIGNATURE"
	EnvOperationsMaxInflight       = "ATHENA_OPERATIONS_MAX_INFLIGHT"
	EnvOperationsMaxQueue          = "ATHENA_OPERATIONS_MAX_QUEUE"
	EnvOperationsAdmissionWaitMS   = "ATHENA_OPERATIONS_ADMISSION_WAIT_MS"
	EnvOperationsRequestTimeoutSec = "ATHENA_OPERATIONS_REQUEST_TIMEOUT_SEC"
	// EnvConfigPath points to the YAML config file (db + memory sections).
	EnvConfigPath = "AGENT_RUNTIME_CONFIG"
)

// Default listen addresses and config path.
const (
	DefaultGRPCAddr   = ":18080"
	DefaultHTTPAddr   = ":18081"
	DefaultConfigPath = "config.yaml"
)

// Context keys carried in RunRequest/AgentRequest.context (google.protobuf.Struct)
// used to scope memory to a session/user/agent.
const (
	ContextKeySessionID  = "session_id"
	ContextKeyUserID     = "user_id"
	ContextKeyAgentID    = "agent_id"
	ContextKeyProjectDir = "project_dir"
)

// HTTP gateway routes and headers.
const (
	RouteHealth               = "/healthz"
	RouteRun                  = "/run"
	RouteAgent                = "/agent"
	RouteGenerated            = "/generated"
	RouteCapabilities         = "/capabilities"
	RouteReady                = "/readyz"
	RouteGAReadiness          = "/readiness"
	RouteMetrics              = "/metrics"
	HeaderTraceID             = "X-Trace-Id"
	HeaderRequestID           = "X-Request-Id"
	HeaderCorrelationID       = "X-Correlation-Id"
	HeaderTraceparent         = "Traceparent"
	HeaderAthenaInternalToken = "X-Athena-Internal-Token"
)

// Agent identity used when constructing the ADK ChatModelAgent.
const (
	AgentName        = "main_agent"
	AgentDescription = "Main agent backed by an eino chat model"
)

// Runtime defaults.
const (
	Version                    = "1.0.0"
	DefaultMaxIterations       = 20
	DefaultParallelToolWorkers = 8
)

// Environment variable names for skill/sandbox directory resolution.
const (
	EnvAgentRuntimeHome = "AGENT_RUNTIME_HOME"
	EnvBaseDir          = "XQL_BASE_DIR"
	EnvSkillsDir        = "SKILLS_DIR"
	EnvGlobalSkillsDir  = "GLOBAL_SKILLS_DIR"
	EnvSkillsConfigPath = "SKILLS_CONFIG_PATH"
	EnvSandboxImage     = "SANDBOX_IMAGE"
)

// Base directory layout used for skills, reports and other runtime data.
const (
	DefaultBaseDirName        = ".agent-runtime"
	DefaultAthenaHomeDirName  = ".athena"
	DefaultAthenaTempDirName  = "athena"
	DirSkills                 = "skills"
	DirDataReports            = "data/reports"
	DirModels                 = "models"
	DirDiffusers              = "diffusers"
	DirImageRuntime           = "image-runtime"
	DirVenv                   = "venv"
	DirGeneratedImages        = "generated-images"
	SkillsConfigRelPath       = "config/skills-config.yaml"
	FallbackReportsDir        = "/tmp/reports"
	FallbackReportsURL        = "/reports"
	SkillMDFileName           = "SKILL.md"
	SkillScriptsDir           = "scripts"
	AutoDataFileName          = ".auto_data.json"
	DiffusersCompleteFileName = ".athena_complete"
	OllamaStartupLogFileName  = "athena-ollama.log"
	SkillScopeBoth            = "both"
)

// Sandbox execution defaults.
const (
	DefaultSandboxImage         = "alpine:latest"
	DefaultPptxImage            = "node:18-alpine"
	SandboxWorkdir              = "/workspace"
	DefaultSandboxTimeoutMs     = 120000
	DefaultSandboxExecTimeoutMs = 60000
)

// Timeout defaults (seconds unless noted).
const (
	DefaultBashTimeoutSec         = 30
	DefaultSkillExecTimeoutSec    = 120
	DefaultWebRequestTimeoutSec   = 30
	DefaultWebFetchCacheTTLMin    = 15
	DefaultSubAgentWaitTimeoutSec = 30
	MaxSubAgentWaitTimeoutSec     = 300
	DefaultParallelTaskTimeoutSec = 60
	DefaultPoolTimeoutSec         = 300
	DefaultResearchTimeoutSec     = 30
	DefaultProviderTimeoutSec     = 10
	DefaultCircuitOpenSec         = 120
)

// Miscellaneous runtime magic numbers.
const (
	DefaultSkillMaxIterations  = 30
	DefaultMaxReviewMemory     = 10
	DefaultResearchMaxQueries  = 6
	DefaultResearchMaxPages    = 8
	DefaultResearchMaxRounds   = 3
	DefaultResearchResults     = 5
	DefaultProviderFailures    = 3
	DefaultResearchCacheTTLMin = 60
	DefaultNewsCacheTTLMin     = 5
)

// External service endpoints.
const (
	DefaultOpenAIAPIBase                         = "https://api.openai.com/v1"
	DefaultOllamaAPIBase                         = "http://127.0.0.1:11434"
	DefaultRuntimePublicURL                      = "http://127.0.0.1:18081"
	DefaultRuntimeClientInternalScheduledTaskURL = "http://127.0.0.1:8090/api/agent-runtime-client/v1/internal/scheduled-task"
	DefaultRuntimeClientInternalGoalURL          = "http://127.0.0.1:8090/api/agent-runtime-client/v1/internal/goals"
	DuckDuckGoHTMLSearchURL                      = "https://html.duckduckgo.com/html/?q=%s"
	GitHubAPIBase                                = "https://api.github.com"
	WikipediaEnglishBase                         = "https://en.wikipedia.org"
	WikipediaChineseBase                         = "https://zh.wikipedia.org"
	ArxivAPIURL                                  = "https://export.arxiv.org/api/query"
	GDELTDocAPIURL                               = "https://api.gdeltproject.org/api/v2/doc/doc"
)

// Local model providers and lifecycle modes.
const (
	ProviderOllama    = "ollama"
	ProviderDiffusers = "diffusers"

	RuntimeModeAlwaysOn = "always_on"
	RuntimeModeOnDemand = "on_demand"
	RuntimeModeOff      = "off"
)

// Risk levels used by the approval engine.
const (
	RiskLevelLow    = "low"
	RiskLevelMedium = "medium"
	RiskLevelHigh   = "high"
)

// Tool type identifiers used for approval wrapping and diagnostics.
const (
	ToolTypeBuiltin = "builtin"
	ToolTypeSkill   = "skill"
	ToolTypeMCP     = "mcp"
	ToolTypeA2A     = "a2a"
	ToolTypeHTTP    = "http"
)
