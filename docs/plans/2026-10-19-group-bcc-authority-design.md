# 主持人模式下的密送权限（成员间禁止互发）设计

日期：2026-10-19
状态：已实施

## 背景

群聊有主持人模式和自由模式两种。密送（BCC，`private` 提及）是「只给指定目标看」的
私密载荷通道。此前两种模式对密送一视同仁——**任何人可以密送给任何人**，包括主持人
模式下的成员互相密送。

用户要求明确：

> 主持人模式就是不应该互相密送，因为主持人是权威。成员间可以互相 @，但不触发调度，
> 基本保持现状。

于是规则定为：**主持人模式下，两个普通成员之间不得互发密送**；其余组合保持开放。

## 为什么只禁「成员 ↔ 成员」

主持人模式的本质是**主持人独占调度权与共享记录**。据此逐个组合判断：

| 组合 | 是否允许 | 理由 |
|---|---|---|
| 主持人 → 成员 | 允许 | 主持人单独交代一个人，是它的职责（发词、点人先说的前提） |
| 成员 → 主持人 | 允许 | 向上汇报：把只有主持人该知道的信息告诉主席，不让其他成员看到 |
| **成员 → 成员** | **禁止** | 主持人在共享记录里看不到的私下协调。这正是主持人模式要防的「背着主席串联」 |
| 成员 → 用户 | 允许 | 用户地位高于主持人；向人类私下报告不算「背着主席」 |
| 用户 → 任何人 | 允许 | 人类不受主持人约束 |

这条规则可以概括成一句话：**主持人模式下，密送不能绕过主持人**——要么发给主持人，
要么由主持人发出，要么是用户发起的。

### 为什么不改成「必须涉及主持人」

最初的实现把「成员 → 用户」也禁掉了（要求每条密送都涉及主持人）。实现时发现这与
「用户高于主持人」冲突：成员向用户报告一件事，本来就不需要经过主持人。于是收窄成
只禁成员↔成员——这是**唯一**会让主持人失去可见性的组合。

### 为什么在写入时拦截，而不是在投递时

`storeRouteNotes` 是密送落库的唯一入口。在这里丢弃意味着**这条记录从未存在**：

- 不存在「某条投递路径忘了检查」的漏洞；
- 不会占用 `group_pending_bcc` 的空间；
- 不会因为后来加了新的投递路径而复活。

如果放在投递侧（`pendingBccForTarget`），一条被禁止的密送会先落库，然后依赖每一个
读取方都记得过滤——这是典型的「靠调用方守规矩」。

## 顺带修掉的问题

这次核对代码时发现密送链路上有五个既有问题，一并修了。

### 1. 注入文案硬编码「主持人密送给你」

`group_inject.go` 的 `appendHostDirectives` 无条件写「主持人密送给你（其他成员看不到）：」。
但自由模式下 A 给 B 的密送也走这条路，用户自己的密送也是——**B 会看到「主持人密送给你」
而实际上是 A 或用户发的**。署名是错的，而且 `group_free_test.go` 曾把这个错误字符串
当成「区分密送与非密送」的判据，等于把 bug 写成了契约。

修法：`group_pending_bcc` 增加 `sender_role` / `sender_name` 两列，落库时记录发送者身份，
投递时由 `pendingBccHeader` 渲染：

| sender_role | 渲染 |
|---|---|
| `host` | 主持人密送给你（其他成员看不到）：… |
| `member` | `<名字>` 密送给你（其他成员看不到）：… |
| `user` | `<用户昵称>` 密送给你（其他成员看不到）：… |
| `''`（升级前的旧行） | 主持人密送给你…（保持历史文案，旧笔记不变成无名氏） |

`joinPendingBcc` 从「拼接内容」改成「每条带自己的署名行再拼接」，`appendHostDirectives`
不再加任何固定前缀。

### 2. 密送队列无上限

`addPendingBcc` 不限制条数。一个「在场但一直没被点名」的成员可以无限堆积密送
（`storeRouteNotes` 已经丢掉未知/离场/自我目标，所以这是唯一的积压路径，但它是真的）。

修法：`maxPendingBccPerTarget = 64`，超限时**丢弃最旧的若干条**并写 warn 日志。
选最旧而不是拒绝新的：过期的指令价值低于刚写的，而且拒绝新条目会丢失用户最新的意图。

> **淘汰量必须是 `count - cap + 1`，不是恒为 1**（review 修正）。只丢一条只能在
> 「恰好等于上限」时守住线：队列一旦**已经超限**就永远降不回来。这不是假想——上限是
> 本次改动引入的，**升级前的老库没有任何上限**，可以直接带着几百条进入新版本；并发写入
> 的越界同理。因此改成按差额淘汰，且 COUNT / DELETE / INSERT 三步放进**同一个写事务**
> （`store.WriteBegin`），既保证收敛也消除「读池看到过期快照导致少淘汰」与「并发 add
> 互相穿插」两种竞态。

### 3. 删除项目不清理 `group_pending_bcc`

`DeleteProjectData` 的两张删除表清单都不含这张表，而它只有 `group_id`、没有
`project_id`，所以 `historyPredicate` 也匹配不到。**删除项目后密送行永久残留**——
只有 `HardDeleteSession`（硬删单个会话）和归档会话回收器会清，两者都不在删除项目的
路径上。

修法：在 `sessionChildren` 里加一条按 `group_id IN (SELECT id FROM chat_sessions
WHERE project_id = ?)` 的删除。`TestDeleteProjectData_RemovesAllProjectScopedRows`
补了种子行与断言，变异验证确认删掉这条 SQL 测试必红。

### 4. `deletePendingBccForGroup` 是死代码

注释写「group archived/deleted 时清」，但零调用——实际清理走 `HardDeleteSession` 与
`session_queries.go` 的归档回收。删掉了这个函数，避免后来者以为归档路径已经覆盖。

### 5. 注入路径全量读时间线

`snapshotTurn` → `groupInjectionText` → `GetMessagesBySessionIDRaw(groupID)` 这条链
**没有 id 过滤**，把整条群时间线（含每条消息的完整 content JSON）读进内存，然后
`buildInjectionText` 再逐条 `if m.ID <= cursor continue` 丢掉。一个成员回合会触发
最多四次这样的全表扫描（注入、refill 解析主持人路由、`lastMemberOutput`、
`memberAskedQuestion`），并行组再乘 N。

修法：
- 新增 `GetMessagesBySessionIDRawAfter(sessionID, afterID)`，注入路径只取 `id > cursor`；
- `groupInjectionTextWithRoster` 接收 `prepareTurn` 已加载的 roster，去掉每个成员回合
  重复的 `ListGroupMembers`；
- 旧的 `groupInjectionText` 变成零调用，删除。

## 实现落点

| 位置 | 改动 |
|---|---|
| `group_orchestrator.go` `noteAllowed` | 新增：按模式判定某个 (speaker, target) 组合是否允许 |
| `group_orchestrator.go` `notePolicy` | 新增：返回发送者身份（role/name）与准入函数 |
| `group_orchestrator.go` `storeRouteNotes` | 加 `notePolicy` 参数，不通过则丢弃并记 info 日志 |
| `group_orchestrator.go` `storeSpeakerNotes` / `storeUserNotes` | 传各自的 policy |
| `group_orchestrator.go` `joinPendingBcc` | 每条密送加署名行 |
| `group_orchestrator.go` `pendingBccHeader` | 新增：按 role 渲染署名 |
| `group_inject.go` `appendHostDirectives` | 不再写死「主持人密送给你」 |
| `group_store.go` `addPendingBcc` | 加 sender 参数 + 队列上限（丢最旧） |
| `group_store.go` `pendingBccForTarget` | 读 sender 列 |
| `group_store.go` `pendingBcc` | 加 `SenderRole` / `SenderName` 字段 |
| `group_store.go` `deletePendingBccForGroup` | 删除（死代码） |
| `database.go` DDL + 迁移 | `group_pending_bcc` 加 `sender_role` / `sender_name`，并对存量库补列 |
| `project_delete.go` | `sessionChildren` 加 `group_pending_bcc` 清理 |
| `chat.go` | 新增 `GetMessagesBySessionIDRawAfter` |
| `group_inject.go` | 注入改用增量查询 + 复用 roster |
| `group_prompt.go` | 主持人/成员/自由模式三处提示词说明新规则 |

### 迁移

`group_pending_bcc` 的 CREATE TABLE 在存量库上是 no-op，所以新列必须单独 ALTER。
沿用 `chat_history.agent_id` 的做法：`pragma_table_info` 查列存在性，缺则 ADD COLUMN。
存量行的 `sender_role` 是 `''`，`pendingBccHeader` 对它回退到「主持人密送给你」——
升级前写下、升级后才投递的密送仍然读得通。

## 提示词

规则必须让模型知道，否则它会写一条被静默丢弃的密送，然后以为生效了。

**主持人**（`BuildHostSystemPrompt`）在密送段末尾加：

```
成员之间不能互发密送——需要让某人知道什么，由你转达或公开说明。
```

**主持人模式的成员**（`BuildMemberSystemPrompt`）明确它唯一的私密出口：

```
若你有话只能让主持人或用户看到，用密送（给标签加 private 属性，targets 写主持人「Codebuddy」或用户「User」）：
  <clawbench-mention targets="Codebuddy" private>只给主持人看的话</clawbench-mention>
不要给其他成员发密送——成员之间不传递私密信息，会被系统丢弃。
```

> **主持人名必须具体**（review 修正）：`targets` 是按**显示名**解析的，所以占位符
> `targets="主持人名"` 会被模型原样照抄，解析不到任何成员，密送被静默丢弃——等于这条
> 提示词不可执行。因此 `BuildMemberSystemPrompt` 增加 `hostName` 参数，由
> `groupTurnSetup.hostMemberID`（`prepareTurn` 每回合取一次）解析成名字注入。自由模式
> 没有主持人，传空串即整段省略（那里由 `BuildFreeMemberSystemPrompt` 说明无限制）。

**自由模式的成员**（`BuildFreeMemberSystemPrompt`）说明这里没有限制：

```
本群没有主持人，你可以密送给任何成员或用户。
```

## 公开 @ 不触发调度（保持现状，不改）

用户同时确认：主持人模式下成员可以互相 @，但**不触发调度**。这已经是当前行为，无需改动：

- 注入侧 `renderMentionsReadable` 把成员的 @ 渲染成可读的 `@B 内容`，所以其他成员**看得见**
  这段交流；
- 调度侧 `drainSpeakers(..., onSpoke=nil)`（`runRounds`）意味着被 @ 的人**不会**被排进队列；
- 前端气泡上方的 `@ B` 摘要照常显示。

一个已知的观感问题：成员 @ 了不存在的名字时，自由模式会写「X 点名了不存在的成员」系统行，
主持人模式不会（那条路径在 `appendFreeTargets` 里，主持人模式下 `onSpoke` 是 nil）。
这属于「成员 @ 只是评论」的必然结果，本次不改。

## 测试

| 用例 | 断言 |
|---|---|
| `TestNoteAllowed_HostModeForbidsMemberToMemberOnly` | 七种组合的真值表；唯一 false 是成员↔成员 |
| `TestNoteAllowed_FreeModeAllowsEveryPair` | 自由模式全放行 |
| `TestHostLoop_MemberNotePolicyEnforcedEndToEnd` | **真实回合**：成员给同伴的密送不落库、不进入同伴注入；给主持人的落库并按成员名署名（变异：`storeSpeakerNotes` 不传 policy 即红） |
| `TestStoreRouteNotes_HostModeDropsMemberToMember` | store 层：A→B 不落库；A→主持人落库且署名 A |
| `TestStoreRouteNotes_HostNoteIsStampedAsHost` | 主持人的密送 role=host |
| `TestStoreUserNotes_HostModeUserMayBriefAnyMember` | 用户→成员在主持人模式下落库，role=user |
| `TestPendingBccQueueIsBounded` | 超 64 条丢最旧，最新保留 |
| `TestPendingBccQueueConvergesFromOverflow` | **已超限**的队列（升级/并发越界）一次写入即收敛到 64，且按最旧优先淘汰 |
| `TestPendingBccStoresSenderAttribution` | sender_role / sender_name 正确往返 |
| `TestJoinPendingBcc_AttributesEachSender` | 三种 role 各自的署名文案 |
| `TestJoinPendingBcc_LegacyRowFallsBackToHost` | 空 role 回退到历史文案 |
| `TestBuildMemberSystemPrompt_NamesTheHostForPrivateNotes` | 成员提示词给出**真实主持人名**，不含 `主持人名` 占位符 |
| `TestBuildMemberSystemPrompt_NoHostOmitsPrivateChannel` | 无主持人时整段省略，不留悬空占位符 |
| `TestFreeLoop_PublicMentionNotDeliveredAsNote` | 公开 @ 不进密送通道（判据是队列为空 + 密送头形状，不是「主持人密送给你」） |
| `TestDeleteProjectData_RemovesAllProjectScopedRows` | 补种子 + 断言，删掉新 SQL 必红（已做变异验证） |

## 文档同步

- `docs/spec/features/group-chat.md`：密送条目改为「主持人模式：主持人 ↔ 成员双向、
  用户不受限、成员之间禁止；自由模式无限制」，并记录署名与队列上限。
- `docs/user-guide/group-chat.md`：向用户解释为什么成员之间不能互发密送，以及成员能密送给谁。
- `docs/plans/2026-10-04-ai-group-chat-design.md`：决策 #86（「密送目标必须是本轮被点名者」）
  与实现不符（实现是 #97 的「延迟送达」），标注为已被 #97 取代；补一条 #99 记录本次权限规则。

## 不做的事

1. **不改自由模式**。那里没有主持人，成员互发密送是正常能力。
2. **不加「成员→用户」的限制**。用户高于主持人，成员向人类报告不需要经过主席。
3. **不给公开 @ 加调度**。用户明确要求保持「不触发调度」。
4. **不把丢弃变成报错**。密送被模式禁止时静默丢弃（只记日志），因为提示词已经说明规则；
   对模型报错只会让它反复重试。
