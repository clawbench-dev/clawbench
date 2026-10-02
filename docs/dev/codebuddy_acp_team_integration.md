# CodeBuddy ACP Agent Teams：协议实测与 ClawBench 集成方案

本文基于**真机全生命周期探针**（非静态分析）给出 CodeBuddy Agent Teams 在
`codebuddy --acp`（stdio）上的完整协议面，并给出 ClawBench 的集成与展示方案。

- **探针**：`internal/ai/codebuddy_acp_team_probe_integration_test.go`（`-tags integration`）
  - `TestCodebuddyACP_TeamMode_FullProbe` —— create → spawn → messaging → cleanup → loadSession
  - `TestCodebuddyACP_TeamMode_UnsolicitedDrain` —— 后台 worker / drain 轮
  - `TestCodebuddyACP_TeamMode_MemberPermission` —— 成员审批帧的团队归属 meta
- **运行**：
  ```bash
  go test -v -run 'TestCodebuddyACP_TeamMode' -tags integration -timeout 900s ./internal/ai/
  ```
- **静态分析对照**：`docs/dev/codebuddy_acp_extensions.md`（§6.2 已列出 team 相关 meta key）。
- **分析对象**：`@tencent-ai/codebuddy-code` v2.161.1。

---

## 0. 快速结论

| 结论 | 证据 |
|------|------|
| Agent Teams 协议在 **stdio** 上完整可用 | 探针实测 `team_created` / `member_status_change` / `team_deleted` / `team_idle` 全部到达 |
| 团队内容通过 `_meta` 扩展承载，**不新增 ACP 方法** | teamUpdate 挂在标准 `session_info_update` 上；成员内容挂在标准 `agent_message_chunk` 等上 |
| **成员帧不带 `parentToolCallId`** | 116/116 成员帧无该键 ⇒ 现有子代理归组机制**无法复用** |
| 唯一可行的关联键是**成员名** | Agent 生成帧带 `_meta.memberName` + `subagentType`；成员帧带 `memberEvent`=成员名 |
| `unsolicitedTurn`（后台 drain 轮）**本次未触发** | 两测试 0 次；该信号属 **multitask 协调器**，非 team 成员 |
| 权限帧的团队归属（`isTeamMember`/`memberName`/`agentColor`） | ✅ **已确认在 `toolCall._meta`**；默认模式不触发，需 asking 模式 |

---

## 1. 协议面（实测载荷）

### 1.1 团队状态：`session_info_update._meta["codebuddy.ai/teamUpdate"]`

信封是**标准** `session_info_update`，`update` 只有 `{sessionUpdate, _meta}` 两个键。

```json
{"sessionUpdate":"session_info_update",
 "_meta":{"codebuddy.ai/teamUpdate":{
   "type":"member_status_change",
   "teamName":"clawbench-probe",
   "isAutoTeam":false,
   "hasLiveMembers":true,
   "members":[{ "name":"probe-alpha", "agentType":"general-purpose",
                "color":"blue", "description":"…",
                "status":"running", "activity":"working", "lifecycle":"alive",
                "taskId":"agent-08d3…", "sessionId":"01a0fadc-…",
                "tokenUsage":{"inputTokens":56763,"outputTokens":229,"lastContextWindow":28430},
                "toolCallCount":2 }]}}}
```

**实测的事件类型**（`type`）：

| type | 触发时机 | 实测 |
|------|----------|------|
| `team_created` | 显式 `TeamCreate` 成功 | ✅ |
| `member_status_change` | 成员状态任一字段变化（含空 members 快照） | ✅ 每次变化都推 |
| `team_deleted` | 显式 `TeamDelete` 成功 | ✅ |
| `team_idle` | **所有成员终止后自动推**（无需 TeamDelete） | ✅ |
| `team_busy` | 团队进入忙碌 | ⚠️ 未观测（进程在忙碌期被杀） |

**`members[]` 字段**（实测，比官方文档多 `activity` / `lifecycle`）：

| 字段 | 值域 | 说明 |
|------|------|------|
| `name` | string | 成员名（**关联键**） |
| `agentType` | string | 子代理类型（如 `general-purpose`） |
| `color` | string | 显示色（blue/green/…） |
| `description` | string | 成员职责（来自 spawn prompt） |
| `status` | pending/running/completed/failed/killed | 任务状态 |
| `activity` | **starting/working/idle** | 活动状态（新增，文档未提） |
| `lifecycle` | **alive/terminated** | 生命周期（新增，文档未提） |
| `taskId` | string | `agent-<uuid>` |
| `sessionId` | string | 成员子会话 id（**早期快照可能缺**） |
| `tokenUsage` | {inputTokens,outputTokens,lastContextWindow} | 逐次累积 |
| `toolCallCount` | int | 工具调用数 |

> **三个状态字段独立演进**：实测 `status` 先转 `completed`，`activity` 转 `idle`，
> `lifecycle` 最后转 `terminated`。UI 应分别理解，不能只取其一。
> **`isAutoTeam` 仅部分快照带**（首个 `member_status_change` 常缺），实现须容忍缺省。

### 1.2 成员内容：`_meta["codebuddy.ai/memberEvent"]`

成员产生的每一帧（文本 / 思考 / 工具 / 用户回显）都带 `memberEvent`=成员名：

```json
{"sessionUpdate":"agent_message_chunk",
 "content":{"type":"text","text":"正在分析…"},
 "_meta":{"codebuddy.ai/memberEvent":"probe-alpha",
          "codebuddy.ai/memberHistoryItemId":"01a0fadc-…"}}
```

实测覆盖的 `sessionUpdate` 种类：`agent_message_chunk`、`agent_thought_chunk`、
`tool_call`、`tool_call_update`、`user_message_chunk`。
`memberHistoryItemId` **仅部分帧带**（spawn 阶段 116 帧里 13 帧）。

### 1.3 成员 ↔ 工具调用的关联（**实现关键**）

实测：**成员帧 116/116 不带 `parentToolCallId`**。现有 ClawBench 子代理归组
（`internal/ai/acp_parent_link.go`，靠 `codebuddy.ai/parentToolCallId`）**无法路由团队成员**。

唯一可用的 join key 是**成员名**，链路为：

```
Agent 生成帧 (tool_call, _meta.memberName="probe-alpha", _meta.subagentType="general-purpose")
   toolCallId = call_00_gQeF1pbxpR25Ip7O4kwj0521
        │  （建立映射：memberName → toolCallId）
        ▼
成员内容帧 (_meta.memberEvent="probe-alpha")
```

- Agent 生成帧同时携带 `memberName` + `subagentType` + `isBackground`，其 `toolCallId`
  是**唯一同时含成员名与工具 id 的位置**。
- 成员帧里的**工具名可能被改写为 `@<成员名>`**（实测 `tool_use.name` 出现过
  `@probe-alpha`），真实工具名以 `_meta["codebuddy.ai/toolName"]` 为准
  （ClawBench 已优先取该键，见 `acp_codebuddy_tool.go`）。
- loadSession 回放帧里 Agent 生成帧**仍带 `memberName`+`subagentType`**，故映射可从历史重建。

### 1.4 后台 drain 轮：`_meta["codebuddy.ai/unsolicitedTurn"]`

**本次未触发**。该信号属 **multitask 协调器**（detached worker 完成后在父会话 idle 时
再跑一轮汇总），不是 team 成员路径。文档 §5「非用户发起的 drain 轮」描述完整，但
需要 multitask overlay 开启 + 真正的 detached worker 才会出现。**接入 team 模式不依赖它**；
若要支持 multitask，须单独验证。

### 1.5 成员审批帧（已实测确认）

成员执行需审批的命令时，`session/request_permission` 的 **`toolCall._meta`**（注意：不是
`request._meta`）携带团队归属：

```json
{"method":"session/request_permission",
 "params":{"toolCall":{"toolCallId":"call_00_IXnR9…",
   "_meta":{"codebuddy.ai/isTeamMember":true,
            "codebuddy.ai/memberName":"probe-perm",
            "codebuddy.ai/agentColor":"blue",
            "codebuddy.ai/toolName":"Bash"}}}}
```

- **归属信息在 `toolCall._meta`**；`request._meta` 只有 `baggage` + `requestId`。
- **默认权限模式下团队成员拥有完整工具访问**（实测 0 次审批请求）；必须显式切到
  asking 模式（`default` / `ask` / `plan` 之一，经 `session/set_mode`）才能触发审批。
  这与官方文档「Subagent 默认使用 `default` 权限模式（完整工具访问）」一致。

### 1.6 仍未验证项

- **`team_busy`**：需在团队忙碌期持续观察。
- **`teamResumeIntent`**：需在成员运行中 kill 进程再 resume。

---

## 2. ClawBench 现状（缺口）

| # | 缺口 | 位置 |
|---|------|------|
| 1 | `session_info_update` 是**死分支**（只 `slog.Debug`） | `internal/ai/acp_events.go:401` |
| 2 | **不读 `memberEvent`** → 成员内容全塌进主气泡 | `acp_events.go:139/158/173` |
| 3 | 权限帧不读 `isTeamMember`/`memberName`/`agentColor` | `internal/ai/acp_client.go:358+` |
| 4 | `unsolicitedTurn` 未处理（multitask 相关，暂不阻塞） | 同 1 |
| 5 | `memberHistoryItemId` / `historyReplay` / `teamResumeIntent` 未消费 | 同 1 |

实测 R6 确认：当前映射把成员内容产出为普通 `content`/`thinking`/`tool_use`/`tool_result`，
teamUpdate 被丢弃。

---

## 3. 集成方案（后端）

### 3.1 新增 StreamEvent 类型

在 `internal/ai/interface.go` 的 `StreamEvent` 增加：

```go
// Type=team_update：团队/成员状态快照（来自 session_info_update 的 teamUpdate meta）
Team *TeamState

// Type=team_member：成员内容路由（文本/思考/工具）的成员归属
// 复用现有字段：Content/ThinkID/Tool + 新增 MemberName
MemberName string
```

`TeamState` 建议形状（与 wire 对齐，前端可直接用）：

```go
type TeamState struct {
    Type        string        `json:"type"`        // team_created/team_deleted/member_status_change/team_idle/team_busy
    TeamName    string        `json:"team_name"`
    IsAutoTeam  bool          `json:"is_auto_team,omitempty"`
    HasLive     bool          `json:"has_live_members"`
    Members     []TeamMember  `json:"members"`
}
type TeamMember struct {
    Name, AgentType, Color, Description string
    Status, Activity, Lifecycle         string
    TaskID, SessionID                   string
    TokenUsage  *TeamTokenUsage         `json:"token_usage,omitempty"`
    ToolCallCount int                   `json:"tool_call_count"`
}
```

### 3.2 解析点（`internal/ai/acp_events.go`）

1. **`session_info_update` 分支**：读 `_meta["codebuddy.ai/teamUpdate"]` → 发 `team_update`。
   保持分支对无该 meta 的普通 `session_info_update`（title/updatedAt）无副作用。
2. **内容帧分支**（`AgentMessageChunk` / `AgentThoughtChunk` / `ToolCall` / `ToolCallUpdate` /
   `UserMessageChunk`）：读 `_meta["codebuddy.ai/memberEvent"]` → 填入 `StreamEvent.MemberName`。
   与现有 `extractParentToolCallID` **并列**（互不冲突：团队帧无 parent，子代理帧无 member）。
3. **成员 ↔ 工具映射**：在 `ToolCall` 分支识别 Agent 生成帧
   （`_meta.memberName` + `_meta.subagentType` 同时存在），记录 `memberName → toolCallId`；
   成员内容帧据此回填 `ParentToolCallID`，**从而复用现有的 `ContentBlocks.vue` 归组渲染**。
   - 映射表须按 ACP session 作用域存放，随会话生命周期清理。
   - 回放（loadSession）路径同样经此分支，故历史会话的映射自动重建。

> **设计取舍**：方案 A（复用 `parent_tool_call_id`）改动最小 —— 前端零改动即可把成员
> 内容归到 Agent 卡下；方案 B（新增独立 `member_name` 字段）信息更全（成员色/状态可直达
> 消息层），但需前端新增路由。**建议先 A 后 B**：A 让数据立刻可用，B 在团队 UI 落地时补。

### 3.3 持久化

- `TeamState` 是**会话级实时状态**（同 `mode_update`/`usage_update`），**不逐条落库**。
- 成员内容的 `ParentToolCallID` 已随 `ContentBlock` 落库（`internal/model/chat.go`），
  故重载后归组不丢。
- 团队状态在重载后由 loadSession 重放（实测回放帧含 Agent 生成帧的 `memberName`，
  可重建成员列表与映射；teamUpdate 重推本次未观测，需前端容忍"重载后短暂无团队栏"）。

### 3.4 WS 转发（`internal/ws/stream_hub.go`）

- 新增 `team_update` → `acpStatePayload` 加分支，输出 `event.Team`。
- 成员内容复用现有 `simpleTextPayload` / `toolUsePayload` / `toolResultPayload`，
  在 `parent_tool_call_id` 旁增加 `member_name`（方案 B 时）。

---

## 4. 展示方案（前端）

设计约束见 [`docs/spec/client/design-guide.md`](../spec/client/design-guide.md)：
状态用**动效而非颜色**承载（36 套主题里 3 套 `--accent-color` == `--color-orange`）、
无内容不占位、动效承载信息时不做 `prefers-reduced-motion` opt-out、
共享类基规则必须在全局 CSS。

### 4.1 团队状态栏（新增 `TeamPanel.vue`）

位置：`ChatPanelContent.vue` 中 `PlanPanel` 与 `QueuedMessageBar` 之间（聊天列表之下、
输入框之上）。折叠 ↔ 展开两形态，直接复用 `PlanPanel.vue` 的 chip/expanded 范式。

**展开态：**

```
┌─────────────────────────────────────────────┐
│ Team · clawbench-probe         2 active  ⌄  │   ← 标题行可点折叠
├─────────────────────────────────────────────┤
│ ● probe-alpha   running    Bash×2   85.2k   │   ← 名字用 members[].color 着色
│ ✓ probe-beta    completed  —        85.2k   │
└─────────────────────────────────────────────┘
```

**收起态**（单行 chip，复用 `.plan-chip` 范式）：

```
┌─────────────────────────────────────┐
│ ● probe-alpha 正在运行        1/2  ⌃ │
└─────────────────────────────────────┘
```

- **名字着色**：wire 的 `members[].color`（`blue`/`green`/…）映射到主题色，与官方 TUI 一致。
- **状态点复用会话行状态槽语义**（design-guide §动效）：`running` 脉动点、
  `completed` 静止 ✓、`failed` 红 ✗、`killed`/`terminated` 灰 —。
  **不新增环**——「正在推进」已由脉动点表达，同一事实不说两遍。
- **右侧计量**：`toolCallCount` + `tokenUsage.inputTokens + outputTokens`（缩写 `85.2k`）。
- **tooltip**：`description`（成员职责）+ 完整 token 明细。
- **无团队时整块 `v-if` 不渲染**，不占位。
- 状态来源：`team_update` 事件 → 会话级 composable（仿 `usePlanProgress.ts`）。

### 4.2 成员时间线（内容路由）

**第一阶段（方案 A）**：成员内容已通过 `parent_tool_call_id` 归到 Agent 卡下，
现有 `ContentBlocks.vue` 的 `childBlocksByParent`（按 `parent_tool_call_id` 归组、
递归渲染、左侧 2px 粉紫边框 `--subagent-accent`）**直接生效 —— 零前端改动**。

```
▸ Agent · probe-alpha                    3 steps  ⌄
    ├ 💭 正在分析…（成员思考）
    ├ ⚡ Bash  echo TEAMPROBE-ALPHA
    └ 📄 TEAMPROBE-ALPHA
```

**第二阶段（方案 B）** 在递归分组上叠加成员维度，两处差异：
- 卡标题用 `memberName` + `subagentType`（当前用 `display_name`）。
- 左边框色用**成员 color** 而非固定 `--subagent-accent`（#ec4899），
  使时间线与状态栏的成员配色一致。

### 4.3 权限卡成员徽标

成员审批卡（现有 `PermissionApproval`）在标题旁加一枚成员徽标：

```
┌─ 权限请求 ────────────────────────────┐
│ 🔵 probe-alpha · Bash                 │   ← 成员色圆点 + 名字
│ rm -f /tmp/…                          │
│ [允许] [拒绝]                         │
└───────────────────────────────────────┘
```

数据来自 `requestPermission` 的 **`toolCall._meta`**（`isTeamMember`/`memberName`/`agentColor`，
见 §1.5），需在 `internal/ai/acp_client.go` 的 `RequestPermission` 里提取并透传。

### 4.4 状态语义（`status` / `activity` / `lifecycle` 三者独立）

实测三者**独立演进**（`completed` 先于 `terminated`），不能只读一个：

| wire 字段 | 取值 | 视觉 |
|-----------|------|------|
| `status` | pending / running / completed / failed / killed | 主状态点 + 状态文字 |
| `activity` | starting / working / idle | 副标题（"启动中/工作中/空闲"） |
| `lifecycle` | alive / terminated | `terminated` → 整行降透明度（成员已回收） |
| 顶层 `hasLiveMembers:false` + `team_idle` | — | 状态栏折叠为「Team · 已结束」或隐藏 |

### 4.5 生命周期行为

| 事件 | 界面 |
|------|------|
| `team_created` | 状态栏出现（可带动效滑入） |
| `member_status_change` | **就地更新**行状态/计量（不整块重挂载，避免闪帧） |
| `team_deleted` | 状态栏淡出；成员时间线**保留**（历史内容不删） |
| `team_idle` | 折叠为「已结束」，不消失 |
| loadSession 重载 | 由回放帧重建成员列表；容忍「重载后短暂无状态栏」 |

### 4.6 实现约束

- **宽窄屏**：状态栏在聊天列内，随面板宽度自适应；窄屏同样位于输入框上方。
- **i18n**：所有文案走 `t('chat.team.*')`，不硬编码中文。
- **动效**：时长用 `--duration-*` token；状态点脉动**不做** `prefers-reduced-motion`
  opt-out（脉动承载「正在跑」的信息，与 `.session-status` 先例一致）。
- **共享类**：若 `TeamPanel` 复用 `.plan-chip` 之类共享类，基规则须在全局 CSS，
  组件 scoped 只留布局（design-guide 红线 2）。

---

## 5. 实施顺序与验证

| 步骤 | 内容 | 验证 | 状态 |
|------|------|------|------|
| P0 | 补探针：成员审批帧 meta | 扩展本文件探针 | ✅ |
| P1 | 后端解析 `teamUpdate` → `team_update` StreamEvent | `codebuddy_team_bridge_test.go` | ✅ |
| P2 | 后端读 `memberEvent` + 建成员↔工具映射 → 回填 `ParentToolCallID` | 同上（含变异验证） | ✅ |
| P3 | WS 转发 `team_update` + `member_name` | `stream_hub_test.go` | ✅ |
| P4 | 前端 `TeamPanel.vue`（§4.1/§4.4/§4.5）+ `useTeamState.ts` | `TeamPanel.test.ts` / `useTeamState.test.ts` | ✅ |
| P5 | 权限卡成员徽标（§4.3）—— 后端已透传 `teamMember`，前端徽标 UI 待做 | 依赖 P0 | 🟡 部分 |
| P6 | 成员时间线方案 B（§4.2 第二阶段，成员配色） | 视觉回归 | ⬜ 待做 |
| P7 | E2E：团队面板（bridge 注入） | `e2e/specs/team-panel.spec.ts`（7 passed） | ✅ |

**已落地文件**：

- 后端：`internal/ai/codebuddy_team_bridge.go`（解析 + 成员 join）、
  `interface.go`（`TeamState`/`TeamMember`/`StreamEvent.Team`/`.MemberName`）、
  `acp_pool.go`（`cachedTeamState` + `memberToolCallIDs`，stateMu 叶锁）、
  `acp_events.go`（`session_info_update` 分支 + 成员归因）、
  `acp_tool.go`/`acp_debounce.go`（tool 映射带 conn）、
  `acp_backend.go`（重连重放团队状态）、`acp_client.go`（权限帧 `teamMember`）、
  `internal/ws/stream_hub.go`（`team_update` + `member_name`）。
- 前端：`web/src/composables/useTeamState.ts`、`web/src/components/chat/TeamPanel.vue`、
  `useChatStream.ts`（`team_update` 分支）、`useChatSession.ts`/`App.vue`（会话切换清理）、
  `ChatPanelContent.vue`（挂载）、i18n `chat.team.*`。

**回归红线**：改动不得影响非团队会话 —— `session_info_update` 分支对无 `teamUpdate`
的更新（title/updatedAt）必须保持原有行为（已由 `TestMapACPSessionUpdate_SessionInfoNoTeamNoEvent`
与 `TestBridgeTeamUpdate_NoopWithoutKey` 钉住）；子代理（非团队）内容仍走 `parentToolCallId`
（`memberParentOrParent` 优先显式 parent）。

---

## 6. 待办与风险

- **待验证**：`team_busy` 触发条件；`teamResumeIntent` 语义（成员运行中 kill 再 resume）。
- **风险**：`isAutoTeam` 部分快照缺省；`memberHistoryItemId` 仅部分帧带；
  `team_idle` 与 `team_deleted` 语义需区分（前者是成员全终止，后者是显式删除）。
- **多团队**：官方限制每会话仅一个团队，协议未暴露 team 列表，实现按单团队处理。
- **unsolicitedTurn**：属 multitask，接入 team 不需要；若后续支持 multitask 须单独探针。
