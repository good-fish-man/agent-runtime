# Athena Agent OS v0.8-v1.0 交付报告

## 路线对账

| 版本 | 工程范围 | 本地判定 | 不能由本地测试替代的证据 |
| --- | --- | --- | --- |
| v0.8 | Capability SDK、签名 Provider、Fail-closed Grant、Sandbox、Registry、调用溯源 | 由 `run-v0.8-engineering-gates.sh` 判定 | 公共 Registry、第三方密钥托管、生产 Sandbox 认证 |
| v0.9 | 供应链、加密备份、恢复、升级回滚、租约、背压、SLO | 由 `run-v0.9-engineering-gates.sh` 判定 | P0/P1 安全审查、三平台签名公证、DR、故障注入、24/72 小时观测 |
| v1.0 | Protocol Freeze、十条 Golden Journey、可信 E2E 入口、连续 Provenance、GA Readiness | 由 `run-v1.0-engineering-gates.sh` 判定 | 完整真实 E2E、签名发布、Security/Privacy Review、恢复演练、SLO Release Window |

## 本轮关键修复

- 修正示例 Provider 把 Package Schema 写入 Trust Store 的错误，统一为 `athena.plugin-trust.v1`，并增加真实构建测试。
- Launcher Readiness 不再按文件存在性统计恢复点，改为认证 HMAC、身份、状态和制品完整性。
- Runtime Client 将成功验证状态重新签名并持久化，备份清单同时校验制品 SHA/大小；未验证备份不能关闭恢复门禁。
- E2E Evidence 从公共管理员 API 移到机器内部令牌边界，并要求明确 Owner。
- Provenance 必须由一个实际步骤同时证明 Build、Manifest、Capability、Action 和 Observation，禁止跨步骤拼接。

## 聚合回归

```bash
cd /Users/dom/agent-ui/agent-runtime
./scripts/run-agent-os-v0.8-v1.0-engineering-gates.sh
```

预期本地终态：

```text
v0.8-v1.0 ENGINEERING_VERIFIED
RELEASE_STATUS=EXTERNAL_REQUIRED
```

本轮于 2026-08-18 实际执行聚合门禁，结果为：

```text
[v0.8] ENGINEERING_VERIFIED
[v0.9] ENGINEERING_VERIFIED
[v1.0] ENGINEERING_VERIFIED
[agent-os] v0.8-v1.0 ENGINEERING_VERIFIED
[agent-os] RELEASE_STATUS=EXTERNAL_REQUIRED
```

门禁覆盖 Protocol `make check`、五个仓库的静态检查与构建、Go 全量测试、关键并发路径 Race 测试、Frontend TypeScript 检查和生产构建。当前仅保留 Vite 大分块告警，不影响门禁结论，但应在后续性能预算中拆分。

## 发布前 Stop Rule

任何外部门禁缺少可审计证据时，不得创建“GA Ready”结论。代码合并、数据库表存在、模拟数据、Preflight、单次 Demo 或开发签名均不能替代真实发布证据。独立仓库 CI 在不使用本地 `go.work` 时，还必须先发布并引用同一协调版本的 `athena-protocol` 模块。
