# Operations, Observability, and Scheduling

## Purpose

This group keeps Runtime operable after the core agent logic works. It controls admission, readiness, traces and timings, local administration, scheduled work, database construction, and release-evidence scanning.

## Main Locations

| Location | Responsibility |
| --- | --- |
| [`internal/operations`](../../internal/operations) | Admission, deadlines, draining, health, and SLO snapshots |
| [`internal/observability/invocation.go`](../../internal/observability/invocation.go) | Uniform timed invocation spans |
| [`internal/readiness/report.go`](../../internal/readiness/report.go) | Production and GA readiness report |
| [`internal/admin/handler.go`](../../internal/admin/handler.go) | Loopback-only configuration, restart, model, and provider admin routes |
| [`internal/cron`](../../internal/cron) | Cron parsing, durable task file, scheduler, and loop tools |
| [`internal/database/database.go`](../../internal/database/database.go) | GORM/PostgreSQL connection construction |
| [`internal/evidenceaudit/scanner.go`](../../internal/evidenceaudit/scanner.go) | Release-evidence and credential scanner |
| [`cmd/v03-evidence-audit`](../../cmd/v03-evidence-audit) | Scanner CLI |

## Operations Gate

`operations.Gate` is the request-admission authority. Its configuration includes maximum inflight requests, queue size, queue wait, and run timeout.

`Acquire` returns a derived context and a completion function. The caller reports whether the admitted operation succeeded so the gate can update metrics.

The gate tracks:

- inflight and queued requests;
- accepted and rejected totals;
- timeout and cancellation totals;
- dropped stream events;
- latency samples and p95;
- drain state.

HTTP and gRPC wrappers apply the same gate semantics. Health/readiness methods are excluded from ordinary admission so overload remains observable.

## Health, Readiness, and SLO

These concepts are distinct:

| Signal | Question answered |
| --- | --- |
| Health | Is the process alive and what is its current operational state? |
| Readiness | Is configuration safe and is the instance eligible to serve? |
| SLO snapshot | How is admitted work performing? |
| GA readiness | Does the deployment satisfy frozen release invariants? |

`readiness.Build` checks production invariants such as safe plugin settings, memory/database coherence, and expected Runtime version. A report contains individual pass/fail checks and an overall status.

## Invocation Observability

`observability.Begin` creates a timed invocation object with kind, name, ID, trace context, caller source, and structured fields. `End` records completion, elapsed time, and error outcome.

Use it at expensive or externally meaningful boundaries:

- model generate and stream;
- tool execution;
- capability provider invocation;
- research provider search/fetch;
- device Action waiting;
- Sub-Agent execution;
- memory extraction.

A useful span says when the operation started, when it ended, how long it took, what stable identity it used, and whether it failed. Avoid logging entire prompts, API keys, cookies, or raw credentials.

Errors should be wrapped as they cross abstraction boundaries and logged once when the request completes. The trace ID connects inner spans to that terminal log.

## Local Administration

The admin handler rejects non-loopback callers. It supports:

- service status and Runtime-owned paths;
- reading and atomically updating Runtime configuration;
- reading and updating skills configuration;
- strict YAML validation where required;
- service restart signaling;
- local-model lifecycle operations;
- signed Capability Provider reload.

Writes use a temporary file and atomic replacement. A malformed configuration must not replace the working file.

Loopback is a boundary, not complete authentication for a remotely exposed port. Do not bind these routes publicly.

## Scheduled Work

The cron subsystem provides:

| Part | Responsibility |
| --- | --- |
| `cron.go` | Parse cron expressions and compute next fire time |
| `tasks.go` | Session and durable task records, atomic file persistence |
| `scheduler.go` | Poll due tasks, apply deterministic jitter, notify a handler |
| `loop_tool.go` | Model-facing create, list, and delete tools |

Durable scheduled tasks are persisted under the project/runtime directory. Session tasks disappear with the process. Task IDs are generated rather than inferred from prompt text.

The scheduler uses a handler interface. Runtime itself should not perform purchases, reservations, CAPTCHA, payment, or other high-risk irreversible actions in an unattended background tick. Such tasks need an interactive checkpoint and confirmation.

## Database Construction

`database.New` builds the GORM PostgreSQL connection from `config.DBConfig`, applies the configured GORM log level, and returns a connection for packages such as memory.

Schema ownership stays with the package that owns the data. Database construction should not silently auto-migrate unrelated service tables.

Runtime can continue without persistent memory when configuration permits a degraded mode. It must log the causal database error and expose the reduced readiness state rather than pretending memory is active.

## Evidence Audit

The evidence scanner inspects files, ZIP archives, and tarballs used by release gates. It detects prohibited fixed credentials and evidence-rule violations without printing secret values.

The scanner:

- bounds reads;
- skips binary content;
- rejects unsafe symlink roots;
- understands explicit safe placeholders;
- emits a structured JSON report.

This is release governance, not runtime prompt evidence ranking.

## Invariants

1. Admission is applied consistently across business transports.
2. Draining rejects new work and wakes queued requests.
3. Cancellation and timeout are counted separately.
4. Expensive operations emit start/end/elapsed observability.
5. Admin routes remain loopback-only and use atomic writes.
6. Background tasks cannot bypass interactive risk policy.
7. Readiness reports degraded dependencies honestly.
8. Evidence scans never echo discovered credential values.

## Tests

Operations tests cover queue full, cancellation, drain, deadlines, HTTP status, and metric separation. Readiness tests cover unsafe production combinations. Admin tests cover loopback rejection and observable reload. Cron changes should test parser edge cases, next-run calculations, atomic persistence, jitter, cancellation, and one-shot cleanup. Evidence audit tests cover archives, binaries, symlinks, placeholders, and fixed credentials.
