# AI 群聊设计（Group Chat）

日期：2026-10-04
状态：设计定稿（待实现）

## 1. 目标

让用户创建一个"群"，把多个已配置的智能体（可跨后端，如 CodeBuddy + Claude Code + Codex）加进去，然后就同一话题进行**多智能体辩论式群聊**：

- 有一个**主持人**（一个被指定为主持人、同时是群成员的普通智能体）控场，决定下一个该谁发言、给它什么指令；
- 成员**轮次间共享上下文**——每个成员发言时能看到其他成员此前的发言文本；**v1 一轮内按顺序依次发言**（后者可见前者本轮内容），同轮并行留作 v2；
- **人类可随时介入**：@ 某成员或打断纠偏，立即抢占；
- 循环在主持人发出结束信号 / 达到最大轮数 / 人类打断时停止；
- **建群极简**：只选主持人，其余成员进群后手动添加。

一句话：**"中心协调（主持人）+ AI 决定发言者 + 人类可随时介入"的混合模式**，像真实团队开会。

## 2. 核心决策（已定稿）

| # | 决策点 | 结论 |
|---|---|---|
| 1 | 交互模型 | 广播并行 + 轮次间共享上下文（非纯并行、非轮流） |
| 2 | 轮次推进 | 主持人控场，AI 决定发言者 |
| 3 | 主持人形态 | **就是一个普通智能体**，被指定为"主持人" |
| 4 | 路由表达 | **结构化标签**（复用 `internal/askquestion` 模式），解析失败回退轮转 |
| 5 | 群与现有会话 | **现有会话不可升级为群**（群只能新建） |
| 6 | 成员变动 | 创建后**可增删成员**（建群时只定主持人）；新增成员首次发言由系统注入群历史"补课" |
| 7 | 停止条件 | **主持人结束信号 + 最大轮数兜底 + 用户随时打断**（三合一） |
| 8 | 成员连接 | 每个成员**一条独立持久连接**，复用现有 5 分钟空闲回收 + resume/load 恢复 |
| 9 | 成员可见性 | 成员间**只交换发言文本**；工具调用、深度思考不进入他人上下文 |
| 10 | 上下文拼装 | **增量注入**——只补"上次它发言后群时间线新增的内容" |
| 11 | 数据存储 | **单一数据源**：消息只存一份，存于**群会话**；成员只是**连接绑定** |
| 12 | 编排位置 | **新增独立编排器** `internal/service/group_orchestrator.go`；`run_turn` / ACP 池 / runner 尽量不动 |
| 13 | 主持人可见性 | 主持人输出**可见，特殊样式** |
| 14 | 人类介入 | **抢占**——立即停当前发言，优先处理人类输入 |
| 15 | 后端混合 | **允许任意混合**；群会话 `backend` 列用主持人后端填充 |
| 16 | 成员绑定 | 每个成员一条**隐藏的 `chat_sessions` 行**（`session_type='group_member'`） |
| 17 | 主持人指令 | **注入**给成员，作为**高优先级指令** |
| 18 | 异常可见性 | 异常终止时插**可见系统消息**，保留已有发言 |
| 19 | 花名册 UI | **顶部横向头像条**，发言中高亮 |
| 20 | 测试 | **两层**：单测用注入式假 run_turn，E2E 用 `acp-mock` |
| 21 | 多发言者 | 主持人**可一次点名多个成员**（`<clawbench-speaker>A,B,C</...>`）；v1 按顺序依次发言 |
| 22 | 同轮执行（v1） | **顺序执行**——被点名者按标签顺序**依次**发言，后者能看到前者本轮的发言。**放弃同轮并行**（见 §12 评审勘误 C1：并行需按消息 id 重写流式行定位，风险高） |
| 23 | 注入游标语义 | 游标 = 该成员**上次发言时**的群时间线高水位；注入 = `id > 游标` **且作者 ≠ 自己**。顺序模式下无并发竞态，游标即"发言前的最大 id"（见 §5.2） |
| 24 | 建群入口 | 会话列表头**独立"建群"按钮**（非 `+` 二选一、非长按） |
| 25 | 建群流程 | **极简**：只选主持人；成员建群后再手动添加 |
| 26 | 主持人身份 | **主持人 = 成员之一（兼主持人）**；"只有主持人的群"退化为单聊，仍可用 |
| 27 | 建群后加成员 | 头像条尾部 `+` → **多选抽屉**（扩展 `AgentSelectorDrawer` 支持多选） |
| 28 | 群标题 | **建群时不提供输入框**（抽屉只留成员列表）；后端用**主持人名**占位（"XX 的群聊"），首条消息后 auto-title 接管 |
| 29 | 删成员 | 历史发言**保留并标记"已离场"**（灰显，与"离线"共用视觉）；主持人不再选它 |
| 30 | 成员角色 | **不做群内角色设置**，成员人设完全来自智能体自身的描述与提示词 |
| 31 | 最大轮数 | **默认 10，可在群设置里改**（存群行 `context_state`） |
| 32 | 最终汇总 | **结束时主持人产出总结**（结束信号或达上限都产出） |
| 33 | 指定主持人方式 | **已勾选行内的「主持」胶囊按钮**（皇冠 + 文字，位于原默认星标位置）；**勾选第一个成员时自动成为主持人**（可改，同一时刻仅一个） |
| 34 | 成员管理容器 | **`BottomSheet` 抽屉** + 已离场成员**灰显并标注"已离场"** |
| 35 | 路由标签展示 | **后端保留标签，前端解析成路由卡片**（"主持人 → A、B"） |
| 36 | 成员展示名 | 存成员行的 **`title` 列**（复用现有字段） |
| 37 | 会话上限计入 | **群计入、成员不计**：**4 处** COUNT 改 `session_type IN ('chat','group')`（`chat.go:2046`、`continue_conversation.go:161,430`、`handler/session_resume.go:358`）；`POST /api/group/create` 也须过上限门 |
| 38 | 群设置位置 | **成员管理 BottomSheet 内**（不进设置页） |

## 3. 架构

三层：

```
┌─────────────────────────────────────────────┐
│  群会话层：chat_sessions (session_type=group) │
│  —— 独占时间线；title/pinned/未读/摘要复用     │
├─────────────────────────────────────────────┤
│  编排器层：internal/service/group_orchestrator.go │
│  —— 主循环：主持人 → 路由 → 成员发言 → 循环     │
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

**⚠️ 实现前必读：`SessionID` 在 executor 里身兼两职，不是"两处"能改完的。**

现状：`runTurnStart` 把消息写进 `spec.SessionID`（`run_turn.go:277`）、把 `stream_start` 广播到该会话（`:300`），然后 `RunConfig.SessionID = spec.SessionID`（`:309`）。此后 **`SessionExecutor` 全程用 `e.cfg.SessionID` 同时承担两种语义**：

- **连接/成员语义**（必须用成员行 id）：`captureExternalSessionID` 写 `external_session_id`（`session_executor.go:851-853`）、`GetCachedStateByClawbenchSID`（`:1403`）、`GetSessionTransport`（`:1412`）、`GetSessionModel`（`:1419`）、`GetExternalSessionID`（`:1423`）、`GetAndClearCancelReason`（`:830`）、`PatchContextStateMerge`（`:986`）。
- **时间线语义**（必须用群会话 id）：`AddChatMessage`（`run_turn.go:277`）、`UpdateStreamingMessage` / `FinalizeStreamingMessage` / `FinalizeCancelledStreamingMessage`（`:514,1168,1626`）、`persistThinkingToDB` / `AppendThinkingSegment`（`:1054,1249`）、tool-call 持久化（`:914,968`）、`CreateStreamingMessage`（`:1183`）、`triggerChatSummarization`（`:1644`）。
- **⚠️ `UpdateLastRead`（`:1143`）例外——保持 `cfg.SessionID`（成员行 id），不是时间线 id**（评审三轮 I10 / 五轮 R-4）：改时间线 id 会让任一成员回合把**群**标记已读，群徽章永不亮。此点与上面的时间线语义相反，勿混。

因此**必须引入第二个字段**，而不是复用 `SessionID`：

- `TurnSpec` 增加 `TimelineSessionID string`；`RunConfig` 增加 `TimelineSessionID string`。两者缺省（空）时等于对应的 `SessionID`——既有调用方行为完全不变（`effectiveTimelineSessionID()` 返回 `SessionID`）。
- **连接语义**的全部调用点继续用 `cfg.SessionID`（= 成员行 id）。
- **时间线语义**的全部调用点改用 `cfg.effectiveTimelineSessionID()`（缺省 = `cfg.SessionID`）。
- `runTurnStart` 里两处（`:277` 落库、`:300` 广播）也改用 `spec.effectiveTimelineSessionID()`。
- **WS 广播**：`emitStreamEvent`（`:357`）用**时间线 id**（群），因为前端订阅的是群会话；成员流式必须广播到群。
- **`activeStreams` 键（`:349,369`）保持 `cfg.SessionID`（成员行）**——**不要**改成时间线 id（见 §12 C6：它是服务端注册表，供 `FlushStreamingNow`/`WaitStreamsDrained`，与前端订阅无关；改群 id 会让并发成员互相覆盖、优雅关停丢尾部）。

**实现纪律**：改完必须逐一核对 `session_executor.go` 里每个 `e.cfg.SessionID` 的归属（上表两类），漏改会把成员消息写进成员会话（用户看不到）或把连接状态写到群会话（成员 resume 失效）。这是本设计里**最容易出错、最需要测试钉死**的一处。

向后兼容由"缺省=同值"保证：单会话路径 `TimelineSessionID == SessionID`，所有调用点等价于现状。

## 4. 数据模型

### 4.1 群会话

复用 `chat_sessions`，`session_type='group'`。

- `backend`：**NOT NULL**，用主持人成员的后端填充（约定值）。
- `agent_id`：填主持人成员对应的 agent id（便于列表展示）。
- 其余列（`title` / `pinned` / `sort_order` / `archived` / `last_read_at` / `context_state`）照常使用——**未读、摘要、归档、置顶全部复用现有机制**。

### 4.2 成员会话

`chat_sessions` 行，`session_type='group_member'`。

- `agent_id` / `backend` / `transport` / `model` / `external_session_id` / `auto_approve` / `context_state` 承载连接与配置。
- 需要新增一列指向所属群：**`group_id TEXT DEFAULT ''`**（群会话 id；非成员为空）。
- `context_state` 的 JSON 里额外存**增量注入游标** `seen_cursor`（该成员上次发言时群时间线的最大消息 id）——**零新表**。

**⚠️ 成员会话没有 `chat_history` 行 → resume 判定会失效（实现前必读）。**

消息只存群时间线，成员会话行**没有任何 chat_history 记录**。但 `BuildChatRequest` 用 `SessionHasAssistant(sessionID)`（`chat_request.go:62` → `chat.go:2497`）决定 `resume`；成员行没有 assistant 消息 ⇒ `resume=false` ⇒ **成员每轮都被当成全新会话，原生记忆永远建立不起来**，本设计的"独立持久连接 + 增量注入"全部落空。

修法见 **§12 C5 / §12.1 N1**（**以此为准**，早期"强制置 resume=true"的说法已作废）：成员回合在 `BuildChatRequest` 之后调 `applyMemberResumeOverrides`，按 **CLI/ACP 分流**——ACP 的 `req.SessionID` 是**连接池键**必须保持成员行 id，仅覆盖 `Resume`/`HasConversationHistory`/`AssistantMessageCount`；CLI 才把 `SessionID` 换成 `external_session_id` 并 `Resume=true`。判据统一为"成员行 `external_session_id != \"\"`"。

### 4.3 群时间线归属

`chat_history` 新增一列 **`agent_id TEXT DEFAULT ''`**：

- **存"发言人成员行 id"（不是 agent id）**（评审 N4）：同一 agent 可被加进同一群两次，若按 agent id 归属则两条成员无法区分，且增量注入的"作者 ≠ 自己"过滤会失效。列名沿用 `agent_id`，语义为**发言人的成员会话行 id**；前端用群成员列表（头像条已加载）把行 id 解析为 agent 头像/名字。
- 主持人发言写入时，`agent_id` = 主持人的**成员行 id**；前端据 **`agent_id == 主持人成员行 id`** 判定"主持人发言"（见 §5.4，**不引入 content 信封标记**）。
- 用户消息 `agent_id=''`。
- **普通单会话消息 `agent_id` 一律留空**（不要填会话 id——非成员行 id 的值会污染前端"行 id → agent"映射）。
- **增量注入的"作者 ≠ 自己"按该列（成员行 id）比较**，与 `buildInjectionText(..., self, ...)` 的 `self` 参数同源（`self` 传成员行 id）。
- **⚠️ 与 `chat_metadata.agent_id` 同名不同义**：后者是真实 agent id（`database.go:1679` 从 `chat_sessions.agent_id` 填）。两列语义不同，勿混用。

**消息只存一份**——不存在第二份副本，从根上消除一致性问题。

### 4.4 成员表？

**不新建成员表**。成员即隐藏 session 行；花名册通过 `SELECT ... FROM chat_sessions WHERE group_id = ? AND session_type='group_member'` 得到。成员的**展示名存成员行的 `title` 列**（决策 #36；成员行无 chat_history，auto-title 永不触发，故 `title` 安全）。建成员时由 agent 名派生写入。

**主持人成员行 id 存群行 `context_state`**（`host_member_id`）——**读取须用专门的 reader**（`ContextState`（`chat.go:2200`）只认 `Mode`/`ThinkingEffort`/`Usage`，不含 `maxRounds`/`host_member_id`）。需新增 `GetGroupHostMember(groupID)` / `GetGroupMaxRounds(groupID)`（用 `json_extract` 或独立解析），见 §10 #31 与计划 E1。

## 5. 主循环（编排器）

### 5.1 伪流程

```
用户消息 → 写入群时间线(agent_id='') → 广播到群
  ↓
轮次 r = 1..MaxRounds：
  1. 主持人发言
     - 增量注入：群时间线中 id > 主持人.seen_cursor 的消息文本（作者≠自己）
     - run_turn(连接=主持人成员行, 时间线=群会话)
     - 输出写入群时间线（特殊样式），更新主持人.seen_cursor
     - 解析路由标签
  2. 路由
     - <clawbench-speaker>A,B,C</...> + 指令 → 本轮发言者 = [A,B,C]（有序）
     - 结束信号 → break（**该轮主持人的输出已含最终汇总**，见 §5.3/决策 #32）
     - 解析失败 → 回退规则轮转（下一个未发言成员）；连续失败 2 次 → 收尾
  3. 被点名成员**按顺序依次发言**（v1）
     - 对每个成员（按标签顺序）：
       - 记录其发言前高水位 H = 当前群时间线最大消息 id
       - 增量注入 = 群时间线中 id > 该成员.seen_cursor 且作者≠自己 的消息文本
         + 主持人指令（高优先级）
       - run_turn(连接=该成员行, 时间线=群会话)
       - 输出写入群时间线(agent_id=该成员)，更新该成员.seen_cursor = H
     - 后发言的成员能看到先发言者本轮的内容（顺序执行的自然结果）
  4. 检查抢占 / 打断 / 达 MaxRounds（默认 10，可配，决策 #31）
  ↓
若因**达 MaxRounds** 退出（非结束信号）：**再跑一次主持人回合**产出汇总
（提示词变体：要求只写结论、不再输出路由标签，避免重入循环）
```

**汇总语义（决策 #32，评审三轮 C-2）**：
- **结束信号路径**：主持人发出 `<clawbench-group-end/>` 的那一轮，其输出**本身就是最终汇总**（B2 提示词要求"结束标签之后写结论"）——**不再额外跑主持人**，避免重复汇总。
- **达上限路径**：因轮数耗尽被迫退出，主持人**尚未**产出结论 → **额外跑一次主持人回合**（用"只写结论"的提示词变体，且**不解析其路由标签**，防重入循环）。
- 两条路径**恰好各产出一次**汇总。设计 §6 表格中"可选让主持人汇总"的旧措辞作废（汇总在两条路径都产出）。

**注**：v1 **顺序执行**。被点名者依次发言，后者能看到前者本轮发言——这正好让"B 回应 A"成为可能，无需主持人跨轮排序。同轮并行是 v2 候选，需先完成 §12 C1 的流式行按 id 重写。

### 5.2 增量注入

- 游标语义（决策 #23，顺序模式）：成员 `seen_cursor` = 该成员**本次发言前**的群时间线高水位 `H`。这样：
  - 本轮中先发言的同伴内容（id > H）**不会**被跳过——后续轮次仍会注入给该成员；
  - 自己刚说过的话（作者=自己）**不会**被重放。
- 注入内容 = 群时间线中 `id > seen_cursor` **且 `agent_id ≠ 自己`** 的**发言文本**（不含工具调用、深度思考——决策 #9）。
- 主持人指令单独作为高优先级段注入（决策 #17）。
- 游标存在成员行 `context_state` JSON（决策 #10 落地）。
- **顺序模式下无并发竞态**：同一时刻只有一个成员在发言，游标取"发言前最大 id"即可。若未来恢复并行（v2），游标须改为"本轮开始高水位 + 作者过滤"（见 §12 C1 与决策 #23 原并行语义）。

### 5.3 路由标签

仿 `internal/askquestion/` 建独立叶子包（`internal/grouprouting/`）：

- 主持人系统提示注入格式说明，要求输出 `<clawbench-speaker>A,B,C</clawbench-speaker>` + 指令文本（逗号分隔多个成员）。
- 结束信号：`<clawbench-group-end/>`；**结束标签之后的文本即最终汇总**（决策 #32，评审三轮 C-2）。
- **契约**：检测即解析；不可解析时**不剥离**标签（与 askquestion 的"绝不丢内容"一致，保留原文给用户看），并回退轮转。
- **后端保留标签**（不剥离），**前端解析成路由卡片**（决策 #35）——故 Go 实现与前端**镜像** + parity corpus（与 askquestion 同款双向固化）。

**前端卡片契约（评审三轮 #35）**：
- **可解析**：把**标签 span**替换为卡片（"主持人 → A、B" chips）；**标签之后的指令文本保留显示一次**（卡片里可含指令，但正文不得重复渲染同一段——即卡片替换范围包含指令文本，或卡片不含指令而正文保留，二选一，实现时统一，**不得两处都显示**）。
- **不可解析**：**不剥离**，原样显示（含标签），与后端契约一致——**绝不因解析失败丢内容**。

### 5.4 主持人发言的特殊样式

**决策（#35/#N6）**：主持人发言与成员发言的区分**靠 `agent_id == 主持人成员行 id`**（群的主持人成员行 id 已知）。**不引入 content 信封标记**（N6：content 由 executor 的 `buildContentJSON` 构建，编排器无注入点）。

前端对主持人气泡渲染**居中特殊样式**，并把其中的路由标签**解析成"主持人 → A、B"卡片**（决策 #35）。标签解析逻辑与后端 `internal/grouprouting` 镜像。

### 5.5 抢占

- 人类 @ 成员 / 打断 → 取消当前成员回合。**用成员感知的 `cancelMemberTurn(memberRowID)`**（评审 I5：直接 `CancelSession(memberRowID)` 会给隐藏成员发终态 `session_update` + push 并 finalize 其不存在的历史）。**不**复用 `CancelSession`。
- **群级停止（评审 N2）**：前端停止按钮对 `groupID` 发 cancel。编排器必须让每个成员回合的 `TurnSpec.Ctx` **派生自编排器自己的可取消 ctx**，这样 `CancelSession(groupID)`（或编排器的 cancel）能传播到当前成员回合；否则停止只取消了群占位 runner，成员回合照跑。
- 人类消息写入群时间线。
- 主持人基于新状态重新选人。
- 被中断的成员发言按现有 `cancel` 路径标记为"中断"而非错误。

### 5.6 顺序执行的流式行不变量（评审 C1 残留）

**必须显式保证**：编排器**串行 await** 每个成员的 `runTurn` 完全结束（含 Finalize）后，才启动下一个成员/主持人回合。这是 `UpdateStreamingMessage` 的 `WHERE session_id=group AND streaming=1 ORDER BY id DESC LIMIT 1` 能命中正确行的**前提**——若任何实现用第二个 goroutine 提前启动下一个发言者，延迟的 Finalize 会打到**下一个发言者的行**。

- **孤儿行清理**：若某成员回合 finalize 失败，群时间线会留下 `streaming=1` 孤儿行，且 `SetSessionRunning` **不会**清理它（`session_runtime.go:406-409`）。编排器需在群回合开始/结束时对群时间线做一次孤儿 finalize（参考 `finalizeOrphanedStreamingMessages`），否则重载时出现幽灵流式气泡。
- 这两条须各有一条测试钉住（串行 await 纪律 + 孤儿清理）。

## 6. 错误处理与降级

**贯穿原则：任何单个成员的失败不终止整个群。**

| 失败点 | 降级 |
|---|---|
| 路由标签解析失败 | 回退规则轮转；连续失败 2 次自动收尾 |
| 主持人跑挂（后端错误/超时） | 可见错误 + 回退轮转继续；再失败终止群回合 |
| 某成员发言失败 | 该成员标"本轮失败"，时间线记可见错误；循环继续，主持人可改选 |
| 成员连接无法恢复（resume/load 都失败且有历史） | 标"离线"，跳过，主持人不再选它；用户可手动重连 |
| 达到最大轮数 | 强制停，**并额外跑一次主持人回合产出汇总**（决策 #32，见 §5.1） |
| 用户抢占 | 取消当前成员回合（`cancelMemberTurn`），人类消息进时间线，主持人重选 |
| 整个群回合崩溃 | 已有发言全部保留（已落库），群回到空闲 |

异常终止时插**可见系统消息**（决策 #18），复用主持人特殊样式。

## 7. 界面

- **会话列表**：群与普通会话并列，群行以 **`Users` 图标 + 成员头像堆叠**标识（meta 行替换掉单聊的 `[AgentIcon] 主持人名`——群行的 `agentId` 是主持人，直接显示会被误读成单聊）；堆叠只显示**在群**成员（离场者不进列表，完整名单见成员管理抽屉），**最多 4 个**、首个完整可见后续压在其后、**不显示 `+N`**（超出者不渲染，hover tooltip 列出全部）；`group_member` 行被过滤（防泄漏到侧边栏）。成员预览由 `GET /api/ai/sessions`（及 `/overview`）随列表**一次批量**返回（`groupMembers`，非群会话省略该键）。
- **群时间线**：成员发言气泡显示头像 + 名字 + 后端标识（复用 `AgentIcon` / `getAgentName`）；主持人发言居中特殊样式。
  - **⚠️ 实时归属缺口（评审 N3）**：`stream_start` 的 payload 只有 `message_id`（`ws/stream_hub.go:373`），`simpleTextPayload`（`:345`）也不带 agent id。所以**流式过程中**前端无法知道当前气泡属于哪个成员——`agent_id` 只在消息落库后由 DB 读回。v1 方案二选一：(a) 在 `stream_start` 增加 `agent_id`（后端小改）；(b) 明确降级为"流式时用通用样式、重载后才显示发言人"。**v1 推荐 (a)**（改动小且体验完整）。
- **（v2）同轮并发气泡**：一轮内 N 个成员同时流式时的多锚点渲染——**v1 顺序轮次不需要**，见 §12。
- **顶部横向头像条**（决策 #19）：群会话头部一行成员头像，**发言中高亮/脉动**；点击可查看成员详情。
- **人类介入**：输入框支持 @ 成员（走已有 `@` 提及模式），发送即抢占。
- **主持人识别（评审三轮 I-5）**：前端需要"主持人成员行 id"来判定主持人气泡与设置。`GET /api/group/members` 的返回**须含 `isHost` 标志**（或新增 `GET /api/group/info` 返回 `{hostMemberId, maxRounds}`）。J1/K1 依赖此来源。

## 7.1 建群交互（决策 #24–#30）

### 入口

会话列表头（`SessionListHeader.vue`，`data-action="create"` 旁）新增一个**独立"建群"按钮**（群图标）。移动端抽屉与桌面侧栏共用同一 header，两处自然都有入口。

> 为什么不复用 `+`：`+` 是"新建单聊"的高频动作，改成二选一会拖慢最常见的操作；也不复用"长按 `+`"（与现有"长按=默认 agent 建会话"语义冲突）。

### 建群流程（一步到位，决策 #25/#33）

点击"建群" → 打开 `AgentSelectorDrawer` 的**建群模式**（多选）→ **成员与主持人一起选** → 点「创建群聊」→ 建群 + 建成员**一次完成**，直接切进该群。

- **一个列表完成两件事**：勾选 = 选成员；点行内「主持」**按钮** = 指定主持人。**不再有"先建群、后逐个加成员"**。
- **主持人 = 成员之一**（决策 #26）：主持控件只出现在**已勾选行**上（未勾选行不显示，避免"选了圈外人当主持"）。它是群里的第一个成员，既控场又参与发言。
- **首个勾选自动成为主持人、可改**（决策 #33）：勾选第一个成员时自动把它设为主持人（省一次点击、且"选完就能建"）；之后点其他行的「主持」按钮即改选，**同一时刻只有一个主持人**。取消勾选当前主持人 → 主持人清空；若此时仍有其他成员，下一个勾选会重新自动指定。一个成员都没勾选时无主持人，底部「创建群聊」按钮**禁用**。
- **主持控件放在原「默认星标」的位置**（决策 #33）：用**皇冠 + 「主持」文字**的胶囊按钮，而非不显眼的小圆点——用户必须一眼看出它是"选主持人"的动作。加成员模式下不显示该控件。
- **不提供群名输入框**（决策 #28）：抽屉只留成员列表；后端用主持人名占位（"XX 的群聊"），首条消息后 auto-title 接管。
- **成员不做群内角色设置**（决策 #30）：成员在群里的"人设"完全来自它作为智能体自身的描述与提示词。

**抽屉内三种行状态**（建群模式）：

| 状态 | 行右侧 |
|---|---|
| 未勾选 | 仅空复选框（不显示主持控件） |
| 已勾选、非主持人 | 复选框（已勾）+ **「主持」胶囊按钮**（可点，空心描边） |
| 已勾选、主持人 | 复选框（已勾）+ **「主持」胶囊按钮（高亮填充）** |

- 点**行体** = 勾选/取消；点**主持按钮** = 设为主持人（`@click.stop`，互不干扰）。
- 「加成员」模式（头像条 `+` 进入）复用同一多选组件，但**不显示主持控件**、已入群者灰显标注"已添加"。

**后端一次落库**：`POST /api/group/create` 扩展为接收 `memberAgentIds`（含主持人），**同一事务**创建群 + 全部成员；失败整批回滚，不留"只有主持人的半成品群"。

### 建群后加成员（决策 #27）

- **入口**：群会话顶部头像条**尾部一个 `+`**，点击打开多选 `AgentSelectorDrawer`（加成员模式），一次可勾多个、确认后批量加入。
- **扩展 `AgentSelectorDrawer` 支持多选**：加 `multiple` prop + `v-model` 数组 + checkbox 行 + 选中不自动关闭；单选模式（单聊入口）行为不变。列表行（`AgentIcon` + 名字 + 专业 + 后端标签 + 默认徽章）直接复用。
- **成员展示名/颜色**：展示名存成员行 **`title` 列**（决策 #36）；颜色复用 `AgentIcon` + `teamMemberColor.ts`（与 `TeamPanel.vue` 同款）。

### 删成员

- **入口**：成员管理面板（头像条 `+` 进入后的成员列表里可移除）。
- **容器**：**`BottomSheet` 抽屉**（决策 #34），与移动端现有抽屉一致；抽屉内同时承载**群设置**（最大轮数，决策 #31）。
- **历史发言保留并标记"已离场"**（决策 #29）：发言原样保留，头像灰显 + "已离场"标记；主持人不再选它。与"离线"状态（连接无法恢复）复用同一套视觉。
- 理由：群时间线是"会议纪要"，删掉某人发言会让后续"B 回应 A"失去上下文；完全不留痕又会让用户以为系统出 bug。

## 8. API

新增（需同步 `internal/api/openapi.yaml`）：

- `POST /api/group/create` — 建群（`hostAgentId` + `memberAgentIds[]`，**同一事务**创建群与全部成员；须过会话上限门，决策 #37）。
- `POST /api/group/members` — 增成员（批量 `{groupId, agentIds:[]}`）。
- `DELETE /api/group/members` — 删成员。
- `GET /api/group/members?groupId=` — 列成员，**含 `isHost` 标志**（决策 #34/I-5；J1/K1 依赖它识别主持人）。
- `PATCH /api/group/settings` — 群设置（最大轮数，决策 #31）。
- 群消息发送：**复用 `/api/ai/chat`**——`AIChat` POST 分支检测到群会话（`session_type='group'`）即委派编排器 `RunGroupTurn`，**前端不改**（决策见 §12.2 C-3；**不新增** `/api/group/chat`，避免双入口）。
- 群回合取消：复用现有 cancel（须经编排器 ctx 传播，见 §5.5）。

（字段名必须从 handler 代码抄，禁止望文生义。）

## 9. 测试

### 9.1 单元测试（Go）

- `group_orchestrator` 循环逻辑：注入**假 run_turn**（脚本化返回），断言路由解析、增量游标、结束信号、最大轮数兜底、抢占、成员失败不终止群。
- **顺序多发言者（v1）**：主持人点名多个成员时，断言**按标签顺序依次**执行、后者注入能看到前者本轮发言、各自 `seen_cursor` 更新为发言前高水位。
- **（v2）多发言者并行**：断言同轮并发执行、注入高水位 `H` 一致——**v1 不适用**。
- 路由标签解析器（`internal/grouprouting/`）：独立叶子包 + parity corpus（含多成员逗号分隔、结束信号、畸形输入）。
- `run_turn` 的 `TimelineSessionID`：断言"连接用成员、落库/广播用群"，且缺省时行为不变（向后兼容）。

### 9.2 单元测试（前端）

- `agent_id`（成员行 id）→ 气泡头像/名字渲染；映射缺失时降级通用样式。
- 主持人特殊样式（`agentId == 主持人成员行 id`）。
- **路由卡片解析**（决策 #35）：前端解析 `<clawbench-speaker>` 成"主持人 → A、B"卡片；与 Go 镜像由 parity corpus 固化。
- 顶部头像条状态（空闲/发言中/离线）；v1 顺序轮次同一时刻至多一个发言中。
- 群设置：改最大轮数 → PATCH 调用。
- **（v2）N 个并发流式气泡**——v1 不适用。

### 9.3 E2E（Playwright + `acp-mock`）

- 建群 → 加 2 个成员 → 发一条消息 → 断言多成员气泡按序出现 + 主持人消息可见 + 人类抢占生效。
- 跨层接线（连接用成员、落库/广播用群）必须用真后端验证——单测 mock 会掩盖接线错误。

### 9.4 守卫测试

- 会话列表必须过滤 `group_member`（防成员泄漏）。
- 群消息的 `agent_id` 非空。

## 10. 开放问题

> 以下均已决策（2026-10-04）。保留列表以便追溯。

1. ~~最大轮数默认值~~ → **默认 10，可在群设置改**（决策 #31）。
2. ~~主持人是否最终汇总~~ → **产出总结**（决策 #32）。
3. ~~`content` 信封主持人标记字段名~~ → **作废**（N6：机制不可行），改用 `agentId == 主持人成员行 id`。
4. ~~前端是否解析路由标签~~ → **是**（决策 #35：后端保留标签，前端解析成路由卡片）。
5. ~~`group_id` 索引~~ → `idx_sessions_group(group_id, session_type)`（Task A2）。
6. ~~群标题自动生成~~ → 占位用**主持人名** + auto-title（决策 #28）。
7. ~~成员展示名存放~~ → 成员行 **`title` 列**（决策 #36）。
8. ~~多群并发连接串扰~~ → 预期无串扰（每成员独立 session id），E2E 验证。
9. **（v2）** 同轮并发最大成员数上限。
10. **（v2）** 成员并发失败原子性。
11. ~~选主持人方式~~ → **建群模式多选列表 + 已勾选行内「主持」按钮 + 首个勾选自动成为主持人**（决策 #33/#25）。
12. ~~群标题占位文案~~ → `"{主持人名} 的群聊"`（决策 #28）。
13. ~~成员管理面板容器~~ → **`BottomSheet` + 已离场灰显标注**（决策 #34）。
14. ~~是否 v1 顺序轮次~~ → **已决定：v1 顺序轮次**（§12 结论）。
15. ~~实时发言人归属~~ → **`stream_start` 加 `agent_id`**（Task F0b）。
16. ~~成员身份用行 id~~ → **`chat_history.agent_id` 存成员行 id**（§4.3）。
17. ~~群级孤儿流式行清理~~ → 编排器在群回合首尾清理（§5.6）。
18. ~~群停止传播~~ → 成员回合 ctx 派生自编排器（§5.5）。
19. ~~`GetSessionCount` 是否计入群~~ → **群计入、成员不计**（决策 #37）：**4 处** COUNT 改 `session_type IN ('chat','group')`；`POST /api/group/create` 也须过上限门。
20. ~~群设置 UI 形态~~ → **成员管理 BottomSheet 内**（决策 #38），不进设置页。

## 12. 设计评审勘误（2026-10-04，Superpower code-reviewer，已逐条核对代码）

> 架构主干（成员=隐藏会话行、单一群时间线、增量注入游标语义）经代码验证**成立**。但"其余零改动"的乐观判断在若干承重点上是错的。以下为**实现前必须修订**项，已核对到具体代码。

### Critical

**C1 — 流式行定位是「按会话找最新一条」，同轮并行从构造上就是坏的。**
`UpdateStreamingMessage`（`chat.go:2512`）与 `FinalizeStreamingMessage`（`chat.go:2593`）用
`WHERE session_id=? AND streaming=1 ORDER BY id DESC LIMIT 1` 定位。§3.1 要求它们用群会话 id ⇒ N 个并发成员**互相覆盖同一条流式行**。§5.1 的"同轮并行"（决策 #22）不成立。
**修（v1 决定：回避）**：v1 改为**顺序轮次**（决策 #22），同一时刻只有一个成员在写群时间线，`ORDER BY id DESC LIMIT 1` 恰好命中正确的那条流式行 ⇒ C1 不触发。**v2 若恢复并行**，必须新增按消息 id 的原语 `UpdateStreamingMessageByID(id, content)` / `FinalizeStreamingMessageByID(id, content)`（`chat.go:2721` 的 `UpdateMessageContent` 是雏形但不改 `streaming`/`completed_at`），executor 所有内容写入改用 `e.cfg.StreamingMessageID`。计划 L0/L1 只加 WS 事件 `message_id`（前端路由），**不解决此问题**。

**C2 — `chat_history.agent_id` 无写入路径也无读取路径。**
`insertChatMessageTx`（`chat.go:764`）INSERT 无该列、`AddChatMessage`（`chat.go:624`）无该参数；`scanMessages`（`chat.go:131`）与所有 SELECT（`chat.go:70,343,361`）未选该列；`model.ChatMessage`（`model/chat.go:124`）**无 `AgentID` 字段**（`ChatSession` 才有，`:278`）。
**修**：`AddChatMessage`/`insertChatMessageTx` 贯穿 `agentID`；`model.ChatMessage` 加 `AgentID`；所有 SELECT + `scanMessages` + handler JSON 补上；否则 Task F1 的测试无法实现、§9.4 守卫永不过。

**C3 — "只需过滤会话列表"错误；`session_type` 影响面 17 处（历史计数，实际以计划 E2 的逐处清单为准），且参数化查询 grep 不到。**
`GetRecentSessions`（`store/session_queries.go:133`）、`SearchSessionsByTitle`（`:250`）是 `WHERE s.session_type = ?` **参数化**的，`grep "session_type = 'chat'"` **找不到**。遗漏点还包括 `chat.go:1478,1641,2007,2014,2046,2068`、`continue_conversation.go:48,141,161,430`、`session_command.go:46,71`、`handler/session_resume.go:358,459`。
**修**：按 `grep -rn "session_type" internal/`（非字面量）逐一判断"该处是否应显示群"；注意 `GetSessionCount`（`chat.go:2046`）喂会话上限门（`chat_session.go:174`），群会绕过上限。补 search/browse/overview 的守卫测试。

**C4 — Phase C 漏了 `run_turn.go` 两处，错误与 metadata 落进隐藏成员会话。**
`failTurn`（`run_turn.go:196`）的 `emitDrainEvent` + `AddChatMessage` 用 `s.SessionID`；`runTurnFinalize`（`:340`）的 metadata 广播用 `at.spec.SessionID`。
**修**：均改 `effectiveTimelineSessionID()`。另需决策 `session_executor.go:797` 的 `chat_tool_calls.session_id` 是否用群 id（否则群导出/分享/fork 上下文取不到工具调用）。

**C5 — 成员 resume 修法错误。**
- **ACP 根本不读 `ChatRequest.Resume`**：`acp_backend.go` 的 resume 完全由 DB `external_session_id` 经 `GetOrCreateConn`/`ensureAliveWithSession`（`acp_conn_lifecycle.go:280`）驱动 ⇒ 对 ACP 成员"强制 resume"是 no-op。
- **对 CLI 反而误导**：`resolveResumeSessionID`（`chat_request.go:267`）在 `resume=false` 时返回 `SessionID=memberUUID, Resume=false`；事后翻 `Resume=true` 会让 `BuildBaseStreamArgs`（`common_stream.go:21`）传 `--resume <CLI 没见过的 id>`。修法须**重跑解析**或同时设 `SessionID=GetExternalSessionID(member)`。
- **兄弟字段同病**：`HasConversationHistory`（`chat_request.go:136` 由 `GetChatMessageCount` 推出）对成员恒 false ⇒ `shouldNewSessionFallback(false)=true`（`acp_backend.go:335`）⇒ ACP 瞬时断连时**静默新建会话丢原生上下文**（正是该字段要防的失忆）。`AssistantMessageCount=0` 还会破坏周期性系统提示重注入。
**修**：成员回合的请求构造需整体修正（resume + HasConversationHistory + AssistantMessageCount 三处都基于"成员行有 external_session_id"而非 `chat_history`）。

**C6 — 把 `activeStreams` 改成时间线 id 是无谓错误，破坏优雅关停。**
`activeStreams`（`session_executor.go:25`）是**服务端**注册表（供 `FlushStreamingNow`/`WaitStreamsDrained`），与前端订阅哪个会话无关。改成群 id 后 N 个成员互相覆盖 ⇒ SIGTERM 只 flush 1/N，`WaitSessionStreamDrained(group)` 提前返回。
**修**：`emitStreamEvent`（`:357`）用时间线 id（对）；**`activeStreams` Store/Delete（`:349,369`）保持 `cfg.SessionID`**（或改复合键）。**v1 顺序模式**下同时只有一个成员在跑，覆盖不会发生，但仍应保持 `cfg.SessionID` 语义正确（键 = 正在执行的那个成员行）。

**C7 — E2E（M1）按现状必失败。**
`e2e/helpers/server.ts:148` 只写 1 个 agent（`acp-mock`），`cmd/acp-mock/main.go` 不含任何路由标签（grep=0）。
**修**：M1 需 (a) ≥2 个 agent YAML，(b) mock 支持产出 `<clawbench-speaker>`/`<clawbench-group-end/>`，(c) 接好人类抢占路径（见 I4）。

### Important

**I1 — Phase A/E 测试脚手架编译不过。** `setupTestEnv` 只在 `internal/handler/testutil_test.go:41`，返回 `(*testEnv, func())`（无 `.Cleanup()`，字段是 `ProjectDir` 非 `ProjectPath`）。service 包测试用 `InitDB()`/`store.SetDBForTest`（参 `schema_migrations_test.go:20`）。

**I2 — `internal/ai/stream_event.go` 不存在。** `StreamEvent` 在 `internal/ai/interface.go:414`。计划 Task L0 的文件路径错误。

**I3 — Task L0 Step 4 "PASS" 为假。** 把 `simpleTextPayload` 从 `map[string]string` 改 `map[string]any` 会打断 `stream_hub_test.go` 的 **12 处** `map[string]string` 断言（`:299,306,340,397,1008,1016,1024,1202,1214,1221,1252,1284`），须一并更新。

**I4 — runner/running-state 接线全缺 → 停止按钮是 no-op。** `runTurn` 不调 `TryClaimSessionRun`/`SetSessionRunning`（在 `handler/chat.go:469,527`）。后果：(a) `IsSessionRunning(memberID)` 恒 false ⇒ ACP 空闲回收（`acp_pool.go:481`）可能杀掉长辩论中的成员连接；(b) 群未注册 running ⇒ 前端对 `groupID` 发 cancel，`CancelSession(groupID)` 找不到 runner，**静默 no-op**；(c) F3 的 `CancelSession(memberRowID)` 同样需成员已注册。

**I5 — 用 `CancelSession(memberRowID)` 抢占对隐藏行有副作用。** 会发终态 `session_update` + push（`session_runtime.go:832`）并 finalize 孤儿消息 → 对隐藏成员是多余通知/误 finalize。需成员感知的取消（跳过 push/broadcast）。

**I6 — Task C3 的"源码守卫"钉错东西。** 断言"调用点用了 `timelineSID()`"在 C1 冲突仍在时照样绿。应改为**行为测试**：两个并发 executor、不同 `StreamingMessageID`、同一时间线，断言两行各自正确 finalize。

**I7 — 前端"单一 streaming 消息"假设远比 `findStreamingMsg` 广。** `useChatStream.ts` 有 **20 处** `findStreamingMsg`（`.find()` 取第一个）。L1 只处理 Map，未覆盖 `rebuildFromDb`/`loadHistory` 切回时的 N 个并发占位合并——正是"切走切回内容消失"类 bug 所在。

**I8 — `triggerChatSummarization(timelineSID=group)` 每个成员回合都跑 ⇒ O(n²)。** 每次扫全群 assistant 消息（`session_runtime.go:892`）。

**I9 — 主持人游标语义自相矛盾。** §5.1 说主持人发言后更新 `seen_cursor`；§5.2/决策 #23 定义游标为**轮次开始高水位** `H`（针对成员）。主持人的游标取什么须显式定。

**I10 — `UpdateLastRead`→时间线 id 会清群未读。** Task C3 要求如此，则任一成员回合 `splitAtSteerBoundary`（`session_executor.go:1143`）都会把群标记已读，群徽章永不亮。

### Minor

- **M1** `PatchContextStateMerge` 用 `json(?)`（`chat.go:2362`），`seen_cursor` 必须传合法 JSON 数字串（如 `"42"`）否则 SQLite 报错被静默丢；`SaveContextState`（`chat.go:2327`）会整列覆盖，未来调用者会抹掉 `seen_cursor`/`host_member_id`。
- **M2** `grouprouting.Parse` 只在闭标签**之后**取指令；标签前的指令会丢；未知发言人未校验（须编排器校验）。
- **M3** §5.4 主持人标记（开放 #3）与最大轮数（开放 #1）在 F2 已硬编码，须先定。
- **M4** `session_type` 无 CHECK 约束，拼错会静默造出未列出的会话；`model/chat.go:284`、`store/session_queries.go:99` 注释仍写 `"chat" | "scheduled"`。
- **M5** `handler/tts.go:504` 直接 `json.Marshal(event)`，加未打 tag 的 `MessageID int64` 会改其载荷；建议 `json:"-"`。
- **M6** 群行的 `agent_id`/`backend`（来自主持人）不用于执行（执行走成员行）；`ResolveAgentID(groupID)` 返回主持人，仅展示用，须注释防被"修正"进执行路径。
- **M7** 计划卫生：C2 无真失败测试（全推给 C3）；F3/G1 缺显式 Step 2/4。
- **M8** 决策 #22（同轮并行）正是逼出 C1 流式行重写的根源。**v1 已决定改为顺序轮次**（决策 #22 修订），完全规避 C1/C6/I6/I7，仍交付"主持人控场+AI 决定发言者+人类可介入"。并行留作 v2。

### 结论

**已采纳 v1 = 顺序轮次。** 架构主干成立；顺序模式直接消除 C1/C6/I6/I7（并行专属问题）。**实现前仍需修订**：Phase C 须补漏掉的 `run_turn` 两处（C4）并明确 `activeStreams` 保持成员 id（C6）；Phase D 须重做 resume 机制（C5）；Phase F 前须补 runner/running-state 接线（I4）；C2（agent_id 读写路径）、C3（`session_type` 影响面，历史计数 17 处，实际以计划 E2 逐处清单为准）须按勘误落实。**L0/L1（并发气泡）整体移出 v1。**

## 12.1 二轮评审勘误（2026-10-04，Superpower code-reviewer，已核对代码）

> 二轮评审验证 C1–C7 的修订是否真正消解，并检查 v1=顺序 是否引入新问题。结论：**2 个新 Critical（N1 是上轮修订新引入的）+ 6 个新 Important**。

### Critical（二轮）

**N1 — Phase D 的 resume 修法会破坏 ACP 池键（上轮修订新引入）。**
`resolveMemberResume` 在 `extID != ""` 时返回 `SessionID = extID`，而计划要求"覆盖 `ChatRequest.SessionID`"且**无 ACP/CLI 区分**。但 ACP 的 `req.SessionID` 是**连接池键**（`acp_backend.go:61` `GetOrCreateConn(ctx, b.agent, req.SessionID, ...)`；`:126` `getSessionAutoApprove(req.SessionID)`；`acp_pool.go:529` 预填）。覆盖成 ACP session id ⇒ 池里多一个错误条目、`getExternalSessionID(extID)` 查不到、auto-approve 丢失。
**修**：`SessionID` 覆盖**仅对 CLI**（`!isACP`）；ACP 只覆盖 `Resume`/`HasConversationHistory`/`AssistantMessageCount`，`SessionID` 保持成员行 id。D1 文案自相矛盾（"ACP 的返回 SessionID 被忽略" vs "必须 SessionID=extID"）须改。另 `assistantCount` 参数未用，删。

**C5（二轮复核）— 仍部分未修**，根因同上 N1。

### Important（二轮）

**N2 — 群"停止"到不了当前成员回合。** F0 为 `groupID` 与 `memberRowID` 都注册了运行态，但前端停止对 `groupID` 调 `CancelSession(groupID)`，只取消群占位 runner，**不取消成员的 `runTurn`**。须让每个成员回合的 `TurnSpec.Ctx` 派生自编排器可取消 ctx（见 §5.5）。F0 的测试能抓到，但实现步骤缺失。

**N3 — 实时成员气泡无法归属。** `stream_start` payload 只有 `message_id`（`ws/stream_hub.go:373`），`simpleTextPayload` 无 agent id；`agent_id` 仅落库后有。流式中前端无法渲染"成员头像+名字"。v1 方案见 §7（推荐 `stream_start` 加 `agent_id`）。

**N4 — 同一 agent 加两次会破坏自排除。** 注入按 `agent_id ≠ self` 过滤；若两成员同 agent 则互相看不到。**成员身份改用成员行 id**（见 §4.3 修订）。

**N5 — C2 读路径不全。** 除 A3 已列，遗漏：`GetChatHistory`（`chat.go:34`，fork/RAG）、`GetMessageByID`（`chat.go:260`，TTS/summary/RAG）、`GetSessionMessagesForSelection`（`session_share_payload.go:129`，**群分享丢归属**）、`continue_conversation.go:215,446`（fork/continue 复制丢归属）、`chat.go:2116`（preview）。§9.4 守卫只测主路径，这些遗漏会绿着上线。

**N6 — 主持人标记的写入/读取机制未定义。** F2 说"写群消息时加 `meta.role`"，但 content 由 executor 的 `buildContentJSON`（`session_executor.go:1430`）构建，编排器无注入点；J1 读 `meta.role` 也与 `ChatMessage.metadata`（响应元数据，非 content 信封）不符。须给出机制。

**N7 — I1 只修了一半。** A1/E2 已用 `InitDB()` 模式，但 **A2 Step 1（计划 166-167 行）与 E1 Step 1（922-924 行）仍写 `setupTestEnv(t)` + `env.Cleanup()` + `env.ProjectPath`**（该函数只在 handler 包、字段是 `ProjectDir`）。仍不可编译。

**N8 — A3 测试夹具没有 agent_id 列。** A3 让用 `setupDB(t)`，但 `chat_test.go:29-42` 的 `schema` 常量**不含 `agent_id`**，其 `m.AgentID` 断言会因错误原因失败。A3 须同时给该内存 schema 加列。

### Minor（二轮）

- **N9 设计正文残留过时并行段落**（已在本轮修正）：§7、§9.1、§9.2、§10 #9/#10 曾强制"并发"测试/气泡——v1 不满足。
- **N10** `GetSessionCount` 计入群的决策仍开放（见 §10 #19，会话上限门 4 处：`chat.go:2046`、`continue_conversation.go:161,430`、`session_resume.go:358`）。
- **N11** `RegisterSessionTurnCancel` 的键（成员行 id）与群级中断路径不连通（同 N2）。
- **N12** 顺序游标语义**验证正确**（B 能看到 A 同轮发言，且不重放自己）✅。
- **N13** I8（summarization O(n) 每成员回合）未被处理：C3 反而把 `triggerChatSummarization` 指向群时间线。损害有界（跳过已摘要），但计划未测未缓解。
- **M4/M6** 仍未进计划（`session_type` 无 CHECK、群行 `agent_id` 仅展示用）——低优先，可留 v2。

### 二轮结论

**仍不满足直接实现条件（2 Critical）。** 顺序轮次的**定位**判断正确，但 (1) D1 的 `ChatRequest.SessionID` 覆盖须 CLI-gated（否则毁 ACP 池键，N1）；(2) C2 读路径须补全（share/fork/continue/TTS，N5）且 A3 夹具 schema 须加列（N8）。另外 N2（停止传播）、N3（实时归属）、N4（成员身份）、N6（主持人标记机制）须在对应 Phase 落实；设计正文的并行残留（N9）本轮已清。

## 12.2 三轮评审勘误（2026-10-04，Superpower code-reviewer，已核对代码）

> 三轮评审验证决策 #31–#38 与二轮修复 N1–N8 是否真正落地。结论：**5 个新 Critical（C-1..C-5）+ 10 个 Important**，多为"规范自相矛盾/不可执行"类，非架构问题。

### Critical（三轮）

**C-1 — `TurnSpec.AgentID` 传成员行 id 会毁后端解析。**
`run_turn.go:234` 是 `agentID := ResolveAgentID(spec.SessionID, spec.AgentID)`，`ResolveAgentID`（`agent_resolve.go:25`）在 `requested != ""` 时**原样返回**；随后 `run_turn.go:245` 把它喂给 `NewBackendForAgentWithTransport(spec.BackendName, agentID, transport)`。若 `AgentID` = 成员行 id，`model.GetAgent(成员行id)` → nil → 落到 `NewBackend(backendName)`，对 **ACP-only 后端直接失败**（"unsupported backend type"）——每个 ACP 成员回合都死。
**修**：**新增独立字段** `TurnSpec.SpeakerID` / `RunConfig.SpeakerID`（= 成员行 id）；`StreamStartData` 也加 **`SpeakerID`**（**不是 `AgentID`**——四轮 N-1 修正：字段名不得复用 `AgentID`），`stream_start` 广播与 `AddChatMessageWithAgent` 的归属参数**都取 `SpeakerID`**；**`TurnSpec.AgentID` 保持真实 agent id（或留空让 `ResolveAgentID` 从成员行推出）**。F0b/C2/F2 须按此改。

**C-2 — #32 汇总：B2 说"同轮"、F2 说"另跑一轮"，二者冲突。**
B2 提示词（计划 565 行）让**发结束标签那一轮**自己写结论；F2（1268 行）又"循环结束后再跑一次主持人"。一起实现 ⇒ 结束信号路径**两份汇总**；达上限路径 B2 的提示词（"结束标签之后"）**没要求汇总**。
**修（已写入 §5.1）**：结束信号路径——**发标签那轮即汇总，不再另跑**；达上限路径——**额外跑一次**主持人（提示词变体：只写结论、不输出路由标签、不解析其标签）。§6 旧"可选"措辞已作废。

**C-3 — 群发送链路从未接到编排器。**
设计 §8 说"复用 `/api/ai/chat` 由编排器接管；或新增 `/api/group/chat`"，G1 定义了 `POST /api/group/chat`，F2 定义了 `RunGroupTurn`——但**没有任何 Task** (a) 让 `AIChat`（`handler/chat.go:30`）把群会话委派给编排器，或 (b) 让前端 `sendMessage`（现发 `/api/ai/chat`）改发 `/api/group/chat`。照现状，往群里发消息会跑一次**普通单智能体回合**（用群行）或打到**永不调用的端点**。
**修**：明确二选一并加 Task：**方案 A（推荐）** 在 `AIChat` 的 POST 分支判断 `session_type='group'` → 委派 `RunGroupTurn`；前端不改。方案 B 前端按会话类型选端点。**须显式写入 G1/F2**。

**C-4 — A3 的测试文件有两个 Go 包声明。**
A1 把 `database_group_test.go` 定为 `package service`（计划 84 行）；A3 复用**同一文件**却要求 `package service_test`（258 行，因需 `helperCreateSession` + `service.*`）。一个文件不能既是 `service` 又是 `service_test`。
**修**：A3 用**独立文件**（如 `chat_agent_id_test.go`，`package service_test`），或把 A1/A3 拆开。

**C-5 — F0 修改 F2 才创建的文件。**
F0（计划 1138-1139）`Modify: internal/service/group_orchestrator.go`；F2（1242）`Create` 同一文件。F0 在 F2 **之前**跑，文件尚不存在。
**修**：把 F0 的编排器相关改动合并进 F2，或让 F2 先建骨架、F0 后接线（调换顺序）。

### Important（三轮）

- **I-1** E1 测试与 `ListGroupMembers` 契约矛盾：契约含 `archived=1`（已离场），但测试删成员后断言 `len==1`；应含 archived ⇒ 断言应为 2（或 `ListGroupMembers` 提供 includeArchived 开关）。
- **I-2** #37 的"5 处 COUNT"实为 **4 处**（已修正设计 #37/§10 #19）；且 E2 把 `IN ('chat','group')` 套到 `continue_conversation.go:48,141` / `session_resume.go:459` 的 **`source_session_id` 查找**（非列表/搜索）属类别错误。
- **I-3** N3"不会破坏既有断言"**为假**：`stream_hub_test.go:1066,1100` 是**整 map 相等**断言，无条件加 `agent_id:""` 会破坏它们 → `stream_start` 的 `agent_id` **须为空时省略**（同 `simpleTextPayload` 对 `think_id` 的处理）。
- **I-4** F1 夹具用**显示名**作 `self` 且消息**无 `agent_id`** → 自排除断言无法成立。夹具须给消息填成员行 id 的 `AgentID`，`self` 也传成员行 id。
- **I-5** J1 拿不到主持人成员行 id：`GET /api/group/members` 返回无 `isHost`，且无 `GET /api/group/info`。须补（见 §7）。
- **I-6** `POST /api/group/members` 是单数 `{groupId, agentId}`，但流程要**批量**（设计 #27/§8、K1）。body 须接受数组。
- **I-7** `AddGroupMember` 无展示名参数，但 #36 把名字存 `title`。须加参数或明确由 agent 名派生。
- **I-8** 设计 §4.2 仍写第一轮旧修法（"强制置 resume=true"）——**已修正**（改指向 §12 C5/N1）。
- **I-9** 设计 §6"可选让主持人汇总"与 #32 冲突——**已修正**。
- **I-10** `maxRounds` 的**读取路径未定义**：`ContextState`（`chat.go:2200`）只认 `Mode`/`ThinkingEffort`/`Usage`；F2/G1 引用但无 Task 实现 reader。须加 `GetGroupMaxRounds`（§4.4 已注明）。

### Minor（三轮）

- 计划头部"决策表 30 条"应改为 **38 条**。
- 设计 §11 文件索引若干行号过时（`database.go:563/577` 实际 279/293；`chat_request.go:62` 约 66）。
- 设计 §4.3"普通单会话消息 `agent_id` 留空（或填会话 id）"——已删"或填会话 id"（会污染前端映射）。
- F0/G1 缺显式 Step 2/4；E2 的 `Files` 漏 `session_resume.go`。
- §5.1"连续失败 2 次自动收尾"未进 F2。
- ~~会话列表未渲染群图标（设计 §7）~~ → **已实现**：群行 meta 行 = `Users` 图标 + 成员头像堆叠（主列表 + 跨项目列表），成员预览随列表批量返回。
- E1 的 `GetGroupHost` 描述为"返回 agent id"实为成员行 id。

### 三轮结论

**仍不满足直接实现条件（5 Critical）。** 架构成立、v1=顺序正确，但 C-1（后端解析被成员行 id 破坏）、C-2（汇总两种矛盾写法）、C-3（群发送链路未接）、C-4（A3 双包）、C-5（F0 先于 F2）必须先修；I-1/I-2/I-3/I-4 是首跑必挂的夹具缺陷，I-10 使 maxRounds 不可读。**均为局部规范修正，无需重构架构。**

## 12.3 四轮评审勘误（2026-10-04，Superpower code-reviewer，已核对代码）

> 四轮验证三轮 5 个 Critical 的修复，并**专门追查"修复本身引入的新 bug"**（前三轮已两次出现此模式）。结论：**3 个新 Critical（N-1/N-2/N-3，其中 N-1/N-2 是三轮修复引入的回归）+ 9 个 Important**。

### Critical（四轮）

**N-1（三轮 C-1 修复引入的回归）— `chat_history.agent_id` 仍被写成真实 agent id。**
三轮把 `StreamStartData` 的字段改名 `SpeakerID`，但 **C2 的代码片段仍把 `agentID`（真实 agent id）传给 `AddChatMessageWithAgent`**。而该调用是 `chat_history.agent_id` 的**唯一写入点**（`UpdateStreamingMessage`/`FinalizeStreamingMessage` 只改 `content`/`streaming`/`completed_at`，不碰 `agent_id`）。后果：F1 的 `agent_id != self` 自排除失效、前端行 id→agent 映射失效、N4 失效，而 §9.4 守卫（"agent_id 非空"）**照样通过**。
**修**：`AddChatMessageWithAgent(..., spec.SpeakerID)`；`stream_start` 广播也填 `SpeakerID`；加测试断言"占位行 `agent_id == 成员行 id`"。（设计 §4.3 已明确该列语义 = 成员行 id。）

**N-2（三轮 C-5 修复引入的回归）— 执行顺序把 F2 排在 F1 之前。**
三轮改为 F2→F0→F1→F3，但 **F2 的测试与实现都依赖 F1 的 `buildInjectionText`**（`group_inject.go`），且 F2 广播 `stream_start` 依赖 F0b 的 `SpeakerID` 字段。
**修**：正确顺序 **F0b → F1 → F2 → F0 → F3**。

**N-3（三轮 C-4 修复未覆盖的连锁）— A3 改 SELECT 会打挂 ~25 个测试夹具。**
A3 给 `GetChatHistory`/`GetMessageByID`/`GetSessionMessagesForSelection` 等 reader 的 SELECT 增加 `agent_id` 后，**所有自带内存 `chat_history` DDL 却无该列的测试**都会 "no such column" 失败。`grep -rln "CREATE TABLE.*chat_history" internal/ --include=*_test.go` 得 **25 个文件**（含 `handler/testutil_test.go`，约 1575 个调用者）。
**修**：A3 须枚举全部并补列（推荐抽共享 DDL 常量）。见计划 A3。

### Important（四轮）

- **N-4** 一条消息同时含路由标签与结束标签时：`End=true` 但结束标签会**泄漏进 `Instruction`**（`text[loc[1]:]`）。须"结束优先 + 从 Instruction 剥除结束标签"，并加 both-tag 测试。
- **N-5** C-3 的两个未定义依赖：(a) **无 `session_type` reader**（须新增 `GetSessionType`）；(b) **委派点未定**——须明确在 `TryClaimSessionRun`/`AddChatMessage` **之前**委派，否则用户消息写两次。
- **N-6** J2 要求的 parity corpus + `parity_test.go` **无人创建**（B1 只建 `grouprouting.go`+`_test.go`）。须 B1 建 Go 侧语料与测试，J2 补 TS 侧。
- **N-7** `CreateStreamingMessage`（`chat.go:2571`）内部 `AddChatMessage(..., "", "")` ⇒ split 出的 "after" 行**无归属**。须加 `agentID` 参数传 `SpeakerID`（与 N-1 同源）。
- **N-8** `SetSessionGroupID` **不存在**（E1 引用它）。须实现或内联 UPDATE。
- **N-9** E1 的 `GetGroupHost` 与 `GetGroupHostMember` 重叠且返回语义矛盾（前者描述为 agent id）。**只保留后者**（返回成员行 id）。
- **M-1** `stream_hub_test.go:1066` 是 `streamSplitPayload` 的断言（不受影响）；只有 `:1100` 相关。须补"`SpeakerID==""` 时键不存在"的测试。
- **M-7** 删除 `POST /api/group/chat`（双入口是下一轮"哪条路活着"bug 的温床）。
- **M-6** 设计 §12/§12.1 残留的历史计数（"17 处"/"5 处"）标为历史，避免第五轮重新翻案。

### 四轮结论

**仍不满足直接实现条件（3 Critical）。** 三轮修复的**意图正确但未落到代码片段**：C-1 改了字段名却没改消息行写入（N-1）、C-5 调序把 F2 排到 F1 前（N-2）、C-4 只改测试文件名未覆盖 A3 的夹具连锁（N-3）。加上 N-5（缺 reader + 委派点）、N-8（幻影函数），修完即可实现；其余为规范卫生。

**四轮累计**：一轮 7C、二轮 2C、三轮 5C、四轮 3C。**模式**：每轮修复都倾向"改勘误文字而非改代码片段"，导致下一轮发现片段未同步。**建议**：修完四轮后，实现者以**代码片段**为准（勘误文字仅作背景），逐 Task 编译验证。

## 12.4 五轮评审勘误（2026-10-04，Superpower code-reviewer，已核对代码）

> 五轮**以代码片段为唯一判据**逐行核对。结论：**3 个新 Critical（R-1/R-2/R-3，均为四轮修复暴露的"声明/文件清单滞后"）+ 10 个 Important**。四轮 N-1 的**片段改动确实落地**（无片段再把 `agentID` 当成员行 id），漂移又上移一层到"**结构体声明所在 Task 的片段未同步**"。

### Critical（五轮）

**R-1（四轮 N-1 修复暴露）— `SpeakerID` 在 Phase C 使用、却在 Phase F 才声明。**
C2 的片段（plan 780/786/798）读 `spec.SpeakerID` 并构造 `RunConfig{SpeakerID:...}`，但 **C1（拥有 `TurnSpec`/`RunConfig` 的 Task）的片段只加了 `TimelineSessionID`**；F0b 才提 `SpeakerID`，而 Phase C 早于 Phase F ⇒ **照片段实现会 `spec.SpeakerID undefined`**。
**修**：把 `TurnSpec.SpeakerID` / `RunConfig.SpeakerID` / `StreamStartData.SpeakerID` 的**声明放进 C1 的片段**（F0b 只加 payload 与接线）。已改计划 C1。
- 附：F0b 的 Files 把 `RunConfig` 误标在 `run_turn.go`（实际 `session_executor.go:197`）；commit 漏 `session_executor.go`。

**R-2（四轮 C-3 修复暴露）— G1 无法交付委派：`handler/chat.go` 不在 Files/commit 中。**
G1 body 要求改 `AIChat`（`handler/chat.go:30`），但 Files/commit 无此文件 ⇒ 照片段实现则**群发送静默走普通单智能体回合**（C-3 回归原状）。
**修**：G1 Files/commit 加入 `internal/handler/chat.go`。已改计划 G1。

**R-3（四轮 N-3 修复暴露）— A3 改 `insertChatMessageTx` 签名，漏了第二个调用者。**
`insertChatMessageTx` 有两个调用者：`chat.go:653` 与 **`queue_store.go:217`**（`materializeQueuedRowTx`）。A3 只列 `chat.go` ⇒ Go 无默认参数 ⇒ **未列文件编译失败**。
**修**：A3 Files/commit 加入 `internal/service/queue_store.go`。已改计划 A3。

### Important（五轮）

- **R-4** 设计 §3.1 body 仍把 `UpdateLastRead` 归为**时间线语义**，与计划 C3/I10（保持 `cfg.SessionID`）矛盾——而计划让实现者"先读设计 §3.1"。**已修**（改为显式例外）。
- **R-5** 设计 §8 **漏 `GET /api/group/members`**（J1/K1 的 `isHost` 来源）。**已修**。
- **R-6** **`GroupMember` 类型无人声明**（E1 的 `countActiveMembers([]GroupMember)` 依赖它）。**已修**（E1 定义）。
- **R-7** E2 的 Files/commit **漏 `handler/session_resume.go:358`**（#37"4 处"之一）。**已修**。
- **R-8** fork/continue 的 **INSERT 路径**（`continue_conversation.go:244,479`）不写 `agent_id`——A3 的 N5 只覆盖 SELECT。**已修**（A3 Files 加 continue_conversation 的写路径）。
- **R-9** J2 的 both-tag 用例未在 TS 侧固定 `Instruction`；parity 语料"或新建"会破坏唯一性。**已修**（B1 建语料 + J2 读同一份，both-tag 断言 `instruction`）。
- **R-10** N-3 推荐的"共享 DDL 常量"对 `internal/store` **不可行**（import cycle）。**已修**（store/rag 逐文件改）。
- **M-1'** B1 测试数：片段 6 个、plan 写"5 个"。**已修**。
- **M-2'** `GetSessionFullInfo` 不含 `session_type`（N-5 的"或复用"是死路）。**已修**（G1 直接说须新增 `GetSessionType`）。
- **M-3'** F2 片段引用未声明的 `groupMemberTurn`/`groupMemberResult` 类型。**已修**（F2 声明）。

### 五轮结论

**仍不满足直接实现条件（3 Critical）。** 四轮 N-1 的片段改动**确已落地**（无片段再误用 `agentID`），但漂移上移一层：**声明所在 Task 的片段未同步**（R-1）、**task 的 Files/commit 滞后于自己的 body**（R-2/R-3）。修完 R-1/R-2/R-3 + R-4/R-5/R-6/R-7 即可实现。

**五轮累计**：7C / 2C / 5C / 3C / 3C。**趋势**：Critical 不再来自架构或核心机制，而是**文档内部一致性**——每轮修复的文字与片段/清单错位。**强烈建议**：不再依赖文档迭代，直接进入实现——以**代码片段**为准，逐 Task `go build`/`go test` 验证；文档已足够指导实现，剩余问题会在编译期即时暴露（比再评审一轮更快更准）。

## 11. 关键文件索引

| 关注点 | 文件:行 |
|---|---|
| 会话表 schema | `internal/service/database.go:293`（`chat_sessions` 建表） |
| 消息表 schema | `internal/service/database.go:279`（`chat_history` 建表） |
| 会话结构体 | `internal/model/chat.go:274` |
| 消息结构体 | `internal/model/chat.go:124` |
| 会话 agent 解析 | `internal/service/agent_resolve.go:19` |
| 回合编排 | `internal/service/run_turn.go:220` |
| 请求构造 | `internal/service/chat_request.go:42`（`SessionHasAssistant` 判定约 `:62`） |
| 消息落库 | `internal/service/chat.go:624` |
| 分页读取 | `internal/service/chat.go:70` |
| 未读查询 | `internal/service/chat.go:1187` |
| 会话列表 | `internal/service/chat.go:1229` |
| 会话计数（上限门） | `internal/service/chat.go:2040`（`GetSessionCount`） |
| runner registry | `internal/service/session_runner.go:46` |
| ACP 连接池 | `internal/ai/acp_pool.go:185` |
| ACP 空闲回收 | `internal/ai/acp_pool.go:199` |
| ACP 恢复 | `internal/ai/acp_conn_lifecycle.go:224` |
| WS 订阅 | `internal/ws/stream_hub.go:38` |
| stream_start payload | `internal/ws/stream_hub.go:373` |
| StreamEvent / StreamStartData | `internal/ai/interface.go:414` / `:479` |
| 前端订阅 | `web/src/composables/useChatStream.ts:86` |
| 消息列表渲染 | `web/src/components/chat/ChatMessageList.vue` |
| 消息气泡 | `web/src/components/chat/ChatMessageItem.vue` |
| 团队面板先例 | `web/src/components/chat/TeamPanel.vue` |
| ask-question 先例 | `internal/askquestion/` |
| 建会话按钮 | `web/src/components/session/SessionListHeader.vue:26` |
| 会话创建核心 | `web/src/composables/useChatSession.ts:1030` |
| agent 选择抽屉 | `web/src/components/common/AgentSelectorDrawer.vue` |
| 会话创建端点 | `internal/handler/chat_session.go:171` |
| agent 图标 | `web/src/components/common/AgentIcon.vue` |
| 成员颜色 | `web/src/utils/teamMemberColor.ts` |
