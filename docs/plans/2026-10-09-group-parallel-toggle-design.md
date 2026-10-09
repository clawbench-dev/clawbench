# 群聊并发开关 + 成员卡锁图标按需显示 设计

> 2026-10-09。两项改动：
> A. `MentionCard` 的锁图标**只在有密送时才显示**（当前恒显）。
> B. 群聊 actionbar 增加**并发执行开关**：打开后，用户 @ 多人（或不 @ 人）时
>    他们**并发执行**；智能体之间 @ 不受此开关控制（由它们自己决定）。

## 1. 问题

### A. 锁图标恒显

`MentionCard.vue:27` 无条件渲染 `<Lock>`，卡片注释解释为「锁是密送功能的固定
affordance，是否设置由 `is-private` 样式承载」。用户要求：**默认不展示锁，
只有填了密送内容才显示锁**，锁本身成为「存在密送」的信号。

### B. 用户 @ 的成员串行执行

自由模式（free）下，用户的 @ 成员卡在发送时被 `serializeMentionCards` 序列化为
**每卡一个** `<clawbench-mention targets="id">` 标签。后端 `freeInitialGroups`
按标签分组（`resolveSpeakerGroups`），于是 N 张卡 = N 个 sequential 组 = **串行**发言。

并发能力**已存在**（§14 并发发言已实现）：标签带 `mode="parallel"` 即并发，
且智能体已被 `group_prompt.go:73` 教导自己用这个属性。缺的是**用户侧的一个入口**：
让用户一句话就能让本轮自己 @ 的成员并发执行，而不必手写 `mode="parallel"`。

## 2. 关键事实（已核实）

1. **智能体之间 @ 已自带并发控制**：`group_prompt.go` 已教成员/主持人用
   `mode="parallel"`，`grouprouting.Parse` 已解析该属性，`resolveSpeakerGroups`
   已按组 `Parallel` 并发。故「智能体之间 @ 不受开关控制」**无需任何改动**。
2. **主持人模式下用户不路由**（决策 #47）：`runRounds` 的种子来自主持人的
   mention，用户的 `o.userMessage` 只被 `storeUserNotes`（密送）消费。故开关
   在主持人模式**无作用** → 仅自由模式显示。
3. **开关只影响「用户种子」**：并发组由 `freeInitialGroups` 产出。用户 @ 多人 →
   现为多个 sequential 组；用户不 @ 人 → 现为一个 **sequential** 组（全体成员）。
   开关打开时两者都应变成**一个 parallel 组**。
4. **排队路径共享同一后端编排**：`RunGroupTurnDrain` 读 `o.userMessage = row.Content`，
   直发与排队都走它。故把开关做成**群级持久设置**并在后端 drain 时读取，
   两条路径自动一致，无需给排队表加列。

## 3. 决策

| # | 决策 |
|---|---|
| 1 | 锁图标 `v-if="hasNote"`：无密送不渲染锁；有密送显示 accent 锁 + accent 描边。 |
| 2 | 并发开关是**群级持久设置** `parallelDefault`，存 `context_state.$.parallelDefault`（同 `maxRounds`）。 |
| 3 | 开关**仅自由模式显示**（主持人模式无作用，显示会误导）。 |
| 4 | 开关语义 = **用户种子的默认并发**：打开时，用户 @ 的多个成员合并为**一个 parallel 组**；不 @ 人时全体成员为**一个 parallel 组**。 |
| 5 | 实现位置 = **后端 drain 时按群设置合并**（单一真源，直发/排队一致；消息文本不记录并发意图）。 |
| 6 | **不覆盖智能体自身的 `mode`**：开关只作用于用户种子；智能体 @ 出的组沿用其标签的 `mode`。 |
| 7 | 开关**优先于用户种子的分组**：开关打开时，用户种子的全部公开 mention 成员合并为一个 parallel 组（成员卡从不写 `mode`，故这是唯一实际形态）。开关**不触碰**智能体消息里的任何 `mode`。 |

## 4. Part A：锁图标

`MentionCard.vue`：
- `<Lock v-if="hasNote" ... />`（当前无条件）
- 注释更新：锁从「固定 affordance」改为「密送存在信号」；`is-private` 样式保留
  （描边 + 锁高亮）。
- 无密送时卡片只剩头像 + 名字，宽度自然收缩。

`MentionCard.test.ts`：
- `shows the lock indicator in both states` **反转为**：
  - 无 note → `.mention-card-lock` 不存在
  - 有 note → 存在
- 其余不变。

## 5. Part B：并发开关

### 5.1 后端：群设置字段

`group_store.go`（镜像 `GetGroupMaxRounds`/`SetGroupMaxRounds`）：
```go
// GetGroupParallelDefault: context_state.$.parallelDefault，缺省 false。
func GetGroupParallelDefault(groupID string) bool
// SetGroupParallelDefault: PatchContextStateMerge({"parallelDefault": "true"|"false"})
func SetGroupParallelDefault(groupID string, enabled bool) error
```
- 存储用 JSON 布尔（`json_extract` 读 `1`/`0`），与 `maxRounds` 的数字存法区分；
  缺省/非法 → `false`（串行，旧行为不变）。

`handler/group.go`：
- `serveGroupMembersList` 响应加 `parallelDefault`。
- `ServeGroupSettings` 请求体：`maxRounds` 与 `parallelDefault` **都改为可选**
  （指针/omitempty 判存在），至少一项存在才处理；`maxRounds` 仍要求 `>0`。
  向后兼容：只传 `maxRounds` 的旧客户端行为不变。

`openapi.yaml`：`/api/group/members` 响应加 `parallelDefault`；
`/api/group/settings` 的 `required` 去掉 `maxRounds`，两个字段都列，加 `parallelDefault`。

### 5.2 后端：编排合并

`group_orchestrator.go` 的 `freeInitialGroups`：
- 新增读取 `GetGroupParallelDefault(o.groupID)`。
- **用户无 @ 人**（`route.Found == false` 或 `len(groups)==0`）：全体 active 成员
  构成**一个组**，`parallel` 取开关值（当前恒 `false`）。
- **用户 @ 了人**（`resolveSpeakerGroups` 有结果）：若开关打开，把**所有组的
  成员**摊平进**一个** `speakerGroup{parallel: true}`（保留各自 instruction 拼接或
  取并集；用户标签体为空，故 instruction 无实际内容——见 §5.4）。
  - 但**标签显式带 mode** 时（用户手写）：按标签分组（决策 #7）。
- 去重：合并时沿用 `resolveSpeakerGroups` 已有的跨组去重（`seen`），不重复发言。

实现要点：给 `resolveSpeakerGroups` 增加一个「合并为一个 parallel 组」的参数，
或在 `freeInitialGroups` 内对返回的 groups 做合并。**合并必须是纯函数可测**。

### 5.3 前端：设置读取与回显

`useGroupChat.ts`：`listGroupMembers` 返回加 `parallelDefault: boolean`。
`useGroupMembers.ts`：新增 `parallelDefault` ref（默认 false），`refresh` 时赋值，
`return` 暴露。
`App.vue`：从 `useGroupMembers` 解构 `parallelDefault`；传给 `ChatPanelContent`。
`ChatPanelContent.vue`：新增 prop `groupParallelDefault`，传给 `ChatInputBar`，
并接 `@toggle-parallel` → PATCH + refresh（server-authoritative，同 auto-approve）。

### 5.4 前端：actionbar 按钮

`ChatInputBar.vue`：
- 新增 prop `groupMode`（`'host'|'free'`，默认 `'host'`）与
  `groupParallelDefault`（boolean，默认 false）。
- actionbar 内新增按钮，`v-if="isGroupSession && groupMode === 'free'"`：
  - 图标：并发语义图标（如 `Zap`/`Layers`/`GitBranch`——待选，见 §7）
  - `:class="{ active: groupParallelDefault }"`（复用 `.chat-action-btn.active` 样式）
  - `@click="$emit('toggle-parallel', !groupParallelDefault)"`
  - `:title` / wide label 走 i18n
- 新增 emit `'toggle-parallel'`。

i18n（zh/en）：`chat.actions.parallel`、`chat.actions.wideLabels.parallel`、
`group.parallelHint`（说明「打开后你 @ 的成员并发执行；智能体之间的 @ 不受影响」）。

### 5.5 数据流总览

```
用户点 actionbar 开关
  → ChatInputBar emit('toggle-parallel', next)
  → ChatPanelContent: PATCH /api/group/settings {groupId, parallelDefault:next}
  → 成功后 refreshGroupMembers()（回读服务端值）
  → useGroupMembers.parallelDefault 更新 → 按钮高亮态刷新

用户发送消息
  → 后端 RunGroupTurnDrain → runFreeLoop → freeInitialGroups
  → 读 GetGroupParallelDefault(groupID)
  → 用户 @ 多人 或 不@人 → 合并为一个 parallel 组（若开关开）
  → drainSpeakers → runParallelGroup（并发）
```

## 6. 测试

- **Go（设置）**：`GetGroupParallelDefault` 缺省 false；`Set` 后读回 true；
  写 `false` 后读回 false。
- **Go（编排）**：开关打开时——
  - 用户 @ 两个成员 → **一个** parallel 组（并发），两行都落库；
  - 用户不 @ 人 → 全体为一个 parallel 组；
  - 开关关闭 → 保持现有行为（@ 多人 = 多个 sequential 组；不@人 = 一个 sequential 组）。
  - **智能体 @ 不受影响**：成员发出的 `mode="parallel"` 标签仍并发（开关关闭时亦然）。
  - 显式标签 mode 优先（决策 #7）。
- **Go（handler）**：PATCH 只带 `parallelDefault` 成功；只带 `maxRounds` 仍成功；
  两者都缺 → 400。
- **前端 util**：`listGroupMembers` 解析 `parallelDefault`（缺省 false）。
- **前端组件**：`ChatInputBar` 在 free 模式渲染开关、host 模式不渲染；
  点击 emit `toggle-parallel`；`active` 态随 prop。
- **MentionCard**：锁只在有 note 时存在（变异：改回恒显 → 红）。
- **e2e（可选）**：free 群打开开关 → @ 两成员 → 两成员并发（两行气泡同时流式）。
  若成本高，后端 DB 级测试已覆盖语义。

## 7. 已定

- 并发开关图标 = **`Layers`**（`Zap` 已用于发送按钮的快捷菜单，避免语义冲突）。
- 文案：`chat.actions.parallel`（长 tooltip）/ `chat.actions.wideLabels.parallel`
  =「并发」/「Parallel」。**注意 `@` 在 vue-i18n 消息里必须写 `{'@'}`**，否则
  `@` 被当作 linked-message 语法导致构建失败（本次踩到）。

## 9. 实施结果（2026-10-09）

全部落地，测试通过：

| 层 | 文件 | 改动 |
|---|---|---|
| 前端组件 | `MentionCard.vue` | 锁 `v-if="hasNote"`；`.mention-card-lock` 去掉 idle 态 |
| 前端组件 | `MentionCard.test.ts` | 断言反转为「无 note 无锁、有 note 有锁」 |
| 后端设置 | `group_store.go` | `GetGroupParallelDefault` / `SetGroupParallelDefault`（`$.parallelDefault`） |
| 后端 handler | `group.go` | members 返回 `parallelDefault`；settings 两字段可选、至少一个 |
| 后端编排 | `group_orchestrator.go` | `freeInitialGroups` 读设置；`mergeParallel` 合并种子 |
| OpenAPI | `openapi.yaml` | members 响应 + settings 请求体 |
| 前端 API | `useGroupChat.ts` | `listGroupMembers` 返回 `parallelDefault`；`setGroupParallelDefault` |
| 前端状态 | `useGroupMembers.ts` | `parallelDefault` ref |
| 前端接线 | `App.vue` / `ChatPanelContent.vue` | 透传 + `handleToggleGroupParallel`（PATCH + refresh） |
| 前端组件 | `ChatInputBar.vue` | free 模式 actionbar 开关按钮 + `toggle-parallel` emit |
| i18n | `zh.ts` / `en.ts` | `chat.actions.parallel` / `wideLabels.parallel` |

**测试与变异**：
- Go：设置往返 3 态；handler 三例（只 parallel / 两者都缺 400 / 只 maxRounds）；
  编排 4 例（开关开@多人合并、开关开不@人全体并发、开关关不覆盖智能体 mode、
  开关关@多人串行）。
- 变异：把 `parallelSeed` 强制 `false` → 2 个「开关开」测试变红（确认非空转）。
- 前端：MentionCard 锁两态；ChatInputBar 开关渲染/active/emit 三例；
  useGroupChat 解析 + PATCH 体；useGroupMembers 解析；ChatPanelContent 透传守卫。
- 构建：`npm run build` 通过；产物含 `toggle-parallel` 与 `mention-card-lock`。

## 8. 不做 / 边界

- **不改** `buildMentionTag` / `serializeMentionCards` 的文本协议（决策 #5：设置是唯一真源）。
- **不改**智能体 prompt（它们已会用 `mode="parallel"`）。
- **不改**主持人模式的种子（用户不路由，开关隐藏）。
- **不给排队表加列**（设置是群级持久状态，drain 时读取）。
- **不覆盖**用户显式写入标签的 `mode`。
- **I1**：用户 @ 的成员已离场 → 现有 `freeInitialGroups` 回退到全体成员
  （既有行为，本次不处理）。
