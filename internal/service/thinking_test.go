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

func TestAppendThinkingSegment_GetThinkingConcat(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	sessionID := "thinking-append-sess"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Test Session")
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	t.Run("append chunks then concat in seq order", func(t *testing.T) {
		if err := AppendThinkingSegment(msgID, sessionID, "th_app1", 0, "part1"); err != nil {
			t.Fatalf("append seq0: %v", err)
		}
		if err := AppendThinkingSegment(msgID, sessionID, "th_app1", 1, "part2"); err != nil {
			t.Fatalf("append seq1: %v", err)
		}
		if err := AppendThinkingSegment(msgID, sessionID, "th_app1", 2, "part3"); err != nil {
			t.Fatalf("append seq2: %v", err)
		}
		rec, err := GetThinking("th_app1", msgID)
		if err != nil {
			t.Fatalf("GetThinking: %v", err)
		}
		if rec == nil {
			t.Fatal("GetThinking returned nil")
		}
		if rec.Text != "part1part2part3" {
			t.Errorf("Text = %q, want concatenated part1part2part3", rec.Text)
		}
	})

	t.Run("upsert full text replaces chunks without duplication", func(t *testing.T) {
		if err := UpsertThinking(msgID, sessionID, "th_app1", "full replacement"); err != nil {
			t.Fatalf("UpsertThinking: %v", err)
		}
		rec, _ := GetThinking("th_app1", msgID)
		if rec.Text != "full replacement" {
			t.Errorf("Text = %q, want full replacement (no chunk duplication)", rec.Text)
		}
		// Only one row remains after the full-text upsert.
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE think_id = 'th_app1'").Scan(&count); err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 1 {
			t.Errorf("expected 1 row after UpsertThinking, got %d", count)
		}
	})

	t.Run("idempotent append same seq overwrites not duplicates", func(t *testing.T) {
		if err := AppendThinkingSegment(msgID, sessionID, "th_app2", 0, "alpha"); err != nil {
			t.Fatalf("append seq0: %v", err)
		}
		// Simulated retry of the same seq after a failure.
		if err := AppendThinkingSegment(msgID, sessionID, "th_app2", 0, "alpha"); err != nil {
			t.Fatalf("append seq0 retry: %v", err)
		}
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE think_id = 'th_app2'").Scan(&count); err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 1 {
			t.Errorf("expected 1 row after idempotent retry, got %d", count)
		}
	})
}

func TestGetThinkingBySessionAll(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	sessionID := "thinking-all-sess"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Test Session")
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	t.Run("returns empty for session with no thinking", func(t *testing.T) {
		records, err := GetThinkingBySessionAll(sessionID)
		if err != nil {
			t.Fatalf("GetThinkingBySessionAll: %v", err)
		}
		if len(records) != 0 {
			t.Errorf("expected 0 records, got %d", len(records))
		}
	})

	t.Run("returns all thinking records for session", func(t *testing.T) {
		if err := UpsertThinking(msgID, sessionID, "th_001", "first thought"); err != nil {
			t.Fatalf("UpsertThinking: %v", err)
		}
		if err := UpsertThinking(msgID, sessionID, "th_002", "second thought"); err != nil {
			t.Fatalf("UpsertThinking: %v", err)
		}
		records, err := GetThinkingBySessionAll(sessionID)
		if err != nil {
			t.Fatalf("GetThinkingBySessionAll: %v", err)
		}
		if len(records) != 2 {
			t.Fatalf("expected 2 records, got %d", len(records))
		}
		got := map[string]string{}
		for _, r := range records {
			got[r.ThinkID] = r.Text
		}
		if got["th_001"] != "first thought" || got["th_002"] != "second thought" {
			t.Errorf("mismatch: %+v", got)
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

	t.Run("slims empty-text thinking with no record", func(t *testing.T) {
		in := `{"blocks":[{"type":"thinking","done":true},{"type":"text","text":"hi"}]}`
		slim, records, err := slimThinkingInContent(in)
		if err != nil {
			t.Fatalf("slimThinkingInContent: %v", err)
		}
		if len(records) != 0 {
			t.Fatalf("records = %d, want 0", len(records))
		}
		if slim == in {
			t.Error("content should be rewritten (think_id added)")
		}
		var parsed struct {
			Blocks []map[string]any `json:"blocks"`
		}
		if err := json.Unmarshal([]byte(slim), &parsed); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if parsed.Blocks[0]["think_id"] == "" {
			t.Errorf("empty-text thinking block should get think_id: %v", parsed.Blocks[0])
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

func TestSlimThinkingInContent_PreservesParentToolCallID(t *testing.T) {
	// A sub-agent thinking block must keep its parent link through slimming, so
	// a reload can regroup it under the parent Agent card.
	in := `{"blocks":[
		{"type":"thinking","text":"child reasoning","done":true,"parent_tool_call_id":"call_p"}
	]}`
	slim, records, err := slimThinkingInContent(in)
	if err != nil {
		t.Fatalf("slimThinkingInContent: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	var parsed struct {
		Blocks []map[string]any `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(slim), &parsed); err != nil {
		t.Fatalf("unmarshal slim: %v", err)
	}
	if parsed.Blocks[0]["parent_tool_call_id"] != "call_p" {
		t.Errorf("parent_tool_call_id lost in slim block: %v", parsed.Blocks[0])
	}
	if _, hasText := parsed.Blocks[0]["text"]; hasText {
		t.Error("slim block should not have text")
	}
}
