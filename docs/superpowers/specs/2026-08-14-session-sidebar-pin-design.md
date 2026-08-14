# Design: 会话列表图钉固定侧栏（Session Sidebar Pin）

## 目标

在 **PC 宽屏**下，让会话列表支持两种形态，通过「图钉（Pin）」按钮切换：

- **抽屉模式**（现状）：会话列表作为 BottomSheet 从底部弹出。
- **侧栏模式**（新）：会话列表常驻于聊天面板右侧的独立列，可拖拽调整宽度。

图钉按钮同时出现在**抽屉标题栏**和**侧栏标题栏**，点击即在两种模式间即时切换。

## 背景与现状

- 会话列表抽屉：`web/src/components/session/SessionDrawer.vue`，用 `BottomSheet` 承载。标题栏（header slot）含标题、会话计数条、搜索按钮、新建按钮（`SessionDrawer.vue:12-17`）；正文为 `session-list` 会话行列表（选中/归档/未读徽标/运行态/无限滚动加载更多），键盘 ↑/↓+Enter 导航（`useListNav`/`useListKeys`）。
- 触发入口：`ChatInputBar.vue:9-13` 顶部的「会话」`List` 图标按钮 → `@click="$emit('open-session-tab', 'sessions')"`；`ChatPanelContent.vue:99` 转发给 `identity.openSessionTab()`；`useSessionIdentity.ts:695` 调 `sessionDrawer.open()`。快捷键 Ctrl+K 同样走 `openSessionTab`（`ChatPanelContent.vue:1024-1033`）。
- 聊天面板位于宽屏 `SplitView` 的右侧 `col-right`（`App.vue:220`），内部是 `TabPanel tabId="chat"`（含 header slot，显示 agent 标题）包住 `ChatPanelContent`。
- 宽屏判定：`useWideScreenLayout().isWideScreen`。持久化用 `localStorage`（参考 `WIDE_SCREEN_SPLIT_RATIO_KEY` 等键模式，`useWideScreenLayout.ts`）。

## 范围

- 仅 PC 宽屏（`isWideScreen === true`）渲染侧栏；窄屏/手机行为不变（仍只有抽屉）。
- 复用会话列表的展示与操作（选中/新建/归档/搜索/加载更多/未读徽标/运行态/键盘导航）。
- 拖拽调宽、宽度与开关状态持久化。
- 侧栏打开时禁用抽屉入口。

## 架构

### 1. 抽取可复用组件 `SessionList.vue`

从 `SessionDrawer.vue` 抽离会话行列表主体为 `web/src/components/session/SessionList.vue`，供抽屉与侧栏共用。

Props：
- `sessions`: `SessionRow[]`（已含 running 状态）
- `currentSessionId: String`
- `loading`, `loadingMore`, `hasMore: Boolean`
- `getAgentBackend`, `getAgentName`: `Function`（由父级注入 `useAgents` 结果）

Emits：
- `select(sessionId, backend)`
- `archive(sessionId)`

内置：行渲染（标题/时间/agent/模型/未读徽标/运行态/运行扫描线）、无限滚动（IntersectionObserver + 分页）、键盘 ↑/↓+Enter 导航（`useListNav`/`useListKeys`，内部管理）。样式从 `SessionDrawer.vue` 迁移至本组件 scoped。

### 2. 抽取可复用头部 `SessionListHeader.vue`

抽出标题栏主体为 `web/src/components/session/SessionListHeader.vue`，抽屉与侧栏共用：

Props：`sessionCount`, `sessionMaxCount`（会话计数条）。
Slots：`#default` 右侧动作区（放搜索/新建/图钉按钮，位置由父级决定）。
Emits：`open-search`, `create`。

### 3. `SessionDrawer.vue` 改造

- 正文替换为 `<SessionList>`。
- header slot 中，在搜索/新建按钮**前**新增图钉按钮（仅 `isWideScreen` 渲染）：
  `<button class="header-action-btn" @click.stop="$emit('pin')"><Pin/></button>`。
- 新增 emit `pin`。
- 保留现有数据加载逻辑（loadSessions/loadMore/计数条/addSessionLocally）。

### 4. 新组件 `SessionSidebar.vue`

`web/src/components/session/SessionSidebar.vue`，会话列表的侧栏形态：

- 结构：标题栏（图钉 + 标题「会话」+ 搜索 + 新建，复用 `SessionListHeader`）+ `<SessionList>` 正文。
- 图钉按钮（`Pin` 图标）→ `emit('unpin')`：切回抽屉模式。
- **拖拽调宽**：左侧 1px 分割线 + 拖拽手柄（hover/active 加宽，负 margin 布局不位移，复用 `SplitView.vue` 的拖拽模式）。宽度 clamp 在 `[220, 480]px`。拖动结束 emit `resize(width)`。
- 点击图钉、关闭按钮（X）→ `emit('close')`。
- 本组件仅由 App.vue 在 `isWideScreen` 时渲染。

### 5. 状态管理 `useSessionSidebar.ts`

`web/src/composables/useSessionSidebar.ts`，单例 composable：

```ts
const open = ref(false)        // 侧栏开关
const width = ref(280)         // 当前宽度 px
const mode = ref<'drawer' | 'sidebar'>('sidebar')  // 当前展示形态
const ready = ref(false)       // 是否已从 localStorage 恢复（防闪）
```

- `init()`：读 `localStorage['clawbench-session-sidebar'] = { open, width }`。**PC 宽屏默认 `open=true`**（无记录时）；窄屏强制 `open=false`。恢复 `width`（clamp 后）。
- `openSidebar()` / `closeSidebar()` / `toggleSidebar()`。
- `unpinToDrawer()`：`open=false`，`mode='drawer'`，`openSessionTab()`（打开抽屉）。
- `pinToSidebar()`：关闭抽屉（`sessionDrawer.close()`），`open=true`，`mode='sidebar'`。
- `setWidth(w)`：clamp 后写 `width` 与 localStorage。
- 任一状态变化持久化 `{ open, width }` 到 localStorage。
- `openSessionTab` 桥接：当侧栏打开时，`openSessionTab` 改为 `toggleSidebar()`（收起侧栏）而非打开抽屉；否则打开抽屉。

### 6. 接入 `App.vue`

- `useSessionSidebar()` 初始化。
- 在 `<template #right>` 的 `col-right` 内、`TabPanel` 之后增加侧栏（`v-show="sidebar.open && isWideScreen"`）：
  ```
  <div v-show="sidebar.open.value && isWideScreen" class="session-sidebar-slot">
    <SessionSidebar :width="sidebar.width.value" @resize="sidebar.setWidth" @unpin="sidebar.unpinToDrawer" @close="sidebar.closeSidebar" ... />
  </div>
  ```
  `col-right` 需 `display:flex`，TabPanel 占剩余宽度，侧栏固定宽。
- `SessionDrawer` 绑定新增 `@pin="sidebar.pinToSidebar"`。
- 宽屏切换到非聊天 tab 时侧栏仍常驻（它属于 `col-right`，随聊天面板显示/隐藏由现有 `v-show` 控制）。当宽屏聊天面板隐藏（窄屏）时侧栏自然隐藏。
- 提供 `sessionSidebar` 给需要隐藏入口的组件（`ChatInputBar`）。

### 7. 隐藏抽屉入口

- `ChatInputBar.vue` 顶部「会话」`List` 按钮：新增 prop `sessionPanelOpen: Boolean`，为 true 时 `v-show="!sessionPanelOpen"` 隐藏该按钮（侧栏打开时隐藏）。
- `openSessionTab`（含 Ctrl+K）：由 `useSessionSidebar` 桥接（见上），侧栏打开时收起侧栏而非弹抽屉。
- `App.vue` 向 `ChatPanelContent` 传递 `:session-sidebar-open="sidebar.open.value"`，`ChatPanelContent` 转发给 `ChatInputBar`。

## 数据流

```
[抽屉图钉] --pin--> pinToSidebar(): 关抽屉 + 开侧栏 + mode=sidebar
[侧栏图钉] --unpin--> unpinToDrawer(): 关侧栏 + mode=drawer + openSessionTab()开抽屉
[拖拽]     --resize--> setWidth(): clamp + 持久化
[状态变更] ----持久化----> localStorage['clawbench-session-sidebar']
[ChatInputBar 会话按钮] --sessionPanelOpen--> v-show 隐藏
[Ctrl+K]  --openSessionTab--> 侧栏开? toggleSidebar : sessionDrawer.open()
```

## 错误处理与边界

- localStorage 解析失败/越界 → 用默认值（`open=宽屏默认 true`，`width=280`）。
- 拖拽宽度 clamp 至 `[220,480]`，防止极端值。
- 非宽屏：`open` 强制 false，图钉按钮不渲染，侧栏不渲染。
- 切项目：`resetWideScreenState` 不涉及侧栏；侧栏状态按项目无关全局持久化（同一开关跨项目保留）。初始化在 App 启动时执行一次。
- 宽屏→窄屏切换：侧栏自动隐藏；切回宽屏恢复此前 open/width。

## 测试

`web/src/composables/__tests__/useSessionSidebar.test.ts`：
- 默认 PC 宽屏 `open=true`、`width=280`；无 localStorage 时。
- `setWidth` clamp 到 `[220,480]` 且持久化。
- `pinToSidebar` 关抽屉+开侧栏；`unpinToDrawer` 关侧栏+开抽屉。
- 侧栏打开时 `openSessionTab` → toggleSidebar；关闭时 → 打开抽屉。
- localStorage 损坏回退默认。

`web/src/components/session/__tests__/SessionList.test.ts`：
- 渲染会话行（标题/时间/agent/模型）；未读徽标/运行态。
- 点击行 emit `select`；点归档 emit `archive`。
- 键盘 ↑/↓+Enter 选中。

`web/src/components/session/__tests__/SessionSidebar.test.ts`：
- 标题栏渲染（图钉/搜索/新建）。
- 点图钉 emit `unpin`；点关闭 emit `close`。
- 拖拽触发 `resize`（或暴露宽度 clamp 逻辑单测）。

`web/src/components/session/__tests__/SessionDrawer.test.ts` 更新：
- 宽屏下 header 含图钉按钮；点图钉 emit `pin`。
- 窄屏下图钉不渲染。

`ChatInputBar` 测试更新：`sessionPanelOpen=true` 时「会话」按钮隐藏。

## 验收

- PC 宽屏：聊天面板右侧出现常驻会话侧栏（默认打开），可拖拽调宽并记忆。
- 抽屉标题栏图钉 → 立即关闭抽屉、打开侧栏，当前会话不变。
- 侧栏标题栏图钉 → 关闭侧栏、打开抽屉。
- 侧栏打开时，聊天面板顶部「会话」按钮隐藏；Ctrl+K 收起侧栏而非弹抽屉。
- 窄屏/手机行为与现状完全一致（无侧栏、无图钉）。
- 宽度与开关状态跨刷新/切项目保留。
