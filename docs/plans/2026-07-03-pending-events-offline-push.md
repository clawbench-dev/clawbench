# Pending Events Persistence + On-Reconnect Fetch Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Persist terminal-state WS events to SQLite so clients that were offline (device off, network loss) can fetch missed notifications on reconnect, instead of losing them after the 10s in-memory buffer window.

**Architecture:** Global append-only `pending_events` SQLite table stores terminal state events. Clients track their own position with a `last_seen_event_id` cursor in localStorage/SharedPreferences. On WS reconnect, clients fetch missed events via `GET /api/ai/events/pending?after=evt_xxx`. Events only stored when no WS client is connected (conditional storage). `permission_pending` events get 7-day TTL; other terminal events get 24h TTL. Event IDs include server instance ID to be unique across restarts.

**Tech Stack:** Go (SQLite via modernc.org/sqlite), OkHttp (Android HTTP GET), fetch API (frontend)

**Arch Review Fixes Applied:**
- C1: Dedup on Android + post-replay fetch ordering
- C2: Write-ahead (store before broadcast)
- C3: `expires_at` column: permission_pending=7d, others=24h
- C4: Conditional storage only when clients disconnected
- C5: `GenerateEventID()` includes server instance ID
- C6: UNIQUE index on `event_id`
- C7: `IsNotifiableEvent` handles both pointer and map types

---

### Task 1: Fix `GenerateEventID()` for cross-restart uniqueness

**Files:**
- Modify: `internal/ws/manager.go` (GenerateEventID + serverInstanceID)
- Test: `internal/ws/manager_test.go` (extend)

**Context:** Current `GenerateEventID()` uses an in-memory atomic counter that resets to 0 on server restart. This causes `evt_1`, `evt_2` etc. to collide across restarts, breaking the `pending_events` cursor and causing `INSERT OR IGNORE` to silently drop rows.

**Implementation:**

In `internal/ws/manager.go`, replace the current `GenerateEventID`:

```go
// serverInstanceID is set once at init time to make event IDs unique across restarts.
var serverInstanceID string

// eventSeq is an atomic counter to ensure unique event IDs within a server instance.
var eventSeq atomic.Int64

func init() {
	serverInstanceID = fmt.Sprintf("%d", time.Now().UnixMilli())
}

// GenerateEventID creates a globally unique event ID.
// Format: evt_{unixmillis}_{counter} — unique across server restarts.
func GenerateEventID() string {
	return fmt.Sprintf("evt_%s_%d", serverInstanceID, eventSeq.Add(1))
}
```

**Test:**

```go
func TestGenerateEventID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := GenerateEventID()
		if ids[id] {
			t.Fatalf("duplicate event ID: %s", id)
		}
		ids[id] = true
		if !strings.HasPrefix(id, "evt_") {
			t.Fatalf("event ID doesn't start with evt_: %s", id)
		}
	}
}
```

**Commit:** `feat(ws): make GenerateEventID unique across server restarts`

---

### Task 2: Add `pending_events` table + `HasDisconnectedClients()` to WS Manager

**Files:**
- Modify: `internal/service/database.go` (CREATE TABLE block)
- Modify: `internal/ws/manager.go` (add HasDisconnectedClients)
- Test: `internal/service/pending_events_test.go` (new)
- Test: `internal/ws/manager_test.go` (extend)

**Table schema:**

```sql
-- Pending events for offline push notifications (added 2026-07)
CREATE TABLE IF NOT EXISTS pending_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pending_event_id ON pending_events(event_id);
CREATE INDEX IF NOT EXISTS idx_pending_expires ON pending_events(expires_at);
```

Key differences from original plan:
- No `client_id` column — global event log
- No `acked` column — client-side cursor
- `expires_at` column — permission_pending gets 7d, others get 24h
- UNIQUE index on `event_id` for cursor lookup performance

**`HasDisconnectedClients()` in manager.go:**

```go
// HasDisconnectedClients returns true if any subscription is disconnected
// or if there are no subscriptions at all. Used to conditionally persist
// events only when clients might miss them.
func (m *Manager) HasDisconnectedClients() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.subscriptions) == 0 {
		return true
	}
	for _, sub := range m.subscriptions {
		sub.mu.Lock()
		disconnected := sub.conn == nil
		sub.mu.Unlock()
		if disconnected {
			return true
		}
	}
	return false
}
```

**Test for HasDisconnectedClients:**

```go
func TestManager_HasDisconnectedClients(t *testing.T) {
	m := NewManagerForTest()
	// No subscriptions → true
	if !m.HasDisconnectedClients() {
		t.Fatal("expected true with no subscriptions")
	}

	// Add connected subscription → false
	conn1, _, _ := testConn() // helper to create test websocket.Conn
	sub := m.Subscribe(conn1, &sync.Mutex{}, "client-1", "en")
	_ = sub
	if m.HasDisconnectedClients() {
		t.Fatal("expected false with connected client")
	}

	// Disconnect → true
	m.DisconnectClient("client-1")
	if !m.HasDisconnectedClients() {
		t.Fatal("expected true after disconnect")
	}
}
```

**Commit:** `feat: add pending_events table and HasDisconnectedClients`

---

### Task 3: Add pending events CRUD + IsNotifiableEvent + StoreNotifiableEvent

**Files:**
- Create: `internal/service/pending_events.go`
- Test: `internal/service/pending_events_test.go` (extend)

**Implementation:**

```go
package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"clawbench/internal/ws"
)

// PendingEvent represents a persisted event for offline clients.
type PendingEvent struct {
	ID        int64  `json:"-"`
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Payload   string `json:"payload"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

const (
	pendingEventTTL          = 24 * time.Hour
	pendingEventPermPendTTL  = 7 * 24 * time.Hour // permission_pending: 7 days
	pendingEventMaxRows      = 1000
)

// IsNotifiableEvent returns true if the event is a terminal state that
// should be persisted for offline clients.
func IsNotifiableEvent(event string, data any) bool {
	var status string
	switch d := data.(type) {
	case *ws.SessionUpdateData:
		status = d.Status
	case *ws.TaskUpdateData:
		status = d.Status
	case map[string]any:
		if s, ok := d["status"].(string); ok {
			status = s
		}
	default:
		return false
	}
	switch event {
	case "session_update":
		return status == "completed" || status == "cancelled" || status == "permission_pending"
	case "task_update":
		return status == "completed" || status == "failed" || status == "cancelled"
	default:
		return false
	}
}

// pendingEventExpiresAt returns the expires_at timestamp for an event type.
func pendingEventExpiresAt(event, status string) string {
	if event == "session_update" && status == "permission_pending" {
		return time.Now().Add(pendingEventPermPendTTL).Format(time.RFC3339)
	}
	return time.Now().Add(pendingEventTTL).Format(time.RFC3339)
}

// StorePendingEvent persists a notifiable event to the global event log.
func StorePendingEvent(eventID, eventType, payload, expiresAt string) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(
		`INSERT OR IGNORE INTO pending_events (event_id, event_type, payload, expires_at) VALUES (?, ?, ?, ?)`,
		eventID, eventType, payload, expiresAt,
	)
	if err != nil {
		return err
	}
	// Evict expired events and cap total rows
	_, _ = DB.Exec(`DELETE FROM pending_events WHERE expires_at < datetime('now')`)
	_, _ = DB.Exec(
		`DELETE FROM pending_events WHERE id NOT IN (
			SELECT id FROM pending_events ORDER BY created_at DESC LIMIT ?
		)`,
		pendingEventMaxRows,
	)
	return err
}

// GetPendingEvents returns events optionally after a cursor event_id.
func GetPendingEvents(afterEventID string) ([]PendingEvent, error) {
	if DB == nil || DBRead == nil {
		return nil, nil
	}
	var rows *sql.Rows
	var err error
	if afterEventID != "" {
		rows, err = DBRead.Query(
			`SELECT event_id, event_type, payload, expires_at, created_at
			 FROM pending_events
			 WHERE expires_at >= datetime('now')
			   AND id > (SELECT COALESCE(id, 0) FROM pending_events WHERE event_id = ?)
			 ORDER BY id ASC`,
			afterEventID,
		)
	} else {
		rows, err = DBRead.Query(
			`SELECT event_id, event_type, payload, expires_at, created_at
			 FROM pending_events
			 WHERE expires_at >= datetime('now')
			 ORDER BY id ASC`,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []PendingEvent
	for rows.Next() {
		var e PendingEvent
		if err := rows.Scan(&e.EventID, &e.EventType, &e.Payload, &e.ExpiresAt, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// CleanupPendingEvents removes expired events.
func CleanupPendingEvents() {
	if DB == nil {
		return
	}
	result, err := DB.Exec(`DELETE FROM pending_events WHERE expires_at < datetime('now')`)
	if err != nil {
		slog.Warn("pending_events: cleanup failed", "error", err)
	} else if n, _ := result.RowsAffected(); n > 0 {
		slog.Debug("pending_events: cleaned up expired", "count", n)
	}
}

// StoreNotifiableEvent persists a notifiable WS event if it's a terminal state.
// Only stores when there are disconnected clients (conditional storage).
// Uses write-ahead: store before broadcast so the event log has no gaps.
func StoreNotifiableEvent(msg ws.ServerMessage) {
	if !IsNotifiableEvent(msg.Event, msg.Data) {
		return
	}
	// Conditional storage: only persist if clients are disconnected
	mgr := ws.GetManager()
	if mgr != nil && !mgr.HasDisconnectedClients() {
		return
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		slog.Warn("pending_events: marshal failed", "error", err)
		return
	}
	// Determine status for expires_at calculation
	status := ""
	if d, ok := msg.Data.(*ws.SessionUpdateData); ok {
		status = d.Status
	} else if d, ok := msg.Data.(*ws.TaskUpdateData); ok {
		status = d.Status
	}
	expiresAt := pendingEventExpiresAt(msg.Event, status)
	if err := StorePendingEvent(msg.ID, msg.Event, string(payload), expiresAt); err != nil {
		slog.Warn("pending_events: store failed", "error", err)
	}
}
```

**Tests:** Cover StorePendingEvent, GetPendingEvents (with/without cursor), CleanupPendingEvents, IsNotifiableEvent (all event types + map[string]any fallback), conditional storage behavior.

**Commit:** `feat: add pending events CRUD with conditional storage and expires_at`

---

### Task 4: Wire StoreNotifiableEvent into EmitSessionEvent and emitTaskEvent (write-ahead)

**Files:**
- Modify: `internal/service/session_runtime.go:47-83` (EmitSessionEvent)
- Modify: `internal/service/scheduler.go:515-542` (emitTaskEvent)

**Key change:** Generate event ID once, store (write-ahead), then broadcast. Same event ID used for both.

**EmitSessionEvent refactor:**

```go
func EmitSessionEvent(sessionID, status string, hasNewMessages bool, toolName ...string) {
	mgr := ws.GetManager()
	if mgr == nil {
		return
	}

	data := &ws.SessionUpdateData{...} // existing data construction

	// Generate one event ID, use for both store and broadcast (write-ahead)
	msg := ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		ID:    ws.GenerateEventID(),
		Event: "session_update",
		Data:  data,
	}
	// Write-ahead: persist before broadcast so event log has no gaps
	StoreNotifiableEvent(msg)
	mgr.BroadcastEvent(msg)
}
```

**emitTaskEvent refactor:** Same pattern.

**Commit:** `feat: wire StoreNotifiableEvent with write-ahead into event emitters`

---

### Task 5: Add GET /api/ai/events/pending HTTP endpoint

**Files:**
- Create: `internal/handler/pending_events.go`
- Modify: `internal/handler/handler.go` (add route)
- Test: `internal/handler/pending_events_test.go`

Handler returns `{ "events": [...] }` where each event has `event_id`, `event_type`, `payload`, `expires_at`, `created_at`.

Route: `register("/api/ai/events/pending", middleware.Auth(http.HandlerFunc(ServePendingEvents)))`

**Commit:** `feat: add GET /api/ai/events/pending endpoint`

---

### Task 6: Wire CleanupPendingEvents into periodic cleanup loop

**Files:**
- Find and modify the file that calls `ws.GetManager().CleanupStale()` periodically

Add `service.CleanupPendingEvents()` in the same goroutine/ticker.

**Commit:** `feat: add pending events cleanup to periodic stale cleanup`

---

### Task 7: Android — Add event dedup + fetch pending events on WS reconnect

**Files:**
- Modify: `android/app/src/main/java/com/clawbench/app/BackgroundService.java`

**Key changes:**

1. **Add `processedEventIds` LRU dedup set** (mirrors frontend pattern):

```java
private final LinkedHashSet<String> processedEventIds = new LinkedHashSet<>();
private static final int MAX_PROCESSED_IDS = 100;

private boolean isDuplicateEvent(String eventId) {
    return processedEventIds.contains(eventId);
}

private void addProcessedEventId(String eventId) {
    if (processedEventIds.size() >= MAX_PROCESSED_IDS) {
        Iterator<String> it = processedEventIds.iterator();
        it.next();
        it.remove();
    }
    processedEventIds.add(eventId);
}
```

2. **Add dedup check in NativeEventListener.onMessage():**

```java
// Dedup check
if (!eventId.isEmpty() && isDuplicateEvent(eventId)) {
    return; // skip duplicate
}
if (!eventId.isEmpty()) {
    addProcessedEventId(eventId);
}
```

3. **Add `KEY_LAST_SEEN_EVENT_ID` SharedPreferences key**

4. **Update cursor on every WS event** (after ack):

```java
if (!eventId.isEmpty()) {
    getSharedPreferences(PREFS_NAME, MODE_PRIVATE)
            .edit().putString(KEY_LAST_SEEN_EVENT_ID, eventId).apply();
}
```

5. **Add `fetchPendingEvents()` method** — HTTP GET to `/api/ai/events/pending?after=xxx`, parse response, dedup + postEventNotification for each, update cursor.

6. **Call in `NativeEventListener.onOpen()`** after `startWsPingLoop()`:

```java
String serverUrl = getSharedPreferences(PREFS_NAME, MODE_PRIVATE)
        .getString(KEY_SERVER_URL, "");
if (!serverUrl.isEmpty()) {
    networkExecutor.execute(() -> fetchPendingEvents(serverUrl));
}
```

**Commit:** `feat(android): add event dedup and fetch pending events on WS reconnect`

---

### Task 8: Frontend — Fetch pending events on WS reconnect (post-replay)

**Files:**
- Modify: `web/src/composables/useGlobalEvents.ts`

**Key changes:**

1. **Add `LAST_SEEN_KEY` and `fetchPendingEvents()` function**

2. **Call fetchPendingEvents AFTER WS replay completes** — the WS handler in `events.go` replays buffered events synchronously during `Subscribe`. By the time `ws.onopen` fires on the client, the replay has been received. So calling `fetchPendingEvents()` in `ws.onopen` is safe — the `processedEventIds` set already contains replayed events, and the pending fetch cursor (`after=last_seen_event_id`) will be past the replayed events since the cursor was updated by the replay.

3. **Update cursor on every WS event** — but debounce: batch-write at most once per 5 seconds or on `ws.onclose`/`visibilitychange`.

```typescript
const LAST_SEEN_KEY = 'clawbench_last_seen_event_id'
let cursorDirty = false
let cursorTimer: ReturnType<typeof setTimeout> | null = null

function updateCursor(eventId: string) {
    cursorDirty = true
    localStorage.setItem(LAST_SEEN_KEY, eventId)
    if (!cursorTimer) {
        cursorTimer = setTimeout(() => { cursorTimer = null }, 5000)
    }
}
```

4. **Dedup already exists** via `processedEventIds` Set — pending events fetch uses same dedup.

**Commit:** `feat(frontend): fetch pending events on WS reconnect for offline notification recovery`

---

### Task 9: Update push-notifications spec document

**Files:**
- Modify: `docs/spec/features/push-notifications.md`

Add section describing offline event persistence with the revised design (global log, expires_at, conditional storage, write-ahead).

**Commit:** `docs: add offline event persistence section to push-notifications spec`

---

### Task 10: End-to-end verification

- Run `go test ./internal/service/ ./internal/handler/ ./internal/ws/ -v -count=1`
- Run `npm run typecheck` (frontend)
- Run `go build ./cmd/server`
- Run `npm run build` (frontend)
- Android build if JDK available

**Commit fixes if needed.**
