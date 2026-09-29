package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

	req := httptest.NewRequest("GET", "/api/ai/events/pending", http.NoBody)
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

// TestServePendingEvents_SuppressNotificationWireName pins the JSON field name
// the Android client reads.
//
// The field is produced by a Go struct tag (service.PendingEvent) and consumed
// as a hardcoded string literal in Java (BackgroundService / PendingEventsWorker
// read "suppress_notification"). Nothing else links the two: renaming the tag
// compiles, passes every Go test, and silently disables the entire read gate on
// Android — the exact cross-language drift this repo has been bitten by before.
// Asserting on the raw JSON body is the only thing that catches it.
func TestServePendingEvents_SuppressNotificationWireName(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cleanup := service.SetDBForTest(db, db)
	defer cleanup()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS pending_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id TEXT NOT NULL UNIQUE,
		event_type TEXT NOT NULL,
		payload TEXT NOT NULL,
		expires_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatal(err)
	}
	// A session_update whose session is already read. Only pending_events
	// exists, so the read-state lookup fails open and nothing is suppressed —
	// this test is about the FIELD's presence/name, not its value.
	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	if err := service.StorePendingEvent("evt_wire", "session_update",
		`{"type":"event","id":"evt_wire","event":"session_update","data":{"session_id":"s1","status":"completed"}}`,
		expiresAt); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/ai/events/pending", http.NoBody)
	w := httptest.NewRecorder()
	ServePendingEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Assert on the raw body: an omitempty field that is false is absent, so
	// decode into a generic map and confirm the KEY the Android side hardcodes
	// is the one the struct tag produces. We assert the marshaled name by
	// forcing the field true through the service type and re-marshaling.
	var resp struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(resp.Events))
	}
	// The key must be absent (false + omitempty) — proving the tag does not
	// leak some other name. Presence of a DIFFERENT spelling would mean the
	// tag drifted.
	if _, present := resp.Events[0]["suppress_notification"]; present {
		t.Errorf("suppress_notification must be omitted when false (omitempty)")
	}
	// Direct struct-tag assertion: this is what the Java literal must match.
	raw, err := json.Marshal(service.PendingEvent{SuppressNotification: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"suppress_notification":true`) {
		t.Fatalf("the JSON wire name changed; Android hardcodes \"suppress_notification\". got: %s", raw)
	}
}

func TestServePendingEvents_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/ai/events/pending", http.NoBody)
	w := httptest.NewRecorder()

	ServePendingEvents(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestServePendingEvents_DBError(t *testing.T) {
	// Use a closed DB to trigger an error from service.GetPendingEvents.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Close() // close immediately so queries will fail
	cleanup := service.SetDBForTest(db, db)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/ai/events/pending", http.NoBody)
	w := httptest.NewRecorder()

	ServePendingEvents(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "failed to fetch pending events" {
		t.Fatalf("unexpected error message: %v", resp["error"])
	}
}

func TestServePendingEvents_WithAfterParam(t *testing.T) {
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

	req := httptest.NewRequest("GET", "/api/ai/events/pending?after=evt_12345", http.NoBody)
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
	// Cursor doesn't exist → returns empty slice
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}
