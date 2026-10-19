# 群聊模式合一（主持人 = 可选协调者）设计

日期：2026-10-18
状态：设计待评审

## 背景

群聊现在有两个模式，由建群时是否指定主持人决定（`group_store.go:253`），存进
`context_state.group_mode`（`:410`），运行时在 `runLoop` 里分派（`group_orchestrator.go:340`）：

| | 主持人模式 `runRounds:963` | 自由模式 `runFreeLoop:1339` |
|---|---|---|
| 队列空时 `refill` | 跑一次主持人，解析它的路由标签（`:1016`） | `return nil, false`（结束） |
| 有人发言后 `onSpoke` | **nil**（不接力） | `appendFreeTargets`（`:1758`） |
| 成员能否 @ | 不能（提示词禁止 + 代码忽略） | 能，这是核心机制 |

需求（用户原话）：「把主持模式跟自由模式合一，只不过在选了主持人的时候，会在那个成员和
主持人提示词中注入谁是主持人，然后这个工作要谁来负责主持，让大家都知道就好了。然后最后
再加一轮主持人总结轮。」

主持人的职责被明确为四件事：**开场协调 / 断链推进 / 判断完成 / 总结收尾**。

## 关键事实（已核实）

1. **两个循环已经是同一个内核**。`drainSpeakers`（`:588`）是唯一的调度循环，两个模式只
   传不同的 `refill` / `onSpoke` 两个回调。合并就是把两个回调都接上。
2. **`refill` 不必解析路由**。它只需要返回「接下来该谁说话」的 speaker 列表；主持人说了
   什么、@ 了谁，由 `onSpoke` 统一处理。现在 `refill` 自己解析路由（`:1016`）是历史遗留
   的第二条解析路径。
3. **主持人模式下成员写的 `private` 密送真会送达**。`executeTurn`（`:547`）对每个发言者都
   调 `storeSpeakerNotes`，不判模式；而公开 @ 却因为 `onSpoke=nil` 完全无效。同一句话的两个
   分支一个有效一个无效，用户和模型都推不出来。合并后两者都走同一条路，这个不对称自然消失。
4. **主持人看不到自己的上一条发言**。`buildInjectionText`（`group_inject.go:64`）会跳过
   发言者自己的历史行，所以总结轮从注入里读不到它刚写的结论（ACP 会话里有，但压缩后可能丢）。
   「已总结过就别重复」不能靠模型自查，必须把原文显式塞进提示词。
5. **主持人模式下用户 @ 被完全忽略**（`TestHostMode_IgnoresHumanMention`，`group_free_test.go:745`）。
   合并后保持这个行为：有主持人时用户不参与路由。
6. **巡航的六种收工形态可以整体删除**。现在 `hostTurnOutcome`（`:1064`）要枚举结束标签、
   `@User`、AskUserQuestion、主动声明、无标签散文、大白话提问六种「想收工」的写法，是因为
   `refill` 必须当场判断「这一轮是不是要收工」。合并后**讨论自然结束 = 收工**，巡航只要在
   「循环要结束时」发起确认即可，不需要识别形态。

## 决策汇总

| # | 维度 | 决策 |
|---|---|---|
| 1 | 模式 | **不再有模式**。有主持人 = 多一个协调者；无主持人 = 纯接力。二者共用一条循环 |
| 2 | 主持人定位 | 队列空时被唤醒的接力节点：开场、断链推进、判断完成、总结收尾 |
| 2a | 每轮用户消息的第一句 | **永远是主持人**，且**只做分派**（分工 + 点名），不替成员答题——沿用现有提示词「你的职责是控场，而不是代替成员回答问题」（`group_prompt.go:85`） |
| 3 | 成员 @ | 两种群都生效，走 `appendFreeTargets` |
| 4 | 主持人 @ | 同成员，走同一条通道（不再有专用路由解析） |
| 5 | 用户 @ | 有主持人时忽略（保持现状）；无主持人时是开局种子（保持现状） |
| 6 | 结束信号 | 只有**主持人**的 `<clawbench-group-end/>` 有效；成员写的忽略 |
| 7 | 主持人没点名任何人 | **视为结束**（等价于「没有人需要继续说」），不再有两连败回退 |
| 8 | 主持人回合失败 | 直接以错误结束本轮（时间线可见），不再回退轮转成员 |
| 9 | 发言计数 | `chat.group_max_speeches` 统计**所有发言者**（含主持人、含巡航确认轮） |
| 10 | 总结轮触发 | **每次干净收尾都跑**；提示词带上主持人的上一条原文，已含结论则只确认完成 |
| 11 | 总结轮与上限 | **不计入**发言计数、也不受上限约束（否则撞上限时永远没有结论；计入会让「共 N 轮」多算） |
| 12 | 总结轮接力 | 天然不接力（`runSpeakerTurn` 不经过 `onSpoke`），无需额外门控 |
| 13 | 移除主持人 | **不允许**（保持现状：`ErrCannotRemoveHost`）。主持人只在建群时指定，之后不可变——`mode` 一旦确定就不再变化 |
| 14 | 巡航 | 变成统一循环的**外层包装**：讨论要结束时发起确认轮，未通过则再跑一轮讨论 |
| 15 | 存储 | 删除 `context_state.group_mode`；模式由 `host_member_id` 是否为空派生 |
| 16 | 线上契约 | `GET /api/group/members` 的 `mode` 字段**保留**（派生值），前端大面积改动为零 |
| 17 | 成员提问 | **有主持人的群禁止成员直接向用户提问**：成员提问既不渲染可交互卡片、也不交回话筒；由主持人在下一轮看到后自行转达或决定是否转达 |
| 18 | 单成员群 | 修 `BuildHostSystemPrompt` 的「除 User 外无可点名成员」判断，让它直接回答而不是空转一次 |

## 目标形态

```
runLoop(groupID)
├─ 有主持人：queue = 空  ──┐
│                          └→ refill: 主持人发言（开场分派）
├─ 无主持人：queue = 用户 @ 的成员，或全体成员
│
└─ drainSpeakers
     ├─ 队列非空 → 取出一个 group → 成员依次/并发发言
     │                            └─ onSpoke: 把被 @ 的人排进队尾（所有人都一样）
     └─ 队列为空 → refill
          ├─ 有主持人 且 上一轮主持人点到了人 → 主持人再发言（断链推进）
          ├─ 有主持人 且 上一轮主持人没点到人 → 结束
          ├─ 有主持人 且 它写了结束标签 → 结束
          └─ 无主持人 → 结束
     
     结束后（干净收尾）→ 主持人总结轮（有主持人才跑，禁止接力）
```

时间线上一条用户消息对应的发言顺序：

```
用户 → 主持人（分派：谁上、给什么任务）
     → 成员A → 成员B（A @ B 就接着上）
     → 主持人（断链推进：再分派一批）   ← 可有多次
     → 主持人（结束标签 + 结论）
     → 主持人（总结轮：完整结论或一句确认）
```

**主持人只分派、不代答**（决策 #2a）。

**已知边界：只有一个成员的群。** `CreateGroup` 允许只建「主持人自己」一个成员（
`group_store.go:87` 的 1-spec 路径）。此时 `activeMemberNamesExcept`（`:2117`）会排除主持人
再**追加 User**，所以 `BuildHostSystemPrompt:99` 的 `len(members) == 0` 分支永远不触发——主持人
被告诉「可点名 User」，它就只能 @User 交回用户，等于这一轮什么也没做。合并后这个边界会更明显
（每轮都先问主持人）。

处理建议（本次一并修，约 5 行）：`BuildHostSystemPrompt` 判断「除 User 外没有可点名成员」时走
直接回答分支：

```go
routable := 0
for _, m := range members { if m.Name != groupUserTarget() { routable++ } }
if routable == 0 {
    b.WriteString("当前群内没有其他成员，你无需路由，直接回答用户即可。\n")
    return b.String()
}
```

## 代码落点

### 删除（约 380 行）

| 位置 | 内容 |
|---|---|
| `group_orchestrator.go:963-1062` | `runRounds`（主持人专用循环） |
| `:1064-1118` | `hostTurnOutcome`（六形态识别） |
| `:1120-1148` | `hostAsksUserInProse` + `reverseLines`（只服务于六形态识别） |
| `:1150-1167` | `routeNamesUser`（主持人 @User 改由 `onSpoke` 走通用 @User 路径） |
| `:938-961` | `hostRouteFailure` |
| `:887-905` | `hostFailureFallback` |
| `:1169-1189` | `resolveCruiseSpeakerGroups`（未通过的确认轮改为重新唤醒主持人） |
| `:1211-1265` | `runCruiseConfirmHost` + `cruiseFallbackGroup` |
| `:1630-1647` | `pickFallbackMember`（三个调用方全部删除后无引用） |
| `:1318-1326` | `memberTurn`（主持人路由改走 `freeGroupFromRoute`，不再有专用构造器） |
| `:108-116` | 常量 `maxConsecutiveParseFailures` / `maxConsecutiveHostFailures` |
| 字段 `:78-89` | `parseFailures` / `hostFailures` / `fallbackIdx` |
| `group_prompt.go:139-153` | `BuildMemberSystemPrompt`（教成员「不许输出标签」——合并后成员本来就要输出） |
| `group_store.go:1130-1140` | `GetGroupMode` |
| `group_store.go:1142-1145` | `IsFreeGroup`（唯一调用方是测试；改为 `GetGroupHostMember(sid) == ""` 的派生判断，或直接删） |

保留 `confirmCruiseDone:1267`（它只是 `cruiseSatisfied` 的闩锁，两种群都用）与
`GroupModeHost` / `GroupModeFree` 两个常量（handler 仍要下发派生值 `mode`）。

**保留** `ErrCannotRemoveHost` 与 `RemoveGroupMember:1063` 的守卫、`handler/group.go:327-330`
的 409 分支、前端 `GroupSettingsSheet.vue:82-87` 的禁用态与 `group.hostNotRemovable` 文案：
主持人不可移除（见决策 #13）。

### 改写

**`runLoop:338`** —— 不再分派，统一一条：

```go
func (o *GroupOrchestrator) runLoop(ctx context.Context, groupID string) DrainResult {
    s, err := o.prepareTurn(groupID)
    if err != nil { return DrainResult{Err: err.Error()} }
    defer clearGroupMembersActive(s.activeIDs)

    for {
        res := o.discuss(ctx, s)                 // = 现 runFreeLoop 去掉 prepareTurn/cruise 的骨架
        if res.CancelReason != "" || res.Err != "" { break }
        if !o.cruiseOn() { break }
        if o.capReached { break }
        confirmed, cres := o.confirmRound(ctx, s) // = 现 runFreeConfirmRound + 主持人分支
        if cres.CancelReason != "" || cres.Err != "" { res = cres; break }
        if confirmed { o.cruiseSatisfied = true; break }
        // 未通过：o.ended/o.hostStalled 清零，重新 discuss（有主持人 → 空队列，主持人重新分派）
    }
    // 总结轮：干净收尾就跑（撞上限也跑——上限只闸接力回合）。
    // 提示词自己判断「上一条已含结论 → 只确认」，所以巡航确认轮也走这里，
    // 不会出现两段完整结论。
    if o.hostID != "" && res.CancelReason == "" && res.Err == "" {
        o.summarize(ctx, s)
    }
    // 系统行 + orphan 收尾 + 摘要（保持现有顺序）
}
```

**`prepareTurn` 上移**：现在 `runRounds` 与 `runFreeLoop` 各自调它。上移到 `runLoop` 后，
`discuss`/`confirmRound`/`summarize` 共用同一份 roster 与 `activeIDs`，`markGroupMembersActive`
也只调一次（少一次 DB 查询，且 ACP 空闲清扫的保护窗口覆盖整个回合而不是被重复注册）。

**`refill`** —— 只排主持人，不解析路由：

```go
refill := func(ctx context.Context) ([]speakerGroup, bool, DrainResult) {
    if o.hostID == "" || o.ended || o.hostStalled { return nil, false, DrainResult{} }
    if o.checkSpeechCap() { return nil, false, DrainResult{} }
    o.speeches++            // 主持人的回合计入上限
    o.hostStalled = true    // 假定这一轮点不到人；onSpoke 点到人时清除
    return []speakerGroup{{turns: []speakerTurn{{member: host, sysPrompt: hostPrompt()}}}}, true, DrainResult{}
}
```

`hostStalled` 是**一位闩锁**，作用：

- 主持人漏写标签 → 没有新 group → 下一轮 refill 看到 `hostStalled` 仍是 true → 结束。
  取代现在的「两连败后回退轮转 / 再失败中止」。
- 不会无限循环（主持人发言后队列仍空时，refill 只返回一次）。
- 主持人写了结束标签时，`appendFreeTargets` 直接置 `o.ended`（见下），与闩锁互不依赖。

**注意 refill 的 `ok` 返回值**：返回 `false` 会让 `drainSpeakers:603` 立即返回该 `DrainResult`；
返回 `true` + 空列表会**自旋**（源码注释明确写了）。所以「该结束」必须返回 `false`，不能返回
`true, []`。

**`onSpoke`** —— 所有人共用；主持人额外更新闩锁（结束判断在 `appendFreeTargets` 里）：

```go
onSpoke := func(_ context.Context, sp speakerTurn, outer, appended []speakerGroup, pending []speakerTurn) []speakerGroup {
    g := o.appendFreeTargets(outer, appended, pending, groupID, sp.member.ID, s.members, s.names)
    if sp.member.ID == o.hostID {
        // 主持人的回合已经消费掉闩锁：点到人 → 解除；没点到（或写了结束标签）
        // → 保持置位，下一次 refill 直接结束。
        o.hostStalled = len(g) == 0
    }
    return g
}
```

**`appendFreeTargets:1758`** —— 增加结束标签识别（主持人专属）。注意顺序：`Parse` 对「只写了
结束标签」的输出返回 `Found=false`，所以结束判断必须在 `!Found` 提前返回**之前**：

```go
route := grouprouting.Parse(output)
// 只有主持人的结束标签有效：成员写结束标签会被忽略（防止成员抢收尾权）。
// 必须在 Found 判断之前——只写结束标签时 Found 为 false。
if speakerID == o.hostID && route.End {
    o.ended = true
    return nil
}
if !route.Found { return nil }
```

**`freeGroupFromRoute:1817`** —— 主持人被 @ 时必须拿**主持人提示词**，不是成员提示词：

```go
sysPrompt := o.freeMemberPromptFromRoster(rl.members, rl.names, m.ID)
if m.ID == o.hostID {
    sysPrompt = o.hostPrompt(rl.members, rl.names)   // 主持人身份 + 四职责
}
```

不加这一条，成员 @ 主持人时它会以「我是参与者、不要输出标签」的身份作答——正好与合并后
「主持人要能点名」相反，而且它会以为自己不该收尾。这是合并后最容易漏的一处。

### 成员禁止直接向用户提问（决策 #17）

**目标**：有主持人的群里，成员提出问题后**不出现可交互卡片、也不交回话筒**；问题以普通文本留在
时间线上，由主持人在下一轮看到后决定如何转达（或直接用自己的卡片问用户）。

**现状有三条通路，必须同时掐断**：

1. **XML 标签 → 卡片**：`postProcessBlocks`（`session_executor.go:859`）在
   `Mode == ModeInteractive` 时把 `<clawbench-ask-question>` 转成 `tool_use` 块
   （`ConvertAskQuestionBlocks`，`ai/block_helpers.go:97`），前端据此渲染可交互卡片。而群成员
   回合用的正是 `ModeInteractive`（`group_orchestrator.go:1976`）。
2. **原生工具块 → 卡片**：后端（如 Claude Code）也可能直接发出名为 `AskUserQuestion` 的
   `tool_use` 块，不经过 XML 转换。
3. **交回话筒**：`memberAskedQuestion`（`:1894`）识别上述两种形态后置
   `groupMemberResult.AskedQuestion`，`runSequentialGroup:731` 与
   `handbackIfQuestionAsked:908` 据此 `handUserBack` 并终止本轮。

**做法**：给 `TurnSpec` / `RunConfig` 增加 `SuppressAskQuestion bool`，只在**成员**回合置真
（主持人回合不置真——主持人问用户是合法的，正是「转达」的出口）。

`postProcessBlocks` 在转换之后追加一步降级：

```go
if e.cfg.SuppressAskQuestion {
    blocks = ai.DowngradeAskQuestionBlocks(blocks)   // 新增于 ai/block_helpers.go
}
```

`DowngradeAskQuestionBlocks` 把每个 `Name == "AskUserQuestion"` 的 `tool_use` 块替换为等价的
`text` 块，正文取 `askquestion.PlainText(askquestion.NormalizeInput(block.Input))`（问题 + 选项），
`Done: true`。两个后果：

- 前端没有 `tool_use` 块 → 不渲染可交互卡片（`ContentBlocks.vue:160` 的分支进不去）；
- `memberAskedQuestion` 找不到 `tool_use` 也找不到 `<clawbench-ask-question` → 返回 false →
  不交回话筒、不终止本轮。

**为什么不用「删掉」**：问题是讨论上下文（「A 在问用户什么」会影响 B 与主持人的判断），
`askquestion` 包的契约也是「绝不丢内容」。降级成文本既保住信息，又拿掉交互性。

**为什么在 `postProcessBlocks` 而不是注入层**：注入层（`stripAskQuestionTags:204` +
`renderAskQuestions:137`）已经能把标签渲染成可读散文，但它只影响**注入给其他成员**的文本；
落库内容与前端渲染走的是另一条路。要掐断卡片必须在落库前降级。

**提示词**（`BuildFreeMemberSystemPrompt` 的角色段之后，仅有主持人时追加）：

```
如果你需要用户确认某件事，**不要直接向用户提问**（不要输出提问标签或提问卡片）。
把问题告诉主持人——主持人会替你转达。
```

**主持人的对应段**（`BuildHostSystemPrompt`）：

```
成员可能把需要用户确认的问题交给你。由你判断：能自行决定就自行决定；
确实需要用户拍板时，用提问卡片转达给用户。
```

**不要误伤主持人回合**：`SuppressAskQuestion` 必须**只在成员回合**置真。主持人回合保持
`false`，否则「由主持人转达」这个唯一出口也被堵死。

**流式期间仍会闪一下原始标签**：前端 `renderTextBlock` 的 streaming 分支
（`useChatRender.ts:300-302`）只跑 markdown、不剥 ask 标签。这是全仓既有行为（单聊也一样），
`stream_finalize` 后重载历史即变成降级文本。本次不改。

**不改的地方**：注入层（`stripAskQuestionTags:204` + `renderAskQuestions:137`）保持现状——
它把成员的问题渲染成「[A 向用户提问，等待用户回答] …」给其他成员看，正是「主持人能看到并转达」
的信息来源。降级后那段文本与注入文本内容一致，不会重复（注入读的是落库后的块）。

**`runSequentialGroup:713-731`** —— 主持人失败必须终止本轮。现在只检查 `CancelReason` 与
`AskedQuestion`，成员失败被静默跳过（时间线有一条错误气泡，讨论继续），这对成员是合理的
降级；但主持人失败后 `hostStalled` 会停在 true，下一轮 refill 直接结束——等于「主持人挂了
一次，讨论就结束」，而用户看到的是「讨论莫名停止」。因此：

```go
// 紧跟在 CancelReason 检查之后
if r.Err != "" && t.member.ID == o.hostID {
    return DrainResult{Err: r.Err}, appended, true
}
```

成员失败保持现状（继续下一个成员）。`runParallelGroup` 不需要改：并行组里不可能有主持人
（主持人从不与成员并发，`refill` 每次只排它一个）。

**`summarize:1923`** —— 加原文参数：

```go
func (o *GroupOrchestrator) summarize(ctx context.Context, s groupTurnSetup, hostID string) {
    prev := o.lastMemberOutput(o.groupID, hostID)   // 结束轮 / 确认轮的那条
    o.runSpeakerTurn(ctx, s, hostMember, "", BuildHostSummaryPrompt(name, roster, prev))
}
```

**不需要额外禁止接力**：`runSpeakerTurn:446` 只做 `snapshotTurn` + `executeTurn`，从不经过
`runSequentialGroup`/`runParallelGroup`，而 `onSpoke` 只在这两个函数里被调用——所以走
`runSpeakerTurn` 的回合天然不接力。总结轮只要继续用 `runSpeakerTurn` 就安全（这也是
`summarize` 现在的写法，无需改动这一层）。

**发言计数** —— `memberSpeeches` 改名为 `speeches`，计数点共四处：

| 位置 | 场景 |
|---|---|
| `:719` | 顺序组里的每个成员 |
| `:779` | 并行组按 `len(g.turns)` 批量加 |
| `:1215` | 确认轮（主持人或全体成员） |
| 新增（refill） | 主持人的开场/断链回合 |

`checkSpeechCap` 只在**接力回合之前**调用（`refill` 开头、`runSequentialGroup` 每个成员前、
`runParallelGroup` 批量前）。`summarize` 既不调用它也不计数——总结轮不是讨论回合，
把它算进「共 N 轮」会让完成行多报一次。

### 提示词

**`BuildHostSystemPrompt:83`** 重写。现有文案的前提是「你每轮都必须点名」，合并后要改成
「你只在开场和断链时发言」：

```
[群聊主持人] 你是本群的主持人，负责开场分派、断链推进、判断完成和收尾总结。
你的职责是控场与分工，**不要代替成员回答**——除非群里没有其他可点名的成员。

每一轮用户消息都由你先发言。你在两种时机被唤醒：
  1) 用户刚发来消息（本轮开始）—— 你分派任务，点名接下来该发言的成员；
  2) 没人接话、讨论停住 —— 你决定继续推进还是收尾。
成员之间会互相 @ 接力，因此**一轮里你通常只发言一两次**；没有新任务时不要重复点名。
如果你认为讨论已经充分，输出 <clawbench-group-end/>，并在同一段里写出结论。
注意：如果你这一轮**没有点名任何人**，本轮讨论会直接结束——所以想让讨论继续就必须点名。
不要点名你自己。
```

三条必须保留的约束（都来自现有文案，合并后语义不变）：**控场不代答**（`:85`）、
**不要点名自己**（`:97`）、**结束标签后要写结论**（`:96`）。

同时删掉「成员不要输出 mention 标签」那条（在 `BuildMemberSystemPrompt` 里，整个函数删除）。

**`BuildFreeMemberSystemPrompt:185`** 不能只是「追加一行」——它开头就写着
「[自由群聊] 你是一个多智能体自由群聊中的参与者，**没有主持人**，大家平等对话」，与有主持人
的群直接矛盾。要改成参数化的角色行：

```
有主持人：[群聊成员] 你是本群成员之一。本群由「X」主持——它负责开场分派、断链推进与
          收尾总结；你和其他成员也可以互相 @ 接力，也可以 @「X」。
无主持人：[自由群聊] 你是一个多智能体自由群聊中的参与者，没有主持人，大家平等对话。
```

其余部分（@ 语法、不 @ 即结束、密送）两种群共用。实现上把 `selfName` 之外再加一个
`hostName string` 参数（空 = 无主持人），或新增 `BuildMemberSystemPromptV2` 并在迁移完成后
删除旧的 `BuildMemberSystemPrompt`。这一行是「让大家都知道谁是主持人」的落点。

**主持人自己也要知道**：`BuildHostSystemPrompt` 已声明身份，但合并后主持人会被成员 @ 而
以普通接力节点的形式被唤醒（见「改写 / `freeGroupFromRoute`」），那条路径拿的是主持人提示词，
身份依然在。两条路径共用同一个 `o.hostPrompt(...)` 构造器，避免漂移。

**`BuildHostSummaryPrompt:160`** 增加原文：

```
你上一条回复是：
---
<原文>
---
如果其中已经包含完整结论，本条只写一句完成确认（例如「任务已完成，结论见上」），
不要重复总结；否则写一段完整的结论，汇总各成员观点与你的判断。
不要再输出任何路由标签或结束标签。
```

原文超过约 2000 字时取**最后** 2000 字（结论在结尾；截开头比截结尾安全）。

> 为什么不用「已总结过就跳过整轮」：跳过会让「结束轮没写结论」的情况永远没有总结。让模型
> 在提示词里二选一（只确认 / 完整总结）是唯一能同时覆盖两种情况的形态。

### 巡航

统一成外层包装后，`confirmRound` 按有无主持人分叉（这是数据分叉，不是模式分叉）：

| | 有主持人 | 无主持人 |
|---|---|---|
| 确认轮 | 主持人一人跑一次，带 `BuildCruiseConfirmPrompt(false)` | 全体成员各跑一次，带 `BuildCruiseConfirmPrompt(true)` |
| 裁决 | 它输出 `<clawbench-goal-satisfied/>` 即通过 | ≥ ⌈n/2⌉ 成员声明即通过（`countGoalSatisfied:1477` 原样复用） |
| 未通过 | 再跑一轮讨论（主持人重新分派） | 再跑一轮讨论（`freeDiscussionGroups:1514` 重新 seed） |
| 确认轮没点名也没声明 | 视为未通过（`hostStalled`），再跑一轮讨论；连续两次未通过则结束 |

`GroupCruiseConfirming` 系统行、`GroupCruiseSatisfied` 完成行保持现状。

**注意 `GroupCruiseSatisfied` 的文案**：现在是「任务已确认完成，共 {{.Rounds}} 次发言」，
`Rounds` 取 `memberSpeeches`。改为 `speeches` 后计数含义从「成员发言数」变成「所有发言者的
回合数」，中英文案都要改（「共 N 次发言」→「共 N 轮」）。

## 前端

### 必须改

1. **`GroupModeHint.vue` 文案**。**标签本身不用改**（`group.hostMode` / `group.freeMode`
   仍然准确：有主持人 / 无主持人），要改的是描述与三条 tip——现在的「主持人决定谁发言 /
   随时打断 / 主持人收尾或达上限结束」没提「主持人不常发言、成员会互相接力」。同一批文案在
   `ChatMessageList.vue:334-345` 的空状态里有一份镜像（`groupModeHostDesc` / `Tip1-3`），
   两处要一起改。
2. **`GroupCruiseSatisfied` 文案**：`{{.Rounds}}` 的语义从「成员发言数」变成「所有回合数」，
   en/zh 两条都要改措辞（见「巡航」一节的说明）。

### 不改（有意保留）

- **主持人不可移除**：`GroupSettingsSheet.vue:82-87` 的禁用态、`group.hostNotRemovable`
  文案、`CannotRemoveHost`（`active.zh.yaml:6`）全部保留。合并后主持人仍是群的必要角色
  （没有它就退化成纯接力，而建群时既然选了主持人，就不该中途变成另一种群）。
- **`mode` 字段**：`GET /api/group/members` 继续返回 `host` / `free`（由 `host_member_id`
  派生）。`SessionList.vue:452` 的 `groupModeOf` 本来就靠 `isHost` 推，`GroupAvatarStack`、
  `SessionSearchDrawer` 同理，零改动。
- **并发开关的门控** `v-if="mode === 'free'"`：有主持人时用户 @ 被忽略，这个开关确实无效，
  门控依然正确。
- **`@` 成员卡的密送**：用户仍可给任意成员留 private note，与主持人无关。

## 测试

### 必须改写（编码了旧前提）

| 测试 | 原因 |
|---|---|
| `TestGroupOrchestrator_SpeechCapCountsMembersOnly` | 「只计成员」的前提消失 → 改为「所有发言者都计入」 |
| `TestGroupOrchestrator_SpeechCapSummary` | 主持人回合现在计数，host 轮次数断言要改 |
| `TestGroupOrchestrator_AbortsAfterTwoParseFailures` | 语义变为「一次未点名即结束」 |
| `TestGroupOrchestrator_AbortsAfterRepeatedHostFailure` | 失败不再回退轮转，直接结束 |
| `TestGroupOrchestrator_SuccessfulHostResetsFailureStreak` | 失败 streak 已删除 |
| `TestGroupOrchestrator_FallbackRotates` | 轮转回退已删除 |
| `TestGroupOrchestrator_FallbackMemberGetsMemberSystemPrompt` | 同上 |
| `TestHostGroup_ModeStoredAndRead` | `GetGroupMode` 删除，改为断言 `host_member_id` 派生 |
| `TestGroupOrchestrator_MemberPromptCarriesGroupRole` | 提示词改写（角色描述变了） |
| `TestGroupOrchestrator_SequentialRouting` | `hostTurns != 2` 的断言会变：干净收尾多一轮总结 |
| `TestGroupOrchestrator_UserTargetStopsRound` | 主持人 @User 现在走 `onSpoke` 的通用路径，交回时机不变但实现不同 |
| `TestGroupTurn_SummarizesOnce` | 名字里的「once」现在是「每轮一次」而非「只在撞上限时一次」 |
| `TestGroupOrchestrator_SpeechCapSummary` 的 host 脚本 | 现在每轮收尾都会多一次总结轮，脚本要备足条数（否则落到 `(no script)`） |

### 保持不变（合并后语义仍成立）

`MemberSeesPriorSameRoundSpeech`、`HostNeverRoutesToItself`、`BccOnlyToTarget`、
`BccToUnnamedReachesNobody`、全部 `TestFreeLoop_*`（自由模式行为不变）、全部 BCC 存储测试。

### 新增

| 用例 | 断言 |
|---|---|
| 主持人开场 + 断链唤醒 | 队列空 → 主持人发言；点到 A → A 发言；队列再空 → 主持人再发言 |
| 主持人漏写标签即结束 | 主持人第二次发言不带标签 → 本轮结束，且**不**出现轮转回退成员 |
| 主持人结束标签终止 | 主持人写 `group-end` → 结束；**成员**写 `group-end` → 忽略，讨论继续 |
| 主持人只写结束标签也终止 | 输出只有 `<clawbench-group-end/>`（`Found=false`）→ 仍结束（覆盖 `!Found` 顺序陷阱） |
| 总结轮总是跑 | 干净收尾后主持人被调用两次（结束轮 + 总结轮），总结轮提示词含上一轮原文 |
| 总结轮不接力 | 总结轮输出 `@A` → A 不被排队 |
| 总结轮不受上限约束 | cap 恰好用尽时总结轮仍然运行 |
| 总结轮不计数 | 总结轮不增加 `speeches`（完成行的「共 N 轮」不多算） |
| 主持人不可移除 | `RemoveGroupMember(host)` 仍返回 `ErrCannotRemoveHost`（回归守卫） |
| 每轮先主持人 | 用户发消息后 `order[0] == hostID`（连续两轮用户消息都成立） |
| 主持人只分派不代答 | 提示词含「不要代替成员回答」；且开场轮之后有成员回合（不是主持人自问自答） |
| 单成员群直接回答 | 群里只有主持人时，提示词走「无需路由，直接回答」分支，且不产生 `@User` 交回 |
| 所有发言者计数 | cap=3、主持人 1 次 + 成员 2 次 → 第 4 次被拦 |
| 成员密送在主持人模式下送达 | 与 `TestFreeLoop_PrivateNoteDeliveredToTarget` 同型，但群有主持人 |
| 成员 @ 在主持人模式下接力 | 主持人点 A，A 的输出 @B → B 接着发言（合并前 B 不会） |
| 成员提问不渲染卡片 | 成员回合输出 `<clawbench-ask-question>` → 落库为 `text` 块（无 `tool_use`），`Done: true` |
| 成员提问不交回话筒 | 成员提问后本轮继续（`AskedQuestion=false`），主持人在下一轮被唤醒 |
| 主持人提问仍然交回 | 主持人回合输出提问 → 仍产生 `tool_use` 卡片并 `handUserBack`（防止降级误伤） |
| 原生工具块也被降级 | 直接注入 `Name=AskUserQuestion` 的 `tool_use` 块 → 同样变成文本 |

### E2E

`e2e/specs/group-free-mode.spec.ts` 与 `group-chat.spec.ts` 的主持人路径需要确认：
`acp-mock` 的脚本输出若依赖「主持人每轮必发言」，会挂。合并后主持人第一次发言后若没点到人
就直接结束，mock 脚本要保证开场那条带 `targets`。

## 文档

- `docs/spec/features/group-chat.md`：`### 主持人模式：一轮的推进` 与自由模式两节合并成一节
  「讨论循环」+ 「可选主持人」小节；决策 #47（主持人模式用户 @ 无效）保留但改述为「有主持人
  时用户不参与路由」；决策 #86（密送目标必须是本轮被点名者）需改写——合并后密送会排队到下次
  发言，与自由模式一致。
- `docs/user-guide/group-chat.md`：§5.1 / §5.2 的模式说明改写，新增「主持人不再每轮发言」的
  行为说明。
- `docs/plans/2026-10-04-ai-group-chat-design.md`：在文末追加一节「2026-10-18 模式合一」，
  指向本文档，不修改历史决策（保留演进记录）。

## 不做的事

1. **不做「主持人可切换」UI**。建群时指定，之后不可更改、也不可移除；「给已存在的群加主持人」
   需要新的端点与权限判断，与本功能无关。
2. **不做总结轮的历史版本管理**。总结就是一条普通助手消息，落群时间线。
3. **不改 `parallelDefault` 的语义**。它仍只作用于无主持人时的用户种子。
4. **不迁移存量数据**。旧群的 `group_mode` 字段留着不读不写即可（不主动清理 JSON 键，
   避免迁移风险）。
5. **不合并巡航的两种裁决**（一人拍板 vs 投票）——一个 agent 无法投票，这是能力差异。
6. **不做「主持人降级」**。主持人不可移除，因此不存在「有主持人的群变成无主持人的群」这条
   迁移路径；`mode` 一旦建群确定就不再变化，`host_member_id` 也不会被清空。

## 风险

1. **主持人被架空**。合并后如果成员接力顺畅，主持人只在开场和结尾出现，用户可能觉得「主持人
   没起作用」。缓解：提示词要求它在断链时推进；观察真实群一轮后再决定是否给主持人加「每 N 轮
   强制发言」的节流机制（本次不做）。
2. **主持人漏写标签就结束**。这是把「模型失误」直接变成「讨论提前终止」。现在有两连败兜底，
   合并后没有。缓解：提示词明确写「不点名 = 结束」；真实群观察误终止率。
3. **测试改写期回归网变薄**。73 个用例里 14 个的前提消失，改写期间它们对新代码的保护力打折。
   缓解：先补新增用例，再改旧用例，最后删代码。
4. **总结轮与结束轮重复**。靠提示词「已含结论则只确认」约束，模型可能仍重复。可接受（多一段
   重复优于少一段结论）。
5. **主持人失败路径**。现在失败会回退轮转成员，合并后直接结束。若主持人后端不稳定，整轮会
   比现在更早失败。缓解：错误在时间线可见，用户可重发。
6. **成员提问被降级后主持人可能不转达**。降级只保证「不出卡片、不交回话筒」，是否转达靠提示词
   约束。若主持人忽略，用户会看到一条成员提问但没有可点的卡片。缓解：主持人提示词明确要求
   「能自行决定就自行决定，需要用户拍板时用卡片转达」；真实群观察漏转达率。

## 实施顺序

1. **补新增测试**（上表 20 条），此时它们应当失败——确认测的是新行为。
2. **统一循环**：`runLoop` 单一路径、`refill` 只排主持人、`onSpoke` 全量挂载、`hostStalled`
   闩锁、结束标签归属主持人、主持人被 @ 时拿主持人提示词。删除 `runRounds` 及其六个辅助函数。
3. **总结轮**：`summarize` 加原文参数、改由 `runLoop` 在干净收尾后调用；发言计数改为全体。
4. **成员提问降级**：`TurnSpec`/`RunConfig` 加 `SuppressAskQuestion`，`ai.DowngradeAskQuestionBlocks`
   + `postProcessBlocks` 接线 + 两处提示词；成员回合置真、主持人回合不置真。
5. **单成员群**：`BuildHostSystemPrompt` 判断「除 User 外无可点名成员」时直接回答。
6. **巡航包装**：把 `cruiseFreeRound` 提升为两种群共用的外层循环，删除
   `runCruiseConfirmHost` / `cruiseFallbackGroup` / 六形态识别。
7. **清 `group_mode`**：`GetGroupMode` 删除，`writeGroupModeInline` 不再写该键，handler 派生。
8. **前端文案**：`GroupModeHint` / `ChatMessageList` 空状态两处镜像 + `GroupCruiseSatisfied`。
9. **文档同步**：spec / user-guide / 本设计的决策表回填。
10. **全量验证**：`go test ./internal/service/` + `npm test`（群聊相关文件）+ 新增 e2e。

第 2 步之后就应该跑一次真实群验证观感，再继续第 3–7 步。

## 待确认（实现前需要定）

1. **`speeches` 的显示文案**。`GroupCruiseSatisfied` 的 `{{.Rounds}}` 从「成员发言数」变成
   「所有回合数」，中英文案要改（本设计按「共 N 轮」处理）。若希望保留「只报成员发言数」，
   需要额外维护两个计数器。
2. **`hostStalled` 与用户插话的交互**。用户在讨论中途发消息会开启新回合（新的
   `GroupOrchestrator` 实例），闩锁随实例重建，因此不影响下一回合——但**同一回合内**用户
   排队消息不会重置它。当前设计接受这一点（排队消息本来就在本轮结束后才跑）。
3. **总结轮的失败处理**。总结轮失败时本轮已产出的结论还在时间线上，因此只记 warn 不重试
   （与现在 `summarize` 的 `res.Err` 被忽略一致）。