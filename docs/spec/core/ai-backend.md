# AI 后端抽象

ClawBench 支持多种 AI 工具，每种工具的调用方式、输出格式各不相同。AI 后端抽象层将这种差异封装为统一的 `AIBackend` 接口——handler 只需调用 `ExecuteStream()`，不关心背后是 Claude 还是 Kimi。系统支持两种传输模式：CLI shell-out（传统模式，通过 stdout 流式解析）和 ACP stdio（Agent Client Protocol，通过 JSON-RPC 双向通信，提供模式切换、斜杠命令和权限管理等结构化能力）。12 个后端通过插件注册体系（`backends` 包）自描述，每个后端子包在 `init()` 中注册自己的 CLI 参数、流式解析器和工具名称映射。传输选择在 factory 层根据 Agent 的 `Transport` 字段决定，调用方完全透明。

## 流程图

### 后端选择与传输分流

```mermaid
flowchart TD
    A[POST /api/ai/chat] --> B{解析 Agent Transport}
    B -->|acp-stdio| C{SupportsACP?}
    C -->|是| D[ACPBackend]
    C -->|否| E[降级 CLI + 警告]
    B -->|cli| F[backends 插件注册表]

    D --> G[ACP JSON-RPC over stdio]
    E --> F
    F --> H{需要 AutoResume?}
    H -->|是| I[AutoResumeBackend 包装]
    H -->|否| J[直接使用 CLIBackend]
    I --> J
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

### AutoResume 流程（仅 CLI 模式）

```mermaid
sequenceDiagram
    participant 调用方
    participant AutoResume
    participant 内层后端

    调用方->>AutoResume: ExecuteStream()
    AutoResume->>内层后端: ExecuteStream(原始 prompt)
    内层后端-->>AutoResume: StreamEvent 流
    AutoResume-->>调用方: 转发事件
    Note over AutoResume: 检测到 ExitPlanMode
    AutoResume->>AutoResume: 取消内层上下文
    AutoResume-->>调用方: 发出 resume_split 事件
    AutoResume->>内层后端: ExecuteStream("继续", Resume=true)
    内层后端-->>AutoResume: 新的 StreamEvent 流
    AutoResume-->>调用方: 转发恢复后的事件
```

AutoResume 只用于 CLI 模式后端。ACP 后端使用会话级取消而非进程终止来处理卡死，不需要 AutoResume 的"杀进程→恢复"模式。某些 CLI 后端（Claude、Codebuddy、Qoder 等）在计划审批时触发 ExitPlanMode——AutoResumeBackend 自动处理：检测到后取消当前执行，自动恢复并继续，对调用方透明。

## 功能与设计要点

### 功能清单

- **统一流式接口**：所有 AI 后端实现 `AIBackend` 接口，对外暴露统一的 `ExecuteStream()` 方法，返回 `<-chan StreamEvent`。调用方无需关心底层差异
- **双传输模式**：CLI shell-out（传统模式，通过 stdout 解析）和 ACP stdio（JSON-RPC 双向通信，提供模式切换、斜杠命令、权限审批等结构化能力）。Agent 的 `Transport` 字段决定使用哪种传输，可按会话切换
- **多后端支持**：支持 12 种 AI 后端（Claude、Codebuddy、OpenCode、Codex、Qoder、VeCLI、DeepSeek、Cline、Kimi、Copilot、MiMo-Code、Pi），每个后端是 `backends` 包下的独立子包，通过 `BackendPlugin` 自描述注册
- **ACP 连接管理**：每个聊天会话独占一个 ACP 连接（一对一模型），5 分钟空闲自动回收，活跃会话受保护不被回收。连接断开后自动重生并重试，崩溃的配置值自动跳过
- **自动恢复（AutoResume）**：仅 CLI 模式。对 ExitPlanMode 场景自动执行"取消→恢复继续"流程，避免用户手动干预
- **流式事件标准化**：各后端不同的输出格式经 LineParser（CLI）或 ACP 事件翻译层（ACP）统一为标准 StreamEvent 类型。ACP 额外提供 mode_update、config_update、thinking_effort_update、plan_update、model_list_update、commands_update 等能力事件
- **ACP 权限审批**：ACP 后端请求用户审批工具调用时，系统推送 `permission_pending` 事件，前端展示审批界面，用户批准/拒绝后通过 `/api/ai/permission/respond` 回传。移动端通过 JPush 通知收到审批提醒
- **工具名称归一化**：不同后端对同一操作使用不同的工具名称（如 `read_file` vs `Read`），归一化层统一映射，保证前端显示和 RAG 索引的一致性
- **孤儿进程清理**：服务启动时扫描系统中的 AI 子进程孤儿（通过 `CLAWBENCH_CHILD=1` 环境变量标记），检查父进程存活后安全清理。防止服务崩溃后遗留的进程占用资源

### 设计要点

- **双传输分流在 factory 层**：`NewBackendForAgentWithTransport` 根据 Agent 的 `Transport` 字段（"cli" / "acp-stdio"）决定创建 ACPBackend 还是查询插件注册表创建 CLIBackend。ACP 不可用时降级到 CLI 并记录警告——用户选择 ACP 是有意的，降级是容错而非静默回退
- **ACP 一对一而非连接池**：虽然代码中有 `ACPConnectionPool` 的历史命名，实际是每个 ClawBench 会话独占一个 ACP 连接。AI Agent 的会话状态是私有的，无法在连接间共享
- **CLIBackend 是通用骨架**：所有 shell-out 后端共享 `CLIBackend` 的进程管理、stdout 管道、上下文取消逻辑，差异仅在于 `BuildArgsFn` 和 `NewParserFn`——新增后端只需实现这两个函数并在 `init()` 中注册
- **后端插件自描述注册**：每个后端子包在 `init()` 中调用 `Register()` 注册 `BackendPlugin`，包含 CLI 工厂、工具名称映射（ToolNameMap）、输入字段重映射（InputRemaps）和 ACP 映射数据。新增后端只需添加子包，无需修改 factory 代码
- **AutoResumeBackend 是透明包装器**：仅包装 CLI 后端。ACP 后端不使用 AutoResume——ACP 用会话级取消替代进程终止，两种取消策略不兼容。`NeedsAutoResume` 标志在 `BackendPlugin` 中声明
- **ACP 状态缓存与重发**：每个连接缓存当前的 mode、thinking effort、config、commands、plan 状态。新连接或重连时自动重发，保证前端在任何时刻都能恢复完整的 UI 状态
- **ACP 工具调用防抖**：`ToolCallUpdate` 事件以 50ms 窗口批量发送，将 SSE 事件率降低约 95% 而不丢失信息——AI 工具调用的流式更新频率极高，逐条推送会淹没前端
- **Agent 存储是纯 DB 驱动**：Agent 配置存储在数据库（`agents` 表），YAML 仅用于手动定义的特殊 Agent。DB 优先，`source` 字段区分 "auto"（自动发现）和 "setup"（向导创建）。ACP 相关字段（`transport`、`acp_command`、可用模式/思考深度/命令等）持久化在 `agents` 表中，重启后无需重新发现
