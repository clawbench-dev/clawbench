package service

import (
	"database/sql"
	"testing"
	"time"

	"clawbench/internal/ws"

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
	origDB := DB
	origDBRead := DBRead
	DB = db
	DBRead = db
	defer func() { DB = origDB; DBRead = origDBRead }()

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

func TestGetPendingEvents(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	origDB := DB
	origDBRead := DBRead
	DB = db
	DBRead = db
	defer func() { DB = origDB; DBRead = origDBRead }()

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

func TestGetPendingEventsAfterCursor(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	origDB := DB
	origDBRead := DBRead
	DB = db
	DBRead = db
	defer func() { DB = origDB; DBRead = origDBRead }()

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

func TestCleanupPendingEvents(t *testing.T) {
	db, teardown := setupTestDBForPendingEvents(t)
	defer teardown()
	origDB := DB
	origDBRead := DBRead
	DB = db
	DBRead = db
	defer func() { DB = origDB; DBRead = origDBRead }()

	// Insert event with past expires_at (expired)
	db.Exec(`INSERT INTO pending_events (event_id, event_type, payload, expires_at, created_at) VALUES ('evt_1','session_update','{}',datetime('now','-1 hour'),datetime('now','-25 hours'))`)

	CleanupPendingEvents()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM pending_events").Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 after cleanup, got %d", count)
	}
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
