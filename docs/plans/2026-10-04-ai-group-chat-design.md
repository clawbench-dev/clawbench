# AI 群聊设计（Group Chat）

日期：2026-10-04
状态：**v1 基线（阶段 A–M）已合入 main（`aa88f921a`）；阶段 N/O 全部实现并合入 main（24/24 Task）；§14 并发发言阶段 1–4 全部实现**

---

## 0. 实现清单（原「未实现清单」，2026-10-07 已全部完成）

> **本节已完成**：阶段 N（N1–N5）与阶段 O（O1–O19）的 **24 个 Task 全部实现并合入 main**
> （实施细节与提交见 `2026-10-04-ai-group-chat-plan.md`）。下面保留当初的「未实现证据」作为
> 起点对照——每条证据点均已消除。**不要再把本节当成待办清单。**
>
> **一处后续回退（2026-10-07）**：**O9（#59 发言中高亮）已按用户要求移除**。头部头像栈上
> 的「正在输出的成员」边框动效被认为观感奇怪，故删掉 `activeSpeakerId` 全链路
> （`AvatarStack` 的 `.is-speaking` 环 + 脉冲、`GroupAvatarStack`/`App.vue` 的 prop 透传、
> `useChatStream` 的写点、`useSessionIdentity` 的 ref）。`stream_start.agent_id` 后端信号
> 保留（仍用于流式行归属），只是不再驱动任何高亮。O9 其余判定不受影响。

### 阶段 N（决策 #39–#44）：成员可见性与描述

| Task | 决策 | 未实现证据 |
|---|---|---|
| N1 | #40/#43/#44 `chat_history.role='system'` + 整表重建 | `database.go` 无 `rebuildChatHistoryRoleCheck`、无 `'system'`；CHECK 仍只有 `('user','assistant')` |
| N2 | #39/#41 成员描述注入三处 + 离场标注 | `group_prompt.go:12` 入参仍是 `[]string`（只有名字）；`activeMemberNamesExcept` 只返回 `m.Name` |
| N3 | #40 增删成员写系统事件 | `handler/group.go:148,172` 直接调增删，**不写时间线** |
| N4 | #43 前端系统事件渲染（居中细条） | 前端无 `role==='system'` 分支 |
| N5 | #42 拒绝移除主持人 | `RemoveGroupMember`（`group_store.go:341`）**无 host 守卫** |

### 阶段 O（决策 #45–#70）：排队、生命周期与降级

| Task | 决策 | 未实现证据 |
|---|---|---|
| O1 | #45/#46/#58/**#72** 群消息入队 + 复用 drain + 推送 | `handler/chat.go:323` 群分支在 `TryClaimSessionRun`（`:494`）**之前**就 return；无群 drain；`emitGroupTerminal:178` **不调 `EmitSessionPushNotification`**（切走后收不到通知） |
| O2 | #48 级联删成员行 / 离群刷 `updated_at` | `HardDeleteSession` 只删群行；`RemoveGroupMember` 无 `updated_at` |
| O3 | #51 成员失败复用 `failTurn` | `group_orchestrator.go:158` 仅 `slog.Warn` |
| O4 | #55 群聊跳过摘要推荐 | `session_executor.go:1672` 无群判断 |
| O5 | #56 回退轮转 + 连续失败收尾 | 回退轮转曾恒选第一个；无失败计数。**已修**：`pickFallbackMember`（轮转）+ `hostFailures`/`parseFailures` 计数 |
| O6 | #50 智能体重名收敛 | `agent_store.go` 无 `AgentNameTaken` |
| O7 | #57 归档/销毁群关成员连接 | `chat_session.go` 只 `CloseConn(群行 id)` |
| O8 | #52/#74 隐藏回溯入口 + 拒绝 fork 群 | 前端未隐藏 rewind；`ServeForkSession` 无群守卫（`ForkSession` 硬编码 `'chat'`） |
| O9 | #59 发言中高亮 | 无 `activeSpeaker`/`isSpeaking` |
| O10 | #61 群 auto-approve 批量写成员 | `chat_session.go:583` 只写群行；无 `SetGroupAutoApprove` |
| O11 | #62 `isGroupSession` 用会话类型 | `GET /api/ai/chat` 响应**无 `sessionType`**；前端靠名单非空 |
| O12 | #64 项目计数排除成员行 | `project_registry.go:70`、`chat.go:1013` 无 `session_type` 条件 |
| O13 | #65/#66 群消息附件注入 | `handler/chat.go:324` 仍 400 拒绝群附件 |
| O14 | #67/#68 剥离主持人标签 + 指令去重 | `grouprouting.Result` 无 `Before` 字段；`buildInjectionText` 不剥标签 |
| O15 | #69/#70 失败不推游标 + warning 不注入 | `group_orchestrator.go:120,157` 先推游标后判错；`group_inject.go:32` 不排除 warning |
| O16 | #71 终态前兜孤儿流式行 | `emitGroupTerminal:178` 只发事件，不 finalize 流式行 |
| O17 | #73 保护群讨论中的成员连接（= 未落地的 I4(a)） | 群只对群行 `SetSessionRunning`；成员行从不标记 running ⇒ sweep（`acp_pool.go:419`）会杀正在用的成员连接 |
| O18 | #75/#76 IM 机器人支持群会话 | `session_command.go:46,71` 硬编码 `session_type='chat'`（群在 IM 列表缺席）；`sendMessageToSessionFromPush:182` 走单聊回合（对群发消息会退化） |
| O19 | #78 群成员数上限 10 | `CreateGroupWithMembers` 只校验 `len(specs)==0`；`AddGroupMember` 无个数校验；连接池无上限 |

### 已实现（勿重复设计）

- **群会话级设置隐藏**（#60）：`ChatInputBar.vue:33,316` 已按 `isGroupSession` 隐藏 model/mode/auto-approve/usage；`ChatMessageItem.vue:144,149` 隐藏 fork。
- **成员 resume**（Task D1）：`group_member_request.go` 已实现。
- **群时间线渲染/路由卡片/头像条**（J/K 阶段）：`GroupMemberSheet.vue`、`groupRouting.ts` 等已存在。

### 已核实安全（非缺口）

`context_state` / 未读 `last_read_at` / `activeStreams` / auto-title / RAG 索引（详见 §12.7(15)）。

---

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
| 21 | 多发言者 | 主持人**可一次点名多个成员**（`<clawbench-mention targets="A,B,C">…</clawbench-mention>`，标签名见 §13.2）；v1 按顺序依次发言 |
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
| 39 | 离场成员的注入标注 | 注入上下文里离场成员名后加 **"（已离场）"**；主持人提示词的**可点名名单**同时区分"在场 / 已离场" |
| 40 | 成员增删的系统事件 | 增删成员时写一条 **`role='system'`** 的消息到群时间线（如"XX 加入了讨论"）；**需整表重建** `chat_history` 以放宽 `role` 的 CHECK |
| 41 | 成员描述注入（三处） | 成员的专业描述（`Agent.Specialty`）注入 **①主持人提示词**、**②系统事件**、**③成员自己的注入上下文**（见 §5.2） |
| 42 | 主持人被移除 | **拒绝**：`RemoveGroupMember` 遇 `memberID == host_member_id` 直接报错（决策 #53 修订：**不可转移**，故无"先转移"路径） |
| 43 | 系统事件的消息归属 | `role='system'`，**`agent_id=''`**（非成员发言）；前端按系统事件样式渲染（居中细条，非气泡） |
| 44 | 系统事件是否进游标 | **计入**（`id > cursor` 即注入）——成员/主持人都要知道谁进出了；但**不计入未读数**（未读只数 `role='assistant'`，无需改） |
| 45 | 群回合运行中人类发消息 | **排队**（复用单聊语义）：群委派移到 `TryClaimSessionRun` **之后**，忙碌时入队；群回合复用现有 `RunDrainLoop`（把群回合当一次 turn） |
| 46 | 排队条目的操作按钮 | **只给「停止并发送」，但文案显示为「插话」**（行为=取消本轮+用该消息起新一轮）；**「加入本轮」不提供**（群回合是多轮循环，无单一在跑回合可注入） |
| 47 | 人类 @ 成员 | **v1 不做**。人类一律排队，想指定发言人时由**主持人**下一轮自行决定；不引入第二套路由入口 |
| 48 | 群删除/归档的成员行 | **删除→级联硬删成员行**；**归档→只归档群行，成员行不动**（传递性隐藏，避免 `archived` 列同时表示"已离场"与"群已归档"）；`RemoveGroupMember` **刷新 `updated_at`**（防保留期把"离场"当"过期"硬删） |
| 49 | 新成员"补课" | **全量注入**（现状）：新成员 `seen_cursor=0` → 首次发言注入全部群历史 |
| 50 | 智能体重名 | **全局禁止**：收敛到单一函数 `AgentNameTaken(name, excludeID)`，由 `SaveAgent`（创建/复制）与 `PatchAgentFields`（改名）调用；命中即**报错**（含复制路径）。仅比对**其他 id**，否则内置后端重注册会自我拒绝 |
| 51 | 成员失败呈现 | **完全复用单聊 `failTurn`**（warning block、`role='assistant'`、带成员归属 `SpeakerID`）；不在群路径另造 `role='system'` 错误消息 |
| 52 | 群聊回溯（rewind） | **v1 不支持**：前端群会话**隐藏回溯入口**（成员游标单调递增，回溯会致成员失忆） |
| 53 | 主持人可否更换 | **不可换、不可移除**：建群时指定即固定；`SetGroupHostMember` 仅创建路径使用，不暴露端点 |
| 54 | 成员"离线"状态 | **不引入**：连接不可恢复时 AI 层已自动开新会话并发可见警告（`acp_backend.go:87`），群时间线据此暴露；不新增状态机 |
| 55 | 摘要与推荐 | **群聊跳过**：成员回合 Finalize 不跑 `triggerChatSummarization`；**只在群回合整轮结束时跑一次**（否则 N×轮数 次 LLM 推荐调用 + 全表摘要扫描） |
| 56 | 路由解析失败的回退 | **轮转指针选下一个未发言成员** + **连续失败 2 次自动收尾**（现状是永远选第一个且永不收尾） |
| 57 | 归档/销毁群的成员连接 | **显式关闭**：遍历 `ListGroupMembers` 逐个 `CloseConn(memberRowID)`（群行 id 关不到成员连接） |
| 58 | 群回合的终止事件 | **调 `MarkDoneAndSendFinal`**，由 drain loop 决定是否继续；群回合不自发 `done`（否则与 drain 契约冲突，队列未排空就清 running） |
| 59 | 头像条"发言中高亮" | **前端消费已有信号**：`stream_start.agent_id`（发言人成员行 id）+ `stream_finalize`；后端零改动 |
| 60 | 群会话级设置（model/mode/effort/usage） | **不显示选择器**，每个成员用自己 agent 的默认（**已实现**：`ChatInputBar.vue:33,316` 已按 `isGroupSession` 隐藏） |
| 61 | 群 auto-approve 开关 | **作用于全体成员**：切换时批量写各成员行 `auto_approve` + 逐个 `SetAutoApprove` 同步活连接（现状：写群行，而 ACP 读成员行 `acp_backend.go:126` ⇒ **死开关**） |
| 62 | `isGroupSession` 判定 | **用 `session_type === 'group'`**，不靠"名单非空"（后者被 `useGroupMembers.ts:52` 的 catch 置空击穿，群会退回单聊形态）；需后端在 `GET /api/ai/chat` 响应补 `sessionType` 字段 |
| 63 | 成员行的隔离方式 | **白名单**：所有用户可见的会话查询一律 `session_type IN ('chat','group')`，成员行天然不匹配。**不用黑名单** `!= 'group_member'`（新增查询漏加条件时，白名单默认隐藏、黑名单默认泄漏） |
| 64 | 项目会话计数 | **排除成员行**：`ListAllProjects`（`project_registry.go:70`）与 `GetConversationProjects`（`chat.go:1013`）的 COUNT 都加 `session_type IN ('chat','group')`（与决策 #37 同规则；现状两处无类型条件 ⇒ 5 人成员的群使项目数虚增 6） |
| 65 | 群消息的附件 | **支持**（放开后端的 400 拒绝）；**注入时渲染，不写 `content`**——`buildInjectionText` 渲染用户消息时调 `ApplyAttachmentPrefixes`，气泡只靠 `files` 字段渲染（**与单聊逐字一致**）。`GetMessagesBySessionIDRaw` 已取 `files`（`chat.go:361,379`），**无需改取数** |
| 66 | 前端附件入口 | 群输入框**保留**附件按钮 / `@` 文件补全 / 引用入口（决策 #65 让它们真正可用；现状是入口在、发送被 400 拒且无提示） |
| 67 | 主持人标签对成员的可见性 | **注入时剥离**：`buildInjectionText` 渲染主持人发言时去掉 `<clawbench-mention>…</clawbench-mention>`（只在 `Found==true` 时剥，解析失败**绝不剥**——同 askquestion 契约），成员只看到指令文本。理由：标签是后端↔主持人内部协议，留着会诱导成员模仿输出、污染时间线。实现见 `renderMentionsReadable`（`group_inject.go`；标签统一改名见 §13.2） |
| 68 | 指令重复 | **去掉重复**：`grouprouting.Result` **新增"标签前背景"字段**（做法 A），注入时只渲染背景、指令仍由末尾 `主持人要求你：…` 给一次。**同步改前端镜像 `groupRouting.ts` + parity 语料**（该包本有 parity 契约，加字段是设计内演进） |
| 69 | 失败时游标是否推进 | **不推进**：只在回合**成功**时 `SetMemberCursor`。失败 = 没处理过，游标推过去会让那段上下文**永久丢失**（成员后续答非所问且时间线看不出）。失败残留（半截输出 / warning block）**保留在时间线**供用户查看 |
| 70 | warning block 的注入 | **不注入给成员**：成员失败的 warning block（`role='assistant'` + 该成员 `agent_id`）排除在 `buildInjectionText` 之外——它是给用户看的运维信息，不是讨论内容（现状会被当"某成员发言"注入，原始错误如 `create backend: …` 广播给其他成员，而当事人自己反被作者过滤跳过） |
| 71 | 终态前的流式行收尾 | **`emitGroupTerminal` 内部调 `finalizeOrphanedStreamingMessages(groupID,"interrupt")`**，四条退出路径（取消/结束信号/达上限/主持人失败终止）共用。**定位是"兜 `FinalizeStreamingMessage` 自身失败"**——发出终态事件时 `runTurn` 早已返回（`defaultRunner:303` 同步阻塞，所有调用点都在 runner 返回后），**不引入等待**。该函数幂等（查 `streaming=1` 再 finalize） |
| 72 | 群回合的推送通知 | **与单聊完全一致**：复用 `EmitSessionPushNotification(groupID,"completed")`（带 once-per-run 守卫）与 `EmitTurnAnsweredNotification`（排队中途逐条，不占名额）。现状 `emitGroupTerminal` **只发 WS、不推送** ⇒ 用户切走后群讨论跑完完全收不到通知（IM/Android/桌面全无）。`pushSessionTerminal` 依赖的 `GetSessionTitle`/`getSessionResponsePreviewRaw` 对群会话均成立 |
| 73 | 成员连接的 sweep 保护（细化 I4） | **给 ACP idle sweep 单独的查询，不动 `GetRunningSessionIDs`**：编排器维护"当前群回合正在使用的成员行集合"，`SetSessionRunningChecker`（`main.go:863` 注入）的回调改为同时查它。理由：`GetRunningSessionIDs` 的语义是"用户可见的 running 会话"（喂会话列表 `chat_session.go:34,173`、项目删除判定 `project_delete.go:198`），混入隐藏成员行会误触发"项目有会话在跑不能删" |
| 74 | fork 群会话 | **后端拒绝**（与 #52 rewind 对称：v1 不支持，前端隐藏 + 后端守卫）。现状 `ServeForkSession` 无守卫，API 直调会产出"成员归属解析不到的假单聊"（`ForkSession` 硬编码 `session_type='chat'`，`continue_conversation.go:383`）。**`ContinueFromExecution` 无需守卫**——其源是 `task_executions`，群不产生该行，天然够不到 |
| 75 | IM 机器人（钉钉/飞书）的会话列表 | **包含群会话**：`session_command.go:46,71` 两处硬编码 `session_type = 'chat'` 改 `IN ('chat','group')`（与决策 #63 同一白名单）。现状群里讨论在 IM 里完全看不到、也选不中，与 Web 端不一致 |
| 76 | IM 里对群会话发消息 | **委派群编排器**：`sendMessageToSessionFromPush`（`session_command.go:182`）开头判 `GetSessionType == "group"` → 走 `RunGroupTurn`（与 Web 的 `AIChat` 群分支同构）。否则"选中群→发消息"会用**主持人单个 agent 跑单聊回合**，其他成员不参与，且消息落进群时间线**污染讨论**。该函数已走 `EnqueueAndMaybeStart`（`:197`），**天然支持忙碌入队**（决策 #45） |
| 77 | 群聊的 usage 统计 | **保持现状**（usage 写成员行、群行不写）。**非缺口**：群里的 usage 进度条与 popup **整体已被 `isGroupSession` 隐藏**（`ChatInputBar.vue:316` 容器，决策 #60），故无用户可见失效；全局统计按 `chat_metadata` 逐消息聚合（`usage_stats.go:283`），群消息 metadata 正常落库 ⇒ **统计不丢**；成员行各存一份 usage 只是无人读取（无害） |
| 78 | 群成员数上限 | **10 个活跃成员**。理由：每成员是一条独立 ACP 子进程（各带 node/npx 运行时，数百 MB 级），连接池**无上限**（`acp_pool.go` 的 `conns` 是 map）⇒ 无护栏时"加 50 个成员"可瞬间打爆内存；且 10 人以上"辩论"对模型无意义。校验放 **service 层**（唯一写入口）：`CreateGroupWithMembers` 与 `AddGroupMember`；**批量加须先算"现有活跃 + 去重后的净新增"**（`AddGroupMember` 对同 agent 幂等复用，重复添加不得算超限） |
| 79 | 群里的 `/btw` 与 `/cb-*` 命令 | **保留，且 `/cb-*` 在群内真正生效（由主持人执行）**。~~原判断"`/cb-*` 走各自 HTTP API 与群无关"是错的~~：`/cb-*` 实为**提示词注入**（`processClawbenchCommand` 把模板拼进发给 AI 的 prompt），而群分支只落库 `req.Message`、丢弃了 prompt ⇒ 群里发 `/cb-*` 曾是**静默 no-op**。**修（B6）**：渲染出的模板**只注入本回合第一个主持人 turn 的 prompt**（`RunGroupTurnDrain` 渲染 → `runRounds` round 0 拼接），主持人的发言文本进入时间线、成员据此讨论（工具调用对成员不可见是决策 #9）；**不进时间线 content**（气泡保持用户字面原话）、**不注入给成员**（API 契约对成员是噪声）。跨包经 `service.SetRenderGroupCommandFn` ← `main.go` 注入 `handler.RenderClawbenchCommand`（service 不能 import handler）。`/btw` 不受影响（存独立表、用全局摘要模型，见 §12.7(26) 更正） |
| 80 | 通知点击跳转 / 跨设备已读 | **无需改动**（非缺口）。桌面通知 nav 携带 `sessionId`（`notification.ts:37-43` → `clawbench-open-session`），群传群行 id ⇒ 正常跳转；Android `NativeNotificationPolicy` 是**纯 status 判定**（`isNotifiableSessionStatus`），与 `session_type` 无关 ⇒ 群 `completed`/`cancelled` 天然覆盖（**前提是决策 #72 让群回合真的发 `completed`**）；`UpdateLastRead`（`chat.go:997`）按 sessionID 更新并广播 `read`，群行同构 |
| 81 | 中途订阅的 live run 重放 | **无需改动**（非缺口）。`GetLiveRunState`（`chat.go:2725`）按 `session_id` 查最新 `streaming=1` 行并取该行 `agent_id` 作 speaker ⇒ 群会话（session_id=群行）的流式行带**成员行 agent_id**，speaker **正确解析**。这正是决策 #59「发言中高亮」的数据源 |
| 82 | 密送（BCC）与公共指令 | **叠加**：公共指令（speaker 标签之后）+ 对个别成员额外密送，可同时存在 |
| 83 | 密送持久化 | **持久化**：密送嵌在主持人消息里落库，刷新/切走切回后仍在 |
| 84 | 密送多目标 | **支持**：一条密送可发多个成员（`targets="A,B"`，逗号分隔） |
| 85 | 密送用户可见性 | **对用户可见，UI 默认折叠**，点击展开（用户可审计主持人的私下交代） |
| 86 | 密送目标约束 | **必须是本轮被点名者**；给未点名者写密送 → 忽略（结构上满足：只在 targets 循环内按名注入） |
| 87 | 密送语法 | **属性式** `<clawbench-mention targets="A,B" private>内容</clawbench-mention>`（标签统一后，密送是同一标签加 `private` 属性，见 §13.2） |
| 88 | 密送卡片位置 | **气泡内底部**（非 speaker 行、非气泡外） |
| 89 | 密送泄漏阻断 | **双契约**：**显示侧 fail-open**（`Bcc`/`renderMentionChips` 只认良构，畸形保留原文）；**注入侧 fail-closed**（`StripProtocolTags` 剥一切 mention 形态：畸形/嵌套/未闭合）。`Parse` 的 `Before`/`Instruction`/`End` 一律在 fail-closed 文本上计算；成员注入（`renderMentionsReadable`）同样 fail-closed。（函数名见 §13.2 标签统一） |
| 90 | 密送畸形标签 | **显示**：不解析、不剥离（原文保留，同 askquestion"绝不丢内容"）。**注入**：一律剥离（保密优先于保内容——畸形标签的明文绝不外发） |
| 91 | 密送注入边界（穷举） | 所有会把主持人文本送到非目标成员的路径都须剥（review 复查补全）：①`route.Instruction`（Parse 已剥）②`Before`（Parse 已剥）③成员注入（`renderMentionsReadable`，fail-closed）④**引用载荷**（`quotableMessageText`）⑤**TTS**（`ttsExtractConclusion` 覆盖前端文本）⑥**自动朗读**（`onStreamEnd`）⑦**摘要落库 + IM/通知推送**（`ExtractLastAnswerFromBlocks` 内部剥，单点覆盖 summary 与 preview）⑧前端气泡正文（`renderTextBlock` 流式+非流式）。统一剥离原语：Go `grouprouting.StripProtocolTags` / TS `stripGroupProtocolTags` |
| 92 | 密送字符类 parity | Go/JS 空白类统一为 `[\s\p{Z}]`：Go 的 `\s` 仅 ASCII，JS 的含 Unicode 空白，否则 NBSP/全角空格会导致"前端认良构、后端不认"的安全侧分歧；语料加 NBSP/全角 case 钉住 |
| 93 | 密送分享页 | share payload 增 `session.hostMemberId`，分享页传 `resolveSpeaker`/`resolveSpeakerByName`/`hostMemberId` 给 `ChatMessageItem`，使密送卡片在分享页也按决策 #85 渲染（默认折叠） |
| 94 | 密送折叠头不显示目标名 | 折叠头只写「密送」——**谁知道收到了密送本身就是线索**（推理游戏里"B 收到私密信息"是信息）。展开后每条仍显示「发给 XX」（保留可审计性，决策 #85） |
| 95 | 用户是可点名参与者 | 用户在群里的**保留名是英文 `User`**（`groupUserTarget`，语言中性；注入前缀 `User: ...` 与之同名）。`activeMemberNamesExcept` 追加 `User` → 主持人可在「可选的成员名」里看到并点名。哨兵 id `__user__`（无成员行、无 AI turn、无后端连接） |
| 96 | 点名 User 结束本轮 | targets 循环遇到 `groupUserTargetID` → 写一条 `role='system'` 行（i18n `GroupYourTurn`）并 `return DrainResult{}`（`running=false`）；**其后的 targets 不跑**。用户随时发言即开新回合。前端**不做**"轮到你"提示 |
| 97 | 密送延迟送达 | 密送目标本轮未被点名 → 存 `group_pending_bcc`，随该目标**下次被点名**的 turn 送达（AI 成员与 User 一视同仁）。这是「给所有人发词、只点一人先说」的必要条件。交付成功即 DELETE；失败/取消保留（#69 语义） |
| 98 | 密送给 User 的卡片 | 与成员密送**同一张卡片**但形态不同：默认**展开**、文案 `group.bcc.toYou`（密送给你）、`User` 图标 + accent 强调色；成员密送仍折叠。两段式渲染（按 `targets.includes('User')` 分流），而非单一 toggle 改默认值——后者会让 User 密送把成员密送一并展开 |

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
     - <clawbench-mention targets="A,B,C">…</clawbench-mention> + 指令 → 本轮发言者 = [A,B,C]（有序）
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

**离场成员的标注（决策 #39）**：注入文本里，已离场成员的名字后加 **"（已离场）"**，如 `Claude（已离场）: 我认为...`。理由：主持人与其他成员都能看到离场者的历史发言（游标不因离场而重置），但不标注会让主持人**继续点名一个已经不在的人**——`resolveSpeakerTargets` 查不到就静默回退轮转，表现为"主持人反复点名某人然后莫名换人"。标注后模型能自行避免。同理，**主持人提示词的可点名名单**也须区分在场/已离场（见 §5.4）。

**成员描述的注入（决策 #41，三处）**：成员的专业描述来自 `model.GetAgent(agentID).Specialty`（如"全栈开发助手"），注入到：
1. **主持人提示词**——路由名单渲染成 `名字（描述）`，让"AI 决定发言者"有依据（否则只能看名字猜专业）。**默认模型不注入**（模型不是路由信号，只添噪音）；**完整运行时提示词不注入**（含内置共享前缀，每成员数千字，token 成本爆炸且泄漏内部提示词）。
2. **系统事件**——增删成员时写入的时间线消息带上描述，如"Claude（代码编写与推理）加入了讨论"（决策 #40）。
3. **成员自己的注入上下文**——每个成员发言前，能看到**其他成员的描述**，知道"我在跟谁讨论"。渲染在注入文本的**头部**（一次性、不随每条消息重复），格式：`参与者：A（描述）、B（描述）`。

三者共用同一个取描述的函数（`GetAgentSpecialty(agentID) string`，空则省略括号），避免三处各写一份而漂移。

### 5.3 路由标签

仿 `internal/askquestion/` 建独立叶子包（`internal/grouprouting/`）：

- 主持人系统提示注入格式说明，要求输出 `<clawbench-mention targets="A,B,C">给他们的指令</clawbench-mention>`（逗号分隔多个成员；标签统一见 §13.2）。
- 结束信号：`<clawbench-group-end/>`；**结束标签之后的文本即最终汇总**（决策 #32，评审三轮 C-2）。
- **契约**：检测即解析；不可解析时**不剥离**标签（与 askquestion 的"绝不丢内容"一致，保留原文给用户看），并回退轮转。
- **后端保留标签**（不剥离），**前端解析成路由卡片**（决策 #35）——故 Go 实现与前端**镜像** + parity corpus（与 askquestion 同款双向固化）。

**前端卡片契约（评审三轮 #35）**：
- **可解析**：把**标签 span**替换为卡片（"主持人 → A、B" chips）；**标签之后的指令文本保留显示一次**（卡片里可含指令，但正文不得重复渲染同一段——即卡片替换范围包含指令文本，或卡片不含指令而正文保留，二选一，实现时统一，**不得两处都显示**）。
- **不可解析**：**不剥离**，原样显示（含标签），与后端契约一致——**绝不因解析失败丢内容**。

**密送（BCC，决策 #82–#90）**：

- 语法：`<clawbench-mention targets="A,B" private>只有目标成员能看到的内容</clawbench-mention>`，与公开指令**叠加**（公共指令 + 个别密送可并存；标签统一见 §13.2）。
- **目标必须是本轮被点名者**：编排器只在成员派发循环内按 `names[t.ID]` 过滤注入，未点名者写的密送**结构上**无人可得。
- **摘除顺序是安全核心**：`Parse` 在 **fail-closed** 文本（`StripProtocolTags` 剥一切 mention 形态）上算 `Before`/`Instruction`/`End` —— 否则密送会经 `Instruction`（公共指令段）或 `Before`（共享背景）泄漏给所有成员。
- **双契约（决策 #89/#90）**：**显示侧 fail-open**（`Bcc`/`renderMentionChips` 只认良构，畸形保留原文供用户查看）；**注入侧 fail-closed**（`StripProtocolTags` 剥畸形/嵌套/未闭合/任意属性形态）。保密优先于保内容。
- **兜底路径**：主持人消息无 mention 标签（`Found=false`）时成员注入走 `renderMentionsReadable`（fail-closed）。
- **注入边界穷举（决策 #91）**：前端正文（`renderTextBlock` 流式+非流式）、**引用载荷**（`quotableMessageText`）、**TTS**（`ttsExtractConclusion`）、**自动朗读**（`onStreamEnd`）、**摘要落库 + IM/通知推送**（`ExtractLastAnswerFromBlocks` 内部剥）全部剥离。仅靠 `Instruction`/`Before` 是不够的——review 复查发现引用链路无需畸形输入即可把密送注入全员。
- **字符类 parity（决策 #92）**：Go/JS 统一 `[\s\p{Z}]`（Go 的 `\s` 仅 ASCII），语料含 NBSP/全角 case。
- **UI**：气泡内底部折叠卡片，默认折叠，点击展开（`Lock` + `密送` + 目标名 + `ChevronDown`）；分享页同样渲染（决策 #93）。
- 该包有 parity 契约：`Result`/`GroupRouting` 新增 `Bcc []BccEntry`，语料 `want.bcc` 双向固化。

### 5.4 主持人提示词与发言样式

**样式决策（#35/#N6）**：主持人发言与成员发言的区分**靠 `agent_id == 主持人成员行 id`**（群的主持人成员行 id 已知）。**不引入 content 信封标记**（N6：content 由 executor 的 `buildContentJSON` 构建，编排器无注入点）。

前端对主持人气泡渲染**居中特殊样式**，并把其中的路由标签**解析成"主持人 → A、B"卡片**（决策 #35）。标签解析逻辑与后端 `internal/grouprouting` 镜像。

**可点名名单（决策 #39/#41）**：`BuildHostSystemPrompt` 的成员列表**不再只传名字**，而是渲染成 `名字（描述）`，并区分在场状态：

```
可选的成员名：
Claude（代码编写与推理）、CodeBuddy（全栈开发助手）、Grok（xAI 编码代理，已离场）
```

- **描述**来自 `GetAgentSpecialty(agentID)`（决策 #41）——这是"AI 决定发言者"的唯一依据；不注入描述时主持人只能看名字猜专业。
- **在场/离场**（决策 #39）：已离场成员名后加"（已离场）"，主持人据此避免点名一个不在的人（点名后 `resolveSpeakerTargets` 查不到会静默回退，用户看到"莫名换人"）。
- 因此 `BuildHostSystemPrompt` 的入参从 `[]string` 改为**带元数据的结构体切片**（名字 + 描述 + 是否离场），测试与调用点同步改。

### 5.7 成员增删的系统事件（决策 #40/#43/#44）

增删成员时，往**群时间线**写一条系统消息，让主持人和其他成员都知道成员变动。

- **存储**：`role='system'`，**`agent_id=''`**（不是任何成员的发言）。需**整表重建** `chat_history` 放宽 `role` 的 CHECK（原 `CHECK(role IN ('user','assistant'))`）——SQLite 不能改 CHECK，只能 `CREATE new → INSERT SELECT → DROP → RENAME`（仓库有先例：`rebuildChatMetadataWithoutFK`、`migrateChatThinkingSeq`）。
  - **影响面（实现时必须一次改全）**：生产 DDL 1 处 + 重建迁移 1 段 + **约 20 个测试夹具**手写了同一句 CHECK（`chat_test.go:32`、`handler/testutil_test.go`、`session_runtime_test.go:1370` 等）——漏改任一个该测试即失败。
  - **不得用 `role='assistant'` 冒充**：`role='assistant'` 被大量语义占用（未读数 `unreadCountSubquery`、`SessionHasAssistant`、auto-title、resume 判定、`pending_events`），冒充会让系统事件被算成"一条 AI 回复"。
- **内容**：如 `Claude（代码编写与推理）加入了讨论`、`Claude 已离场`（描述来自决策 #41）。
- **注入（决策 #44）**：系统事件**计入游标**（`id > cursor` 即注入），成员/主持人都能看到"谁进谁出"；但**不计入未读数**（未读只数 `role='assistant'`，天然不受影响）。
- **渲染（决策 #43）**：前端按系统事件样式渲染——**居中细条**（非气泡、无头像），与主持人/成员气泡区分。

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

- **会话列表**：群与普通会话并列，群行以 **`Users` 图标 + 成员头像堆叠**标识（meta 行替换掉单聊的 `[AgentIcon] 主持人名`——群行的 `agentId` 是主持人，直接显示会被误读成单聊）；堆叠只显示**在群**成员（离场者不进列表，完整名单见成员管理抽屉），**最多 4 个**、**主持人排最前**（首位 z-index 最高、完整可见）、后续压在其后、**不显示 `+N`**（超出者不渲染，hover tooltip 列出全部）；`group_member` 行被过滤（防泄漏到侧边栏）。成员预览由 `GET /api/ai/sessions`（及 `/overview`）随列表**一次批量**返回（`groupMembers`，非群会话省略该键）。
- **群时间线**：成员发言气泡显示头像 + 名字 + 后端标识（复用 `AgentIcon` / `getAgentName`）；主持人发言居中特殊样式。
- **群空状态**（群会话无消息时）：**极简内容 + 单聊同款卡片样式**——复用 `.agent-welcome` 的卡片语言（`--bg-secondary` 底 + `--border-color` 边 + `--radius-md` + `max-width: 280px`），头像堆叠占据"图标"位（`AvatarStack`，`size=lg`，最多 4 个，排除已离场），右侧文字位放引导语「发送消息，主持人会协调成员完成工作」。**不重复**群名与成员名单（头部头像条已展示）。数据由 `ChatPanelContent` 把已有的 `groupMembers` 透传给 `ChatMessageList`。

**头像堆叠的统一规格**（`AvatarStack`，头部 / 会话列表 / 空状态共用）：**每个 disc 都用主题色边框**（`--accent-color`，不再区分主持人用主题色、其余用中性色）；**主持人排在最前**（`ordered` 把 host 提到首位）——因为**第一个 disc 的 z-index 最高、完整可见**，主持人放最后会被压在下面认不出。主持人由**位置**区分，不再靠不同边框色。
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
- **写一条系统事件**（决策 #40）：时间线追加"XX 已离场"，让全员知道。
- **拒绝移除主持人**（决策 #42）：`RemoveGroupMember` 若 `memberID == host_member_id` 直接返回错误（handler 映射为 409/400），提示"请先转移主持人"。否则会出现**主持人已被移出群却继续控场**的缺陷——`RunGroupTurn` 读的是 `context_state.host_member_id`（行 id），不校验该行是否已归档。
- 理由：群时间线是"会议纪要"，删掉某人发言会让后续"B 回应 A"失去上下文；完全不留痕又会让用户以为系统出 bug。

**加成员**：除成员行外，同样写一条系统事件（决策 #40）——"XX（描述）加入了讨论"（决策 #41）。**重新加入**（`AddGroupMember` 复用原行并清 `archived`）也写一条"XX 重新加入"。

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
- **主持人提示词（决策 #39/#41）**：`BuildHostSystemPrompt` 断言成员渲染为 `名字（描述）`、离场者带"（已离场）"、缺描述时省略括号。
- **注入上下文（决策 #39/#41）**：`buildInjectionText` 断言离场成员名后带"（已离场）"、头部含"参与者：名字（描述）"清单。
- **系统事件（决策 #40/#43/#44）**：增删成员各写一条 `role='system'` 行（`agent_id=''`）；断言该行**计入注入**（`id > cursor` 时出现）且**不计入未读**（`role='assistant'` 过滤天然排除）。
- **拒绝移除主持人（决策 #42）**：`RemoveGroupMember(hostMemberID)` 返回错误、主持人行 `archived` 仍为 0。

### 9.2 单元测试（前端）

- `agent_id`（成员行 id）→ 气泡头像/名字渲染；映射缺失时降级通用样式。
- 主持人特殊样式（`agentId == 主持人成员行 id`）。
- **系统事件渲染（决策 #43）**：`role='system'` 渲染为居中细条、无头像、非气泡。
- **@ 汇总行解析**（决策 #35，原"路由卡片"）：前端解析 `<clawbench-mention targets="A,B">` 成气泡顶部一行"本条 @ 了 A、B"汇总行（§13.4 统一了两模式的渲染）；与 Go 镜像由 parity corpus 固化。
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
21. ~~主持人是否知道成员增删~~ → **知道**：注入标注离场（#39）、提示词区分在场（#39）、系统事件（#40）。
22. ~~主持人是否知道成员描述~~ → **知道**：描述注入三处（#41）。
23. ~~主持人被移除~~ → **拒绝**，须先转移（#42）。
24. ~~系统事件落库方式~~ → **新增 `role='system'`，整表重建**（#40/#43/#44）。

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
**修**：M1 需 (a) ≥2 个 agent YAML，(b) mock 支持产出 `<clawbench-mention targets="A,B">…</clawbench-mention>`（旧标签 `<clawbench-speaker>`）/`<clawbench-group-end/>`，(c) 接好人类抢占路径（见 I4）。

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

## 12.5 实现后缺陷：群聊人工权限审批（2026-10-06，已修）

> 合入 main（`aa88f921a`）后发现。设计与五轮评审**均未覆盖审批响应路径**——评审只钉住了 auto-approve 不变量（N1），漏了人工点按钮这条链。

**症状**：群聊里 ACP 成员请求权限时卡片正常弹出，用户点「批准」**静默失败**（前端乐观显示已批准，后端返回 404 `PermissionNotFound`），成员回合卡在 pending permission 上直到看门狗超时或用户取消。

**根因**：**连接定位键前后端不一致**。

| 环节 | 用的键 | 代码 |
|---|---|---|
| 成员回合建 ACP 连接 | **成员行 id** | `group_orchestrator.go` `SessionID: turn.MemberRowID` → `acp_backend.go:61` `GetOrCreateConn(ctx, agent, req.SessionID)` |
| 权限卡片广播 | 群会话 id | `session_executor.go:385` `ws.EmitToSession(e.timelineSID())` = groupID |
| 前端卡片 `data-session-id` | 群会话 id | `ChatMessageItem.vue` `chatSession.sessionId()` = `identity.currentSessionId` |
| 审批回传后查连接 | 群会话 id → **nil** | `session_runtime.go` `mgr.GetConn(groupID)` |

`GetConn` 是 `conns[clawbenchSID]`，连接只以成员行 id 注册过 ⇒ 用 groupID 查必为 nil。群行虽写了 `agent_id = hostAgentID`（`group_store.go`）故第一道检查通过，但卡在 `GetConn`。

**为什么 auto-approve 不受影响**：它读 `getSessionAutoApprove(req.SessionID)`，而 `req.SessionID` 就是成员行 id，键天然一致。

**修法（后端解析，两条响应路径共用）**：`RespondPermission` 先做单聊快路径；若 `GetSessionType(sessionID) == 'group'`，遍历 `ListGroupMembers` 的成员连接，用新增的 `ClawACPClient.HasPendingPermission(key)` 命中持有该 tool call 的连接后应答。单聊路径的 `"session not running"` / `"ACP session not found"` 诊断信息**原样保留**（既有测试钉住）。

- 覆盖 HTTP `POST /api/ai/permission/respond`（`handler/permission.go`）与 WS `permission_respond`（`ws/events.go`）——两者都汇聚到 `service.RespondPermission`，故一处修复两条路径生效。
- 前端与 ACP 层**零改动**（卡片继续带群 id，后端解析）。tool call id 每回合唯一 ⇒ 至多一个成员命中，其余连接不受影响。
- 测试：`internal/service/permission_group_test.go`（多成员判别 + 无 pending 仍失败），变异验证关掉群分支即变红。

## 12.6 增补：成员可见性、描述注入与系统事件（2026-10-06，决策 #39–#44）

> 合入 `aa88f921a` 后增补。以下三项是**实现里确实存在的缺口**，已逐条核对当前代码。

**（1）主持人只知道成员名字。**
`group_orchestrator.go:200` 调 `BuildHostSystemPrompt(activeMemberNamesExcept(...))`，而 `activeMemberNamesExcept`（`:350`）只返回 `m.Name`；`group_prompt.go:31` 仅 `strings.Join(members, "、")`。⇒ 主持人路由**等于看名字猜专业**。而成员自己走 `BuildChatRequest` → `SystemPrompt: agent.RuntimeSystemPrompt`（`chat_request.go:209`），人设对成员已成立——缺口只在主持人侧。
**修（决策 #41）**：描述（`Agent.Specialty`）注入三处（主持人提示词 / 系统事件 / 成员注入上下文），`BuildHostSystemPrompt` 入参由 `[]string` 改为带元数据的结构体切片。

**（2）增删成员不写时间线，且离场者仍被点名。**
`handler/group.go:148,172` 直接调 `AddGroupMember`/`RemoveGroupMember`，**不往 `chat_history` 插行**；主持人获取上下文只靠时间线注入（`group_inject.go:26`）⇒ 主持人眼里"什么都没发生"。且 `buildInjectionText`（`group_inject.go:28-53`）**没有 Left 过滤**，离场者的旧发言照注入却不标注，主持人会继续输出 `<clawbench-mention targets="离场者">…</clawbench-mention>`（旧标签 `<clawbench-speaker>`）→ `resolveSpeakerTargets`（旧名 `resolveTargets`）查不到 → 静默回退轮转（表现为"反复点名某人然后莫名换人"）。
**修（决策 #39/#40）**：注入时离场者名后加"（已离场）"；主持人提示词区分在场/离场；增删写系统事件。

**（3）主持人被移除后仍继续控场。**
`RemoveGroupMember`（`group_store.go:341`）只置 `archived=1`，**不清 `context_state.host_member_id`**；`RunGroupTurn:90` 读 `GetGroupHostMember` 拿到行 id 后**不校验该行是否已归档**，`buildMemberTurnSpec` 也不检查 `Left`。`groupMemberTurn.IsHost`（`:23`）字段从未用于校验。⇒ 把主持人移出群，它被标"已离场"、从名单消失，**但下一轮照样发言控场**。
**修（决策 #42）**：`RemoveGroupMember` 加守卫，遇 `memberID == host_member_id` 直接报错，须先 `SetGroupHostMember` 转移。

**（4）系统事件落库的硬约束（决策 #40/#43/#44）。**
`chat_history.role` 有 `CHECK(role IN ('user','assistant'))`（`database.go:308`）。加 `role='system'` **SQLite 不能改 CHECK**，只能整表重建（先例：`rebuildChatMetadataWithoutFK`、`migrateChatThinkingSeq`）。
- **影响面（一次改全，漏一处即失败）**：生产 DDL 1 处 + 重建迁移 1 段 + **约 20 个测试夹具**手写同一句 CHECK（`chat_test.go:32`、`handler/testutil_test.go`、`session_runtime_test.go:1370`、`database_test.go:61/2123/2510/3367/3724`、`drain_test.go:51`、`scheduler_test.go:26`、`scheduler_executor_test.go:28`、`scheduler_script_phase_test.go:31`、`summary_test.go:258`、`session_cleanup_test.go:351`、`chat_metadata_ledger_migrate_test.go:50`、`thinking_migrate_test.go:48`、`session_command_test.go:66` 等）。
- **不得用 `role='assistant'` 冒充**：该角色被大量语义占用——未读数 `unreadCountSubquery`（`chat.go:1199`）、`SessionHasAssistant`（`chat.go:2497`）、auto-title、resume 判定、`pending_events.go:381`、`thinking_migrate.go:23/54`、`database.go:1927/1997/2318/2346`。冒充会让系统事件被算成"一条 AI 回复"。
- **好消息**：未读天然只数 `role='assistant'`，故系统事件**不计入未读**无需额外改动（决策 #44）；但它**计入游标**（`id > cursor` 即注入），成员/主持人都能看到成员变动。

## 12.7 增补：排队、生命周期与降级（2026-10-06，决策 #45–#59）

> 第二轮 grill（逐分支核对代码）。以下缺口均为**实现里确实存在**的，已定位到具体行。

**（1）群委派绕过运行中检查 → 并发编排器（决策 #45/#46）。**
`handler/chat.go:323` 的群委派在 `TryClaimSessionRun`（忙碌检查）**之前**就 `go RunGroupTurnForSession(...)` 并 `return`（`:339`）。单聊路径则在忙碌时入队（`:292` 注释 + `:521` `AddQueuedMessage`）。⇒ 群回合运行中再发消息会起**第二个编排器**，两个都 `SetSessionRunning(groupID,true)` 且都写群时间线，`UpdateStreamingMessage` 的 `ORDER BY id DESC LIMIT 1` 互相打到对方流式行——顺序轮次只保证**单编排器内**串行。
**修**：群委派移到 claim 之后；复用 `RunDrainLoop`（`drain.go:275`，`ExecuteRunWithMessage func(msgID, row) DrainResult` 是唯一 run 钩子，传一个调 `RunGroupTurn` 的实现即可）。

**（2）群删除/归档不处理成员行（决策 #48）。**
`HardDeleteSession`（`chat.go:2864`）只 `DELETE FROM chat_sessions WHERE id=?`（群行），成员行是独立 `chat_sessions` 行 → **永久残留**。`RemoveGroupMember`（`group_store.go:341`）只 `UPDATE archived=1` **不刷 `updated_at`** → 若开 `ArchiveRetentionEnabled`，`GetExpiredArchivedSessions`（`session_queries.go:480`，`archived=1 AND updated_at<cutoff`）会把"离场"当"过期"**硬删**，历史发言失去名字映射。

**（3）群回合运行中的人类消息无抢占语义（决策 #45–#47）。**
决策 #14 说"人类可 @ 成员 / 打断"，代码里**无任何 @ 解析或抢占处理**。

**（4）成员失败静默（决策 #51）。**
`group_orchestrator.go:158` 仅 `slog.Warn`，时间线不留痕，违背决策 #18/§6"时间线记可见错误"。**早失败**已走 `failTurn`（`run_turn.go:214`，已对群时间线接线：`emitDrainEvent(s.effectiveTimelineSessionID(),...)` + `AddChatMessageWithAgent(..., s.effectiveTimelineSessionID(), ..., s.SpeakerID)`）；**运行中失败**这条路径静默。

**（5）摘要/推荐按成员回合触发（决策 #55）。**
`session_executor.go:1672` 每次 Finalize 调 `triggerChatSummarization(e.ctx, e.timelineSID())`；群会话的 `timelineSID()` 是**群行** ⇒ 每个成员回合都（a）摘要全群未摘要的 assistant 消息（DB 扫描 + 写锁）、（b）发起推荐 LLM 调用（`session_runtime.go:969`，最长 60s）。3 成员 × 10 轮 ≈ 30 次。

**（6）回退永远选第一个且永不收尾（决策 #56）。**
`speakNextMember`（`group_orchestrator.go:250`；重构后改名 `pickFallbackMember`，见 §13.6）`for ... { ...; return true }` 恒选名单第一个非主持人成员；`len(targets)==0` 分支（`:139`）只 `continue`，**无失败计数** ⇒ 主持人持续畸形会以同一成员空转到 `maxRounds`。

**（7）主持人不可换且无转移入口（决策 #53）。**
`SetGroupHostMember`（`group_store.go:361`）存在但**无 handler、无前端**（`GroupMemberSheet` 无换人入口）⇒ "拒绝移除主持人"会把用户锁死（唯一出路是解散重建）。用户决定：**干脆不可换**，规则最简。

**（8）归档群不关成员连接（决策 #57）。**
`chat_session.go:373/397/450` 都 `CloseConn(sessionID)` = 群行 id，而成员连接注册在**成员行 id**（`group_orchestrator.go:282` `SessionID: turn.MemberRowID`）⇒ 一个连接都关不到，5 个 ACP 成员会多存活最长 5 分钟（每会话一个子进程）。

**（9）发言中高亮未实现（决策 #59）。**
`GroupAvatarStack.vue` 只有 host ring；全仓无 `activeSpeaker`/`isSpeaking`。但信号已在线上：`stream_start.agent_id`（`stream_hub.go:384`）+ `stream_finalize`（`:399`）⇒ **纯前端改动**。

**（10）rewind 与成员游标冲突（决策 #52）。**
`TruncateSessionAfterMessage`（`continue_conversation.go:770`）删群时间线行后，成员 `seen_cursor` 仍是旧高水位（`SetMemberCursor` 单调递增）⇒ 之后 `id > cursor` 恒不成立 ⇒ 成员失忆。且 `ServeSessionRewind` 会 `CloseConn`，恢复的是 agent 侧**未被回溯**的历史，与群时间线不一致。

**（11）群 auto-approve 是死开关（决策 #61）。**
`toggleAutoApprove`（`useSessionIdentity.ts:420`）PATCH `currentSessionId` = **群行**；`chat_session.go:583-590` 写群行并 `GetConn(群行)` 同步（群行无连接，恒 nil）。而 ACP 读的是 `getSessionAutoApprove(req.SessionID)`（`acp_backend.go:126`），成员的 `req.SessionID` 是**成员行 id**（`group_orchestrator.go:282`）⇒ 群里开 auto-approve **完全不生效**，成员照弹权限卡片。同类"配置写在群行、成员读成员行"的错位。

**（12）`isGroupSession` 靠名单非空，会被 fetch 失败击穿（决策 #62）。**
`ChatPanelContent.vue:350` `isGroupSession = (props.groupMembers?.length ?? 0) > 0`，而 `useGroupMembers.ts:52-53` 在任何异常时 `members.value = []` ⇒ 一次网络抖动就让群会话**按单聊渲染**（fork/rewind/model/mode 控件全部冒出来、头像条消失）。需改为从 `session_type` 派生——但 `GET /api/ai/chat` 目前**不返回** `sessionType`（只返回 `sessionBackend`/`sessionAgentId`/`sessionModelId`/`sessionTransport`/`sessionAutoApprove`），故需后端补字段。

**（13）群会话级设置已正确隐藏（决策 #60，非缺口）。**
`ChatInputBar.vue:33`（ACP 控制栏）与 `:316`（model/mode/auto-approve/usage 行）已按 `isGroupSession` 隐藏；`ChatMessageItem.vue:144,149` 隐藏 fork；`ContentBlocks.vue` 隐藏 warning-reset 按钮。此项**无需改动**，记录以免重复设计。

**（14）项目会话计数把隐藏成员行算进去了（决策 #64）。**
`ListAllProjects`（`project_registry.go:70-73`）与 `GetConversationProjects`（`chat.go:1013-1014`）都对 `chat_sessions` 做 `COUNT(*)` **且无 `session_type` 条件** ⇒ 一个 5 人成员的群使项目会话数**虚增 6**（1 群行 + 5 成员行），而侧边栏只显示 1 条。两处分别喂设置页项目列表（`GET /api/projects/registry`）与项目选择器（`GET /api/conversation-projects`），**用户可见**。

**（16）群消息的附件：入口在、发送必被拒、注入也丢（决策 #65/#66）。**
三处叠加：
- **后端拒绝**：`handler/chat.go:324-327` 群会话带 `Files`/`FilePaths` 即 400 `InvalidRequest`。
- **前端入口照常**：附件按钮（`ChatInputBar.vue:112`）与 `@` 文件补全（`:979`）**无 `isGroupSession` 门控**；引用（`kind='quote'` 的 `FileEntry`，`model/chat.go:93`）也走 `req.Files`。⇒ 用户能挂、能选，点发送得到**无提示的失败**。
- **即使放开也丢**：单聊把附件拼进 prompt 是 handler 干的（`chat.go:424` `ApplyAttachmentPrefixes`），而群注入走 `groupInjectionText` → `buildInjectionText`（`group_inject.go:26`）**只渲染 `ExtractPlainText(content)`，不读 `files`** ⇒ 附件对成员不可见。
**修（决策 #65/#66）**：放开后端拒绝；在 `buildInjectionText` 渲染用户消息时调 `ApplyAttachmentPrefixes`（**不写 content**，气泡与单聊一致）；前端入口保留。**`GetMessagesBySessionIDRaw` 已经取了 `files` 并解析进 `msg.Files`（`chat.go:361,379-381`），无需改取数。**

**（17）主持人路由标签泄漏进成员上下文，且指令重复两遍（决策 #67/#68）。**
`buildInjectionText`（`group_inject.go:45-52`）渲染每条消息用 `ExtractPlainText(m.Content)`，**不做任何标签剥离** ⇒ 被点名成员看到 `主持人: <clawbench-mention targets="A">请谈谈你的看法</clawbench-mention>`（旧标签 `<clawbench-speaker>`）。两个后果：① 成员可能**模仿该格式**在自己的发言里也输出该标签（前端会误渲染成路由卡片）；② 用户引用成员原话时带出标签。（现由 `renderMentionsReadable` 剥离，见决策 #67）
叠加**重复**：`group_inject.go:54-61` 把 `route.Instruction` **单独再拼一段**「主持人要求你：…」，而主持人那条发言文本里**已含同一句** ⇒ 成员看到两遍。
`grouprouting.Parse` 按契约**只解析不剥离**（`Result.Raw` 保留原文，注释明说 "never mutates input"）⇒ 剥离需在新地方做，且必须遵守「解析失败绝不剥离」。
**修（决策 #67/#68）**：`Result` 新增**标签前背景**字段；注入时渲染背景 + 剥离标签；指令只由末尾那一段给一次。**前端镜像 `groupRouting.ts` 与 parity 语料同步改。**

**（18）失败时游标照推 + warning block 被当成员发言注入（决策 #69/#70）。**
- **游标**：`group_orchestrator.go:119-121`（主持人）与 `:156-158`（成员）都是**先 `SetMemberCursor(高水位)`、后判 `Err`**。⇒ 失败回合（后端创建失败/连接不可用）实际上**什么都没处理**，但游标已被推过 ⇒ 本轮注入的内容**永久丢失**，成员后续答非所问，且时间线上看不出。
- **warning block**：成员失败的 warning block 是 `role='assistant'` + **该成员行 `agent_id`**（决策 #51）。`buildInjectionText:32` 的作者过滤只跳 `m.AgentID == self` ⇒ **其他成员**会把 `ExtractPlainText` 解出的原始错误文本（`create backend: …`）当"某成员发言"读进上下文；而**失败者自己**反被过滤跳过，看不到自己的失败。
**修（决策 #69/#70）**：游标只在成功时推进；warning block 排除在注入之外。

**（19）终态前无孤儿行收尾（决策 #71）。**
`emitGroupTerminal`（`group_orchestrator.go:178`）只做 `SetSessionRunning(false)` + `done` + `completed`，**不 finalize 任何流式行**；而 `finalizeGroupOrphans`（`:373`）只在**回合开始时**调（`:108`）。⇒ 若某成员回合的 `FinalizeStreamingMessage` 自身失败（DB 写错），该行停在 `streaming=1`，终态事件发出后前端重载即出现**幽灵流式气泡**。
**澄清（勿夸大）**：这**不是**取消竞态——`defaultRunner:303` 是同步 `runTurn`，`emitGroupTerminal` 的六个调用点全在某个 `runner(...)` 返回之后，故发出终态时 executor 的 finalize 早已执行。**无需引入等待**；只需把幂等的孤儿清理放进 `emitGroupTerminal` 兜住"finalize 失败"。
**修（决策 #71）**：`emitGroupTerminal` 内先 `finalizeOrphanedStreamingMessages(groupID, "interrupt")`，再发终态。

**（20）群回合结束不推送通知（决策 #72）。**
`emitGroupTerminal`（`group_orchestrator.go:178`）只发 WS `done`/`completed`，**不调 `EmitSessionPushNotification`**；而单聊终止路径会推（`session_command.go:304`、`session_runtime.go:174`）。⇒ 用户在群里发完消息切走，群讨论跑完（可达数十秒到数分钟）**完全收不到通知**——IM 机器人 / Android 原生 / 桌面系统通知全都没有，而单聊同样操作会收到。群讨论耗时更长，通知价值反而更高。
**修（决策 #72）**：群回合复用 `EmitSessionPushNotification(groupID,"completed")` + `EmitTurnAnsweredNotification`（排队中途逐条），不另写推送逻辑。与决策 #58（`MarkDoneAndSendFinal` 决定终态）同批落地——单聊的 `MarkDoneAndSendFinal` 本就带推送。

**（21）成员连接会被 idle sweep 在群讨论进行中杀掉（= 未落地的 I4(a)，决策 #73）。**
连接以**成员行 id** 为键注册（`group_orchestrator.go:282` `SessionID: turn.MemberRowID`），而 idle sweep 的存活判断是 `m.isSessionRunning(sid)`（`acp_pool.go:419,457`），`sid` 即成员行。但群路径只对**群行**调 `SetSessionRunning`（`group_orchestrator.go:79`），**成员行从不被标记 running** ⇒ 群回合进行中，一个空闲超 5 分钟（`idleConnTimeout`）的成员连接会被 sweep **杀掉**（功能上会 respawn，但每个 ACP spawn 可能数秒到数十秒，且讨论中反复 spawn）。
**注意**：此问题**已被评审识别为 I4(a)**（§12），但**从未落地修复**。
**修（决策 #73）**：给 sweep 单独的"群回合正在使用的成员行"查询，**不把成员行塞进 `GetRunningSessionIDs`**（那会污染会话列表与项目删除判定）。

**（22）fork 群会话无后端守卫（决策 #74）。**
`ServeForkSession`（`chat_session.go:806`）**不校验 `session_type`**；`ForkSession`（`continue_conversation.go:290`）按源会话的 backend/agent_id 建**硬编码 `session_type='chat'`** 的新会话（`:383`），并把群时间线消息**复制**过去（含 `agent_id` = 成员行 id）。⇒ fork 一个群会产出：backend = 主持人的后端、内容为整群讨论副本、但**成员行 id 在新单聊里解析不到成员**（`useGroupMembers` roster 为空）→ 前端 `resolveSpeaker` 返回 null → 一堆**无归属**的 AI 发言；且 `fork_context_budget` 会把这些当普通历史注入。
前端已隐藏群 fork 按钮（`ChatMessageItem.vue:144,149`），但**API 可绕过**。
**修（决策 #74）**：`ForkSession` 加群守卫（与 #52 对称）。**`ContinueFromExecution` 无需改**（源是 `task_executions`，群不产生）。

**（23）IM 机器人看不到群会话，且对它发消息会退化成单聊（决策 #75/#76）。**
- **看不到**：`session_command.go:46`（按 id 前缀查找）与 `:71`（列最近会话）**硬编码 `session_type = 'chat'`** ⇒ 群在钉钉/飞书的会话列表与查找里**完全缺席**，无法选中。与 Web 端（群计入列表，决策 #37）不一致。
- **发消息会退化**：`sendMessageToSessionFromPush`（`session_command.go:182`）走 `EnqueueAndMaybeStart` → 普通单聊回合 ⇒ 即使能让 IM 选中群，发消息也只会用**主持人那一个 agent** 跑单聊，其他成员不参与，且该消息落进群时间线**污染后续讨论**。
**修（决策 #75/#76）**：两处改 `IN ('chat','group')`；`sendMessageToSessionFromPush` 判群后委派 `RunGroupTurn`（与 Web `AIChat` 群分支同构）。该函数已走 `EnqueueAndMaybeStart`，**天然支持忙碌入队**。

**（24）usage 统计经核实是群安全的（非缺口，决策 #77）。**
成员回合的 usage 走 `PatchContextStateMerge(e.cfg.SessionID, …)`（`session_executor.go:1014`）写到**成员行** `context_state.usage`；群行不写。但：
- 群里的 usage 进度条 + popup **整体在 `v-if="!isGroupSession"` 容器内**（`ChatInputBar.vue:316`，决策 #60）⇒ **无用户可见失效**。
- 全局用量统计按 `chat_metadata` **逐消息**聚合（`usage_stats.go:283`），群消息的 metadata 由 `SaveMetadata(msgID, …)`（`session_executor.go:1679`）正常落库 ⇒ **统计不丢**。
- 成员行各存一份 usage 无人读取，无害。
**决定（#77）**：保持现状，不加"汇总到群行"的逻辑（每个成员有各自上下文窗口，"合并"语义本身有歧义）。

**（25）群成员数无上限（决策 #78）。**
`CreateGroupWithMembers`（`group_store.go:74`）只校验 `len(specs) == 0`；`handler/group.go:142-154` 的批量加成员也不限个数。而**连接池无上限**（`acp_pool.go` 的 `conns` 是 `map[string]*ACPConn`，`minAliveConns = 3` 是**下限**不是上限）⇒ 一个 20 成员群首次讨论会 spawn 最多 20 个 ACP 子进程（各带 node/npx 运行时）。sweep 会在 5 分钟空闲后回收，但**讨论期间**是真实资源峰值，且"加 50 个成员"可瞬间打爆内存。
**修（决策 #78）**：上限 10 个活跃成员，校验在 service 层两个写入口。

**（26）`/btw` 与 `/cb-*` 在群里可用（决策 #79）。**
`slashCandidates`（`ChatInputBar.vue:1014-1036`）**无 `isGroupSession` 门控** ⇒ 群输入框弹命令菜单。
- **`/btw`**：问答存独立表 `btw_questions`（`btw.go:35`），由**全局 AI 摘要模型**回答（`btw.go:141` `summarize.NewAISummarizer(model.ConfigInstance.AISummary)`）——**与会话/群 backend 无关**（此处更正原"用群行 backend"的说法）。不破坏群时间线，保留。
- **`/cb-*`**：~~"走各自 HTTP API 与群无关"~~ 是**错的**——实为**提示词注入**。群分支曾只落库 `req.Message`、丢弃已渲染的 prompt ⇒ **静默 no-op**。已修（B6，见决策 #79）：模板**只注入第一个主持人 turn**，主持人的发言文本进时间线供成员讨论，不进 content、不注入成员。

**（27）通知跳转与跨设备已读经核实是群安全的（非缺口，决策 #80）。**
- **桌面通知点击**：nav 携带 `sessionId`（`desktop/src/main/notification.ts:37-43` `channelFor` → `clawbench-open-session`），群会话传**群行 id** ⇒ 正常切到该群。
- **Android 原生通知**：`NativeNotificationPolicy` 是**纯 status 判定**（`isNotifiableSessionStatus`：completed/cancelled/permission_pending），**与 `session_type` 无关** ⇒ 群的状态天然覆盖。**但前提是决策 #72**（群回合目前根本不调 `EmitSessionPushNotification`，也就不写 `pending_events`，Android 通知链无从触发）。
- **跨设备已读**：`UpdateLastRead`（`chat.go:997` → `chat.go:1522`）按 sessionID 更新 `last_read_at` 并 `EmitSessionEventWSOnly(sessionID,"read")`，群行与单聊行同构。

**（28）中途订阅的 live run 重放是群安全的（非缺口，决策 #81）。**
`EmitLiveRunStateToClient`（`stream_hub.go:724`）在客户端中途订阅时补发 `user_message` + `stream_start`（否则前端缓冲的事件永不排空）。其 speaker 来自 `GetLiveRunState`（`chat.go:2725`）：按 `session_id` 查最新 `streaming=1` 的 assistant 行，取该行 `agent_id`。群会话的 `session_id` 是群行、流式行的 `agent_id` 是**成员行** ⇒ speaker 正确解析。**这也是决策 #59「发言中高亮」的数据源**。

**（29）N1 实现时发现的两个硬陷阱（整表重建 chat_history）——已修并测试固化。**
计划只写了「CREATE new → INSERT SELECT → DROP → RENAME，同一事务」，但实测还有两条**不做就静默毁数据**的约束：
1. **`DROP TABLE chat_history` 在 FK=ON 时会级联删除 `chat_thinking` / `chat_tool_calls`**（SQLite 对 DROP TABLE 做隐式 `DELETE FROM`，触发 `ON DELETE CASCADE`）⇒ 思考与工具调用记录**全部清空**。修法：`store.WriteLock()` + `WriteDBRaw().Conn(ctx)` **钉住一条连接**（`PRAGMA foreign_keys` 是**每连接**的），在该连接上 `PRAGMA foreign_keys=OFF`（SQLite 官方 ALTER 流程），事务完成后恢复 `=ON` 再还池。**变异验证：去掉 FK-off → 子表计数归零、测试报红。**
2. **必须在 `migrateQueuedMessagesToOwnTable` 之后跑**：重建的显式列清单不含 legacy `queue_id`/`queued`，跑前面会让队列迁移的 `SELECT` 失败 ⇒ **永久丢排队消息**。已加专门测试钉住顺序。
3. 附带：重建按「源表**实际存在**的列」求交集拷贝（旧库可能缺 `files` 等可选列）——被 `TestInitDB_MigratesChatThinkingSeq` 的极简夹具抓出的真回归。
**给后续任何 `chat_history` 系整表重建的提示：复用「钉连接 + FK off」模式，否则静默清空思考/工具调用。**

**（15）context_state / unread / activeStreams 经核实是群安全的（非缺口）。**
- `context_state`：成员回合写 `mode/effort/usage` 走 `PatchContextStateMerge(e.cfg.SessionID)`（`session_executor.go:1014`）= **成员行**；`seen_cursor` 也写成员行；群行只存 `host_member_id`/`maxRounds`。键不冲突。
- 未读：`unreadCountSubquery`（`chat.go:1196`）以**群行**为 `s`，群时间线的 `role='assistant'` 行（成员发言 + warning block）计入未读；成员行无 `chat_history` 故永不显示未读。`UpdateLastRead`（`chat.go:1522`）锚定群行。
- `activeStreams` 以 `cfg.SessionID` = **成员行** 为键（`session_executor.go:367,397`），是 executor 级注册表，多成员并发互不覆盖；优雅关停的 `FlushStreamingNow`/`WaitStreamsDrained` 正常。

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

## 13. 自由聊天模式（Free Chat Mode，2026-10-08）

> 与主持人模式**并列**的第二种群聊形态。主持人模式原样保留（只跟随 §13.2 的标签统一改名）。
> 本节记录 2026-10-08 grilling 的全部决策。**开发阶段，不兼容旧群**（见 #F14）。

### 13.1 模式判定

- **建群时是否指定主持人决定模式**：指定了 → 主持人模式（`mode="host"`）；未指定 → 自由聊天模式（`mode="free"`）。
- **不可中途切换**（#F1）。
- **显式存储**（#F2）：群行 `context_state` 增 `group_mode` 字段（`"host"` | `"free"`）。建群时一次性写入，之后只读。
  - **键名必须是 `group_mode` 而非 `mode`**：`ContextState`（`chat.go:2233`）已用 `$.mode` 存持久化的智能体模式（`ModeStatePersist`），共用键名会让 `GetContextState` 对群行反序列化失败（`cannot unmarshal string into ... ModeStatePersist`）——free 模式 E2E 实测抓到这个冲突。
  - 判定读 `group_mode`；`host_member_id` 仅在 `group_mode="host"` 时要求非空。
  - 数据损坏（`group_mode` 缺失）时回退为 host 模式（开发阶段不兼容旧群，故无迁移）。
- **群行身份字段**（#F3）：自由模式群用**第一个成员**填充 `backend`（NOT NULL）与 `agent_id`（列表图标）；标题占位 `"群聊"`（首条消息后 auto-title 接管）。不碰 schema。
- **最少成员**（#F10）：自由模式至少 **2** 个成员（1 个无意义，与单聊重复）。主持人模式仍按现状。

### 13.2 标签统一（破坏性，不兼容旧名）

- `<clawbench-speaker>` **改名** `<clawbench-mention>`，并把**目标 + 内容 + 密送融合进一个标签**（#F4）：
  - 公开指派/提及：`<clawbench-mention targets="A,B">给他们的内容</clawbench-mention>`
  - 密送：`<clawbench-mention targets="C" private>只有 C 能看到的内容</clawbench-mention>`
  - 结束信号：`<clawbench-group-end/>` 不变。
- **targets 取值**（#F5）：智能体写**成员显示名**（LLM 不知道行 id）；用户侧写**成员行 id**。解析器两个都试（先按 id 精确匹配成员行，再按名字匹配）。
- **内容从"标签之后"移入"标签体内"**。旧 `Before` 语义（标签前的背景文本）保留为"标签外的其余文本"。
- **不兼容旧名**（#F14）：老群的 `<clawbench-speaker>` / `<clawbench-bcc>` 历史消息会退化成裸文本显示。开发阶段可接受。
- **前后端镜像**：`internal/grouprouting` 与 `web/src/utils/groupRouting.ts` 同步改，parity corpus 更新。
- **剥离契约**：host 模式成员看到的文本仍剥标签（防模仿）；**free 模式不剥**（成员的 @ 必须对其他成员可见，否则接力链上的人不知道被点名）。

### 13.3 自由模式主循环

```
用户消息 → 写入群时间线(agent_id='') → 广播
  ↓
初始目标集 = 用户消息里 @ 的成员（按 @ 出现先后，轮内去重）
            若无 @ → 全体活跃成员，按花名册（加入）顺序
  ↓
待发言队列 = 初始目标集（FIFO）
  ↓
while 队列非空:
  取队首成员 → runSpeakerTurn(连接=成员行, 时间线=群)
    - 注入 = 群时间线 id>cursor 且作者≠自己 的发言（mention 渲染成人话 @名字）+ 自由模式成员提示词
    - 解析该成员输出里的 <clawbench-mention>
    - 若 @ 了 User（保留名）→ 停下，本轮结束，等人类回复（#F6）
    - 否则把有效新目标**排到队尾**（队内去重：已在队列中的人不重复加；已说完的人可再次被加）（#F9/#F11）
    - 无有效目标 → 队列自然耗尽 → 结束
用户点停止 → 结束整条接力，已说完内容保留（#F12）
```

- **发言顺序**：FIFO 队列，"谁被 @ 谁排到队尾"（#F11，排尾而非插队首）。
- **去重**：只跟**当前待发言队列**去重；已说完的人被再次 @ 就再说 → A@B、B@A 可无限循环（#F9）。这是"无限接力"的实现基础。
- **无 @ 顶层消息**：全体按花名册顺序各说一次（#F8），其间的 @ 追加到队尾。
- **无效 @ 目标**：`@自己` 与 `@已离场成员` 静默丢弃；`@不存在的名字` 额外插一条可见系统提示（#F7）。若一轮内有效目标为空则停止。
- **无轮数上限、无循环检测**（#F13）：纯靠用户手动停。**已知风险：A@B、B@A 会无限烧 token**。
- **顺序执行**（#F9）：与主持人模式同一执行内核（`runSpeakerTurn` → `buildMemberTurnSpec` + 顺序 `runTurn`）。设计 §12 C1 的"单一流式行"约束同样适用，故**不并行**。
- **不产出汇总发言**（#F15）：接力停即停；群会话仍复用 `triggerChatSummarization`（标题/摘要/推荐），只是没有"主持人结论"气泡。
- **`/cb-*` 命令**（#F16）：由**第一个发言者**执行一次，结果进时间线供讨论（对齐主持人模式的决策 #79）。
- **密送（private）**：成员与用户都能发（#F24）。复用主持人模式的 `group_pending_bcc` 通道——发言者发出的 private mention 经 `storeRouteNotes`（带目标校验：未知/离场/自我丢弃）按解析后名字存入表，在该目标**下次被点名发言时**注入（与主持人模式的"下次点名时送达"语义一致）；`@User` 的密送在 UI 卡片里读，注入前清账。
- **人类插话**（#F17）：排队，等当前回合跑完再处理（复用现有 drain 队列）。
- **运行中加成员**（#F18）：下一条顶层消息起生效，当前回合参与者冻结。
- **自由群 UI**（#F19）：无主持人皇冠/标识，正常成员头像堆叠。

### 13.4 前端

- **@ 补全**（#F5/#F20）：输入框 `@` 浮层同时列**成员**与**文件**两组候选；选中成员插入 `<clawbench-mention targets="成员行id"></clawbench-mention>`（空体）。用户只是"点名让谁发言"，问题本身是消息其余文本。
- **渲染**（#F21）：mention 标签渲染成**正文内联 `@名字` chip** + 气泡顶部一行"本条 @ 了 B、C"**汇总行**。两种模式统一（主持人模式也改成这套，取代原"主持人 → A、B"卡片）。
- **建群 UI**（#F22）：主持人**纯可选**——移除"首个勾选自动成为主持人"、"取消清空"、"必须选主持人"逻辑；成员列表下方一行小字"未指定主持人将进入自由聊天模式"。
- **host 模式忽略人类 @**（#F23）：只有自由模式认人类的 @；主持人模式里 @ 谁都没用，仍由主持人决定。
- **主持人模式不产出/不变**：主持人模式除标签改名 + 渲染统一外，行为完全不变。

### 13.5 受影响文件（初判）

| 层 | 文件 |
|---|---|
| 标签解析（叶子） | `internal/grouprouting/grouprouting.go` + `testdata/parity_corpus.json` |
| 前端镜像 | `web/src/utils/groupRouting.ts` |
| 编排器 | `internal/service/group_orchestrator.go`（新增 free 循环）、`group_prompt.go`（新增 free 提示词）、`group_inject.go`（mention 渲染） |
| 存储 | `internal/service/group_store.go`（`group_mode` 读写、free 建群） |
| HTTP | `internal/handler/group.go`（建群 host 可选）、`internal/handler/chat.go`（群分支按 mode 分派） |
| 前端 | `web/src/components/chat/ChatInputBar.vue`（@ 补全）、`ChatMessageItem.vue`（渲染）、建群抽屉、`useGroupChat.ts`/`useGroupMembers.ts` |

### 13.6 两模式循环内核统一（2026-10-08，重构，行为不变）

> 自由模式落地后，主持人与自由两套循环存在大量逐字重复（回合脚手架、成员发言+游标+密送投递、@User 交回、目标解析、密送落库）。此节记录统一后的抽象，供代码与文档对齐。除「回退成员补系统提示词」（下注）外**无行为变化**。

**共享抽象（`internal/service/group_orchestrator.go`）：**

| 抽象 | 职责 |
|---|---|
| `prepareTurn(groupID)` | 名册加载 + ACP 空闲回收保护（`markGroupMembersActive`）+ runner 解析 + 孤儿流式行清理；调用方 `defer clearGroupMembersActive` |
| `runSpeakerTurn(...)` | **所有发言者的唯一回合路径**（主持人自身、被点名成员、回退成员、汇总回合、自由模式成员都走它）：注入待送达密送 → 构造提示词 → 执行 → **成功推进游标** → 干净回合清账 + 落本回合密送 + `/cb-*` 命令注入（给本回合**第一个**发言者）。游标规则（#69）、密送投递/落库、命令只注入一次——三条规则都不再各写一份 |
| `handUserBack(groupID)` | @User 交回人类、清 User 密送账、写系统行、结束本轮 |
| `drainSpeakers(...)` | **唯一循环内核**：FIFO 队列驱动 + 三条不变量（取消即停整轮 / @User 交回 / 队列空即结束）。主持人模式用有界 `refill`（主持人回合 + 轮数上限 + 失败计数 + 结束信号 + 末尾汇总），自由模式用自扩展 `onSpoke` |
| `memberTurn(m, instruction, s)` | 构造主持人模式下一个成员队列项（公开指令 + 成员系统提示词）。**被点名成员与回退成员共用** |
| `resolveSpeakerTargets(route, members, excludeID)` | 合并原 `resolveTargets` + `resolveMentionNames`：先按行 id 再按名字、排除指定 id 与已离场、保序去重（主持人模式由此也能解析用户按行 id 写的 @） |
| `lookupMember(tgt, byID, byName)` | 目标解析的**唯一匹配规则**（先行 id 再名字），`resolveSpeakerTargets` 与 `appendFreeTargets` 共用 |
| `pickFallbackMember(...)` | 从原 `speakNextMember` 拆出的纯选取逻辑（保留轮转语义，#56） |
| `memberInfos(members, excludeID, withUser)` | 名册 → 提示词用的 `HostMemberInfo` 切片；`memberRoster`（全量）与 `activeMemberNamesExcept`（排除主持人 + 追加 User）都是它的薄封装 |
| `activeMembers(members)` | "还能发言的成员"（非离场），自由模式种子与回退选取共用 |
| `storeSpeakerNotes` / `storeRouteNotes` | 密送落库唯一路径（目标校验 + 按解析后名字键控），由 `runSpeakerTurn` 对每个发言者调用 |

**旧名 → 新名对照（文档他处若仍见旧名，以此为准）：**

| 旧名 | 现状 |
|---|---|
| `resolveTargets`（主持人目标解析） | 合并入 `resolveSpeakerTargets` |
| `resolveMentionNames`（自由目标解析） | 合并入 `resolveSpeakerTargets` |
| `speakNextMember`（轮转回退，含执行） | 拆为 `pickFallbackMember`（选取）+ `memberTurn`/`drainSpeakers` 派发 |
| `lastHostOutput` / `lastSpeakerOutput` | 合并为 `lastMemberOutput`（主持人只是成员行，无需两个变体） |
| `publicMentionTargets` | 删除（`route.Speakers` 已是等价的有序去重公开目标列表） |
| `hostPrompt`（主持人回合手写路径） | 删除，主持人回合走 `runSpeakerTurn`（只是 sysPrompt 用 `BuildHostSystemPrompt`） |
| `storeFreeSpeakerNotes` | 删除，统一为 `storeSpeakerNotes`（`runSpeakerTurn` 内调用） |
| `groupMemberTurn.IsHost`（死字段） | 删除；消费方按 `MemberRowID == hostMemberID` 判断 |
| `runRounds` / `runFreeLoop` | **保留**，但退化为 `drainSpeakers` 的薄封装（各自只提供一个 `refill`/`onSpoke` 闭包） |
| `hostSpeechForMembers` | 删除，统一为 `renderMentionsReadable` |
| `StripBccSpans`（Go）/ `stripGroupBccTags`（TS） | 统一为 `StripProtocolTags` / `stripGroupProtocolTags`（§13.2 标签统一） |
| 标签 `<clawbench-speaker>` / `<clawbench-bcc>` | 统一为 `<clawbench-mention>`（+ `private` 属性），见 §13.2 |

**行为修正（唯一一处）**：轮转回退成员此前**没有**成员系统提示词（只有被点名成员有），会诱发"成员自称主持人"；现与 `memberTurn` 共用同一提示词构造。

**顺带修复**：`CreateGroupWithMembers` 的 `gocyclo`/`gocognit` 超阈值（自由模式提交引入，`only-new-issues` 漏报），抽出 `dedupeGroupSpecs` / `groupIdentityFields` / `insertGroupRow` / `insertGroupMemberRows` / `writeGroupModeInline` 降回预算内。

## 14. 并发发言（Parallel Speaking，规划中，2026-10-08）

> 本节是**待实现的设计**，不是现状。动机：桌游类场景需要"**同时发言、互不参考**"——例如一轮里先让 C 单独表态，再让 A、B **同时**行动（狼人夜晚、同时亮牌、并行出招），且 A、B 各自**看不到对方本轮**的发言。现系统**严格串行**（§5.6 / §12 C1），本节的语义恰好比"真并行"弱，因此成本可控。

### 14.1 语义：快照后启动（snapshot-then-launch）

"同时发言、互不参考"的**真正**保证不是游标，而是**调度不变量**：

> **同一并行组内所有成员的提示词，都在任何人开始写时间线之前，从同一份组开始快照构建完毕。**

因为注入规则是 `id > 游标 且作者 ≠ 自己`（§5.2 / 决策 #23），若 A 的提示词在 B 的行落库**之后**才构建，A 就会看到 B——所以"看不到彼此"**必须**靠"先全部构建、再一起启动"，而**不是**靠游标。

由此得出精确的组内语义：

- **注入游标 = 成员各自的 `seen_cursor`（不变）**。它决定"这个人上次之后漏看了什么"——若改成组开始高水位，会把成员**上轮尚未消化**的发言永久跳过（静默丢上下文）。
- **推进目标 = 组开始高水位 `H`**。组内每人成功后，`seen_cursor` 推进到 `H`（而非各自的发言后高水位）——这样下一组开始时，组内没人"看过"本组其他人的发言。
- **组间严格串行**：`errgroup.Wait` 收尾后才构建下一组快照，故下一组能看到上一组的**全部**发言。

**所以不引入新的可见性模型**：注入规则、游标规则都不变，只新增"快照先于启动"这一条调度纪律（当前 `runSpeakerTurn` 把构建与执行混在一起，须拆开——见 §14.4）。

> **评审修正（2026-10-08，Superpower code-reviewer）**：本节初稿把"看不到彼此"归因于"共用组开始游标"，是**逻辑倒置**——在 `id > 游标` 规则下共用游标会让成员**互相看见**。且初稿写"用组开始高水位作游标基准"，会**永久丢弃**成员上轮未消化的上下文。两者已按上文本修正。

### 14.2 决策点

| # | 决策 | 说明 |
|---|---|---|
| P1 | **粒度 = 每个 mention 标签** | "顺序 / 同时"由**发起点名的那一方**逐个 mention 决定，而非群级开关。这样"先一人、再多人同时"的混合节奏可表达。 |
| P2 | **属性承载** | mention 标签新增一个属性 `mode`，取值 `sequential`（默认）| `parallel`。缺省即 `sequential`，故旧消息与未标注场景行为不变。 |
| P3 | **谁可发起** | 主持人（host 模式）与 @ 人的成员（free 模式）都可标注。free 模式里"同时 @"多个成员 = 让他们并行接力。 |
| P4 | **组内不互相注入** | 见 §14.1：靠"快照先于启动"，注入游标仍是各成员自己的。 |
| P5 | **用户消息的 mention 也可带 `mode`** | free 模式种子队列按用户的 mention 分组（首个 `parallel` 组并行）。 |
| P6 | **不做"边写边被看到"** | 明确排除流式互相可见——那是另一套语义，本设计不做。 |
| P7 | **`parallel` 组内禁止 `User`** | 用户无法"同时发言"。并行组的 targets 含 `User` 时按 `sequential` 处理并写一条可见系统提示（否则会像现状一样 `handUserBack` 直接终止整组，静默丢弃同组其他成员）。 |
| P8 | **并行度上限 = 启动上限，非注入上限** | 见 §14.7：超过上限时分批**启动**，但所有成员的提示词**仍从组开始快照构建**，故"互不参考"不被破坏。 |

### 14.3 解析层改动（`internal/grouprouting` + 前端镜像）

**问题**：现在 `Parse` 把所有 mention 的 targets **摊平**成一个有序去重的 `Speakers` 列表，**丢失了每个 mention 的边界**。而 `mode` 是**每个 mention 各自的**。

**改动**：
- `MentionEntry` 新增 `Mode` 字段（`"sequential"` | `"parallel"`）；`parseMentionAttrs` 一并解析 `mode`（未知值按 `sequential` 处理，不视为畸形）。
- `Result` **新增**按 mention 分组的分组视图：`Groups []MentionGroup`，每组 = `{Members []string, Parallel bool, Instruction string}`。**`Groups` 是新的真源，`Speakers` 由它派生**（保持既有消费者不变）——**方向不能反**：`Speakers` 已跨 mention 去重，无法还原边界。
- **跨组去重保留**：同一成员若同时出现在 `parallel` 组与后续 mention 里，仍按现有 `resolveSpeakerTargets` 语义去重（否则一轮内说两次）。
- **畸形契约不变**：只有 `targets` 属性是良构的判定依据；`mode` 非法值**不使标签畸形**（回退 `sequential`）。
- **前后端镜像 + parity 语料**：`web/src/utils/groupRouting.ts` 同步；`testdata/parity_corpus.json` 增加并行/混合/非法 mode/含 User 四类 case，双向固化。

### 14.4 编排层改动（`group_orchestrator.go`）

`runRounds` / `runFreeLoop` 现在向 `drainSpeakers` 返回**一个** `[]speakerTurn` 队列（严格 FIFO）。改为：

- refill / 种子返回**若干组**（`[]speakerGroup`），组内 `Parallel` 标记；`runRounds` 的**每条 mention 的 `Instruction` 只给该组**（不再是现在的"整条 route 的 Instruction 摊给所有成员"）。
- **两阶段执行**（这是 §14.1 的落地）：
  1. **快照阶段**（串行，启动任何成员之前）：为**整组**每个成员计算注入文本（`GetMemberCursor` + `pendingBccForTarget` + `groupInjectionTextOrEmpty`），取一次 `GroupTimelineHighWater` 作 `H`，并**确定性地**决定 `/cb-*` 命令注入归谁（组内第一个成员）并置 `firstTurnDone`。
  2. **执行阶段**：`Parallel` 组用 `errgroup` 并发跑（每人用**快照好的**提示词与 `H`）；`Sequential` 组沿用现有 FIFO。
- **`runSpeakerTurn` 须拆分**：把"构建提示词"（现 L272-286）与"执行 + 推进游标 + 落密送"分开——否则快照阶段无法在启动前完成。
- **游标**（§14.1）：注入用成员自己的 `seen_cursor`（**不变**）；成功后推进到 `H`（`advanceCursorOnSuccess(t.ID, H, res)`，仍是"仅干净回合推进"，决策 #69 不变）。
- **`/cb-*` 命令注入**：归组内**第一个成员**（在快照阶段确定，**不依赖 goroutine 调度顺序**）。**`firstTurnDone` 是普通 `bool`，并发下是数据竞争**——须在快照阶段（单线程）设置，执行阶段只读。
- **密送延迟送达**：快照阶段已为全组读好 `pendingBccForTarget`，故组内 A 给 B 的密送**必然**落到 `group_pending_bcc`、下组才送达（不会因 A 先跑完而提前注入 B）。
- **`@User`（P7）**：并行组含 `User` 时**拆分**——AI 成员保持 `parallel` 并发执行，随后 `handUserBack` 交回话语权并终止本轮。`User` 恒在队尾（否则其后的成员会被静默丢弃）。

### 14.5 流式管道改动（**关键路径**，§12 C1 的落地）

这是**唯一真正困难**的部分，且**必须先做**，否则并行会内容串台。

**根因**：`UpdateStreamingMessage` / `FinalizeStreamingMessage`（以及 `FinalizeCancelledStreamingMessage`）的定位是
`WHERE project_id=? AND backend=? AND session_id=? AND role='assistant' AND streaming=1 ORDER BY id DESC LIMIT 1`（`chat.go`）——**没有消息 id**。群时间线共享，并行时后启动者的占位行（id 更大）会被先启动者的后续写入命中 ⇒ 互相覆盖。

> **注意 `backend` 谓词**：同 backend 的两个成员才互相覆盖；不同 backend 已天然隔离。故**回归测试矩阵**用"同 backend 一对成员"复现。

**改动**：
1. **新增按消息 id 的原语**：`UpdateStreamingMessageByID(id, content)` / `FinalizeStreamingMessageByID(id, content)` / `FinalizeCancelledStreamingMessageByID(id, content)`（`UpdateMessageContent` 是雏形，但它不改 `streaming` / `completed_at`，不能直接用）。旧原语保留给单聊（单聊天然只有一条流）。
2. **executor 全程用消息 id**：`session_executor.go` 的 **3 处 `UpdateStreamingMessage` + 3 处 finalize**（L1208 的 `FinalizeStreamingMessage`、L1666 的 `FinalizeCancelledStreamingMessage`、L1668 的 `FinalizeStreamingMessage`）改用 `e.cfg.StreamingMessageID`。**取消路径（L1666）必须一并改**——§14.7 要求"取消整组"，漏改它会让被取消的成员覆盖同组的行。
3. **归属键已就绪**：`e.cfg.StreamingMessageID` **已存在**（`RunConfig`，非 `TurnSpec`）且**已被思考/工具调用写入使用**（`thinking.go` / `tool_calls.go` 按 `message_id` 归属）；`run_turn.go` 建占位行时已拿到 id 并放进 `RunConfig`，无需新机制。

**影响面（已核实）**：`UpdateStreamingMessage` 3 个调用点（全在 executor）；finalize 共 6 个——executor 3 个须改，`scheduler.go:955` / `session_command.go:625` / `handler/chat.go:630` 是单聊/panic 路径，保留旧原语；测试引用 3 个文件。

### 14.6 前端路由改动

**根因**：所有内容事件都走 `findStreamingMsg`——**取唯一**的流式气泡（`chatStreamUtils.ts` 的 reducer 内联 `state.find(...)` 有 15 处，`useChatStream.ts` 另有 8 处）。并行时多条流同时存在，取唯一必错。

**须按 id 路由的消费者（穷举，不止 content）**：
- `content` / `content_reset`（后者**清空首个**流式气泡的 blocks——并行下会清错人的输出）
- `thinking` / `thinking_done` / `tool_use` / `tool_result` / `metadata` / `warning` / `ws_error` / `stream_split`
- `stream_finalize`：reducer（`chatStreamUtils.ts`）也是 `forceCleanupStreamingState`=首个匹配，**不是"最新的"**——故成员 2 的 finalize 在成员 1 仍在流式时是 **no-op**。须加按 id 的收尾变体（改 reducer，不只改 dispatch 层）。
- `done`：`_forceCleanupStreamingState` 只收尾**一个**气泡；并行下其余 N−1 个会一直 `streaming:true` 直到 `loadHistory` 重建。

**改动**：
- 后端在每个内容事件上补 `message_id`（`stream_start` 已带；delta 类事件当前不带，见 §14.9 的 `interface.go`）。
- 前端改为**按 `message_id` 路由**到对应气泡；保留"取唯一"作**单流 fallback**（单聊与串行群聊不受影响）。
- **事件缓冲须按 id 分桶**：现 `pendingStreamEvents` 是**全局单桶**，`replayBufferedEvents` 以"存在任一流式气泡"为触发、且被 `connectStream`/`done`/切会话整体清空——并行下成员 2 的缓冲会在成员 1 开占位时被误回放到错误气泡。须改为按 `message_id` 分桶、按 id 触发回放。
- 头部头像栈的"发言中高亮"（O9 已移除）若未来恢复，可同时高亮多个——**本次不做**。

### 14.7 资源与护栏

每个成员是一条独立 ACP 子进程（数百 MB）。顺序执行时"10 人最多 1 个在跑"是隐性护栏；并行会同时拉起 N 个。

- **并行度上限 = 启动上限**（建议 ≤ 3~4）：超出时分批**启动**，但**注入快照仍取自组开始**（§14.1/§14.4），故分批不破坏"互不参考"。**这是 P8 的关键**——若把上限理解成"注入也分批"，第 2 批就会看到第 1 批。
- **成员数上限 10**（决策 #78）不变。
- **取消语义**：用户停止时须取消**整组**（组内所有在飞回合），沿用现有 `groupCtx` 传播；`drainSpeakers` 的"取消即停整轮"不变量扩展到组。**取消路径的 finalize 必须按 id**（§14.5 I1）。
- **DB 压力**：`storeSpeakerNotes` → `lastMemberOutput` 每次全量扫群时间线；并行组内 N 个成员 = N 次并发全表读。非正确性问题，但须与进程内存一并纳入护栏考量。

### 14.8 分阶段实施

> **实施进度（2026-10-09）**：**阶段 1–4 全部实现**（含 §14.1 快照纪律、§14.3 `mode` 属性 + `Groups`、§14.4 两阶段分组执行、§14.5 by-id 原语、§14.6 前端按 id 路由、§14.7 启动上限 + P7/P8、§14.9 多流重连恢复）。后端并行、前端多气泡并行渲染、重连恢复、并发上限全部打通。

| 阶段 | 内容 | 可验证的交付（**注意验收标准**） | 状态 |
|---|---|---|---|
| **1. 后端管道** | §14.5 按消息 id 原语 + executor（含取消路径）改用 `StreamingMessageID` | **单元级**：两个并发 executor、不同 `StreamingMessageID`、同一时间线，断言两行各自正确 finalize（这正是 §12 I6 要求的测试）。此阶段**用户可见行为无变化**（仍串行），故不能靠"界面并行"验收。 | ✅ 已实现 |
| **2. 解析 + 编排** | §14.3 `mode` 属性 + 分组视图；§14.4 两阶段执行 + 组高水位 | **DB 行级**：同 backend 两成员同组并发、两行内容不串台、组间有序。用户可见仍会串台（前端未改）。 | ✅ 已实现 |
| **3. 前端路由** | §14.6 内容事件补 id + 按 id 路由 + 按 id 分桶缓冲 + 按 id 收尾 | 前端多气泡并行渲染正确。 | ✅ 已实现 |
| **4. 语义收尾** | §14.1 快照纪律落地校验；§14.7 启动上限；P7 含 User 降级 | 组内互不参考、组间可见。 | ✅ 已实现 |


> **阶段耦合**：阶段 2 与 3 **必须一起落地**才能有用户可见验收（否则只见后端行正确、界面串台）。阶段 1 单独可验（单元测试），但**没有用户可见产出**——不要按"界面并行"验收它。
>
> **已实现部分的落地差异（2026-10-08/09）**：
> - 按 id 原语**额外带 `session_id` 谓词**（`UpdateStreamingMessageByID(sessionID, id, content)` 等）。原计划只按 id，但 `FlushStreamingNow` 会遍历**所有**已注册 executor，一个陈旧 executor 的行 id 可能与另一会话的活动行相撞 ⇒ 必须用 session 限定（这也是既有 `FlushStreamingNow` 测试的护栏）。executor 传 `e.timelineSID()`。
> - `NewSessionExecutor` 在 `StreamingMessageID` 为 0 时按**时间线会话**解析一次（生产总由 `run_turn.go` 传入；此兜底为测试与旧调用方保留，且带 `store.DBReady()` 守卫）。
> - §14.6 的 `message_id` 放在 **WS 信封层**（`ChatStreamData.MessageID`，`payload` 的兄弟），而非每个 payload 内部——这样所有事件类型统一携带，无需每个 payload builder 感知。
> - **`stream_start` 不再"finalize 上一个流式气泡"**：并行下那个"不同的流式消息"可能是**活着的兄弟流**，finalize 会误杀。改为按 id 判断占位符是否存在；真正陈旧的气泡由其自己的 `stream_finalize`（后端每个 producer 回合都发）或 `done`/reload 收尾。
> - **`forwardEvent` 不得取 `e.mu`**：早期实现为读 `StreamingMessageID` 加了 `e.mu.Lock()`，导致每个事件排在 flush 的 DB I/O 之后，全量 service 测试从 190s 涨到 400s+。`cfg.StreamingMessageID` 只由同一事件循环 goroutine 写，无需锁。
> - §14.7 的**并行度启动上限**已实现（`maxParallelLaunches = 3`）：并行组按批启动，批间 await，但**所有成员的提示词仍从组开始快照构建**（在启动任何一批之前），故分批不破坏"互不参考"——第 2 批看到的是与第 1 批相同的组开始快照。用户取消时不再启动后续批次。



### 14.9 受影响文件（初判）

| 层 | 文件 |
|---|---|
| 标签解析（叶子） | `internal/grouprouting/grouprouting.go` + `testdata/parity_corpus.json` |
| 前端镜像 | `web/src/utils/groupRouting.ts` |
| 编排器 | `internal/service/group_orchestrator.go`（分组 + 两阶段 + 组高水位；`runRounds` 逐组 Instruction；free 的 `freeInitialTargets`/`appendFreeTargets` 分组） |
| 落库原语 | `internal/service/chat.go`（by-id 变体，含 cancelled） |
| 执行器 | `internal/service/session_executor.go`（改用 `StreamingMessageID`） |
| 事件载荷 | `internal/ai/interface.go`（delta 类事件补 `message_id`） |
| 实时状态恢复 | `internal/service/chat.go` 的 `GetLiveRunState`（单行 → **全部**在飞流，见下） |
| 前端流 | `web/src/composables/useChatStream.ts` + `web/src/utils/chatStreamUtils.ts`（按 id 路由 / 分桶缓冲 / 按 id 收尾） |

**重连/恢复（此前遗漏，属前置依赖）**：`GetLiveRunState`（`chat.go`）只取**一条** `streaming=1` 行，`EmitLiveRunStateToClient` 据此只补发**一个** `stream_start`；而前端缓冲回放的**唯一**触发点就是 `stream_start`。故并行下"切走再切回/重连"只会恢复 N 条流中的 1 条，其余成员的流开始事件已错过 ⇒ 须让 `GetLiveRunState` 返回**全部**在飞流、前端处理 N 个补发的 `stream_start`。这是阶段 3 的**硬前置**，不是可选清理。

### 14.10 与既有决策的关系

- **修订决策 #22**（同轮执行 = 顺序）：本设计是它明确预留的 v2 路径（原文"同轮并行是 v2 候选，需先完成 §12 C1 的流式行按 id 重写"）。**#22 对未标注 `mode` 的场景仍然成立**（默认 `sequential`）。
- **落地 §12 C1 的修法**：C1 已给出 by-id 原语方案，本节把它具体化（并补上 C1 未提的取消路径与重连恢复）。
- **不改决策 #9**（成员间只交换发言文本）：并行组内成员之间**连发言文本也不交换**（快照先于启动），比 #9 更严，方向一致。
- **§5.6 顺序不变量**：**`sequential` 路径保留**；`parallel` 路径用 by-id 原语替代"唯一流式行"前提。原 §5.6 的"串行 await"守卫测试须**限定在 `sequential` 路径**（否则并行会误触发它）。
- **决策 #69**（仅干净回合推进游标）：不变，只是推进目标在并行组内取组开始高水位。
- **`golang.org/x/sync`**：`errgroup` 当前是**间接**依赖，引入后变直接（`go mod tidy` 会改 `go.mod`）。

### 14.11 评审勘误（2026-10-08，Superpower code-reviewer，已核对代码）

> 首轮评审（对 §14 初稿）。**结论：目标成立、§14.5 的 by-id 原语是正解，但初稿不可照实现。** 以下 Critical/Important 已并入正文；此节保留原始记录。

**Critical（已修）**

- **C1 — 初稿把"注入游标"与"推进目标"混为一谈。** 初稿写"组内成员用组开始高水位作游标基准（而非各自 `GetMemberCursor`）"。代码里注入用 `GetMemberCursor(t.ID)`（`group_orchestrator.go:274`），推进用 `advanceCursorOnSuccess(t.ID, preH, res)`。若注入也改用组高水位 `H`，则 `(成员游标, H]` 的行被跳过——而那正是**上轮后发言者**的发言，且推进到 `H` 后**永久**成为 `< 游标` ⇒ **静默永久丢上下文**。**修**：注入仍用成员自己的游标，只有推进目标取 `H`。
- **C2 — §14.1 的等价论证逻辑倒置。** 初稿称"组内 A 看不到 B，因为 B 的行 id > A 的游标"——但注入规则正是 `id > 游标 ⇒ 注入`，共用游标会让 A **看见** B。真正机制是**时间性**的：A 的提示词在 B 的行落库**之前**构建。**修**：§14.1 改为"快照先于启动（snapshot-then-launch）"调度不变量。
- **C3 — 提示词构建竞态未处理。** `runSpeakerTurn` 把"构建提示词"与"执行"混在一起；`errgroup` 下成员 2 的构建可能发生在成员 1 首次 flush（executor 每 500ms）之后。**修**：§14.4 拆成"快照阶段（串行）+ 执行阶段（并行）"。
- **C4 — §14.7 的"分批"直接违反 §14.1。** 分批会让第 2 批看到第 1 批。**修**：上限是**启动上限**，注入快照仍取自组开始（P8）。
- **C5 — `firstTurnDone` 数据竞争。** 它是普通 `bool`，`errgroup` 下两个成员可同时读到 `false` ⇒ `/cb-*` 注入两次（`go test -race` 会报）。**修**：在快照阶段（单线程）确定归属。
- **C6 — 重连/恢复只恢复 N 条流中的 1 条（初稿完全未提）。** `GetLiveRunState` 取单行、`EmitLiveRunStateToClient` 只补发一个 `stream_start`，而前端缓冲回放唯一触发点就是它。**修**：§14.9 增列，列为阶段 3 硬前置。

**Important（已修）**

- **I1 — §14.5 漏了取消路径 `FinalizeCancelledStreamingMessage`（L1666）。** 它同样用 `ORDER BY id DESC LIMIT 1`，而 §14.7 恰恰要求"取消整组"。**修**：executor 的 **3 处 finalize**（含取消）全改 by-id。
- **I2 — §14.5 的 WHERE 引用漏了 `backend` 谓词。** 同 backend 才互相覆盖；不同 backend 已隔离。**修**：正文补全 WHERE，并据此定回归矩阵（同 backend 一对成员）。
- **I3 — §14.6 事件枚举不全。** 还有 `content_reset`（会清错人的 blocks）、`thinking_done`/`warning`/`ws_error`/`stream_split`；且 `stream_finalize` 在 **reducer** 里也是"首个匹配"（非"最新的"），成员 2 的 finalize 是 no-op；`done` 只收尾一个气泡。**修**：正文穷举 + 明确须改 reducer。
- **I4 — 前端缓冲是单流形状。** `pendingStreamEvents` 全局单桶、回放以"存在任一流式气泡"触发、整体清空。**修**：按 `message_id` 分桶。
- **I5 — `@User` 在并行组内未定义。** 现状 `handUserBack` 会终止整组、丢弃同组其他成员。**修**：P7 定义（并行组含 User 降级为顺序 + 系统提示）。
- **I6 — 密送可跨并行组提前送达。** 若发送者先跑完 `addPendingBcc`，慢的同组成员可能本轮就读到。**修**：快照阶段为全组读好 `pendingBccForTarget`。
- **I7 — §14.8 阶段 1 的验收标准不可达。** 阶段 1 后仍串行，用户可见无变化。**修**：改为单元级验收（两并发 executor 各自正确 finalize，即 §12 I6 要求的测试），并标注阶段 2↔3 耦合。
- **I8 — 逐组 Instruction / free 模式分组未接线。** `runRounds` 现把扁平 `route.Instruction` 给所有成员；`freeInitialTargets`/`appendFreeTargets` 也消费 `route.Speakers`。且 §14.3 的派生方向写反了（`Groups` 须为真源）。**修**：正文补明 + 反转派生方向 + §14.9 补文件。
- **I9 — §14.10 说"不改 §5.6"是误导。** §5.6 的"串行 await"正是被并行打破的。**修**：改为"`sequential` 路径保留，守卫测试须限定路径"。

**Minor（已采纳）**

- M1 占位 id 放在 `RunConfig`（非 `TurnSpec`）——正文已改。
- M2 `errgroup` 使 `golang.org/x/sync` 从间接变直接——已注明。
- M3 并行组内行 id 顺序**不确定**（下一组按任意序看到，内容无损）——已隐含在"组间串行"表述中。
- M4 跨组去重语义须保留——§14.3 已补。
- M5 `lastMemberOutput` 全表扫 × N 并发——§14.7 已补 DB 压力。
- M6 `rePrivate` 正则 `\bprivate\b` 会匹配任意属性值内的词（既有缺陷，与 §14 相邻）——留待实现时顺手锚定。

### 14.12 实现后评审勘误（2026-10-09，Superpower code-reviewer，已核对代码）

> 第二轮评审（对**已实现**的 `5a43534de` + `660701a72`）。**结论：内核（快照纪律、游标规则、by-id 原语、重连恢复、启动上限）正确扎实，但重构在自由模式上引入两个已复现的回归。** 以下均已修复并加守卫测试。

**Critical（已修 + 变异验证）**

- **RC1 — 自由模式种子误用宿主成员提示词。** `freeInitialGroups` 走 `resolveSpeakerGroups` → `memberTurn` → `BuildMemberSystemPrompt`，该提示词**明确禁止成员输出 mention 标签**——而自由模式的中继**就是** mention 标签，接力从第一步被自己的提示词掐断；且把用户 mention 正文当"主持人要求你"注入（自由模式无主持人，且正文已在用户行出现）。**修**：`resolveSpeakerGroups` 改为接收 `turnFor` 构造器，自由模式传 `freeTurn`（`BuildFreeMemberSystemPrompt`、无 instruction）。守卫：`TestFreeLoop_SeedUsesFreeModePrompt`（变异：改回 `memberTurn` → 红）。
- **RC2 — 自由模式同组内 @ 已排队成员 → 重复发言。** `appendFreeTargets` 的 `waiting` 只含 `appended`（外层队列），**看不到当前组里尚未发言的成员**。用户一条标签点名 A、B，A @B → B 被排队两次（实测 `[A,B,B]` vs 旧版 `[A,B]`）。**修**：`onSpoke` 增加 `pending`（当前组未发言成员）并入 `waiting`。守卫：`TestFreeLoop_PeerMentionWithinGroupNotDuplicated`（变异：去掉 `pending` 并入 → 红）。

**Important（已修）**

- **I1 — `resolveStreamingMsg` 的单流 fallback 会串台。** 带 id 但找不到流式消息时退回"唯一流式"→ 迟到的 finalize 会关掉活着的兄弟流。**修**：带 id 且无匹配 → 直接 `undefined`，不 fallback；无 id 才走单流回退。守卫：前端"重复 finalize 不关兄弟流"。
- **I2 — `message_id:0` 的 finalize 关掉唯一活流。** 成员早失败（无占位符）时 `MsgID=0`，前端把 0 当 falsy → 单流 fallback → 关掉活着的兄弟。**修**：前端 0-id finalize 明确 no-op。守卫：前端"0-id finalize 是 no-op"。
- **I3 — `warning`/`ws_error` 未按 id 路由。** 设计 §14.6 已列入穷举，但只改了 reducer、handler dispatch 未带 `messageId` ⇒ 并行下 warning 永不渲染。**修**：两处 handler 补 `messageId`，reducer `ws_error` 改用 `resolveStreamingMsg`。
- **I4 — P7 只对"User 在组尾"生效。** `User` 在前的并行组降级后，`handUserBack` 立刻终止，**丢弃**其后的成员（实测 `User,A` → `order=[]`）。**修**：降级时 `userLastGroup` 把 User 移到组尾 + 写系统提示（`GroupParallelDowngraded`）。守卫：`TestFreeLoop_ParallelUserFirstStillSpeaksMembers`（变异：去掉重排 → 红）。

**Minor（已修）**

- M1 `queryLiveStreamRows` 补 `rows.Err()`（部分列表 → 整体放弃）。
- M2 `go mod tidy`：`golang.org/x/sync` 从 indirect 移到直接依赖（仅 `go.mod`）。
- M3 文档状态行与 §14.8 的矛盾已消除（状态行改为"阶段 1–4 全部实现"）。
- M4 `forceCleanupStreamingState` 无 id 时恢复"首个流式"语义（`done` 清理循环保证推进），带 id 时仍严格按 id。
- M5 `targets`/`private`/`mode` 正则加属性边界锚定（`(?:^|[\s\p{Z}])`），`data-mode` 不再误匹配；parity 语料加 `mode_hyphenated_attr_not_matched`（35 例）。
