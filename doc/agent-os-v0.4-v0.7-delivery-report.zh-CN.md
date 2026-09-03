# Athena Agent OS v0.4-v0.7 工程交付报告

总状态：**ENGINEERING_VERIFIED**

本报告汇总 v0.4-v0.7 的工程交付与自动验收结果。该状态不等于 Release/GA；v0.3 外部证据门禁仍然独立存在。

| 版本 | 核心交付 | 工程状态 | 独立验收 |
| --- | --- | --- | --- |
| v0.4 | 声明式 Skill/Strategy Candidate、演示学习、离线评估、人工 Review | ENGINEERING_VERIFIED | `run-v0.4-engineering-gates.sh` |
| v0.5 | AgentBuild、RunManifest、Shadow、低风险 Canary、Promotion 与一键 Rollback | ENGINEERING_VERIFIED | `run-v0.5-engineering-gates.sh` |
| v0.6 | Evidence Knowledge、冲突/时效治理、Snapshot、受控 Ontology | ENGINEERING_VERIFIED | `run-v0.6-engineering-gates.sh` |
| v0.7 | Persistent Goal、有限多 Agent、统一 Scheduler、Checkpoint 与跨设备恢复 | ENGINEERING_VERIFIED | `run-v0.7-engineering-gates.sh` |

## 贯穿版本的治理链

```text
Sanitized Experience
  -> Learning Candidate
  -> Offline Evaluation
  -> Human Review
  -> Immutable Artifact Version
  -> AgentBuild + RunManifest
  -> Shadow / Low-risk Canary
  -> Promotion or Rollback
  -> Evidence-grounded Knowledge
  -> Persistent Goal + Bounded Supervisor
  -> Observation-backed Verification
```

这条链保持以下边界：学习不能直接生成代码执行；Candidate 不能自动进入生产；Observation 不是未经治理的真相；外部效果没有真实 Observation 不能算成功；长期 Goal 不能绕过 Policy、预算和设备授权。

## 总门禁

在工作区执行：

    ./agent-runtime/scripts/run-agent-os-v0.4-v0.7-engineering-gates.sh

总门禁顺序执行四个版本的协议、数据库、服务端、Runtime、Launcher、前端、Race Detector、静态分析和生产构建检查。任一版本失败即停止，不输出总 `ENGINEERING_VERIFIED`。

## 数据与迁移

- 新表由 Runtime Client 启动迁移统一初始化，迁移可重复执行。
- v0.4 与 v0.6 使用可重复的前向迁移；v0.5 的产品回滚切换 Build 指针并保留补偿审计；v0.7 另外提供显式破坏性 SQL 回滚资产。破坏性回滚不会在启动时自动执行。
- Owner、Organization、Visibility、Provenance、Revision、Checksum 与审核记录由服务端约束，不能只依赖前端字段。

## 尚未关闭的外部门禁

- 三平台签名安装器与真实升级/回滚演练。
- 完整 500 Journey Soak，而不是确定性缩短夹具。
- 完整十 Span Trace 与生产近似终态 Experience 覆盖率。
- 真实多日 Goal、断网/断电、跨物理设备恢复和生产通知验证。
- v0.9 规划中的 HA、压力测试、SLO、安全审计、备份恢复和供应链签名。
- 当前聚合门禁运行在多仓库 `go.work` 集成工作区。拆分仓库独立 CI 前，必须先发布包含 `athena.learning.v2` 与 `athena.orchestration.v2` 的 `athena-protocol v0.2.0`，再更新 Runtime/Client 的模块校验和并以 `GOWORK=off` 复跑。

因此，当前可以准确表述为“v0.4-v0.7 工程实现与自动门禁通过”，不能表述为“v0.7 已生产 GA”。
