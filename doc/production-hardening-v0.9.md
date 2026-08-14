# Athena Runtime Production Hardening v0.9

## Scope

v0.9 turns the Runtime execution boundary into a bounded, observable service. It does not claim that a locally built binary has passed an external penetration test or platform notarization. Those are release evidence, not source-code properties.

## Admission and shutdown

Every execution request entering `/run`, `/agent`, or an admitted gRPC method passes through one `operations.Gate`:

- `max_inflight` bounds active model/tool executions.
- `max_queue` bounds waiting callers; overflow fails closed.
- `admission_wait_ms` bounds queue wait.
- `request_timeout_sec` bounds the complete request.
- graceful shutdown first enters draining mode, rejects new work, and waits for admitted HTTP/gRPC work before forcing shutdown.

Health and telemetry remain outside the admission queue:

- `GET /readyz` returns the admission health snapshot.
- `GET /metrics` returns `athena.operations.v1` health and SLO JSON.
- `GET /healthz` keeps the existing runtime dependency health contract.

Environment overrides are `ATHENA_OPERATIONS_MAX_INFLIGHT`, `ATHENA_OPERATIONS_MAX_QUEUE`, `ATHENA_OPERATIONS_ADMISSION_WAIT_MS`, and `ATHENA_OPERATIONS_REQUEST_TIMEOUT_SEC`.

## SLO interpretation

The built-in counters cover the current process lifetime. `requests`, `errors`, rejection count, latency samples, availability, inflight work, and queue depth are measured in process and exposed without credentials. A production monitor should scrape and retain these snapshots externally; restarting the process intentionally resets the in-memory window.

The release targets are:

| Signal | Target |
| --- | --- |
| Control API availability | 99.9% |
| Device convergence | within 10 seconds |
| Action dispatch p95 | under 200 ms, excluding action execution |
| Lost task events | 0 |
| Duplicate irreversible effects | 0 |
| Crash-free desktop sessions | at least 99.5% |
| Upgrade success | at least 99% |

## Threat model

Trust boundaries are the authenticated user API, the machine-local Launcher token, the device WebSocket, signed Provider packages, model output, tool observations, and persisted PostgreSQL state.

The Runtime assumes model and tool output is untrusted. Typed capability schemas, policy/risk checks, explicit approval, observation validation, signed Provider loading, request budgets, and sandbox limits remain mandatory. A prompt or webpage cannot grant itself a capability, expand a credential scope, activate a Provider, or bypass approval.

Secrets must arrive through environment/configuration owned by the operator. Health/SLO output must not include model keys, database credentials, Device Tokens, cookies, raw DOM, screenshots, or private reasoning.

## Verification

Run:

```bash
GOCACHE=/private/tmp/athena-go-cache go test ./...
go test -race ./internal/operations ./internal/provider ./internal/server
```

Before a public release, also execute long-running soak, database restart, network loss, disk-full, multi-device, and platform installer suites. Source tests are required but do not replace those release-environment exercises.
