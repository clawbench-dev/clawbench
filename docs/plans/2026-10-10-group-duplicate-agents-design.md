# 群聊支持同一智能体多实例（临时昵称）设计

日期：2026-10-10
状态：已实施（分支 `feat/group-duplicate-agents`）
关联：`docs/spec/features/group-chat.md`

## 1. 目标

允许**同一个智能体被加进同一个群多次**，每次是一个独立「实例」，自动获得唯一临时昵称（`Claude`、`Claude 2`、`Claude 3`…），使 AI 之间的 @ 路由与用户 @ 提及都能精确点名到某个实例。

## 2. 背景：现状与为什么要改

### 2.1 成员就是一条隐藏的 `chat_sessions` 行

群成员当前存为 `chat_sessions` 中 `session_type='group_member'`、`group_id=<群 id>` 的行（`internal/service/database.go:423-440`）。这是**刻意的设计**（`group-chat.md:93`）：成员行天然携带 `agent_id` / `transport` / `model` / `external_session_id` / `auto_approve` / `context_state`，正好是「一条持久 ACP 连接」所需的全部字段，于是连接池、空闲回收、resume/load、CLI `--resume` 全部零改动继承。

成员行 id 同时承担三种角色：

1. **ACP 连接池的 key**（`internal/ai/acp_pool.go:185`）
2. **`chat_history.agent_id` 的发言人标识**（前端据此解析头像/名字、判定主持人）
3. **一组共享会话读写函数的入参**（`GetExternalSessionID` / `GetSessionAutoApprove` / `GetSessionModel` / `GetSessionTransport` / `GetContextState` / `GetSessionAgentID` / `GetSessionFullInfo`）

### 2.2 当前是双层去重

- **前端**：`AgentSelectorDrawer.selectedIds` 是 agent-id 集合（`web/src/components/common/AgentSelectorDrawer.vue:163`），同 agent 只能选一次；加成员抽屉还用 `excludedAgentIds` 把已有成员灰显。
- **后端**：`dedupeGroupSpecs`（`group_store.go:94-111`）在建群时折叠重复 agent；`AddGroupMember`（`group_store.go:311-342`）按 `(group_id, agent_id)` 复用已有行。

去重的根因是**路由按名字匹配**：`memberLookups.byName`（`group_orchestrator.go:1293-1304`）是 name→member 的 map，同名会互相覆盖，导致其中一个成员不可达（"哑巴成员"）。

### 2.3 两个既有缺陷（本需求正好一并修掉）

成员显示名当前存在成员行的 `title` 列，但**读取端并不以 `title` 为准**：

`speakerFromAgent`（`internal/service/speaker_identity.go:66-79`）的逻辑是「只要 `agents` 表里还有该 agent，就用它的当前 `name` 覆盖 `title`，`title` 仅作兜底」。由此产生两个问题：

- **缺陷 1（既有 bug）**：用户改了智能体的 `name`，群里历史发言在**分享/导出快照**里的发言人名会追溯性改变，而应用内（前端直接读 `m.name`，来自成员行存储的名字）显示的是建群快照 → 同一份对话，应用内外名字不一致。（注：改智能体名**不会**改动任何 `chat_sessions` 行——`PatchAgentFields` 只写 `agents` 表；不一致纯粹来自分享路径读了实时 `agents.name`。）
- **缺陷 2（会阻断本需求）**：临时昵称 `Claude 2` 存下来后，`speakerFromAgent` 会用 `agents.name`（`Claude`）覆盖它 → 分享快照里两个实例都叫 `Claude`，昵称失效。

因此本设计必须**把「成员显示名以什么为准」定死**：新增专用列 `agent_name` 作为唯一权威，读取端不再回退 `agents.name`。

## 3. 已确认的决策

| # | 决策 | 选择 |
|---|---|---|
| D1 | 选人交互 | 行内**计数步进器** `− N +`，只显示计数徽标，**不预览昵称** |
| D2 | 昵称规则 | 首个实例用原名 `Claude`；后续实例优先取名池名（`Alice`/`Bob`/…），池尽回退 `Claude 2`、`Claude 3`…；**永不出现 `Claude 1`**（名池为 2026-10-10 后续演进，见 §4.1） |
| D3 | 单实例 | 保持原名，不带序号 |
| D4 | 主持人 | 仅**第一实例**可当主持；主持人仍是**行级**（agent id）操作 |
| D5 | 成员上限 | 按**实例总数**计，上限 **10 → 12** |
| D6 | 序号分配 | **递增不复用**：新实例序号 = 该 agent 已有成员行数（含已离场）+ 1（仅在名池耗尽回退时用作序号） |
| D7 | 头像 | **每实例各生成**（按成员名做种子，落 `agent_avatar`；2026-10-10 后续演进，见 §4.1） |
| D8 | 显示名存储 | **方案 B**：成员行新增专用列 **`agent_name`** 作为成员显示名的**权威来源**；成员行 `title` 留空 |
| D9 | 重新加入 | **一律新建实例**（不 rejoin；放弃连接恢复） |
| D10 | 线格式 | 不变；`memberAgentIds[]` / `agentIds[]` **允许含重复元素** |

### 3.1 为什么选方案 B（成员显示名专用列 `agent_name`）

- **不复用 `title`**：`title` 本是会话标题语义，成员行借用它属于"一列两用"。新增 `agent_name` 让"成员显示名"有独立、无歧义的归属。
- **成员行 `title` 留空**：成员行是隐藏会话，`title` 对它无意义；写入时置空串（列是 `NOT NULL`），显示名只走 `agent_name`。已核实 `title` 的成员读点仅 5 处（全在 `group_store.go`），且 `speaker_identity.go:43` 读的是**群行**的 `title`（非成员行），故留空安全。
- **修掉缺陷 1、2**：`agent_name` 为快照值，读取端**不再回退** `agents.name`，改智能体名不影响群历史名；多实例昵称（`Claude 2`）也因此稳定。
- 迁移：一次 `ALTER TABLE chat_sessions ADD COLUMN agent_name TEXT NOT NULL DEFAULT ''`；存量成员行 `agent_name` 从现有 `title` 回填、`title` 置空（一次性数据迁移）。
- 备选（已否决）：方案 A（复用 `title`，语义不纯）；独立 `group_members` 表（成员行 id 是 ACP 池 key + 历史署名 + 7 个共享访问器入参，搬表需数据迁移与重接连接层，代价与风险高一个数量级）。

## 4. 昵称生成（后端，唯一真源）

命名逻辑从 handler **下移到 service**（需要按群 + agent 查历史行数，只有 service 能访问 DB）。

### 4.1 算法

对 agent `A` 在群 `G` 中命名本批次的每个实例：

```
base  = GetAgentDisplayName(A)                       // 或 spec.DisplayName
n     = countMemberRows(G, A)                         // 含 archived，即历史所有实例
multi = n + batchCount(A) > 1                         // 该 agent 在群内（含本批次）实例数 > 1
k     = n + batchOrdinal                              // 实例序号（1 起，含 archived）
candidate = nextPoolName(used)  if multi              // 多实例：每个实例都取名池未占用名
          = base                otherwise             // 单实例：保留智能体本名
if candidate == "" or collides(candidate, G):
    m = max(k, 2)                                     // 池耗尽 / 本名冲突 → 回退 base + " " + m
    candidate = base + " " + m
    while collides(candidate, G):
        m += 1
        candidate = base + " " + m
```

- **多实例不保留本名**（2026-10-11 调整）：只要该 agent 在群内的实例数 > 1（已存在 + 本批次），它的**全部实例（含第 1 个）都取名池名**，不再保留智能体本名——避免「一个叫 `Claude`、一个叫 `Alice`」这种混排。群内仅此一个实例时才保留本名。
- **已存在的实例不改名**：改名会改写历史快照（历史消息里的 @ 引用的是旧名），故**后续加第 2 个实例**时，先存在的那个保持它已有的名字，**只有新实例**取名池名。因此「多实例不保留本名」对同一批次内命名的实例完全成立；跨批次新增时，先前已命名的实例不回改。
- **永不出现 `base + " 1"`**：池耗尽 / 冲突回退时编号从 2 起。
- **池优先**：多实例从代码内置的英文名池 `groupNamePool`（`internal/service/group_name_pool.go`：`Alice`/`Bob`/…）取第一个未被占用的名字；名池刻意不可配置、无设置 UI。池耗尽才回退 `base + " " + m`。
- **真名预留**（2026-10-11）：池名**永不取**与本批次任一实例本名、或群内已有智能体本名相同的名字。这样真实智能体名**恒被保留**、池确定性避让，且**与 specs 顺序无关**——否则「一组重复实例先取走池名 `Alice`、而另一个 agent 恰好叫 `Alice`」会把后者挤成 `Alice 2`。
- **递增不复用**：`n` 含已离场（archived）行，且 D9 决定一律新建，故回退序号单调递增、历史消息中的昵称永远指向同一个实例。
- **唯一性兜底**：与群内任一成员名、或用户昵称（`IsUserNickname` / `groupUserTarget()`）冲突时继续取下一个未占用池名（池尽则序号 +1），直到唯一。

### 4.2 唯一性的意义

`memberLookups.byName`（`group_orchestrator.go:1293`）与前端 `resolveByName`（`useGroupMembers.ts:37`）都按名字匹配。保证成员名唯一后，主持人路由、自由模式 @、用户 @ 自动补全都能精确命中单个实例。

### 4.2.1 路由标识：AI 用名字、用户用成员行 id（不对称是有意的）

解析入口 `lookupMember`（`group_orchestrator.go:1310-1317`）**先按成员行 id 精确匹配，再按显示名匹配**。两侧用不同标识：

| 来源 | target 取值 | 原因 |
|---|---|---|
| **AI（主持人/成员）** | **显示名**（`<clawbench-mention targets="Claude 2">`） | LLM 拿不到成员行 id，只能写名字（`BuildHostSystemPrompt`，`group_prompt.go:69`） |
| **用户 @ 提及** | **成员行 id**（`buildMentionTag`，`groupRouting.ts:443`） | 前端在 @ 菜单选中时手里就有 id；用 id 是精确引用，改名/重名都不会误路由 |

**不统一为名字的原因**：

- **各用各的最强标识**：用户侧此刻有 id，用 id 绕过归一化/唯一性/大小写等一整类匹配脆弱点；AI 侧只有名字。
- **名字"固化"≠"永不变"**：固化只挡住"智能体改名"；若将来加"成员改名"，或用户手改输入框里的标签，名字路由会解析失败，id 不会。
- **避免"一物两用"**：名字是显示标识、id 是身份标识，分开与 §3.1 的取舍一致。

**对本需求的意义**：AI 之间用名字 ⇒ 名字必须唯一（否则 `byName` 覆盖、成员不可达），这正是当前去重的根因，也是昵称方案（`Claude` / `Claude 2`）解锁多实例的关键；用户侧本就是 id 路由，**天然支持多实例、前端无需改**。

### 4.3 主持人的实例归属

主持人仍以 **agent id** 标识（`hostAgentId`）。建群时取该 agent 的**第一实例**为 `hostMemberId`（D4）。`insertGroupMemberRows` 的 host 匹配逻辑（`group_store.go:268`）需保证在重复 agent 下命中的是首个实例。

## 5. 后端改动

### 5.0 数据模型：新增列 `agent_name`

- `ALTER TABLE chat_sessions ADD COLUMN agent_name TEXT NOT NULL DEFAULT ''`（按现有迁移风格，`database.go` 里加一段 `pragma_table_info` 守卫的 `ADD COLUMN`）。
- **成员行**：`agent_name` = 成员显示名（权威）；`title` 写入空串。
- **非成员行**（`chat` / `group`）：`agent_name` 留空，不受影响。
- **存量数据迁移**（一次性，同一迁移段内）：`UPDATE chat_sessions SET agent_name = title, title = '' WHERE session_type = 'group_member' AND agent_name = ''`（幂等；`agent_name` 已有值则跳过，可重入）。
- 成员显示名读点（`group_store.go` 共 5 处：`findGroupMemberByAgent:581`、`ListGroupMembers:632`、`GroupMembersForGroups:710`、`GroupMemberSpeakersForGroups:775`、`memberDisplayName:874`）**全部改读 `agent_name`**；写入路径（`insertGroupMemberRows:263`、`AddGroupMember` 经 `CreateSession`）改为写 `agent_name` 并令 `title=''`。

### 5.1 `internal/service/group_store.go`

- **删** `dedupeGroupSpecs`，改为 `validateGroupSpecs`：仅做空 `AgentID` 校验 + 实例数上限校验。
- **`CreateGroupWithMembers`**：移除去重；改为按批内顺序为每个 spec 生成唯一显示名（§4.1），写入 `agent_name`；上限按实例数计。
- **`insertGroupMemberRows`**：写 `agent_name`（而非 `title`），`title` 置空；host 指针匹配改为「该 agent 的首个实例」。
- **`AddGroupMember` / `AddGroupMembers`**：移除 rejoin 分支（`findGroupMemberByAgent` 复用逻辑删除），一律新建实例；`netNew` 改为「新实例数」（不再按唯一 agent 去重）；批量调用时按批内出现顺序递增序号。
- **`MaxGroupMembers`**：`10 → 12`；语义注释从「去重 agent 数」改为「实例总数」。
- **`memberNameMap` / `memberLookups`**：无需改（依赖名字唯一，由 §4 保证）。
- **`GroupMember` 结构体**：`Name` 字段语义不变（显示名），数据来源改为 `agent_name` 列。

### 5.2 `internal/service/speaker_identity.go`（修缺陷 1、2）

- `ResolveSessionSpeakers` 的成员循环中，成员发言人名以 `m.Name`（成员行 `agent_name`）为**权威**，**不再回退** `agents.name`。
- `speakerFromAgent` 保留给 `sessionAgent`（会话自身 agent，仍应用 `agents.name`）；为成员另走一条不做名字覆盖的构造路径（backend 兜底与 avatar 查找保留）。
- 头像优先取成员自身快照（`includeAvatar` 时 `m.Avatar` 优先，缺失回退 `agents.avatar`），符合 D7 的 2026-10-10 演进。

### 5.3 `internal/handler/group.go`

- `groupMemberSpecs`：不再在此处填 `DisplayName`（下移到 service）；保留 agent 存在性与 `Disabled` 校验。
- `serveGroupMembersAdd`：同上；允许 `agentIds` 含重复元素。
- 错误映射：`ErrGroupMemberLimit` 现在按实例数触发；响应体 `maxCount` 反映新上限 12。

### 5.4 `session_type` 守卫（堵结构性隐患）

- 会话重命名路径（`PATCH /api/ai/session` → `SetSessionTitleLocked`，`chat_session.go:627`）对 `session_type='group_member'` 的行应**拒绝或忽略**。
- 说明：改用 `agent_name` 后，成员显示名已与 `title` 解耦，误改 `title` 不再影响显示名；此守卫仍建议加（纵深防御，防止对成员行做无意义的会话操作）。

### 5.5 不改动

- ACP 连接层、`external_session_id`、`context_state`、`auto_approve`、`transport`、`model`、级联删除、`agent_store` 用量统计：**均不变**（成员仍是会话行）。

## 6. 前端改动

### 6.1 `AgentSelectorDrawer.vue`

- 新增可选 prop `allowInstances`（**仅群聊两处开启**；单聊 / Task / Fork 行为不变）。
- 选中行右侧（主持人按钮左侧）出现**步进器**：`−` / 计数 / `+`。
  - 未选中 → 点行 = 选中 1 个；已选中 → 点行 = 取消（回 0）。
  - `+` / `−` 调整实例数，减到 0 即取消选中。
  - 只显示数字徽标（D1）。
- 内部：`selectedIds` 允许重复；提交 emit 展开为含重复 id 的数组（`['claude','claude','gpt']`）。
- 主持人按钮：语义不变（agent id），仅对已选中的行显示（D4）。
- `confirmDisabled`：逻辑不变（host 模式 host ∈ members；free 模式 ≥2 个实例）。

### 6.2 `GroupSettingsSheet.vue`（加成员抽屉）

- 移除 `excludedAgentIds`（不再灰显"已是成员"），允许再加已有 agent 生成新实例。
- 启用 `allowInstances`。

### 6.3 其余

- `useGroupChat.ts`：类型不变（`GroupMemberInfo` 等无需新增字段——`name` 仍是显示名，只是后端来源改为 `agent_name`）。
- 头像解析（`useGroupMembers.resolveSpeaker` / `AvatarStack.vue`）改为**优先成员自身头像快照**（`avatar`），缺失回退智能体头像（D7 的 2026-10-10 演进）。

## 7. API 与文档

- **线格式不变**。仅语义扩展：`memberAgentIds[]` / `agentIds[]` 允许重复元素代表多实例；`hostAgentId` 仍为 agent id；成员的 `name` 字段语义不变。
- `internal/api/openapi.yaml`：更新 `/api/group/create`、`/api/group/members` 的字段描述（说明可含重复、上限 12）；无字段增删。
- `docs/spec/features/group-chat.md`：更新成员上限（12）、昵称规则、去重移除说明、成员显示名权威来源改为 `agent_name`；更新数据模型表（`chat_sessions.agent_name`）。

## 8. 边界与影响

- **放弃 rejoin 的连接恢复**（D9 代价）：离场后再加回 = 新实例、新 ACP 进程、新 `external_session_id`。
- **资源**：每个实例 = 一个独立 ACP 子进程；上限 12 即最多 12 个。
- **上限语义变化**：`Claude ×3 + GPT ×2` 现在计 5（此前按去重 agent 计 2）。
- **一次 schema 迁移**：新增 `agent_name` 列 + 存量成员行回填（`agent_name = title, title = ''`）。迁移须幂等、可重入，且对无成员的旧库是 no-op。
- **既有测试需改**：`TestCreateGroupWithMembers_DedupesAgents`（`group_store_test.go:1122-1154`）等钉住去重行为的用例须改为钉住「多实例 + 唯一昵称」；读成员名的用例改断言 `agent_name`。

## 9. 测试计划

**Go**
- 昵称：首实例原名 / 第二实例 `2` / 批量创建递增 / 离场后再加仍递增（不复用）/ 与用户昵称或其它成员名冲突时兜底。
- 上限：按实例数触发 12；`Claude ×13` 被拒。
- 建群：同 agent 多次出现在 `memberAgentIds` 生成多个成员行。
- 加成员：同 agent 可再加（新建实例，非 rejoin）。
- 主持人：重复 agent 时 `hostMemberId` 指向首实例。
- 数据模型：成员行 `agent_name` 有值、`title` 为空；非成员行 `agent_name` 为空、`title` 正常。
- 迁移：旧库（成员行 `title` 有值、无 `agent_name` 列）升级后 `agent_name` 回填正确、`title` 清空；重复执行迁移结果不变（幂等）。
- `speaker_identity`：成员发言人名取 `agent_name`，改 `agents.name` 后分享快照名字**不变**（回归缺陷 1）；多实例分享快照中 `Claude` 与 `Claude 2` 名字不同（回归缺陷 2）。
- `session_type` 守卫：对成员行做会话重命名被拒/忽略。

**前端**
- 步进器计数增减、减到 0 取消选中。
- 提交含重复 id 的数组。
- `confirmDisabled`（host / free 两模式）。
- 主持人仍按 agent id。
- 加成员抽屉不再排除已有 agent。

**E2E**：建群流程补充"同 agent 加两次"用例（`e2e/specs/`，chromium 单跑）。

## 10. 实施顺序（建议）

1. **schema**：`database.go` 新增 `agent_name` 列迁移 + 存量成员行回填（幂等）。
2. service：`validateGroupSpecs` + 昵称生成 + `MaxGroupMembers=12` + 去重/rejoin 移除；成员显示名读写全部切到 `agent_name`。
3. `speaker_identity`：成员名以 `agent_name` 为权威（修缺陷 1、2）。
4. handler：specs 不再填名；错误映射与上限；`session_type` 守卫。
5. 前端：`AgentSelectorDrawer` 步进器 + `GroupSettingsSheet` 去排除。
6. 测试：Go + 前端 + E2E；更新既有去重用例。
7. 文档：openapi.yaml + group-chat.md。
