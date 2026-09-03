# Athena Governed Dynamic Specialist Orchestration: Technical Architecture and Delivery Plan

**Language:** [简体中文](./athena-governed-dynamic-specialist-orchestration-plan.zh-CN.md) | **English**

> The Chinese and English editions describe the same architecture semantics. Any change to a frozen invariant, stage gate, or acceptance criterion must update both documents in the same change.

| Field | Value |
| --- | --- |
| Document type | Architecture Baseline + Implementation Plan |
| Workstream | DSO (Dynamic Specialist Orchestration) |
| Semantic baseline | FROZEN |
| Current implementation stage | DSO-W0 |
| Object schema | `draft/v0alpha` |
| Persistence schema | `draft/v0alpha` |
| Event protocol | `draft/v0alpha` |
| Runtime / control-plane protocol | `draft/v0alpha` |
| Lease protocol | `draft/v0alpha` |
| Budget protocol | `draft/v0alpha` |
| Repositories | `athena-protocol`, `agent-runtime-client`, `agent-runtime`, `athena-launcher`, `frontend/agent-ui` |

---

## 1. Purpose

This document converts Athena's frozen Dynamic Subagent semantics into an executable engineering plan. It answers:

1. What a dynamic Specialist is and is not.
2. Who may propose delegation, who owns durable state, and who may execute actions.
3. How a Subagent continues to obey the Outcome, Plan, Policy, Action, Observation, and Verification chain.
4. How context isolation, capability reduction, budgets, concurrent resources, cancellation, retries, recovery, and replay work.
5. What each repository owns and which legacy paths must be reused, replaced, or removed.
6. What each DSO stage solves, what it delivers, and how completion is objectively accepted.

This is not a field-level freeze. Object fields, database tables, event payloads, and RPC definitions remain `draft/v0alpha` until validated by real vertical slices. Responsibility boundaries, execution authority, invariants, and the single execution chain are frozen.

---

## 2. Executive Summary

Athena will not build an unrestricted multi-agent network. It will add governed, dynamic composition of temporary decision actors inside the existing Agent OS:

```text
Complex TaskStep
    -> DelegationProposal
    -> DelegationDecision
    -> Scoped Specialist
    -> Durable Agent Loop
    -> Typed Candidate Result
    -> External Verification
    -> Parent Outcome Evaluation
```

A Subagent is a temporary and constrained Decision Actor. It is not an execution authority. It may research, analyze, verify, propose an `ActionProposal`, and propose a `PlanFragmentProposal`, but it may not:

- Declare its own outcome successful.
- Create an authoritative `PlanCandidate` directly.
- Bypass `PolicyDecision`, `PlanRun`, or `ActionAttempt`.
- Commit `WorldFact` directly.
- Expand its own capabilities, permissions, risk ceiling, or budget.
- Receive raw passwords, cookies, API keys, or access tokens.
- Publish a Skill, Strategy, Plugin, or `SpecialistProfile` directly.

Every action that can affect the external world, regardless of whether it originated from a Main Agent, Subagent, Skill, Automation, Plugin, Evolution process, or future Robot Agent, must use one execution path:

```text
ActionProposal
    -> PlanCandidate
    -> PolicyDecision
    -> PlanRun
    -> ActionAttempt
    -> Observation
```

The architecture is summarized as:

> Effect-Grounded Delegation + Durable Agent Loop + Unified Governed Execution + External Verification

---

## 3. Goals and Non-Goals

### 3.1 Goals

DSO must eventually support:

- Main Agent decisions to delegate based on complexity, parallelism, specialization, context pressure, and verification value.
- Reuse of reviewed `SpecialistProfile` artifacts before creating constrained ad-hoc Specialist specifications.
- Least-context and least-privilege capability views for each Subagent.
- Durable, cancellable, recoverable, and replayable single Specialist, parallel Specialist, and bounded DAG execution.
- Independent budgets, deadlines, actor bindings, and verification requirements for each `SubagentRun`.
- Exact model, prompt, context, schema, capability, and runtime-artifact binding for every `SubagentAttempt`.
- A single Athena Execution Kernel for every Subagent action request.
- Short-lived leases for shared mutable resources such as browser tabs, terminals, files, and robot arms.
- `TypedCandidateResult` output whose success is decided only by external `VerificationResult` objects.
- A user-visible timeline for delegation decisions, actions, evidence, cost, verification, cancellation, and takeover.
- Governed learning that can propose candidates but cannot silently modify production behavior.

### 3.2 Non-Goals

DSO-W0 through DSO-W5 explicitly do not provide:

- An open peer-to-peer agent society.
- Recursive delegation with unbounded depth.
- Autonomous code generation followed by direct execution.
- Self-issued permissions or credentials.
- Automatic publication of learned Specialist profiles.
- A second action engine dedicated to Subagents.
- A replacement for the existing Outcome, Policy, Action, Observation, World State, or Verification authorities.
- A new message broker before the internal protocol has been validated.

### 3.3 Initial Production Constraints

The first production-capable release uses conservative defaults:

```yaml
delegation:
  enabled: false
  max_depth: 1
  max_subagents_per_task: 4
  max_parallel_subagents: 3
  allow_ad_hoc_specialists: false
  allow_subagent_delegation: false
  require_typed_result: true
  require_external_verification: true
```

Risky features are enabled only after their stage exit gates pass.

---

## 4. Current Baseline and Gaps

### 4.1 Existing Capabilities to Reuse

| Existing capability | Current location | DSO role |
| --- | --- | --- |
| Effect and Outcome semantics | `athena-protocol/draft/v0alpha` | Parent and delegated outcome model |
| Protocol v4 Action/Observation | `athena-protocol` | Frozen device execution wire contract |
| Durable Goal and Task | `agent-runtime-client/application/service/goal` | Parent lifecycle and cancellation root |
| Execution and verification services | `agent-runtime-client/application/service/execution` | Single governed action chain |
| AgentBuild / RunManifest | `agent-runtime-client/application/service/deployment` | Parent build snapshot inherited by an invocation manifest |
| Runtime Artifact Resolver | `agent-runtime-client/application/service/evolution` and deployment code | Resolve exact immutable artifacts |
| Supervisor and Specialist reasoning | `agent-runtime` | Decision Runtime implementation |
| Device WebSocket control plane | `agent-runtime-client` + `athena-launcher` | Remote action execution and observations |
| Browser Runtime and Perception | `athena-launcher` | Browser execution and environment observations |
| Chat timeline and approval UI | `frontend/agent-ui` | User-visible delegation and action controls |

### 4.2 Legacy Paths That Must Be Replaced

| Legacy implementation | Problem | Replacement |
| --- | --- | --- |
| Request-scoped `agent-runtime/internal/subagent` manager | In-memory lifecycle and non-global task identity | Durable Control Plane Delegation Orchestrator |
| Direct `spawn/delegate` flow | Can form a simplified execution path | Compatibility adapter to `DelegationProposal`, then removal |
| `os_specialist_run` aggregate record | Cannot represent attempt, manifest, lease, budget, or recovery | New DSO persistence model |
| Tool call directly invoking Launcher | Bypasses Plan and action policy | `ActionProposal -> PlanCandidate -> PolicyDecision` |
| Model text treated as success | Produces false completion | Typed candidate plus external verification |
| Full prompt/context persistence | Privacy and replay ambiguity | Redacted content references and content hashes |

There must be exactly one logical durable delegation authority. During migration, compatibility adapters may translate old calls into a new `DelegationProposal`, but they may not execute a Subagent themselves.

---

## 5. Frozen Architecture Invariants

### 5.1 Athena Kernel Invariants

1. Athena has exactly one governed execution path for every action that can affect the external world.
2. An `ActionProposal` is not executable. Only an allowed `PlanCandidate` may produce a `PlanRun`.
3. A valid `PolicyDecision` must exist before `PlanRun` and `ActionAttempt` execution.
4. An `Observation` is a sanitized representation of an external result, not a model claim.
5. Only World State Authority may commit `WorldFact`.
6. Only `VerificationResult` may determine whether an `EffectClause` is satisfied.
7. Secrets never enter the intelligence plane.

### 5.2 DSO Invariants

1. `DelegationDecision` evaluates one specific and unambiguous `DelegationProposal`.
2. A proposal may carry drafts, but only accepted drafts are materialized as immutable, versioned objects.
3. One Subagent owns exactly one delegated `TaskStep` scope.
4. Every Subagent binds to one `DelegatedOutcomeSpec`.
5. A Subagent cannot declare its own outcome satisfied.
6. A Subagent may only propose an `ActionProposal` or `PlanFragmentProposal`.
7. A Subagent cannot create an authoritative `PlanCandidate` directly.
8. Read-only and side-effecting actions use the same governance protocol. R0 may be auto-admitted but never bypassed in audit.
9. `RequestedCapabilitySet` is not `AdmittedCapabilityView`.
10. Only the Control Plane may create an `AdmittedCapabilityView`.
11. `RequestedContextScope` is not the delivered `RedactedContextSlice`.
12. Delivered context must pass tenant filtering, classification, redaction, and taint handling.
13. External content is Evidence, never Authority or AgentInstruction.
14. Subagent permission, risk, capability, and budget limits are subsets of their parent limits.
15. `SubagentRun` is a durable logical execution. `SubagentAttempt` is a complete recoverable Agent Loop attempt.
16. `ModelInvocation` is not a `SubagentAttempt`.
17. `DecisionTurn` is one cognitive decision cycle.
18. `SubagentRun` maintains actor binding and session affinity. `ActionAttempt` owns shared mutable resource leases.
19. A mutable resource lease is short-lived, has a TTL, and is owned by an `ActionAttempt`.
20. Parent consumption plus all active child reservations cannot exceed the parent budget.
21. Budgets use Reserve, Commit, and Release semantics.
22. Cancellation propagates to all descendant execution units and releases leases and unused budget.
23. A late or superseded attempt result cannot mutate the active run.
24. Every attempt binds to a replayable `InvocationManifest`.
25. `InvocationManifest` inherits a parent `RunManifest` and records only invocation-specific differences.
26. Credentials appear only as secret-handle references, never as values in prompt, context, manifest, observation, world fact, experience, log, or trace.
27. `SpecialistProfile` is a `RuntimeArtifact`, not an independent authority registry.
28. Delegation Orchestrator is the single logical durable delegation authority, while physical HA instances are allowed.
29. Delegation Orchestrator belongs to the Control Plane. Supervisor and Specialist reasoning belong to the Decision Runtime.
30. Learning may propose `DelegationPolicyCandidate` or `SpecialistProfileCandidate`, but cannot activate either directly.

---

## 6. System Architecture

```text
                                    User / Frontend
                                           |
                                           v
                              Persistent Goal / TaskStep
                                           |
                                           v
+------------------------------ Control Plane --------------------------------+
| agent-runtime-client                                                         |
|                                                                              |
| Delegation Orchestrator                                                      |
|   +-> Proposal / Decision Store                                              |
|   +-> Admission Service                                                     |
|   +-> Budget Ledger                                                         |
|   +-> Context Builder                                                       |
|   +-> Runtime Artifact Resolver                                             |
|   +-> Run / Attempt State Machines                                          |
|   +-> Cancellation / Recovery / Retry                                       |
|   +-> Result Aggregator                                                     |
|   +-> Policy / Resource / Device Coordination                               |
+-----------------------------------+------------------------------------------+
                                    |
                       typed commands and events
                                    |
                                    v
+----------------------------- Decision Runtime -------------------------------+
| agent-runtime                                                                |
|                                                                              |
| Supervisor Reasoner        Specialist Worker                                |
|   +-> local/delegate         +-> DecisionTurn                               |
|   +-> proposal drafts        +-> ModelInvocation                            |
|                              +-> ActionProposal / CandidateResult            |
+-----------------------------------+------------------------------------------+
                                    |
                              ActionProposal
                                    |
                                    v
+---------------------------- Execution Kernel --------------------------------+
| PlanCandidate -> PolicyDecision -> PlanRun -> ActionAttempt                  |
| -> Capability Executor -> Sanitized Observation -> Verification             |
+-----------------------------------+------------------------------------------+
                                    |
                          Device Action / Observation
                                    |
                                    v
+------------------------------ Device Runtime --------------------------------+
| athena-launcher                                                             |
| Browser Runtime | Desktop | File | Terminal | Audio | Perception            |
+-------------------------------------------------------------------------------+
```

### 6.1 Control Plane: `agent-runtime-client`

The Control Plane owns all durable orchestration authority:

- Proposal and decision persistence.
- Admission, context redaction, capability reduction, and policy coordination.
- Budget reservation, settlement, and release.
- Run and attempt state machines, leases, cancellation, retry, and recovery.
- Runtime artifact resolution and `InvocationManifest` creation.
- Action-proposal conversion into the unified execution chain.
- Typed result aggregation and external verification coordination.
- Outbox events, audit trails, and frontend projections.

It must not perform LLM reasoning itself.

### 6.2 Decision Runtime: `agent-runtime`

The Decision Runtime owns reasoning, not authority:

- Supervisor proposes `LOCAL` or a concrete `DelegationProposal`.
- Specialist executes `DecisionTurn` according to an `InvocationManifest`.
- Model adapters record model usage, latency, errors, and provider retries.
- Runtime emits typed progress, action proposals, and candidate results.
- Runtime never writes authoritative Control Plane state directly.

### 6.3 Protocol Repository: `athena-protocol`

The protocol repository owns shared type semantics:

- DSO object schemas and validations.
- State and transition constants.
- Command/event envelopes.
- Hash canonicalization rules.
- Compatibility and conformance fixtures.

DSO-W0 does not modify the frozen Protocol v4 Action/Observation wire contract. DSO correlation IDs may initially travel through metadata or internal Control Plane events. A public protocol change requires evidence from a working vertical slice.

### 6.4 Device Runtime: `athena-launcher`

Launcher remains an execution and perception runtime:

- Executes only governed `ActionAttempt` requests.
- Returns sanitized observations with stable resource identity and version.
- Enforces local permission and user takeover boundaries.
- Provides browser session, window, tab, file, terminal, and desktop resource state.
- Never decides delegation and never evaluates parent outcome success.

### 6.5 Frontend: `frontend/agent-ui`

The frontend renders and controls durable state:

- Delegation reason and accepted Specialist.
- Run, attempt, turn, model, action, observation, and verification timeline.
- Budget and token usage.
- Evidence, source links, conflicts, and unknown outcomes.
- Cancel, retry, approve, reject, or take over where policy allows.

The frontend does not relay privileged execution messages and is not required to stay open for task continuation.

---

## 7. Frozen End-to-End Chain

```text
TaskStep
   |
   v
DelegationProposal
   +-- Draft DelegatedOutcomeSpec
   +-- Draft SubagentSpec
   +-- RequestedCapabilitySet
   +-- RequestedContextScope
   +-- CostBenefitEstimate
   |
   v
DelegationDecision
   |
   +-- LOCAL --------> Main Agent Fast Path
   |
   +-- DELEGATE
          |
          v
Materialize immutable objects
   +-- DelegatedOutcomeSpec
   +-- SubagentSpec
          |
          v
Subagent Admission
   +-- Admission PolicyDecision
   +-- BudgetReservation
   +-- AdmittedCapabilityView
   +-- RedactedContextSlice
   +-- ActorBinding
          |
          v
SubagentRun
          |
          v
Prepare Attempt
          |
          v
Resolve Runtime Artifacts
          |
          v
InvocationManifest
          |
          v
SubagentAttempt
   |
   +-> DecisionTurn
   |      +-> ModelInvocation
   |      +-> ActionProposal / wait / result / fail
   |
   +-> ActionProposal
          |
          v
      PlanCandidate
          |
          v
      Action PolicyDecision
          |
          v
      PlanRun
          |
          v
      ActionAttempt(RESERVED)
          |
          v
      Preconditions
          |
          v
      Acquire ResourceLease(owner=ActionAttempt)
          |
          v
      Critical Preconditions Recheck
          |
          v
      ActionAttempt(EXECUTING)
          |
          v
      Capability Executor
          |
          v
      Sanitized Observation
          |
          v
      Release ResourceLease
          |
          +------> next DecisionTurn
   |
   +-> TypedCandidateResult
          |
          v
Evidence Validation
          |
          v
VerificationResult per EffectClause
          |
          v
Result Aggregator
          |
          v
Parent Outcome Evaluation
```

### 7.1 Decisions That Must Remain Distinct

| Object | Question answered | Owner |
| --- | --- | --- |
| `DelegationProposal` | How should this task be delegated? | Decision Runtime proposes |
| `DelegationDecision` | Is this exact delegation proposal worthwhile? | Delegation Orchestrator |
| `PolicyDecision` | May this Subagent or Plan execute in the current world? | Control Plane Policy |
| `VerificationResult` | Did the observed effect actually occur? | Verifier / World Authority |

### 7.2 Fast Path

The following normally do not create a Subagent:

- Open one known web page.
- Read one known file.
- Answer a simple question that requires no tools.
- Perform a single low-context step that requires no independent verification.

Fast Path is not a second execution path. Any external action still uses the unified Plan chain.

---

## 8. Core Object Model

### 8.1 DelegationProposal

An immutable, non-executable candidate describing one concrete delegation option:

```yaml
proposal_id: string
task_step_ref: string
draft_outcome: object
draft_subagent_spec: object
requested_capability_set: []
requested_context_scope: object
candidate_specialist_refs: []
cost_benefit_estimate: object
reasons: []
input_hash: string
created_by: string
created_at: timestamp
```

One `TaskStep` may have several proposals. A `DelegationDecision` must reference the exact proposal and input hash it evaluated.

### 8.2 DelegatedOutcomeSpec

A constrained contribution to the parent `OutcomeSpec`, not merely a natural-language task:

```yaml
delegated_outcome_id: string
parent_outcome_ref: string
parent_effect_clause_refs: []
task_step_ref: string
target_spec_ref: string
delegated_effect_clauses: []
must_preserve: []
forbidden_effects: []
verification_requirements: []
contribution_type: satisfy | support | verify | disambiguate
definition_hash: string
created_at: timestamp
```

A Subagent result cannot mutate this object and cannot use its own status as effect evidence.

### 8.3 SubagentSpec

Describes the temporary Decision Actor that is requested:

```yaml
subagent_spec_id: string
task_step_ref: string
delegated_outcome_ref: string
role: object
requested_capabilities: []
requested_context_scope: object
permission_ceiling_ref: string
risk_ceiling: string
budget_request: object
model_constraints: object
output_schema_ref: string
delegation_policy:
  may_delegate: false
  max_depth: 0
definition_hash: string
created_at: timestamp
```

It contains no API key and does not bind unverified prompt text directly.

### 8.4 Capability and Context

```text
RequestedCapabilitySet
    -> availability / ownership / risk / policy
    -> AdmittedCapabilityView

RequestedContextScope
    -> tenant filter / classification / redaction / taint
    -> RedactedContextSlice
```

Every context item includes at least:

```yaml
content_ref: string
source_type: string
trust_class: trusted_system | trusted_user | trusted_internal | untrusted_external
taint_flags: []
classification: public | internal | confidential | restricted
owner_ref: string
content_hash: string
```

`untrusted_external` content is placed only in a clearly marked Evidence area, never in System Instruction.

### 8.5 ActorBinding

Describes environment affinity without implying long-term resource ownership:

```yaml
actor_binding_id: string
device_ref: string
browser_session_ref: optional
terminal_session_ref: optional
environment_ref: string
valid_until: timestamp
```

Concrete mutable resources such as a browser tab cannot be held indefinitely through `ActorBinding`.

### 8.6 SubagentRun

A durable logical child-task execution:

```yaml
subagent_run_id: string
subagent_spec_ref: string
task_step_ref: string
delegated_outcome_ref: string
actor_binding_ref: string
status: string
active_attempt_ref: optional
revision: integer
deadline: timestamp
created_at: timestamp
updated_at: timestamp
terminal_reason: optional
```

A run may have multiple attempts, but at most one attempt may hold the valid execution-owner lease at any time.

### 8.7 InvocationManifest

Each attempt binds to one content-addressed invocation configuration:

```yaml
invocation_manifest_id: string
parent_run_manifest_ref: string
subagent_spec_ref: string
delegated_outcome_ref: string
specialist_profile_ref: string
prompt_artifact_ref: string
context_slice_ref: string
context_hash: string
model_ref: string
model_build_ref: string
model_parameters_hash: string
output_schema_ref: string
capability_view_ref: string
strategy_refs: []
skill_refs: []
runtime_build_ref: string
secret_handle_refs: []
content_hash: string
created_at: timestamp
```

The manifest stores secret handles, never secret values. A retry creates a new hash whenever any input changes; exact reuse is allowed only when the complete configuration is unchanged.

### 8.8 SubagentAttempt

One complete and recoverable Agent Loop attempt:

```yaml
subagent_attempt_id: string
subagent_run_ref: string
attempt_no: integer
invocation_manifest_ref: string
idempotency_key: string
owner_instance_id: string
lease_expires_at: timestamp
heartbeat_at: timestamp
status: string
budget_reservation_ref: string
result_ref: optional
error_ref: optional
started_at: timestamp
ended_at: optional
revision: integer
```

An attempt is not one model call. It may contain several decision turns, model invocations, action proposals, and observations.

### 8.9 DecisionTurn and ModelInvocation

```yaml
decision_turn_id: string
subagent_attempt_ref: string
sequence: integer
input_context_ref: string
observation_refs: []
model_invocation_ref: string
decision_type: propose_action | request_observation | produce_result | wait | fail
output_ref: string
created_at: timestamp
```

`ModelInvocation` records provider, model, tokens, latency, finish reason, error chain, and timing. Transparent provider retries have separate invocation-attempt records but do not create a new `SubagentAttempt`. A new `SubagentAttempt` is created only when Agent Loop ownership or the recovery boundary changes.

### 8.10 TypedCandidateResult

```yaml
result_id: string
subagent_run_ref: string
subagent_attempt_ref: string
status: produced | partial | failed | indeterminate
claims: []
evidence_refs: []
hypothesis_refs: []
proposed_affordances: []
proposed_plan_fragments: []
artifacts: []
unresolved_questions: []
usage: object
confidence: number
created_at: timestamp
```

`produced` means only that a candidate exists. It does not mean the delegated outcome is satisfied.

### 8.11 BudgetReservation

Budgets form a hierarchical ledger:

```text
Consumed(parent) + ActiveReservations(children) <= TotalBudget(parent)
```

State model:

```text
REQUESTED -> RESERVED -> COMMITTED
                    \-> RELEASED
                    \-> EXPIRED
```

Supported dimensions include tokens, money, action count, query count, page count, compute time, and wall-clock time. Reservation and run-state changes are linked in the same transaction or through a reliable outbox.

### 8.12 ResourceLease

```yaml
resource_lease_id: string
resource_ref: string
resource_version: string
mode: shared_read | exclusive_write
owner_action_attempt_ref: string
owner_instance_id: string
status: requested | active | released | expired | revoked
acquired_at: timestamp
expires_at: timestamp
heartbeat_at: timestamp
revision: integer
```

The required action order is:

```text
PlanCandidate
-> Action PolicyDecision
-> PlanRun
-> ActionAttempt(RESERVED)
-> Preconditions
-> Acquire Lease(owner=ActionAttempt)
-> Critical Preconditions Recheck
-> ActionAttempt(EXECUTING)
-> Executor
-> Sanitized Observation
-> Release Lease
```

---

## 9. State Machines

### 9.1 DelegationProposal

```text
DRAFT -> SUBMITTED -> ACCEPTED
                   -> REJECTED
                   -> SUPERSEDED
                   -> EXPIRED
```

A terminal proposal cannot return to `SUBMITTED`. Re-evaluation creates a new proposal.

### 9.2 SubagentRun

```text
CREATED
  -> ADMITTED
  -> QUEUED
  -> RUNNING
       -> WAITING_OBSERVATION
       -> WAITING_USER
       -> WAITING_DEVICE
       -> WAITING_RETRY
       -> COMPLETED
       -> FAILED
       -> CANCELLED
       -> EXPIRED
```

`COMPLETED` means execution ended. It does not mean outcome success, which is aggregated from verification results.

### 9.3 SubagentAttempt

```text
RESERVED
  -> STARTING
  -> RUNNING
       -> WAITING_ACTION
       -> WAITING_OBSERVATION
       -> CANCEL_REQUESTED
       -> COMPLETED
       -> FAILED
       -> TIMED_OUT
       -> ABANDONED
```

Constraints:

- One run has at most one active attempt.
- A former owner cannot write after its attempt lease expires.
- `FAILED`, `TIMED_OUT`, and `ABANDONED` cannot return to `RUNNING`.
- Retry creates a new attempt and idempotency key.
- Late results are retained only as audit evidence.

### 9.4 ActionAttempt

DSO does not create a second action state machine. It extends the existing one:

```text
RESERVED -> POLICY_ALLOWED -> LEASED -> EXECUTING
          -> POLICY_DENIED                 -> SUCCEEDED
          -> WAITING_APPROVAL              -> FAILED
                                            -> CANCELLED
                                            -> UNKNOWN_OUTCOME
```

`UNKNOWN_OUTCOME` requires reconciliation. A side-effecting action cannot be retried blindly.

### 9.5 Cancellation Tree

```text
Task / Goal Cancel
  -> PlanRun
  -> SubagentRun
  -> SubagentAttempt
  -> DecisionTurn / ModelInvocation
  -> ActionAttempt
  -> Provider call / Device action
```

Cancellation must:

1. Persist `cancel_requested` and its reason.
2. Prevent new decision turns and action attempts.
3. Cancel model, network, and device calls when supported.
4. Release resource leases.
5. Commit consumed budget and release the remainder.
6. Reconcile uncertain side effects.

### 9.6 Recovery Rules

At startup, the Control Plane scans for:

- Attempts still running after their owner lease expired.
- Model invocations without a heartbeat.
- Executing action attempts with no observation.
- Active budget reservations whose parent task terminated.
- Cancelled runs whose resource leases remain active.

Recovery may only `resume`, `retry as new attempt`, `reconcile`, `wait user/device`, or `terminalize`. It cannot mutate a terminal object back into a running object.

---

## 10. Persistence and Transaction Boundaries

### 10.1 Proposed Tables

| Table | Purpose |
| --- | --- |
| `os_delegation_proposal` | Proposal, input hash, cost/benefit, and status |
| `os_delegation_decision` | Local/delegate decision, reasons, and evaluator version |
| `os_delegated_outcome` | Parent-child effect relationship and verification requirements |
| `os_subagent_spec` | Immutable temporary Specialist definition |
| `os_subagent_run` | Durable logical run |
| `os_subagent_attempt` | Agent Loop attempt, owner lease, and state |
| `os_decision_turn` | Cognitive turn and observation inputs |
| `os_model_invocation` | Model call, token usage, timing, and errors |
| `os_invocation_manifest` | Content-addressed attempt configuration |
| `os_budget_reservation` | Hierarchical reservation and settlement |
| `os_resource_lease` | Shared-resource lease |
| `os_candidate_result` | Typed candidate result |
| `os_dso_event` | Outbox, audit, and frontend event source |

### 10.2 Common Fields

Every mutable business table includes at least:

```text
owner_id
revision
status
created_at
updated_at
```

Immutable definition tables use `definition_hash` or `content_hash`. Every user query includes `owner_id`; administrator cross-user queries require explicit administrative permission.

### 10.3 Critical Unique Constraints

- `(owner_id, proposal_id)`
- `(owner_id, subagent_run_id)`
- `(subagent_run_id, attempt_no)`
- `(subagent_run_id, idempotency_key)`
- `(subagent_attempt_id, sequence)` for decision turns
- `(content_hash)` for invocation manifests
- Conflict constraints for active `(resource_ref, mode)` leases
- Single-owner constraint for an active `(subagent_run_id)` attempt

PostgreSQL may use partial unique indexes. MySQL uses explicit owner rows and conditional updates to enforce equivalent semantics.

### 10.4 Atomic Transactions

The following operations are atomic:

- Accept proposal and materialize delegated outcome and Subagent spec.
- Record admission, reserve budget, and create run.
- Acquire attempt-owner lease and update attempt state.
- Create reserved action attempt with idempotency key.
- Acquire resource lease and bind it to action attempt.
- Persist observation, terminalize action attempt, and release lease.
- Terminalize attempt, settle budget, and append outbox event.

External model and device calls never run inside a database transaction. The implementation uses short transactions, leases, an outbox, and idempotent callbacks.

### 10.5 Data Retention

- Manifests, decisions, verification results, and audit events are retained long term.
- Prompt and context retain only redacted references and hashes; source content follows user retention policy.
- Model deltas follow product settings and do not permanently retain raw user content by default.
- Secret values never enter a DSO table.

---

## 11. Control Plane and Decision Runtime Protocol Draft

### 11.1 Commands

```text
ProposeDelegation
EvaluateDelegation
StartSubagentRun
StartSubagentAttempt
ContinueDecisionTurn
CancelSubagentRun
CancelSubagentAttempt
SubmitActionProposal
SubmitCandidateResult
HeartbeatAttempt
```

### 11.2 Events

```text
DelegationProposed
DelegationAccepted / DelegationRejected
SubagentAdmitted / SubagentDenied
SubagentRunStarted / Waiting / Completed / Failed / Cancelled
SubagentAttemptStarted / Superseded / TimedOut
DecisionTurnStarted / DecisionTurnCompleted
ActionProposed
ObservationAvailable
BudgetReserved / Committed / Released
ResourceLeaseAcquired / Released / Expired
VerificationCompleted
```

### 11.3 Minimum Envelope

```yaml
message_id: string
correlation_id: string
causation_id: string
trace_id: string
owner_id: string
goal_id: string
task_step_id: string
subagent_run_id: optional
subagent_attempt_id: optional
sequence: integer
idempotency_key: string
schema: string
payload: object
created_at: timestamp
```

### 11.4 Protocol Rules

- Commands are delivered at least once, and handlers are idempotent.
- Run and attempt events increase monotonically by entity sequence.
- Writes with stale revision, invalid owner lease, or superseded attempt are rejected.
- Runtime disconnection does not change Control Plane authority.
- Runtime submits candidates and progress but cannot mutate authoritative persistence directly.
- v0alpha starts with internal HTTP/gRPC plus database outbox. It does not introduce new messaging infrastructure prematurely.

---

## 12. Delegation and Specialist Selection

### 12.1 Default Fast Path

The default decision is `LOCAL`. Delegation is normally rejected when:

- The task is one low-risk step.
- Coordination cost exceeds expected quality or context benefit.
- An independent `DelegatedOutcomeSpec` cannot be formed.
- Budget or time is insufficient for external verification.
- The task requires continuous control of one browser resource and splitting would increase contention.

### 12.2 Delegation Signals

```text
DelegationScore =
    Complexity
  + ParallelismValue
  + SpecializationValue
  + ContextIsolationValue
  + VerificationValue
  + RecoveryValue
  - CoordinationCost
  - ExpectedLatencyCost
  - BudgetPressure
```

DSO-W2 uses versioned rules plus LLM judgment. The proposal records the scorer version and inputs. DSO-W7 may generate candidate policies from experience.

### 12.3 Specialist Resolution Order

```text
1. Exact reviewed SpecialistProfile
2. Reviewed general profile plus constrained declarative overlay
3. Main Agent local execution
4. Ask the user or report unavailable capability
```

An ad-hoc overlay may change only:

- Role description.
- Current delegated outcome.
- Requested capability set.
- Requested context scope.
- Output-schema parameters.

It may not:

- Add capabilities absent from the parent task.
- Modify the security system prompt.
- Introduce executable code, scripts, or a new provider.
- Persist itself as a production Specialist profile.

### 12.4 Progressive Delegation

```text
Level 0: Main Agent Fast Path
Level 1: Single predefined Specialist
Level 2: Parallel predefined Specialists
Level 3: Bounded Specialist DAG
Level 4: Ad-hoc declarative Specialist overlay
Level 5: Evaluated learned delegation policy
```

Each level is enabled only after the prior level produces accepted evidence.

---

## 13. Security, Privacy, and Governance

### 13.1 Secret Boundary

Secret values exist only in Credential Store and short-lived Capability Executor memory:

```text
Agent sees secret_handle_ref
    -> governed ActionAttempt
    -> Capability Executor resolves handle
    -> external request
    -> response is redacted before Observation
```

Secret scanning and rejection cover:

- Invocation manifest.
- Prompt and context slice.
- Candidate result.
- Observation and evidence.
- World fact and experience record.
- Log, trace, and frontend event.

### 13.2 Prompt Injection

- Web pages, files, and third-party API content default to `untrusted_external`.
- Context Builder preserves provenance and taint and never inserts external content into System Instruction.
- Actions suggested by external content cannot elevate risk or request additional secrets automatically.
- Evidence agents verify claims and never execute instructions contained in evidence.

### 13.3 Permission Inheritance

```text
Capabilities(Subagent) subset of Capabilities(Task)
Permissions(Subagent) subset of Permissions(Parent)
RiskCeiling(Subagent) <= RiskCeiling(Parent)
Budget(Subagent) <= RemainingBudget(Parent)
```

Deny overrides allow. Every temporary capability view expires and binds to one run.

### 13.4 User Control

- High-risk actions continue to require approval.
- Users can inspect delegation reason, model, budget, sources, and actions.
- Users can cancel a task or one Specialist.
- Users can disable dynamic Specialists per Agent.
- Users can disable experience retention or delete retained history.

---

## 14. Observability and Frontend Experience

### 14.1 Unified Timeline

Every event correlates as many of these identifiers as apply:

```text
trace_id
goal_id
task_step_id
delegation_proposal_id
subagent_run_id
subagent_attempt_id
decision_turn_id
model_invocation_id
plan_candidate_id
plan_run_id
action_attempt_id
observation_id
verification_id
```

### 14.2 Required Metrics

- Delegation proposal, accept, and reject counts.
- Fast Path ratio.
- Produced, partial, failed, and externally verified results per Specialist.
- Model token usage, invocation count, time to first token, and total latency.
- Capability invocation start, end, duration, and complete error chain.
- Coordination overhead, duplicate search rate, and duplicate page rate.
- Budget reserve, commit, release, and reconciliation difference.
- Lease wait, conflict, expiry, and forced-recovery counts.
- Cancellation propagation latency.
- Late-result, reconciliation, and recovery counts.
- Prompt-injection, secret-redaction, and policy-deny counts.

### 14.3 Frontend Timeline

The UI presents one expandable task timeline:

```text
Main Task
  Isaac Research Specialist       completed / verified
  Gazebo Research Specialist      running / 42% budget
  AWSIM Research Specialist       waiting for source
  Evidence Specialist             blocked by dependencies
```

Opening a Specialist displays:

- Delegation reason.
- Delegated outcome and verification requirements.
- Admitted capabilities without exposing unavailable or unauthorized capabilities.
- Exact model and runtime-artifact version.
- Search, fetch, browser, and other action timing.
- Evidence links, contradictions, and verification results.
- Allowed cancel, retry, and takeover operations.

---

## 15. Staged Delivery Plan

DSO stages are workstream stages, not necessarily product versions. A product release may include one or more completed DSO stages, but stage order cannot be skipped.

### 15.1 DSO-W0: Semantic Contracts and State Machines

#### Problems Solved

- Convert the frozen architecture into compilable and testable `draft/v0alpha` types.
- Remove ambiguity among run, attempt, turn, action attempt, manifest, lease, and budget.
- Reject illegal transitions before database and RPC implementation begins.

#### Entry Criteria

- This architecture document is approved.
- Existing Athena v0alpha Effect semantic tests pass.
- Protocol v4 Action/Observation is explicitly out of scope for this stage.

#### Deliverables

- `athena-protocol/draft/dso/v0alpha` types, constants, and validations.
- Six state machines: proposal, run, attempt, action-attempt extension, lease, and budget reservation.
- JSON Schema and Go schema tests.
- Automated checks for every DSO core invariant.
- ADRs for single delegation authority, unified execution, and secret boundary.
- Legacy-to-new object migration map.

#### Tests

- Unit tests for every allowed state transition.
- Rejection tests for every illegal reverse transition.
- Stable canonical hash and JSON round-trip tests.
- Expired policy and changed world-read-set tests.
- Secret fixtures rejected by manifest and observation validation.
- Property tests for budget conservation and one-active-attempt ownership.

#### Acceptance Criteria

- `go test ./...` passes in `athena-protocol`.
- Every frozen invariant has at least one automated test.
- No object represents both run and attempt semantics.
- `ActionProposal` schema has no executable state.
- Invocation-manifest validation rejects plaintext secrets and unknown executable code.
- State-machine tests cover 100% of defined transition edges.

#### Exit Gate

All validation and transition tests pass before DSO-W1 starts. Fields remain `v0alpha`.

### 15.2 DSO-W1: Durable Control Plane Authority

#### Problems Solved

- Move delegation lifecycle from Runtime memory into durable storage.
- Establish the single logical Delegation Orchestrator.
- Recover run, attempt, budget, cancellation, and heartbeat state after process restart.

#### Entry Criteria

- DSO-W0 exit gate passed.
- Database migration test environments are available.

#### Deliverables

- DSO persistence objects, repositories, services, and migrations.
- Durable Delegation Orchestrator loop.
- Optimistic revision, entity sequence, and outbox.
- Attempt-owner lease and heartbeat.
- Budget Ledger Reserve/Commit/Release.
- Cancellation tree and startup recovery scanner.
- A compatibility adapter that never executes the legacy path directly.

#### Tests

- Multiple instances compete for one run; only one owner succeeds.
- Kill and recover at CREATED, RUNNING, WAITING_OBSERVATION, and CANCEL_REQUESTED.
- Attempt 1 times out, Attempt 2 succeeds, and Attempt 1's late result is fenced.
- One hundred concurrent reservations never exceed the parent budget.
- Cancellation releases leases and remaining reservations within the limit.
- Duplicate outbox delivery does not create duplicate runs or charges.

#### Acceptance Criteria

- All orphan attempts are detected and handled within 30 seconds after restart.
- A run never has more than one valid attempt owner.
- Budget overrun is zero in concurrency tests.
- Duplicate commands do not create duplicate logical objects.
- Cancellation prevents new decision turns within 5 seconds and cleans cancellable calls within 30 seconds.
- Every state transition emits trace, causation, and audit information.

#### Exit Gate

PostgreSQL and MySQL transaction and concurrency suites pass. Legacy calls may remain only through the adapter.

### 15.3 DSO-W2: Single Specialist Decision Loop

#### Problems Solved

- Connect Control Plane and Decision Runtime.
- Let Main Agent select Fast Path or propose one Specialist.
- Separate requested from admitted capability and context.
- Complete a side-effect-free Research Specialist loop.

#### Entry Criteria

- DSO-W1 exit gate passed.
- At least one reviewed Research `SpecialistProfile` runtime artifact exists.

#### Deliverables

- Supervisor `DelegationProposal` output schema.
- Delegation decision rules plus LLM judgment v1.
- Context Builder with classification, redaction, provenance, and taint.
- Artifact Resolver producing an attempt `InvocationManifest`.
- Specialist worker DecisionTurn and ModelInvocation loop.
- Typed candidate result plus external verification.
- Removal of the old request-scoped manager from production entry points.

#### Golden Path

Compare Isaac Sim, Gazebo, and AWSIM across ROS 2 support, sensors, robot types, and development cost. This stage delegates one independent research step and keeps other steps serial to validate semantics before parallel optimization.

#### Tests

- Simple questions stay on Fast Path.
- Complex research creates exactly one proposal and run.
- Unauthorized requested capabilities are absent from the admitted view.
- Context contains no other-user data, secret, or irrelevant history.
- Web prompt injection is marked untrusted evidence.
- Candidate self-reporting success does not change outcome state.
- Missing evidence yields `unknown` or `unsatisfied` verification.

#### Acceptance Criteria

- 100% of Subagent attempts bind a valid invocation manifest.
- 100% of candidate results pass typed-schema validation.
- Secret leakage across scanning fixtures is zero.
- Simple-task p95 overhead is below 50 ms and creates no Subagent.
- The single-Specialist golden path completes 20 consecutive runs with a complete durable chain.
- At least 95% of traces connect TaskStep through ModelInvocation to VerificationResult.

#### Exit Gate

The legacy manager production entry is removed. Only Delegation Orchestrator may create a run.

### 15.4 DSO-W3: Unified Action Chain and Browser Vertical Slice

#### Problems Solved

- Prove that a Subagent cannot use a simplified action path.
- Add actor binding, action-scoped resource leases, and critical precondition recheck.
- Continue the same attempt with a new decision turn after receiving an observation.

#### Entry Criteria

- DSO-W2 exit gate passed.
- Browser Runtime exposes stable session/tab identity, resource version, and sanitized observation.

#### Deliverables

- Deterministic ActionProposal-to-PlanCandidate adapter.
- Separate Subagent admission policy and action policy.
- Resource Lease Service bound to ActionAttempt ownership.
- Browser session affinity and tab single-writer behavior.
- Observation return into the Specialist decision loop.
- User takeover, page-drift, and cancellation handling.

#### Tests

##### Automated Golden Path

Using a deterministic local browser fixture:

1. Open a video-list page.
2. Read the first three titles and summaries.
3. Select the best match for a requested topic.
4. Click the corresponding card.
5. Observe playback state.
6. Verify that the selected video is playing.

##### Manual Staging Path

Run the same flow against YouTube or a similar live site. External-site stability is not a CI pass/fail dependency.

##### Fault Injection

- User changes page between precheck and lease acquisition.
- Tab closes or resource version changes.
- Two action attempts request the same tab write lease.
- Browser disconnects and reconnects.
- Click succeeds but observation is lost, producing unknown outcome and reconciliation.
- User cancels during model inference, lease wait, and action execution.

#### Acceptance Criteria

- Every browser action has a PlanCandidate and action PolicyDecision.
- No code path calls Launcher directly from a Subagent tool call.
- One tab never has more than one valid writer.
- Critical recheck blocks every stale action in the test suite.
- Cancellation produces no new browser action.
- Fixture golden path succeeds at least 96% over 50 consecutive runs.
- Failures return concrete state and complete error chain, never empty response or false success.

#### Exit Gate

Browser fixture, live staging, and fault-injection reports are complete, and static unified-path checks pass.

### 15.5 DSO-W4: Parallel Specialists and Result Aggregation

#### Problems Solved

- Execute independent research branches within budget and concurrency limits.
- Handle duplicate search, conflicting evidence, partial results, and dependency DAGs.
- Demonstrate whether multiple Specialists outperform a single Agent.

#### Entry Criteria

- DSO-W3 exit gate passed.
- Single-Specialist cost, latency, and quality baselines are recorded.

#### Deliverables

- Parallel run scheduling and `max_parallelism`.
- Task dependency gates.
- Typed Result Aggregator.
- Claim deduplication, evidence correlation, and contradiction detection.
- Evidence and Synthesis Specialist profiles.
- Frontend DAG and live progress.

#### Golden Path

```text
Isaac Specialist ----+
Gazebo Specialist ---+--> Evidence Specialist --> Result Aggregator
AWSIM Specialist ----+
```

#### Tests

- Three branches run concurrently while a fourth waits for dependencies.
- One branch fails and policy chooses retry, replacement, partial result, or user input.
- Two sources conflict on one claim.
- Provider rate limits reduce concurrency safely.
- Insufficient total budget rejects a new branch rather than overspending.
- Parent cancellation stops every parallel branch.

#### Acceptance Criteria

- Concurrency and total-run limits are never exceeded.
- Conflicting claims are never silently merged into one fact.
- Every important final claim has the configured minimum evidence count.
- Quality or evidence coverage improves significantly over the single-Agent baseline; otherwise parallel delegation remains disabled.
- Coordination token overhead stays below 25% of total tokens.
- Duplicate URL or page fetch ratio stays below 15%.

#### Exit Gate

Submit reproducible comparison reports for Single Agent, Static Specialist, and Dynamic DSO.

### 15.6 DSO-W5: Dynamic Ad-Hoc Specialists

#### Problems Solved

- Create a safe declarative temporary Specialist when no reviewed profile matches exactly.
- Preserve dynamic composition without creating new permissions or mutating production artifacts.

#### Entry Criteria

- DSO-W4 exit gate passed.
- Runtime Artifact Resolver supports `SpecialistProfile` plus declarative overlays.

#### Deliverables

- Ad-hoc `SubagentSpec` builder.
- Reviewed base profile plus constrained overlay.
- Overlay schema, hash, admission, and audit.
- Experience path for `SpecialistProfileCandidate` creation.
- Frontend identity and provenance for temporary Specialists.

#### Tests

- No exact profile causes a temporary role proposal.
- Overlay attempts to add terminal, payment, or file-delete capabilities are denied.
- Overlay containing scripts, prompt overrides, or secrets is denied.
- A temporary Specialist does not become a production profile after completion.
- Repeated success creates only a candidate, never automatic activation.

#### Acceptance Criteria

- Successful capability expansion by an overlay is zero.
- Every overlay has content hash, base-profile reference, and admission decision.
- 100% of temporary Specialists are unavailable for direct cross-user reuse after the run.
- Production exposure of unreviewed candidates is zero.
- Dynamic Specialists outperform Main Agent fallback on the target benchmark; otherwise the feature remains disabled.

#### Exit Gate

Security review, prompt-injection suite, and cross-tenant isolation tests pass.

### 15.7 DSO-W6: Recovery, Replay, Security, and Production Hardening

#### Problems Solved

- Preserve correctness across crashes, partitions, provider failures, device outages, and HA contention.
- Make the system replayable, auditable, operable, and ready for controlled production use.

#### Entry Criteria

- DSO-W5 exit gate passed.
- Every core path emits invocation manifests and structured events.

#### Deliverables

- Replay runner with exact-config, recorded-observation simulation, and live re-execution modes.
- Chaos and fault-injection suite.
- HA owner lease, leader failover, and split-brain protection.
- SLO dashboards, alerts, and administrator diagnostics.
- Data retention, export, and deletion utilities.
- Threat model and penetration-test report.

#### Tests

##### Failure Matrix

- Control Plane crashes before and after transaction commit.
- Runtime disconnects during streamed model invocation.
- Device disconnects after a side effect but before observation.
- Database is temporarily unavailable or fails over.
- Provider returns 429, 500, or timeout.
- Lease owner disappears and an old owner later returns.
- Secret-like data appears in page content, model output, or executor response.
- Policy version changes during a run.

#### Acceptance Criteria

- Confirmed side-effect actions are never executed twice.
- Late results never contaminate an active run.
- Scheduling recovers within 30 seconds after HA failover.
- Cancellation propagation p95 is below 5 seconds; non-cancellable external actions explicitly enter reconciliation.
- Manifest replay explains the exact model, prompt, context, capability, and artifact versions for every golden path.
- Plaintext secret persistence and log leakage are zero in security scans.
- Pre-production load testing meets 99.9% Control Plane availability.

#### Exit Gate

Production Readiness Review, recovery exercise, and rollback exercise are approved.

### 15.8 DSO-W7: Governed Delegation Learning

#### Problems Solved

- Learn when to delegate, which profile to select, and how much budget and context to grant.
- Improve DSO without directly changing production behavior.

#### Entry Criteria

- DSO-W6 exit gate passed.
- Sufficient successful, failed, Fast Path, and user-intervention experience exists.

#### Deliverables

- `DelegationPolicyCandidate`.
- `SpecialistProfileCandidate`.
- Offline evaluation, replay, shadow, canary, and rollback.
- Benchmark dashboard for Single Agent, Static Specialist, and Dynamic DSO.
- Human candidate-review interface.

#### Tests

- A candidate cannot activate without review.
- Shadow runs produce no external side effects.
- Canary applies only to allowed users and low-risk tasks.
- Metric regression triggers automatic rollback.
- Users who disable learning produce no user-derived candidate.

#### Acceptance Criteria

- Production exposure of unpromoted candidates is zero.
- Canary rollback completes within one minute after threshold breach.
- A new strategy improves at least one primary quality, cost, latency, or recovery metric without degrading safety.
- Every promotion traces to source experiences, evaluation, and approver.
- Learned delegation can be disabled instantly and fall back to the rule policy.

#### Exit Gate

Only artifacts that complete the full governance pipeline may enter the default `AgentBuild`.

---

## 16. Cross-Stage Test Matrix

| Test category | W0 | W1 | W2 | W3 | W4 | W5 | W6 | W7 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Schema / validation | Required | Required | Required | Required | Required | Required | Required | Required |
| State transitions | Required | Required | Required | Required | Required | Required | Required | Required |
| Database concurrency |  | Required | Required | Required | Required | Required | Required | Required |
| Runtime contract |  | Draft | Required | Required | Required | Required | Required | Required |
| Secret / taint | Basic | Basic | Required | Required | Required | Required | Required | Required |
| Browser fixture |  |  |  | Required | Required | Required | Required | Regression |
| Parallel / DAG |  |  |  |  | Required | Required | Required | Required |
| Crash recovery |  | Required | Required | Required | Required | Required | Required | Required |
| Replay |  |  | Basic | Basic | Comparison | Comparison | Required | Required |
| Shadow / canary |  |  |  |  |  |  | Basic | Required |

Every stage continues to run Athena's existing tests. Passing DSO tests never justifies breaking Fast Path, Action/Observation, Browser, Goal, Learning, or Deployment behavior.

---

## 17. Initial Acceptance Scenarios

### 17.1 Research Golden Task

Goal: Compare Isaac Sim, Gazebo, and AWSIM across ROS 2, sensors, robot types, and development cost, then provide an evidence-backed recommendation.

Validate:

- Rational proposal decomposition.
- Context isolation.
- Evidence deduplication and contradiction detection.
- External verification rather than candidate self-approval.
- Measurable benefit from parallelism.

### 17.2 Browser Golden Task

Goal: In one browser session, inspect the first three videos in a list, select the best topic match, and play it.

Validate:

- Stable target identity and tab affinity.
- Action proposal entering the unified Plan chain.
- Tab single-writer and resource version.
- Critical recheck after user page changes.
- Playback observation and verification.

### 17.3 Failure Golden Task

Inject into the preceding tasks:

- Model timeout.
- Provider rate limit.
- Runtime restart.
- Browser disconnect.
- User closes tab.
- Cancellation.
- Budget exhaustion.
- Capability offline.
- Late result.
- Prompt injection.
- Secret-like content.

Acceptance does not require eventual success in every case. The system must stop safely, recover, replan, or ask the user explicitly. It may not return an empty result, claim false success, duplicate a side effect, or lose the originating error chain.

---

## 18. Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Supervisor becomes a god object | Hard to recover and test | LLM proposes candidates; orchestrator, context, admission, and aggregator remain separate |
| Agent-count explosion | Unbounded cost and latency | Fast Path, depth 1, reservation ledger, expected-value gate |
| Browser tab contention | Action targets the wrong page | Actor binding, action-scoped single-writer lease, version recheck |
| Subagent self-declares success | False completion | Typed candidate plus external verification |
| Two execution paths | Broken safety boundary | Remove legacy manager; static check requires Plan and Policy for every action |
| Prompt injection propagation | Privilege escalation or data leak | Trust class, taint, context builder, evidence/instruction separation |
| Secret leakage | Critical security incident | Secret handles, executor-boundary resolution, full-chain scanner |
| Budget race | Overspending | Atomic reservation ledger, optimistic revision, property tests |
| Late result | New result overwritten | Attempt generation fencing, owner lease, audit-only retention |
| Unrealistic replay | Invalid evaluation | Separate exact-config, recorded-observation, and live replay |
| Dynamic profile reaches production directly | Ungoverned self-modification | Candidate, review, shadow, canary, promotion |

---

## 19. Definition of Done

The DSO workstream is complete only when all are true:

1. Main Agent automatically chooses Fast Path or proposes a constrained delegation.
2. A safe declarative temporary Specialist can be created when no exact profile exists.
3. Run, attempt, turn, manifest, budget, and lease are traceable and recoverable after restart.
4. Subagent capability, permission, context, risk, and budget remain subsets of the parent task.
5. Every external action uses `PlanCandidate`, `PolicyDecision`, `PlanRun`, and `ActionAttempt`.
6. Shared resources such as browser tabs have no concurrent write conflict.
7. A Subagent cannot declare outcome success, and all required effects have verification results.
8. Secrets never enter the intelligence plane, business persistence, logs, or traces.
9. Users can inspect delegation, progress, evidence, and cost and can cancel or take over.
10. Dynamic Specialists and learned policies both use the artifact-governance pipeline.
11. Single Agent, Static Specialist, and Dynamic DSO have reproducible comparison evaluations.
12. Fault injection produces no silent failure, false success, double execution, or lost error origin.

---

## 20. Delivery Order and Release Rules

Strict order:

```text
DSO-W0 Contracts
  -> DSO-W1 Durable Authority
  -> DSO-W2 Single Specialist
  -> DSO-W3 Browser Governed Action
  -> DSO-W4 Parallel DAG
  -> DSO-W5 Ad-hoc Specialist
  -> DSO-W6 Production Hardening
  -> DSO-W7 Governed Learning
```

Release rules:

- Each workstream stage receives an independent commit containing code, tests, acceptance report, and unresolved risks.
- Production implementation of the next stage cannot start before the previous exit gate passes.
- Schema changes update `athena-protocol`, Control Plane, Runtime, and Frontend types together.
- v0alpha may make breaking field changes but may not violate frozen invariants.
- Cross-repository tags require dependency-version and `RunManifest` resolution validation.
- DSO is disabled by default. It may be enabled in development after W3 and in production canary only after W6.

---

## 21. Immediate Next Step

Begin DSO-W0:

1. Create `draft/dso/v0alpha` in `athena-protocol`.
2. Implement six state machines and validation.
3. Write ADRs for single delegation authority, unified execution, and secret boundary.
4. Create deterministic Research and Browser golden fixtures.
5. Produce the DSO-W0 Engineering Acceptance report.

Before DSO-W0 passes, do not create production tables, modify frozen Protocol v4, or extend the legacy `spawn/delegate` path.
