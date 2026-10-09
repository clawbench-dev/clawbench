# 群聊 @ 成员卡（公开点名 + 密送批注）设计

> 2026-10-08。取代 `2026-10-04-ai-group-chat-design.md` §13.4 的「选中成员插入空体标签」。
> 目标：输入框不再出现 `<clawbench-mention targets="…">` 原始标签；@ 成员做成卡片，
> 卡片可选填「密送」批注（对应引用卡片的批注）。

## 1. 问题

群聊里用 `@` 菜单选中成员后，`buildMentionTag` 把原始标签直接插进 textarea：

```
<clawbench-mention targets="m-b"></clawbench-mention> 
```

用户在输入框里看到一串协议标签，体验差。已发送的气泡没问题（`renderMentionChips`
把它渲染成 `@名字` chip），只有**输入框**难看。

## 2. 关键事实（已核实）

1. **用户 @ 的标签体永远是空的**。`buildMentionTag(memberRowId)` 返回空体标签；
   解析时 `instruction` 只取标签体（`e.content`），所以用户场景下
   `route.Instruction` 恒为空。**不存在「哪部分内容要分进标签」的取舍**。
2. **用户的问题始终是标签外的正文**，后端以 `User: <正文>` 注入成员
   （`group_inject.go:107`）。
3. **用户密送目前完全没实现**（不是本次会砍掉的东西）：
   - `o.userMessage` 只在 `freeInitialTargets` 用一次，且只取 `route.Speakers`
     （`group_orchestrator.go:609`）；
   - 密送落库唯一入口 `storeRouteNotes` 只被 `runSpeakerTurn` 调用（`:303`），
     只有**发言者**走得到，用户不是发言者；
   - 用户气泡的 `groupRouting` 以 `msg.agentId` 为门（`ChatMessageItem.vue:332`），
     用户消息没有 agentId → 解析为空；正文走 `renderMentionChips` 的 private 分支
     直接 `return ''`（`groupRouting.ts:288`）。即用户手打 `private` 标签，
     **内容在自己气泡里也看不见**。
4. 设计文档 #F24 写「密送：成员与用户都能发」——只对成员/主持人成立，用户链路从未接。

## 3. 决策

| # | 决策 |
|---|---|
| 1 | **公开 @ 与密送统一成一种卡片**（不再是内联 `@名字`）。输入框里每个被 @ 的成员是一张卡。 |
| 2 | **卡片本身 = 公开 @（点名让谁发言）**；**卡片上的批注 = 密送（private）**。 |
| 3 | 用户的公开问题由**输入框正文**承担（后端本就以 `User: 正文` 注入）。 |
| 4 | 卡片进输入框上方的附件条（与引用/附件卡同排），发送时序列化成标签。 |
| 5 | 本次**补齐用户密送**（新功能，含后端改动）。 |

## 4. 卡片视觉

与现有 `.chat-file-attachment` 系列（附件卡 / 引用卡）拉开距离，走**「人 vs 载荷」**轴：

```
┌─────────────────────────────┐
│  (●)  Bob          🔒    ×  │
└─────────────────────────────┘
    ↑圆形真实头像     ↑密送标记
```

| 维度 | 附件卡 / 引用卡 | **成员卡** |
|---|---|---|
| 前导 | 文件类型图标（方形 20% 圆角） | **智能体真实头像（圆形）** |
| 文字 | 等宽 `--font-mono` | **比例字体**（人名，不是路径） |
| 底色 | accent 10% / accent 渐变 | **中性 `--bg-tertiary` + 描边** |
| 形状 | `--radius-lg` | **`--radius-full` 胶囊** |
| 竖脊 | 引用卡独占（accent 3px inset） | 不用 |

三个**正交结构信号**（圆形头像 / 比例字体 / 中性底）任何主题下一眼可辨；头像自带
智能体品牌色，身份由它承载，卡片本身不抢色（不靠颜色档位——3/36 主题下 accent 会撞，
见 `session_status_slot_motion_over_hue` 的教训）。

**密送两态**：
- 无密送：中性底 + 描边，锁为**空心**（`Lock` 描边图标）
- 有密送：描边转 accent + 锁为**实心/高亮**

点卡片展开一行输入框填「仅 Bob 可见的内容」（对应引用卡的批注编辑，`QuoteCard.vue` +
`updateStagedQuoteNote`）。密送内容**不印在卡上**，只用锁图标提示（同引用卡用
`MessageSquareText` 图标提示有批注）。

## 5. 数据流

### 5.1 前端状态

新增 `StagedMention`（镜像 `StagedQuote` 形状）：

```ts
interface StagedMention {
  id: string          // 稳定身份（去重/编辑用）
  memberId: string    // 成员行 id（标签 targets）
  name: string        // 显示名（头像/标签渲染）
  agentId?: string    // 头像解析（属性属于 agent，不是成员行）
  backend?: string    // 无自定义头像时的内建图标
  note: string        // 密送内容（'' = 无密送）
}
```

存放：`useChatContext` 的附件域（与 `stagedQuotes` 同层，同样有 per-session 草稿快照
与 `snapshotAttachments`/`restoreAttachments` 参与）。**必须进草稿快照**，否则切会话丢卡。

### 5.2 发送序列化

发送前（唯一转换点，镜像 `materializeQuotes`）把每张卡转成标签文本，追加到消息文本：

```
（无密送）  <clawbench-mention targets="m-b"></clawbench-mention>
（有密送）  <clawbench-mention targets="m-b"></clawbench-mention>
            <clawbench-mention targets="m-b" private>仅 Bob 可见的内容</clawbench-mention>
```

- 位置：正文之前还是之后？**追加在正文之后**，与引用块一致（引用是 APPEND，用户的话在前）。
- 序列化函数放 `groupRouting.ts`（与 `buildMentionTag` 同处，纯函数可单测）。

### 5.3 后端补齐用户密送

> **本节经 2026-10-08 review 修正。** 初版三处判断错误（C1/C2/C3），已在下方标注并给出正解。

当前缺口与修法：

| 缺口 | 位置 | 修法 |
|---|---|---|
| **C1 用户密送目标解析不到** | `group_orchestrator.go:590` | `storeRouteNotes` **只按名字查** `byName[target]`，而用户的标签写的是**成员行 id** → 每条用户密送被当"未知目标"静默丢弃。修法：`storeRouteNotes` 改收 `byID, byName` 两个 map，用 `lookupMember(target, byID, byName)`（`resolveSpeakerTargets`/`appendFreeTargets` 早已这么做） |
| **C2 密送泄漏（三条路径）** | 见下表 | 初版断言"注入边界不用改"**是错的**。决策 #91 的边界清单只覆盖**助手**文本；用户密送进了**用户消息文本**，命中三条从未枚举的路径 |
| **C3 只改门不足以渲染用户密送** | `ChatMessageItem.vue:420` | `msgText` 对非助手角色直接返回 `''`，所以放宽 `groupRouting` 的门后仍解析出空。必须补用户文本来源 |
| 用户密送不落库 | `RunGroupTurnDrain:126-153` | 解析 `o.userMessage` 的 `route.Bcc` 调 `storeRouteNotes`。**注意**：此处先于模式分派，故**主持人模式也会生效**——需显式决定（见 §5.5 I1） |

**C2 泄漏三路径（已逐条验证）**：

| 路径 | 位置 | 现象 |
|---|---|---|
| RAG 索引 | `internal/rag/chunker.go:35-37` | `ExtractTextFromContent` 对 **user 角色直接原样返回**；strip 只在 assistant 分支且 `sessionType==group` 时生效（`:49-51`）→ 密送成为可被**任意会话**检索的向量块 |
| 自动标题 | `internal/service/chat.go:909` | `maybeAutoTitleSessionTx` 用 `ExtractPlainText(content)`（无 strip）→ 密送文字变成会话标题。触发点：`queue_store.go:139,147`（入队）+ `chat.go:680` |
| 分享预览 | `internal/service/session_share_payload.go:173` | `GetSessionMessagesForSelection` 用 `ExtractPlainText` 无 strip |

修法：三处都补「群会话下的用户文本 → `grouprouting.StripProtocolTags`」（与 assistant 分支同一原语）。
**判据**：泄漏测试须分别覆盖三条路径（RAG chunk / 标题 / 分享预览），且**逐个来源单独触发**
（去重/折叠类断言只钉一个来源会假通过）。

**注入边界（成员上下文）不用改**：密送经 `storeRouteNotes` → `group_pending_bcc` → 目标
下次被点名时注入（决策 #97 既有路径），与用户是谁无关。用户消息注入成员时走
`renderMentionsReadable`（`group_inject.go:107`），它本就 fail-closed（private 整体丢弃）。

**送达语义**（沿用 #97）：用户密送给「本轮被点名的成员」→ 该成员本回合就收到
（`runSpeakerTurn` 注入 `pendingBccForTarget`）。给未被点名者 → 存 `group_pending_bcc`，
下次被点名时送达。

**speakerID**：用户无成员行，传 `groupUserTargetID`（`group_store.go:45` 哨兵）。`storeRouteNotes`
的自我密送丢弃判据是 `m.ID == speakerID`，故用户给自己写密送会被丢弃——这是对的（用户读密送
在 UI 卡片里，注入前清账，见 `handUserBack`）。

### 5.5 第二发送路径（I2）—— review 证伪

review 怀疑跨会话发送 `dispatchToTarget`（`useConversationTarget.ts:216-229`）会丢卡。**已证伪（不可达）**：

- 成员卡**只能**由 `ChatInputBar` 的 `@` 菜单创建，而该组件在 `ChatPanelContent` 内（聊天面板可见）。
- `requestTarget` 在聊天面板可见时**直接 return false**（`:126`），picker 根本不打开。
- `PendingRequest` 无 `mentions` 字段，无任何调用方从聊天输入构造请求。

故 `stageMentionIntoDraft` 是死代码，**未实现**（不写投机代码）。**无需改动** `useConversationTarget.ts`。

### 5.6 序列化进文本 vs 结构化通道（I3）

初版以「镜像 `materializeQuotes`」为由——**类比不成立**：引用是结构化 `FileEntry`，**从不
烘焙进文本**（正因当年 enqueue 丢过，见 `quoteItem.ts:371-373`）。序列化进文本可能仍是对的
（标签协议本就是文本契约），但须**另立理由**：标签是既有的文本协议，后端 `grouprouting.Parse`
只认文本；改结构化通道要新增请求字段 + 编排器接新通道，成本更大。**故本次仍走文本**，
但接受 §5.3 的泄漏审计代价。

### 5.4 @ 菜单改造

`ChatInputBar.vue` 的 `fileMenu`（`onSelect`/`buildReplacement`）：
- 成员项不再 `buildReplacement` 插入文本标签 → 改为 `emit('add-mention', member)` 加卡
- `closeOnSelectFor` 保持（成员选完关菜单）

## 6. 影响面

| 层 | 文件 | 改动 |
|---|---|---|
| 前端 util | `web/src/utils/groupRouting.ts` | 新增 `serializeMentionCards`（卡→标签文本）；`buildMentionTag` 保留（后端/e2e 仍在用） |
| 前端状态 | `web/src/composables/useChatContext.ts` | `stagedMentions` + 草稿快照 + add/remove/updateNote |
| 前端组件 | `web/src/components/chat/MentionCard.vue`（新） | 卡片渲染 + 批注编辑 |
| 前端组件 | `ChatInputBar.vue` | 附件条渲染成员卡；@ 菜单改为加卡；发送序列化 |
| 前端组件 | `ChatMessageItem.vue` | 用户消息也解析 `groupRouting`（**含 C3 的 `msgText` 用户分支**） |
| 后端 | `internal/service/group_orchestrator.go` | `RunGroupTurnDrain` 落用户密送；`storeRouteNotes` 改收 `byID,byName`（**C1**） |
| 后端 | `internal/rag/chunker.go` | **C2**：user 分支在群会话下补 strip |
| 后端 | `internal/service/chat.go` | **C2**：自动标题补 strip（群会话） |
| 后端 | `internal/service/session_share_payload.go` | **C2**：分享预览补 strip（群会话） |
| 测试 | 各层 | 见 §7 |

## 7. 测试

- **util**：`serializeMentionCards` 空体/带密送/多卡/去重/转义（名字含引号；**标签体含 `</clawbench-mention>` 的转义**）。
- **util**：序列化 → **Go `Parse` 往返**（Speakers + Bcc 都对）。
- **useChatContext**：`stagedMentions` 增删改 + 草稿快照往返。
- **ChatInputBar**：选成员加卡不插文本；发送序列化正确；切会话草稿保留。
- **MentionCard**：两态视觉（有/无密送）；批注编辑。
- **ChatMessageItem**：用户消息的密送卡片渲染（现有 `ChatMessageItem.group.test.ts` 已有 host 版，补 user 版；**须覆盖 C3**——否则会假通过）。
- **Go（C1）**：id 目标的用户密送被落库并送达（当前会静默丢弃）。
- **Go（C2 泄漏）**：RAG chunk / 会话标题 / 分享预览**三条分别**断言不含密送文字。
- **Go（I1）**：主持人模式下用户密送的行为（按 §5.5 决定）。
- **I2 已证伪**：不可达，无需测试。
- **e2e**：`group-free-mode.spec.ts` 的用户 @ 路径目前直接 POST 标签文本（不走 UI），
  保持可用即可；**新增** UI 路径的 spec（选成员→加卡→发送→成员发言）。

## 8. 不做 / 边界

- **不删** `buildMentionTag`：后端/测试/e2e 仍以标签文本为契约。
- **不改**已发送气泡的既有渲染（`renderMentionChips` 已正确）。
- **名字带空格 / 重名**：卡片走 memberId，无内联方案的歧义问题（这正是卡片优于内联处）。
- **I1 已决**：用户密送**两种模式都生效**——密送是私密载荷通道，与「谁路由」（决策 #47 的 @）
  正交，用户应能私下交代成员，与谁当主持人无关。
- **I4 待决**：被 @ 的成员已离场 → `freeInitialTargets` 回退到**全体成员**（`:610-612`），
  行为意外，本次不处理但需记录。
- **I5 已澄清**：`speakerID = groupUserTargetID`（见 §5.3）。

## 9. 实施结果（2026-10-09）

全部落地，测试与变异验证通过。**实施中额外发现并修复**：

1. **失败发送会写回原始标签**（自查发现）：`sendMessage` 的两个 catch 分支原先 `restoreInput(inputText)`，
   而 `inputText` 现在是**序列化后**的文本（含标签）。改为 `restoreInput(rawText)`——恢复用户原文，
   否则发送失败时输入框会冒出原始标签（正是本功能要消除的）。守卫测试已同步反转。
2. **e2e 选择器**：新 spec 用 `[data-action="session"]` 稳定 hook，而非 `.chat-action-btn').first()`
   （后者会命中消息级按钮，见下）。

**判归属时发现的两个既有失败（均已在 clean HEAD `eb68bac68` 上复现，非本功能引入）**：

| 失败 | 根因 |
|---|---|
| `e2e group-chat-ui.spec.ts`（跑在其它 spec 之后） | 共享 `ChatPage.openSessionList()` 用 `.chat-action-btn').first()`：会话有消息时首页是**消息级**按钮，点错元素。**已修**：helper 改用 `[data-action="session"]`（4 个 spec 共用）。修后 `group-bcc + group-chat-ui`（clean HEAD 必红）转绿，全量 group spec 10/10。 |
| `go test TestLegacyProjectPathIndexes_AreAllRecreated`（在 `.worktrees/` 下运行） | 该测试 walk 自身模块根但**排除任何含 `/.worktrees/` 的路径**（本意跳过兄弟 checkout）；worktree 根就在 `.worktrees/` 下 → 全被排除 → 0 个索引。clean HEAD 的 `.worktrees/` 里同样失败。 |

**变异验证**（每个都先确认变异真落地再跑）：
- `serializeMentionCards` 的 note 消毒：去掉 → 2 测试红
- `stagedMentions` 快照恢复：去掉 → 4 测试红
- C3 解析门：还原成「只看 speaker」→ 1 测试红
- C1 目标解析：还原成按名查 → 1 测试红
- C2 三条泄漏路径：换成 `StripEndTag`（不去 private）→ 5 测试红（三条各覆盖）
- e2e：还原 @ 菜单插入标签 → spec 红（`unexpected value "<clawbench-mention …>"`）

## 10. 第二轮子智能体 review（2026-10-09）与整改

第一轮实施后派子智能体 review，查出 1 Critical + 2 Important + 3 Minor，**全部已修并变异验证**：

| # | 严重度 | 问题 | 修法 | 变异判据 |
|---|---|---|---|---|
| C-1 | Critical | **AI 自动改名泄漏密送**（第 4 条泄漏路径，§5.3 表遗漏）：`CollectSessionUserMessages` 与两处 `ScheduleAutoRename(...ExtractPlainText(content))` 未 strip，AI 标题模型会读到 `private` 正文，把本地标题的 strip 又撤销了 | `CollectSessionUserMessages` 内按会话类型 strip（覆盖 DB 行 + extraText 两个来源） | 换成 `StripEndTag` → 2 测试红 |
| I-1 | Important | **发送失败静默丢卡**（我引入的回归）：`clearAll()` 先清卡，catch 只恢复 `rawText`（不含标签）⇒ @ 意图与密送消失无痕。**改前**标签在文本里，故 `restoreInput(inputText)` 能恢复 | 发送前快照 cards，失败时 `restoreStagedCards` 一并恢复 | 把 `restoreStagedCards` 改成 no-op → 1 测试红 |
| I-2 | Important | **用户密送卡显示成员行 id**（`发给: <uuid>`）：`bccEntries` 从不解析 targets（agent 写名字，用户写 id） | `bccEntries` 加 `targetLabels`，用共享 `resolveMentionDisplayName`（id 优先再名字，与内联 chip 同规则） | 还原成 `e.targets` → 1 测试红 |
| M-1 | Minor | **`storeRouteNotes` 接受了离场成员**：`byID` 含离场成员（只有 `byName` 排除），而该路径无 `m.Left` 检查 ⇒ 永不可送达的密送无限累积 | 显式拒绝 `m.Left`（保留用户哨兵） | 删掉该检查 → 1 测试红 |
| M-2 | Minor | `GetLastUserMessageMeta` / `GetSessionFirstMessage` 也未 strip | **不改**：均为单用户会话内读取（不跨成员），且改 `GetLastUserMessageMeta` 会牵动 `session_runtime` 广播载荷；记录为已知低危面 | — |
| M-3 | Minor | `serializeMentionCards` 不转义 `memberId` | **不改**：member id 由应用生成（uuid），不含 `"`；记录 | — |

**结论**：C-1 说明 §5.3 的「三条泄漏路径」清单仍不完备——**教训：枚举泄漏面时必须把「AI 摘要/改名」这条独立于本地标题的路径也纳入**（本地标题 strip 了，AI 改名仍会把原文喂给模型）。

**第二轮变异验证**（同上，先确认变异落地）：C-1 → 2 红；M-1 → 1 红；I-1 → 1 红；I-2 → 1 红。
