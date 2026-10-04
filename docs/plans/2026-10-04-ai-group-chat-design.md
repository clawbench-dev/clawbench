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
| 28 | 群标题 | **可留空**，占位用**主持人名**（如"XX 的群聊"），首条消息后 auto-title 接管 |
| 29 | 删成员 | 历史发言**保留并标记"已离场"**（灰显，与"离线"共用视觉）；主持人不再选它 |
| 30 | 成员角色 | **不做群内角色设置**，成员人设完全来自智能体自身的描述与提示词 |
| 31 | 最大轮数 | **默认 10，可在群设置里改**（存群行 `context_state`） |
| 32 | 最终汇总 | **结束时主持人产出总结**（结束信号或达上限都产出） |
| 33 | 选主持人组件 | **复用扩展后的多选 `AgentSelectorDrawer`**（此处单选） |
| 34 | 成员管理容器 | **`BottomSheet` 抽屉** + 已离场成员**灰显并标注"已离场"** |
| 35 | 路由标签展示 | **后端保留标签，前端解析成路由卡片**（"主持人 → A、B"） |
| 36 | 成员展示名 | 存成员行的 **`title` 列**（复用现有字段） |

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
- **时间线语义**（必须用群会话 id）：`AddChatMessage`（`run_turn.go:277`）、`UpdateStreamingMessage` / `FinalizeStreamingMessage` / `FinalizeCancelledStreamingMessage`（`:514,1168,1626`）、`persistThinkingToDB` / `AppendThinkingSegment`（`:1054,1249`）、tool-call 持久化（`:914,968`）、`CreateStreamingMessage`（`:1183`）、`triggerChatSummarization`（`:1644`）、`UpdateLastRead`（`:1143`）。

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

修法（编排器/请求构造层，不改判定函数本身）：群成员的回合在 `BuildChatRequest` 之后**强制置 `resume=true`**（ACP 与 CLI 都适用；ACP 用成员行的 ClawBench UUID 走池映射，CLI 用成员行的 `external_session_id`）。判据：成员行 `external_session_id != ""` 或 `SessionHasAssistant` 为真任一成立即 resume。首轮（两者皆空）自然 resume=false，符合预期。此逻辑须在成员回合的请求构造里显式写，并加单测钉住"第二轮成员发言 resume=true"。

### 4.3 群时间线归属

`chat_history` 新增一列 **`agent_id TEXT DEFAULT ''`**：

- **存"发言人成员行 id"（不是 agent id）**（评审 N4）：同一 agent 可被加进同一群两次，若按 agent id 归属则两条成员无法区分，且增量注入的"作者 ≠ 自己"过滤会失效。列名沿用 `agent_id`，语义为**发言人的成员会话行 id**；前端用群成员列表（头像条已加载）把行 id 解析为 agent 头像/名字。
- 主持人发言写入时，`agent_id` = 主持人的**成员行 id**，并以**消息级标记**区分"这是主持人发言"（见 §5.4）。
- 用户消息 `agent_id=''`。
- 普通单会话消息 `agent_id` 留空（或填会话 id），不影响既有查询。
- **增量注入的"作者 ≠ 自己"按该列（成员行 id）比较**，与 `buildInjectionText(..., selfAgentID, ...)` 的 `self` 参数同源（实现时 `self` 传成员行 id）。

**消息只存一份**——不存在第二份副本，从根上消除一致性问题。

### 4.4 成员表？

**不新建成员表**。成员即隐藏 session 行；花名册通过 `SELECT ... FROM chat_sessions WHERE group_id = ? AND session_type='group_member'` 得到。成员的**展示名/颜色/角色说明**存 `context_state` JSON 或复用 `title` 字段（title 存成员在群内的展示名）。

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
     - 结束信号 → break
     - 解析失败 → 回退规则轮转（下一个未发言成员）；连续失败 2 次 → 自动收尾
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
主持人产出最终汇总（决策 #32；正常结束或达上限都产出）
```

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
- 结束信号：`<clawbench-group-end/>`。
- **契约**：检测即解析；不可解析时**不剥离**标签（与 askquestion 的"绝不丢内容"一致，保留原文给用户看），并回退轮转。
- **后端保留标签**（不剥离），**前端解析成路由卡片**（决策 #35）——故 Go 实现与前端**镜像** + parity corpus（与 askquestion 同款双向固化）。

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
| 达到最大轮数 | 强制停（可选让主持人做最终汇总） |
| 用户抢占 | 取消当前 runner，人类消息进时间线，主持人重选 |
| 整个群回合崩溃 | 已有发言全部保留（已落库），群回到空闲 |

异常终止时插**可见系统消息**（决策 #18），复用主持人特殊样式。

## 7. 界面

- **会话列表**：群与普通会话并列，群图标标识；`group_member` 行被过滤（防泄漏到侧边栏）。
- **群时间线**：成员发言气泡显示头像 + 名字 + 后端标识（复用 `AgentIcon` / `getAgentName`）；主持人发言居中特殊样式。
  - **⚠️ 实时归属缺口（评审 N3）**：`stream_start` 的 payload 只有 `message_id`（`ws/stream_hub.go:373`），`simpleTextPayload`（`:345`）也不带 agent id。所以**流式过程中**前端无法知道当前气泡属于哪个成员——`agent_id` 只在消息落库后由 DB 读回。v1 方案二选一：(a) 在 `stream_start` 增加 `agent_id`（后端小改）；(b) 明确降级为"流式时用通用样式、重载后才显示发言人"。**v1 推荐 (a)**（改动小且体验完整）。
- **（v2）同轮并发气泡**：一轮内 N 个成员同时流式时的多锚点渲染——**v1 顺序轮次不需要**，见 §12。
- **顶部横向头像条**（决策 #19）：群会话头部一行成员头像，**发言中高亮/脉动**（可同时多个高亮）；点击可查看成员详情。
- **人类介入**：输入框支持 @ 成员（走已有 `@` 提及模式），发送即抢占。

## 7.1 建群交互（决策 #24–#30）

### 入口

会话列表头（`SessionListHeader.vue`，`data-action="create"` 旁）新增一个**独立"建群"按钮**（群图标）。移动端抽屉与桌面侧栏共用同一 header，两处自然都有入口。

> 为什么不复用 `+`：`+` 是"新建单聊"的高频动作，改成二选一会拖慢最常见的操作；也不复用"长按 `+`"（与现有"长按=默认 agent 建会话"语义冲突）。

### 建群流程（极简，单步）

点击"建群" → 打开**扩展为多选模式的 `AgentSelectorDrawer`**（本流程只需选一个，但复用同一多选组件；单选即选主持人）→ 用户**显式指定主持人**（无默认）→ 创建。

- **只选主持人，不选其他成员**：成员全部在群建好后手动添加（决策 #25）。
- **主持人 = 成员之一**（决策 #26）：它是群里的第一个成员，既控场又参与发言（其路由指令本身就是可见发言）。因此"只有主持人的群"不是空群——用户可直接和它聊（退化成单聊），加人后成为真正的群聊。
- **群标题可留空**（决策 #28）：不填则占位用**主持人名**（如"CodeBuddy 的群聊"），首条消息后由现有 auto-title 机制接管。
- **成员不做群内角色设置**（决策 #30）：成员在群里的"人设"完全来自它作为智能体自身的描述与提示词，不引入群内二次设定。

### 建群后加成员

- **入口**：群会话顶部头像条**尾部一个 `+`**，点击打开多选 `AgentSelectorDrawer`，一次可勾多个、确认后批量加入（决策 #27）。
- **扩展 `AgentSelectorDrawer` 支持多选**：加 `multiple` prop + `v-model` 数组 + checkbox 行 + 选中不自动关闭；单选模式（单聊入口）行为不变。列表行（`AgentIcon` + 名字 + 专业 + 后端标签 + 默认徽章）直接复用。
- **成员展示名/颜色**：展示名存成员行 **`title` 列**（决策 #36）；颜色复用 `AgentIcon` + `teamMemberColor.ts`（与 `TeamPanel.vue` 同款）。

### 删成员

- **入口**：成员管理面板（头像条 `+` 进入后的成员列表里可移除）。
- **容器**：**`BottomSheet` 抽屉**（决策 #34），与移动端现有抽屉一致；抽屉内同时承载**群设置**（最大轮数，决策 #31）。
- **历史发言保留并标记"已离场"**（决策 #29）：发言原样保留，头像灰显 + "已离场"标记；主持人不再选它。与"离线"状态（连接无法恢复）复用同一套视觉。
- 理由：群时间线是"会议纪要"，删掉某人发言会让后续"B 回应 A"失去上下文；完全不留痕又会让用户以为系统出 bug。

## 8. API

新增（需同步 `internal/api/openapi.yaml`）：

- `POST /api/group/create` — 建群（标题 + **主持人 agent**；成员建群后再加）。
- `POST /api/group/members` — 增成员（批量）。
- `DELETE /api/group/members` — 删成员。
- `PATCH /api/group/settings` — 群设置（最大轮数，决策 #31）。
- 群消息发送：复用 `/api/ai/chat`（携带群会话 id），由编排器接管；或新增 `/api/group/chat`。
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
11. ~~选主持人组件~~ → **复用多选 `AgentSelectorDrawer`（此处单选）**（决策 #33）。
12. ~~群标题占位文案~~ → `"{主持人名} 的群聊"`（决策 #28）。
13. ~~成员管理面板容器~~ → **`BottomSheet` + 已离场灰显标注**（决策 #34）。
14. ~~是否 v1 顺序轮次~~ → **已决定：v1 顺序轮次**（§12 结论）。
15. ~~实时发言人归属~~ → **`stream_start` 加 `agent_id`**（Task F0b）。
16. ~~成员身份用行 id~~ → **`chat_history.agent_id` 存成员行 id**（§4.3）。
17. ~~群级孤儿流式行清理~~ → 编排器在群回合首尾清理（§5.6）。
18. ~~群停止传播~~ → 成员回合 ctx 派生自编排器（§5.5）。
19. **（v1 遗留）** `GetSessionCount` 是否计入群（会话上限门 4 处）——**建议计入**（群是用户可见会话），实现时确认。
20. **（v1 遗留）** 群设置 UI 的形态（最大轮数在哪改）——可先放成员管理抽屉内。

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

**C3 — "只需过滤会话列表"错误；`session_type` 影响面 17 处，且参数化查询 grep 不到。**
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

**已采纳 v1 = 顺序轮次。** 架构主干成立；顺序模式直接消除 C1/C6/I6/I7（并行专属问题）。**实现前仍需修订**：Phase C 须补漏掉的 `run_turn` 两处（C4）并明确 `activeStreams` 保持成员 id（C6）；Phase D 须重做 resume 机制（C5）；Phase F 前须补 runner/running-state 接线（I4）；C2（agent_id 读写路径）、C3（session_type 17 处）须按勘误落实。**L0/L1（并发气泡）整体移出 v1。**

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
| 建会话按钮 | `web/src/components/session/SessionListHeader.vue:26` |
| 会话创建核心 | `web/src/composables/useChatSession.ts:1030` |
| agent 选择抽屉 | `web/src/components/common/AgentSelectorDrawer.vue` |
| 会话创建端点 | `internal/handler/chat_session.go:171` |
| agent 图标 | `web/src/components/common/AgentIcon.vue` |
| 成员颜色 | `web/src/utils/teamMemberColor.ts` |
