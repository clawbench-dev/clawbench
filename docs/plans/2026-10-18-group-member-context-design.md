# 群成员上下文信息（成员列表展示 + 详情面板）设计

日期：2026-10-18
状态：**已实现**（2026-10-18）。三处与初稿的差异见文末「实现记录」

## 背景

群聊里每个成员都是一条隐藏的 `chat_sessions` 行（`session_type='group_member'`），
各自持有独立的 ACP 连接和独立的上下文窗口。单聊的上下文面板（`ChatInputBar` 的
`usage-popup`）已经能完整展示 used/size/remaining、Token 明细、上下文分项与费用，
但**群聊刻意隐藏了它**：`ChatInputBar.vue:360` 的 `v-if="!isGroupSession"` —— 群时间线
上有多条上下文，"当前会话的上下文"没有唯一所指。

结果是用户在群里完全看不到各成员的上下文压力：谁快爆了、谁刚压缩过、哪个成员
根本没上报过用量，全靠猜。

需求（用户原话）：

1. 在**成员列表界面**（群聊设置抽屉的花名册）为**每个智能体**展示上下文信息；
2. **点击能打开详情面板**；
3. **最好复用单聊的上下文面板**。

## 关键事实（已核实）

1. **成员用量已经在落库，只是没人读**。成员回合的 `RunConfig.SessionID` 是成员行 id
   （`group_orchestrator.go:1979`），而 `usage_update` 的持久化走
   `flushPendingContextState → PatchContextStateMerge(e.cfg.SessionID, …)`
   （`session_executor.go:1097`）。也就是说**成员行的 `context_state.usage` 是权威的**，
   群行从来不被写。
2. **实时值也有现成的读口**：`ai.GetACPConnManager().GetCachedStateByClawbenchSID(memberRowID).Usage`
   （连接 key 就是成员行 id，因为连接是按 `cfg.SessionID` 建的）。连接空闲回收后该值消失，
   此时回落到 DB。
3. **前端不能读群会话的 usage 缓存**。`useChatStream.ts:1111` 的 `usage_update` 分支把
   载荷写进**信封里的 session_id**（群 id）的缓存 —— 成员回合的 `usage_update` 广播在群时间线
   上（`timelineSID`），于是群会话的缓存里装的是"最后一个发言成员"的用量。这个值语义上是错的，
   本次设计**不消费它**（也不改它，见"不做的事"）。
4. **单聊的上下文面板不是组件，是 `ChatInputBar.vue` 里的一段内联 `PopupMenu`**
   （`:256-348`）+ 一组 computed（`:556-620`）。"复用"必须先抽出来。
5. 群聊没有 `/compact` 入口（`handleCompact()` 只在单聊的 usage 弹窗里，而弹窗被
   `!isGroupSession` 挡掉）；成员也没有"给它发一条命令"的通道。压缩动作本次不做。
6. 群设置抽屉 `GroupSettingsSheet` 是 `GroupAvatarStack` 的子组件，整个应用只有一个实例
   （`groupSettingsButtonWiring.test.ts` 钉住）。它内部**没有** BottomSheet 嵌套，但
   `BottomSheet` 的 `drawerStack` 支持嵌套（后开者 z 更高、返回手势先关后者）。

## 决策汇总

| # | 维度 | 决策 |
|---|---|---|
| 1 | 数据来源 | 成员行 `context_state.usage`（DB，权威）**优先用活连接缓存覆盖**（实时） |
| 2 | 接口 | `GET /api/group/members` 每个成员增加 `usage`（可为 `null`）与 `model`（可为 `""`） |
| 3 | 行内展示 | 花名册每行一个 `.gm-usage`：Activity 图标 + 28×6 进度条 + 百分比；无数据显示 `—` |
| 4 | 离场成员 | 不显示用量、不可点击（冻结的旧数字会误导，且它已不在讨论里） |
| 5 | 详情入口 | **点击整行主区域**（`<button class="gm-row-main">`），删除按钮是兄弟节点（禁止 button 嵌 button） |
| 6 | 详情容器 | **嵌套 BottomSheet**（`MemberContextSheet`），不关掉群设置抽屉；返回手势先关它 |
| 7 | 面板复用 | 从 `ChatInputBar` 抽出 `ContextUsageDetail.vue`（纯展示）+ `utils/usageDisplay.ts`（纯函数），两处共用 |
| 8 | 压缩动作 | 成员面板**不显示**「压缩上下文」按钮（单聊才显示），见"不做的事" |
| 9 | 实时刷新 | 抽屉打开期间：收到该群的 `usage_update` / `stream_finalize` 时**去抖 1.5s 重拉花名册** |
| 10 | 兼容 | 新增字段可选；老客户端忽略。`ListGroupMembers`（编排器热路径）**不动** |

## 数据层

### 新查询（不动热路径）

`internal/service/group_store.go`：

```go
// GroupMemberContext 是成员行上"给详情面板看"的会话级状态。
type GroupMemberContext struct {
    Usage *ai.UsageState // 成员行 context_state.usage；nil = 从未上报（CLI 后端）
    Model string         // 成员行 model 覆盖；"" = 用智能体默认模型
}

// GroupMemberContexts 一次查询取回全群成员的上下文状态，按成员行 id 索引。
func GroupMemberContexts(groupID string) (map[string]GroupMemberContext, error)
```

一条 SQL：`SELECT id, COALESCE(model,''), COALESCE(context_state,'') FROM chat_sessions
WHERE group_id = ? AND session_type = 'group_member'`（含已离场行——它们也有历史用量，
由 handler 决定是否下发）。`context_state` 解析失败**只记 warn 并当作 nil**，
不能因为一个坏 JSON 让整个花名册 500（与 `GetContextState` 同策略）。

**为什么单独开函数而不是扩 `ListGroupMembers`**：后者被编排器**每个回合**调用
（`group_orchestrator.go:410`），它只需要身份字段。把 JSON 解析和 `model` 列塞进去，
等于给每个回合加一次无用的解析。

### Handler

`internal/handler/group.go` 的 `serveGroupMembersList`：

```go
ctxs, _ := service.GroupMemberContexts(groupID)   // 失败降级为空 map，不影响身份字段
for _, m := range members {
    c := ctxs[m.ID]
    // 活连接缓存覆盖 DB（实时）；连接被空闲回收后自动回落 DB。
    if s := ai.GetACPConnManager().GetCachedStateByClawbenchSID(m.ID); s.Usage != nil {
        c.Usage = s.Usage
    }
    out = append(out, map[string]any{ …既有字段…, "model": c.Model, "usage": c.Usage })
}
```

- `usage` 的 JSON 形状 = `ai.UsageState` = `/api/ai/chat` 的 `usageState` = `usage_update`
  载荷，三者同源，前端可以共用一个类型。
- 已离场成员的 `usage` **照常下发**（前端按 `left` 自行不渲染）；后端不做展示策略。

### OpenAPI

`internal/api/openapi.yaml` 的 `groupMembersList` 响应 schema 补两个字段，并注明
`usage` 的形状与 `usageState` 一致、`null` 表示该成员从未上报（CLI 传输的智能体不会上报）。

## 前端

### 1. 抽取共享面板（本次的核心重构）

新增 `web/src/utils/usageDisplay.ts`（纯函数，可单测）：

| 函数 | 从哪来 |
|---|---|
| `usagePct(used, size)` | `ChatInputBar.vue:556` |
| `usageColor(pct)` | `:557`（>=95 红 / >=90 橙 / >=75 琥珀 / 其余绿） |
| `cacheHitRate(hit, miss)` | `:566` |
| `formatUsageCost(amount, currency)` | `:597`（货币符号表；未知币种退化为 `12.34 CNY`；空币种只出数字） |
| `usageCategoryRows(cat, t)` | `:585`（按值过滤 + i18n 标签，未知 key 原样显示） |
| `hasTokenDetail(usage)` | `:579` |

新增 `web/src/components/chat/ContextUsageDetail.vue`（**纯展示**，props：

```ts
defineProps<{
  usage: UsageSnapshot            // used/size/…/usageByCategory（与 ai.UsageState 同形）
  /** 单聊才显示「压缩上下文」；成员面板传 false */
  showCompact?: boolean
}>()
defineEmits<{ compact: [] }>()
```

渲染顺序（与现状逐行一致，避免单聊外观漂移）：进度条 + 百分比 → 已用/总量/剩余 →
「Token 明细」分组（有值才出现）→「上下文分项」（`usageByCategory`）→ 压缩按钮。
样式沿用 `.usage-popup*`，随组件迁走；`ChatInputBar` 只保留 `PopupMenu` 外壳与
定位锚点，把 `contextUsed/contextSize/…` 组装成 `usage` 传入。

> 这一步没有行为变化，但**单聊的既有测试必须继续全绿**（`ChatInputBar.test.ts`
> 里 15+ 条 usage 用例是这次重构的回归网）。

### 2. 花名册行内展示（`GroupSettingsSheet.vue`）

行结构从「avatar + identity + tag/remove」改为：

```
<li class="gm-row">
  <button class="gm-row-main" data-action="member-context">   ← 新：整行主区域
    avatar · 名字 · 真实智能体名 · [用量 chip] · [主持人/离场 tag]
  </button>
  <button class="gm-remove">                                  ← 兄弟节点，不在主按钮内
</li>
```

- `.gm-usage`：`Activity`(11) + `.gm-usage-bar`(28×6，与单聊 `.usage-bar` 同尺寸)
  + 百分比文字（`--font-size-xs`、`tabular-nums`）。颜色取 `usageColor()`。
- **无数据**（`usage == null` 或 `size <= 0`）：只显示一个 `—`（`--text-muted`），
  `title` 提示"该成员未上报上下文用量（CLI 传输不提供）"。
- **离场成员**：不渲染 `.gm-usage`，`gm-row-main` 也不可点（渲染成 `<div>` 而非 `<button>`）。
- **宿主 tag / 离场 tag** 留在主按钮内部（纯 span，合法）。
- 两个按钮的点击互不影响：`gm-remove` 已是兄弟节点，天然不冒泡到主按钮。
- **红线**：`<button>` 默认 `text-align: center`，`.gm-row-main` 必须显式 `text-align: left`
  且 `padding: 0; background: none; border: none`，否则名字与用量会居中（历史踩坑）。

### 3. 成员详情面板（`web/src/components/chat/MemberContextSheet.vue`）

```
BottomSheet(auto, title=成员名)
├─ header: AgentIcon + 成员名 + RealAgentNameTag + [主持人] tag
└─ body
   ├─ 身份区（2–3 行 label/value）
   │    智能体 / 模型 / 状态（离场时）
   └─ <ContextUsageDetail :usage="member.usage" :show-compact="false" />
```

- **嵌套而非下钻**：从群设置抽屉里打开，群设置抽屉保持打开；`drawerStack` 保证它画在
  上面，返回手势/Android 返回先关它（`instanceSeq` 越大优先级越高）。
- 由 `GroupSettingsSheet` 持有 `selectedMemberId` + `memberSheetOpen`，避免 App 再加一层状态。
- 模型取值：`member.model`（成员行覆盖）优先，否则 `getAgentDefaultModelName(agentId)`；
  两者都空显示 `—`。
- 面板里**不出现**模型/模式切换等单聊设置（那些是成员行自己的设置，入口不在这里）。

### 4. 实时性

`useGroupMembers` 新增 `memberContexts`？**不需要**：`usage` 直接挂在
`GroupMemberInfo` 上，随 `refresh()` 一起刷新。

- 打开成员抽屉时：`refresh(groupId)` 一次（拿最新值）。
- 抽屉打开期间：`onEvent` 监听 `chat_stream`，当
  `data.session_id === groupId && (event_type === 'usage_update' || event_type === 'stream_finalize')`
  时**去抖 1.5s** 调 `refresh`。
  - 不去重则每个成员每个回合 3–5 次全量拉取；
  - 1.5s 覆盖一次 500ms flush 窗口 + 一次渲染，体感上"数字在动"。
- 抽屉关闭即解绑监听（`onBeforeUnmount`）。

## 测试

| 层 | 用例 |
|---|---|
| Go（service） | `GroupMemberContexts` 返回 usage + model；坏 JSON 退化为 nil 不报错；离场成员也在结果里 |
| Go（handler） | `GET /api/group/members` 带 `usage`/`model`；活连接缓存覆盖 DB（注入 fake cached state）；无 usage 时字段为 `null` 而不是 `{}` |
| 前端（utils） | `usageDisplay`：阈值边界（74/75/89/90/94/95）、size=0 不除零、货币符号/未知币种/空币种、category 过滤与未知 key |
| 前端（组件） | `ContextUsageDetail`：逐段渲染 + `showCompact=false` 时无按钮 + `compact` 事件 |
| 前端（组件） | `GroupSettingsSheet`：有量显示 bar+pct、无量显示 `—`、离场行无 chip 且不可点、点行 emit、点删除不 emit |
| 前端（组件） | `MemberContextSheet`：身份区 + 复用面板；`usage=null` 时是空态不是空白 |
| 前端（守卫） | 源码守卫：`ChatInputBar.vue` 内**不得再出现** `usage-popup-row` 的行渲染（防止有人把面板复制回去，两处漂移） |
| 回归 | `ChatInputBar.test.ts` 全部 usage 用例保持通过（这是抽取重构的验收线） |

E2E：`acp-mock` 不产生 usage，真实值无法在 e2e 里断言；只覆盖"打开花名册 → 点成员 →
抽屉打开 → 关闭"的交互链路（可选，若成本高则留给单测）。

## 不做的事（明确记录，避免回头重走）

1. **不把用量显示到群会话信息栏**。群时间线没有"自己的上下文"，那一条是成员回合的
   副产品（见关键事实 3）。群栏保持只有巡航/自动批准两个 chip。
2. **不改 `useChatStream` 的 `usage_update` 分支**。它把成员用量写进群会话缓存是既有行为，
   本次不消费、也不修（要修得给信封加 `speaker_id`，属于另一件事）。**新代码不得读取
   群会话的 usage 缓存**。
3. **不给成员做 `/compact`**。没有按成员发命令的通道；要做需要新的端点 + 运行态守卫，
   与"看上下文"是两件事。
4. **不在消息气泡的说话人头上加入口**。同一套面板以后可以挂上去，本次只做花名册。
5. **不做历史曲线**。只看当前占用，不做用量随时间的变化图。
6. **不引入已读/角标**（与本功能无关）。

## 风险

- **`<button>` 嵌 `<button>`**：现有行只有删除按钮，改造后主区域变成按钮，删除必须留在
  外面（本设计已如此）。非法嵌套会让浏览器把内层按钮拆出去，点击行为随机。
- **行高变化**：加 chip 后密集行从 40px 起，窄屏（<360px）可能挤压名字。策略：`.gm-name`
  已有 `text-overflow: ellipsis`，chip 与 tag 都 `flex-shrink: 0`，名字优先被压缩。
- **嵌套抽屉的返回优先级**：两个 BottomSheet 的 priority 是
  `PRIORITY_OVERLAY + instanceSeq * 0.001`，后注册者胜——成员抽屉是后注册的，返回先关它。
  若有人把成员抽屉改成"和群设置共用同一个 BottomSheet"，这条会失效。
- **离场成员的历史值**：`context_state` 还在，接口也会返回，但前端不展示。若以后要展示，
  必须标"离场前的最后值"，不能当成实时值。

## 实施顺序

1. `utils/usageDisplay.ts` + `ContextUsageDetail.vue`，把 `ChatInputBar` 改成消费它
   （纯重构，测试必须全绿）。
2. 后端 `GroupMemberContexts` + handler 字段 + OpenAPI。
3. `GroupMemberInfo` 增加 `usage`/`model`；`GroupSettingsSheet` 行改造。
4. `MemberContextSheet` + 打开/关闭 + 实时刷新。
5. 守卫测试与文档（`docs/spec/features/group-chat.md` 补一节）。

## 实现记录（与初稿的差异）

1. **压缩做了**（初稿列为"不做"）。新增 `POST /api/group/member-compact`，直接驱动成员
   的 ACP 连接发送 `/compact`，状态事件持久化到**成员行**，不在群时间线产生任何输出。
   之所以不能走 `/api/ai/chat`：群回合写群时间线，命令文本和助手回复都会落进讨论，被
   其他成员读成发言。守卫顺序 = 群运行中(409) → 成员不属于/已离场(404) → 非 ACP(400)
   → 该成员已在压缩(409)；3 分钟硬上限；期间用 `markGroupMembersActive` 挡空闲回收。
   实现拆成 `activeMember` / `beginCompaction` / `runMemberCompact` /
   `consumeCompactEvent` 以压住认知复杂度。
2. **类名保留 `usage-popup-*`**（初稿设想改成 `usage-detail-*`）。`ChatInputBar.test.ts`
   有 15+ 条用例按这些类名断言；保留原名让抽取变成零测试改动的纯重构，抽取的验收线
   因此是"252 条测试原样全绿"。
3. **实时刷新挂在成员面板的打开状态上**，不是群设置抽屉。抽屉常开而群回合持续发
   `usage_update`，按抽屉开合监听会让每次发言都触发一次全量拉取。
4. **`ChatInputBar.vue` 的 script 块是纯 JS**（无 `lang="ts"`），不能写 `type` 导入；
   `usageSnapshot` 因此是推断类型而非显式 `UsageSnapshot`。

## 验证

- Go：`TestGroupMemberContexts`（含坏 JSON 降级、离场成员仍可解析）、
  `TestServeGroupMembersCarriesContext`（`usage` 为 `null` 而非 `{}`）、
  `TestCompactGroupMemberGuards`（空 id / 跨群 / 离场 / CLI）、
  `TestCompactGroupMember_RejectsWhileGroupRuns`、`TestServeGroupMemberCompactGuards`
- 前端：`usageDisplay`（阈值 74/75/89/90/94/95、size=0 不除零、未知/空币种、category
  过滤）、`ContextUsageDetail`、`MemberContextSheet`（空态、gating、错误回显、切换成员
  清错误）、`GroupSettingsSheet`（chip / `—` / 离场不可点 / 删除不冒泡 / 压缩后刷新）、
  `usagePanelSingleSource`（面板只有一份实现，且 `ChatInputBar` 不再有行渲染）
- 回归：`ChatInputBar.test.ts` 252 条、`GroupAvatarStack`、`groupSettingsButtonWiring`
- 构建：`npm run build` 通过；`golangci-lint` 对新增文件无告警
