# 通知中心（Notification Center）设计

日期：2026-10-15
状态：设计已定稿，待实现

## 背景与问题

当前 ClawBench 的通知是**瞬时投影**，不是可回溯的数据：

- 三类事件（`session_update` / `task_update` / `forge_event`）经 WS 广播，由五个独立通道消费：应用内完成卡片（`CompletionPopover`）、浏览器通知、Electron 原生通知、Android 原生通知、IM 推送（钉钉/飞书）。
- 唯一的持久化存储是 `pending_events`，但它是**回放日志**而非可浏览列表：IM 推送成功后即删除该行、24h/7d 过期、上限 1000 条。
- 已读状态分散在三套语义不同的模型里（`chat_sessions.last_read_at` 水位线 / `task_executions.read_at` 每行 / `forge_events.read_at` 每事件），且没有任何一处能回答「过去 3 天发生过哪些通知」。

用户需求：把所有通知汇聚到一个可回溯的列表里查看。

## 决策汇总

| 维度 | 决策 |
|---|---|
| 入口 | 左栏新增 `notifications` dock 页签（宽屏竖排 / 窄屏底部，共用同一面板组件） |
| 数据源 | **单表 `notifications` 是通知中心的唯一数据源**（列表/筛选全单表、零 join） |
| 覆盖范围 | 与现有通知**完全一致**：会话 completed/cancelled/permission_pending、任务 running/completed/failed/cancelled、forge 六种 |
| 粒度 | 每条事件一行（扁平时间线，卡片式列表） |
| 项目范围 | 全局（所有项目） |
| **已读/未读** | **不存在**。通知是只追加的事件日志，不区分已读未读（详见下节） |
| 角标 | **无** |
| 筛选 | 项目 / 类型 / 时间段（三个下拉，复用 `MenuSelect` + `PopupMenu`） |
| 保留 | **30 天 且 最多 500 条**（双上限） |
| 跳转 | 点击跳转事件源；源不存在时标注「源已删除」，点击**不发跳转、就地提示** |
| 源探测 | 列表渲染时**批量探测**，给每行打 `sourceMissing` |
| 三个既有角标 | **本次不动**（chat/tasks/forge 保持各自模型） |

## 明确不做：通知的已读/未读

**通知中心不引入已读概念，是一张只追加的事件日志。**

理由（用户判断，经设计推演确认）：通知的本质是「事情发生了，记一笔」，不是收件箱。它要么已在应用内完成卡片弹过、要么已在钉钉/飞书弹过、要么用户翻日志时看到——再叠加一层「已读」状态是多余的。

这一决策一次消掉四处复杂度，是本设计最关键的简化：

- 不需要 `read_at` 列
- 不需要「主体已读 → 通知已读」或反向的任何同步/回写
- 不存在「我读了会话，通知却不更新」的矛盾（因为无「更新」可言）
- 不存在「新回复让历史通知重新变红」的问题
- 不需要「全部已读」按钮，也不需要已读/未读筛选

**曾评估并否决的两个方案**（记录于此以免回头重走）：

1. **通知表自持 `read_at` + 双向回写** — 两份已读记录靠同步保持一致，漏写一处即「读了不更新」；且「全部已读」会连带清空三个既有角标。
2. **已读派生自主体（零双写）** — 会话通知用 `notification.created_at <= chat_sessions.last_read_at` 判定，任务/仓库查各自 `read_at`。虽为真·单源，但读取需 join，且会话粒度下「读一个会话 = 其全部通知变已读」；既然最终决定不要已读，此方案一并放弃。

## 明确不做：不统一三个既有角标

调研后确认，把 `notifications` 表向上蔓延为三个既有角标的已读源**不是存储重构，而是三个角标语义的重新定义 + 跨模块数据迁移**，收益远小于风险。三条硬证据：

1. **计数单位根本不同**：chat 数**会话数**（有未读或待审批）、task 数**执行数**、forge 数**条目数**（`COUNT(DISTINCT item_key)`，一条 issue 上 3 条评论 = 1）。单表无法在不加 per-kind 聚合规则的前提下同时保住三者。
2. **chat 角标不是事件派生的**：它读 `chat_history` vs `chat_sessions.last_read_at` 水位线，与事件日志无关。而 `StoreNotifiableEvent` 受 `push_mode != "disabled"` 门控——**push_mode=disabled 时事件不落库但 chat 角标照常工作**。改为由通知表派生会让这些用户角标永不亮，是明确的功能回归。chat 角标还折入了 `pendingApproval`（状态，非事件）。
3. **性能路径会被推翻**：`unreadCountSubquery` 靠 `idx_history_sess_unread` 把 186ms 压到 0.01ms（源码注释明确记载）；`chat_sessions.last_read_at` 与 `task_executions.read_at` **目前都没有索引**。改为扫通知表等于重造一套。

连带影响还有：`pending_events.go` 的 `suppress_notification` 门控、Android `FloatingStatusController` 的独立 `unreadSessions` 集合与 `/api/ai/sessions/overview` 载荷形状、forge「角标与列表过滤必须同源」的既有测试约束。

因此：**通知中心与三个既有角标是两组独立的数**，各答各的问题（角标答「现在有什么要处理」，通知中心答「发生过什么」）。点开通知会跳转到源，打开会话本身触发 `UpdateLastRead` 会顺带清掉对应角标；但通知中心自身**既无角标、也无已读**，与那三者零交互。

## 数据层

```sql
CREATE TABLE IF NOT EXISTS notifications (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id       TEXT UNIQUE,        -- 源 WS event id，去重用
  kind           TEXT NOT NULL,      -- session | task | forge
  event_type     TEXT NOT NULL,      -- completed / cancelled / permission_pending / failed / running / merged ...
  title          TEXT NOT NULL,
  body           TEXT DEFAULT '',
  project_id     INTEGER,            -- 筛选列（复用项目注册表）
  session_id     TEXT DEFAULT '',    -- 跳转目标
  task_id        TEXT DEFAULT '',
  execution_id   TEXT DEFAULT '',
  forge_item_key TEXT DEFAULT '',
  agent_id       TEXT DEFAULT '',
  created_at     TEXT NOT NULL       -- 亚秒精度（strftime('%Y-%m-%dT%H:%M:%fZ')），保证同秒多条可稳定排序
);
CREATE INDEX IF NOT EXISTS idx_notifications_created ON notifications(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_filter ON notifications(project_id, kind, created_at);
```

**无 `read_at` 列** —— 通知是只追加的日志，已读/未读概念不存在（见上节）。

`project_id` 复用项目注册表；`project_id = 0` 是既有哨兵（无法归属的事件），沿用不新增语义。

`created_at` 用**亚秒精度文本**而非 `DATETIME DEFAULT CURRENT_TIMESTAMP`（后者只到秒）：同一秒内可能产生多条通知（一次 forge 轮询 drain 一批），秒级时间戳会让它们无法稳定排序，翻页时可能重复或漏行。

## 三条写入路径

关键约束：**不复用 `pending_events` 的任何门控**。`StoreNotifiableEvent` 受 `push_mode != "disabled"` 与 `AllConnectedClientsSubscribe` 双重门控——通知中心必须无视这两条（push 关 ≠ 不记录；没人在看也要记录）。

1. **会话** — 在 `emitSessionEvent`（`internal/service/session_runtime.go:77`）与 `pushSessionTerminal`（`:204`）内，紧挨 `StoreNotifiableEvent` 调 `StoreNotification(...)`，**独立门控**。
2. **任务** — 在 `emitTaskEvent`（`internal/service/scheduler.go:737`）内并列写入。
3. **仓库** — 新增 `ForgeNotificationRecorder` sink，经 `NewForgeCompositeSink(...)`（`internal/service/forge_sink.go:17`）与现有 dispatcher / task trigger 并列注册。

三处均以 `event_id`（`ws.GenerateEventID()`）作 `INSERT OR IGNORE` 去重键，与 `pending_events` 同源。

## API（全部同步 `internal/api/openapi.yaml`）

| 方法 | 路径 | 作用 |
|---|---|---|
| GET | `/api/notifications` | 列表：项目 / 类型 / 时间段筛选 + 分页；响应含每行 `sourceMissing` |
| DELETE | `/api/notifications` | 手动清空（可选带筛选，或全清） |

**无已读端点**（无已读概念）。角标端点也不需要（页签无角标）。

字段名必须从 handler 代码抄（`decodeJSON` 结构体 tag、`r.URL.Query().Get(...)`、`requireMethod(...)`），路由唯一来源是 `internal/handler/handler.go` 的 `RegisterRoutes`。

### 源存在性批量探测

列表查询后，对当页**去重后**的 `session_id` / `task_id` 各做一次 `SELECT 1 ... WHERE id IN (...)`，给不存在的行打 `sourceMissing = true`。覆盖所有删除路径、零新增删除钩子。

- **归档 ≠ 已删除**：`chat_sessions.archived = 1` 的行仍在（可 resume），探测只看行是否存在，不看 `archived`。
- **forge 条目不参与探测**：条目在上游（GitHub/GitLab），本地无删除语义，故 forge 通知永不标 `sourceMissing`。这是有意的范围收窄（若需要，可另按「仓库绑定是否仍存在」扩展）。

### 保留策略

30 天 且 500 条双上限，接入现有周期性清理 worker 模式（参照 `CleanupPendingEvents`）。两条 SQL：先删 `created_at < now-30d`，再删超出 500 条的最旧行。

## 前端

### 页签接线（4 处，缺一即静默失效）

1. `web/src/composables/dockTabs.ts` — `TabId` union 加 `'notifications'`，`DOCK_TABS` 加 `{ id: 'notifications', titleKey: 'nav.notifications' }`（渲染列表与切换白名单都从它派生）。
2. `web/src/composables/dockTabMeta.ts` — 加 `Bell` 图标。
3. `web/src/App.vue` — 加 `<TabPanel tabId="notifications" :activeTab="leftPanelActive" :noHeader="true">`。（`dockTabBadgeCount` **不加** case——本页签无角标。）
4. i18n — `nav.notifications` 与 `notifications.*` 加进 `en.ts` 与 `zh.ts`（**必须同键集**，过 `navKeys.test.ts` / `literalKeys.test.ts`）。

### 组件

- `web/src/composables/useNotifications.ts` — 列表 / 筛选（无需已读相关状态）。
- `web/src/components/notifications/NotificationsPanelContent.vue` — 面板根，接 `:active` 门控加载。

### 视觉（已用真实 token 出样张，见 `.clawbench/generated/img_notification_center_*.png`）

- **header 复刻 `TaskListPage.vue` 的 `.list-header`**：36px 高、面包屑式标题（`Bell` 图标 + 「通知」）+ 右侧 28×28 圆形 `header-btn`（仅刷新）。
- **筛选条**：一行三个 `MenuSelect` 下拉（项目 / 类型 / 时间），复用 `MenuSelect.vue` + `PopupMenu.vue`，右侧显示当前结果条数。
- **卡片列表复刻 `.task-item`**：`--bg-secondary` 底 + 边框 + `--space-4` 间距；**所有卡片外观统一**（无未读轨、无未读点、无已读淡化）。
- **信息层次**：标题行（类别图标 + 标题 + 时间）→ 摘要（最多 2 行）→ 元信息行（事件类型 chip + 项目 + 「源已删除」标记）。
- **图标全部用 lucide**，零 emoji：类别用 `Bot`/`Clock`/`GitMerge`，事件 chip 用 `Check`/`X`/`AlertTriangle`/`GitMerge`，标记用 `Folder`/`Unlink`。
- 事件 chip 语义色从状态 token `color-mix` 出来（success/danger/warning/info/purple）。

### 交互

- **点击有源的通知** → 复用现有三个 CustomEvent：`clawbench-open-session` / `clawbench-open-task` / `clawbench-open-forge`（跨项目时各自已实现热切换）。**不标记任何已读状态**（无此概念）。
- **点击 `sourceMissing` 的通知** → **不发跳转**，就地弹一句「源已删除，无法跳转」的提示。理由：会话的现有降级是**静默改道**（`useChatSession.ts:733-745` 收到 404 后清空 sessionId 并自动选中最新会话），照常跳转会让用户莫名落到别的会话上，比明确提示更困惑。

## 测试

**Go**：三条写入路径各自落库；`push_mode=disabled` 时仍写入；无订阅者时仍写入；`event_id` 去重；30 天 / 500 条双上限清理；筛选组合（项目 / 类型 / 时间段）；分页不重不漏（含同秒多条）；源探测（存在 / 硬删 / 归档仍算存在）；空 `project_id` 哨兵。

**前端**：筛选切换；空态；`sourceMissing` 标注与「不发跳转」；点击跳转派发正确的 CustomEvent。

**E2E**：新增 spec 覆盖跨组件流程（产生通知 → 页签出现 → 筛选 → 点击跳转）。

## 已识别的实现风险

1. **写入点分散在三处**（会话/任务/forge），任一处漏接即该类通知永不出现——需逐点测试钉住。
2. **`project_id` 归属**：会话/任务有项目，forge 事件的 `project_path` 是绑定项目（`ForgeRepoRef.ProjectPath`，非身份字段），需在写入时解析成 `project_id`；无法归属时落 `0` 哨兵。
3. **保留策略与「可回溯」的张力**：500 条上限在高频 forge 事件下可能很快淘汰历史；如实际使用中超限频繁触发，需重新评估上限。
4. **`sourceMissing` 探测的额外查询**：每页 2 次 `IN (...)` 查询，需确认在通知量大时不成为负担。
5. **新包/新文件的覆盖率门槛**：新增 Go 文件须自带测试（包级覆盖率不低于基线、变更行 ≥ 80%）。
6. **同秒多条的排序稳定性**：`created_at` 必须亚秒精度，分页游标用 `(created_at, id)` 复合条件，否则翻页会重复或漏行。

## 实现记录（2026-10-15）

已按本文档落地。落地时确认/新增的几点：

- **写入点实际位置**：会话走 `pushSessionTerminal`（`session_runtime.go`）——这是正常完成（`markDoneAndSendFinal` → `EmitSessionPushNotification`）与 drain 中途逐条（`EmitTurnAnsweredNotification`）**共同**的终态路径，故只在这里记一次即可；`emitSessionEvent` 里另记一处（覆盖 `cancelled` / `permission_pending`），并**以 `pushEnabled` 门控**，使 `EmitSessionEventWSOnly` 的「回声」路径不会重复记一行。任务在 `emitTaskEvent` 里记，且**刻意放在 `mgr == nil` 早返回之前**——日志不该依赖 WS manager 是否存在。
- **DB 未就绪时的守卫**：三个 `Record*` 适配器都先查 `store.DBReady()` 再碰任何 DB（`GetSessionProjectPath` / `ProjectIDForPath` 会访问句柄）。此前 `emitTaskEvent` 在无 DB 的测试里是 DB-free 的，不加守卫会 panic。
- **E2E 时序坑**：`chat.waitForReply` 只等**流式文本**出现（中途即返回），而通知行写在**终态完成**（实测约 19s 后）。只等文本就开面板会与写入竞态。可靠信号是停止按钮消失（`.chat-stop-btn` count 归零）。
- **`created_at` 亚秒精度**：已按风险 6 落地（`notificationTimestamp` 固定 3 位小数 UTC），并有测试钉住同秒多条的 keyset 分页不重不漏。

