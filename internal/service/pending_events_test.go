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
		{"queue_update", &ws.QueueUpdateData{}, false},
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

	// Now HasDisconnectedClients should return false → StoreNotifiableEvent returns early
	msg := ws.ServerMessage{
		Type:  "event",
		ID:    "evt_connected",
		Event: "session_update",
		Data:  &ws.SessionUpdateData{Status: "completed"},
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
