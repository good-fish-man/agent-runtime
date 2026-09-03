# Athena Governed Dynamic Specialist Orchestration 技术架构与实施计划

**语言：** **简体中文** | [English](./athena-governed-dynamic-specialist-orchestration-plan.en-US.md)

> 中英文版本表达同一架构语义。修改冻结不变量、阶段门禁或验收标准时，必须同步更新两份文档。

| 字段 | 值 |
| --- | --- |
| 文档类型 | Architecture Baseline + Implementation Plan |
| Workstream | DSO（Dynamic Specialist Orchestration） |
| 语义基线 | FROZEN |
| 实现阶段 | DSO-W0 |
| Object Schema | `draft/v0alpha` |
| Persistence Schema | `draft/v0alpha` |
| Event Protocol | `draft/v0alpha` |
| Runtime / Control Plane Protocol | `draft/v0alpha` |
| Lease Protocol | `draft/v0alpha` |
| Budget Protocol | `draft/v0alpha` |
| 适用项目 | `athena-protocol`、`agent-runtime-client`、`agent-runtime`、`athena-launcher`、`frontend/agent-ui` |

---

## 1. 文档目的

本文档把 Athena Dynamic Subagent 的已冻结语义转换为可执行的工程计划，回答以下问题：

1. 动态 Specialist 在 Athena 中是什么，不是什么。
2. 谁可以提出委派，谁拥有持久化状态，谁可以执行动作。
3. Subagent 如何继续服从 Outcome、Plan、Policy、Action、Observation 与 Verification 主链。
4. 如何处理上下文隔离、能力裁剪、预算、并发资源、取消、重试、恢复和 Replay。
5. 各仓库分别负责什么，哪些现有实现需要复用、替换或删除。
6. DSO 每个阶段解决什么问题，交付什么，以及如何判断阶段真正完成。

本文档不是字段冻结文档。对象字段、数据库表、事件载荷和 RPC 在真实垂直切片验证前保持 `draft/v0alpha`，但责任边界、执行权、不变量和唯一执行主链不再随意改变。

---

## 2. 执行摘要

Athena 不建设一套独立的自由 Multi-Agent 网络。它在现有 Agent OS 内增加一种受治理的动态智能组合能力：

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

Subagent 是一个临时、受约束的 Decision Actor，不是执行权威。它可以研究、分析、验证、提出 ActionProposal 和 PlanFragmentProposal，但不能：

- 自行声明目标成功。
- 直接创建权威 PlanCandidate。
- 绕过 PolicyDecision、PlanRun 或 ActionAttempt。
- 直接修改 WorldFact。
- 自行扩大 Capability、权限或预算。
- 接触原始密码、Cookie、API Key 或 Token。
- 直接发布 Skill、Strategy、Plugin 或 SpecialistProfile。

所有影响外部世界的动作，无论来自 Main Agent、Subagent、Skill、Automation、Plugin、Evolution 还是未来 Robot Agent，只允许经过一条执行路径：

```text
ActionProposal
    -> PlanCandidate
    -> PolicyDecision
    -> PlanRun
    -> ActionAttempt
    -> Observation
```

核心定义：

> Effect-Grounded Delegation + Durable Agent Loop + Unified Governed Execution + External Verification

---

## 3. 目标与非目标

### 3.1 目标

DSO 最终应支持：

- Main Agent 根据 TaskStep 的复杂度、可并行度、领域专业性、上下文压力和验证价值，决定是否提出委派。
- 优先复用经过审核的 SpecialistProfile；没有合适 Profile 时，可生成受限的临时 Specialist Spec。
- 为每个 Subagent 提供最小必要上下文和最小权限 Capability View。
- 单 Specialist、并行 Specialist 和有界 DAG 均可持久化、取消、恢复和重放。
- 每次 SubagentRun 具有独立预算、截止时间、ActorBinding 和验证要求。
- 每次 SubagentAttempt 固定准确的模型、Prompt、Context、Schema、Capability 和 Runtime Artifact 版本。
- Subagent 的动作请求统一进入 Athena Execution Kernel。
- 浏览器 Tab、Terminal、File、Robot Arm 等共享资源通过短期 Lease 防止竞争。
- Subagent 只返回 TypedCandidateResult；成功由外部 VerificationResult 判定。
- Experience 可以生成 SpecialistProfileCandidate 或 DelegationPolicyCandidate，但不能自动进入生产。

### 3.2 非目标

DSO-W0 至 DSO-W5 明确不实现：

- Agent 之间自由聊天或任意点对点通信。
- Subagent 自主创建下一层 Subagent。
- 无预算、无期限、无限递归的 Agent 群体。
- Subagent 直接拥有设备、Capability Provider 或 World Model。
- 将网页内容作为系统指令。
- 生成任意代码并直接执行。
- 未经 Replay、Review、Shadow、Canary 的自动生产发布。
- 为兼容旧 `spawn/delegate` 保留第二条执行路径。

### 3.3 初始生产约束

```yaml
delegation:
  max_depth: 1
  max_parallelism: 3
  max_total_subagents_per_task: 8
  allow_subagent_delegation: false
  require_typed_result: true
  require_external_verification: true
```

这些是默认上限，不是调用方可自行放宽的建议值。放宽必须经过版本化 Policy 和评测。

---

## 4. 当前基线与差距

### 4.1 可直接复用的现有能力

| 能力 | 当前位置 | DSO 用法 |
| --- | --- | --- |
| Effect-Centric 语义 | `athena-protocol/draft/v0alpha` | 复用 OutcomeSpec、PlanCandidate、PolicyDecision、PlanRun、ActionAttempt、VerificationResult |
| 长期 Goal 与 Task Graph | `athena-protocol/protocol/orchestration/v2` | TaskStep 和依赖图继续作为父级任务系统 |
| Control Plane Orchestration | `agent-runtime-client/application/service/orchestration` | 扩展为唯一持久化 Delegation Orchestrator |
| RunManifest / AgentBuild | `agent-runtime-client/application/service/deployment` | 作为 Attempt InvocationManifest 的父级构建快照 |
| Runtime Artifact Resolver | `agent-runtime-client` 与 `agent-runtime/internal/runtimeartifact` | 固定 SpecialistProfile、Prompt、Schema、Skill、Strategy 版本 |
| Action / Observation v4 | `athena-protocol/protocol/v4` | 保持唯一设备执行协议，不为 DSO 新建旁路 |
| Device Control Plane | `agent-runtime-client/application/service/control` | 路由 Action、持久化 Observation、取消与设备状态 |
| Experience / Learning / Evolution | 对应 protocol 与 service | 记录 DSO 经验，生成候选但不直接激活 |

### 4.2 必须替换的旧实现

以下实现与冻结语义不一致，不能作为第二条路径长期保留：

| 旧实现 | 问题 | 处理方式 |
| --- | --- | --- |
| `agent-runtime/internal/subagent` 请求级 Manager | 状态只在内存中；任务 ID 和生命周期不具备全局持久性 | DSO-W2 后删除，由 Control Plane Delegation Orchestrator 取代 |
| `dispatcher/tools.go` 按请求注册 `spawn/delegate` | 仅能调用预配置 Subagent，且模型直接拥有委派工具 | 改为向 Control Plane 提交 DelegationProposal |
| `protocol/orchestration/v2.SpecialistTask` | 混合 TaskStep、角色、预算、Attempt 等概念 | DSO 草案中拆分 TaskStep、SubagentSpec、SubagentRun、SubagentAttempt |
| `os_specialist_run` | 只保存聚合内容，无法表达 Attempt、Manifest、Lease、预算和恢复 | DSO-W1 建立新表；旧表停止写入后删除 |
| 当前 `Supervisor` 命名 | 同时容易被理解为 LLM Supervisor 和持久化调度器 | 文档和代码中区分 Decision Supervisor 与 Delegation Orchestrator |

硬规则：

> There must be exactly one logical durable delegation authority.

迁移期间允许 Compatibility Adapter，但 Adapter 只能把旧调用翻译成新 DelegationProposal，不能自己执行 Subagent。

---

## 5. 冻结架构不变量

### 5.1 Athena Kernel 不变量

1. Athena 中所有影响外部世界的动作只有一条受治理执行路径。
2. ActionProposal 不可执行；只有允许的 PlanCandidate 才能产生 PlanRun。
3. PolicyDecision 必须在 PlanRun 和 ActionAttempt 执行前有效。
4. Observation 是外部执行结果的净化表示，不是模型自述。
5. 只有 World State Authority 可以提交 WorldFact。
6. 只有 VerificationResult 可以判定 EffectClause 是否满足。
7. Secrets never enter the intelligence plane.

### 5.2 DSO 不变量

1. DelegationDecision 必须评估一个具体且不可混淆的 DelegationProposal。
2. Proposal 可以包含草案；只有接受的草案才能物化为版本化不可变对象。
3. 一个 Subagent 只拥有一个 Delegated TaskStep Scope。
4. 每个 Subagent 必须绑定 DelegatedOutcomeSpec。
5. Subagent 不能声明自己的 Outcome 已满足。
6. Subagent 只能提出 ActionProposal 或 PlanFragmentProposal。
7. Subagent 不能直接创建权威 PlanCandidate。
8. 只读动作和副作用动作使用同一治理协议；R0 可以自动准入，但不能绕过审计。
9. RequestedCapabilitySet 不等于 AdmittedCapabilityView。
10. 只有 Control Plane 可以生成 AdmittedCapabilityView。
11. RequestedContextScope 不等于实际交付的 RedactedContextSlice。
12. 交付上下文必须经过租户隔离、分类、脱敏和 taint 处理。
13. 外部内容是 Evidence，永远不是 Authority 或 AgentInstruction。
14. Subagent 权限、风险上限和预算必须是父 Task 的子集。
15. SubagentRun 是持久化逻辑执行；SubagentAttempt 是完整可恢复 Agent Loop 尝试。
16. ModelInvocation 不是 SubagentAttempt。
17. DecisionTurn 表示一次认知决策周期。
18. SubagentRun 维持 ActorBinding 和 SessionAffinity；ActionAttempt 持有共享可变资源 Lease。
19. 可变资源 Lease 必须短期、带 TTL，并以 ActionAttempt 为 owner。
20. 父级已消费预算加所有活跃子级预留不得超过父预算。
21. 预算采用 Reserve、Commit、Release 语义。
22. 取消必须传播到所有后代执行单元，并释放 Lease 和未使用预算。
23. 迟到或已被替代 Attempt 的结果不能修改活跃 Run 状态。
24. 每个 Attempt 必须绑定可重放的 InvocationManifest。
25. InvocationManifest 继承父 RunManifest，只记录本次执行差异。
26. Credential 只能以 Secret Handle 引用，不能嵌入 Prompt、Context、Manifest、Observation、WorldFact、Experience、Log 或 Trace。
27. SpecialistProfile 是 RuntimeArtifact，不是独立权威 Registry。
28. Delegation Orchestrator 是唯一逻辑持久化委派权威，物理上允许 HA 多实例。
29. Delegation Orchestrator 属于 Control Plane；Supervisor 与 Specialist reasoning 属于 Decision Runtime。
30. Learning 只能提出 DelegationPolicyCandidate 或 SpecialistProfileCandidate，不能直接激活。

---

## 6. 总体技术架构

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
|   |                                                                          |
|   +-> Proposal Store / Decision Store                                        |
|   +-> Admission Service                                                      |
|   +-> Budget Ledger                                                          |
|   +-> Context Builder                                                        |
|   +-> Runtime Artifact Resolver                                              |
|   +-> Run / Attempt State Machine                                            |
|   +-> Cancellation / Recovery / Retry                                        |
|   +-> Result Aggregator                                                      |
|   +-> Policy / Resource / Device coordination                                |
+-----------------------------------+------------------------------------------+
                                    |
                     Decision requests / typed events
                                    |
                                    v
+----------------------------- Decision Runtime -------------------------------+
| agent-runtime                                                                |
|                                                                              |
| LLM Supervisor                                                               |
|   +-> Task decomposition proposal                                            |
|   +-> DelegationProposal candidate                                           |
|   +-> Result synthesis proposal                                              |
|                                                                              |
| Specialist Worker                                                            |
|   +-> DecisionTurn                                                           |
|   +-> ModelInvocation                                                        |
|   +-> ActionProposal / PlanFragmentProposal                                  |
|   +-> TypedCandidateResult                                                   |
+-----------------------------------+------------------------------------------+
                                    |
                          governed ActionProposal
                                    |
                                    v
+----------------------------- Execution Kernel -------------------------------+
| PlanCandidate -> PolicyDecision -> PlanRun -> ActionAttempt                  |
|                                    |                                         |
|                                    v                                         |
|                    Device Runtime / Capability Executor                      |
|                                    |                                         |
|                                    v                                         |
|                      Sanitized Observation / Evidence                        |
+-----------------------------------+------------------------------------------+
                                    |
                                    v
                        Verification / World Authority
                                    |
                                    v
                         Parent Outcome Evaluation
```

### 6.1 Control Plane：`agent-runtime-client`

Control Plane 拥有：

- 所有 DSO 持久化对象和状态机。
- Proposal 物化、Admission、Policy 协调和预算预留。
- Context Builder 与 Secret Handle 边界。
- Runtime Artifact 解析和 InvocationManifest 生成。
- Run/Attempt 的租约、心跳、重试、取消和恢复。
- ActionProposal 接入统一 Plan 主链。
- ResourceLease 和 ActorBinding 协调。
- Result Aggregation 与 Verification 协调。
- 用户、租户、Agent、模型和设备归属校验。

Control Plane 不负责生成复杂自然语言计划或模拟模型推理。

### 6.2 Decision Runtime：`agent-runtime`

Decision Runtime 负责：

- LLM Supervisor 生成 Task decomposition proposal 和 DelegationProposal。
- Specialist 按 InvocationManifest 执行 DecisionTurn。
- 输出 ActionProposal、PlanFragmentProposal、Evidence Candidate 和 TypedCandidateResult。
- 把 Observation 作为下一 DecisionTurn 的输入继续执行 Agent Loop。
- 记录模型 Token、延迟、FinishReason 和结构化调用轨迹。

Decision Runtime 不拥有 durable Run 状态、Policy、Lease、Credential 或最终成功判定。

### 6.3 协议仓库：`athena-protocol`

新增建议目录：

```text
draft/dso/v0alpha/
  types.go
  state.go
  validation.go
  transitions.go
  schema_test.go

schema/dso/v0alpha/
  delegation-proposal.schema.json
  delegated-outcome.schema.json
  subagent-spec.schema.json
  subagent-run.schema.json
  subagent-attempt.schema.json
  decision-turn.schema.json
  invocation-manifest.schema.json
  budget-reservation.schema.json
  resource-lease.schema.json
```

DSO-W0 不修改冻结的 Protocol v4 Action/Observation wire contract。必要的 DSO 关联 ID 先通过 Metadata 或 Control Plane 内部事件承载，经过垂直切片验证后再决定是否升级公开协议。

### 6.4 Device Runtime：`athena-launcher`

Launcher 不运行 Subagent。它只负责：

- 接收已治理 ActionAttempt。
- 根据 resource/version preconditions 执行动作。
- 返回 Sanitized Observation。
- 支持 ActionAttempt 取消。
- 上报 Browser Session、Tab、Window、Terminal 等资源标识和版本。
- 在执行边界解析已授权 Secret Handle；不得把 Secret 回传。

### 6.5 Frontend：`frontend/agent-ui`

Frontend 需要提供：

- Task Graph 与 SubagentRun 实时状态。
- 为什么发生委派、使用哪个 Specialist、预算和耗时。
- Pending Approval、Waiting User、Waiting Device 和失败恢复入口。
- 用户取消整个 Task、单个 SubagentRun 或待执行 Action。
- Evidence、来源链接、VerificationResult 和冲突状态。
- 管理员查看 Artifact 版本、Replay、Shadow 与 Canary 结果。

---

## 7. 冻结端到端主链

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

### 7.1 四种决策不可混淆

| 对象 | 回答的问题 | 所有者 |
| --- | --- | --- |
| DelegationProposal | 建议怎样委派 | Decision Runtime 提出 |
| DelegationDecision | 这个具体委派方案是否值得采用 | Delegation Orchestrator |
| PolicyDecision | 当前环境是否允许创建 Subagent 或执行 Plan | Control Plane Policy |
| VerificationResult | 执行后 Effect 是否真的成立 | Verifier / World Authority |

### 7.2 Fast Path

以下任务默认不创建 Subagent：

- 打开一个已知网页。
- 读取一个已知文件。
- 回答无需工具的简单问题。
- 单一步骤、低上下文、无需独立验证的动作。

Fast Path 不是另一套执行路径。涉及外部动作时仍然经过统一 Plan 主链。

---

## 8. 核心对象模型

### 8.1 DelegationProposal

不可执行的候选委派方案。至少包含：

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

同一 TaskStep 可以存在多个 Proposal；DelegationDecision 必须引用被评估的具体 Proposal 和输入哈希。

### 8.2 DelegatedOutcomeSpec

是父 OutcomeSpec 的受限贡献单元，而不是自然语言任务说明：

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

SubagentResult 不能修改该对象，也不能把自己的状态作为 Effect 满足证据。

### 8.3 SubagentSpec

描述需要什么临时 Decision Actor：

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

SubagentSpec 不包含 API Key，也不直接固定未经 Resolver 验证的 Prompt 文本。

### 8.4 Capability 与 Context

```text
RequestedCapabilitySet
    -> availability / ownership / risk / policy
    -> AdmittedCapabilityView

RequestedContextScope
    -> tenant filter / classification / redaction / taint
    -> RedactedContextSlice
```

ContextItem 最少需要：

```yaml
content_ref: string
source_type: string
trust_class: trusted_system | trusted_user | trusted_internal | untrusted_external
taint_flags: []
classification: public | internal | confidential | restricted
owner_ref: string
content_hash: string
```

任何 `untrusted_external` 内容只能放入明确的 Evidence 区域，不能拼接到 System Instruction 区域。

### 8.5 ActorBinding

描述 SubagentRun 的环境亲和性，不代表独占资源：

```yaml
actor_binding_id: string
device_ref: string
browser_session_ref: optional
terminal_session_ref: optional
environment_ref: string
valid_until: timestamp
```

Browser Tab 等具体资源不得通过 ActorBinding 长期锁定。

### 8.6 SubagentRun

一个持久化逻辑子任务实例：

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

Run 可经历多个 Attempt，但同一时刻最多只有一个拥有有效执行 Lease 的活跃 Attempt。

### 8.7 InvocationManifest

每个 Attempt 绑定一个内容寻址的执行构型：

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

Manifest 只保存 Secret Handle，不保存 Secret 值。Attempt 重试时，如果任一输入构型变化，必须生成新 hash；构型完全相同才允许复用。

### 8.8 SubagentAttempt

一次完整、可恢复的 Agent Loop 尝试：

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

Attempt 不是模型单次调用。一次 Attempt 可以包含多个 DecisionTurn、ModelInvocation、ActionProposal 和 Observation。

### 8.9 DecisionTurn 与 ModelInvocation

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

ModelInvocation 保存 Provider、模型、Token、延迟、FinishReason、错误链和调用时间线。Provider 透明重试必须留下独立 invocation attempt 记录，但不必升级为新的 SubagentAttempt；只有 Agent Loop 的所有权或恢复边界改变时才创建新 SubagentAttempt。

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

这里的 `produced` 只表示候选结果已生成，不表示 DelegatedOutcome 满足。

### 8.11 BudgetReservation

预算是分层账本：

```text
Consumed(parent) + ActiveReservations(children) <= TotalBudget(parent)
```

状态：

```text
REQUESTED -> RESERVED -> COMMITTED
                    \-> RELEASED
                    \-> EXPIRED
```

支持 Token、金额、动作数、查询数、页面数、计算时间和墙钟时间。所有预留与 Run 状态变更必须在同一事务或可靠 Outbox 中关联。

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

正确动作顺序：

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

## 9. 状态机

### 9.1 DelegationProposal

```text
DRAFT -> SUBMITTED -> ACCEPTED
                   -> REJECTED
                   -> SUPERSEDED
                   -> EXPIRED
```

终态不可回到 SUBMITTED。重新评估必须创建新 Proposal。

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

`COMPLETED` 表示执行结束，不等于 Outcome succeeded。Outcome 状态由 VerificationResult 聚合决定。

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

约束：

- 同一 Run 同时最多一个活跃 Attempt。
- Attempt Lease 失效后，旧 owner 不得提交状态变更。
- `FAILED/TIMED_OUT/ABANDONED` 不可回到 RUNNING。
- 重试创建新的 Attempt 和新的幂等键。
- 迟到结果仅作为审计事件保存。

### 9.4 ActionAttempt

DSO 不建立第二套 Action 状态机。扩展现有 ActionAttempt：

```text
RESERVED -> POLICY_ALLOWED -> LEASED -> EXECUTING
          -> POLICY_DENIED                 -> SUCCEEDED
          -> WAITING_APPROVAL              -> FAILED
                                            -> CANCELLED
                                            -> UNKNOWN_OUTCOME
```

`UNKNOWN_OUTCOME` 必须进入 reconcile，不允许直接重试有副作用动作。

### 9.5 取消树

```text
Task / Goal Cancel
  -> PlanRun
  -> SubagentRun
  -> SubagentAttempt
  -> DecisionTurn / ModelInvocation
  -> ActionAttempt
  -> Provider call / Device action
```

取消处理必须：

1. 写入 `cancel_requested` 及原因。
2. 阻止新 DecisionTurn 和 ActionAttempt。
3. 取消模型、网络和设备调用。
4. 释放 ResourceLease。
5. Commit 已实际消费预算，Release 剩余预留。
6. 对不确定副作用执行 reconcile。

### 9.6 恢复规则

Control Plane 启动时扫描：

- Lease 已过期但状态仍运行的 Attempt。
- 无心跳的 ModelInvocation。
- `EXECUTING` 但没有 Observation 的 ActionAttempt。
- 活跃预算预留但父 Task 已终止的记录。
- 已取消但 Lease 未释放的资源。

恢复动作只能是 `resume`、`retry as new attempt`、`reconcile`、`wait user/device` 或 `terminalize`，不能把旧终态对象改回运行态。

---

## 10. 持久化与事务边界草案

### 10.1 建议表

| 表 | 主要用途 |
| --- | --- |
| `os_delegation_proposal` | Proposal、输入哈希、成本收益和状态 |
| `os_delegation_decision` | local/delegate 决策、原因和评估版本 |
| `os_delegated_outcome` | 父子 Effect 关系和验证要求 |
| `os_subagent_spec` | 不可变临时 Specialist 定义 |
| `os_subagent_run` | 持久化逻辑 Run |
| `os_subagent_attempt` | Agent Loop Attempt、owner lease 和状态 |
| `os_decision_turn` | 认知轮次和 Observation 输入 |
| `os_model_invocation` | 模型调用、Token、延迟和错误 |
| `os_invocation_manifest` | Attempt 内容寻址构型 |
| `os_budget_reservation` | 分层预算预留与结算 |
| `os_resource_lease` | 共享资源 Lease |
| `os_candidate_result` | TypedCandidateResult |
| `os_dso_event` | Outbox、审计和前端事件源 |

### 10.2 通用字段

所有可变业务表至少包含：

```text
owner_id
revision
status
created_at
updated_at
```

不可变定义表使用 `definition_hash/content_hash`。所有查询必须带 `owner_id`，管理员跨用户查询需要显式管理权限。

### 10.3 关键唯一索引

- `(owner_id, proposal_id)`
- `(owner_id, subagent_run_id)`
- `(subagent_run_id, attempt_no)`
- `(subagent_run_id, idempotency_key)`
- `(subagent_attempt_id, sequence)` for DecisionTurn
- `(content_hash)` for InvocationManifest
- 活跃 Lease 的 `(resource_ref, mode)` 冲突约束
- 活跃 Attempt 的 `(subagent_run_id)` 单 owner 约束

PostgreSQL 可以使用 partial unique index；MySQL 使用显式 lease owner 行和条件更新实现相同语义。

### 10.4 必须原子的事务

以下操作必须在一个事务内完成：

- 接受 DelegationProposal并物化 Outcome/Spec。
- Admission 决策、预算预留和 Run 创建。
- Attempt owner lease 获取与状态更新。
- ActionAttempt RESERVED 与幂等键登记。
- ResourceLease 获取与 ActionAttempt 关联。
- Observation 持久化、ActionAttempt 终态和 Lease 释放。
- Attempt 终态、预算 Commit/Release 和 Outbox 事件。

外部模型和设备调用不能放在数据库事务中。采用短事务 + Lease + Outbox + 幂等回调。

### 10.5 数据保留

- Manifest、Decision、Verification 和审计事件长期保留。
- Prompt/Context 只保留脱敏引用和 hash；原内容服从用户数据保留策略。
- Model delta 可按产品设置保留，默认不永久保存包含用户内容的原始流。
- Secret 值永不进入 DSO 表。

---

## 11. Control Plane 与 Decision Runtime 协议草案

### 11.1 命令

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

### 11.2 事件

```text
DelegationProposed
DelegationAccepted / Rejected
SubagentAdmitted / Denied
SubagentRunStarted / Waiting / Completed / Failed / Cancelled
SubagentAttemptStarted / Superseded / TimedOut
DecisionTurnStarted / Completed
ActionProposed
ObservationAvailable
BudgetReserved / Committed / Released
ResourceLeaseAcquired / Released / Expired
VerificationCompleted
```

### 11.3 Envelope 最小字段

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

### 11.4 协议规则

- 命令至少一次投递，处理必须幂等。
- Run/Attempt 事件按实体 sequence 单调递增。
- 旧 revision、无效 owner lease 或 superseded Attempt 的写入必须拒绝。
- Runtime 断线不改变 Control Plane 的权威状态。
- Runtime 只能提交候选和执行进度，不能直接修改数据库状态。
- v0alpha 阶段优先使用内部 HTTP/gRPC 加 DB Outbox；不要先引入新的消息基础设施。

---

## 12. Delegation 与 Specialist 选择算法

### 12.1 默认 Fast Path

Delegation 默认结果是 `LOCAL`。满足以下任一条件时通常不委派：

- 任务只有一个低风险步骤。
- 预计协调成本大于质量或上下文收益。
- 无法形成独立 DelegatedOutcomeSpec。
- 无足够预算或时间完成外部验证。
- 需要连续控制同一浏览器资源且拆分会增加竞争。

### 12.2 DelegationProposal 触发信号

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

DSO-W2 使用规则 + LLM judgment；评分参数必须版本化并写入 Proposal。DSO-W7 才允许从 Experience 生成候选策略。

### 12.3 Specialist 解析顺序

```text
1. 已审核 SpecialistProfile 精确匹配
2. 已审核通用 Profile + 受限声明式 Overlay
3. Main Agent 本地执行
4. 请求用户澄清或报告能力不足
```

临时 Overlay 只能修改：

- role description
- 当前 DelegatedOutcome
- RequestedCapabilitySet
- RequestedContextScope
- 输出 Schema 参数

临时 Overlay 不能：

- 增加父 Task 未拥有的 Capability。
- 修改安全 System Prompt。
- 引入代码、脚本或新的 Provider。
- 自动保存为生产 SpecialistProfile。

### 12.4 Progressive Delegation

```text
Level 0: Main Agent Fast Path
Level 1: Single predefined Specialist
Level 2: Parallel predefined Specialists
Level 3: Bounded Specialist DAG
Level 4: Ad-hoc declarative Specialist Overlay
Level 5: Evaluated learned delegation policy
```

每一级必须通过上一等级的验收数据后才启用。

---

## 13. 安全、隐私与治理

### 13.1 Secret Boundary

Secret 值只允许存在于 Credential Store 和 Capability Executor 的短期执行内存中：

```text
Agent sees secret_handle_ref
    -> governed ActionAttempt
    -> Capability Executor resolves handle
    -> external request
    -> response redacted before Observation
```

必须扫描和拒绝以下位置中的 Secret：

- InvocationManifest
- Prompt 和 Context Slice
- Candidate Result
- Observation 与 Evidence
- WorldFact 与 ExperienceRecord
- Log、Trace 和前端事件

### 13.2 Prompt Injection

- 网页、文件和第三方 API 内容默认 `untrusted_external`。
- Context Builder 保留来源和 taint，不把外部文本拼入 System Instruction。
- 外部内容提出的动作不能自动提升风险等级或申请额外 Secret。
- Evidence Agent 只验证 Claim，不执行 Evidence 中的指令。

### 13.3 权限继承

```text
Capabilities(Subagent) subset of Capabilities(Task)
Permissions(Subagent) subset of Permissions(Parent)
RiskCeiling(Subagent) <= RiskCeiling(Parent)
Budget(Subagent) <= RemainingBudget(Parent)
```

Deny 优先于 Allow；所有临时 Capability View 带过期时间并绑定具体 Run。

### 13.4 用户控制

- 高风险动作继续要求审批。
- 用户可以查看委派原因、模型、预算、来源和动作。
- 用户可以取消 Task 或单个 Specialist。
- 用户可以禁止某个 Agent 使用动态 Specialist。
- 用户可以关闭 Experience retention 或删除相关历史。

---

## 14. 可观测性与前端体验

### 14.1 统一时间线

每个事件至少关联：

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

### 14.2 必须统计的指标

- Delegation proposal/accept/reject 数量。
- Fast Path 比例。
- 每个 Specialist 的成功、Partial、失败和外部验证率。
- Model Token、调用次数、首 Token 延迟和总延迟。
- Capability 调用开始、结束、耗时和错误链。
- Coordination overhead、重复搜索和重复页面比例。
- Budget reserve/commit/release 差额。
- Lease wait、冲突、过期和强制回收次数。
- Cancel propagation latency。
- Late result、reconcile 和 recovery 次数。
- Prompt injection、secret redaction 和 policy deny 次数。

### 14.3 前端展示

建议采用一个可折叠 Task Timeline：

```text
Main Task
  Isaac Research Specialist       completed / verified
  Gazebo Research Specialist      running / 42% budget
  AWSIM Research Specialist       waiting for source
  Evidence Specialist             blocked by dependencies
```

点击 Specialist 后显示：

- Delegation reasons。
- DelegatedOutcome 与验证要求。
- 已准入 Capability，不显示未授权系统能力。
- 当前模型和 Runtime Artifact 版本。
- Search、Fetch、Browser 和其他 Action 时间线。
- Evidence 链接、冲突和 VerificationResult。
- Cancel、Retry、Take Over 等允许的操作。

---

## 15. 分阶段实施计划

DSO 阶段不等于 Athena 产品版本。产品 Release 可以包含一个或多个 DSO Workstream，但不得改变 DSO 阶段顺序。

### 15.1 DSO-W0：语义契约与状态机

#### 解决的问题

- 将冻结架构转换成可编译、可验证的 `draft/v0alpha` 对象。
- 消除 Run、Attempt、Turn、ActionAttempt、Manifest、Lease 和 Budget 的语义混淆。
- 在写数据库和 RPC 前验证非法状态转换。

#### 进入条件

- 本文档评审通过。
- Athena v0alpha Effect 语义测试保持通过。
- 明确 Protocol v4 Action/Observation 不在本阶段修改。

#### 交付物

- `athena-protocol/draft/dso/v0alpha` 类型、常量和 Validation。
- 六套状态机：Proposal、Run、Attempt、ActionAttempt 扩展、Lease、BudgetReservation。
- JSON Schema 与 Go schema tests。
- DSO Core Invariants 自动验证测试。
- ADR：唯一委派权威、统一动作执行路径、Secret Boundary。
- 旧对象到新对象的迁移映射说明。

#### 测试

- 所有合法状态转换单元测试。
- 所有非法逆向状态转换拒绝测试。
- Hash 稳定性和 JSON round-trip。
- `PolicyDecision` 过期、world read set 变化测试。
- Secret fixture 无法通过 Manifest/Observation Validation。
- Property test：预算守恒和单活跃 Attempt。

#### 验收标准

- `go test ./...` 在 `athena-protocol` 通过。
- 所有冻结不变量至少有一个自动化测试。
- 任何对象都不能同时表达 Run 与 Attempt。
- `ActionProposal` Schema 中不存在可直接执行状态。
- InvocationManifest Validation 拒绝明文 Secret 字段和未知可执行代码。
- 状态机覆盖率达到 100% 的定义转换边。

#### 退出门槛

只有 Validation 与 transition tests 全部通过，才能进入 DSO-W1。字段仍保持 `v0alpha`。

### 15.2 DSO-W1：Control Plane 持久化委派权威

#### 解决的问题

- 把委派生命周期从 Runtime 内存迁移到数据库。
- 建立唯一逻辑 Delegation Orchestrator。
- 支持 Run/Attempt、预算、取消、心跳和进程重启恢复。

#### 进入条件

- DSO-W0 Exit Gate 通过。
- 数据库迁移测试环境可用。

#### 交付物

- DSO PO、Repository、Service 和 migration。
- Delegation Orchestrator durable loop。
- Optimistic revision、entity sequence 和 Outbox。
- Attempt owner lease 与 heartbeat。
- Budget Ledger Reserve/Commit/Release。
- Cancellation tree 和 startup recovery scanner。
- Compatibility Adapter，但不执行旧 Subagent。

#### 测试

- 多实例同时抢占同一 Run，只允许一个 owner。
- 在 CREATED、RUNNING、WAITING_OBSERVATION、CANCEL_REQUESTED 时 kill process 并恢复。
- Attempt #1 超时、Attempt #2 成功后，#1 迟到结果被隔离。
- 100 个并发预算预留不突破父预算。
- Cancel 后所有 Lease 和余额在限定时间内释放。
- Outbox 重复投递不会重复创建 Run 或扣减预算。

#### 验收标准

- Control Plane 重启后 30 秒内识别并处理所有 orphan Attempt。
- 同一 Run 在任意时刻最多一个有效 Attempt owner。
- 并发测试中预算超支为 0。
- 重复命令不会产生重复逻辑对象。
- Cancel 请求 5 秒内阻止新 DecisionTurn；30 秒内完成可取消调用清理。
- 每个状态变化都有 trace、causation 和审计事件。

#### 退出门槛

必须通过 PostgreSQL 和 MySQL 的事务/并发测试。旧 `spawn/delegate` 尚可存在，但只能走 Adapter，不能直接执行。

### 15.3 DSO-W2：单 Specialist 决策闭环

#### 解决的问题

- 接通 Control Plane 与 Decision Runtime。
- 让 Main Agent 自动判断 Fast Path 或提出单 Specialist 委派。
- 实现 Capability 和 Context 的 Requested/Admitted 分离。
- 完成无副作用 Research Specialist 闭环。

#### 进入条件

- DSO-W1 Exit Gate 通过。
- 至少一个审核后的 Research SpecialistProfile RuntimeArtifact。

#### 交付物

- LLM Supervisor DelegationProposal 输出 Schema。
- DelegationDecision 规则 + LLM judgment v1。
- Context Builder、classification、redaction、taint。
- Artifact Resolver 生成 Attempt InvocationManifest。
- Specialist Worker 的 DecisionTurn / ModelInvocation loop。
- TypedCandidateResult 和外部 VerificationResult。
- 旧请求级 SubAgentManager 停止生产调用。

#### Golden Path

用户请求：比较 Isaac Sim、Gazebo 和 AWSIM 的 ROS 2、传感器、机器人类型及开发成本。

本阶段只为其中一个独立研究 TaskStep 创建一个 Research Specialist，其余步骤先串行，以验证完整语义而非追求并行性能。

#### 测试

- 简单问题保持 Fast Path。
- 复杂研究任务创建一个 Proposal 和一个 Run。
- Requested Capability 中的未授权能力不会出现在 Admitted View。
- Context Slice 不包含其他用户数据、Secret 或不相关历史。
- 网页 Prompt Injection 被标记为 untrusted evidence。
- CandidateResult 自报 `success` 不会改变 Outcome 状态。
- VerificationResult 缺少证据时保持 unknown/unsatisfied。

#### 验收标准

- 100% SubagentAttempt 绑定有效 InvocationManifest。
- 100% CandidateResult 通过 Typed Schema Validation。
- Secret scanning fixture 泄漏数为 0。
- 简单任务 p95 额外延迟不超过 50ms，且不创建 Subagent。
- 单 Specialist Golden Path 连续运行 20 次，持久化链完整率 100%。
- 至少 95% Trace 能从 TaskStep 追踪到 ModelInvocation 和 VerificationResult。

#### 退出门槛

旧 Manager 生产入口删除；只有 Delegation Orchestrator 可以创建 Run。

### 15.4 DSO-W3：统一动作链与 Browser 垂直切片

#### 解决的问题

- 验证 Subagent 的动作不会形成简化执行路径。
- 实现 ActorBinding、Action-scoped ResourceLease 和关键前置条件二次检查。
- 让 Specialist 在 Observation 后继续同一个 Attempt 的下一 DecisionTurn。

#### 进入条件

- DSO-W2 Exit Gate 通过。
- Browser Runtime 能提供稳定 session/tab ID、resource version 和 Sanitized Observation。

#### 交付物

- ActionProposal -> PlanCandidate 确定性 Adapter。
- Subagent Admission Policy 与 Action Policy 分离。
- ResourceLease Service 和 ActionAttempt owner 绑定。
- Browser SessionAffinity 与 Tab single-writer。
- Observation 回传 Specialist DecisionTurn。
- 用户接管、页面漂移和取消处理。

#### 测试

##### 自动化 Golden Path

使用本地确定性 Browser Fixture：

1. 打开视频列表页。
2. 读取前三个视频标题和简介。
3. 按主题选择最匹配视频。
4. 点击对应卡片。
5. 观察播放状态。
6. 用 VerificationResult 验证目标视频正在播放。

##### 人工 Staging Path

在真实 YouTube 或同类网站执行相同任务，但不把外部站点稳定性作为 CI 成败依据。

##### 故障测试

- 用户在 Precheck 和 Lease 获取之间切换页面。
- Tab 被关闭或 resource version 改变。
- 两个 ActionAttempt 请求同一 Tab 写 Lease。
- 浏览器断线后恢复。
- 点击成功但 Observation 丢失，进入 unknown outcome + reconcile。
- 用户在模型推理、等待 Lease、执行动作时分别取消。

#### 验收标准

- 所有 Browser 动作均有 PlanCandidate 和 Action PolicyDecision。
- 不存在直接从 Subagent tool call 调用 Launcher 的代码路径。
- 同一 Tab 同时有效 writer 数永远不超过 1。
- Critical Recheck 能阻止 100% 测试中的过期动作。
- Cancel 后不再产生新 Browser action。
- Fixture Golden Path 连续 50 次成功率不低于 96%。
- 失败时必须返回具体状态和错误链，不允许空响应或虚假成功。

#### 退出门槛

Browser Fixture、真实 Staging 和故障注入报告完成，统一执行路径静态检查通过。

### 15.5 DSO-W4：并行 Specialist 与结果聚合

#### 解决的问题

- 在预算和并发上限内并行执行独立研究分支。
- 处理重复搜索、证据冲突、Partial Result 和依赖 DAG。
- 验证多 Specialist 是否真的优于单 Agent。

#### 进入条件

- DSO-W3 Exit Gate 通过。
- 单 Specialist 成本、延迟和准确性基线已记录。

#### 交付物

- Parallel Run scheduling 和 max_parallelism。
- Task dependency gate。
- Typed Result Aggregator。
- Claim 去重、Evidence correlation 和 Contradiction detection。
- Evidence Specialist 和 Synthesis Specialist。
- Frontend DAG 与实时进度。

#### Golden Path

```text
Isaac Specialist ----+
Gazebo Specialist ---+--> Evidence Specialist --> Result Aggregator
AWSIM Specialist ----+
```

#### 测试

- 三个分支并行，第四个等待依赖。
- 一个分支失败，系统选择 retry、replace、partial 或 ask user。
- 两个来源对同一 Claim 冲突。
- Provider rate limit 触发并发降级。
- 总预算不足时拒绝新分支，而不是超支。
- 父 Task 取消时并行分支全部停止。

#### 验收标准

- 并发数和总 Run 数从未超过 Policy 上限。
- 冲突 Claim 不会被静默合并为事实。
- 每个最终核心 Claim 至少绑定规定数量的 Evidence。
- 相比 Single Agent baseline，质量或证据覆盖有统计显著改善；若没有改善，默认关闭并行策略。
- Coordination token overhead 可测量并低于总 Token 的 25%。
- 重复 URL/页面抓取比例低于 15%。

#### 退出门槛

必须提交 Single Agent、Static Specialist、Dynamic DSO 三组对照报告。

### 15.6 DSO-W5：动态临时 Specialist

#### 解决的问题

- Registry 没有精确角色时，自动生成安全、声明式、临时 Specialist。
- 保持动态智能组合，同时不生成新权限或直接修改生产 Artifact。

#### 进入条件

- DSO-W4 Exit Gate 通过。
- Runtime Artifact Resolver 支持 SpecialistProfile 和声明式 Overlay。

#### 交付物

- Ad-hoc SubagentSpec Builder。
- Approved base profile + constrained overlay。
- Overlay Schema、hash、Admission 和审计。
- SpecialistProfileCandidate 经验记录入口。
- Frontend 展示“临时 Specialist”及其来源。

#### 测试

- 没有精确 Profile 时生成临时角色。
- Overlay 试图增加 terminal、payment 或 file.delete 时被拒绝。
- Overlay 包含脚本、Prompt override 或 Secret 时被拒绝。
- 临时 Specialist 结束后不成为生产 Profile。
- 多次成功只生成 Candidate，不自动激活。

#### 验收标准

- 动态 Overlay Capability 扩权成功数为 0。
- 所有 Overlay 都有 content hash、base profile ref 和 Admission Decision。
- 100% 临时 Specialist 在 Run 结束后不可被其他用户直接复用。
- 未审核 Candidate 的生产曝光率为 0。
- 动态 Specialist 在目标 benchmark 上优于 Main Agent fallback，否则保持关闭。

#### 退出门槛

安全评审、Prompt Injection 测试和跨租户隔离测试通过。

### 15.7 DSO-W6：恢复、Replay、安全与生产加固

#### 解决的问题

- 在崩溃、网络分区、Provider 故障、设备离线和 HA 竞争中保持正确状态。
- 建立可重放、可审计、可运营的生产系统。

#### 进入条件

- DSO-W5 Exit Gate 通过。
- 所有核心路径都有 InvocationManifest 和结构化事件。

#### 交付物

- Replay runner：exact-config、recorded-observation simulation、live re-execution 三种模式。
- Chaos 与 fault injection suite。
- HA owner lease、leader failover 和 split-brain 防护。
- SLO dashboard、告警和管理员诊断页面。
- 数据保留、导出和删除工具。
- 安全威胁模型与渗透测试报告。

#### 测试

##### 故障矩阵

- Control Plane 在事务提交前后崩溃。
- Runtime 在 ModelInvocation 流式中断。
- Device 在有副作用动作后、Observation 前断线。
- DB 短暂不可用和主从切换。
- Provider 429/500/timeout。
- Lease owner 失联和旧 owner 恢复。
- Secret-like 数据出现在网页、模型输出或 Executor response。
- Policy version 在 Run 中途改变。

#### 验收标准

- 不发生双执行已确认副作用动作。
- 迟到结果污染活跃 Run 次数为 0。
- HA failover 后在 30 秒内恢复调度。
- 取消传播 p95 小于 5 秒；不可立即取消的外部动作明确进入 reconcile。
- Manifest replay 可解释 100% Golden Path 使用的模型、Prompt、Context、Capability 和 Artifact 版本。
- 安全扫描中明文 Secret 持久化和日志泄漏数为 0。
- DSO Control Plane 可用性达到 99.9% 的预发布压测目标。

#### 退出门槛

生产 Readiness Review、恢复演练和回滚演练全部签字通过。

### 15.8 DSO-W7：受治理的 Delegation Learning

#### 解决的问题

- 从 Experience 学习何时委派、选择哪个 Profile、分配多少预算和上下文。
- 在不直接修改生产行为的前提下提升 DSO。

#### 进入条件

- DSO-W6 Exit Gate 通过。
- 已积累足够的成功、失败、Fast Path 和用户干预 Experience。

#### 交付物

- DelegationPolicyCandidate。
- SpecialistProfileCandidate。
- Offline Evaluation、Replay、Shadow、Canary 和 Rollback。
- Single Agent / Static Specialist / Dynamic DSO benchmark dashboard。
- Candidate 人工审核页面。

#### 测试

- 候选不能绕过 Review 直接激活。
- Shadow 不产生外部副作用。
- Canary 仅对允许用户和低风险任务生效。
- 指标恶化自动回滚。
- 用户关闭学习后不产生该用户 Candidate。

#### 验收标准

- 未经 Promotion 的 Candidate 生产曝光率为 0。
- Canary rollback 在阈值触发后 1 分钟内完成。
- 新策略必须在质量、成本、延迟或恢复率至少一个主指标改善，且安全指标不退化。
- 所有 Promotion 都能追溯到 Experience、Evaluation 和批准人。
- Learned Delegation 可以一键关闭并回退规则策略。

#### 退出门槛

只有通过完整治理流水线的 Artifact 才能进入默认 AgentBuild。

---

## 16. 跨阶段测试矩阵

| 测试类型 | W0 | W1 | W2 | W3 | W4 | W5 | W6 | W7 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Schema / Validation | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 |
| State Transition | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 |
| DB Concurrency |  | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 |
| Runtime Contract |  | 草案 | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 |
| Secret / Taint | 基础 | 基础 | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 |
| Browser Fixture |  |  |  | 必须 | 必须 | 必须 | 必须 | 回归 |
| Parallel / DAG |  |  |  |  | 必须 | 必须 | 必须 | 必须 |
| Crash Recovery |  | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 |
| Replay |  |  | 基础 | 基础 | 对照 | 对照 | 必须 | 必须 |
| Shadow / Canary |  |  |  |  |  |  | 基础 | 必须 |

所有阶段都必须继续运行 Athena 既有测试，不能以 DSO 功能通过为由破坏 Fast Path、Action/Observation、Browser、Goal、Learning 或 Deployment。

---

## 17. 第一批验收场景

### 17.1 Research Golden Task

目标：比较 Isaac Sim、Gazebo 和 AWSIM，覆盖 ROS 2、传感器、机器人类型、开发成本，并给出有证据推荐。

验证：

- Proposal 是否合理拆分。
- Context 是否隔离。
- Evidence 是否去重和冲突检测。
- Candidate 是否由外部 Verification 判定。
- 并行是否真的改善质量或上下文效率。

### 17.2 Browser Golden Task

目标：在同一 Browser Session 的视频列表中读取前三项，选择符合主题的视频并播放。

验证：

- Stable target 与 Tab affinity。
- ActionProposal 是否进入统一 Plan 主链。
- Tab single-writer 和 resource version。
- 用户切换页面后的 Critical Recheck。
- 播放状态 Observation 和 Verification。

### 17.3 Failure Golden Task

在上述任务中依次注入：

- 模型 timeout。
- Provider rate limit。
- Runtime 重启。
- Browser 断线。
- Tab 被用户关闭。
- Cancel。
- Budget exhausted。
- Capability offline。
- Late result。
- Prompt injection。
- Secret-like content。

验收重点不是“仍然成功”，而是系统必须安全停止、恢复、重新规划或明确请求用户，不能空响应、假成功、重复副作用或丢失错误链。

---

## 18. 关键风险与缓解

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| Supervisor 变成 God Object | 难恢复、难测试 | LLM 只提候选；Orchestrator、Context、Admission、Aggregator 拆分 |
| Agent 数量膨胀 | 成本和延迟失控 | Fast Path、max_depth=1、预算预留、ExpectedValue gate |
| Browser Tab 竞争 | 操作错误页面 | ActorBinding + action-scoped single-writer Lease + version recheck |
| Subagent 自报成功 | 虚假完成 | TypedCandidateResult + external VerificationResult |
| 两套执行路径 | 安全边界破裂 | 删除旧 Manager；静态检查所有 Action 必须有 Plan/Policy |
| Prompt Injection 传播 | 越权与数据泄漏 | trust class、taint、Context Builder、Evidence/Instruction 分区 |
| Secret 泄漏 | 严重安全事件 | Secret Handle、Executor 边界解析、全链路 redaction scanner |
| Budget race | 超额消费 | 原子 reservation ledger、optimistic revision、property tests |
| Late result | 覆盖新结果 | Attempt generation fencing、owner lease、审计隔离 |
| Replay 不真实 | 错误评测结论 | 区分 exact-config、recorded-observation 和 live replay |
| Dynamic Profile 直接生产 | 不受控自我修改 | Candidate、Review、Shadow、Canary、Promotion |

---

## 19. Definition of Done

DSO Workstream 完成必须同时满足：

1. Main Agent 可以自动决定 Fast Path 或提出受约束 DelegationProposal。
2. 没有预配置精确 Specialist 时，可以创建安全的临时声明式 Specialist。
3. 所有 Run、Attempt、Turn、Manifest、Budget 和 Lease 可追踪并可在重启后恢复。
4. Subagent 只获得父 Task 权限、能力、上下文和预算的子集。
5. 所有动作统一经过 PlanCandidate、PolicyDecision、PlanRun 和 ActionAttempt。
6. Browser 等共享资源不存在并发写冲突。
7. Subagent 不能自报 Outcome 成功，所有必要 Effect 均有 VerificationResult。
8. Secret 不进入 intelligence plane、数据库业务内容、日志或 Trace。
9. 用户可以看到委派原因、进度、证据、成本，并能取消或接管。
10. Dynamic Specialist 和 Learned Delegation 都经过 Artifact 治理流水线。
11. Single Agent、Static Specialist 和 Dynamic DSO 有可重复的对照评测。
12. 故障注入中不存在静默失败、虚假成功、双执行或错误链丢失。

---

## 20. 实施顺序与发布规则

严格顺序：

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

发布规则：

- 每个 Workstream 独立 commit，包含代码、测试、验收报告和未解决风险。
- 前一阶段 Exit Gate 未通过，不开始下一阶段生产实现。
- Schema 变更必须同步 `athena-protocol`、Control Plane、Runtime 和 Frontend 类型。
- v0alpha 阶段允许破坏性字段调整，但不得破坏冻结不变量。
- 任何跨仓库 Tag 必须先验证依赖版本和 RunManifest 解析。
- DSO 默认 feature flag 关闭；W3 后允许开发环境开启，W6 后才允许生产 Canary。

---

## 21. 下一步

立即进入 DSO-W0：

1. 在 `athena-protocol` 创建 `draft/dso/v0alpha`。
2. 实现六套状态机和 Validation。
3. 编写三个 ADR：唯一委派权威、统一执行路径、Secret Boundary。
4. 建立 Research/Browser Golden Fixture 的测试数据。
5. 输出 DSO-W0 Engineering Acceptance 文档。

在 DSO-W0 通过前，不创建生产数据库表，不修改冻结 Protocol v4，也不继续扩展旧 `spawn/delegate`。
