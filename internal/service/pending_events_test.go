package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"clawbench/internal/ws"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// setupTestDBForPendingEvents creates an in-memory SQLite with the pending_events table.
func setupTestDBForPendingEvents(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
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
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db, func() { db.Close() }
}

func TestPendingEventsTableCreated(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()

	var hasTable int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='pending_events'").Scan(&hasTable)
	if err != nil {
		t.Fatal(err)
	}
	if hasTable != 1 {
		t.Fatal("pending_events table not found")
	}

	var hasIndex int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_pending_event_id'").Scan(&hasIndex)
	if err != nil {
		t.Fatal(err)
	}
	if hasIndex != 1 {
		t.Fatal("idx_pending_event_id index not found")
	}
}

func TestStorePendingEvent(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	err := StorePendingEvent("evt_1", "session_update", `{"status":"completed"}`, expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1, got %d", count)
	}
}

func TestStorePendingEventNilDB(t *testing.T) {
	cleanup := SetDBForTest(nil, nil)
	defer cleanup()

	// Should return nil without panic when db is nil
	err := StorePendingEvent("evt_1", "session_update", `{}`, "2026-01-01T00:00:00Z")
	assert.Nil(t, err)
}

func TestStorePendingEventDuplicate(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	err := StorePendingEvent("evt_dup", "session_update", `{}`, expiresAt)
	require.NoError(t, err)

	// INSERT OR IGNORE should silently skip duplicates
	err = StorePendingEvent("evt_dup", "session_update", `{}`, expiresAt)
	require.NoError(t, err)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 1, count)
}

func TestDeletePendingEvent(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	err := StorePendingEvent("evt_del_1", "session_update", `{"status":"completed"}`, expiresAt)
	require.NoError(t, err)
	err = StorePendingEvent("evt_del_2", "task_update", `{"status":"failed"}`, expiresAt)
	require.NoError(t, err)

	// Delete one event
	err = DeletePendingEvent("evt_del_1")
	require.NoError(t, err)

	// Verify only the other event remains
	events, err := GetPendingEvents("")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "evt_del_2", events[0].EventID)
}

func TestDeletePendingEventNilDB(t *testing.T) {
	cleanup := SetDBForTest(nil, nil)
	defer cleanup()

	err := DeletePendingEvent("evt_1")
	assert.Nil(t, err)
}

func TestGetPendingEvents(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	StorePendingEvent("evt_10", "session_update", `{"status":"completed"}`, expiresAt)
	StorePendingEvent("evt_20", "task_update", `{"status":"failed"}`, expiresAt)

	events, err := GetPendingEvents("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2, got %d", len(events))
	}
}

func TestGetPendingEventsNilDB(t *testing.T) {
	cleanup := SetDBForTest(nil, nil)
	defer cleanup()

	events, err := GetPendingEvents("")
	assert.Nil(t, err)
	assert.Nil(t, events)
}

func TestGetPendingEventsAfterCursor(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	StorePendingEvent("evt_10", "session_update", `{}`, expiresAt)
	StorePendingEvent("evt_20", "task_update", `{}`, expiresAt)

	events, _ := GetPendingEvents("evt_10")
	if len(events) != 1 {
		t.Fatalf("expected 1 after cursor, got %d", len(events))
	}
	if events[0].EventID != "evt_20" {
		t.Fatalf("expected evt_20, got %s", events[0].EventID)
	}
}

func TestGetPendingEventsExpiredCursor(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	StorePendingEvent("evt_20", "session_update", `{}`, expiresAt)

	// Query with a cursor that doesn't exist (expired and cleaned up)
	// Should return empty slice, not all events
	events, err := GetPendingEvents("evt_10_gone")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 for expired cursor, got %d", len(events))
	}
}

func TestGetPendingEventsFiltersExpired(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	// Insert one expired, one not expired
	_, err := db.Exec(`INSERT INTO pending_events (event_id, event_type, payload, expires_at) VALUES ('evt_expired','session_update','{}',datetime('now','-1 hour'))`)
	require.NoError(t, err)

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	err = StorePendingEvent("evt_active", "session_update", `{}`, expiresAt)
	require.NoError(t, err)

	events, err := GetPendingEvents("")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "evt_active", events[0].EventID)
}

func TestGetPendingEventsCursorAtLastEvent(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	StorePendingEvent("evt_10", "session_update", `{}`, expiresAt)
	StorePendingEvent("evt_20", "task_update", `{}`, expiresAt)

	// Cursor at last event should return empty
	events, err := GetPendingEvents("evt_20")
	require.NoError(t, err)
	assert.Empty(t, events)
}

func TestGetPendingEventsCursorReturnsNonExpiredAfterCursor(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	StorePendingEvent("evt_10", "session_update", `{}`, expiresAt)

	// Insert an expired event after the cursor
	_, err := db.Exec(`INSERT INTO pending_events (event_id, event_type, payload, expires_at, created_at) VALUES ('evt_expired','task_update','{}',datetime('now','-1 hour'),datetime('now'))`)
	require.NoError(t, err)

	// Insert a non-expired event after the expired one
	StorePendingEvent("evt_30", "task_update", `{}`, expiresAt)

	events, err := GetPendingEvents("evt_10")
	require.NoError(t, err)
	// Should only return non-expired events after cursor
	require.Len(t, events, 1)
	assert.Equal(t, "evt_30", events[0].EventID)
}

func TestCleanupPendingEvents(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	// Insert event with past expires_at (expired)
	db.Exec(`INSERT INTO pending_events (event_id, event_type, payload, expires_at, created_at) VALUES ('evt_1','session_update','{}',datetime('now','-1 hour'),datetime('now','-25 hours'))`)

	CleanupPendingEvents()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 after cleanup, got %d", count)
	}
}

func TestCleanupPendingEventsNilDB(t *testing.T) {
	cleanup := SetDBForTest(nil, nil)
	defer cleanup()

	// Should not panic
	CleanupPendingEvents()
}

func TestCleanupPendingEventsRowCapping(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	// Insert more than pendingEventMaxRows events
	for i := range pendingEventMaxRows + 50 {
		// Use different created_at to ensure deterministic ordering
		createdAt := time.Now().Add(-time.Duration(pendingEventMaxRows+50-i) * time.Second).UTC().Format(time.RFC3339)
		_, err := db.Exec(
			`INSERT INTO pending_events (event_id, event_type, payload, expires_at, created_at) VALUES (?, 'session_update', '{}', ?, ?)`,
			"evt_cap_"+string(rune(i)), expiresAt, createdAt,
		)
		require.NoError(t, err)
	}

	CleanupPendingEvents()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.LessOrEqual(t, count, pendingEventMaxRows)
}

func TestCleanupPendingEventsKeepsNonExpired(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	err := StorePendingEvent("evt_active", "session_update", `{}`, expiresAt)
	require.NoError(t, err)

	// Insert expired event
	_, err = db.Exec(`INSERT INTO pending_events (event_id, event_type, payload, expires_at, created_at) VALUES ('evt_expired','session_update','{}',datetime('now','-1 hour'),datetime('now','-25 hours'))`)
	require.NoError(t, err)

	CleanupPendingEvents()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 1, count)

	var eventID string
	db.QueryRow("SELECT event_id FROM pending_events").Scan(&eventID)
	assert.Equal(t, "evt_active", eventID)
}

func TestIsNotifiableEvent(t *testing.T) {
	tests := []struct {
		event  string
		data   any
		expect bool
	}{
		{"session_update", &ws.SessionUpdateData{Status: "completed"}, true},
		{"session_update", &ws.SessionUpdateData{Status: "cancelled"}, true},
		{"session_update", &ws.SessionUpdateData{Status: "permission_pending"}, true},
		{"session_update", &ws.SessionUpdateData{Status: "running"}, false},
		{"task_update", &ws.TaskUpdateData{Status: "completed"}, true},
		{"task_update", &ws.TaskUpdateData{Status: "failed"}, true},
		{"task_update", &ws.TaskUpdateData{Status: "cancelled"}, true},
		{"task_update", &ws.TaskUpdateData{Status: "running"}, false},
		{"summary_update", &ws.SummaryUpdateData{}, false},
		{"session_update", map[string]any{"status": "completed"}, true},
		{"session_update", map[string]any{"status": "running"}, false},
		// Edge cases
		{"session_update", nil, false},
		{"task_update", nil, false},
		{"unknown_event", &ws.SessionUpdateData{Status: "completed"}, false},
		{"unknown_event", map[string]any{"status": "completed"}, false},
		{"session_update", map[string]any{"status": 123}, false},                 // non-string status
		{"session_update", map[string]any{}, false},                              // missing status key
		{"task_update", map[string]any{"status": "completed"}, true},             // map with task_update
		{"task_update", map[string]any{"status": "failed"}, true},                // task_update + failed via map
		{"task_update", map[string]any{"status": "cancelled"}, true},             // task_update + cancelled via map
		{"task_update", map[string]any{"status": "running"}, false},              // task_update + running via map
		{"session_update", map[string]any{"status": "cancelled"}, true},          // session_update + cancelled via map
		{"session_update", map[string]any{"status": "permission_pending"}, true}, // session_update + permission_pending via map
		{"session_update", 42, false},                                            // non-matching type
		{"session_update", "some_string", false},                                 // string type
	}
	for _, tt := range tests {
		got := IsNotifiableEvent(tt.event, tt.data)
		if got != tt.expect {
			t.Errorf("IsNotifiableEvent(%q, %v) = %v, want %v", tt.event, tt.data, got, tt.expect)
		}
	}
}

func TestPendingEventExpiresAt(t *testing.T) {
	// permission_pending should get 7-day TTL
	ppExpiry := pendingEventExpiresAt("session_update", "permission_pending")
	ppTime, _ := time.Parse(time.RFC3339, ppExpiry)
	ppDiff := time.Until(ppTime)
	if ppDiff < 6*24*time.Hour || ppDiff > 8*24*time.Hour {
		t.Fatalf("permission_pending expiry should be ~7 days, got %v", ppDiff)
	}

	// completed should get 24h TTL
	compExpiry := pendingEventExpiresAt("session_update", "completed")
	compTime, _ := time.Parse(time.RFC3339, compExpiry)
	compDiff := time.Until(compTime)
	if compDiff < 23*time.Hour || compDiff > 25*time.Hour {
		t.Fatalf("completed expiry should be ~24h, got %v", compDiff)
	}
}

func TestPendingEventExpiresAtNonPermPend(t *testing.T) {
	// task_update with any status gets 24h TTL
	expiry := pendingEventExpiresAt("task_update", "completed")
	tm, _ := time.Parse(time.RFC3339, expiry)
	diff := time.Until(tm)
	assert.True(t, diff > 23*time.Hour && diff < 25*time.Hour, "task_update expiry should be ~24h, got %v", diff)

	// session_update with non-permission_pending status gets 24h
	expiry = pendingEventExpiresAt("session_update", "cancelled")
	tm, _ = time.Parse(time.RFC3339, expiry)
	diff = time.Until(tm)
	assert.True(t, diff > 23*time.Hour && diff < 25*time.Hour, "session_update+cancelled expiry should be ~24h, got %v", diff)
}

func TestIsNotifiableEvent_UserMessage(t *testing.T) {
	// chat_stream + user_message (value type — what StreamHub.Emit constructs)
	assert.True(t, IsNotifiableEvent("chat_stream", ws.ChatStreamData{EventType: "user_message", SessionID: "s1"}))
	// pointer variant for compatibility
	assert.True(t, IsNotifiableEvent("chat_stream", &ws.ChatStreamData{EventType: "user_message", SessionID: "s1"}))
}

func TestIsNotifiableEvent_NonUserMessage(t *testing.T) {
	assert.False(t, IsNotifiableEvent("chat_stream", ws.ChatStreamData{EventType: "content"}))
	assert.False(t, IsNotifiableEvent("chat_stream", ws.ChatStreamData{EventType: "thinking"}))
	assert.False(t, IsNotifiableEvent("chat_stream", ws.ChatStreamData{EventType: "tool_use"}))
	assert.False(t, IsNotifiableEvent("chat_stream", ws.ChatStreamData{EventType: "stream_start"}))
	assert.False(t, IsNotifiableEvent("chat_stream", ws.ChatStreamData{EventType: "queue_drain"}))
	assert.False(t, IsNotifiableEvent("chat_stream", &ws.ChatStreamData{EventType: "content"}))
}

func TestStoreNotifiableEvent_UserMessage_Disconnected(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	// No WS subscriptions → HasDisconnectedClients returns true → should store
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_um_1",
		Event: "chat_stream",
		Data: ws.ChatStreamData{
			SessionID: "sess_1",
			EventType: "user_message",
			Payload: map[string]any{
				"messageId": int64(42),
				"content":   "hello",
				"queueId":   "q-1",
			},
		},
	}

	StoreNotifiableEvent(msg)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 1, count)

	// Payload must be complete JSON containing messageId/queueId for offline recovery
	var eventType, payload string
	db.QueryRow("SELECT event_type, payload FROM pending_events").Scan(&eventType, &payload)
	assert.Equal(t, "chat_stream", eventType)
	var parsed ws.ServerMessage
	require.NoError(t, json.Unmarshal([]byte(payload), &parsed))
	data, ok := parsed.Data.(map[string]any)
	require.True(t, ok, "expected ChatStreamData serialized as JSON object")
	assert.Equal(t, "user_message", data["event_type"])
	inner, _ := data["payload"].(map[string]any)
	require.NotNil(t, inner)
	assert.Equal(t, float64(42), inner["messageId"])
	assert.Equal(t, "hello", inner["content"])
	assert.Equal(t, "q-1", inner["queueId"])
}

func TestStoreNotifiableEvent_UserMessage_AllConnected(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	// Subscribe a real connected client so HasDisconnectedClients returns false
	connected := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		mgr.Subscribe(conn, &wmu, "connected-client", "")
		// Subscribe to the session under test: storage is skipped only when
		// every connected client is actually watching THIS session.
		mgr.StreamHub().Subscribe("connected-client", "sess_1")
		close(connected)
		time.Sleep(2 * time.Second)
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for connected client")
	}

	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_um_2",
		Event: "chat_stream",
		Data:  ws.ChatStreamData{SessionID: "sess_1", EventType: "user_message", Payload: map[string]any{"messageId": int64(1)}},
	}

	StoreNotifiableEvent(msg)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 0, count, "should not store user_message when all clients are connected")
}

func TestPendingEventExpiresAt_UserMessage(t *testing.T) {
	// chat_stream + empty status → 24h default TTL (no special branch)
	expiry := pendingEventExpiresAt("chat_stream", "")
	tm, err := time.Parse(time.RFC3339, expiry)
	require.NoError(t, err)
	diff := time.Until(tm)
	assert.True(t, diff > 23*time.Hour && diff < 25*time.Hour, "user_message expiry should be ~24h, got %v", diff)
}

func TestStoreNotifiableEventUserMessageExpiresAt(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_um_ttl",
		Event: "chat_stream",
		Data:  ws.ChatStreamData{SessionID: "s1", EventType: "user_message", Payload: map[string]any{"messageId": int64(1)}},
	}

	StoreNotifiableEvent(msg)

	var expiresAt string
	db.QueryRow("SELECT expires_at FROM pending_events").Scan(&expiresAt)
	tm, err := time.Parse(time.RFC3339, expiresAt)
	require.NoError(t, err)
	diff := time.Until(tm)
	assert.True(t, diff > 23*time.Hour && diff < 25*time.Hour, "stored user_message expiry should be ~24h, got %v", diff)
}

func TestGetPendingEvents_ReturnsUserMessage(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, StorePendingEvent("evt_10", "session_update", `{"status":"completed"}`, expiresAt))
	require.NoError(t, StorePendingEvent("evt_um", "chat_stream", `{"event":"chat_stream","data":{"event_type":"user_message"}}`, expiresAt))

	events, err := GetPendingEvents("evt_10")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "evt_um", events[0].EventID)
	assert.Equal(t, "chat_stream", events[0].EventType)
}

func TestStoreNotifiableEvent(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	// Set up a WS manager with no subscriptions → HasDisconnectedClients returns true
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_notif_1",
		Event: "session_update",
		Data:  &ws.SessionUpdateData{Status: "completed", SessionID: "sess_1"},
	}

	StoreNotifiableEvent(msg)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 1, count)

	var payload string
	db.QueryRow("SELECT payload FROM pending_events").Scan(&payload)
	// Verify payload is valid JSON containing the event
	assert.True(t, json.Valid([]byte(payload)))
}

func TestStoreNotifiableEventNotNotifiable(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	// Running status is not notifiable → should not store
	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_notif_2",
		Event: "session_update",
		Data:  &ws.SessionUpdateData{Status: "running"},
	}

	StoreNotifiableEvent(msg)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 0, count)
}

func TestStoreNotifiableEventNoDisconnectedClients(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	// Set up a WS manager with nil → GetManager returns nil → skip disconnected check
	ws.SetManagerForTest(nil)

	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_notif_3",
		Event: "session_update",
		Data:  &ws.SessionUpdateData{Status: "completed"},
	}

	StoreNotifiableEvent(msg)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	// With nil manager, the mgr != nil check fails, so we skip HasDisconnectedClients
	// and proceed to store. This is the intended behavior.
	assert.Equal(t, 1, count)
}

func TestStoreNotifiableEventWithTaskUpdateData(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_task_1",
		Event: "task_update",
		Data:  &ws.TaskUpdateData{Status: "failed", TaskID: "task_1"},
	}

	StoreNotifiableEvent(msg)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 1, count)

	// Verify the expires_at is ~24h (not 7-day)
	var expiresAt string
	db.QueryRow("SELECT expires_at FROM pending_events").Scan(&expiresAt)
	tm, err := time.Parse(time.RFC3339, expiresAt)
	require.NoError(t, err)
	diff := time.Until(tm)
	assert.True(t, diff > 23*time.Hour && diff < 25*time.Hour, "task_update+failed expiry should be ~24h, got %v", diff)
}

func TestStoreNotifiableEventWithMapData(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_map_1",
		Event: "session_update",
		Data:  map[string]any{"status": "completed", "session_id": "sess_1"},
	}

	StoreNotifiableEvent(msg)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 1, count)
}

func TestStoreNotifiableEventPermPendTTL(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_perm_1",
		Event: "session_update",
		Data:  &ws.SessionUpdateData{Status: "permission_pending", SessionID: "sess_1"},
	}

	StoreNotifiableEvent(msg)

	var expiresAt string
	db.QueryRow("SELECT expires_at FROM pending_events").Scan(&expiresAt)
	tm, err := time.Parse(time.RFC3339, expiresAt)
	require.NoError(t, err)
	diff := time.Until(tm)
	assert.True(t, diff > 6*24*time.Hour && diff < 8*24*time.Hour, "permission_pending expiry should be ~7 days, got %v", diff)
}

func TestStoreNotifiableEventWithMapPermPend(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_map_perm",
		Event: "session_update",
		Data:  map[string]any{"status": "permission_pending", "session_id": "sess_1"},
	}

	StoreNotifiableEvent(msg)

	var expiresAt string
	db.QueryRow("SELECT expires_at FROM pending_events").Scan(&expiresAt)
	tm, err := time.Parse(time.RFC3339, expiresAt)
	require.NoError(t, err)
	diff := time.Until(tm)
	assert.True(t, diff > 6*24*time.Hour && diff < 8*24*time.Hour, "map+permission_pending expiry should be ~7 days, got %v", diff)
}

func TestStoreNotifiableEventUnknownDataType(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	// Data type that doesn't match any switch case in IsNotifiableEvent → returns false
	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_unknown",
		Event: "session_update",
		Data:  "not_a_valid_type",
	}

	StoreNotifiableEvent(msg)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 0, count)
}

func TestStoreNotifiableEventMapWithNonStringStatus(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	// Map with non-string status → IsNotifiableEvent returns false
	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_nonstr",
		Event: "session_update",
		Data:  map[string]any{"status": 123},
	}

	StoreNotifiableEvent(msg)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 0, count)
}

func TestStoreNotifiableEventCancelled(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_cancelled",
		Event: "session_update",
		Data:  &ws.SessionUpdateData{Status: "cancelled"},
	}

	StoreNotifiableEvent(msg)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 1, count)
}

func TestStoreNotifiableEventAllClientsConnected(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	// Subscribe a connected client via a real WS server so HasDisconnectedClients returns false
	connected := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		mgr.Subscribe(conn, &wmu, "connected-client", "")
		close(connected)
		time.Sleep(2 * time.Second)
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)

	// Wait for the server to register the subscription
	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for connected client")
	}

	// The connected client is watching "sess_watched", so an event for that
	// session has no unreached audience and is not stored.
	mgr.StreamHub().Subscribe("connected-client", "sess_watched")

	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_connected",
		Event: "session_update",
		Data:  &ws.SessionUpdateData{SessionID: "sess_watched", Status: "completed"},
	}

	StoreNotifiableEvent(msg)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 0, count, "should not store when all clients are connected")
}

func TestStoreNotifiableEventMarshalError(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	// Data that cannot be marshaled to JSON (e.g., channel)
	// Use map[string]any which passes IsNotifiableEvent but put unmarshallable value inside.
	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_marshal_err",
		Event: "session_update",
		Data:  map[string]any{"status": "completed", "unmarshallable": make(chan int)},
	}

	// IsNotifiableEvent returns true (map with status="completed")
	// but json.Marshal will fail due to channel value
	StoreNotifiableEvent(msg)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	assert.Equal(t, 0, count, "should not store when json.Marshal fails")
}

func TestGetPendingEventsDBError(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()

	// Set up DB, then close it to cause query errors
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	StorePendingEvent("evt_10", "session_update", `{}`, expiresAt)

	// Close the DB to cause subsequent query errors
	db.Close()

	// GetPendingEvents with cursor should return error from cursor check
	events, err := GetPendingEvents("evt_10")
	// After DB close, we expect an error or nil result
	assert.Error(t, err)
	assert.Nil(t, events)
}

func TestGetPendingEventsNoCursorDBError(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()

	cleanup := SetDBForTest(db, db)
	defer cleanup()

	// Close the DB to cause query errors
	db.Close()

	events, err := GetPendingEvents("")
	assert.Error(t, err)
	assert.Nil(t, events)
}

func TestCleanupPendingEventsDBError(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()

	cleanup := SetDBForTest(db, db)
	defer cleanup()

	// Close the DB to cause DELETE errors
	db.Close()

	// Should not panic, just log warnings
	CleanupPendingEvents()
}

func TestStoreNotifiableEventStoreError(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()

	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	// Close the DB so StorePendingEvent will fail
	db.Close()

	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_store_err",
		Event: "session_update",
		Data:  &ws.SessionUpdateData{Status: "completed"},
	}

	// Should not panic, just log warning
	StoreNotifiableEvent(msg)
}

// setupReadGateDB builds an in-memory DB with the three tables the read gate
// touches: pending_events, chat_sessions and chat_history. The read-state
// helpers query chat_sessions/chat_history by session id, so those must exist
// even though only pending_events is directly under test.
func setupReadGateDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS pending_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id TEXT NOT NULL UNIQUE,
			event_type TEXT NOT NULL,
			payload TEXT NOT NULL,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL DEFAULT 'claude',
			title TEXT NOT NULL DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0,
			last_read_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			streaming INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL,
			session_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'running',
			read_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)
	return db, func() { db.Close() }
}

// sessionEventPayload builds a stored session_update payload for a session.
func sessionEventPayload(t *testing.T, sessionID, status string) string {
	t.Helper()
	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_payload",
		Event: "session_update",
		Data:  &ws.SessionUpdateData{SessionID: sessionID, Status: status},
	}
	raw, err := json.Marshal(msg)
	require.NoError(t, err)
	return string(raw)
}

// taskEventPayload builds a stored task_update payload. executionID is written
// as a string, matching what scheduler.go emits.
func taskEventPayload(t *testing.T, executionID, status string) string {
	t.Helper()
	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_payload",
		Event: "task_update",
		Data:  &ws.TaskUpdateData{TaskID: "1", ExecutionID: executionID, Status: status},
	}
	raw, err := json.Marshal(msg)
	require.NoError(t, err)
	return string(raw)
}

// TestGetPendingEvents_ReadSessionSuppressed is the regression guard for the
// reported bug: a reply the user already read must not re-notify on recovery.
func TestGetPendingEvents_ReadSessionSuppressed(t *testing.T) {
	db, teardown := setupReadGateDB(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	// Session read at "now"; the reply landed before that, so it is read.
	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_path, last_read_at) VALUES ('s_read', '/p', datetime('now'))`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chat_history (project_path, role, content, session_id, streaming, completed_at)
		VALUES ('/p', 'assistant', 'reply', 's_read', 0, datetime('now','-1 hour'))`)
	require.NoError(t, err)

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, StorePendingEvent("evt_read", "session_update", sessionEventPayload(t, "s_read", "completed"), expiresAt))

	events, err := GetPendingEvents("")
	require.NoError(t, err)
	require.Len(t, events, 1, "a read event must still be returned so the cursor can advance past it")
	assert.True(t, events[0].SuppressNotification, "an already-read completion must suppress its notification")
}

// TestGetPendingEvents_UnreadSessionNotifies is the converse: an unread reply
// must still notify, or the fix would silence real completions.
func TestGetPendingEvents_UnreadSessionNotifies(t *testing.T) {
	db, teardown := setupReadGateDB(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	// last_read_at is before the reply, so the reply is unread.
	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_path, last_read_at) VALUES ('s_unread', '/p', datetime('now','-2 hours'))`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chat_history (project_path, role, content, session_id, streaming, completed_at)
		VALUES ('/p', 'assistant', 'reply', 's_unread', 0, datetime('now','-1 hour'))`)
	require.NoError(t, err)

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, StorePendingEvent("evt_unread", "session_update", sessionEventPayload(t, "s_unread", "completed"), expiresAt))

	events, err := GetPendingEvents("")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.False(t, events[0].SuppressNotification, "an unread completion must keep notifying")
}

// TestGetPendingEvents_NeverReadSessionNotifies: last_read_at IS NULL means the
// user never opened the session, so its completion must notify.
func TestGetPendingEvents_NeverReadSessionNotifies(t *testing.T) {
	db, teardown := setupReadGateDB(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_path) VALUES ('s_new', '/p')`)
	require.NoError(t, err)

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, StorePendingEvent("evt_new", "session_update", sessionEventPayload(t, "s_new", "completed"), expiresAt))

	events, err := GetPendingEvents("")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.False(t, events[0].SuppressNotification, "a never-read session must notify")
}

// TestGetPendingEvents_PermissionPendingNeverSuppressed: an approval request is
// not a reply to read; it must keep notifying until answered.
func TestGetPendingEvents_PermissionPendingNeverSuppressed(t *testing.T) {
	db, teardown := setupReadGateDB(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	// Fully read session — but permission_pending is exempt from the gate.
	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_path, last_read_at) VALUES ('s_perm', '/p', datetime('now'))`)
	require.NoError(t, err)

	expiresAt := time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, StorePendingEvent("evt_perm", "session_update", sessionEventPayload(t, "s_perm", "permission_pending"), expiresAt))

	events, err := GetPendingEvents("")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.False(t, events[0].SuppressNotification,
		"permission_pending is an approval request, not a read reply — it must keep notifying")
}

// TestGetPendingEvents_ReadExecutionSuppressed covers the task family, whose
// read state lives in task_executions.read_at rather than the session.
func TestGetPendingEvents_ReadExecutionSuppressed(t *testing.T) {
	db, teardown := setupReadGateDB(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	_, err := db.Exec(`INSERT INTO task_executions (id, task_id, session_id, status, read_at)
		VALUES (7, 1, 's_task', 'completed', datetime('now'))`)
	require.NoError(t, err)

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, StorePendingEvent("evt_task", "task_update", taskEventPayload(t, "7", "completed"), expiresAt))

	events, err := GetPendingEvents("")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.True(t, events[0].SuppressNotification, "a read task execution must suppress its notification")
}

// TestGetPendingEvents_UnreadExecutionNotifies is the task-side converse.
func TestGetPendingEvents_UnreadExecutionNotifies(t *testing.T) {
	db, teardown := setupReadGateDB(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	_, err := db.Exec(`INSERT INTO task_executions (id, task_id, session_id, status) VALUES (8, 1, 's_task', 'completed')`)
	require.NoError(t, err)

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, StorePendingEvent("evt_task_unread", "task_update", taskEventPayload(t, "8", "completed"), expiresAt))

	events, err := GetPendingEvents("")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.False(t, events[0].SuppressNotification, "an unread task execution must notify")
}

// TestGetPendingEvents_CursorAdvancesPastSuppressedEvent proves the suppressed
// event still participates in cursor paging: fetching "after" a suppressed event
// must move past it rather than returning it forever.
func TestGetPendingEvents_CursorAdvancesPastSuppressedEvent(t *testing.T) {
	db, teardown := setupReadGateDB(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_path, last_read_at) VALUES ('s_read2', '/p', datetime('now'))`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chat_history (project_path, role, content, session_id, streaming, completed_at)
		VALUES ('/p', 'assistant', 'reply', 's_read2', 0, datetime('now','-1 hour'))`)
	require.NoError(t, err)

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, StorePendingEvent("evt_a", "session_update", sessionEventPayload(t, "s_read2", "completed"), expiresAt))
	require.NoError(t, StorePendingEvent("evt_b", "session_update", sessionEventPayload(t, "s_read2", "completed"), expiresAt))

	// Both suppressed...
	events, err := GetPendingEvents("")
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.True(t, events[0].SuppressNotification)
	assert.True(t, events[1].SuppressNotification)

	// ...and the cursor can advance past the first, leaving only the second.
	events, err = GetPendingEvents("evt_a")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "evt_b", events[0].EventID)
}

// TestGetPendingEvents_UserMessageNotReadGated: chat_stream user_message has no
// read model, so it must never be suppressed by this gate.
func TestGetPendingEvents_UserMessageNotReadGated(t *testing.T) {
	db, teardown := setupReadGateDB(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_path, last_read_at) VALUES ('s_um', '/p', datetime('now'))`)
	require.NoError(t, err)

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, StorePendingEvent("evt_um_gate", "chat_stream",
		`{"event":"chat_stream","data":{"session_id":"s_um","event_type":"user_message"}}`, expiresAt))

	events, err := GetPendingEvents("")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.False(t, events[0].SuppressNotification, "user_message has no read state and must not be gated")
}

// TestGetPendingEvents_MalformedPayloadNotSuppressed: a payload that cannot be
// parsed must fall back to notifying (pre-existing behavior) rather than being
// silently dropped.
func TestGetPendingEvents_MalformedPayloadNotSuppressed(t *testing.T) {
	db, teardown := setupReadGateDB(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, StorePendingEvent("evt_bad", "session_update", `{not json`, expiresAt))

	events, err := GetPendingEvents("")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.False(t, events[0].SuppressNotification, "unparseable payload must default to notifying")
}

// TestGetPendingEvents_ReadGateMissingTablesIsNonFatal: the gate queries
// chat_sessions/chat_history. If those are unavailable the event must still be
// returned (notifying) rather than the whole fetch failing.
func TestGetPendingEvents_ReadGateMissingTablesIsNonFatal(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t) // only pending_events exists
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	require.NoError(t, StorePendingEvent("evt_no_tables", "session_update", sessionEventPayload(t, "s_x", "completed"), expiresAt))

	events, err := GetPendingEvents("")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.False(t, events[0].SuppressNotification, "a failed read-state lookup must not suppress the notification")
}

// TestAnyToString covers the execution_id renderings the lookup must match.
func TestAnyToString(t *testing.T) {
	assert.Equal(t, "7", anyToString("7"))
	assert.Equal(t, "7", anyToString(float64(7)))
	assert.Equal(t, "7.5", anyToString(float64(7.5)))
	assert.Equal(t, "", anyToString(nil))
	assert.Equal(t, "", anyToString(map[string]any{}))
}

// TestSQLPlaceholders pins the IN-clause builder used by both lookups.
func TestSQLPlaceholders(t *testing.T) {
	assert.Equal(t, "", sqlPlaceholders(0))
	assert.Equal(t, "?", sqlPlaceholders(1))
	assert.Equal(t, "?,?,?", sqlPlaceholders(3))
}

// TestStoreNotifiableEvent_ConnectedButNotSubscribed is the regression guard
// for a permanently lost cross-device message.
//
// The reported scenario: a browser has the target session OPEN (so a WS
// connection exists and is "connected"), but at the moment DingTalk sends, that
// client holds no StreamHub subscription for the session — e.g. a reconnect
// replaced the connection and the re-subscribe had not landed yet. The event is
// then dropped live (no_subscribers).
//
// Recovery must come from the write-ahead log. But StoreNotifiableEvent skips
// storage whenever no client is DISCONNECTED, and here a connected client
// exists — just not for this session. The event therefore vanished entirely:
// neither delivered nor stored, so the reconnect replay could not recover it,
// and the client rendered the assistant reply with no question above it.
//
// Storage must key on "can this session be delivered to anyone" rather than
// "is some unrelated client disconnected".
func TestStoreNotifiableEvent_ConnectedButNotSubscribed(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	cleanup := SetDBForTest(db, db)
	defer cleanup()

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	// A real connected client, subscribed to a DIFFERENT session — exactly the
	// "browser is open on another session" case.
	ready := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		mgr.Subscribe(conn, &wmu, "browser", "")
		mgr.StreamHub().Subscribe("browser", "some-other-session")
		close(ready)
		time.Sleep(3 * time.Second)
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := websocket.Dial(ctx, "ws"+server.URL[4:], nil)
	require.NoError(t, err)
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for connected client")
	}

	// Sanity: a client IS connected, so the old gate would skip storage.
	require.False(t, mgr.HasDisconnectedClients(),
		"precondition: a connected client exists, so the old gate skips storage")

	StoreNotifiableEvent(ws.ServerMessage{
		Type:  "event",
		ID:    "evt_unsubscribed",
		Event: "chat_stream",
		Data: ws.ChatStreamData{
			SessionID: "target-session",
			EventType: "user_message",
			Payload:   map[string]any{"messageId": int64(42), "content": "from dingtalk"},
		},
	})

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count))
	assert.Equal(t, 1, count,
		"a user_message for a session nobody is subscribed to must be persisted, "+
			"even though some unrelated client is connected")
}
