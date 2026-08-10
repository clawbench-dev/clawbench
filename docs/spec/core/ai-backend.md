# AI 后端抽象

ClawBench 支持多种 AI 工具，每种工具的调用方式、输出格式各不相同。AI 后端抽象层将这种差异封装为统一的 `AIBackend` 接口——handler 只需调用 `ExecuteStream()`，不关心背后是 Claude 还是 Kimi。系统支持两种传输模式：CLI shell-out（传统模式，通过 stdout 流式解析）和 ACP stdio（Agent Client Protocol，通过 JSON-RPC 双向通信，提供模式切换、斜杠命令和权限管理等结构化能力）。13 个后端在 `BackendRegistry` 中声明规格（CLI 命令、模型发现策略、ACP 命令），factory 根据后端类型创建对应的 `AIBackend` 实例。传输选择在 factory 层根据 Agent 的 `Transport` 字段决定，调用方完全透明。

## 流程图

### 后端选择与传输分流

```mermaid
flowchart TD
    A[POST /api/ai/chat] --> B{解析 Agent Transport}
    B -->|acp-stdio| C{SupportsACP?}
    C -->|是| D[ACPBackend]
    C -->|否| E[降级 CLI + 警告]
    B -->|cli| F[BackendRegistry 规格匹配]

    D --> G[ACP JSON-RPC over stdio]
    E --> F
    F --> J[直接使用 CLIBackend]
    J --> K[构造 CLI 命令 → 子进程 → LineParser]
    G --> L[输出 StreamEvent channel]
    K --> L
```

### ACP 连接与执行流程

```mermaid
sequenceDiagram
    participant SessionExecutor
    participant ACPBackend
    participant ACPConnManager
    participant Agent进程

    SessionExecutor->>ACPBackend: ExecuteStream(ChatRequest)
    ACPBackend->>ACPConnManager: GetOrCreateConn(sessionID)
    ACPConnManager->>Agent进程: 启动 ACP 子进程
    Agent进程-->>ACPConnManager: Initialize 握手
    ACPConnManager-->>ACPBackend: 返回 ACPConn
    ACPBackend->>Agent进程: NewSession / ResumeSession
    ACPBackend->>Agent进程: Prompt(prompt)
    loop 流式事件
        Agent进程-->>ACPBackend: AgentMessageChunk / ToolCall / Plan 等
        ACPBackend-->>SessionExecutor: StreamEvent(content/tool_use/plan_update...)
    end
    Note over Agent进程: 进程意外退出
    ACPBackend->>Agent进程: 自动重生 + 重试 Prompt（跳过导致崩溃的配置）
```

### LoadSession 异步回放流程

```mermaid
sequenceDiagram
    participant 前端
    participant handler
    participant ACPConn
    participant Agent进程
    participant WS

    前端->>handler: POST /api/ai/session/acp-load
    handler->>ACPConn: LoadSession(sessionID)
    Agent进程-->>ACPConn: SessionUpdate 通知（缓冲）
    handler-->>前端: {sessionId, replayPending: true}
    Note over 前端: 前端可立即发消息
    handler->>handler: 异步 goroutine 处理回放
    handler->>handler: 持久化消息到 DB
    handler->>WS: replay_done 事件
    WS-->>前端: replay_done
    Note over 前端: 回放完成，可显示历史
```



## 功能与设计要点

### 功能清单

- **统一流式接口**：所有 AI 后端实现 `AIBackend` 接口，对外暴露统一的 `ExecuteStream()` 方法，返回 `<-chan StreamEvent`。调用方无需关心底层差异
- **双传输模式**：CLI shell-out（传统模式，通过 stdout 解析）和 ACP stdio（JSON-RPC 双向通信，提供模式切换、斜杠命令、权限审批等结构化能力）。Agent 的 `Transport` 字段决定使用哪种传输，可按会话切换
- **多后端支持**：支持 13 种 AI 后端（Claude、Codebuddy、OpenCode、Codex、Qoder、VeCLI、DeepSeek/CodeWhale、Kimi、Copilot、MiMo-Code、Pi、Antigravity、Grok Build），每个后端在 `BackendRegistry` 中声明规格（CLI 命令、模型发现策略、ACP 命令），factory 根据后端类型创建对应的 `AIBackend` 实例
- **ACP 连接管理**：每个 ClawBench 会话独占一个 ACP 连接（通过 `ACPConnManager` 单例的 `conns map[string]*ACPConn` 维护，键为 `clawbenchSID`）。连接空闲 5 分钟后由定时清理任务回收，活跃会话不会被回收；连接断开后可重新创建并重试，失效的配置值会被跳过
- **流式事件累加（AccumulateBlock）**：StreamEvent 经 `AccumulateBlock()` 合并为 `[]ContentBlock` 列表。text/thinking 事件合并到最近的同类型 Block（跨 tool_use 边界回溯），tool_use 按 ID 增量更新。ACP 子 Agent 回放检测：当子 Agent 在工具调用后重发已完成段落的前缀文本时，累加器识别并替换原始 Block、删除中间重复 Block，避免同一段落被碎片化展示
- **连续 thinking Block 合并**：`MergeConsecutiveThinkingBlocks` 后处理步骤将相邻的 thinking Block（包括跨 tool_use 边界的）合并为连续内容。ACP Agent 交替输出 `AgentThoughtChunk` 和 `ToolCall` 事件，导致大量碎片化的 thinking 片段——合并后前端展示连贯的思考过程
- **ACP context_state 持久化**：ACP 会话的 mode、thinking effort、usage 状态持久化到 `chat_sessions.context_state` 列（JSON 格式）。服务重启后加载会话时即可恢复状态显示，无需等待 ACP 重连推送。部分更新通过原子合并操作写入，避免并发读-写-合并竞态。详见 [会话生命周期](session-lifecycle.md)
- **流式事件标准化**：各后端不同的输出格式经 LineParser（CLI）或 ACP 事件翻译层（ACP）统一为标准 StreamEvent 类型。ACP 额外提供 mode_update、config_update、thinking_effort_update、plan_update、model_list_update、commands_update 等能力事件
- **AskQuestion 标签转换**：`ConvertAskQuestionBlocks()` 检测文本 Block 中的 `<ask-question>` XML 标签（AI Agent 偶尔在文本中输出结构化交互请求），将其解析并转换为标准 `tool_use` Block（name=`AskUserQuestion`）。支持 XML 和 JSON 两种格式，容忍非标准闭合标签和未闭合标签。保证前端交互 UI（确认/选择）能统一处理所有形式的交互请求
- **无效工具调用清理**：`RemoveRejectedToolBlocks()` 剔除被 CLI 拒绝的工具调用（Status="error" 且输出含 "not found in agent cli"），这些是 AI 幻觉产生的不存在工具名（如 `/commit` 斜杠命令或 `AskUserQuestion` 未转为 tool_use 时）。同时删除引用该工具名的警告 Block，避免前端展示无意义的错误提示
- **thinking_done 信号**：累加器将 `thinking_done` 事件标记到最近一个 thinking Block 的 `Done` 字段，前端据此在完整响应结束前即可停止思考过程的旋转动画，而非等到整个流结束
- **thinking 惰性加载（lazy-load）**：聊天流式输出完成后，`Finalize` 将 thinking 文本从消息内容中拆分到独立的 `chat_thinking` 表，前端只收到缩略的 thinking Block（含 `think_id`，不含完整文本）。用户展开 thinking Block 时，前端通过 `GET /api/ai/chat/thinking` 按需加载完整文本（`useThinkingContent` composable）。流式过程中 thinking Block 不缩减，保持完整展示；流结束后立即折叠——避免长 thinking 文本占用大量 DOM 空间，用户只在需要时才加载全文
- **工具调用时长追踪**：`SessionExecutor` 在工具调用的 `ToolCallUpdate` 事件中记录每个工具的开始时间，流结束时写入 `DurationMs` 字段并持久化到 `chat_tool_calls.duration_ms` 列。前端在工具详情抽屉中展示各工具调用耗时，帮助用户理解 AI 执行步骤的时间分布
- **ACP 连接状态提取**：`acp_state_extract.go` 从 ACP 协议响应（NewSession/ResumeSession）提取 mode、thinking effort、model、config option 状态。ACP v2 Agent 通过 `ConfigOptions` 的 category 字段暴露模式（`mode`）和思考深度（`thought_level`），旧版通过独立的 `Modes` 字段暴露——两条路径同时支持，保证新旧 Agent 兼容
- **ACP 崩溃诊断**：Agent 进程意外退出时，`crashDiagnostics` 收集退出码、stderr 尾部（~2KB）、进程存活时间、信号名（SIGKILL/SIGSEGV 等）、父进程 PID、内存占用和 FD 数。数据在 `Wait()` 前从 `/proc/<pid>/status` 和 `/proc/<pid>/fd` 采集（进程 reap 后 `/proc` 数据消失）。诊断结果以紧凑字符串形式记录到日志，帮助定位崩溃根因（如 OOM Kill、SIGSEGV、SIGPIPE）
- **ACP 权限审批**：ACP 后端请求用户审批工具调用时，系统推送 `permission_pending` 事件，前端展示审批界面，用户批准/拒绝后通过 WS `permission_respond` 消息回传（HTTP `/api/ai/permission/respond` 作为备选通道）
- **ACP LoadSession 异步回放**：ACP LoadSession 立即返回 `replayPending: true`，前端无需等待历史回放即可发送新消息——Agent 已从加载的会话获得完整上下文。回放在后台 goroutine 中异步执行，持久化消息到 DB 后通过 `replay_done` WS 事件通知前端。LoadSession 能力来源是 `BackendSpec.ACPLoadSession` 而非 ACP Initialize 响应——某些 Agent（如 CodeBuddy）在 Initialize 中报告 `LoadSession=true` 但实际不支持
- **工具名称归一化**：不同后端对同一操作使用不同的工具名称（如 `read_file` vs `Read`），归一化层统一映射，保证前端显示和 RAG 索引的一致性
- **孤儿进程清理**：服务启动时扫描系统中的 AI 子进程孤儿（通过环境变量标记），检查父进程存活后安全清理。防止服务崩溃后遗留的进程占用资源
- **ACP Stdout 过滤器（acpStdoutFilter）**：所有 ACP 连接的 stdout 经过过滤器处理，修复三类 JSON-RPC 协议违规：
  1. **String-Number ID 不匹配**：CodeWhale 等后端在响应中返回 `"id":"1"`（字符串），而请求发送的是 `"id":1`（数字）。ACP SDK 严格匹配 ID，`"1" != 1` 会导致响应被静默丢弃。过滤器检测并转换回数字形式
  2. **非 JSON 行**：某些后端在 ACP stdio 模式下向 stdout 输出终端转义序列，过滤器跳过不以 `{` 开头的行
  3. **SessionModelState 提取**：Kimi ACP 通过 `NewSessionResponse.models` 字段返回可用模型列表，但 ACP Go SDK v0.13.5 的 `json.Unmarshal` 不包含此字段，导致模型信息被静默丢弃。过滤器在原始 JSON 中拦截并缓存 `models` 字段，作为 `extractACPModelList` 的后备数据源
  - **进程退出防挂起**：过滤器在后台处理过滤和重发行，当 Agent 进程被 kill 但 OS 尚未关闭 stdout 管道时，过滤器的 `Close()` 调用立即解除阻塞的读取操作，防止进程等待挂起
- **CodeWhale ACP 字段重映射**：CodeWhale 在 ACP 模式下使用简写字段名（如 `path` 代替 `file_path`、`search` 代替 `old_string`）。重映射表将其映射为前端渲染器的标准字段名，工具名前缀表将 CodeWhale 工具名（如 `read_file`）映射为 UI 友好的显示前缀（如 `Read`）
- **BackendSpec.AltCmd 回退检测**：`AltCmd` 字段提供备用 CLI 命令名——当主命令在 PATH 中未找到时，检查 `AltCmd` 是否存在。当前仅 CodeWhale 使用：`DefaultCmd: "codewhale", AltCmd: "deepseek"`，兼容旧版二进制名
- **Pi 仅支持 CLI 模式**：Pi 当前不注册 ACP 配置。请求 `acp-stdio` 传输时会自动降级为 CLI 模式
- **Antigravity ACP 桥接**：Antigravity 后端通过 `agy-acp` ACP 桥接适配器接入，仅支持 `acp-stdio` 传输模式，没有 CLI 命令。这是外部 Agent 的集成模式——桥接适配器将非 ACP 原生的 Agent 包装为 ACP 协议兼容的子进程
- **Grok Build 双传输模式**：Grok Build 后端同时支持 ACP（`grok agent stdio`）和 CLI（`grok -p ... --output-format streaming-json`）两种传输。ACP 为首选传输，CLI 作为流式 JSON 回退。`GrokStreamParser` 解析 CLI 的 JSON Lines 输出（text/thought/end/error 事件类型），从 end 事件捕获 session ID 和 token 用量
- **OPENCODE_PERMISSION 注入**：OpenCode 的 ACP 连接自动注入 `OPENCODE_PERMISSION` 环境变量，将默认需人工审批的三个权限（文件读取、文件写入、命令执行）转为自动通过——防止 OpenCode 子 Agent 在无人值守的定时任务场景中因权限审批而挂起
- **共享规则模板（commonRulesTemplate）**：所有 Agent 的系统提示词前注入 `commonRulesTemplate`，包含用户交互格式规范（XML `ask-question` 标签）和媒体生成规则。模板用 `«»` 占位反引号，运行时替换。另有 `mediaRulesTemplate` 仅在用户消息携带文件附件时注入

### 设计要点

- **双传输分流在 factory 层**：`NewBackendForAgentWithTransport` 根据 Agent 的 `Transport` 字段（"cli" / "acp-stdio"）决定创建 ACPBackend 还是 CLIBackend。ACP 不可用时降级到 CLI 并记录警告——用户选择 ACP 是有意的，降级是容错而非静默回退
- **ACP 连接管理实现注意**：`ACPConnManager` 是单例，管理每个 ClawBench 会话独占一个 ACP 连接。`ACPConn` 内部可能复用 goroutine，但对外是一对一映射
- **ACP 一对一而非连接池**：`ACPConnManager` 是单例，管理每个 ClawBench 会话独占一个 ACP 连接。AI Agent 的会话状态是私有的，无法在连接间共享
- **CLIBackend 是通用骨架**：所有 shell-out 后端共享 `CLIBackend` 的进程管理、stdout 管道、上下文取消逻辑，差异仅在于 CLI 参数构建和输出解析策略——新增后端只需提供这两个策略
- **后端规格集中声明**：所有后端的规格（CLI 命令、模型发现策略、ACP 命令）在 `BackendRegistry` 中集中声明，factory 通过后端类型字符串匹配创建实例。新增后端需要同时添加规格条目和 factory 分支
- **ACP 状态缓存与重发**：每个连接缓存当前的 mode、thinking effort、config、commands、plan 状态和 `replayPending` 标志。新连接或重连时自动重发，保证前端在任何时刻都能恢复完整的 UI 状态。`replayPending` 标识 LoadSession 异步回放是否仍在进行
- **ACP 全局函数变量打破循环依赖**：`internal/ai` 包通过全局函数变量（`getExternalSessionID`、`getSessionAutoApprove`、`onPermissionStateChange`）与 `internal/service` 和 `internal/ws` 包通信——Go 不允许循环依赖，函数变量是在编译期解耦、运行期桥接的折中方案
- **ACP 工具调用防抖**：`ToolCallUpdate` 事件以 50ms 窗口批量发送，将推送给前端的 WS 事件率降低约 95% 而不丢失信息——AI 工具调用的流式更新频率极高，逐条推送会淹没前端。终端事件（完成/失败）立即发送，不等待防抖窗口
- **ExitPlanMode 正常结束流**：CLI 后端检测到 ExitPlanMode 事件时结束当前流（不再有 AutoResumeBackend 重连机制）。ExitPlanMode 是 Agent 有意结束计划模式的信号，不应尝试续接——之前的 AutoResumeBackend 会将 ExitPlanMode 误判为异常中断并重试，导致重复执行
- **Agent 存储以 DB 为主**：Agent 配置存储在数据库（`agents` 表），YAML 用于手动定义的特殊 Agent。自动发现只更新基础设施字段（`acp_command`、`transport`），用户自定义的 `name`、`command` 不被覆盖。ACP 相关字段（`transport`、`acp_command`、可用模式、思考深度、命令等）持久化在 `agents` 表中，重启后无需重新发现
