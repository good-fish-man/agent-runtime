# Personal AI Operating System Specification v1.0

[English](#personal-ai-operating-system-specification-v10) | [简体中文](#个人-ai-操作系统规格-v10)

## Chapter 1 Vision

Athena is a Personal AI Operating System: a user-owned agent layer that can understand goals, plan work, use models, operate approved devices, remember preferences, and continue tasks across conversations.

The system is not a single chatbot. It is a coordinated runtime made of an Agent Core, a Runtime Client, a Desktop Runtime, a Perception Layer, action runtimes, and a user-facing UI. The LLM reasons and plans; the client runtime executes typed capabilities; every real-world action is verified by observation before the agent continues.

### Product principles

- The user owns identity, models, keys, memories, devices, browser sessions, and local data.
- The LLM never directly controls the operating system, browser, terminal, files, or credentials.
- All device operations use typed Actions and verified Observations.
- Long-running tasks survive frontend navigation and browser tab closure.
- Risky operations require explicit approval, purpose-specific capabilities, and audit logs.
- Automation must pause for login, CAPTCHA, QR, 2FA, payment, booking, destructive changes, and other sensitive checkpoints.

### Target experience

The user should be able to say:

```text
Open YouTube, search for AI Agent tutorials, open the best first result, and tell me the video title.
```

Athena should:

- Understand the goal and target environment.
- Open or reuse the correct browser session.
- Observe the page.
- Search through semantic browser actions.
- Open the selected result.
- Return the observed title.
- Keep the same session available for follow-up commands.

## Chapter 2 Overall Architecture

```text
User
  |
  v
Frontend / Desktop Shell
  |
  v
Agent Runtime
  |
  +--> Intent Layer
  +--> Planner
  +--> Action Generator
  +--> Evaluator
  |
  v
Runtime Client
  |
  +--> Auth
  +--> User Models / Keys
  +--> Device Registry
  +--> Task Sessions
  +--> Action/Observation WebSocket
  |
  v
Desktop Runtime
  |
  +--> Action Layer
  |     +--> Browser Runtime
  |     +--> OS Runtime
  |     +--> File Runtime
  |     +--> Terminal Runtime
  |
  +--> Perception Layer
        +--> Browser Observation Engine
        +--> Desktop Observation Engine
        +--> File Observation Engine
        +--> Terminal Observation Engine
        +--> Vision Observation Engine
        +--> Audio Observation Engine
  |
  v
Observation
  |
  v
Agent Runtime
```

### Service boundaries

| Project | Owns | Must not own |
| --- | --- | --- |
| `agent-runtime` | Intent, planning, model invocation, capability selection, action generation, observation evaluation | Local OS execution, browser profiles, user credentials |
| `agent-runtime-client` | Users, models, keys, agents, memory, chat history, device registry, task sessions, Action/Observation routing | Host command execution, browser process control |
| `athena-launcher` | Desktop shell, local service lifecycle, device connection, Action Layer adapters, Perception Layer engines, local permissions | Business planning, model reasoning |
| `agent-ui` | Conversation UI, settings, approvals, task progress, model/agent management | Executing model text as commands |
| `agent-browser` | Browser command adapter and semantic DOM primitives | Reasoning, account vault ownership, final Observation ownership |

### Execution modes

| Mode | Purpose | Example |
| --- | --- | --- |
| Command Mode | Directly execute a short user instruction | Open YouTube |
| Goal Mode | Plan and execute a multi-step task | Plan a five-day Hokkaido trip |
| Research Mode | Search, fetch, compare, cite, and synthesize | Summarize today's AI news |
| Assisted Automation Mode | Continue until blocked by approval or human verification | Log in and reserve a ticket |
| Background Mode | Scheduled or long-running task | Check prices every morning |

## Chapter 3 Agent Runtime

Agent Runtime is the reasoning engine. It transforms natural language into structured intent, plans, actions, and final answers.

### Core modules

| Module | Responsibility |
| --- | --- |
| Intent Analyzer | Extract goal, constraints, target environment, expected result, urgency, and ambiguity |
| Planner | Create, revise, and bound execution plans |
| Capability Selector | Select relevant capabilities instead of dumping all tools into every prompt |
| Action Generator | Produce typed `athena.agent.v2` Actions |
| Stream Tool Executor | Consume streamed tool-call chunks, execute capabilities, feed Observations back to the model, and continue to final output |
| Evaluator | Compare Observation with the step postcondition |
| Memory Reviewer | Extract durable preferences and facts after a conversation |
| Safety Policy | Assign risk, approval, and block decisions |

### Capability model

Capabilities are stable product-level abilities, not raw implementation tools.

```yaml
capabilities:
  - id: internet.search
    description: Search public web pages
    input:
      query: string
    output:
      results: SearchResult[]

  - id: browser.open
    description: Open or reuse a visible browser session
    input:
      url: string
      session_id: string?
    output:
      observation: BrowserObservation

  - id: app.open
    description: Open a local application
    input:
      app: string
    output:
      observation: DesktopObservation
```

The runtime should expose only relevant capabilities for the current task. For example, a weather question needs `weather.current`, `internet.search`, and `internet.fetch`; it does not need `file.write`, `terminal.execute`, or `browser.download`.

### Observation-driven loop

```text
intent -> plan -> action -> device execution -> observation -> evaluate -> continue or finish
```

Rules:

- The agent may not infer that an action succeeded just because it emitted an Action.
- Every action must have a postcondition.
- If the Observation is `WAITING_USER`, the agent must ask the user to complete the visible step or choose another path.
- If a browser action lands on a CAPTCHA, unusual-traffic page, login page, QR code, or 2FA prompt, the next response must not claim success.
- If a tool returns partial failure and useful content, the final answer must separate what succeeded from what failed.

## Chapter 4 Planner Protocol

The Planner Protocol describes how the agent turns an ambiguous request into an executable, inspectable task.

### Intent object

```json
{
  "goal": "Find machine learning tutorial videos",
  "environment": "browser",
  "expected_result": "Open a suitable YouTube video and return its title",
  "constraints": {
    "language": "same_as_user",
    "budget": "low",
    "risk": "low"
  },
  "missing_information": []
}
```

### Plan object

```json
{
  "goal": "Open a suitable YouTube tutorial",
  "mode": "command",
  "steps": [
    {
      "id": "step-1",
      "task": "Open YouTube in the active browser session",
      "capability": "browser.open",
      "postcondition": "Observed page is YouTube or a recoverable verification page"
    },
    {
      "id": "step-2",
      "task": "Search for AI Agent tutorials",
      "capability": "browser.type",
      "postcondition": "Search results are visible"
    }
  ]
}
```

### Planner rules

- Ask clarifying questions only when the missing information changes the plan materially.
- Use defaults from user profile, frontend language, locale, current time, and device context when safe.
- Do not ask for “today” or “current” when current date/time is already available.
- For research tasks, form search queries automatically before asking the user for URLs.
- For travel, shopping, tickets, appointments, or purchases, collect constraints before high-risk actions.
- For browser tasks, preserve the active session unless the user asks for a new window or profile.

### Task state

| State | Meaning |
| --- | --- |
| `UNDERSTANDING` | Parse request and context |
| `PLANNING` | Create or update plan |
| `WAITING_ACTION` | Ready to emit an Action |
| `EXECUTING` | Device is executing |
| `OBSERVING` | Device is collecting state |
| `EVALUATING` | Agent compares state with postcondition |
| `WAITING_USER` | User must provide input, approval, login, CAPTCHA, QR, or 2FA |
| `COMPLETED` | Goal satisfied |
| `FAILED` | Task cannot continue safely |
| `CANCELLED` | User or system stopped the task |

## Chapter 5 Desktop Runtime

The Desktop Runtime is the local device host. It is usually provided by `athena-launcher`.

### Responsibilities

- Connect to Runtime Client through outbound WebSocket.
- Register device identity and user binding.
- Advertise capabilities.
- Execute only typed, policy-approved Actions.
- Run the Action Layer for browser, app, file, terminal, and input execution.
- Run the Perception Layer for verified Observations.
- Keep local logs, screenshots, and execution diagnostics.
- Manage service lifecycle for local deployments.
- Support remote Runtime Client mode without installing local backend services.

### Local modules

| Module | Responsibility |
| --- | --- |
| Device Manager | Device ID, pairing token, user binding, online status |
| Capability Manager | Capability discovery and version reporting |
| Service Manager | Start, stop, update, and health-check local services |
| Permission Manager | Microphone, files, browser, app control, screen capture |
| Action Layer | Execute typed Actions through browser, desktop, file, terminal, keyboard, pointer, and audio adapters |
| Perception Layer | Collect browser, desktop, file, terminal, vision, and audio Observations |
| Log Manager | User-readable startup and runtime logs |

### Desktop action policy

| Action family | Default risk | Default decision |
| --- | --- | --- |
| Observe state | `LOW` | `ALLOW` |
| Open requested app or URL | `MEDIUM` | `ALLOW` |
| Type text into active app | `MEDIUM` | `ASK_USER` unless scoped to search/input field |
| Submit forms, send messages, book, buy, or change account state | `HIGH` | `ASK_USER` |
| Terminal, file write, credential use, destructive operation | `HIGH` | Purpose-specific approval required |

## Chapter 6 Browser Runtime

Browser Runtime is the safest and most important desktop execution environment. It gives Athena a controlled browser action surface instead of raw mouse clicks.

### Browser Runtime boundary

- It executes browser actions: open, navigate, click, type, press, scroll, wait, download, screenshot, and close.
- It owns browser process management and safe command execution.
- It does not reason about user goals.
- It does not store account passwords directly.
- It does not decide whether a task succeeded.
- It does not own the final Observation contract.

### Perception Layer boundary

The Perception Layer is the unified observation system. Browser state is observed by Browser Observation Engine; desktop, files, terminal, screenshots, and audio each have their own observation engine.

```text
Perception Layer
  |-- Browser Observation Engine
  |-- Desktop Observation Engine
  |-- File Observation Engine
  |-- Terminal Observation Engine
  |-- Vision Observation Engine
  `-- Audio Observation Engine
```

Perception rules:

- It answers "what does the world look like now?"
- It does not execute actions.
- It does not call the LLM.
- It reports page state, not hidden secrets.
- It must detect bot checks, CAPTCHA, login, QR, and 2FA as `WAITING_USER`.
- It provides the Observation used by Agent Runtime evaluation.

### Browser Observation Providers

The following browser providers belong to Browser Observation Engine. They observe browser state after Browser Runtime executes an action.

### Profile Provider

Purpose: isolate or reuse browser identity safely.

Responsibilities:

- Support isolated Athena profiles for automation.
- Support user-selected persistent profiles for login continuity.
- Prevent accidental use of the user's default browser profile unless explicitly configured.
- Record profile metadata without storing browser secrets in the database.
- Allow profile reset, cleanup, and migration.

Key states:

| State | Meaning |
| --- | --- |
| `isolated` | Athena-owned clean profile |
| `profile` | User-selected persistent profile |
| `auto_connect` | Attach to an existing debuggable browser session |

### Workspace Provider

Purpose: group browser sessions by user task.

Responsibilities:

- Create one browser workspace per task or conversation when needed.
- Reuse the active workspace for follow-up commands.
- Keep independent tasks from fighting over the same page.
- Store workspace metadata in Task Session context.
- Support user-visible workspace naming.

### Window Provider

Purpose: prevent browser chaos.

Responsibilities:

- Reuse existing windows for the same workspace.
- Avoid opening duplicate browser instances for the same target.
- Track active window, focused window, and window bounds.
- Restore a minimized or hidden window before interaction.
- Never close a user-visible session unless the user requested it or policy allows it.

### Tab Provider

Purpose: make browser state explicit.

Responsibilities:

- Track tab IDs, titles, URLs, and ownership.
- Route follow-up commands to the correct tab.
- Open a new tab only when the plan requires it.
- Prevent unrelated goals from racing over the same tab.
- Provide tab list observations to the agent.

### Navigation Provider

Purpose: make page transitions reliable.

Responsibilities:

- Validate URLs before navigation.
- Prefer direct URLs over search-engine navigation when the target is known.
- Use search only when the target is ambiguous.
- Wait for meaningful page load states.
- Detect redirects, blocked pages, and failed navigations.
- Return `WAITING_USER` for login, CAPTCHA, unusual traffic, QR, and 2FA.

### DOM Provider

Purpose: give the agent semantic page understanding.

Responsibilities:

- Return URL, title, visible text, accessibility snapshot, and key elements.
- Normalize clickable and fillable elements into stable refs such as `@e12`.
- Prefer labels, roles, names, and nearby text over coordinates.
- Truncate large pages safely.
- Redact passwords, tokens, cookies, and sensitive input values.
- Identify challenge pages and mark them as not satisfying the original postcondition.

### Download Provider

Purpose: handle files safely.

Responsibilities:

- Require approval before download unless the site and file type are low risk.
- Track download start, progress, completion, failure, and file path.
- Store downloads in an Athena-controlled directory by default.
- Scan file metadata and expose it in Observation.
- Require explicit user approval before opening downloaded executables or scripts.

### Cookie Provider

Purpose: maintain sessions without exposing secrets.

Responsibilities:

- Keep cookies inside the selected browser profile.
- Never send raw cookies to Agent Runtime or Runtime Client.
- Report authentication state only as high-level observations.
- Allow user-controlled cookie/profile clearing.
- Support domain-scoped session diagnostics.

### Session Provider

Purpose: preserve continuity across turns.

Responsibilities:

- Use stable session IDs for repeated targets.
- Store active browser session in Task Session context.
- Resume the same session after frontend navigation or app restart when possible.
- Deduplicate repeated actions using idempotency keys.
- Support explicit `browser.close` and workspace cleanup.

### Browser observation schema

```json
{
  "url": "https://www.youtube.com/results?search_query=ai+agent",
  "title": "AI agent - YouTube",
  "page": {
    "url": "https://www.youtube.com/results?search_query=ai+agent",
    "title": "AI agent - YouTube"
  },
  "snapshot": "- textbox \"Search\" @e1\n- link \"AI Agent Tutorial\" @e2",
  "key_elements": [
    {"ref": "@e1", "label": "textbox \"Search\""},
    {"ref": "@e2", "label": "link \"AI Agent Tutorial\""}
  ],
  "challenge_detected": false
}
```

Challenge example:

```json
{
  "url": "https://www.google.com/sorry/index",
  "title": "Sorry",
  "challenge_detected": true,
  "challenge": {
    "kind": "google_unusual_traffic",
    "message": "Google blocked the automated browser with an unusual-traffic verification page.",
    "requires_user_takeover": true
  }
}
```

## Milestones

| Phase | Goal | Acceptance |
| --- | --- | --- |
| Phase 0 | Freeze architecture boundaries and protocol | New features use capabilities and Action/Observation only |
| Phase 1 | Capability registry and planner grounding | Runtime exposes relevant capabilities per task |
| Phase 2 | Device WebSocket control plane | Launcher stays connected and actions continue without frontend |
| Phase 3 | Browser closed loop | YouTube search flow works in one browser session |
| Phase 4 | Desktop/file/app closed loop | Agent can open apps and inspect authorized local files |
| Phase 5 | Human approval and credential vault | Login and high-risk actions pause safely |
| Phase 6 | Background automation | Scheduled tasks survive UI closure and report progress |
| Phase 7 | Distribution and updates | One-click install, update prompts, logs, and recovery |

## Versioning

This document is a product architecture specification. Wire-level compatibility is governed by `athena.agent.v2` in the Action/Observation protocol. If a future version changes ownership boundaries, transport semantics, risk policy, session rules, or capability envelopes, it must be documented as a new major version.

# 个人 AI 操作系统规格 v1.0

## 第 1 章 愿景

Athena 是一个个人 AI 操作系统：它不是单纯聊天机器人，而是用户拥有的 Agent 层。它可以理解目标、规划任务、使用模型、操作授权设备、记住用户偏好，并在多轮对话里持续完成任务。

核心原则是：大模型负责理解、规划、决策和推理；客户端负责执行、观察和反馈。任何真实世界动作都必须通过结构化 Action 执行，并通过 Observation 验证。

## 第 2 章 总体架构

系统由六个核心部分组成：

- `agent-runtime`：负责意图理解、规划、模型调用、能力选择、动作生成和观察评估。
- `agent-runtime-client`：负责用户、模型、Key、Agent、记忆、聊天历史、设备注册、任务会话和 Action/Observation 路由。
- `athena-launcher`：负责桌面外壳、本地服务生命周期、设备连接、Action Layer 适配器、Perception Layer 感知引擎和本地权限。
- `agent-ui`：负责对话、配置、审批、进度展示和任务状态。
- `agent-browser`：负责浏览器命令适配和语义 DOM 基础能力，不拥有最终 Observation 合同。
- `Perception Layer`：统一负责“世界现在是什么样”，包含 Browser、Desktop、File、Terminal、Vision、Audio Observation Engine。

系统边界必须保持清晰：UI 不执行模型文本，Runtime Client 不执行本机命令，Agent Runtime 不直接控制浏览器或系统。

## 第 3 章 Agent Runtime

Agent Runtime 是推理核心。它需要具备：

- 用户意图分析能力。
- 多步骤规划能力。
- 相关能力筛选能力。
- Action 生成能力。
- 流式工具执行链路。
- Observation 评估能力。
- 用户记忆提取能力。
- 风险策略判断能力。

Agent Runtime 不应该一次性把所有工具和技能塞给模型，而应该按任务选择相关 capability。比如天气问题只需要天气、搜索和网页获取能力，不需要文件写入、终端执行或浏览器下载能力。

## 第 4 章 Planner Protocol

Planner Protocol 定义从模糊自然语言到可执行任务的过程。

例如用户说：

```text
下个月我计划去北海道旅行 5 天。
```

Agent 不应该直接给模板答案，而应该理解目标并主动规划：

- 查询日期范围和天气。
- 询问是否自驾。
- 询问机票和酒店预算是否敏感。
- 询问偏好自然风景、美食、温泉、滑雪还是城市路线。
- 查询交通和景点。
- 生成可执行行程。

缺少信息时，只有在影响计划质量或风险时才追问。当前日期、用户语言、设备状态、位置授权和历史偏好可以作为安全默认值使用。

## 第 5 章 Desktop Runtime

Desktop Runtime 是本地设备宿主，主要由 `athena-launcher` 提供。

它负责：

- 通过出站 WebSocket 连接 Runtime Client。
- 注册设备和用户绑定。
- 上报本机能力。
- 通过 Action Layer 执行被策略允许的 Action。
- 通过 Perception Layer 返回真实 Observation。
- 管理本地服务启动、停止、更新和健康检查。
- 管理麦克风、文件、浏览器、应用控制和屏幕捕获权限。

Desktop Runtime 不能做业务规划，也不能私自执行模型文本里的命令。

## 第 6 章 Browser Runtime

Browser Runtime 是最重要、也最稳定的桌面执行环境。Agent 不应该用鼠标坐标盲点，而应该通过可控浏览器 Action 执行任务。

Browser Runtime 只负责执行：打开、导航、点击、输入、按键、滚动、等待、下载、截图和关闭。它不负责推理，不判断任务是否成功，也不拥有最终 Observation 合同。

### Perception Layer

Perception Layer 统一负责感知：

```text
Perception Layer
  |-- Browser Observation Engine
  |-- Desktop Observation Engine
  |-- File Observation Engine
  |-- Terminal Observation Engine
  |-- Vision Observation Engine
  `-- Audio Observation Engine
```

它回答“世界现在是什么样”，不执行动作，不调用大模型，不返回隐藏密钥。浏览器状态由 Browser Observation Engine 观察，最终 Observation 由感知层生成。

### Browser Observation Providers

下面这些模块属于 Browser Observation Engine 的感知 provider。它们在 Browser Runtime 执行动作之后读取真实状态。

### Profile Provider

负责隔离或复用浏览器身份。默认应使用 Athena 独立 Profile；只有用户明确选择时，才使用持久 Profile。不得偷偷读取用户默认浏览器 Profile。

### Workspace Provider

负责按任务组织浏览器会话。不同任务不应该抢同一个页面；同一任务的后续命令应该复用原来的 workspace。

### Window Provider

负责窗口复用和焦点管理。它要避免同一个目标反复打开多个浏览器，也不能莫名其妙关闭用户还在看的窗口。

### Tab Provider

负责标签页状态。每个标签页都应该有 URL、标题、归属任务和当前状态，后续命令必须路由到正确标签。

### Navigation Provider

负责导航可靠性。已知目标优先使用直接 URL，未知目标才搜索。导航后必须检测是否真的到达目标页面，遇到登录、验证码、扫码、2FA、反爬页面时返回 `WAITING_USER`。

### DOM Provider

负责把页面转换成 Agent 能理解的语义状态，包括 URL、标题、可见文本、可访问性快照和关键元素。它必须隐藏密码、Token、Cookie 和敏感输入内容。

### Download Provider

负责下载文件安全。下载应该有进度、文件路径、完成状态和失败原因。打开可执行文件或脚本必须要求用户确认。

### Cookie Provider

负责维持登录状态，但不能把 Cookie 原文传给 Agent Runtime 或 Runtime Client。它只能报告高层认证状态。

### Session Provider

负责跨回合连续操作。同一个任务应该复用同一个 browser session，用户关闭前端后也不应该丢失任务状态。

## 阶段规划

| 阶段 | 目标 | 验收 |
| --- | --- | --- |
| 阶段 0 | 冻结架构边界和协议 | 新功能只能通过 capability 和 Action/Observation 实现 |
| 阶段 1 | 能力注册和 Planner 接地 | Runtime 按任务暴露相关能力 |
| 阶段 2 | 设备 WebSocket 控制面 | 前端关闭后桌面动作仍能执行和反馈 |
| 阶段 3 | 浏览器完整闭环 | YouTube 搜索流程在同一个 browser session 内完成 |
| 阶段 4 | 桌面、文件、应用闭环 | Agent 能打开应用并读取授权本地文件 |
| 阶段 5 | 人工审批和凭据保险箱 | 登录和高风险动作安全暂停 |
| 阶段 6 | 后台自动化 | 定时任务脱离 UI 持续运行并反馈 |
| 阶段 7 | 分发和更新 | 一键安装、更新提示、日志和自动恢复可用 |

## 版本规则

本文是产品架构规格。线级兼容性以 `athena.agent.v2` Action/Observation 协议为准。未来如果改变职责边界、传输语义、风险策略、会话规则或 capability envelope，必须作为新的大版本设计，不能用临时 JSON、模型文本解析或前端转发链路绕过去。
