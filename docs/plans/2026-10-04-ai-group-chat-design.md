# AI 群聊设计（Group Chat）

日期：2026-10-04
状态：设计定稿（待实现）

## 1. 目标

让用户创建一个"群"，把多个已配置的智能体（可跨后端，如 CodeBuddy + Claude Code + Codex）加进去，然后就同一话题进行**多智能体辩论式群聊**：

- 有一个**主持人**（一个被指定为主持人、同时是群成员的普通智能体）控场，决定下一个该谁发言、给它什么指令；
- 成员**同轮并行、轮次间共享上下文**——每个成员发言时能看到其他成员此前的发言文本；
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
| 21 | 多发言者 | 主持人**可同时点名多个成员**（`<clawbench-speaker>A,B,C</...>`） |
| 22 | 同轮执行 | **同轮并行，跨轮由主持人排序**——轮内被点名者同时发言、互不可见；要"B 回应 A"由主持人把 A 放前一轮、B 放后一轮 |
| 23 | 注入游标语义 | 游标 = **本轮开始时**的群时间线高水位；注入 = `id > 游标` **且作者 ≠ 自己**（防并行同伴被跳过 / 自己旧发言被重放） |
| 24 | 建群入口 | 会话列表头**独立"建群"按钮**（非 `+` 二选一、非长按） |
| 25 | 建群流程 | **极简**：只选主持人；成员建群后再手动添加 |
| 26 | 主持人身份 | **主持人 = 成员之一（兼主持人）**；"只有主持人的群"退化为单聊，仍可用 |
| 27 | 建群后加成员 | 头像条尾部 `+` → **多选抽屉**（扩展 `AgentSelectorDrawer` 支持多选） |
| 28 | 群标题 | **可留空**，先用成员名拼接占位，首条消息后 auto-title 接管 |
| 29 | 删成员 | 历史发言**保留并标记"已离场"**（灰显，与"离线"共用视觉）；主持人不再选它 |
| 30 | 成员角色 | **不做群内角色设置**，成员人设完全来自智能体自身的描述与提示词 |

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
- **WS 广播**：`emitStreamEvent`（`:357`）与 `activeStreams` 键（`:349,369`）用**时间线 id**（群），因为前端订阅的是群会话；成员流式必须广播到群。

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

- 成员发言写入群会话时，`agent_id` = 该成员的 agent id（前端据此渲染头像/名字）。
- 主持人发言写入时，`agent_id` = 主持人的 agent id，并以**消息级标记**区分"这是主持人发言"（见 §5.4）。
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
  0. 记录本轮高水位 H = 当前群时间线最大消息 id
  1. 主持人发言
     - 增量注入：群时间线中 id > 主持人.seen_cursor 的消息文本（作者≠自己）
     - run_turn(连接=主持人成员行, 时间线=群会话)
     - 输出写入群时间线（特殊样式），更新主持人.seen_cursor
     - 解析路由标签
  2. 路由
     - <clawbench-speaker>A,B,C</...> + 指令 → 本轮发言者 = {A,B,C}
     - 结束信号 → break
     - 解析失败 → 回退规则轮转（下一个未发言成员）；连续失败 2 次 → 自动收尾
  3. 被点名成员**同轮并行**发言
     - 每个成员并发：增量注入 = 群时间线中 id > H 且作者≠自己 的消息文本
       + 主持人指令（高优先级）
     - run_turn(连接=该成员行, 时间线=群会话) —— N 个并发
     - 输出各自写入群时间线(agent_id=该成员)，更新各自 seen_cursor = H
  4. 等待本轮全部完成 → 检查抢占 / 打断 / 达 MaxRounds
  ↓
（可选）主持人做最终汇总
```

**注**：第 3 步同轮并行，成员之间**互相看不到本轮同伴的发言**（因为 H 是本轮开始前的高水位）。要"B 回应 A"，由主持人把 A 放在前一轮、B 放在本轮——**时序由主持人通过轮次编排表达**（决策 #22）。这保住了并行广播，同时让"回应"成为可能。

### 5.2 增量注入

- 游标语义（决策 #23）：成员 `seen_cursor` = **本轮开始时**的群时间线高水位 `H`，**不是**它发言结束时的最大 id。这样：
  - 同轮并行同伴的发言（id > H）**不会**被跳过——下一轮仍能注入给该成员；
  - 自己刚说过的话（作者=自己）**不会**被重放。
- 注入内容 = 群时间线中 `id > seen_cursor` **且 `agent_id ≠ 自己`** 的**发言文本**（不含工具调用、深度思考——决策 #9）。
- 主持人指令单独作为高优先级段注入（决策 #17）。
- 游标存在成员行 `context_state` JSON（决策 #10 落地）。
- **竞态说明**：若游标简单取"发言结束时的最大 id"，则 A 先说完、B 后说完时，A 的游标会大于 B 的消息 id，下一轮 A 就再也看不到 B 本轮的发言。故必须用"本轮开始高水位 + 作者过滤"。

### 5.3 路由标签

仿 `internal/askquestion/` 建独立叶子包（暂定 `internal/grouprouting/`）：

- 主持人系统提示注入格式说明，要求输出 `<clawbench-speaker>A,B,C</clawbench-speaker>` + 指令文本（逗号分隔多个成员；单个成员即顺序模式，全部成员即广播并行）。
- 结束信号：`<clawbench-group-end/>`（或标签内特定关键字）。
- **契约**：检测即解析；不可解析时**不剥离**标签（与 askquestion 的"绝不丢内容"一致，保留原文给用户看），并回退轮转。
- Go 实现与前端镜像 + parity corpus（若前端也需解析则建镜像，否则仅 Go）。

### 5.4 主持人发言的特殊样式

消息级标记方案（择一，实现时定）：

- 在 `chat_history.content` 的 JSON 信封里加 `meta.role="coordinator"`；或
- 复用 `agent_id` 与一个群级 `coordinator_agent_id` 比对（若主持人即某成员，则"agent_id == coordinator_agent_id 且内容含路由标签"视为主持人发言）。

前端据标记渲染居中特殊样式。

### 5.5 抢占

- 人类 @ 成员 / 打断 → 取消当前成员的 runner（复用 `CancelSession`，`session_runtime.go:770`）。
- 人类消息写入群时间线。
- 主持人基于新状态重新选人。
- 被中断的成员发言按现有 `cancel` 路径标记为"中断"而非错误。

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
- **同轮并发气泡**（决策 #22）：一轮内 N 个成员**同时**流式，群时间线要能渲染 N 个并发占位气泡。这是前端主要新增复杂度——需为每个并发发言维护独立的流式锚点，而非现有"单一 streaming 消息"假设（`useChatStream.ts` 的 `findStreamingMsg`）。
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
- **群标题可留空**（决策 #28）：不填则先用成员名拼接占位（如"CodeBuddy 的群聊"），首条消息后由现有 auto-title 机制接管。
- **成员不做群内角色设置**（决策 #30）：成员在群里的"人设"完全来自它作为智能体自身的描述与提示词，不引入群内二次设定。

### 建群后加成员

- **入口**：群会话顶部头像条**尾部一个 `+`**，点击打开多选 `AgentSelectorDrawer`，一次可勾多个、确认后批量加入（决策 #27）。
- **扩展 `AgentSelectorDrawer` 支持多选**：加 `multiple` prop + `v-model` 数组 + checkbox 行 + 选中不自动关闭；单选模式（单聊入口）行为不变。列表行（`AgentIcon` + 名字 + 专业 + 后端标签 + 默认徽章）直接复用。
- **成员展示名/颜色**：复用 `AgentIcon` + `teamMemberColor.ts`（与 `TeamPanel.vue` 同款）。

### 删成员

- **入口**：成员管理面板（头像条 `+` 进入后的成员列表里可移除）。
- **历史发言保留并标记"已离场"**（决策 #29）：发言原样保留，头像灰显 + "已离场"标记；主持人不再选它。与"离线"状态（连接无法恢复）复用同一套视觉。
- 理由：群时间线是"会议纪要"，删掉某人发言会让后续"B 回应 A"失去上下文；完全不留痕又会让用户以为系统出 bug。

### 待定

- **群标题占位文案**的确切格式（如"成员名 + 的群聊"）。
- **成员管理面板**是复用 `BottomSheet` 还是独立组件。

## 8. API

新增（需同步 `internal/api/openapi.yaml`）：

- `POST /api/group/create` — 建群（标题 + **主持人 agent**；成员建群后再加）。
- `POST /api/group/members` — 增成员（批量）。
- `DELETE /api/group/members` — 删成员。
- 群消息发送：复用 `/api/ai/chat`（携带群会话 id），由编排器接管；或新增 `/api/group/chat`。
- 群回合取消：复用现有 cancel。

（字段名必须从 handler 代码抄，禁止望文生义。）

## 9. 测试

### 9.1 单元测试（Go）

- `group_orchestrator` 循环逻辑：注入**假 run_turn**（脚本化返回），断言路由解析、增量游标、结束信号、最大轮数兜底、抢占、成员失败不终止群。
- **多发言者并行**：主持人点名多个成员时，断言同轮并发执行、注入的高水位 `H` 一致、各自 `seen_cursor` 更新为 `H`、且下一轮能注入到同轮同伴的发言。
- 路由标签解析器（`internal/grouprouting/`）：独立叶子包 + parity corpus（含多成员逗号分隔、结束信号、畸形输入）。
- `run_turn` 的 `TimelineSessionID`：断言"连接用成员、落库/广播用群"，且缺省时行为不变（向后兼容）。

### 9.2 单元测试（前端）

- `agent_id` → 气泡头像/名字渲染。
- 主持人特殊样式。
- 顶部头像条状态（空闲/发言中/离线），**多个成员同时发言**时多个高亮。
- **N 个并发流式气泡**：同一轮 N 个成员流式事件交错到达时，各自渲染到正确的占位气泡（不被"单一 streaming 消息"假设合并）。

### 9.3 E2E（Playwright + `acp-mock`）

- 建群 → 加 2 个成员 → 发一条消息 → 断言多成员气泡按序出现 + 主持人消息可见 + 人类抢占生效。
- 跨层接线（连接用成员、落库/广播用群）必须用真后端验证——单测 mock 会掩盖接线错误。

### 9.4 守卫测试

- 会话列表必须过滤 `group_member`（防成员泄漏）。
- 群消息的 `agent_id` 非空。

## 10. 开放问题（实现时定）

1. **最大轮数默认值**（暂定 10）与是否可配置。
2. **主持人是否也做最终汇总**（暂定可选，由结束标签控制）。
3. **`content` 信封中主持人标记的确切字段名**（`meta.role` vs 其他）。
4. **前端是否也需解析路由标签**（若仅展示则否，仅 Go 解析）。
5. **`group_id` 索引**与成员查询性能。
6. **群标题自动生成**（复用 auto-title 还是主持人生成）。
7. **成员展示名的存放**（`title` vs `context_state`）。
8. **多群并发**时每个成员的连接是否串扰（每成员独立 session id，预期无串扰，需 E2E 验证）。
9. **同轮并发的最大成员数上限**（防一次点名过多导致资源峰值；暂定不限，靠最大轮数间接约束）。
10. **成员并发失败的原子性**：同轮 N 个成员部分成功部分失败时，`seen_cursor` 与轮次推进如何取舍（暂定失败者不更新游标、下一轮可重试）。
11. **建群时"选主持人"的组件**：是复用扩展后的多选 `AgentSelectorDrawer`（单选即主持人）还是单独一个单选择人抽屉。
12. **群标题占位文案**的确切格式（如"成员名 + 的群聊"）。
13. **成员管理面板**的容器（`BottomSheet` vs 独立组件）与"已离场"标记的确切视觉。
14. **是否 v1 先做顺序轮次**（放弃同轮并行），以规避评审 C1/C6 的流式行重写（见 §12）。

## 12. 设计评审勘误（2026-10-04，Superpower code-reviewer，已逐条核对代码）

> 架构主干（成员=隐藏会话行、单一群时间线、增量注入游标语义）经代码验证**成立**。但"其余零改动"的乐观判断在若干承重点上是错的。以下为**实现前必须修订**项，已核对到具体代码。

### Critical

**C1 — 流式行定位是「按会话找最新一条」，同轮并行从构造上就是坏的。**
`UpdateStreamingMessage`（`chat.go:2512`）与 `FinalizeStreamingMessage`（`chat.go:2593`）用
`WHERE session_id=? AND streaming=1 ORDER BY id DESC LIMIT 1` 定位。§3.1 要求它们用群会话 id ⇒ N 个并发成员**互相覆盖同一条流式行**。§5.1 的"同轮并行"（决策 #22）不成立。
**修**：新增按消息 id 的原语 `UpdateStreamingMessageByID(id, content)` / `FinalizeStreamingMessageByID(id, content)`（`chat.go:2721` 的 `UpdateMessageContent` 是雏形但不改 `streaming`/`completed_at`），executor 所有内容写入改用 `e.cfg.StreamingMessageID`。**这是 Phase C 的必做任务**。计划 L0/L1 只加 WS 事件 `message_id`（前端路由），**不解决此问题**。

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
**修**：`emitStreamEvent`（`:357`）用时间线 id（对）；**`activeStreams` Store/Delete（`:349,369`）保持 `cfg.SessionID`**（或改复合键）。

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
- **M8** 决策 #22（同轮并行）正是逼出 C1 流式行重写的根源。**v1 若先做顺序轮次，可完全规避 C1/C6**，仍交付"主持人控场+AI 决定发言者+人类可介入"。建议作为**分期决策**而非事后补并行。

### 结论

**不满足直接实现条件。** 架构主干成立，但 Phase C 须重新界定（加"按 id 写入原语"与漏掉的 `run_turn` 两处），Phase D 须重做 resume 机制，Phase F 前须补 runner/running-state 接线。**建议 v1 先做顺序轮次**（M8），把 C1/C6/I6/I7 整体推迟到并行版本，显著降低首版风险。

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
