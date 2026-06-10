# Auto-Approve Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a per-session "Auto-Approve" toggle that auto-approves all ACP permission requests while still displaying permission cards in the chat with "自动批准" state.

**Architecture:** The autoApprove flag lives on `ACPConn` (per-session runtime state) and is persisted to `chat_sessions.auto_approve` via the existing PATCH `/api/ai/session` endpoint. When `RequestPermission` is called and autoApprove is true, the backend immediately selects the first `allow_*` option but still emits the `tool_use: PermissionApproval` SSE event with `autoApproved:true` so the frontend can render the card without interactive buttons. The frontend renders a read-only "自动批准" badge instead of Allow/Reject buttons.

**Tech Stack:** Go (backend), Vue 3 + TypeScript (frontend), SQLite (persistence), ACP SDK (permission flow)

**Naming Convention:**
- Internal field name: `autoApprove` (boolean) — clear, describes behavior
- UI tab name: "自动批准" (Chinese) / "Auto-Approve" (English)
- Card status text: "自动批准" (Chinese) / "Auto-Approved" (English)
- SSE field: `autoApproved` (boolean in PermissionApproval input)
- DB column: `auto_approve` (INTEGER 0/1)

---

### Task 1: DB Schema — Add `auto_approve` Column

**Files:**
- Modify: `internal/service/database.go` (migration section)

**Step 1: Add migration**

Add after the `transport` column migration block in `initDB()`:

```go
// Migrate: add auto_approve column for per-session hands-off mode (甩手掌柜)
var hasAutoApprove int
_ = DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='auto_approve'").Scan(&hasAutoApprove)
if hasAutoApprove == 0 {
    if _, err := DB.Exec("ALTER TABLE chat_sessions ADD COLUMN auto_approve INTEGER NOT NULL DEFAULT 0"); err != nil {
        return fmt.Errorf("failed to add auto_approve column: %w", err)
    }
}
```

**Step 2: Run the app to verify migration**

Run: `go build ./... && go test ./internal/service/... -run TestInitDB -count=1`
Expected: PASS, no errors

**Step 3: Commit**

```bash
git add internal/service/database.go
git commit -m "feat: add auto_approve column to chat_sessions table"
```

---

### Task 2: Service Layer — Get/Update AutoApprove

**Files:**
- Modify: `internal/service/chat.go`

**Step 1: Add GetSessionAutoApprove and UpdateSessionAutoApprove**

Add after `UpdateSessionTransport`:

```go
// GetSessionAutoApprove returns whether auto-approve (hands-off) mode is enabled for a session.
func GetSessionAutoApprove(sessionID string) bool {
	var val int
	err := DBRead.QueryRow("SELECT auto_approve FROM chat_sessions WHERE id = ? AND deleted = 0", sessionID).Scan(&val)
	if err != nil {
		return false
	}
	return val == 1
}

// UpdateSessionAutoApprove updates the auto_approve flag for a session.
func UpdateSessionAutoApprove(sessionID string, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	_, err := DB.Exec("UPDATE chat_sessions SET auto_approve = ? WHERE id = ?", val, sessionID)
	return err
}
```

**Step 2: Add tests**

In `internal/service/chat_test.go`, add after the transport tests:

```go
func TestGetSessionAutoApprove_DefaultOff(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "AutoApprove Test")
	assert.False(t, service.GetSessionAutoApprove(sid))
}

func TestUpdateSessionAutoApprove_Enable(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "AutoApprove Enable")
	err := service.UpdateSessionAutoApprove(sid, true)
	assert.NoError(t, err)
	assert.True(t, service.GetSessionAutoApprove(sid))
}

func TestUpdateSessionAutoApprove_Disable(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, "/project", "claude", "AutoApprove Disable")
	service.UpdateSessionAutoApprove(sid, true)
	service.UpdateSessionAutoApprove(sid, false)
	assert.False(t, service.GetSessionAutoApprove(sid))
}
```

**Step 3: Run tests**

Run: `go test ./internal/service/... -run TestSessionAutoApprove -count=1 -v`
Expected: All PASS

**Step 4: Commit**

```bash
git add internal/service/chat.go internal/service/chat_test.go
git commit -m "feat: add GetSessionAutoApprove / UpdateSessionAutoApprove service layer"
```

---

### Task 3: Backend — autoApprove Flag on ACPConn + RequestPermission Auto-Approve Logic

**Files:**
- Modify: `internal/ai/acp_pool.go` (add autoApprove field to ACPConn)
- Modify: `internal/ai/acp_client.go` (auto-approve in RequestPermission)

**Step 1: Add autoApprove field to ACPConn**

In `acp_pool.go`, add to `ACPConn` struct (after `lastSetMode`):

```go
// autoApprove enables hands-off mode: all permission requests are
// automatically approved with the first allow_* option.
autoApprove bool
```

Add getter/setter methods:

```go
// SetAutoApprove enables or disables hands-off mode for this connection.
func (c *ACPConn) SetAutoApprove(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.autoApprove = enabled
}

// IsAutoApprove returns whether hands-off mode is enabled.
func (c *ACPConn) IsAutoApprove() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.autoApprove
}
```

**Step 2: Modify RequestPermission to auto-approve**

In `acp_client.go`, modify `RequestPermission`:

After the `approvalInput` map construction (~line 238), add autoApproved flag:

```go
// Check hands-off (autoApprove) mode — if enabled, mark the event
// and auto-select the first allow option instead of waiting for user.
isAutoApprove := false
if c.connRef != nil {
	isAutoApprove = c.connRef.IsAutoApprove()
}
if isAutoApprove {
	approvalInput["autoApproved"] = true
}
```

Then, after emitting the `tool_use` SSE event (~line 256), before the `select` block, add the auto-approve branch:

```go
if isAutoApprove {
	// Hands-off mode: auto-select the first allow option
	allowOptionID := ""
	for _, opt := range p.Options {
		if opt.Kind == acp.PermissionOptionKindAllowOnce || opt.Kind == acp.PermissionOptionKindAllowAlways {
			allowOptionID = string(opt.OptionId)
			break
		}
	}
	if allowOptionID != "" {
		slog.Info("acp: auto-approving permission request (hands-off mode)",
			"session_id", sessionID,
			"tool_call_id", toolCallID,
			"tool_name", toolName,
			"option_id", allowOptionID,
		)
		// Remove from pending map — we're responding immediately
		c.mu.Lock()
		delete(c.pendingPermission, key)
		c.mu.Unlock()

		// Emit tool_result to mark the PermissionApproval as done
		forwardACPEvent(ch, StreamEvent{
			Type: "tool_result",
			Tool: &ToolCall{
				ID:     permissionBlockID,
				Done:   true,
				Status: "success",
				Output: "Auto-Approved",
			},
		})

		return acp.RequestPermissionResponse{
			Outcome: acp.NewRequestPermissionOutcomeSelected(acp.PermissionOptionId(allowOptionID)),
		}, nil
	}
	// No allow option found — fall through to normal interactive flow
	slog.Warn("acp: hands-off mode but no allow option found, falling back to interactive",
		"session_id", sessionID,
		"tool_call_id", toolCallID,
	)
}
```

**Step 3: Run existing tests**

Run: `go test ./internal/ai/... -count=1 -v -timeout 60s`
Expected: All existing tests PASS (no behavioral change when autoApprove is false)

**Step 4: Commit**

```bash
git add internal/ai/acp_pool.go internal/ai/acp_client.go
git commit -m "feat: auto-approve permission requests in hands-off mode on ACPConn"
```

---

### Task 4: Backend — Wire autoApprove into Session Update/Query API

**Files:**
- Modify: `internal/handler/chat_session.go` (PATCH handler: accept and persist autoApprove)
- Modify: `internal/handler/chat.go` (GET /api/ai/chat: return autoApprove in response; POST: sync to ACPConn)

**Step 1: Add autoApprove to ServeAISessionUpdate request struct**

In `chat_session.go`, `ServeAISessionUpdate`, add to the request struct:

```go
AutoApprove *bool `json:"autoApprove"` // pointer: distinguish "not sent" from false
```

After the Transport handling block, add:

```go
if req.AutoApprove != nil {
	service.UpdateSessionAutoApprove(sessionID, *req.AutoApprove)
	// Sync to ACPConn runtime state
	if conn := ai.GetACPConnManager().GetConn(sessionID); conn != nil {
		conn.SetAutoApprove(*req.AutoApprove)
	}
}
```

**Step 2: Return autoApprove in GET /api/ai/chat response**

In `chat.go`, find where `sessionTransport` is read and add nearby:

```go
sessionAutoApprove := service.GetSessionAutoApprove(sessionID)
```

Then add `"autoApprove": sessionAutoApprove` to the `writeJSON` response maps (both the "no messages" and "has messages" branches).

**Step 3: Sync autoApprove to ACPConn on chat POST**

In `chat.go`'s POST handler (`ServeAIChat`), after the transport sync block, add:

```go
// Sync auto-approve mode from DB to ACPConn on prompt
if conn := ai.GetACPConnManager().GetConn(sessionID); conn != nil {
	conn.SetAutoApprove(service.GetSessionAutoApprove(sessionID))
}
```

**Step 4: Run tests**

Run: `go test ./internal/handler/... -count=1 -v -timeout 60s`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/handler/chat_session.go internal/handler/chat.go
git commit -m "feat: wire autoApprove into session update/query API endpoints"
```

---

### Task 5: Frontend — AutoApprove State in useSessionIdentity

**Files:**
- Modify: `web/src/composables/useSessionIdentity.ts`

**Step 1: Add autoApprove ref and toggle**

Add module-level ref (after `const currentTransport = ref('')`):

```ts
const autoApprove = ref(false)
```

In `resetIdentity()`, add:

```ts
autoApprove.value = false
```

In `initSessionFromAPI()`, after the transport init block, add:

```ts
if (data.autoApprove !== undefined) {
  autoApprove.value = data.autoApprove
}
```

Add toggle function:

```ts
/** Toggle auto-approve (hands-off) mode and persist to server. */
export function toggleAutoApprove(enabled: boolean) {
  autoApprove.value = enabled
  const sid = currentSessionId.value
  if (sid) {
    fetch('/api/ai/session', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId: sid, autoApprove: enabled }),
    }).catch(err => {
      console.error('Failed to update autoApprove:', err)
    })
  }
}
```

In `useSessionIdentity()` return object, add `autoApprove` and `toggleAutoApprove`.

**Step 2: Commit**

```bash
git add web/src/composables/useSessionIdentity.ts
git commit -m "feat: add autoApprove state and toggle to useSessionIdentity"
```

---

### Task 6: Frontend — SessionSettingModal Auto-Approve Toggle (Inside Mode Tab)

**Files:**
- Modify: `web/src/components/chat/SessionSettingModal.vue`

**Step 1: Add auto-approve toggle inside the Mode tab**

Replace the current Mode tab content (lines 151-170) with:

```html
<!-- Mode tab -->
<div v-if="activeTab === 'mode'" class="model-tab-content">
  <div class="model-list">
    <div
      v-for="(mode, idx) in availableModes"
      :key="mode.id"
      class="model-item-wrapper"
    >
      <button
        class="thinking-item"
        :class="{ current: mode.id === currentModeId }"
        @click="selectMode(mode)"
      >
        <span class="model-item-indicator" :class="{ active: mode.id === currentModeId }"></span>
        <span class="model-item-name">{{ mode.name || mode.id }}</span>
      </button>
      <div v-if="idx < availableModes.length - 1" class="model-divider"></div>
    </div>
  </div>
  <!-- Auto-Approve toggle (embedded in Mode tab) -->
  <div class="auto-approve-section">
    <div class="model-divider"></div>
    <div class="auto-approve-toggle">
      <div class="auto-approve-label">
        <span class="auto-approve-title">{{ t('chat.autoApprove.title') }}</span>
        <span class="auto-approve-desc">{{ t('chat.autoApprove.description') }}</span>
      </div>
      <label class="toggle-switch">
        <input type="checkbox" :checked="autoApprove" @change="toggleAutoApprove($event.target.checked)" />
        <span class="toggle-slider"></span>
      </label>
    </div>
  </div>
</div>
```

**Step 2: Add toggle switch CSS**

Add in the `<style scoped>` section:

```scss
.auto-approve-section {
  flex-shrink: 0;
  border-top: 1px solid var(--border-color, #e5e5e5);
}

.auto-approve-toggle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px;
}

.auto-approve-label {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.auto-approve-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}

.auto-approve-desc {
  font-size: 11px;
  color: var(--text-muted, #999);
  line-height: 1.3;
}

.toggle-switch {
  position: relative;
  display: inline-block;
  width: 36px;
  height: 20px;
  flex-shrink: 0;
}

.toggle-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}

.toggle-slider {
  position: absolute;
  cursor: pointer;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: var(--bg-tertiary, #ccc);
  transition: 0.2s;
  border-radius: 20px;
}

.toggle-slider::before {
  position: absolute;
  content: "";
  height: 16px;
  width: 16px;
  left: 2px;
  bottom: 2px;
  background-color: white;
  transition: 0.2s;
  border-radius: 50%;
}

.toggle-switch input:checked + .toggle-slider {
  background-color: var(--accent-color, #0066cc);
}

.toggle-switch input:checked + .toggle-slider::before {
  transform: translateX(16px);
}
```

**Step 3: Add script references**

In the `<script setup>` section, destructure `autoApprove` and `toggleAutoApprove` from `useSessionIdentity()`:

```ts
const { ..., autoApprove, toggleAutoApprove } = useSessionIdentity()
```

**Step 4: Commit**

```bash
git add web/src/components/chat/SessionSettingModal.vue
git commit -m "feat: add auto-approve toggle inside Mode tab in SessionSettingModal"
```

---

### Task 7: Frontend — PermissionApproval Card Auto-Approved Rendering

**Files:**
- Modify: `web/src/utils/renderToolDetail.ts` (renderPermissionApproval function)

**Step 1: Modify renderPermissionApproval to handle autoApproved**

In `renderPermissionApproval`, after `const isApproved = ...`, add:

```ts
const isAutoApproved = input.autoApproved === true
```

In the `permission-approval-view` class list, add:

```ts
if (isAutoApproved) {
  html += ' permission-auto-approved'
}
```

Change the header to show a different icon and title when auto-approved:

```ts
// Header
html += '<div class="permission-header">'
if (isAutoApproved) {
  html += `<span class="permission-icon">✅</span>`
  html += `<span class="permission-title">${escapeHtml(gt('tool.permission.autoApprovedTitle'))}</span>`
} else {
  html += `<span class="permission-icon">⚠️</span>`
  html += `<span class="permission-title">${escapeHtml(gt('tool.permission.title'))}</span>`
}
html += '</div>'
```

For the result/bottom section, when `isAutoApproved` and not yet done, show a static "自动批准" badge instead of buttons:

```ts
// Option buttons / result
if (hasRealResult) {
  // Already responded — show result badge
  if (isApproved) {
    html += `<div class="permission-result permission-result-approved">${escapeHtml(gt('tool.permission.approved'))}</div>`
  } else {
    html += `<div class="permission-result permission-result-denied">${escapeHtml(gt('tool.permission.denied'))}</div>`
  }
} else if (isAutoApproved) {
  // Auto-approved but SSE result not yet arrived — show auto-approved badge
  html += `<div class="permission-result permission-result-auto-approved">${escapeHtml(gt('tool.permission.autoApproved'))}</div>`
} else if (options.length > 0) {
  // ... existing button rendering unchanged
}
```

**Step 2: Skip the click handler for auto-approved cards**

In the PermissionApproval action handler, at the top where it checks `permission-responded`, also check for `permission-auto-approved`:

```ts
if (!view || view.classList.contains('permission-responded') || view.classList.contains('permission-auto-approved')) {
  return true
}
```

**Step 3: Commit**

```bash
git add web/src/utils/renderToolDetail.ts
git commit -m "feat: render auto-approved state on PermissionApproval cards"
```

---

### Task 8: Frontend — i18n Keys for Auto-Approve Mode

**Files:**
- Modify: `web/src/locales/en.ts`
- Modify: `web/src/locales/zh.ts`

**Step 1: Add English locale keys**

```ts
'chat.autoApprove.title': 'Auto-Approve',
'chat.autoApprove.description': 'Automatically approve all permission requests from the agent.',
'tool.permission.autoApprovedTitle': 'Auto-Approved',
'tool.permission.autoApproved': 'Auto-Approved',
```

**Step 2: Add Chinese locale keys**

```ts
'chat.autoApprove.title': '自动批准',
'chat.autoApprove.description': '自动批准代理的所有权限请求。',
'tool.permission.autoApprovedTitle': '自动批准',
'tool.permission.autoApproved': '自动批准',
```

**Step 3: Commit**

```bash
git add web/src/locales/en.ts web/src/locales/zh.ts
git commit -m "feat: add i18n keys for auto-approve mode"
```

---

### Task 9: Frontend — CSS for Auto-Approved Permission Card

**Files:**
- Modify: `web/src/assets/chat.scss` (or the relevant stylesheet for permission cards)

**Step 1: Add styles for auto-approved permission card**

```scss
.permission-auto-approved {
  .permission-header {
    opacity: 0.85;
  }
}

.permission-result-auto-approved {
  display: inline-block;
  padding: 4px 12px;
  border-radius: 4px;
  font-size: 13px;
  font-weight: 500;
  color: #15803d;
  background: #dcfce7;
  border: 1px solid #bbf7d0;
}
```

**Step 2: Commit**

```bash
git add web/src/assets/chat.scss
git commit -m "feat: add CSS for auto-approved permission card state"
```

---

### Task 10: Frontend — Stream Timeout for Auto-Approved Permissions

**Files:**
- Modify: `web/src/composables/useChatStream.ts`

**Step 1: Skip extended stream timeout for auto-approved PermissionApproval**

In `hasPendingPermissionApproval()`, also check that the block is not auto-approved. Auto-approved blocks resolve almost instantly, so they shouldn't trigger the 5-minute timeout:

```ts
function hasPendingPermissionApproval(): boolean {
  const streamingMsg = messages.value.find((m: any) => m.role === 'assistant' && m.streaming)
  if (!streamingMsg?.blocks) return false
  return streamingMsg.blocks.some(
    (b: any) =>
      b.type === 'tool_use' &&
      b.name === 'PermissionApproval' &&
      !b.done &&
      !b.input?.autoApproved
  )
}
```

**Step 2: Commit**

```bash
git add web/src/composables/useChatStream.ts
git commit -m "fix: skip extended stream timeout for auto-approved permissions"
```

---

### Task 11: Backend — Auto-Approve Unit Test

**Files:**
- Modify: `internal/ai/acp_client_test.go` (or create new test file)

**Step 1: Write test for auto-approve in RequestPermission**

```go
func TestRequestPermission_AutoApprove(t *testing.T) {
	client := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{autoApprove: true}
	client.connRef = conn

	acpSessionID := "test-acp-sid"
	client.RegisterSession(acpSessionID, ch)

	ctx := context.Background()
	allowOptID := "allow_once"
	resp, err := client.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(acpSessionID),
		ToolCall: acp.RequestPermissionToolCall{
			ToolCallId: "tc-1",
			Title:      ptrStr("Bash"),
			Kind:       ptrKind(acp.ToolKindExecute),
		},
		Options: []acp.PermissionOption{
			{OptionId: acp.PermissionOptionId(allowOptID), Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce},
			{OptionId: "reject", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
		},
	})

	assert.NoError(t, err)
	assert.NotNil(t, resp.Outcome.Selected)
	assert.Equal(t, acp.PermissionOptionId(allowOptID), resp.Outcome.Selected.OptionId)

	// Should have emitted tool_use and tool_result events
	var toolUseFound, toolResultFound bool
	for i := 0; i < 2; i++ {
		select {
		case evt := <-ch:
			if evt.Type == "tool_use" && evt.Tool.Name == "PermissionApproval" {
				toolUseFound = true
				var input map[string]any
				assert.NoError(t, json.Unmarshal([]byte(evt.Tool.Input), &input))
				assert.True(t, input["autoApproved"].(bool))
			}
			if evt.Type == "tool_result" {
				toolResultFound = true
				assert.Equal(t, "Auto-Approved", evt.Tool.Output)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for SSE event")
		}
	}
	assert.True(t, toolUseFound, "expected tool_use event for PermissionApproval")
	assert.True(t, toolResultFound, "expected tool_result event for PermissionApproval")
}

func ptrStr(s string) *string { return &s }
func ptrKind(k acp.ToolKind) *acp.ToolKind { return &k }
```

**Step 2: Run test**

Run: `go test ./internal/ai/... -run TestRequestPermission_AutoApprove -count=1 -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/ai/acp_client_test.go
git commit -m "test: add unit test for auto-approve in RequestPermission"
```

---

### Task 12: E2E Smoke Test — Manual Verification

**No code changes.** Manual verification checklist:

1. Start the app, open chat with an ACP agent
2. Open Session Settings → 自动批准 tab → 开启
3. Send a message that triggers a permission request (e.g., file write)
4. Verify: permission card appears with "自动批准" badge, no buttons
5. Verify: agent proceeds without waiting for user click
6. 关闭自动批准 → trigger another permission → verify interactive buttons return
7. Refresh page → verify 自动批准 state persists
8. Check DB: `SELECT auto_approve FROM chat_sessions WHERE id = ?` → should be 1

---

## Summary of All File Changes

| File | Change |
|------|--------|
| `internal/service/database.go` | Add `auto_approve` column migration |
| `internal/service/chat.go` | Add `GetSessionAutoApprove` / `UpdateSessionAutoApprove` |
| `internal/service/chat_test.go` | Add auto-approve service tests |
| `internal/ai/acp_pool.go` | Add `autoApprove` field + getter/setter on `ACPConn` |
| `internal/ai/acp_client.go` | Auto-approve branch in `RequestPermission` |
| `internal/ai/acp_client_test.go` | Auto-approve unit test |
| `internal/handler/chat_session.go` | Accept `autoApprove` in PATCH, sync to ACPConn |
| `internal/handler/chat.go` | Return `autoApprove` in GET response, sync on POST |
| `web/src/composables/useSessionIdentity.ts` | Add `autoApprove` ref + `toggleAutoApprove` |
| `web/src/components/chat/SessionSettingModal.vue` | Add 自动批准 toggle inside Mode tab |
| `web/src/utils/renderToolDetail.ts` | Auto-approved card rendering |
| `web/src/composables/useChatStream.ts` | Skip extended timeout for auto-approved |
| `web/src/locales/en.ts` | English i18n keys |
| `web/src/locales/zh.ts` | Chinese i18n keys |
| `web/src/assets/chat.scss` | Auto-approved card CSS |
