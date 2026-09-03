# Entry Points and Transport

## Purpose

This layer turns operating-system processes and network requests into traced, admitted Runtime calls. It owns startup and shutdown, but it does not decide which capabilities an agent should use.

## Main Locations

| Location | Responsibility |
| --- | --- |
| [`cmd/server/main.go`](../../cmd/server/main.go) | Production process composition and lifecycle |
| [`cmd/server/request_log.go`](../../cmd/server/request_log.go) | HTTP request and response logging |
| [`cmd/server/trace_http.go`](../../cmd/server/trace_http.go) | HTTP trace extraction and propagation |
| [`cmd/client/main.go`](../../cmd/client/main.go) | Small gRPC client and usage example |
| [`cmd/v03-evidence-audit/main.go`](../../cmd/v03-evidence-audit/main.go) | Standalone release-evidence scanner command |
| [`internal/server`](../../internal/server) | gRPC service implementation and request mapping |
| [`internal/operations/transport.go`](../../internal/operations/transport.go) | Admission wrappers for gRPC and HTTP |

## Server Startup

`cmd/server/main.go` is the composition root. It is the one place where infrastructure objects are assembled.

The startup sequence is:

1. Resolve the configuration path and load YAML plus environment overrides.
2. Create the operations gate that limits concurrency, queueing, and deadlines.
3. Load signed Capability Providers into the capability registry.
4. Construct the research executor and dispatcher configuration.
5. Connect PostgreSQL and initialize memory only when configured.
6. Construct `internal/server.Server`.
7. Start the gRPC server with trace and operations interceptors.
8. Start the HTTP gateway, health, readiness, metrics, generated-file, and local admin routes.
9. Wait for a signal, restart request, or fatal listener error.
10. Enter drain mode, stop accepting work, and shut down gracefully.

The process can reload configuration through its restart loop. Keep new process-wide dependencies in this composition root instead of creating hidden global clients inside request packages.

## Network Interfaces

### gRPC

The generated `AgentRuntimeServer` is implemented by [`internal/server/server.go`](../../internal/server/server.go). Important RPC families are:

| RPC | Intended use |
| --- | --- |
| `Run` | Rich non-streaming execution with explicit runtime configuration |
| `RunStream` | Rich streaming execution with typed events |
| `RunAgent` | Simplified agent task execution |
| `RunAgentStream` | Simplified streaming agent task execution |
| `Resume` | Resume an interruptible or approval-gated run |
| `Stop` | Cancel an active run |
| `HealthCheck` | Runtime health probe |

The source protocol lives in [`proto/agent/runtime/v1/runtime.proto`](../../proto/agent/runtime/v1/runtime.proto). Generated Go files live under [`gen/agent/runtime/v1`](../../gen/agent/runtime/v1).

### HTTP and SSE

The HTTP gateway exposes equivalent run and agent operations. Streaming requests are rendered as Server-Sent Events so web clients can consume typed progress without speaking gRPC.

The transport is responsible for:

- decoding the HTTP payload;
- selecting streaming versus non-streaming behavior;
- preserving response flushing;
- returning exactly one terminal error or completion;
- keeping the request context alive until the stream ends;
- logging status, byte count, and elapsed time.

It must not reinterpret model output or execute tools.

## Trace Propagation

HTTP accepts the supported trace headers and stores the resolved trace ID in `context.Context`. gRPC interceptors do the same for metadata. Downstream packages call `log.ReqID(ctx)` or pass the same context into model, tool, provider, database, and observation operations.

The trace contract is:

```text
incoming trace header
  -> request context
  -> gRPC metadata or internal call
  -> model/tool/provider spans
  -> stream metadata and terminal response
```

Do not use process-global request IDs. Concurrent requests must remain isolated through context propagation.

## Admission and Shutdown

Before a run enters the server, [`internal/operations.Gate`](../../internal/operations/gate.go) may:

- admit it immediately;
- queue it within the configured bound;
- apply a server-side deadline;
- reject it because the queue is full;
- reject it because the service is draining.

Shutdown first calls `Drain`. This prevents new long-running model calls while allowing admitted calls a grace period. Health and readiness endpoints remain separate from admitted business methods so an orchestrator can observe shutdown state.

## Failure Boundary

Internal layers wrap errors with operation names and source information. The transport boundary is where a failed request should normally be logged as a completed failure. Logging the same error at every return site creates duplicate noise and obscures the causal chain.

Expected transport behavior:

| Failure | Result |
| --- | --- |
| Invalid request | Client error without model invocation |
| Admission rejection | Resource-exhausted or unavailable response |
| Client cancellation | Cancellation propagated to research, model, and tools |
| Internal failure | Wrapped error logged once with trace and elapsed time |
| Partial stream then failure | One terminal error event, never an additional final answer |

## Safe Extension Points

Add a new network route in `cmd/server` only when it is a process-level API. Put reusable behavior in an `internal` package and keep the route thin.

Add a new gRPC field or RPC by changing the `.proto`, regenerating `gen`, updating [`internal/server/dispatch.go`](../../internal/server/dispatch.go), and adding transport tests.

Add a process dependency by wiring it in `cmd/server/main.go` and passing an interface or configuration into the owning package.

## Tests

| Test area | Files |
| --- | --- |
| HTTP traces and logging | `cmd/server/trace_http_test.go`, `request_log_test.go` |
| SSE behavior | `cmd/server/sse_test.go` |
| gRPC request mapping | `internal/server/dispatch_test.go` |
| gRPC traces | `internal/server/trace_interceptor_test.go` |
| Admission and drain | `internal/operations/gate_test.go` |
