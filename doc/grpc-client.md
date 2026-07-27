# Agent Runtime gRPC 测试客户端

`cmd/client` 是一个 Go gRPC 客户端 CLI，用于本地调试和观察 `AgentRuntime` 服务的各个接口。

## 背景：调用入口在哪里？

- **服务端入口**：`cmd/server/main.go` —— `RegisterAgentRuntimeServer` 注册 gRPC 服务，默认监听 `:18080`；同时启动 HTTP/SSE 网关 `:18081`（`/healthz`、`/run`、`/agent`）。
- **服务定义**：`proto/agent/runtime/v1/runtime.proto`，生成代码在 `gen/agent/runtime/v1`。
- **服务实现**：`internal/server/server.go`。
- 仓库原先**只有服务端、没有客户端**，所以找不到"调用入口"。本工具补齐了这一环。

## 1. 启动服务端

```bash
cd agent-runtime

# 可选：配置默认模型（Run / RunAgent 需要模型才能真正生成）
export DEFAULT_MODEL=gpt-4o-mini
export DEFAULT_API_KEY=sk-xxxx
export DEFAULT_API_BASE=https://api.openai.com/v1   # 兼容 OpenAI 的地址

go run ./cmd/server
# gRPC server listening on :18080
# HTTP gateway listening on :18081
```

也可用 `GRPC_ADDR` / `HTTP_ADDR` 改监听地址。

## 2. 运行客户端

另开一个终端：

```bash
cd agent-runtime
go run ./cmd/client <subcommand> [flags]
```

或先编译成二进制：

```bash
go build -o bin/agent-client ./cmd/client
./bin/agent-client health
```

## 3. 子命令

| 子命令 | 对应 RPC | 说明 |
|--------|----------|------|
| `health` | `HealthCheck` | 探活，验证连通性（无需模型） |
| `run` | `Run` | 单轮补全，非流式 |
| `run-stream` | `RunStream` | 单轮补全，流式（逐 delta 打印） |
| `agent` | `RunAgent` | 自然语言任务，非流式 |
| `agent-stream` | `RunAgentStream` | 自然语言任务，流式 |
| `resume` | `Resume` | 目前服务端返回 `Unimplemented` |
| `stop` | `Stop` | 目前服务端返回 `Unimplemented` |

### 通用 flags（所有子命令）

| flag | 默认值 | 说明 |
|------|--------|------|
| `-addr` | `localhost:18080`（或 `GRPC_ADDR`） | gRPC 服务地址 |
| `-timeout` | `60s` | 请求超时 |
| `-trace-id` | 空 | 透传 `trace_id` |
| `-model` | `DEFAULT_MODEL` | 模型名 → `models["default"]` |
| `-api-key` | `DEFAULT_API_KEY` | 模型 API Key |
| `-api-base` | `DEFAULT_API_BASE` | 模型 API Base URL |

> 若 `-model`/`-api-key`/`-api-base` 全为空，则不发送 `models`，服务端会回退到自身的 `DEFAULT_MODEL` 环境变量；两者都没有时 `run`/`agent` 会返回 `InvalidArgument: no model configured`。

## 4. 示例

```bash
# 探活（不需要模型）
go run ./cmd/client health

# 单轮非流式（服务端已配 DEFAULT_MODEL，或用 flag 覆盖）
go run ./cmd/client run -prompt "用一句话解释什么是 gRPC"

# 用 flag 显式指定模型
go run ./cmd/client run -prompt "hello" \
  -model gpt-4o-mini -api-key sk-xxx -api-base https://api.openai.com/v1

# 单轮流式（实时看到 delta）
go run ./cmd/client run-stream -prompt "写一首关于秋天的短诗"

# 自然语言任务
go run ./cmd/client agent -task "帮我列出快速排序的步骤"
go run ./cmd/client agent-stream -task "帮我列出快速排序的步骤"

# 观察 Unimplemented 错误路径
go run ./cmd/client resume -checkpoint-id abc
go run ./cmd/client stop -checkpoint-id abc
```

## 5. 输出说明（命令行）

- 非流式：以 `protojson`（缩进）打印完整响应，含 `content`、`finish_reason`、token 用量、`trace_id`、`metadata`。
- 流式：
  - `[meta]` 起始事件（协议、开始时间）
  - delta 文本直接拼接输出
  - `[tool_call]` / `[tool_result]` / `[interrupted]` / `[error]` 事件单独一行
  - `[done]` 结束事件（finish_reason、token 用量、延迟）

## 6. 调用测试（点击运行）

除了命令行，还提供了 `cmd/client/client_test.go` —— 一组**可点击运行的测试函数**，在 IDE（GoLand / VS Code）里点某个测试左侧的 ▶ 就能直接发起一次真实 gRPC 请求并观察响应。

### 前置

先启动服务端（另一个终端）：

```bash
go run ./cmd/server
```

> 服务端未启动时，测试会自动 **SKIP**（不会 FAIL）。

### 在哪里改参数

打开 `cmd/client/client_test.go`，顶部集中定义了所有可编辑参数，改一处即全局生效：

```go
var (
    testAddr    = getenv(constant.EnvGRPCAddr, "localhost:18080") // 或用 GRPC_ADDR
    testTimeout = 60 * time.Second
    testTraceID = ""

    testModel   = getenv(constant.EnvDefaultModel, "")   // 或直接写死 "gpt-4o-mini"
    testAPIKey  = getenv(constant.EnvDefaultAPIKey, "")
    testAPIBase = getenv(constant.EnvDefaultAPIBase, "")

    testPrompt = "用一句话解释什么是 gRPC"
    testTask   = "帮我列出快速排序的步骤"
    ...
)
```

### 测试函数一览

| 测试函数 | 对应 RPC |
|----------|----------|
| `TestHealth` | HealthCheck |
| `TestRun` | Run |
| `TestRunStream` | RunStream（逐 delta 打印） |
| `TestAgent` | RunAgent |
| `TestAgentStream` | RunAgentStream |
| `TestResume` | Resume（观察 Unimplemented） |
| `TestStop` | Stop（观察 Unimplemented） |

### 运行方式

- **IDE**：点击某个 `TestXxx` 左侧的运行按钮，即发起该请求；响应打印在测试输出里。
- **命令行**：

```bash
# 跑单个
go test -run TestHealth -v ./cmd/client

# 跑全部（需要模型的 Run/Agent 未配模型时会因 InvalidArgument 失败，属正常）
go test -v ./cmd/client

# 临时指定模型跑一次 Run
DEFAULT_MODEL=gpt-4o-mini DEFAULT_API_KEY=sk-xxx DEFAULT_API_BASE=https://api.openai.com/v1 \
  go test -run TestRun -v ./cmd/client
```

> 注意：`Run` / `RunAgent` 需要有效模型，没配模型点击运行会得到 `InvalidArgument: no model configured`，这是预期的——填上 `testModel` 等参数即可真正发起补全。`Health` / `Resume` / `Stop` 无需模型即可观察。

## 7. 对照：HTTP 网关（可选）

同样的能力也暴露在 HTTP/SSE 网关上，可用 curl 直接调：

```bash
curl localhost:18081/healthz
curl -X POST localhost:18081/run   -d '{"prompt":"hi"}'
curl -X POST localhost:18081/agent -d '{"task":"hi"}'
# 流式：请求体里 options.stream=true（/run）或 stream=true（/agent），响应为 SSE
```
