# Athena Personal Agent OS 1.0 - Runtime 指南

[English](personal-agent-os-ga-v1.0.md) | [简体中文](personal-agent-os-ga-v1.0.zh-CN.md)

Agent Runtime 1.0 是 Athena 的决策与模型执行组件，不负责用户、凭据、安装包或桌面权限策略；这些职责属于 Runtime Client 与 Launcher。

## Runtime 架构

```text
HTTP/gRPC -> 准入控制 -> Dispatcher -> 意图/能力路由
          -> 模型/工具/研究循环 -> 类型化事件流
          -> Action -> Runtime Client 控制面 -> Observation
```

Runtime 提供有限队列、请求 Deadline、优雅排空、严格 Provider 校验和可选持久记忆。浏览器与桌面操作是抽象 Capability，模型不会直接调用操作系统 API。

## Readiness

`GET /readiness` 返回 `athena.ga.v1` 报告，检查冻结协议、类型化执行、脱离前端的后台运行、有界准入、签名 Provider 配置，以及启用记忆时的持久数据库。

HTTP `200` 表示 Runtime 自身不变量通过；HTTP `503` 表示至少一个必需条件失败。安装包签名、真实设备、安装器与长时间压测属于其他发布门禁。

## 运维接口

- 健康检查：`GET /healthz`
- 指标与 SLO：`GET /metrics`
- GA 就绪：`GET /readiness`
- 生成文件：`GET /generated/*`
- 仅本地管理接口：`/admin/*`

请求应始终携带 `X-Trace-Id`。模型、工具、能力和传输 Span 会记录开始、结束、耗时及带源码位置的错误链；禁止把密钥写入 Span 或 RunManifest。

## 安全与数据

- 只有配置签名校验和 Trust Store 后才能启用 Plugin。
- 实际授权必须是 Provider 请求权限与资源上限的子集。
- 记忆可以关闭；GA 环境启用时必须配置 PostgreSQL，并通过 Runtime Client 提供用户级保留与删除控制。
- 生成文件和本机管理接口只能放在可信本机或认证后的基础设施后面。
- Runtime 不向浏览器返回模型 API Key。

## 排障

1. 先查询 `/healthz`，再查询 `/readiness`。
2. 按 Trace ID 搜索日志，从错误链第一个源码位置开始定位。
3. `admission.control` 失败时检查队列饱和与排空状态。
4. `plugin.trust` 失败时检查 Trust Store、Digest、签名、授权和平台。
5. `memory.persistence` 失败时恢复数据库，或明确关闭记忆。

## 发布门禁

`go test ./...` 只证明代码级检查。真实模型供应商、签名 Provider、跨平台安装包、公证与持续压测仍需外部证据，不能标记为本地已通过。
