# 群聊分叉与跨形态分叉设计

日期：2026-10-22
状态：设计已定稿，待实现

## 背景与问题

分叉（fork）目前只支持「单聊 → 单聊」，且**群聊被三层同时拦住**：

- 前端：`ChatMessageItem.vue` 的 fork 按钮带 `!isGroupSession`，群聊里不渲染；
- 服务：`ForkSession` 硬编码 `session_type='chat'`，开头还有一道群守卫；
- 错误映射：`writeForkError` 把该错误翻成 `GroupNotForkable`（400）。

这条禁令来自真实的缺陷（设计决策 #74，与回溯 #52 对称）：群时间线的
`chat_history.agent_id` 存的是**成员行 id**，而 `ForkSession` 把这些行原样复制进新单聊后，
成员行 id 在新会话里解析不到，渲染成一整屏无归属的 AI 发言。

用户需求有两条：

1. 分叉时可以重新选智能体（现已支持），并且**也支持选「另一个群聊」**；
2. **群聊自己也要能分叉**，源群的历史整体作为背景知识喂给新会话。

## 决策汇总

| 维度 | 决策 |
|---|---|
| 支持的分叉组合 | 单聊→单聊（现状）、单聊→群聊、群聊→单聊、群聊→群聊 |
| 涉及群聊时的历史素材 | **只取聚合时间线**（群会话行的 `chat_history`），绝不读各成员自己的会话 |
| 涉及群聊时的落库形态 | **不复制消息行**，渲染成背景文本存进目标会话的 `context_state` |
| 新会话可见性 | **保持空白**，只有一条系统提示行 |
| 注入范围 | **所有发言者**（每位成员首次发言时各拿一份完整背景） |
| 注入时机 | 消费方「尚无成功回合」时注入（单聊=读门 `SessionHasRealAssistantContent`；群成员=自己的 `seen_cursor==0`） |
| 背景清理 | **成功后清除**：单聊在成功回合 Finalize 后 `json_remove`；群聊靠成员游标推进，无需额外清除 |
| 分叉弹窗预填 | 预填源形态（群源预选原成员+原 host；单聊源预选当前智能体） |
| 群聊回溯（rewind） | **本次不动**，仍然禁用 |
| 单聊→单聊 | **完全不动**，仍走「复制消息行 + 懒渲染 `BuildForkContext`」 |

## 关键概念澄清：聚合时间线与完整历史

群聊的数据布局是：

- **群会话行**（`session_type='group'`）的 `chat_history`：用户消息 + 每位成员的发言 +
  `role='system'` 的系统行。这就是「聚合时间线」。
- **成员行**（`session_type='group_member'`）**没有任何 `chat_history` 行**。它的记忆只在
  自己的 `external_session_id` 里，也就是各自 CLI/ACP 的会话存储，ClawBench 的 DB 查不到。
- 工具调用与思考**确实在 DB 里**，但挂在群会话 id 下（`chat_tool_calls` / `chat_thinking`，
  `message_id` 指向群时间线的那条发言）。

因此「完整历史」有两种读法，都不采用：

1. **聚合时间线 + 工具调用/思考**：技术上拿得到（同会话 id 三张表），但群聊的既有契约是
   「成员之间只交换发言文本，工具调用与思考是私有的」（`group_inject.go` 头注释，设计 §5.2 /
   决策 #9）。塞进去等于让新会话看到比当年老成员更多的信息。
2. **每个成员各自的完整上下文**：**不存在这个操作**。成员行没有历史行，要取只能对每个成员
   发一次 resume 让它自己回忆——这也正是「没必要逐个取」的技术原因。

体积上两者差距是决定性的。`fork_context.go` 的实测注释记载：用户消息平均约 51 字符，
而助手条目平均约 12 KB（工具输入输出占绝大部分）。所以：

- 只取发言文本：10 万字符预算能覆盖几乎整段对话；
- 带上工具调用：预算瞬间被吃掉，可能只剩最近一两条。

**结论**：背景素材 = 聚合时间线的发言文本。

## 设计

### 1. 四种组合的行为

| 源 → 目标 | 复制消息行 | 背景注入 | 备注 |
|---|---|---|---|
| 单聊 → 单聊 | 是（现状） | 现状（懒渲染 `BuildForkContext`） | 完全不动 |
| 单聊 → 群聊 | 否 | 是 | 新群空白，历史作为背景 |
| 群聊 → 单聊 | 否 | 是 | 只读聚合时间线 |
| 群聊 → 群聊 | 否 | 是 | 成员全部重选，预填源成员 |

**核心规则：只要一端是群聊，就一律不复制消息行。** 复制会带来成员行 id 失效的问题；
改成背景文本注入后这个约束自然消失。

### 2. 背景通道：`context_state.fork_context`

不新增列。`chat_sessions.context_state` 是 JSON 列，`seen_cursor`、`host_member_id`、
`group_mode` 都已经是同一先例。

写入（分叉时一次）：

```go
PatchContextStateMerge(targetSessionID, map[string]string{
    "fork_context": string(json.Marshal(renderedText)),  // json_set 需要 JSON 编码的字符串
})
```

写入位置：

- 目标是单聊 → 写在**目标会话行**；
- 目标是群聊 → 写在**群行**（一份数据全员共享读取，不存 N 份）。

读取：

- 单聊：`json_extract(context_state, '$.fork_context')`；
- 群聊：`prepareTurn` 读一次，放进 `groupTurnSetup`，`snapshotTurn` 直接用。

### 3. 注入门：读时判「有成功回合」，写时在成功回合后清除

目标是：**首轮失败仍能带背景重试；一旦有人成功消化了，就不再重复注入。**
这条不能只靠读时判断实现——必须「读门 + 成功后清除」两段配合。

**为什么不能只靠读门**（审查时发现的两个反例）：

1. `GetAssistantMessageCount(session) == 0` 是错的：失败回合经 `persistTurnWarning`
   （`run_turn.go:249`）会写一条 `role='assistant'` 的 warning 行。计数立刻变 1，
   于是首轮失败后背景**永久失效**——正好与目标相反。
2. 改用现成的 `SessionHasRealAssistantContent` 也不行：它只看**第一条** assistant 行
   （`ORDER BY id ASC LIMIT 1`）。若第一条是 warning（先失败后成功），判定恒为 false，
   背景会**每一轮都重复注入**。要扫全部行才能判对，那是 O(n) 的每轮开销。

**采用的实现**：

- **单聊目标**：读门 = `SessionHasRealAssistantContent(session)`（第一条即成功时成立）；
  另外在**成功回合的 Finalize 之后**调 `ClearForkContext(sessionID)`。清除是 O(1) 的
  `json_remove`，且只在真有成功回合时发生。
  - 首次成功发生在「第一条不是 warning」的情形 → 读门已经挡住，清除是幂等的兜底；
  - 首次成功发生在「第一条是 warning」的情形 → 读门放行，但清除已把背景删掉，
    所以第二轮不会重复注入。
- **群聊目标**：成员自己的 `cursor == 0` 时注入。成员游标只在成功回合后推进
  （决策 #69「失败不推进游标」），所以失败自动带背景重试；成功后游标推进，自然不再注入。
  **群聊不需要额外清除**——游标就是那个「已消化」标记。
  - 副作用：分叉后**新加入的成员**首轮也会拿到背景。这符合「背景知识」的语义，接受。
  - 副作用：free 模式下从头到尾没被 @ 过的成员永远拿不到。可接受（它本来就没参与讨论）。

两者是同一条原则的两种表现：**谁还没产出过成功回合，就还没消化完这份背景**；
而「已消化」这件事必须由一个**成功时才写的标记**来承载（单聊是清除，群聊是游标）。

### 4. 群聊源渲染器

新增 `RenderGroupForkContext(groupID string, cutoff int64, budget int) string`：

- 读群会话行的 `chat_history`（`id <= cutoff`），**跳过 `role='system'`**（「轮到用户发言」
  这类不是对话内容）；
- 用 `ListGroupMembers(groupID)` 建 `成员行 id → 显示名` 映射（含已离场成员，其发言仍在
  时间线上）；解析不到时回退为「成员」；
- 用户消息渲染为 `User: ...`（复用 `groupUserTarget()` 的昵称）；
- 成员发言的 `<clawbench-mention>` / `<clawbench-group-end/>` / 目标满足标签全部转可读
  文本或剥离（复用 `renderMentionsReadable` + `StripProtocolTags`）；
- 跳过只有 ask-question 卡片、没有正文的行时保留问题文本（复用 `askQuestionTexts`）；
- 复用 `BoundForkContext` 的预算裁剪与「N 条更早消息已省略」提示——**不重写预算逻辑**。

单聊源继续用现有的 `BuildForkContextWithOptions`（角色 `User`/`Assistant`）。

两条渲染路径共用一个 `ForkContextOptions` 头尾，只是正文来源不同，因此预算语义完全一致。

### 5. 切点语义

- **单聊源**：沿用现有规则（用户消息→扩展到紧随其后的助手回复；助手消息→就地切）。
- **群聊源**：**切点 = 被点击的那条消息 id，含自身，不向后扩展。**

  理由：群聊对一条用户消息的「回答」是多名成员的讨论，没有唯一的「那个回复」；按现有规则
  取「紧随其后的第一条 assistant」会任意地只包含第一位发言的成员、把其余讨论全部排除。

### 6. 系统提示行

分叉成功后立刻写入目标会话的**第 1 条**（新会话是空的，天然在头部）：

- 单聊目标：`「{源会话名}」的历史消息已作为背景注入当前会话。`
- 群聊目标：`「{源会话名}」的历史消息已作为背景注入本群，每位成员首次发言时都会收到。`

写入方式：单聊用 `AddSystemMessage`；群聊用 `writeGroupSystemMessage`（带 WS 广播，
创建时无订阅者，广播是空操作，用户切进去时从 DB 读到）。

**与自动命名的关系**：`insertChatMessageTx` 只在 `role == "user"` 时触发自动命名，
系统行不干扰分叉标题的 placeholder 语义（首条用户消息仍会替换 `🔀 <源标题>`）。

### 7. API

`POST /api/ai/session/fork` 扩展请求体：

```json
{
  "sessionId": "...",
  "beforeMessageId": 123,
  "targetKind": "group",          // "chat"（默认）| "group"
  "agentId": "...",               // targetKind=chat 时生效（现有字段）
  "hostAgentId": "...",           // targetKind=group 时生效
  "memberAgentIds": ["...", "..."] // targetKind=group 时生效
}
```

校验：

- `targetKind=chat`：沿用现有 agent 校验（存在、未禁用、项目可见）；
- `targetKind=group`：复用 `groupMemberSpecs`（agent 存在/未禁用/项目可见）+ 群成员上限 12 +
  free 模式至少 2 人 + `groupSessionLimitOK` 会话数上限；
- 源会话归属校验、`beforeMessageId` 合法性（存在、非 streaming）保持不变。

`GroupNotForkable` 错误键删除（i18n 双语 + `i18n_test.go` 白名单 + OpenAPI 描述）。

### 8. 前端

- `ChatMessageItem.vue`：fork 按钮去掉 `!isGroupSession`；**rewind 保持禁用**。
- `ChatPanelContent.vue` 的分叉弹窗换成带「单聊/群聊」分段开关的 `AgentSelectorDrawer`
  （复用建群那套 `groupMode` + 实例步进器 + host 点选）。
- 预填：
  - 源是单聊 → 预选当前智能体，停在单聊页签；
  - 源是群聊 → 预选原成员（含重复实例，按 `agent_id` 还原每个实例）+ 原 host 的 agent，
    停在群聊页签。
- 群聊分叉成功后补一次头像生成 + `refreshGroupMembers`（复用 `App.vue`
  `handleGroupCreate` 的后半段）。
- 分叉树：`source_session_id` 对群行同样生效，`sessionForkTree.ts` 无需改动
  （`acp:` 前缀规则不冲突）。

## 边界与风险

**成本**：群聊目标的首轮注入是 N 份（N = 成员数，上限 12）。背景受
`chat.fork_context_budget`（默认 10 万字符）约束，12 人群最坏情况首轮约 120 万字符的提示词
开销。**决策：不做分摊**，每人拿同一份完整背景（可预期、不会有人缺上下文）。

**`context_state` 体积**：背景文本常驻目标行的 `context_state`。单行最多约 10 万字符，可接受；
且不做清理，避免首轮失败丢上下文。

**源会话被删**：背景在分叉时已物化落库，不依赖源会话存活。这比「只存 source_id 懒渲染」安全。

**群聊 rewind 仍然禁用**：截断群时间线会让成员的 `seen_cursor` 指向已删除的行，下一轮会漏掉
一段上下文。与本次改动无关，保持现状（`ErrGroupNotRewindable`）。

**群成员在分叉后被移除**：背景文本是快照，包含已离场成员的发言，这是正确的（当时确实在讨论）。

**空源**：源会话没有可渲染的发言（全是系统行/思考）时，`fork_context` 为空字符串，
不写、不注入、也不写系统提示行（避免出现「已注入」但实际没有内容的谎报）。

## 改动清单

### 后端

- `internal/service/fork_context.go`
  - `RenderGroupForkContext(groupID, cutoff, budget)`：群时间线 → 背景文本
  - 群聊 speaker 名映射（含离场成员）与协议标签剥离
- `internal/service/continue_conversation.go`
  - `ForkSession` 去掉群守卫，按 `targetKind` 分派
  - 新增 `ForkSessionToGroup(...)`：不复制消息，落 `source_session_id` + `fork_context`
  - 单聊目标但源是群聊：不复制消息，只落 `fork_context`
  - 群聊源的切点解析（含自身、不扩展）
- `internal/service/group_store.go`
  - `CreateGroupWithMembers` 增加 `sourceSessionID` 参数（写 `source_session_id`）
  - `insertGroupRow` 补 `source_session_id` 列
  - `GetGroupForkContext(groupID)` 读取
- `internal/service/chat.go`
  - `GetForkContext(sessionID)` / 群聊版共用同一个 `json_extract` 读取
- `internal/service/chat_request.go`
  - `BuildChatRequest` 增加「显式背景」分支：`GetAssistantMessageCount == 0` 时读
    `fork_context` 填进 `ChatRequest.ForkContext`（与现有 resume 分支互斥但并列）
- `internal/service/group_orchestrator.go`
  - `prepareTurn` 读一次 `fork_context` 存进 `groupTurnSetup`
  - `snapshotTurn` 在 `cursor == 0` 时把背景拼到 prompt 最前面
- `internal/handler/chat_session.go`
  - `ServeForkSession` 增加 `targetKind` / `hostAgentId` / `memberAgentIds`
  - 群聊目标走建群校验；成功后写系统提示行
  - 删除 `GroupNotForkable` 分支
- `internal/i18n/locales/active.{zh,en}.yaml`
  - 新增 `ForkContextInjectedChat` / `ForkContextInjectedGroup`
  - 删除 `GroupNotForkable`
- `internal/i18n/i18n_test.go`：白名单同步
- `internal/api/openapi.yaml`：`/api/ai/session/fork` 字段与错误描述

### 前端

- `web/src/components/chat/ChatMessageItem.vue`：fork 按钮放开群聊
- `web/src/components/chat/ChatPanelContent.vue`：分叉弹窗改为单聊/群聊可选 + 预填
- `web/src/composables/useChatSession.ts`：`forkSession` 增加 target 参数
- `web/src/composables/useSessionManager.ts` / `useSessionIdentity.ts`：类型透传
- `web/src/App.vue`：群聊分叉后补头像生成 + 成员刷新
- `web/src/i18n/locales/{zh,en}.ts`：文案

### 文档

- `docs/spec/core/session-lifecycle.md`：分叉条目补充四种组合
- `docs/spec/features/session-navigation.md`：分叉流程与切点语义
- `docs/spec/features/group-chat.md`：群聊可分叉，背景注入规则

## 测试计划

**service**

- `RenderGroupForkContext`：成员名映射、离场成员、系统行跳过、mention 标签剥离、预算裁剪、
  空源返回空串
- `ForkSessionToGroup`：不复制消息行、写 `source_session_id`、写 `fork_context`、
  成员/上限/项目归属校验
- 群聊源 → 单聊：同样不复制、`fork_context` 落目标行
- 切点语义：群源含自身不扩展；单聊源保持现有扩展行为

**orchestrator**

- 每位成员 `cursor == 0` 时都拿到背景；发言成功后不再拿
- 成员首轮失败（cursor 不推进）→ 下一轮仍带背景
- 分叉后新加入成员首轮也带背景
- 单聊目标：无定稿助手消息时注入，有之后不再注入

**handler**

- 四种 `targetKind` × 源形态组合
- 群聊目标的各类拒绝（成员上限、free 需 2 人、agent 禁用、跨项目、会话数上限）
- 系统提示行写入内容与位置（第 1 条）

**前端**

- 预填：群源预选原成员+原 host；单聊源预选当前 agent
- 请求体：`targetKind` / `hostAgentId` / `memberAgentIds` 正确传递
- 群聊里 fork 按钮可见、rewind 仍不可见

## 明确不做

- **不做成员各自会话的读取**：不存在可读取的历史行，且与「聚合时间线」的决策冲突。
- **不把工具调用/思考写进背景**：违反群聊的「只交换发言文本」契约，且会挤爆预算。
- **不做背景按成员数分摊**：每人完整一份，行为可预期。
- **不放开群聊 rewind**：`seen_cursor` 会指向已删行，需要独立设计。
- **不做背景的过期清理**：注入门已让它自然失效；清理只会在首轮失败时误删。
- **不改单聊→单聊的既有路径**：复制消息行 + 懒渲染的组合工作正常，改动风险大于收益。
