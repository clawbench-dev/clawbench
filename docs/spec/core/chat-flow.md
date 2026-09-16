# 聊天流程

聊天是 ClawBench 的核心业务——用户发送一条消息，系统启动对应的 AI 后端执行，流式输出结果到前端，同时持久化到 SQLite 并建立 RAG 索引。会话完成后自动生成摘要，任务的执行结果可以续接为交互式对话。ACP 后端还支持模式切换、计划审批和权限管理，让 AI 从纯文本输出扩展为结构化的交互体验。这条链路贯穿了 handler、SessionExecutor、AI 后端、WebSocket 和前端五个层，是理解整个系统的入口。

## 流程图

### 请求链路：从用户发消息到 AI 开始执行

```mermaid
sequenceDiagram
    participant 前端
    participant handler
    participant BuildChatRequest
    participant runTurn
    participant AI后端

    前端->>handler: POST /api/ai/chat
    handler->>BuildChatRequest: 构造 ai.ChatRequest（唯一实现）
    Note over BuildChatRequest: 会话持久化的 model/transport、<br/>resume 守卫、fork 包裹、附件前缀
    handler->>runTurn: 启动 turn
    runTurn->>AI后端: ExecuteStream(ctx, ChatRequest)
    AI后端-->>runTurn: 返回 StreamEvent channel
    runTurn-->>前端: WS 流式推送 + 落库
```

用户点击发送后，请求进入 handler，由 handler 解析出目标 Agent 和后端类型（CLI 或 ACP）；`runTurn` 负责编排单个 AI 回合，内部的 `SessionExecutor` 持有会话运行时状态并把执行委托给 AI 后端。**"用户消息 → ai.ChatRequest"只有一份实现**：直发路径（HTTP handler）与排队路径（队列 drain、钉钉/飞书）曾各写一份并漂移成 5 处不一致——resume 守卫、会话持久化的 model 与 transport、fork 上下文格式、非 block 内容解包——导致同一条消息在"直接发送"与"排队发送"下行为不同（排队路径会把 CLI 从未见过的会话 ID 传下去造成上下文失忆）。现在两者共用同一实现，并有一条对比测试锁住两条路径对同一会话的构造结果一致。

### 唯一 turn 实现：三入口共用

```mermaid
flowchart TD
    A[POST /api/ai/chat] --> D[runTurn]
    B[队列 drain / 空闲启动] --> D
    C[定时任务 scheduler] --> D
    D --> E[建占位符 + 广播 stream_start]
    E --> F[OnStarted 回调<br/>任务在此发 running 事件]
    F --> G[阻塞事件循环，消费 StreamEvent]
    G --> H[Finalize：落库 + 摘要 + 终态事件]
```

三条入口此前各有一份执行实现且已互相漂移（入队自愈只存在于 queue 路径，导致前端普通发送的消息可能永久滞留 `queued=1`）。现在共用同一实现，差异以显式参数保留：错误文案本地化由调用方提供、Finalize 时是否 drain 事件通道按调用方保留原行为、取消/失败/崩溃分支由 scheduler 自己持有。`OnStarted` 回调是为了让任务在"executor 已建好、占位符存在、`stream_start` 已广播、但事件循环尚未开始"这个确切时刻发出 running 事件——此前 running 事件在阻塞调用返回后才发，实际顺序成了 started → 整个任务跑完 → running → completed，钉钉/飞书推送与 Android 离线事件都按此顺序回放，等于 running 通知完全失效。

### WebSocket 推送链路：流式事件到前端渲染

```mermaid
sequenceDiagram
    participant AI后端
    participant SessionExecutor
    participant handler
    participant 前端

    AI后端->>SessionExecutor: StreamEvent(content/thinking/tool_use/done)
    SessionExecutor->>SessionExecutor: 持久化消息到 SQLite
    SessionExecutor->>SessionExecutor: 触发 RAG 索引（异步）
    SessionExecutor->>StreamHub: EmitToSession(sessionID, event)
    StreamHub-->>前端: WS {type:"event", data:{ChatStreamData | session_update}}
    前端->>前端: useChatRender 解析+合并 Block
```

AI 后端产出的事件经 WebSocket StreamHub 推送给已订阅该 session 的客户端（多客户端扇出）；同时触发 SessionExecutor 的增量持久化和 RAG 索引。会话完成后自动生成摘要——固定提取最后回答文本（`ExtractLastAnswerFromBlocks`，无需 AI 调用）。摘要结果通过 WebSocket `summary_update` 事件实时推送到前端。

### ACP 权限审批流程

```mermaid
sequenceDiagram
    participant ACP后端
    participant SessionExecutor
    participant ws.Manager
    participant 前端

    ACP后端->>SessionExecutor: PermissionRequest(toolCall)
    SessionExecutor->>ws.Manager: permission_pending 事件
    ws.Manager->>前端: WS 推送（含工具名称）
    alt 前端在线
        前端->>前端: 显示审批界面
        前端->>SessionExecutor: POST /api/ai/permission/respond
        SessionExecutor->>ACP后端: RespondPermission(approve/reject)
    else 前端离线
        ws.Manager->>ws.Manager: 缓冲事件，等待重连
    end
```

ACP 后端的工具调用可能需要用户审批（如执行 shell 命令、写入文件）。系统通过 WebSocket 推送 `permission_pending` 事件，前端离线时缓冲事件等待重连。用户批准或拒绝后，前端调用 `/api/ai/permission/respond` 回传结果，系统将响应转发给 ACP 连接。未决的审批请求不会被会话切换/回合结束取消——权限保留到用户响应或 agent 连接死亡时自动清理，避免"审批永远失效"。

### 子智能体内容分组链路

```mermaid
sequenceDiagram
    participant Agent as ACP Agent
    participant 后端 as extractParentToolCallID
    participant 累加 as AccumulateBlock
    participant 前端 as ContentBlocks

    Agent->>后端: 子智能体 content/thinking/tool<br/>_meta[parentToolCallId]
    后端->>后端: 按后端归一化提取父 id
    后端->>累加: StreamEvent.ParentToolCallID
    累加->>累加: 子块不并入父 / 不跨父合并 thinking
    累加->>前端: ContentBlock + WS parent_tool_call_id
    前端->>前端: 按父 id 分块，折叠进父 Agent 卡片
```

父 Agent 卡片就是那条发起子智能体的工具调用（前端渲染为 Agent 胶囊）。子智能体产出的 thinking/text/tool 全部收进同一张卡片，折叠时只显示步数摘要、展开才挂载并递归复用主渲染组件——这样一条"派发多个子智能体并行探索"的长轨迹不会淹没主对话。归属靠 Agent 在 `_meta` 上打的父工具调用 id 精确判定，不靠时间窗口推断。

## 功能与设计要点

### 功能清单

- **消息发送与流式回复**：用户输入 prompt 后，系统选择对应的 AI Agent 执行并实时流式返回结果。这是系统的核心价值——让用户在移动端也能获得与桌面 CLI 等同的 AI 交互体验
- **多 Agent 选择**：用户可以切换不同的 AI 后端（Claude、Codebuddy、Kimi 等），每个 Agent 有独立的系统提示词、模型和思考深度配置。不同后端各有擅长，用户按需选择
- **消息持久化与历史回看**：所有聊天消息存入 SQLite，支持分页加载、搜索、归档。用户可以随时回看历史对话，归档的对话仍可被 RAG 检索，通过会话搜索恢复
- **排队机制**：同一会话的消息排队执行，前一条未完成时后续消息入队等待。防止并发冲突，保证消息顺序。排队消息在入队瞬间以 `queued=1` 写入 `chat_history`（含队列 ID），执行由 drain loop 统一从数据库按序出队（出队即翻 `queued=0` 成为普通会话记录）。drain 循环内置熔断：出队连续失败超过重试窗口（5×100ms≈500ms）时放弃队列、清理残留排队消息并广播 `queue_cancel`，再发送错误事件让会话离开 loading 态——避免数据库持续故障时会话永远卡在"加载中"且队列静默死亡。后端因会话已停止而出队消息时（`needs_start`），前端自动将消息重提交为新聊天而非静默丢失——`chatQueueSend` 封装了"排队→needs_start 重提交"的共享编排逻辑
- **文件上传与引用**：用户可以上传文件作为消息附件，AI 可以读取这些文件。附件支持行范围（`startLine/endLine`），prompt 前缀中文件路径附带行号信息（如 `path:10-20`），帮助 AI 聚焦于文件特定区域。降低了在移动端传递上下文的成本
- **URL 附件（外部链接）**：附件条目除本地文件外还可表示**外部地址**（`kind=url`，如引用的 GitHub issue/PR）——chip 显示 `owner/repo#123` 人类可读标签。此类条目**不参与文件系统校验**（不 `os.Stat`、不落 `Files`，否则会 404），但地址本身必须以 `[Referenced external link: <url>]` 单行前缀注入 prompt——跳过前缀注入会让 AI 完全不知道被引用的条目是什么，而把标签当作 `[User uploaded ...]` 本地路径注入则会让 AI 去找一个不存在的文件。安全性上仅接受 http/https，拒绝 `javascript:`/`data:` 等会被持久化并重新渲染为 `href` 的协议；为防伪造附件标签（其他代码会**任意位置**扫描 `[Current file` / `[User uploaded`），URL 校验还拒绝空白与方括号——真实 URL 必须百分号编码这些字符。这让"引用远端资源问 AI"与"引用本地文件"走同一条附件通道，而不必为 URL 单开一种消息形态
- **附件 prompt 注入必须走共享分类器**：共有**三个** prompt 构建点——`POST /api/ai/chat`（`handler.AIChat`）、队列 drain（`handler.buildChatRequestFromQueue`）、队列端点空闲时启动的独立执行引擎（`service.LaunchSessionExecution` → `executeStreamRunShared`）。三者分属 `handler` 与 `service` 两个包且**不能互相 import**（handler 已 import service），所以分类与注入收敛在 `internal/model/attachment_prompt.go`：`ClassifyAttachments` / `ApplyAttachmentPrefixes` / `FileEntryLabel` / `HasAttachmentEntries`。**新增附件类型或新增 prompt 构建路径时必须复用这些函数**，不要再写第二份循环——历史上正是因为三处各写一份，URL 附件在其中两处被静默丢弃（一处 `continue` 跳过、一处当成假本地文件、一处连 `Files` 都没传）。`service.LaunchConfig.Files` 必须随每次执行传递（含 drain 循环逐条替换为 `msg.Files`，否则上一轮的附件会串到下一轮）。新增前缀必须同步登记到 `clientInjectedStripRules`（引用 `model.ReferencedLinkPrefix` 以免漂移），否则会被误当作人类输入参与会话标题推导
- **引用提问**：选中聊天或文件中的文本片段，以引用形式发送新问题。减少上下文描述的开销，尤其适合代码审查场景
- **快捷发送**：预设常用 prompt 通过输入栏行尾图标一键加入输入框（点击注入后可直接编辑再发送），避免重复输入。移动端打字成本高，这个功能显著降低了常用操作的交互开销
- **聊天自动摘要**：会话完成后自动为助手消息生成摘要，通过 WebSocket 实时推送（含 SummaryCards 结构化卡片元数据）。`summarizeMessage` 统一调度入口固定提取最后回答文本（`ExtractLastAnswerFromBlocks`，无需 AI 调用）。前端 `SummaryToggle` 组件提供按钮模式（聊天中切换）和标签页模式（任务执行详情中切换）。**消息展示模式**（`messageDisplayMode` 设置，默认 mixed）决定有摘要消息的默认呈现：混合模式下最近一条 AI 回复展示原文、其余展示摘要，摘要/原文模式则全局统一；单条消息仍可独立切换，切换逻辑用归一化后的模式加位置判断。摘要视图复用原回复的 warning/error 横幅。用户快速浏览 AI 回复的核心内容，不必逐行阅读长输出
- **推荐回复**：会话完成后自动生成一条下一步建议（`chat_recommendation` WS 事件），前端在输入框上方展示推荐横幅，用户可一键采纳或忽略。推荐由 LLM 基于 stable/rolling 分离的 payload 生成，支持 prompt caching。详见 [推荐回复](../features/chat-recommendation.md)
- **续接对话**：任务的执行结果可以续接为新的交互式聊天会话，继承原始会话的消息、摘要和 `external_session_id`。用户看到任务结果后想继续追问，无需从头描述上下文
- **消息详情弹窗**：点击助手消息可查看元数据弹窗，展示消息的后端原生会话 ID（`external_session_id` 注入 response metadata）、时间、token 等上下文信息，帮助用户理解消息来源与消耗。弹窗字段与上下文面板对齐——从 `chat_metadata` 读取缓存的 _meta 扩展信息（缓存命中/额度/追踪标识等）
- **ACP 模式切换**：ACP 后端支持多种工作模式（如 code、ask、architect），用户可在聊天中切换，切换即时生效并持久化。不同模式适合不同任务，用户按需选择
- **ACP 权限审批**：ACP 后端请求工具调用审批时，系统推送通知提醒用户，避免因未审批而阻塞执行
- **交互式提问卡**：AI 需要用户做结构化选择时（在文本中输出 `<ask-question>` 块），后端将其转换为 `AskUserQuestion` 工具调用，前端渲染成交互卡片——每个问题一张卡，选项可单选或多选，带可选的自由文本补充栏、"推荐"按钮（反向询问 AI 建议选哪个）和"提交"按钮。用户提交后，所选标签（加补充文本）作为一条普通用户消息发回，AI 继续对话。单选模式下再次点击已选中的选项即取消选中，误触可撤销而不被迫改选其他项；提交按钮在未作答时保持禁用，作答后启用。把"AI 提问—用户选择"从纯文本往返变成结构化交互，显著降低移动端打字成本。**用户已填的答案由独立的模块级存储承载**：卡片正文是 `v-html` 注入的 HTML 字符串，任何渲染字符串变化（前后台切换触发的强制重载、列表重挂载、合并卡分支翻转、`block.input` 异步回填、抽屉重赋值）都会整体替换子树，DOM 里的输入随之丢失——因此补充信息、选项勾选、已提交状态三项都写入按卡片身份（`data-ask-key`）索引的模块级存储，在挂载与更新时回填。存储必须是模块级而非组件级（组件级 Map 会随子树重建被回收），提交后不删条目（删了重载会恢复成未答），发送失败必须回滚持久化的已提交标志（否则失败的发送永久卡在已提交、无法重试）
- **消息元信息区在气泡外**：耗时/时间/操作按钮等元信息渲染为气泡的**兄弟节点**而非子节点，落在面板底色上——此前它在气泡内部，会被背景、圆角与 `overflow:hidden` 包裹。用户消息同样有元信息区（友好相对时间 + 复制 + 查看详情），助手独有操作（摘要切换/朗读/分叉/回溯）不进入用户行；排队中的消息不显示元信息。行级内缩只作用于气泡本身，元信息行右边缘与助手消息对齐
- **ACP 计划模式**：ACP 后端在执行前展示计划（步骤列表），用户可以跟踪进度。让用户理解 AI 将要做什么，而非只能看到结果
- **子智能体内容分组**：当 ACP Agent 派生子智能体（如 CodeBuddy 的 Task/Agent 工具、Claude/Codex 的子线程）时，子智能体产出的 thinking/text/工具调用不再平铺进主对话，而是收进发起它的那条父 Agent 卡片内——折叠态只显示「N 步」摘要胶囊，展开才递归渲染完整子轨迹。父卡片与普通工具胶囊交互一致（步数 + chevron 内联），展开后底部带"收起"footer（长轨迹滚到底可直接收起），收起时锚定视口避免长内容骤减导致跳动；子 thinking 走惰性加载。归属由后端从 `_meta` 提取的父工具调用 id 精确判定（CodeBuddy 扁平键 / Claude·Qoder 嵌套键），前端按父 id 分块；孤儿或嵌套子块回退扁平渲染不丢内容。让"派发多个子智能体并行探索"这类长轨迹保持可读，用户按需下钻
- **thinking 惰性加载**：流结束后 thinking Block 被拆分到独立的 `chat_thinking` 表，前端只显示缩略 Block。用户展开时才通过 `GET /api/ai/chat/thinking` 按需加载全文——减少长思考过程对聊天视图的视觉占用
- **工具调用耗时展示**：每个工具调用的执行时长追踪并持久化到 `chat_tool_calls.duration_ms`，前端在工具详情抽屉中展示耗时。用户可以理解 AI 各步骤的时间分布，判断"哪个工具最慢"
- **统一斜杠命令注入**：用户消息以 `/cb-` 内置命令开头时，后端 `processClawbenchCommand()`（`internal/handler/clawbench_command.go`）检测并注入指令模板，指示 AI 用 Bash 调用本地 HTTP API。**接口说明不手写**：端点清单由 `internal/api` 从嵌入的 OpenAPI 规格（`internal/api/openapi.yaml`）按 operationId 渲染注入（`/cb-chatsearch` → `ragSearch`/`ragMessage`/`ragSession`/`ragSessionSearch`；`/cb-task` → `tasksList`/`tasksCreate`/`taskGet`/`taskUpdate`/`taskDelete`/`taskExecutions`/`agentsList`；`/cb-usage` → `usageStats`），规格与路由表之间有双向漂移守卫（`internal/handler/openapi_drift_test.go`）。模板仅保留无法从规格推导的行为规则（如创建任务后输出 `<scheduled-task id="..." />`）。占位符为 `{{BASE_URL}}`（AI 是子进程，需绝对 URL，按 `ResolveTLSActive()` 决定 http/https）、`{{PROJECT_COOKIE}}`（经 `ScopedCookieName()`，非默认端口带 `cb<port>_` 前缀）、`{{PROJECT_PATH}}`、`{{SESSION_ID}}`、`{{NOW}}`（仅 `/cb-usage`，AI 无可靠时钟）。**命令分派已收敛**：主路径（`chat.go`）对任意内置命令走同一条「前置校验 → 渲染 → 前置到 prompt」路径，命令各自的校验放在 `clawbenchCommandPrecheck()`，新增命令无需再加 if 分支。ClawBench 内置命令统一以 `cb-` 命名空间区分于智能体的 ACP 斜杠命令：`IsClawbenchCommand()` 识别 `/cb-*`，chat handler 据此将其排除出 ACP 斜杠命令路径（`IsACPSlashCommand`），避免被原样转发给智能体。前端徽章判定镜像该列表（`web/src/utils/contentBlocks.ts` 的 `CLAWBENCH_COMMAND_RE`，显式列名而非 `cb-` 通配，以免误判同前缀的智能体命令）。
- **用户消息索引导航**：聊天消息列表支持 Ctrl+Up/Down 在用户消息间快速跳转，跳转时自动跨分页加载并高亮目标消息。用户消息索引按钮在输入栏左侧，点击后弹出索引面板，列出所有用户消息的摘要和时间戳，方便在长对话中定位
- **浮动滚动按钮**：消息列表根据滚动方向显示上/下浮动按钮，自动隐藏，帮助快速导航长对话。按钮在用户停止滚动后短暂停留再消失，避免频繁闪烁
- **触摸拖拽防抖**：用户触摸消息列表时暂停自动滚动，防止用户阅读历史消息时被 AI 新输出推走。滚动跟随由统一状态机（`scrollState.ts`）判定——用户滚动（含惯性 fling）期间 `force` 不再无条件钉底，改为挂起跟随（pendingFollow），待滚动停止后恢复；滚动停止检测替代固定时间窗口，避免触屏惯性滚动把视图拉回/弹回
- **滚动保持机制**：滚动位置只在"当前会话内往上翻旧内容"时保留——同会话中途加载旧消息用数组替换锚定（不跳屏），流式新内容到达时不打断阅读位置；会话之间切换、项目切换永远滚到底部（Tab 切换靠 v-show 保留 DOM，浏览器原生保留 scrollTop，零代码）。发送消息后停止滚动则无条件拉回底部。首屏打开与冷启动一致，避免"切回会话落在错误位置"的困惑
- **按项目恢复上次会话**：每个项目独立记住最近打开的会话（localStorage，key 含项目根路径），进入项目时自动恢复上次会话，失效（会话被删除/归档）时自动回退默认逻辑（新建或打开最近会话）。多项目并行工作时，切换项目不必手动找回上次看到哪
- **输入草稿与会话快照**：切换会话时，未发送的输入文本（按会话草稿缓存）、已选附件和引用提问会被快照保存（`useChatContext` 的 `snapshotAttachments`/`restoreAttachments`），切回时自动恢复；消息发送或附件清理后丢弃对应快照，避免发送后残留脏数据
- **"全部加载"提示**：加载完所有历史消息后短暂显示"全部加载"提示，让用户明确知道已无更多历史内容，避免反复上拉触发加载
- **输入栏功能按钮**：聊天输入栏集成多个功能按钮——消息索引、ACP 同步、会话搜索、创建会话（点 `+` 总是打开 Agent 选择器，由用户明确选择后端而非一键创建）、归档（带确认对话框）、自动语音、上下文用量弹窗。按钮按使用频率排列，避免输入栏过于拥挤
- **推荐回复横幅**：AI 回复完成后在输入栏上方展示推荐横幅，可展开/折叠。横幅不遮挡输入区域，折叠后仅显示一行摘要
- **快捷发送菜单**：空输入时点击发送按钮弹出快捷菜单，选择预设 prompt 一键发送。与已有快捷发送功能互补，提供更轻量的入口
- **ACP 斜杠命令自动补全**：ACP 后端的斜杠命令在输入栏自动补全，用户输入 `/` 时弹出匹配的命令列表，减少记忆负担
- **上下文用量弹窗**：显示 token 使用详情（输入/输出/缓存），帮助用户了解当前会话的上下文消耗情况，决定是否需要压缩或新建会话。流式过程中 ACP usage 采用「最新完整快照」语义——CodeBuddy 一轮内推送多个 usage_update 通知、各带一部分扩展字段，带 token 计数的完整快照整体替换、纯 cost 裸通知永不覆盖，保证面板收到部分通知时不闪回最简视图、token 行始终内部自洽（累计型 counter 不被逐字段拼接），使下游用量统计 SUM 聚合有意义
- **ACP _meta 扩展元信息展示**：ACP Agent 通过 `_meta` 字段携带异构的 token/成本/追踪信息（CodeBuddy 的 OpenAI 风格 usage + cache split + credit、Claude/Codex 的 `_meta.quota` 分项、OpenCode 走标准 usage_update），后端按 agent 适配器解析并归一化，持久化到 `chat_metadata` 扩展列（缓存读/写 token、thought token、cache 分类、credit、request/trace ID、模型名、stop reason 等）。前端上下文面板、Token 明细与消息详情弹窗集中展示——缓存命中行与缓存读同值但标签区分，另展示缓存命中率（hit/(hit+miss)）。让用户理解每次 AI 回复的真实模型、用量分项与成本，而不只是一个大致的 token 数。这些逐条用量行同时是[数据统计面板](../features/usage-stats.md)的数据源——按项目对 `chat_metadata` 做维度聚合
- **消息回溯（Rewind）**：助手消息行内按钮（`POST /api/ai/session/rewind`）把会话历史**原址截断**到锚点 assistant 消息——事务删除其后全部 `chat_history` 及子表行（raw/tool_calls/thinking/summaries/recommendations），并清理对应 RAG 索引分块（RAG purge 在写锁释放后进行，避免自死锁）；`chat_metadata` 用量台账**刻意保留**——被回溯的轮次真实消耗过 token/成本，删掉会少计用量，而重发会生成新 message id 故不会双计；随后取消运行中的流、清空外部会话映射并回收 agent 连接，重启为全新 AI 会话——下次发送靠 fork-context 注入把保留的历史喂给新会话。被删除的最近一条用户消息文本回传给前端预填输入框，用户可重新编辑这条问题再发。锚点须为非流式 assistant 消息且有后续消息（无可回溯内容时返回错误、前端禁用最后一条按钮）。与「会话重置」不同：重置保留历史只回收卡死进程，Rewind 物理删除历史——解决"AI 从某处走偏，想删掉其后垃圾重来但保留此前上下文"的需求。**回溯后执行计划面板必须清空**：计划进度只缓存在 ACP 连接对象上，回溯销毁连接后已无从还原，若不显式清空，前端会一直显示上一轮的计划步骤
- **Compact 按钮**：上下文使用率 ≥ 75% 时显示 Compact 按钮，一键发送 `/compact` 命令压缩上下文。降低用户手动管理上下文的认知负担
- **模式长按切换自动审批**：长按模式芯片切换自动审批，无需每次工具调用都手动确认。适合信任 AI 操作的进阶用户
- **会话重置**：AI 错误/警告横幅上的"重置会话"按钮（`POST /api/ai/session/reset`）解决 ACP 会话卡死——当一轮交互以"工具已批准但从未执行"的悬挂状态结束时，后续 prompt 会毫秒级空响应。重置**刻意保留外部会话 ID 映射**，只回收卡死的 agent 进程，下一次 prompt 通过 ResumeSession 重新附着同一 agent 会话，对话上下文与聊天历史完整保留；前端确认后自动重发最后一条用户消息
- **完成弹窗**：会话或任务完成时，若聊天界面不在前台（用户在看其他 Tab 或当前会话不是目标会话），顶部弹出 Android 通知风格的完成卡片——展示摘要全文、项目名/路径、最近一条用户消息和 agent 后端图标，内置快捷输入框可直接追问，标记已读按钮和跳转按钮（跳转会话/任务执行详情）。发送追问或点标记已读会清空该会话的未读徽标（`POST /api/ai/chat/read`，支持 `project_path` 参数使外部项目弹窗也能通过归属校验）；点击空白处关闭弹窗（展示不足 1 秒时防误触忽略）；发送成功弹出确认气泡。用户消息以引用式样块展示（左侧 accent 竖线 + 淡色底），点击可展开完整内容。多个完成事件排队依次展示，取代了旧的会话结束 Toast 气泡。详见[完成通知弹窗](../features/completion-popup.md)。用户专注其他工作区时也能感知 AI 已完成并直接跟进，无需时刻盯着聊天窗口
- **未读自动清除**：当前会话执行结束（completed/cancelled）自动标记已读，切回前台时也自动标记当前会话已读——未读徽标只为"用户没在看"的会话保留（后台完成时跳过 mark-read，把未读留给悬浮窗/Live Updates 展示），用户回到该会话后徽标立即消失，无需手动操作
- **错误码透传与展示**：AI 后端返回的错误携带结构化错误码（`error_code`/`http_status`/`error_source`），从 StreamEvent 透传到前端 warning/error 卡片——错误码后缀（`[code xxx]`/`[HTTP xxx]`）+ 来源 chip（agent/clawbench/network）标注错误出处。ACP 后端把上游错误归类为 refusal 时（如钉住过期模型），系统识别 `stopReason=refusal` 发出 ReasonRefused 警告事件而非误判为"无内容返回"，refused 加入可重置会话的原因集合。用户能一眼判断"是 Agent 的问题还是平台/网络的问题"，而不是面对一条笼统的失败提示
- **Mermaid SVG 灯箱导航**：Mermaid 渲染后的 SVG 图表加入图片灯箱导航序列，与 `<img>` 按文档顺序排列，支持 prev/next 切换浏览所有视觉媒体

### 设计要点

- **排队消息持久化到 DB**：排队消息在入队时即写入 `chat_history`（`queued=1` 标记 + 队列 ID），由 drain loop 原子出队（写锁事务下翻转 `queued=0` 为普通会话记录）。相比纯内存队列，排队状态有数据库权威记录——历史加载、取消队列（按 `queue_id` 删除）、前端乐观 pending 气泡都以 `queued` 状态为准对齐，drain 循环与前端不会出现"消息已发但队列不知情"的分歧
- **归档保留 RAG 可搜索性**：归档的会话和消息标记 `archived=1` 而非物理删除，RAG 索引仍可检索到，用户可通过会话搜索恢复归档的会话——历史知识不应因用户整理而丢失
- **单 WS 通道统一推送**：聊天内容（`content/thinking/tool_use` 等 `ChatStreamData` 子事件）和系统事件（`session_update`/`task_update`/`summary_update`/`permission_pending`）共用 `/api/ai/events/ws`，由 `StreamHub`（`internal/ws/stream_hub.go`）做会话级扇出。同一 session 可被多客户端同时订阅；客户端通过 `subscribe` 消息加入，`unsubscribe` 退出
- **前端 Block 合并**：连续的 text/thinking 事件在 `AccumulateBlock` 中向后搜索同类型块进行合并，tool_use 作为自然边界——减少 DOM 更新频率，提升渲染性能。ACP 子代理完整重放产生的重复文本块通过前缀匹配去重，避免子代理回放时在 UI 中出现重复内容。父工具调用 id 是合并的硬边界：子 thinking 不并入父、连续 thinking 合并不跨父——否则子智能体的思考会被缝进父的思考块，分组信息丢失
- **子智能体归属用精确键而非窗口推断**：Agent 在 `_meta` 上主动标记父工具调用 id，是协议层给的可信归属信号；若靠"某段内容出现在某工具调用之后"推断，长回合中并行子智能体的内容会互相错配。因此后端只在标记存在时分组，缺失时回退扁平渲染——宁可少分组，不可错分组
- **提问卡按"可撤销的选择"设计**：结构化提问把 AI 的澄清意图变成可点选的选项，降低移动端打字成本；单选模式下允许再次点击取消选中，是因为误触后若只能改选其他项，会把错误的标签拼进发送内容——允许回到未作答态并同步禁用提交按钮，是"宁可让用户重新选，也不发出错误答案"的取舍。用户提交的选择以一条普通用户消息发回，不引入新的消息类型，复用既有的排队/持久化/摘要链路
- **自动摘要固定提取结论**：`summarizeMessage` 统一调度入口从消息 Block 中直接提取最后回答文本（`ExtractLastAnswerFromBlocks`，同步、无 AI 调用），聊天与任务行为一致。摘要结果存入统一的 `summaries` 表（含 `summary_cards` 列），通过 WS `summary_update` 事件推送（含 SummaryCards 结构化卡片元数据）——摘要生成与聊天流解耦，不影响流式体验
- **入口是 `runTurn`，`SessionExecutor` 是它内部的执行体**：两者不是竞争关系，而是分层——`runTurn` 是三条入口（HTTP 直发 / 队列 drain / 定时任务）共用的**编排入口**，负责建占位符、广播 `stream_start`、触发 `OnStarted` 与 Finalize；`SessionExecutor` 由 `runTurn` 用 `NewSessionExecutor(ctx, RunConfig)` 构造，持有该回合的运行时状态（块累加、工具调用与 thinking 的批量落库、steer 分裂、增量持久化），由 `runTurn` 调用 `RunWithChannel` 驱动、`Finalize` 收尾。差异化行为仍通过 `RunConfig.Mode` 控制（ModeInteractive / ModeScheduled）。因此看到"共用 `SessionExecutor`"时不要把它当成入口——三条入口共用的是 `runTurn`，`SessionExecutor` 是每个回合一个实例的内部执行体
- **分叉上下文按优先级挑选，而非尾部窗口**：`buildForkContext` 从原始消息读取（`GetMessagesBySessionIDRaw`——不走会剥离已摘要 assistant 回复 content blocks 的路径），在字符预算内**先选用户消息、再让助手消息填充剩余额度**：用户消息从最新往回逐条保留，放不下时截断而非丢弃（一条残缺的指令仍然约束模型，缺失的则不然），且每条按剩余份额均分上限，避免单条超长消息吃掉预算、把所有更早的指令挤出；助手条目随后填充剩余空间，放不下时直接跳过（它们的正文是工具载荷，截取片段很少有用）。实测用户消息平均约 51 字符而助手条目平均约 12 KB，因此"保留全部用户消息"几乎不占预算。角色比较必须大小写不敏感——Web 路径使用大写显示角色，直接比对小写常量会把该路径上所有条目都判成助手，正好把这条优先级反转
- **取消耗时被度量**：从点击取消到终态事件到达的间隔由模块级时间戳记录（而非 composable 内的 ref）——终态可能由两条独立路径抵达（聊天流的 terminal 事件与 `session_update` 兜底），点击发生在前者而后者可能赢得竞态，共享时间戳保证无论哪条先到都只上报一次。会话执行的 Finalize 阶段同样分阶段计时，仅慢路径输出
- **触摸防抖避免阅读干扰**：用户在阅读历史消息时，自动滚动应暂停，等用户停止触摸后恢复。这是移动端场景的关键体验——AI 持续输出时用户常需要回看上方内容，自动滚动会打断阅读。防抖机制通过检测触摸事件暂停自动滚动，在触摸结束后延迟恢复，平衡了"实时追踪新输出"和"自由回看历史"两个需求
