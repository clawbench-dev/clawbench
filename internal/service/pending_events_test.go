package service

import (
	"database/sql"
	"testing"

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
