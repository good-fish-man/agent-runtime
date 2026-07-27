# Athena Agent Runtime

Athena 的模型执行引擎，提供 gRPC 与 HTTP/SSE 接口，负责 Agent 调度、工具调用、Skills、Sub-Agents、记忆和本地模型能力。

## Requirements

- Go 1.25+
- PostgreSQL（仅启用 memory 模块时需要）
- Docker（仅沙箱工具需要）

## Run

```bash
cp config.yaml config.local.yaml
AGENT_RUNTIME_CONFIG=config.local.yaml go run ./cmd/server
```

默认监听：

- gRPC: `:18080`
- HTTP: `:18081`
- Health: `http://127.0.0.1:18081/healthz`

生产环境请通过私有配置文件或环境变量提供模型凭据，不要把 API Key 提交到仓库。

## Test

```bash
go test ./...
```

## Related Projects

- [agent-runtime-client](https://github.com/good-fish-man/agent-runtime-client)
- [athena-agent-ui](https://github.com/good-fish-man/athena-agent-ui)
- [athena-launcher](https://github.com/good-fish-man/athena-launcher)
