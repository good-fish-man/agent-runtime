# Athena Agent Runtime

[English](README.md) | [简体中文](README.zh-CN.md)

Athena Agent Runtime is the execution and orchestration engine of the Athena agent platform. It accepts structured agent requests over gRPC or HTTP, selects relevant tools and skills, invokes language or image models, coordinates sub-agents, and streams typed execution events back to callers.

This repository contains the runtime only. User accounts, model bindings, agents, API keys, and the browser UI are managed by [`agent-runtime-client`](https://github.com/good-fish-man/agent-runtime-client) and [`athena-agent-ui`](https://github.com/good-fish-man/athena-agent-ui).

## Highlights

- gRPC API with an HTTP/SSE gateway.
- OpenAI-compatible model routing, including local Ollama models.
- Built-in file, shell, web, planning, task, and image-generation tools.
- Relevance-based tool and skill selection instead of sending every capability to the model.
- Skills, knowledge retrieval, uploaded-file context, and sub-agent orchestration.
- Project workspace tools for reading, searching, editing, and writing code.
- Optional PostgreSQL-backed long-term memory and background memory review.
- Local Diffusers image generation and generated-file serving.
- Trace propagation and source-aware error chains across HTTP, gRPC, tools, and models.
- Local-only administration endpoints for configuration, restart, and local-model lifecycle.

## Architecture

```mermaid
flowchart LR
    Client["Runtime Client or gRPC caller"] --> Transport["gRPC + HTTP/SSE gateway"]
    Transport --> Server["Runtime Server"]
    Server --> Dispatcher["Dispatcher"]
    Dispatcher --> Selector["Capability selector"]
    Selector --> Skills["Skills and retrieval"]
    Selector --> Tools["Built-in tools"]
    Selector --> SubAgents["Sub-agents"]
    Dispatcher --> Eino["Eino agent runner"]
    Eino --> Models["Cloud or local models"]
    Eino --> Tools
    Server <--> Memory["Optional PostgreSQL memory"]
    Transport --> Stream["Typed stream events"]
```

Request flow:

1. The transport layer accepts a request and propagates `X-Trace-Id` into gRPC metadata and context.
2. The server resolves model configuration and optional memory context.
3. The dispatcher selects relevant tools and up to the most relevant skills for the current request.
4. The Eino runner executes the model/tool loop and coordinates sub-agents when configured.
5. Results are returned as a completion or typed stream events such as `meta`, `delta`, `tool_call`, `error`, and `done`.

## Requirements

- Go 1.25 or newer.
- PostgreSQL only when the memory module is enabled.
- Docker only when sandboxed skills or commands are enabled.
- Ollama for local LLM/embedding models, or Python model dependencies for local Diffusers image models.

## Quick Start

```bash
git clone https://github.com/good-fish-man/agent-runtime.git
cd agent-runtime
cp config.yaml config.local.yaml
export DEFAULT_API_KEY="your-api-key"
AGENT_RUNTIME_CONFIG=config.local.yaml go run ./cmd/server
```

Default addresses:

| Service | Address |
| --- | --- |
| gRPC | `127.0.0.1:18080` |
| HTTP gateway | `http://127.0.0.1:18081` |
| Health check | `http://127.0.0.1:18081/healthz` |

Verify the service:

```bash
curl http://127.0.0.1:18081/healthz
```

For a complete local Athena installation, use [`athena-launcher`](https://github.com/good-fish-man/athena-launcher) instead of starting each service manually.

## Interfaces

### gRPC

The protocol is defined in [`proto/agent/runtime/v1/runtime.proto`](proto/agent/runtime/v1/runtime.proto). The main RPCs are:

- `Run` and `RunStream` for rich, configured runs.
- `RunAgent` and `RunAgentStream` for agent task execution.
- `Resume` and `Stop` for interruptible/checkpointed execution.
- `HealthCheck` for readiness checks.

See [`doc/grpc-client.md`](doc/grpc-client.md) for client examples.

### HTTP/SSE

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Runtime health |
| `POST` | `/run` | Rich completion or SSE run |
| `POST` | `/agent` | Agent completion or SSE run |
| `GET` | `/generated/*` | Generated image/file access |

Local administration routes are under `/admin/*` and reject non-loopback requests.

## Configuration

The default file is [`config.yaml`](config.yaml). Set `AGENT_RUNTIME_CONFIG` to use another file. Relative skill paths are resolved from the configuration file directory.

```yaml
server:
  grpc_addr: ":18080"
  http_addr: ":18081"
  default_model:
    provider: "openai"
    name: "gpt-4o-mini"
    api_key: "${DEFAULT_API_KEY}"
    api_base: "https://api.openai.com/v1"

memory:
  enabled: false
  auto_migrate: true

skills:
  dir: "skills"
  config_path: "config/skills-config.yaml"
```

Environment overrides:

| Variable | Meaning |
| --- | --- |
| `AGENT_RUNTIME_CONFIG` | Configuration file path |
| `GRPC_ADDR`, `HTTP_ADDR` | Listen addresses |
| `DEFAULT_MODEL`, `DEFAULT_API_KEY`, `DEFAULT_API_BASE` | Fallback model settings |
| `SKILLS_DIR`, `GLOBAL_SKILLS_DIR`, `SKILLS_CONFIG_PATH` | Skill locations |
| `SANDBOX_IMAGE` | Default sandbox image |

Do not commit API keys. In the full platform, model credentials are resolved by `agent-runtime-client` and sent only with server-to-server requests.

## Built-in Capabilities

The runtime can select `Glob`, `Grep`, `Read`, `Edit`, `Write`, `Bash`, web search/fetch, planning, task, and question tools. Bundled skills currently include browser automation, CSV analysis, MarkItDown, PowerPoint generation, S3 upload, and skill creation.

Tools and skills are selected from the current prompt and recent context. Filesystem tools are scoped to the request's `project_dir`.

## Development

```bash
go test ./...
go vet ./...
go build -o bin/server ./cmd/server
```

Generate protobuf code after changing the protocol using your normal `protoc` toolchain, then commit both the `.proto` file and generated Go files.

## Observability

Inbound trace headers are propagated through HTTP, gRPC, database/model calls, and stream events. Errors are wrapped at each layer and logged once at the transport boundary with operation names and source locations. Pass `X-Trace-Id`, `X-Request-Id`, or `X-Correlation-Id` to correlate a request across services.

## Related Projects

- [`agent-runtime-client`](https://github.com/good-fish-man/agent-runtime-client): HTTP API, users, agents, model bindings, and persistence.
- [`athena-agent-ui`](https://github.com/good-fish-man/athena-agent-ui): browser management and chat interface.
- [`athena-launcher`](https://github.com/good-fish-man/athena-launcher): desktop installer and local service manager.

## License

Add a repository license before redistributing Athena or bundled third-party skills. Individual bundled skills may include their own license files.
