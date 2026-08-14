# Athena Personal Agent OS 1.0 - Runtime Guide

[English](personal-agent-os-ga-v1.0.md) | [简体中文](personal-agent-os-ga-v1.0.zh-CN.md)

Agent Runtime 1.0 is the decision and model-execution component of Athena. It
does not own users, credentials, installers, or desktop policy. Those remain in
Runtime Client and Launcher.

## Runtime Architecture

```text
HTTP/gRPC -> admission gate -> dispatcher -> intent/capability routing
          -> model/tool/research loop -> typed stream
          -> Action request -> Runtime Client control plane -> Observation
```

The runtime uses bounded admission, request deadlines, graceful drain, strict
plugin verification, and optional durable memory. Browser and desktop actions
are abstract capabilities; the model does not call OS APIs directly.

## Readiness

`GET /readiness` returns an `athena.ga.v1` report. It checks frozen contracts,
typed execution, frontend-independent processing, bounded admission, signed
Provider configuration, and durable storage when memory is enabled.

HTTP `200` means all Runtime-owned invariants pass. HTTP `503` means at least
one required Runtime invariant failed. Package signing, a connected device,
installer validation, and soak tests belong to other release gates.

## Operations

- Health: `GET /healthz`
- Metrics and SLO snapshot: `GET /metrics`
- GA readiness: `GET /readiness`
- Generated artifacts: `GET /generated/*`
- Local-only administration: `/admin/*`

Every request should preserve `X-Trace-Id`. Model, tool, capability, and
transport spans log start, finish, duration, and the source-aware error chain.
Secrets must not be added to span fields or persisted run manifests.

## Security and Data

- Enable plugins only with signature verification and an explicit trust store.
- Provider grants are a subset of requested permissions and resource limits.
- Memory may be disabled. If enabled for GA, configure PostgreSQL and user-level
  retention/deletion controls through Runtime Client.
- Generated files and local admin endpoints must remain behind trusted local or
  authenticated infrastructure.
- Runtime does not return model API keys to the browser.

## Troubleshooting

1. Query `/healthz`, then `/readiness`.
2. Search logs by trace ID and inspect the first source location in the error chain.
3. If `admission.control` fails, inspect queue saturation and drain state.
4. If `plugin.trust` fails, verify the trust store, package digest, signature, grants, and platform.
5. If `memory.persistence` fails, restore database connectivity or explicitly disable memory.

## Release Gate

Run `go test ./...` for code verification. Real model-provider tests, signed
plugin packages, cross-platform installers, notarization, and sustained load
remain external evidence and must not be represented as locally passed.
