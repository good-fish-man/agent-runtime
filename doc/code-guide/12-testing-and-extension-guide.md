# Testing and Extension Guide

## Purpose

This guide turns the architecture into practical change recipes. It helps contributors make local changes without accidentally bypassing routing, governance, observation, or usage accounting.

## Standard Validation

Run these from the repository root:

```bash
go test ./...
go vet ./...
go build ./cmd/server
git diff --check
```

Use the version-specific scripts under `scripts` when changing a feature covered by an Agent OS acceptance milestone.

Tests should not require paid model calls, a user's browser profile, or real credentials. Use fakes for model, provider, device, and network boundaries.

## Change Recipe: Add a Request Field

1. Decide whether the field is transport data, model evidence, governed context, or policy input.
2. Add it to `internal/types`.
3. Add it to the protobuf only if it crosses the public RPC boundary.
4. Update `internal/server/dispatch.go`.
5. Consume it in the owning package, not directly in unrelated layers.
6. Add mapping, malformed-input, and omission/default tests.
7. Verify sensitive values do not enter logs or prompt sections accidentally.

## Change Recipe: Add a Capability

1. Define a stable public ID and risk metadata.
2. Update the declarative capability manifest.
3. Implement a private tool or provider adapter.
4. Add validation, cancellation, limits, and invocation observability.
5. Update intent/router policy only when the route semantics change.
6. Verify specialist and artifact paths cannot expand authority.
7. Test registry resolution, route selection, tool behavior, and result bounds.

## Change Recipe: Add a Browser Action

1. Express the user outcome and target, not only a click primitive.
2. Define the typed Action operation and arguments in the shared protocol.
3. Define the expected Observation and postcondition.
4. Emit the Action from Runtime without touching the Runtime host UI.
5. Implement idempotent execution in Launcher/Browser Runtime.
6. Return a stable session, window, tab, target, and snapshot identity.
7. Verify the effect before reporting success.
8. Test tab movement, closed tabs, retries, cancellation, and duplicate Action IDs.

## Change Recipe: Add a Research Provider

1. Implement the search-system provider interface.
2. Assign a source class and routing conditions.
3. Normalize URLs and publication times.
4. Apply timeout, response bounds, and cancellation.
5. Define partial failure behavior.
6. Feed normalized results into the common evidence pipeline.
7. Test provider failure, empty results, rate limits, malformed data, and cache behavior.

## Change Recipe: Add a Model Provider

1. Implement or adapt the Eino tool-calling model contract.
2. Bind only the selected tools.
3. Wrap generate and stream calls with model observability.
4. Normalize finish reasons and usage.
5. Support cancellation and provider timeouts.
6. Test text, tool calls, usage-only chunks, empty chunks, malformed tool arguments, and multimodal input where supported.
7. Confirm unsupported model parameters are omitted rather than forced.

## Change Recipe: Add a Skill

1. Create a clear `SKILL.md` with scope and prerequisites.
2. Declare required public capabilities.
3. Put deterministic heavy work in bounded scripts or templates.
4. Keep scripts sandbox-compatible and credential-free.
5. Define output files explicitly.
6. Test discovery precedence, relevance selection, sandbox failure, and output collection.

## Change Recipe: Add a Runtime Artifact Feature

1. Define the schema in `athena-protocol`.
2. Keep artifacts declarative and immutable.
3. Pin them through AgentBuild and RunManifest identity.
4. Evaluate preconditions deterministically and fail closed.
5. Resolve requirements only against already admitted capabilities.
6. Add rendering that clearly states the artifact cannot grant authority.
7. Test tampering, unknown fields/operators, missing capabilities, and replay identity.

## Change Recipe: Add a Dynamic Specialist Feature

1. Keep proposal and policy decision in the control plane.
2. Send Runtime only an admitted immutable envelope.
3. Redact context and bind payload hashes.
4. Restrict capabilities to a validated subset.
5. Propagate parent trace, task lineage, budgets, and cancellation.
6. Execute through the same model-tool-observation-verification chain.
7. Return structured evidence to the parent.
8. Test expiry, tampering, scope expansion, write capability denial, and replay.

## Test Layers

| Layer | What to prove | Typical technique |
| --- | --- | --- |
| Unit | Deterministic parser, policy, validation, ranking, limits | Table-driven Go tests |
| Package integration | Components cooperate inside one package boundary | Fakes and in-memory stores |
| Runtime integration | Request reaches model/tool loop and returns typed result | Fake model and fake tools |
| Protocol contract | Client and Runtime agree on schema and generated APIs | Generated-code compile tests |
| Device contract | Action/Observation identity and lifecycle are correct | Fake device WebSocket/control plane |
| Acceptance | User journey meets effect and safety requirements | Versioned scripts and evidence fixtures |
| Cross-platform | Build and platform-specific behavior work | CI matrix for macOS, Windows, Linux |

## High-Value Regression Cases

Keep explicit tests for these historically expensive failures:

- tool-call stream consumes tokens but returns no visible answer;
- a research request opens the user's local browser;
- a browser command is routed to generic search;
- Action dispatch is reported as success without an Observation;
- the same retry executes a side effect twice;
- a closed tab causes ordinal selection to target another tab;
- model usage is lost from advisor or stream-only chunks;
- memory extraction uses a different user's or environment model key;
- an artifact or specialist expands capabilities;
- a provider redirect reaches a private network;
- a database failure disables memory silently;
- cancellation returns an error and then a second answer.

## Debugging Workflow

1. Capture the trace ID from the caller response.
2. Find the terminal transport log for that trace.
3. Read the causal error chain from outer operation to root cause.
4. Inspect invocation spans in timestamp order.
5. Confirm the parsed intent and primary route.
6. Confirm the effective capability IDs and bound tool aliases.
7. For model issues, compare events, chunks, tool-call chunks, visible chunks, finish reason, and token usage.
8. For device issues, match Action ID, task/session scope, device routing, and Observation ID.
9. Reproduce with a package-level fake before using a live model or browser.
10. Add the reproduction as a regression test before changing behavior.

## Review Checklist

| Question | Why it matters |
| --- | --- |
| Does the change preserve context cancellation? | Prevents orphan model, search, tool, or Sub-Agent work |
| Can it expand authority after routing? | Violates capability monotonicity |
| Is external content marked untrusted? | Prevents prompt-injection privilege changes |
| Is output bounded? | Protects context and memory |
| Is success verified as an effect? | Avoids false-positive actions |
| Are trace, timing, and usage retained? | Makes production failures diagnosable |
| Is the operation idempotent or guarded? | Prevents duplicate side effects |
| Is generated code changed through its source? | Prevents contract drift |
| Are secrets excluded from logs, fixtures, and docs? | Protects users and releases |
| Is there a failure-path test? | Happy-path tests do not prove resilience |

## Documentation Rule

When a change introduces a new package, route, authority boundary, persistent record, or execution stage, update this code guide in the same change. Architecture plans describe intent; code-guide documents describe the implementation that exists now.
