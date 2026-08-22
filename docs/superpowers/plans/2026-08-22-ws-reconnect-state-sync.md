# WS 重连作为唯一状态同步触发点 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 WS 重连（断开→恢复）成为唯一的数据状态同步触发点，前台不再做任何数据同步。

**Architecture:** 保留 `clawbench-foreground` 事件（`useConnectionOverlay` 依赖它重置 5s 宽限期），但删除 `App.vue` 对它的全量拉取；把全量拉取合并进 `handleReconnect`（WS 重连）；把聊天历史重载从 `handleVisibilityChange`（visibility 驱动）迁移到 `handleWsReconnect`（重连驱动）。

**Tech Stack:** Vue 3, TypeScript, Vitest

**Spec:** `docs/superpowers/specs/2026-08-22-ws-reconnect-state-sync-design.md`

---

### Task 1: 改造 `useChatSession.ts` 的 `handleWsReconnect` 逻辑

**Files:**
- Modify: `web/src/composables/useChatSession.ts:1008-1027`
- Test: `web/src/composables/__tests__/useChatSession.test.ts:3664-3690`

行为变化：`loading=false` 时不再"什么都不做"，而是重载历史（skipIfUnchanged），反映断开期间的变化。`loading=true && 仍运行` 保持不动作（交给 stream 自动 re-subscribe）。无会话、`loading=true && 不再运行` 分支不变。

- [ ] **Step 1: 修改测试 "when loading=false: does nothing" → "when loading=false: reloads history"**

将 `web/src/composables/__tests__/useChatSession.test.ts:3664-3690` 整段替换为：

```ts
  it('when loading=false: reloads history (skipIfUnchanged)', async () => {
    const loading = ref(false)
    const onDisconnectStream = vi.fn()
    const onRenderUpdate = vi.fn()
    const options = {
      currentSessionId: ref('s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate,
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    // First call: loadSessionsOnce. Second call: loadHistory.
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [{ id: 's1', running: false }], totalCount: 1 }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessionId: 's1', messages: [], total: 0, running: false }),
      })

    await session.handleWsReconnect()

    expect(onDisconnectStream).not.toHaveBeenCalled()
    await vi.waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/ai/chat?session_id=s1'),
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })
    await vi.waitFor(() => {
      const forceFullCalls = onRenderUpdate.mock.calls.filter((c: any[]) => c[0] === true)
      expect(forceFullCalls.length).toBeGreaterThanOrEqual(1)
    })

    vi.restoreAllMocks()
  })
```

- [ ] **Step 2: 运行测试确认失败（行为未实现）**

Run: `npx vitest run web/src/composables/__tests__/useChatSession.test.ts -t "handleWsReconnect"`
Expected: FAIL — `globalThis.fetch` 未以 `session_id=s1` 调用（当前 `loading=false` 分支直接 return，不调 loadHistory）。

- [ ] **Step 3: 实现 `handleWsReconnect` 新逻辑**

将 `web/src/composables/useChatSession.ts:1008-1027` 的 `handleWsReconnect` 函数体整体替换为：

```ts
  async function handleWsReconnect() {
    if (!currentSessionId.value) return
    // Refresh runningSessions from the backend so the current-session decision
    // below reflects any change that happened while disconnected.
    await loadSessionsOnceInner()
    if (loading.value) {
      if (runningSessions.value.has(currentSessionId.value)) {
        // Session still running — the live stream re-subscribes on reconnect
        // (useChatStream watch on `connected`). Nothing to sync here.
        return
      }
      // AI finished during the disconnection — clean up the stuck loading state
      // and reload history. forceNotRunning=true prevents loadHistory from
      // re-connecting the stream if the server's in-memory running state
      // hasn't been updated yet.
      appLog.w(TAG, `WS reconnect: session ${currentSessionId.value} no longer running — cleaning up stuck loading state`)
      onDisconnectStream()
      forceCleanupStreamingState(messages.value as ChatMessage[], { onRenderNeeded: (f) => onRenderUpdate(f ?? true), onExtractScheduledTasks })
      loading.value = false
      loadHistory(false, false, true, true).then(() => {
        onRenderUpdate(true)
      }).catch(() => {
        loading.value = false
      })
    } else {
      // Session idle — reload history to reflect changes that occurred while
      // disconnected (AI finished a task, mode changed, etc.). skipIfUnchanged
      // avoids UI churn when nothing changed.
      loadHistory(false, false, true).then(() => {
        onRenderUpdate(true)
      }).catch(() => {
        // Non-critical — keep current view on failure.
      })
    }
  }
```

- [ ] **Step 4: 运行测试确认通过**

Run: `npx vitest run web/src/composables/__tests__/useChatSession.test.ts -t "handleWsReconnect"`
Expected: PASS（5 个用例全过）。

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useChatSession.ts web/src/composables/__tests__/useChatSession.test.ts
git commit -m "feat: reload current session history on WS reconnect when idle"
```

---

### Task 2: 删除 `useChatSession.ts` 的 `handleVisibilityChange`

**Files:**
- Modify: `web/src/composables/useChatSession.ts:980-1000`（函数）、`:1170`（导出）
- Test: `web/src/composables/__tests__/useChatSession.test.ts:3313-3550`（整个 describe 块）

前台不再做数据同步，`handleVisibilityChange` 的前台历史重载逻辑由 Task 1 的重连逻辑取代，函数整体删除。

- [ ] **Step 1: 删除实现与导出**

删除 `useChatSession.ts:980-1000` 的 `handleVisibilityChange` 函数体（含上方 JSDoc 注释）。删除 `useChatSession.ts:1170` 导出项 `handleVisibilityChange,`。

- [ ] **Step 2: 删除测试块**

删除 `useChatSession.test.ts:3313-3550` 整个 `describe('handleVisibilityChange', ...)` 块（含第 3313-3316 行的注释头与第 3551 行的分隔注释）。从 `import { useChatSession, loadSessionsOnce, resetChatSessionState }` 之外无需改动——该函数未在别处 import。

- [ ] **Step 3: 运行测试确认通过**

Run: `npx vitest run web/src/composables/__tests__/useChatSession.test.ts`
Expected: PASS，且无 "handleVisibilityChange is not a function" 或未使用报错。

- [ ] **Step 4: Commit**

```bash
git add web/src/composables/useChatSession.ts web/src/composables/__tests__/useChatSession.test.ts
git commit -m "refactor: remove foreground visibilitychange history reload"
```

---

### Task 3: 移除 `ChatPanelContent.vue` 的 visibilitychange 注册

**Files:**
- Modify: `web/src/components/chat/ChatPanelContent.vue:1094`（注册）、`:1111`（移除）

- [ ] **Step 1: 移除注册**

删除 `ChatPanelContent.vue:1094`：
```ts
    document.addEventListener('visibilitychange', session.handleVisibilityChange)
```

- [ ] **Step 2: 移除反注册**

删除 `ChatPanelContent.vue:1111`：
```ts
    document.removeEventListener('visibilitychange', session.handleVisibilityChange)
```

- [ ] **Step 3: 类型检查确认无残留引用**

Run: `npx vue-tsc --noEmit -p web/tsconfig.json 2>&1 | rg -i "handleVisibilityChange" || echo "NO_REFERENCES"`
Expected: `NO_REFERENCES`。

- [ ] **Step 4: Commit**

```bash
git add web/src/components/chat/ChatPanelContent.vue
git commit -m "refactor: stop reloading chat history on foreground visibilitychange"
```

---

### Task 4: `App.vue` 移除前台全量拉取，升级重连为全量

**Files:**
- Modify: `web/src/App.vue:837-865`（函数）、`:911`（监听）、`:2222`（移除监听）

前台不再做数据同步；重连升级为全量拉取。`clawbench-foreground` 事件本身保留（`useConnectionOverlay` 依赖），只移除 App.vue 对它的监听。

- [ ] **Step 1: 删除 `handleForeground` 函数**

删除 `web/src/App.vue:837-851` 的整个 `handleForeground` 定义（含注释）。

- [ ] **Step 2: 升级 `handleReconnect` 为全量**

将 `web/src/App.vue:856-865` 的 `handleReconnect` 替换为：

```ts
// WS reconnect: refresh all state that may have changed while disconnected.
// WS reconnect is the sole state-sync trigger — foreground no longer pulls data
// (in browser mode WS stays alive and state is already live; only a real
// disconnect→reconnect can make state stale).
const handleReconnect = () => {
    if (!isAuthenticated.value) return
    // Re-establish project cookie — server restart invalidates the session
    // cookie, and without it all /api/dir, /api/file, /api/ai/chat calls
    // return 403 (requireProject: "project cookie is empty").
    store.loadProject().catch(() => {})
    store.loadFiles(store.state.currentDir, false, 0, true)
    store.loadGitBranch()
    loadSessionsOnce()
    loadTasks()
    loadTerminalStatus()
    if (store.state.currentFile?.path) {
        refreshCurrentFile()
    }
}
```

- [ ] **Step 3: 移除 `clawbench-foreground` 监听**

删除 `web/src/App.vue:911`：
```ts
window.addEventListener('clawbench-foreground', handleForeground)
```

删除 `web/src/App.vue:2222`：
```ts
    window.removeEventListener('clawbench-foreground', handleForeground)
```

- [ ] **Step 4: 构建验证**

Run: `npm run build --prefix web 2>&1 | tail -20`（或仓库约定前端构建命令）
Expected: 构建成功，无 `handleForeground` 未定义报错。

- [ ] **Step 5: Commit**

```bash
git add web/src/App.vue
git commit -m "feat: make WS reconnect the sole full state-sync trigger"
```

---

### Task 5: 全量回归验证

- [ ] **Step 1: 运行前端测试**

Run: `npm test`
Expected: 全绿。

- [ ] **Step 2: 运行 pre-push 全量检查**

Run: `./scripts/pre-push-checks.sh --skip-coverage`
Expected: lint / test / build / typecheck 全部通过。

- [ ] **Step 3: 确认无残留引用**

Run: `rg -n "handleVisibilityChange|handleForeground" web/src`
Expected: 仅 `useGlobalEvents.ts` 中的 `handleVisibilityChange`（那是 WS 生命周期函数，保留），无 `App.vue handleForeground` 残留。
