# 智能体：会话/任务计数、删除守卫、禁用

日期：2026-10-10

## 背景与目标

当前删除智能体只删 `agents` 表那一行（`internal/service/agent_store.go:245-253`），
对 `chat_sessions` / `scheduled_tasks` 完全不做处理——因为两者都只按 `agent_id`
字符串约定关联，**没有任何外键**（`internal/service/database.go:428` 的
`chat_sessions.agent_id` 是普通 `TEXT DEFAULT ''`）。结果是会话与任务被留下，
`agent_id` 变成悬空字符串。

本次要做三件事：

1. **信息组展示计数**：智能体配置面板「信息」区新增「会话数（含归档）」与
   「使用该智能体的任务数」两项。
2. **删除守卫**：删除智能体前必须确保其所有会话、任务（以及群成员关系）都已
   不存在；否则拒绝删除。
3. **禁用开关**：删除按钮旁新增「禁用 / 启用」开关按钮；被禁用的智能体不再出现
   在**任何新建入口**（新建会话、新建任务、群聊加成员）。

## 已确认的产品决策

| # | 决策 | 取值 |
|---|------|------|
| D1 | 删除语义 | **阻止删除**：只要还有会话/任务/群成员关系就拒绝并提示，用户先自行清理 |
| D2 | 隐藏范围 | **所有新建入口**：新建会话、新建任务、群聊添加成员都看不到被禁用的智能体 |
| D3 | 默认智能体 | **不可禁用**（与不可删除一致） |
| D4 | 禁用按钮形态 | **开关式**：未禁用显示「禁用」，禁用后同位置显示「启用」，可反复切换 |
| D5 | 删除被阻止时的交互 | **可点但拦截**：按钮可点，弹出提示「请先删除所有会话和任务」，不执行删除 |
| D6 | 计数范围 | 会话 = **仅交互会话**（`session_type IN ('chat','group')`，含归档）；任务执行自动产生的 `scheduled` 会话**不计入**（任务单独计数，避免「删任务只归档其会话 → 计数永不清零」的死锁）；群成员关系单独作为一条守卫 |
| D7 | 群成员关系 | **也阻断删除**：X 只是某群普通成员时，需先从群里移除 X 才能删 X |

## 数据模型

### 新增列 `agents.disabled`

- `agents` 表新增 `disabled INTEGER NOT NULL DEFAULT 0`。
- 迁移放在 `internal/service/database.go` 现有 agents 列迁移块内，沿用
  `pragma_table_info` 探测 + `ALTER TABLE` 模式（参照 `auto_approve`、
  `avatar`，见 `database.go:1658-1677`）。
- 同步更新 `AgentDDL`（`internal/service/agent_store.go:67-98`）。
- 同步更新 `TestAgentSchemaMatchesProduction` 的期望列集合
  （`agent_store_test.go:288-299`），否则该测试会因「unexpected column」失败。

### 模型与读写路径

`model.Agent` 新增 `Disabled bool `json:"disabled"``。

所有读取 agents 行的 SQL 都要带上该列，共 3 处（漏一处会让该路径上的 agent
永远读到 `false`）：

- `service.LoadAgentsFromDB`（`agent_store.go:101-157`）
- `model.loadAgentsFromDBRows`（`refresh.go:638-676`）
- 写入：`service.SaveAgent`（`agent_store.go:204-236`）与
  `model.saveAgentToDB`（`refresh.go:475-485`）都要写该列。

**关键陷阱**：`SaveAgent` 是 `ON CONFLICT(id) DO UPDATE` 全列 upsert。
`handler/agent.go:828` 的「刷新模型」路径会用它把**整个** in-memory agent 写回。
若 `Disabled` 没进 SaveAgent 的列清单，刷新模型会把 `disabled` 静默重置为 0。
因此该列必须进 SaveAgent。

`AgentPatch`（`agent_store.go:269-280`）新增 `Disabled *bool`，`PatchAgentFields`
新增对应 `addSet("disabled", 0/1)`。

### 计数与守卫查询（新增，`internal/service/agent_store.go`）

```go
// 交互会话数（含归档），排除任务执行会话与群成员行
func CountSessionsForAgent(agentID string) (int, error)
// SELECT COUNT(*) FROM chat_sessions
//  WHERE agent_id = ? AND session_type IN ('chat','group')

// 任务数
func CountTasksForAgent(agentID string) (int, error)
// SELECT COUNT(*) FROM scheduled_tasks WHERE agent_id = ?

// 群成员关系数（仅未归档的成员行）
func CountGroupMembershipsForAgent(agentID string) (int, error)
// SELECT COUNT(*) FROM chat_sessions
//  WHERE agent_id = ? AND session_type = 'group_member' AND archived = 0
```

三者合并为一个 `AgentUsage(agentID) (sessions, tasks, memberships int, err error)`
供 handler 复用。会话类型常量复用 `store.VisibleSessionTypeInClause`
（= `'chat','group'`，见 `internal/store/session_queries.go:52`），避免硬编码漂移。

## 后端行为

### GET /api/agents — 附带计数

`serveAgentsGet`（`handler/agent.go:104-195`）在返回的每个 agent 上附加
`sessionCount` / `taskCount`。二者是**派生字段**，不进 `model.Agent`（那是持久化
模型），而是像现有 ACP 解析列表一样，通过一份副本或在响应 map 上追加。

实现方式：复用 `serveAgentsGet` 里已有的 `clone := *a` 副本分支思路——但计数
对所有 agent 都需要（不止 ACP）。新增 `agentUsage map[string]struct{...}`
并在 marshal 前附加，或定义一个带计数的匿名结构体切片。选后者更清晰：

```go
type agentWithUsage struct {
    *model.Agent
    SessionCount int `json:"sessionCount"`
    TaskCount    int `json:"taskCount"`
}
```

注意 `clone := *a` 与嵌入指针的组合要保证 `models` 覆盖逻辑不受影响。

计数查询在 `configMutex.RUnlock()` 之后执行（不要持锁做 SQL）。

### DELETE /api/agents — 删除守卫

`serveAgentsDelete`（`handler/agent.go:288-333`）在删除前增加守卫：

```go
sessions, tasks, memberships, err := service.AgentUsage(req.ID)
if err != nil { 500 InternalError }
if sessions > 0 || tasks > 0 || memberships > 0 {
    writeLocalizedErrorf(w, r, http.StatusConflict, "AgentInUse", map[string]any{
        "SessionCount": sessions, "TaskCount": tasks, "MembershipCount": memberships,
    })
    return
}
```

顺序：默认智能体检查 → 存在性检查 → **新增用量检查** → 关闭 ACP 连接 →
删除。守卫必须在关闭 ACP 连接之前，否则被拒绝的删除会误杀连接。

### PATCH /api/agents — 禁用开关

`serveAgentsPatch`（`handler/agent.go:451-720`）新增 `disabled` 字段处理，
与 `auto_approve` 同型：

```go
if v, exists := patch["disabled"]; exists {
    disabled, ok := v.(bool)
    if !ok { 400 InvalidRequestBody }
    if disabled && agentID == model.GetDefaultAgentID() {
        writeLocalizedErrorf(w, r, http.StatusBadRequest, "CannotDisableDefaultAgent")
        return
    }
    ap.Disabled = &disabled
}
```

`model.UpdateAgent` 的闭包里同步 `agent.Disabled = *ap.Disabled`。

### 服务端兜底（D8，必做）

前端过滤是主防线，但服务端也应对「向禁用智能体新建会话 / 新建任务」做兜底：
`handler/chat_session.go` 的 POST 与 `handler/scheduler.go` 的 POST 在建会话/任务
时，若目标 agent 处于 disabled，返回 400 + `msgKey=AgentDisabled`。这样即使老
客户端或竞态也拦得住。

注意：兜底只拦**新建**。对已存在会话继续发消息、对已存在任务继续执行**不拦**
（否则禁用会把在跑的任务/会话打死，且用户无法清理——与 D1「阻止删除」的清理
路径冲突）。判据是「这次请求是否在创建一个**新的** session / task」，而非
「目标 agent 是否 disabled」。

## 前端行为

### 计数展示（信息组）

`SettingsAgentDetail.vue`（`settings.items.agentSectionInfo` 块，`:284-315`）在
「模型数量」附近追加两条 `type: 'info'` 项：

- 会话数：`t('settings.items.agentSessionCount', { count })`
- 任务数：`t('settings.items.agentTaskCount', { count })`

数据来源：`useAgents` 的 `AgentRecord` 新增 `sessionCount?` / `taskCount?`，
`loadAgents` 从 `/api/agents` 读取。`SettingsAgentDetail` 在 `onMounted` 已调用
`loadAgents(true)`，故打开面板即拿到最新计数。

### 删除拦截（D5）

`handleDelete`（`SettingsAgentDetail.vue:461-480`）：先本地判断
`sessionCount` / `taskCount`，若 > 0 直接 toast 提示
`settings.items.agentDeleteBlocked`（带数量），**不弹确认框**。同时在
`deleteAgent`（`useAgents.ts:602-605`）里捕获后端 409 `AgentInUse`
（`err.msgKey === 'AgentInUse'`）并向上抛出，供 UI 显示后端权威计数——
防止本地计数陈旧。群成员关系同样纳入本地拦截文案（或统一为「仍在使用中」）。

### 禁用开关按钮

`SettingsAgentDetail.vue` 的 actions 行（`:49-59`）在「复制」与「删除」之间插入
一个开关按钮（D4）：

- 未禁用：图标 + 文案 `agentDisable`，点击 → `patchAgentField(id,'disabled',true)`
- 已禁用：文案 `agentEnable`，点击 → `patchAgentField(id,'disabled',false)`
- 默认智能体：按钮禁用（`disabled` 属性 + 提示），与删除的默认智能体拦截一致

`patchAgentField`（`useSettingsConfig.ts:622-637`）的 `fieldMap` 增加
`disabled: 'disabled'`；`AgentRecord` 增加 `disabled?: boolean`。

### 从新建入口隐藏（D2）

`AgentSelectorDrawer.vue` 是所有新建入口共用的选择器（新建会话
`SessionDrawer.vue:62`、新建任务 `TaskFormPage.vue:307`、群聊加成员
`GroupSettingsSheet.vue:92`、fork `ChatPanelContent.vue:206`）。在
`useAgents` 里提供一个派生 getter：

```ts
const selectableAgents = computed(() => agents.value.filter(a => !a.disabled))
```

`AgentSelectorDrawer` 改用 `selectableAgents` 渲染（当前直接渲染 `agents`，
`:25`）。这样**一处改动覆盖所有入口**，符合 D2。空列表时沿用现有
`chat.messageList.noAgentsTitle` 提示。

注意：群聊**成员列表**（`useGroupMembers` / `GroupMemberStack`）与**已存在会话**
的头像仍应显示被禁用的智能体（禁用只影响「新建」，不影响历史）。

## 国际化

后端 `internal/i18n/locales/active.{en,zh}.yaml` 新增：

- `AgentInUse`: "This agent still has {{.SessionCount}} sessions / {{.TaskCount}} tasks / {{.MembershipCount}} group memberships"（中英各一）
- `CannotDisableDefaultAgent`: "The default agent cannot be disabled"
- （若做服务端兜底）`AgentDisabled`: "This agent is disabled"

前端 `web/src/i18n/locales/{en,zh}.ts` 在 `settings.items.agent*` 块新增：

- `agentSessionCount` — 会话数（含归档）
- `agentTaskCount` — 任务数
- `agentDisable` / `agentEnable` / `agentDisabledBadge`（可选）
- `agentDeleteBlocked` — 「请先删除该智能体的所有会话和任务」
- `agentCannotDisableDefault`（若需要额外提示）

## 测试

**Go**
- `agent_store_test.go`：`CountSessionsForAgent` / `CountTasksForAgent` /
  `CountGroupMembershipsForAgent` 各覆盖（含「任务执行会话不计入」这条 D6 关键
  边界）；`PatchAgentFields` 的 disabled 往返；`SaveAgent` 的 disabled 往返
  （防刷新模型静默重置）；`TestAgentSchemaMatchesProduction` 更新期望列。
- `handler/agent_test.go`：DELETE 守卫（有会话 / 有任务 / 有群成员 各返回 409
  `AgentInUse`，无则 200）；PATCH disabled 成功、默认智能体被拒；GET 附带
  `sessionCount`/`taskCount`。
- 迁移测试：旧库（无 `disabled` 列）升级后 `ALTER TABLE` 生效。

**前端**
- `useAgents.test.ts`：`disabled` 字段解析、`selectableAgents` 过滤。
- `AgentSelectorDrawer.test.ts`：禁用智能体不出现在列表中（含源码守卫钉住
  渲染用的是 `selectableAgents` 而非 `agents`）。
- `SettingsAgentDetail.test.ts`：计数渲染、禁用开关切换调用 PATCH、
  默认智能体开关禁用、删除被计数拦截（不弹确认框）、后端 409 拦截。

**E2E（可选）**：禁用 → 新建会话选择器中消失 → 启用后恢复。

## 影响面清单（改一处必须同改的镜像点）

| 位置 | 改动 |
|------|------|
| `internal/service/agent_store.go` | DDL、SELECT、SaveAgent、AgentPatch、新计数函数 |
| `internal/model/refresh.go` | `loadAgentsFromDBRows` SELECT、`saveAgentToDB` INSERT |
| `internal/model/agent.go` | `Agent.Disabled` 字段 |
| `internal/service/database.go` | `ALTER TABLE agents ADD COLUMN disabled` 迁移 |
| `internal/handler/agent.go` | GET 附加计数、DELETE 守卫、PATCH disabled |
| `internal/handler/chat_session.go` / `scheduler.go` | （建议）服务端禁用兜底 |
| `internal/i18n/locales/active.{en,zh}.yaml` | 新错误键 |
| `internal/api/openapi.yaml` | GET 响应计数、PATCH 字段、DELETE 409 |
| `web/src/composables/useAgents.ts` | AgentRecord、selectableAgents、deleteAgent 错误 |
| `web/src/composables/useSettingsConfig.ts` | patchAgentField fieldMap |
| `web/src/components/settings/SettingsAgentDetail.vue` | 计数项、禁用按钮、删除拦截 |
| `web/src/components/common/AgentSelectorDrawer.vue` | 改用 selectableAgents |
| `web/src/i18n/locales/{en,zh}.ts` | 新 UI 键 |
