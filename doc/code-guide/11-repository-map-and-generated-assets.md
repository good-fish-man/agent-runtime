# Repository Map and Generated Assets

## Purpose

This document explains top-level repository ownership. Use it when deciding where a new file belongs or whether an existing file is source, generated output, bundled content, or test evidence.

## Top-Level Map

| Path | Purpose | Edit policy |
| --- | --- | --- |
| [`cmd`](../../cmd) | Executable entry points | Keep composition and CLI behavior thin |
| [`internal`](../../internal) | Runtime implementation packages | Primary application source |
| [`pkg`](../../pkg) | Public or compatibility Go packages | Use only for APIs intentionally importable outside this module |
| [`proto`](../../proto) | Protobuf source contracts | Edit here before regeneration |
| [`gen`](../../gen) | Generated protobuf Go code | Do not hand edit |
| [`manifest`](../../manifest) | Declarative Runtime manifests | Keep synchronized with code contracts |
| [`skills`](../../skills) | Bundled skill content, scripts, templates, references | Treat as executable supply-chain content where scripts exist |
| [`scripts`](../../scripts) | Build, release, and acceptance automation | Keep non-interactive and reproducible |
| [`testdata`](../../testdata) | Stable fixtures and release evidence | No real secrets or user data |
| [`third_party`](../../third_party) | Vendored protocol dependencies and notices | Preserve upstream ownership and licences |
| [`doc`](../../doc) | Architecture, operations, ADRs, and this code guide | Keep version/intent clear |
| `bin` | Local build output | Generated; do not treat as source |
| `.github/workflows` | CI and release workflows | Keep aligned with local scripts |

## Command Packages

| Command | Purpose |
| --- | --- |
| `cmd/server` | Production gRPC and HTTP/SSE Runtime service |
| `cmd/client` | Minimal client/example for exercising Runtime RPCs |
| `cmd/v03-evidence-audit` | Release-evidence scanner CLI |

Do not place reusable business logic in `package main`. Extract it into the owning `internal` package and test it there.

## Protocol Source and Generated Code

The gRPC contract is defined under `proto/agent/runtime/v1`. Generated code under `gen/agent/runtime/v1` is consumed by Runtime and Runtime Client.

Protocol change workflow:

1. Change the `.proto` source.
2. Regenerate Go code with the pinned toolchain.
3. Update server request/response mapping.
4. Update dependent repositories or bump the shared module version.
5. Run cross-platform builds.
6. Commit source and generated files together.

Never fix an undefined generated RPC by editing `gen` only. The next generation will erase the change and other repositories will still use a mismatched contract.

Athena also uses shared schemas from the `athena-protocol` Go module. Runtime artifact, specialist orchestration, provider, operations, and readiness types may therefore live in that module rather than this repository.

## Manifests

`manifest/capabilities.yaml` documents the stable public capability catalog. It should match the built-in definitions in `internal/capability`.

Provider Registry, trust store, and installed packages are deployment state configured through paths; they are not hard-coded into the binary.

Release manifests are distribution metadata and must be published as directly downloadable assets when Launcher expects a direct URL. Packaging a manifest only inside a ZIP changes the contract and causes a 404 for direct-download clients.

## Bundled Skills

Each directory in `skills` is a reusable workflow package. Common contents are:

| File or directory | Meaning |
| --- | --- |
| `SKILL.md` | Entry instructions and metadata |
| `scripts/` | Sandboxed deterministic helpers |
| `templates/` | Reusable output templates |
| `references/` | Documentation loaded when relevant |
| `agents/` | Skill-specific agent configuration where supported |

Current bundled areas include browser guidance, CSV analysis, document conversion, S3 upload, skill creation, and presentation generation.

Scripts and templates affect release licensing. Check `THIRD_PARTY_NOTICES.md` and release exclusion rules before redistributing a new skill.

## Scripts and Acceptance Gates

Scripts cover component acceptance, versioned engineering gates, evidence aggregation, and release checks. Versioned architecture work has corresponding gate scripts so a delivery claim can be reproduced.

A gate script should:

- use deterministic inputs;
- fail on the first violated acceptance condition;
- preserve useful evidence without secrets;
- work in CI and locally;
- avoid mutating user configuration;
- report the exact failed stage.

## Test Data

`testdata` contains fixtures for evidence audits and version acceptance. Go treats this directory specially and packages can read it without compiling its contents.

Use synthetic credentials and explicit placeholder markers. Never copy a real API key, database password, cookie, or private user document into a fixture.

## Documentation Types

| Document type | Purpose |
| --- | --- |
| README | Product overview and quick start |
| Code guide | Current source ownership and execution behavior |
| Architecture plan | Intended target design and staged delivery |
| ADR | A frozen architectural decision and its rationale |
| Acceptance report | Evidence that a version met its gates |
| Operations guide | Deployment, troubleshooting, and recovery |

Architecture plans may describe future work. This code guide should describe the current source. When they differ, label the gap rather than presenting a plan as implemented behavior.

## Public Package Boundary

Most Runtime code is deliberately under `internal`, so other repositories cannot import implementation details. Cross-repository contracts should live in:

- protobuf/API schemas;
- the `athena-protocol` module;
- a deliberately public package with versioning guarantees.

The current [`pkg/errtrace`](../../pkg/errtrace) directory is a compatibility placeholder with no active Go source. New error tracing should use the shared `logx` facilities and context-aware wrapping rather than rebuilding a second public error package here.

## Placement Checklist

Before adding a file, ask:

1. Is this process composition, application behavior, protocol source, generated output, bundled content, or test evidence?
2. Which package owns the state and invariants?
3. Does another repository need to import it?
4. Is it safe to distribute?
5. Is it generated from another source of truth?
6. What test or acceptance gate proves it works?
