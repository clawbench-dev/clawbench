# 状态同步触发源：前台 → WS 重连

## 背景与问题

当前 APP 在"从后台切到前台"（`visibilitychange → visible`）时做全量数据同步，由两条路径同时触发：

1. `useGlobalEvents.handleVisibilityChange` 派发 `clawbench-foreground` → `App.vue.handleForeground` 全量拉取（sessions/files/git/tasks/terminal/currentFile）。
2. `useChatSession.handleVisibilityChange`（直接挂在 visibilitychange 上）→ `onDisconnectStream()` + `loadHistory`。

**脆弱点：**
- 前台加载与 WS 重连加载重叠且互相竞争（`loadSessionsOnce` 两条路径都调，历史重载只在 visibility 路径）。
- 浏览器模式下 WS 后台保持存活、事件实时推送，状态本就最新，前台全量重载是**冗余**的。
- 真正"状态可能过期"的根因是 **WS 断开**，而非"前台"这个代理信号。

## 目标

**WS 重连（断开→恢复）是唯一的状态同步触发点。前台不再做任何数据同步。** 前台只负责"确保 WS 连接"。

## 设计

### 1. `App.vue`

- **删除** `handleForeground`（当前 837-851 行的全量拉取），并移除对 `clawbench-foreground` 的监听（当前 911、2222 行）。
- **保留** `clawbench-foreground` 事件的派发（`useGlobalEvents.ts:480`）——`useConnectionOverlay` 仍靠它在前台重置 5s 重连宽限期，属于合法用途。
- **升级** `handleReconnect`（当前 856 行）为全量：在现有 `loadProject / loadSessionsOnce / loadTasks / loadGitBranch` 基础上补充 `store.loadFiles(...)`、`loadTerminalStatus()`、`refreshCurrentFile()`。

### 2. `useChatSession.ts`

- **删除** `handleVisibilityChange`（当前 980-1000 行的前台历史重载）。
- **改造** `handleWsReconnect`（当前 1008 行）为"重连即同步当前会话"，规则：
  - `!currentSessionId` → 直接返回（无会话可同步）。
  - `loading && 仍运行` → 不重载历史（WS 重连后 stream 自动 re-subscribe，见 `useChatStream.ts:740`），交给实时流。
  - `loading && 不再运行` → 清理卡死 loading 状态 + `loadHistory(..., forceNotRunning=true)`（保留现有逻辑）。
  - `!loading` → `loadHistory(false,false,true)`（skipIfUnchanged）反映断开期间的变化。

### 3. `ChatPanelContent.vue`

- 移除 `document.addEventListener('visibilitychange', session.handleVisibilityChange)` 及其移除（当前 1094、1111 行）。

## 数据流

- **app 模式**：后台 `disconnect()` → 前台 `connect()` → onopen `isReconnect=true` → 派发 `clawbench-reconnect` → `handleReconnect` 全量同步 + `handleWsReconnect` 同步当前会话历史。覆盖原前台场景。
- **浏览器模式**：后台 WS 存活、事件实时推送 → 前台状态本为最新 → 无同步、无冗余重载。
- **冷启动**：首次连接 `hasConnectedOnce=false`，不派发 reconnect，由 onMounted 正常加载，无重复请求。

## 边界（不扩大范围）

浏览器模式下若 WS 存活但某个流已静默超时（`STREAM_TIMEOUT_MS=30s` 无消息）导致 loading 卡死，前台不再重载。这属于 stream 自身 watchdog 的既有问题，不在本次范围。

## 测试

- `useChatSession` 的 `handleWsReconnect` 覆盖四个分支（无会话 / loading 仍运行 / loading 不再运行 / 非 loading）的单元测试。
- 运行 `npm test` 相关用例与 `./scripts/pre-push-checks.sh`。
