# 会话列表跨项目活跃会话 Tab

日期：2026-09-11
状态：已实施（前端 tab + 后端 overview 字段；抽屉布局待真机手动验证）

## 1. 目标

在会话列表（`SessionList.vue`）底部新增分段 tab，切换「本项目 / 其他项目」两个视图：

- **本项目**：现有无限滚动会话列表，行为完全不变。
- **其他项目**：展示所有**其他项目**中处于 运行中 / 未读 / 待审批 状态的会话，按项目分组，点击即切换项目并打开该会话。

核心价值：用户不必逐个切换项目，就能发现并跳转到其他项目的活跃会话（正在跑的、有未读的、等你审批的）。

## 2. 关键前提（已探明的现有能力）

后端**已存在**端点 `GET /api/ai/sessions/overview`（`internal/handler/chat_session.go:19-78`），返回所有项目中 running / pendingApproval / unreadCount>0 的会话并按项目分组：

```json
{
  "projects": [
    { "name": "/abs/project/path",
      "sessions": [ { "id", "title", "running", "pendingApproval", "unreadCount", "updatedAt" } ] }
  ],
  "total": 5
}
```

该端点目前**仅被 Android 悬浮窗消费**（`web/src` 零引用），无需鉴权项目 cookie。本次前端直接复用，不改其分组/过滤语义，避免影响 Android。

前端亦已有成熟的跨项目跳转链路：`App.vue` 的 `hotSwitchProject(projectPath, sessionId)`（`App.vue:584-709`），Phase 7 会等待会话就绪后 `switchSession(pendingSessionId)`；`handleOpenSession`（`App.vue:827-848`）是现成的同/跨项目分支范例。

## 3. 已确认决策汇总

| 维度 | 决定 |
|---|---|
| 呈现形式 | 会话列表**底部固定 tab 栏**：「本项目 / 其他项目」互斥切换视图 |
| 当前项目 | 「其他项目」tab **排除** `store.state.projectRoot`（前端过滤） |
| 行渲染 | 视觉与主列表行**完全一致**，但用**独立类名** `.cross-session-item`（避免污染键盘导航索引） |
| 视觉区分 | **分组头显示项目名**（basename 为主 + 路径缩写为副）；行本身沿用主列表样式 |
| 分组排序 | 组按项目路径**字母序**（大小写不敏感）；组内按 `updatedAt` **降序** |
| 数据刷新 | 复用现有两条通道：`store.state.sessionListVersion` watch + WS `session_update` debounce 400ms；**watch 注册在模块顶层**（不可放组件作用域） |
| 数据归属 | 抽 **composable 单例** `useCrossProjectSessions`，两处 `SessionList` 实例共享、只发一次请求 |
| 切项目后 | composable 内 `watch(store.state.projectRoot)`，变化即重拉（保证排除项正确） |
| 点击行为 | **直接切换，无确认**；切项目后自动切回「本项目」tab，并高亮 + 滚动到该会话 |
| tab 状态 | `SessionList` 组件**本地 ref**，不持久化，默认「本项目」；切项目时显式重置 |
| tab 栏显示 | **始终显示**；有其他项目活跃会话时，「其他项目」tab 显示**会话总数**角标 |
| tab 栏位置 | 侧栏在列表容器底部；**抽屉需放 `BottomSheet` 的 `#footer` slot**（见 Step 3b，必须手动验证） |
| 行内操作 | **无归档按钮**；**不进键盘导航**，仅鼠标/触摸点击 |
| 空状态 | 「其他项目」tab 空时显示空状态文案（tab 栏仍显示） |
| v-for key | `projectPath + '/' + id`（防御性写法；id 实为全局唯一，见第 6 节） |
| 测试 | 前端 composable + 组件单测；后端 handler 测试断言新增字段；抽屉布局手动验证 |

## 4. 实施步骤

### Step 1 — 后端：overview 响应补齐行渲染所需字段

`internal/handler/chat_session.go` 的 `overviewSession`（第 40-47 行）增加 3 个字段并映射：

- `Backend string json:"backend"`
- `AgentID string json:"agentId"`
- `Model string json:"model"`

SQL（`internal/service/chat.go:1116`）已查出 `s.backend, s.agent_id, s.model` 并填入 `model.ChatSession`，仅需在 handler 映射。**不改端点分组/过滤语义**，Android 不受影响。

排序**只在前端做**（见 Step 2），后端不动排序逻辑，减少改动面。

### Step 2 — 前端：新增 composable `web/src/composables/useCrossProjectSessions.ts`

模块级单例，导出：

```ts
export interface CrossProjectSession {
  id: string; title: string; backend?: string; agentId?: string; model?: string
  running: boolean; pendingApproval: boolean; unreadCount: number; updatedAt: string
}
export interface CrossProjectGroup {
  name: string            // 绝对路径
  displayName: string     // basename
  displayPath: string     // 缩写路径
  sessions: CrossProjectSession[]
}
```

职责：

1. `groups = ref<CrossProjectGroup[]>([])`、`loading = ref(false)`、`total = computed(...)`、`loaded = ref(false)`。
2. `refresh()`：`fetch('/api/ai/sessions/overview')` → 过滤 `name === store.state.projectRoot` 的组 → 每组按 `updatedAt` 降序 → 组按 `name` 升序（**比较时 `toLowerCase()`**，避免 Windows 盘符大小写导致顺序诡异）→ 计算 `displayName = baseName(name)`、`displayPath`（用 `store.state.homeDir` 前缀替换为 `~`，过长用 `middleEllipsis` 截断）。
3. **副作用注册必须脱离组件作用域（S1，关键）**：
   - ❌ 不要"由首个消费组件触发初始化"——在组件 `setup()` 调用路径里同步创建的 `watch` 会被绑定到该组件的 effect scope，切项目时 `projectKey` 重建组件树会销毁 watcher，而初始化守卫又阻止重新注册 → **跨项目列表永久不再更新**。
   - ✅ 正确做法：在**模块顶层**直接注册（`watch` 在模块作用域执行，不属于任何组件 scope）。参照现有先例 `web/src/composables/useTocDockPreference.ts:46`（模块顶层 `watch(editing, ...)`）。
   - 注册内容：
     - `watch(() => store.state.sessionListVersion, refresh)`；
     - `watch(() => store.state.projectRoot, refresh)`；
     - WS 订阅：`useGlobalEvents().onEvent`，`session_update` → debounce 400ms → `refresh()`。模块顶层注册的 handler **不需要也不能**在组件卸载时移除（单例生命周期 = 应用生命周期）。
4. 错误处理：失败时保留上一次 `groups`（不闪空），`appLog.e('CrossProjectSessions', ...)`。
5. **测试隔离**：导出 `resetCrossProjectSessionsForTest()`（清空 `groups/loaded/loading` 并重置 debounce 计时器），供测试用例 `beforeEach` 调用，避免模块单例状态跨用例泄漏。

路径工具复用：`@/utils/path.ts` 的 `baseName`；缩写复用 `@/utils/completionMatch.ts` 的 `middleEllipsis`（按 `homeDir` 替换 `~` 的逻辑内聚在 composable，注意 Windows 分隔符）。

**M5（已知可接受）**：`SessionList` 已订阅 `session_update → scheduleReload()`，composable 再订阅一次，同一事件会触发两个请求（本项目列表 + overview）。可接受；若后续要优化，可让 `SessionList` 的 handler 同时调用 composable 的 `refresh`，合并为一次订阅。

### Step 3 — 前端：`SessionList.vue` 增加底部 tab 与跨项目视图

模板结构调整（关键：当前根节点本身就是滚动容器 `overflow-y:auto`）：

```
.session-list (flex column)
├── .session-list-scroll (overflow-y:auto, flex:1)   ← 现有内容整体移入
│     ├── LoadingIndicator / empty
│     └── TransitionGroup(.session-rows) + sentinel + loadingMore
├── .session-list-pane--cross (overflow-y:auto, flex:1, v-show=activeTab==='cross')
│     ├── 空状态文案
│     └── 分组：分组头(displayName + displayPath) + 会话行
└── .session-tabs (flex-shrink:0)                     ← 底部固定 tab 栏
      ├── 本项目
      └── 其他项目 (+ 数量角标)
```

- `activeTab = ref('project' | 'cross')`，组件本地。
- 「本项目」用 `v-show` 而非 `v-if`，保留滚动位置与已加载分页（避免切 tab 丢失无限滚动深度）。
- **S2：跨项目行必须用独立类名（如 `.cross-session-item`）**，不要复用 `.session-item`。原因：`scrollActiveIntoView`（`SessionList.vue:270-274`）用 `listRef.querySelectorAll('.session-item')` + `listNav.activeIndex` 定位，而 `useListNav.getCount` 只数本项目行。虽然两个面板 `v-show` 互斥且本项目行在 DOM 顺序靠前，索引当前仍能对齐，但隐藏面板的行仍留在 DOM 中，是脆弱耦合——一旦将来调整 DOM 顺序或渲染方式就会滚错行。用独立类名隔离，零成本消除隐患。
- 跨项目行结构与样式**视觉上**与主列表一致（可共用同一套样式规则，通过类名组合复用），分组头为新增元素，承载项目名与路径缩写（视觉区分的唯一手段）。
- 行点击：`emit('select', session.id, session.backend, group.name)`（**第三个参数 projectPath**，仅跨项目行传）。
- 行**不带归档按钮**；`v-for :key="group.name + '/' + session.id"`；不接入 `useListNav`。
- **M1 角标语义**：角标 = **跨项目活跃会话总数**（`total`，即所有分组 `sessions` 之和），非项目组数。无数据时不显示角标（或显示 0）。
- **M3 observer 与隐藏面板**：切到 `cross` tab 时，本项目滚动区 `display:none`，`IntersectionObserver` 的 root 尺寸为 0，sentinel 可能触发异常 `loadMore`。处理：`watch(activeTab)` → 切到 `cross` 时 `observer.disconnect()`；切回 `project` 时 `nextTick` 后 `setupObserver()`。
- 切项目后回「本项目」tab：在 `SessionList` 内 `watch(store.state.projectRoot)` 重置 `activeTab='project'`（内聚；切项目会重建组件天然重置，但侧栏实例未必重建，故需显式 watch）。

### Step 3b — 抽屉场景 tab 栏位置（S3，必须手动验证）

`SessionDrawer` 通过 `<BottomSheet :auto>` 包裹 `SessionList`。`BottomSheet` 的 auto 模式 CSS 为 `.bs-panel.bs-auto { top:auto; height:auto; max-height:100% }` + `.bs-auto .bs-body { overflow-y:auto }`（`BottomSheet.vue:220-232`），即**面板高度由内容决定**。

风险：若 `SessionList` 根节点改为 `flex column` 且内部滚动区用 `flex:1`，在 auto 高度下内部滚动区拿不到有界高度，结果整个面板无限增高（或内部滚动区塌陷），**底部 tab 栏会被推出可视区**，而非钉在面板底部。

- 侧栏（`SessionSidebar`，固定 `height:100%`）不受此影响。
- 抽屉需要二选一（实施时验证后定）：
  - **方案 A（推荐）**：把 tab 栏移到 `BottomSheet` 的 `#footer` slot（`.bs-footer` 是 `flex-shrink:0`，天然钉在面板底部），`SessionList` 内部只渲染当前 tab 的内容区。需要 `SessionList` 暴露 tab 状态或把 tab 栏拆成独立子组件由 `SessionDrawer` 渲染。
  - **方案 B**：抽屉里给 `SessionList` 一个显式高度约束（如 `min-height:0` + 面板 `max-height` 生效链），让内部 `flex:1` 滚动区有界。
- **此项必须真机/窄屏手动验证**，单测覆盖不到（jsdom 无布局）。

### Step 4 — 前端：事件透传与 App.vue 分支

- `SessionDrawer.vue:99-102`、`SessionSidebar.vue:101-103` 的 `handleSelect(sessionId, backend, projectPath)` 增加第三参数透传。
- `App.vue` `handleSessionSelect(sessionId, backend, projectPath)`（`App.vue:1244-1253`）改为：
  - 同项目（无 projectPath 或等于 `store.state.projectRoot`）→ 现有逻辑（含"已是当前会话则 no-op"守卫）。
  - 跨项目 → `hotSwitchProject(projectPath, sessionId)`；无需确认。
  - 注：`sessionId === currentSessionId` 的 no-op 守卫**无需**加项目条件——`chat_sessions.id` 是全局 `TEXT PRIMARY KEY`（`database.go:288`），且 `generateUUID` 全局查重（`uuid.go:36-47`），线上库实测 `COUNT(*)=COUNT(DISTINCT id)`，不存在跨项目同 id（详见第 6 节"已排查的非问题"）。
- **M2 切换失败兜底**：overview 可能返回已删除、或不在 `RootPaths` 下的项目。`hotSwitchProject` 只对 `NotADirectory` 有友好提示（`App.vue:604-611`），路径不在 roots 时后端返回 `AccessDenied`（403），前端只弹通用 `switchProjectFailed`。需确认该通用提示可接受；如不可接受，为 `AccessDenied` 增加专门文案（如"该项目路径不在允许范围内"）。
- `hotSwitchProject` Phase 7 已实现"等待会话就绪 → `switchTab('chat')` → `switchSession(sessionId)`"，无需改动。
- 「切换后高亮并滚动到该会话」：`hotSwitchProject` 完成后该会话进入本项目列表；现有 `SessionList` 的 `watch(sessionsWithStatus, () => listNav.reset())` 与 `scrollActiveIntoView` 覆盖键盘导航场景。跨项目跳转后需确保列表加载完成后把 active 行滚入视口——在 `SessionList` 内新增：当 `currentSessionId` 变化且能在 `sessions` 中找到对应行时，`nextTick` 后 `scrollIntoView({ block: 'nearest' })`。若目标行尚未加载（分页未覆盖），退化为等待 `loadSessions` 完成后重试一次。

### Step 5 — i18n

`web/src/i18n/locales/{zh,en}.ts` 的 `session` 命名空间新增：

- `tabProject` / `tabCross`（如「本项目」/「其他项目」）
- `crossEmpty`（如「暂无其他项目的活跃会话」）
- 复用现有 `session.running` 等，无需新增状态文案。

## 5. 测试

### 前端

1. `web/src/composables/__tests__/useCrossProjectSessions.test.ts`
   - `beforeEach` 调用 `resetCrossProjectSessionsForTest()`（**M4**：模块单例状态会跨用例泄漏）。
   - 排除当前项目组；保留其他组。
   - 组按路径字母序（含大小写不敏感断言）、组内按 `updatedAt` 降序。
   - `displayName` = basename；`displayPath` 的 `~` 缩写与截断。
   - 请求失败时保留旧 `groups`（不置空）。
   - `sessionListVersion` / `projectRoot` 变化触发 `refresh`。
   - **watcher 存活断言（S1 回归）**：模拟组件挂载→卸载后，再次变更 `projectRoot`，`refresh` 仍被触发（防止将来有人把 watch 挪回组件作用域）。
2. `web/src/components/session/__tests__/SessionList.test.ts`（既有文件追加）
   - **M4：更新 `mockStore`**（`SessionList.test.ts:7`）补 `projectRoot: ''`、`homeDir: ''`——否则过滤条件 `name === undefined` 永不命中，测试无法覆盖"排除当前项目"。
   - 默认显示「本项目」；点击「其他项目」tab 切换到跨项目视图。
   - 跨项目视图渲染分组头（项目名）与行。
   - 点击跨项目行 emit `select` 且**携带 projectPath**。
   - 空状态文案渲染。
   - 跨项目行**不渲染归档按钮**。
   - 跨项目行使用独立类名（S2 回归：`querySelectorAll('.session-item')` 不包含跨项目行）。
   - `v-for` key 唯一性。

### 后端

`internal/handler/session_overview_test.go` 追加断言：overview 响应中会话包含 `backend` / `agentId` / `model` 字段且值与建库时一致。

### 必须手动验证（单测覆盖不到）

- **S3**：抽屉（`BottomSheet :auto`）中 tab 栏是否钉在面板底部、内部滚动区是否有界、列表不再撑爆面板。
- **S3**：侧栏（固定高度）回归正常。
- 窄屏/移动端真机布局。

## 6. 风险与注意事项

1. **Android 回归**：overview 端点新增字段是纯增量，Android 用 Gson/手写解析忽略未知字段，安全。但**不要改动**分组/过滤逻辑。
2. **滚动容器重构**：`SessionList.vue` 根节点从滚动容器变为 flex 布局，需回归现有无限滚动、`IntersectionObserver` 的 `root`（`listRef`）指向新的滚动区，否则 sentinel 永不触发或全局触发。
3. **两实例一致性**：侧栏与抽屉共享 composable 单例，`refresh` 只发一次请求；但 tab 状态各自独立（符合预期）。
4. **切项目时序**：`hotSwitchProject` 会 `resetChatSessionState()` 并重建组件树；composable 是模块级，状态跨项目存活，故必须 `watch(projectRoot)` 重拉，否则排除项滞后。
5. **路径比较**：排除当前项目用严格字符串相等（`name === store.state.projectRoot`）。两者同源（后端绝对路径），无需归一化；若后续发现 Windows 分隔符差异，再引入 `normalizeForCompare`。
6. **tab 高度跳动**：tab 栏始终显示，避免出现/消失导致列表高度变化。
7. **S1 watcher 作用域**：composable 的 `watch` 必须在模块顶层注册，绝不能在组件 setup 调用路径里注册（否则切项目时被销毁且守卫阻止重注册，功能静默永久失效）。有专门回归测试。
8. **S3 抽屉布局**：`BottomSheet :auto` 下 tab 栏必须放 `.bs-footer` 或给内部滚动区显式高度约束，否则 tab 栏被推出可视区。必须手动验证。
9. **S2 类名隔离**：跨项目行用 `.cross-session-item`，避免污染 `querySelectorAll('.session-item')` 的键盘导航索引映射。

### 已排查的非问题（勿重复怀疑）

- **会话 id 跨项目唯一性**：`chat_sessions.id` 是全局 `TEXT PRIMARY KEY`（`database.go:288`），`generateUUID` 全局查重（`uuid.go:36-47`），线上库实测 `COUNT(*)=COUNT(DISTINCT id)=2782`。因此：
  - 前端 `sessionId === currentSessionId` no-op 守卫**不需要**加项目条件；
  - 后端 `runningSet[s.ID]` / `pendingApprovalSet[s.ID]` 按 id 匹配**不会**跨项目串扰，**无需修改**。
  - overview 的 unread 子查询按 `id+project_path` 双列 join，是为防 `chat_history.project_path` 与 session 不一致的脏行，与 id 唯一性无关。
- **`v-for` key 用 `projectPath + '/' + id`**：因 id 已全局唯一，此写法并非必需，但作为防御性写法保留（且天然携带分组归属），无害。

## 7. 不做的事

- 不改主列表的无限滚动与分页逻辑。
- 不给跨项目行加归档按钮、不进键盘导航。
- 不持久化 tab 选择。
- 不改 overview 端点的分组/过滤语义。
- 不引入后端 `exclude_project` 参数（前端过滤）。
- 不改后端 running/pending 匹配逻辑（已证实 id 全局唯一，无串扰）。
