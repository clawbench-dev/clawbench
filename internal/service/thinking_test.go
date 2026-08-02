package service

import (
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
