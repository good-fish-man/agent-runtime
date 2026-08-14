# Athena Runtime v0.9 生产级加固

## 范围

v0.9 将 Runtime 执行入口收敛为有界、可观测的服务。源码实现本身不代表已经通过外部渗透测试或平台公证；这些必须作为正式 Release 的独立证据。

## 准入与停机

进入 `/run`、`/agent` 和受控 gRPC 方法的执行请求统一经过 `operations.Gate`：

- `max_inflight` 限制同时运行的模型/工具请求。
- `max_queue` 限制等待请求；超出后拒绝，而不是无限堆积。
- `admission_wait_ms` 限制排队时间。
- `request_timeout_sec` 限制完整请求生命周期。
- 优雅停机先进入 draining，拒绝新任务并等待已接收的 HTTP/gRPC 请求，超时后才强制结束。

健康接口不进入执行队列：

- `GET /readyz` 返回准入健康状态。
- `GET /metrics` 返回 `athena.operations.v1` 健康与 SLO JSON。
- `GET /healthz` 保留原有依赖健康契约。

环境变量为 `ATHENA_OPERATIONS_MAX_INFLIGHT`、`ATHENA_OPERATIONS_MAX_QUEUE`、`ATHENA_OPERATIONS_ADMISSION_WAIT_MS` 和 `ATHENA_OPERATIONS_REQUEST_TIMEOUT_SEC`。

## SLO 解释

内置指标窗口从当前进程启动开始，记录请求、错误、拒绝、延迟、可用率、运行中数量和队列深度。正式环境应由外部监控持续采集；进程重启会主动重置内存窗口。

发布目标：Control API 99.9% 可用、设备状态 10 秒内收敛、Action Dispatch p95 小于 200ms（不含执行）、任务事件零丢失、不可逆副作用零重复、桌面会话 99.5% 以上无崩溃、升级成功率 99% 以上。

## 威胁模型

信任边界包括：登录用户 API、Launcher 本机服务 Token、设备 WebSocket、签名 Provider、模型输出、工具 Observation 和 PostgreSQL 持久化状态。

模型与工具输出始终是不可信输入。Typed Capability Schema、Policy/Risk、显式审批、Observation 校验、签名 Provider、预算和 Sandbox 都不能被提示词或网页内容绕过。网页不能为自己增加能力、扩大凭据范围、激活 Provider 或跳过审批。

密钥只能来自运维人员控制的环境变量或配置。健康/SLO 输出不得包含模型 Key、数据库密码、Device Token、Cookie、原始 DOM、截图或私有推理。

## 验证

```bash
GOCACHE=/private/tmp/athena-go-cache go test ./...
go test -race ./internal/operations ./internal/provider ./internal/server
```

公开发布前还必须在真实发布环境执行稳定性、数据库重启、断网、磁盘不足、多设备和各平台安装包测试。源码测试不能替代这些外部验收。
