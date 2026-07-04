package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/service"

	_ "modernc.org/sqlite"
)

func TestServePendingEvents_Empty(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cleanup := service.SetDBForTest(db, db)
	defer cleanup()

	db.Exec(`CREATE TABLE IF NOT EXISTS pending_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id TEXT NOT NULL UNIQUE,
		event_type TEXT NOT NULL,
		payload TEXT NOT NULL,
		expires_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)

	req := httptest.NewRequest("GET", "/api/ai/events/pending", nil)
	w := httptest.NewRecorder()

	ServePendingEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	events, ok := resp["events"].([]any)
	if !ok {
		t.Fatal("events not a slice")
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

func TestServePendingEvents_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/ai/events/pending", nil)
	w := httptest.NewRecorder()

	ServePendingEvents(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}
