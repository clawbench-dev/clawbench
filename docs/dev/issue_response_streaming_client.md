# Issue: Response Streaming — Client UI Does Not Refresh

**Branch:** `fix/response-streaming-client`  
**Status:** Implemented (Phase 1 + Phase 2)  
**Affected surfaces:** Web UI, Android WebView (Android is worse; desktop browser can also hit edge cases)

---

## Summary

After the AI finishes generating a response, the **server completes successfully** and persists the full assistant message to the database. The **client UI does not show the final answer** until the user restarts the app or reloads the page.

This is a **frontend lifecycle / refresh gap**, not a backend streaming or persistence failure.

---

## Symptoms

| Observation | Detail |
|-------------|--------|
| Chat appears stuck | User message visible; assistant reply missing or stuck on streaming indicators |
| Server is healthy | Logs show ACP/OpenCode session completing; SQLite has the full assistant message |
| Restart fixes it | Full page reload or app restart loads messages from REST and the answer appears |
| Android worse | Hanging sessions reported more often on Android LAN clients |
| `text.done=false` in DB | Sometimes seen on text blocks — **not the root cause** (see [Red herrings](#red-herrings)) |

---

## Architecture: Two Completion Paths

The client has two independent channels for session lifecycle:

```
┌─────────────────────────────────────────────────────────────────┐
│                        User sends message                        │
└───────────────────────────────┬─────────────────────────────────┘
                                │
            ┌───────────────────┴───────────────────┐
            ▼                                       ▼
   SSE  /api/ai/chat/stream              WebSocket  session_update
   (content deltas + final blocks)        (lifecycle: running → completed)
            │                                       │
            ▼                                       ▼
   `done` event in useChatStream.ts      onSessionEvent() in useChatSession.ts
            │                                       │
            ▼                                       ▼
   ✅ calls loadHistory()                  ❌ removes runningSessions only
   (replaces messages from REST)           (no loadHistory for current session)
```

### Path A — SSE (primary, works when connected)

`web/src/composables/useChatStream.ts` listens for the SSE `done` event and calls `onLoadHistory()`:

```544:575:web/src/composables/useChatStream.ts
    eventSource.addEventListener('done', () => {
      // ...
      disconnectStream()
      reconnect.reset()
      onLoadHistory().then(() => {
        // ...
      }).finally(() => {
        loading.value = false
        // ...
      })
    })
```

When this path fires, the UI refreshes correctly.

### Path B — WebSocket `session_update` (fallback, incomplete)

`web/src/components/chat/ChatPanelContent.vue` wires WS events to `session.onSessionEvent()`:

```920:926:web/src/components/chat/ChatPanelContent.vue
const removeEventHandler = onEvent((event, data) => {
    if (event === 'session_update') {
        session.onSessionEvent(data)
    }
})
```

`onSessionEvent()` in `useChatSession.ts` handles `completed` / `cancelled` by updating `runningSessions` and debouncing `loadSessionsOnce()` **only for other sessions**:

```837:867:web/src/composables/useChatSession.ts
  function onSessionEvent(data: { session_id?: string; status?: string; has_new_messages?: boolean } | undefined) {
    // ...
    } else {
      if (sid) { runningSessions.value.delete(sid); runningSessionsVersion.value++ }
      store.state.chatRunning = runningSessions.value.size > 0
      if (sid && sid !== currentSessionId.value) {
        // debounced loadSessionsOnce() — updates session list unread dots only
      }
    }
  }
```

**Gap:** When `sid === currentSessionId.value` and `status === 'completed'`, nothing calls `loadHistory()`. The chat panel keeps stale in-memory messages.

Existing tests explicitly expect this behaviour today:

```332:338:web/src/composables/__tests__/useChatSession.test.ts
  it('does not mark chatUnread when the current session completes', () => {
    session.onSessionEvent({ session_id: 'current-s1', status: 'completed' })
    expect(mockState.chatUnreadCount).toBe(0)
  })
```

The unread-count behaviour is correct; the missing piece is **message refresh for the active session**.

---

## Root Cause

**The WebSocket completion handler does not reload chat history for the session the user is currently viewing.**

The SSE `done` handler is the only reliable refresh for the active session. Any scenario where SSE is disconnected, throttled, or misses the `done` event leaves the UI stale until a full restart triggers REST `loadHistory()`.

---

## Contributing Factors (Why Android Is Worse)

### 1. SSE disconnected on background

`useChatStream.ts` disconnects the SSE stream when the page becomes hidden, with **no reconnect on visible**:

```895:899:web/src/composables/useChatStream.ts
  function handleStreamVisibility() {
    if (document.visibilityState === 'hidden') {
      disconnectStream()
      stopPolling()
    }
  }
```

If the AI finishes while the tab/WebView is backgrounded, the client may never receive the SSE `done` event.

### 2. WebSocket disconnected in app mode on background

`useGlobalEvents.ts` disconnects the WebSocket when `isAppMode` and the page is hidden:

```416:431:web/src/composables/useGlobalEvents.ts
    function handleVisibilityChange() {
        if (document.visibilityState === 'visible') {
            if (!connected.value) connect()
            window.dispatchEvent(new CustomEvent('clawbench-foreground'))
        } else {
            if (isAppMode.value) {
                disconnect()
                reconnect.disable()
                setTimeout(() => reconnect.reset(), 100)
            }
        }
    }
```

So on Android, **both** SSE and WS can be down when the response completes.

### 3. Foreground handler does not refresh chat messages

`App.vue` `handleForeground` refreshes sessions list, files, git, tasks, and terminal — but **not** the current chat history:

```610:624:web/src/App.vue
const handleForeground = () => {
    if (!isAuthenticated.value) return
    loadSessionsOnce()
    store.loadFiles(store.state.currentDir)
    store.loadGitBranch()
    loadTasks()
    loadTerminalStatus()
    // ... no loadHistory() for active chat session
}
```

### 4. Android lifecycle pauses WebView

`MainActivity.java` calls `webView.onPause()` / `pauseTimers()` on background and toggles native WS. Combined with the above, completion events are easy to miss.

### 5. Partial visibility recovery exists but is narrow

`useChatSession.handleVisibilityChange()` only reloads when `loading.value` is still true:

```879:888:web/src/composables/useChatSession.ts
  function handleVisibilityChange() {
    if (document.visibilityState === 'visible' && loading.value) {
      onDisconnectStream()
      onStopPolling()
      loadHistory(true, false, true).catch(() => {
        loading.value = false
      })
    }
  }
```

If `loading` was cleared (or never set) after SSE disconnect, this path does not run.

---

## Red Herrings

| Observation | Verdict |
|-------------|---------|
| `text.done=false` on assistant text blocks in SQLite | **Normal** — `internal/ai/accumulate.go` sets `Done` on `thinking_done` and `tool_use` events, not on plain `content` text deltas. Final text blocks often have `done=false` while the message is complete. |
| DB patch for stuck `text.done=false` | Workaround only; does not fix client refresh. |
| Backend / OpenCode errors | Separate issue; this bug occurs when the server **succeeds** but the client does not refresh. |
| APK rebuild required | **No** — Android loads UI from the server. Frontend deploy is sufficient. |

---

## Fix Plan

### Phase 1 — Core fix (required)

**File:** `web/src/composables/useChatSession.ts` — `onSessionEvent()`

When a terminal status arrives for the **current** session, call `loadHistory()`:

```typescript
// Pseudocode — implement with existing debounce / in-flight guards
} else if (data.status === 'completed' || data.status === 'cancelled') {
  if (sid) { runningSessions.value.delete(sid); /* ... */ }

  if (sid === currentSessionId.value) {
    loading.value = false
    await loadHistory(false, false, false)  // force refresh; don't skipIfUnchanged
  } else if (sid) {
    // existing debounced loadSessionsOnce() for other sessions
  }
}
```

**Tests:** Update `web/src/composables/__tests__/useChatSession.test.ts`:

- Add: current session `completed` → `loadHistory` called, messages refreshed.
- Add: current session `cancelled` → same.
- Keep: other-session `completed` → `loadSessionsOnce` only (no full history load for current).
- Keep: current session completion does not set `chatUnread`.

### Phase 2 — Resilience (recommended)

| File | Change |
|------|--------|
| `useChatStream.ts` | On `visibilitychange` → `visible`, if `loading` or session was running, reconnect SSE or trigger `loadHistory`. |
| `App.vue` | In `handleForeground`, if chat tab is active (or session was running), call `loadHistory()` for `currentSessionId`. |
| `useChatSession.ts` | Broaden `handleVisibilityChange` to reload when session was running recently, not only when `loading.value === true`. |

### Phase 3 — Optional hardening

- Reconnect SSE after foreground if backend reports `running: false` but local state still shows streaming.
- Add diagnostic logging: `[session_update:completed] sid=… current=… loadHistory=triggered`.

### Out of scope (this branch)

- Backend streaming changes
- Android APK rebuild
- Go binary rebuild (see deployment below)

---

## Deployment

The fix is **frontend-only**. No new Go binary is required if serving from disk `public/`:

```bash
cd /path/to/clawbench   # e.g. D:\ProgramData\clawbench or /mnt/d/ProgramData/clawbench
git checkout fix/response-streaming-client
npm run build

# Hot-swap next to running binary (ClawBench prefers disk public/ over embed)
cp -r public /root/clawbench/public   # Linux/WSL runtime
# or symlink: ln -sfn /mnt/d/ProgramData/clawbench/public /root/clawbench/public

# Restart server
kill -9 $(pgrep -f 'clawbench --data-dir')
cd /root/clawbench && ./start.sh
```

Verify in logs: `frontend: serving from disk` (not `embedded`).

**Android:** No APK rebuild — WebView loads UI from the server. Refresh or reconnect after deploy.

**Optional:** `./build.sh` to bake frontend into a new single-binary release for distribution.

---

## Test Plan

### Manual

1. **Desktop browser — happy path (SSE):** Send message, stay on chat tab → answer appears without reload. (Baseline; should already work.)
2. **Desktop — tab background:** Send message, switch to another tab before completion, return → answer visible without reload.
3. **Android — foreground:** Send message, keep app open → answer appears.
4. **Android — background:** Send message, switch away, return after completion → answer visible without app restart.
5. **Session list:** Complete session B while viewing session A → A unchanged; B shows updated in drawer.
6. **Cancelled:** Cancel mid-stream → UI shows partial/cancelled state without stuck spinner.

### Automated

```bash
npm run test -- web/src/composables/__tests__/useChatSession.test.ts
```

Add cases for current-session `completed` / `cancelled` triggering `loadHistory`.

### Regression checks

- Unread dots still update for **other** sessions on `session_update`.
- `loadHistory` sequence guard still discards stale responses on rapid session switches.
- No duplicate messages when both SSE `done` and WS `completed` fire (coalesce via `loadHistoryInProgress` / `skipIfUnchanged`).

---

## Related Files

| File | Role |
|------|------|
| `web/src/composables/useChatSession.ts` | **Primary fix** — `onSessionEvent`, `loadHistory`, `handleVisibilityChange` |
| `web/src/composables/useChatStream.ts` | SSE `done` → `loadHistory`; visibility disconnect |
| `web/src/composables/useGlobalEvents.ts` | WS lifecycle; app-mode background disconnect |
| `web/src/App.vue` | `handleForeground` — missing chat refresh |
| `web/src/components/chat/ChatPanelContent.vue` | Wires WS → `onSessionEvent` |
| `internal/frontend/embed.go` | Disk `public/` hot-swap support |
| `android/.../MainActivity.java` | WebView pause/resume; native WS fallback |

---

## References

- Observed on WSL deployment at `/root/clawbench` (v0.57.7) with Android LAN client
- Source repo: `D:\ProgramData\clawbench` (mirrored at `/mnt/d/ProgramData/clawbench` in WSL)
- Frontend serving: embedded unless `public/` exists beside the binary (`internal/frontend/embed.go`)
