# AI 群聊设计（Group Chat）

日期：2026-10-04
状态：设计定稿（待实现）

## 1. 目标

让用户创建一个"群"，把多个已配置的智能体（可跨后端，如 CodeBuddy + Claude Code + Codex）加进去，然后就同一话题进行**多智能体辩论式群聊**：

- 有一个**协调器**（一个被指定为主持人的普通智能体）控场，决定下一个该谁发言、给它什么指令；
- 成员**同轮并行、轮次间共享上下文**——每个成员发言时能看到其他成员此前的发言文本；
- **人类可随时介入**：@ 某成员或打断纠偏，立即抢占；
- 循环在协调器发出结束信号 / 达到最大轮数 / 人类打断时停止。

一句话：**"中心协调 + AI 决定发言者 + 人类可随时介入"的混合模式**，像真实团队开会。

## 2. 核心决策（已定稿）

| # | 决策点 | 结论 |
|---|---|---|
| 1 | 交互模型 | 广播并行 + 轮次间共享上下文（非纯并行、非轮流） |
| 2 | 轮次推进 | 协调器控场，AI 决定发言者 |
| 3 | 协调器形态 | **就是一个普通智能体**，被指定为"主持人" |
| 4 | 路由表达 | **结构化标签**（复用 `internal/askquestion` 模式），解析失败回退轮转 |
| 5 | 群与现有会话 | **只能在新建群时添加成员，现有会话不可升级** |
| 6 | 成员变动 | 创建后**可增删成员**；新增成员首次发言由系统注入群历史"补课" |
| 7 | 停止条件 | **协调器结束信号 + 最大轮数兜底 + 用户随时打断**（三合一） |
| 8 | 成员连接 | 每个成员**一条独立持久连接**，复用现有 5 分钟空闲回收 + resume/load 恢复 |
| 9 | 成员可见性 | 成员间**只交换发言文本**；工具调用、深度思考不进入他人上下文 |
| 10 | 上下文拼装 | **增量注入**——只补"上次它发言后群时间线新增的内容" |
| 11 | 数据存储 | **单一数据源**：消息只存一份，存于**群会话**；成员只是**连接绑定** |
| 12 | 编排位置 | **新增独立编排器** `internal/service/group_orchestrator.go`；`run_turn` / ACP 池 / runner 尽量不动 |
| 13 | 协调器可见性 | 协调器输出**可见，特殊样式** |
| 14 | 人类介入 | **抢占**——立即停当前发言，优先处理人类输入 |
| 15 | 后端混合 | **允许任意混合**；群会话 `backend` 列用协调器后端填充 |
| 16 | 成员绑定 | 每个成员一条**隐藏的 `chat_sessions` 行**（`session_type='group_member'`） |
| 17 | 协调器指令 | **注入**给成员，作为**高优先级指令** |
| 18 | 异常可见性 | 异常终止时插**可见系统消息**，保留已有发言 |
| 19 | 花名册 UI | **顶部横向头像条**，发言中高亮 |
| 20 | 测试 | **两层**：单测用注入式假 run_turn，E2E 用 `acp-mock` |

## 3. 架构

三层：

```
┌─────────────────────────────────────────────┐
│  群会话层：chat_sessions (session_type=group) │
│  —— 独占时间线；title/pinned/未读/摘要复用     │
├─────────────────────────────────────────────┤
│  编排器层：internal/service/group_orchestrator.go │
│  —— 主循环：协调器 → 路由 → 成员发言 → 循环     │
├─────────────────────────────────────────────┤
│  执行层：现有 run_turn.go（唯一改动：时间线解耦）│
└─────────────────────────────────────────────┘
```

**关键杠杆**：成员是一条真实的 `chat_sessions` 行（`session_type='group_member'`）。因此它天然携带 `agent_id` / `transport` / `model` / `external_session_id` / `auto_approve` / `context_state`——**正是"连接绑定"所需的一切**。于是：

- `ACP 连接池按 session id 键控`（`internal/ai/acp_pool.go:185`）**零改动**——池照旧按键控，只不过那个 key 是成员行 id；
- `external_session_id` 查询（`internal/service/chat.go:2791`）**零改动**；
- `transport` 解析（`run_turn.go:236`）**零改动**；
- 5 分钟空闲回收、resume/load 恢复、CLI `--resume` **全部自动继承**。

唯一代价：会话列表要过滤掉 `group_member` 行。

### 3.1 run_turn 的时间线解耦（唯一的内核改动）

现状：一次回合把消息写进 `spec.SessionID` 指向的会话（`run_turn.go:277`），并把 `stream_start` 广播到该会话（`:300`）。

群聊需要"**连接解析走成员行，消息写入与广播走群行**"。改动：

- `TurnSpec` 增加可选字段 `TimelineSessionID string`；缺省时等于 `SessionID`（保证既有调用方行为不变）。
- `run_turn.go` 中两处使用 `spec.SessionID` 作为"时间线"的地方改用 `effectiveTimelineSessionID()`：
  - 落库：`AddChatMessage(..., sessionID, ...)`（`:277`）
  - 广播：`stream_start` 的 `EmitToSession`（`:300`）
- 连接解析仍用 `spec.SessionID`（= 成员行 id）。

改动集中在两处，且默认值保证向后兼容。

## 4. 数据模型

### 4.1 群会话

复用 `chat_sessions`，`session_type='group'`。

- `backend`：**NOT NULL**，用协调器成员的后端填充（约定值）。
- `agent_id`：填协调器成员对应的 agent id（便于列表展示）。
- 其余列（`title` / `pinned` / `sort_order` / `archived` / `last_read_at` / `context_state`）照常使用——**未读、摘要、归档、置顶全部复用现有机制**。

### 4.2 成员会话

`chat_sessions` 行，`session_type='group_member'`。

- `agent_id` / `backend` / `transport` / `model` / `external_session_id` / `auto_approve` / `context_state` 承载连接与配置。
- 需要新增一列指向所属群：**`group_id TEXT DEFAULT ''`**（群会话 id；非成员为空）。
- `context_state` 的 JSON 里额外存**增量注入游标** `seen_cursor`（该成员上次发言时群时间线的最大消息 id）——**零新表**。

### 4.3 群时间线归属

`chat_history` 新增一列 **`agent_id TEXT DEFAULT ''`**：

- 成员发言写入群会话时，`agent_id` = 该成员的 agent id（前端据此渲染头像/名字）。
- 协调器发言写入时，`agent_id` = 协调器的 agent id，并以**消息级标记**区分"这是协调器发言"（见 §5.4）。
- 用户消息 `agent_id=''`。
- 普通单会话消息 `agent_id` 留空（或填会话 agent），不影响既有查询。

**消息只存一份**——不存在第二份副本，从根上消除一致性问题。

### 4.4 成员表？

**不新建成员表**。成员即隐藏 session 行；花名册通过 `SELECT ... FROM chat_sessions WHERE group_id = ? AND session_type='group_member'` 得到。成员的**展示名/颜色/角色说明**存 `context_state` JSON 或复用 `title` 字段（title 存成员在群内的展示名）。

## 5. 主循环（编排器）

### 5.1 伪流程

```
用户消息 → 写入群时间线(agent_id='') → 广播到群
  ↓
轮次 r = 1..MaxRounds：
  1. 协调器发言
     - 增量注入：群时间线中 id > 协调器.seen_cursor 的消息文本
     - run_turn(连接=协调器成员行, 时间线=群会话)
     - 输出写入群时间线（特殊样式），更新协调器.seen_cursor
     - 解析路由标签
  2. 路由
     - <clawbench-speaker>B</...> + 指令 → 下一发言者 = B
     - 结束信号 → break
     - 解析失败 → 回退规则轮转（下一个未发言成员）；连续失败 2 次 → 自动收尾
  3. 被选中成员发言
     - 增量注入：群时间线中 id > 该成员.seen_cursor 的消息文本
       + 协调器指令（高优先级）
     - run_turn(连接=该成员行, 时间线=群会话)
     - 输出写入群时间线(agent_id=该成员)，更新 seen_cursor
  4. 检查抢占 / 打断 / 达 MaxRounds
  ↓
（可选）协调器做最终汇总
```

### 5.2 增量注入

- 游标：每个成员 `seen_cursor` = 上次发言时群时间线的最大消息 id。
- 注入内容 = 群时间线中 `id > seen_cursor` 的**发言文本**（不含工具调用、深度思考——按决策 #9）。
- 协调器指令单独作为高优先级段注入（决策 #17）。
- 游标存在成员行 `context_state` JSON（决策 #10 落地）。

### 5.3 路由标签

仿 `internal/askquestion/` 建独立叶子包（暂定 `internal/grouprouting/`）：

- 协调器系统提示注入格式说明，要求输出 `<clawbench-speaker>成员名</clawbench-speaker>` + 指令文本。
- 结束信号：`<clawbench-group-end/>`（或标签内特定关键字）。
- **契约**：检测即解析；不可解析时**不剥离**标签（与 askquestion 的"绝不丢内容"一致，保留原文给用户看），并回退轮转。
- Go 实现与前端镜像 + parity corpus（若前端也需解析则建镜像，否则仅 Go）。

### 5.4 协调器发言的特殊样式

消息级标记方案（择一，实现时定）：

- 在 `chat_history.content` 的 JSON 信封里加 `meta.role="coordinator"`；或
- 复用 `agent_id` 与一个群级 `coordinator_agent_id` 比对（若协调器即某成员，则"agent_id == coordinator_agent_id 且内容含路由标签"视为协调器发言）。

前端据标记渲染居中特殊样式。

### 5.5 抢占

- 人类 @ 成员 / 打断 → 取消当前成员的 runner（复用 `CancelSession`，`session_runtime.go:770`）。
- 人类消息写入群时间线。
- 协调器基于新状态重新选人。
- 被中断的成员发言按现有 `cancel` 路径标记为"中断"而非错误。

## 6. 错误处理与降级

**贯穿原则：任何单个成员的失败不终止整个群。**

| 失败点 | 降级 |
|---|---|
| 路由标签解析失败 | 回退规则轮转；连续失败 2 次自动收尾 |
| 协调器跑挂（后端错误/超时） | 可见错误 + 回退轮转继续；再失败终止群回合 |
| 某成员发言失败 | 该成员标"本轮失败"，时间线记可见错误；循环继续，协调器可改选 |
| 成员连接无法恢复（resume/load 都失败且有历史） | 标"离线"，跳过，协调器不再选它；用户可手动重连 |
| 达到最大轮数 | 强制停（可选让协调器做最终汇总） |
| 用户抢占 | 取消当前 runner，人类消息进时间线，协调器重选 |
| 整个群回合崩溃 | 已有发言全部保留（已落库），群回到空闲 |

异常终止时插**可见系统消息**（决策 #18），复用协调器特殊样式。

## 7. 界面

- **会话列表**：群与普通会话并列，群图标标识；`group_member` 行被过滤（防泄漏到侧边栏）。
- **群时间线**：成员发言气泡显示头像 + 名字 + 后端标识（复用 `AgentIcon` / `getAgentName`）；协调器发言居中特殊样式。
- **顶部横向头像条**（决策 #19）：群会话头部一行成员头像，**发言中高亮/脉动**；点击可查看成员详情。
- **加成员**：从 `GET /api/agents` 选已配置 agent，可配角色说明。
- **人类介入**：输入框支持 @ 成员（走已有 `@` 提及模式），发送即抢占。

## 8. API

新增（需同步 `internal/api/openapi.yaml`）：

- `POST /api/group/create` — 建群（标题 + 成员 agent 列表 + 协调器指定）。
- `POST /api/group/members` — 增成员。
- `DELETE /api/group/members` — 删成员。
- 群消息发送：复用 `/api/ai/chat`（携带群会话 id），由编排器接管；或新增 `/api/group/chat`。
- 群回合取消：复用现有 cancel。

（字段名必须从 handler 代码抄，禁止望文生义。）

## 9. 测试

### 9.1 单元测试（Go）

- `group_orchestrator` 循环逻辑：注入**假 run_turn**（脚本化返回），断言路由解析、增量游标、结束信号、最大轮数兜底、抢占、成员失败不终止群。
- 路由标签解析器（`internal/grouprouting/`）：独立叶子包 + parity corpus。
- `run_turn` 的 `TimelineSessionID`：断言"连接用成员、落库/广播用群"，且缺省时行为不变（向后兼容）。

### 9.2 单元测试（前端）

- `agent_id` → 气泡头像/名字渲染。
- 协调器特殊样式。
- 顶部头像条状态（空闲/发言中/离线）。

### 9.3 E2E（Playwright + `acp-mock`）

- 建群 → 加 2 个成员 → 发一条消息 → 断言多成员气泡按序出现 + 协调器消息可见 + 人类抢占生效。
- 跨层接线（连接用成员、落库/广播用群）必须用真后端验证——单测 mock 会掩盖接线错误。

### 9.4 守卫测试

- 会话列表必须过滤 `group_member`（防成员泄漏）。
- 群消息的 `agent_id` 非空。

## 10. 开放问题（实现时定）

1. **最大轮数默认值**（暂定 10）与是否可配置。
2. **协调器是否也做最终汇总**（暂定可选，由结束标签控制）。
3. **`content` 信封中协调器标记的确切字段名**（`meta.role` vs 其他）。
4. **前端是否也需解析路由标签**（若仅展示则否，仅 Go 解析）。
5. **`group_id` 索引**与成员查询性能。
6. **群标题自动生成**（复用 auto-title 还是协调器生成）。
7. **成员展示名的存放**（`title` vs `context_state`）。
8. **多群并发**时每个成员的连接是否串扰（每成员独立 session id，预期无串扰，需 E2E 验证）。

## 11. 关键文件索引

| 关注点 | 文件:行 |
|---|---|
| 会话表 schema | `internal/service/database.go:577` |
| 消息表 schema | `internal/service/database.go:563` |
| 会话结构体 | `internal/model/chat.go:274` |
| 消息结构体 | `internal/model/chat.go:124` |
| 会话 agent 解析 | `internal/service/agent_resolve.go:19` |
| 回合编排 | `internal/service/run_turn.go:220` |
| 请求构造 | `internal/service/chat_request.go:42` |
| 消息落库 | `internal/service/chat.go:624` |
| 分页读取 | `internal/service/chat.go:70` |
| 未读查询 | `internal/service/chat.go:1187` |
| 会话列表 | `internal/service/chat.go:1229` |
| runner registry | `internal/service/session_runner.go:46` |
| ACP 连接池 | `internal/ai/acp_pool.go:185` |
| ACP 空闲回收 | `internal/ai/acp_pool.go:199` |
| ACP 恢复 | `internal/ai/acp_conn_lifecycle.go:224` |
| WS 订阅 | `internal/ws/stream_hub.go:38` |
| 前端订阅 | `web/src/composables/useChatStream.ts:86` |
| 消息列表渲染 | `web/src/components/chat/ChatMessageList.vue` |
| 消息气泡 | `web/src/components/chat/ChatMessageItem.vue` |
| 团队面板先例 | `web/src/components/chat/TeamPanel.vue` |
| ask-question 先例 | `internal/askquestion/` |
