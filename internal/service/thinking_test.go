package service

import (
	"encoding/json"
	"testing"
)

func TestThinkingCRUD(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	sessionID := "thinking-sess-001"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Test Session")
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	t.Run("insert new thinking", func(t *testing.T) {
		if err := UpsertThinking(msgID, sessionID, "th_abc123", "thinking text"); err != nil {
			t.Fatalf("UpsertThinking: %v", err)
		}
		rec, err := GetThinking("th_abc123", msgID)
		if err != nil {
			t.Fatalf("GetThinking: %v", err)
		}
		if rec == nil {
			t.Fatal("GetThinking returned nil")
		}
		if rec.ThinkID != "th_abc123" || rec.Text != "thinking text" || rec.MessageID != msgID || rec.SessionID != sessionID {
			t.Errorf("record mismatch: %+v", rec)
		}
	})

	t.Run("upsert overwrites text", func(t *testing.T) {
		if err := UpsertThinking(msgID, sessionID, "th_abc123", "updated text"); err != nil {
			t.Fatalf("UpsertThinking: %v", err)
		}
		rec, _ := GetThinking("th_abc123", msgID)
		if rec.Text != "updated text" {
			t.Errorf("Text = %q, want updated text", rec.Text)
		}
	})

	t.Run("get missing returns nil", func(t *testing.T) {
		rec, err := GetThinking("th_missing", msgID)
		if err != nil || rec != nil {
			t.Errorf("expected nil,nil got %+v,%v", rec, err)
		}
	})

	t.Run("get by session fallback", func(t *testing.T) {
		rec, err := GetThinkingBySession("th_abc123", sessionID)
		if err != nil || rec == nil || rec.Text != "updated text" {
			t.Errorf("GetThinkingBySession failed: rec=%+v err=%v", rec, err)
		}
		rec2, err := GetThinkingBySession("th_abc123", "other-session")
		if err != nil || rec2 != nil {
			t.Errorf("expected nil for other session, got %+v,%v", rec2, err)
		}
	})

	t.Run("delete by message", func(t *testing.T) {
		if err := DeleteThinkingByMessage(msgID); err != nil {
			t.Fatalf("DeleteThinkingByMessage: %v", err)
		}
		rec, _ := GetThinking("th_abc123", msgID)
		if rec != nil {
			t.Error("expected nil after delete")
		}
	})
}

func TestGenerateThinkingID(t *testing.T) {
	a, b := generateThinkingID(), generateThinkingID()
	if a == "" || b == "" {
		t.Fatal("generateThinkingID returned empty")
	}
	if a == b {
		t.Error("two generated IDs should differ")
	}
}

func TestSlimThinkingInContent(t *testing.T) {
	t.Run("extracts thinking and keeps metadata", func(t *testing.T) {
		in := `{"blocks":[
			{"type":"text","text":"intro"},
			{"type":"thinking","text":"deep reasoning","done":true},
			{"type":"tool_use","id":"toolu_x","name":"Bash","done":true}
		],"metadata":{"model":"claude"}}`
		slim, records, err := slimThinkingInContent(in)
		if err != nil {
			t.Fatalf("slimThinkingInContent: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("records = %d, want 1", len(records))
		}
		if records[0].Text != "deep reasoning" || records[0].ThinkID == "" {
			t.Errorf("record mismatch: %+v", records[0])
		}
		var parsed struct {
			Blocks   []map[string]any `json:"blocks"`
			Metadata map[string]any   `json:"metadata"`
		}
		if err := json.Unmarshal([]byte(slim), &parsed); err != nil {
			t.Fatalf("unmarshal slim: %v", err)
		}
		if parsed.Blocks[1]["think_id"] != records[0].ThinkID {
			t.Errorf("think_id not in slim block: %v", parsed.Blocks[1])
		}
		if _, hasText := parsed.Blocks[1]["text"]; hasText {
			t.Error("slim block should not have text")
		}
		if parsed.Blocks[1]["done"] != true {
			t.Error("slim block should preserve done")
		}
		if parsed.Blocks[0]["text"] != "intro" {
			t.Error("text block should be untouched")
		}
		if parsed.Metadata["model"] != "claude" {
			t.Error("metadata should be preserved")
		}
	})

	t.Run("no thinking returns unchanged", func(t *testing.T) {
		in := `{"blocks":[{"type":"text","text":"hi"}]}`
		slim, records, err := slimThinkingInContent(in)
		if err != nil || len(records) != 0 || slim != in {
			t.Errorf("expected unchanged, got slim=%q records=%v err=%v", slim, records, err)
		}
	})

	t.Run("already slim thinking skipped", func(t *testing.T) {
		in := `{"blocks":[{"type":"thinking","think_id":"th_x","done":true}]}`
		slim, records, err := slimThinkingInContent(in)
		if err != nil || len(records) != 0 || slim != in {
			t.Errorf("expected unchanged, got slim=%q records=%v err=%v", slim, records, err)
		}
	})
}

func TestPersistThinkingToDB_ParseErrorFallback(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() { db.Close(); dbRead.Close() }()

	bad := "not json {"
	got := persistThinkingToDB(bad, 42, "sess-1")
	if got != bad {
		t.Errorf("expected original content back on parse error, got %q", got)
	}
}
