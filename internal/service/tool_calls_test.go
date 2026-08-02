package service

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
)

func TestUpsertAndGetToolCall(t *testing.T) {
	// Use a test database
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	// Create a session and message first (FK dependency)
	sessionID := "test-session-001"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Test Session")

	var msgID int64
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ = res.LastInsertId()

	t.Run("insert new tool call", func(t *testing.T) {
		input := json.RawMessage(`{"file_path":"/src/main.go"}`)
		err := UpsertToolCall(msgID, sessionID, "toolu_01", "Read", input, "file contents...", "success", "main.go", true, 1250)
		if err != nil {
			t.Fatalf("UpsertToolCall: %v", err)
		}

		record, err := GetToolCall("toolu_01", msgID)
		if err != nil {
			t.Fatalf("GetToolCall: %v", err)
		}
		if record == nil {
			t.Fatal("GetToolCall returned nil")
		}
		if record.ToolID != "toolu_01" {
			t.Errorf("ToolID = %q, want %q", record.ToolID, "toolu_01")
		}
		if record.Name != "Read" {
			t.Errorf("Name = %q, want %q", record.Name, "Read")
		}
		if record.Output != "file contents..." {
			t.Errorf("Output = %q, want %q", record.Output, "file contents...")
		}
		if record.Status != "success" {
			t.Errorf("Status = %q, want %q", record.Status, "success")
		}
		if record.Summary != "main.go" {
			t.Errorf("Summary = %q, want %q", record.Summary, "main.go")
		}
		if !record.Done {
			t.Error("Done = false, want true")
		}
		if record.DurationMs != 1250 {
			t.Errorf("DurationMs = %d, want %d", record.DurationMs, 1250)
		}
	})

	t.Run("update existing tool call (UPSERT)", func(t *testing.T) {
		// Update with new input (merged) and output
		input := json.RawMessage(`{"file_path":"/src/main.go","description":"Read main file"}`)
		err := UpsertToolCall(msgID, sessionID, "toolu_01", "Read", input, "updated contents...", "success", "Read main file", true, 2400)
		if err != nil {
			t.Fatalf("UpsertToolCall: %v", err)
		}

		record, err := GetToolCall("toolu_01", msgID)
		if err != nil {
			t.Fatalf("GetToolCall: %v", err)
		}
		if record.Output != "updated contents..." {
			t.Errorf("Output = %q, want %q", record.Output, "updated contents...")
		}
		if record.Summary != "Read main file" {
			t.Errorf("Summary = %q, want %q", record.Summary, "Read main file")
		}
		if record.DurationMs != 2400 {
			t.Errorf("DurationMs = %d, want %d", record.DurationMs, 2400)
		}
	})

	t.Run("upsert with empty output preserves existing", func(t *testing.T) {
		// Simulate tool_use event (no output yet) after tool_result already set output
		input := json.RawMessage(`{"file_path":"/src/main.go"}`)
		err := UpsertToolCall(msgID, sessionID, "toolu_01", "Read", input, "", "success", "main.go", false, 0)
		if err != nil {
			t.Fatalf("UpsertToolCall: %v", err)
		}

		record, err := GetToolCall("toolu_01", msgID)
		if err != nil {
			t.Fatalf("GetToolCall: %v", err)
		}
		// Output should be preserved from previous upsert
		if record.Output != "updated contents..." {
			t.Errorf("Output = %q, want %q (preserved)", record.Output, "updated contents...")
		}
		// Duration should be preserved from previous upsert (empty duration never overwrites)
		if record.DurationMs != 2400 {
			t.Errorf("DurationMs = %d, want %d (preserved)", record.DurationMs, 2400)
		}
	})

	t.Run("get non-existent tool call returns nil", func(t *testing.T) {
		record, err := GetToolCall("toolu_99", msgID)
		if err != nil {
			t.Fatalf("GetToolCall: %v", err)
		}
		if record != nil {
			t.Error("expected nil for non-existent tool call")
		}
	})

	t.Run("get tool call with wrong message_id returns nil", func(t *testing.T) {
		record, err := GetToolCall("toolu_01", 99999)
		if err != nil {
			t.Fatalf("GetToolCall: %v", err)
		}
		if record != nil {
			t.Error("expected nil for wrong message_id")
		}
	})

	t.Run("GetToolCallBySession finds record by session_id", func(t *testing.T) {
		record, err := GetToolCallBySession("toolu_01", sessionID)
		if err != nil {
			t.Fatalf("GetToolCallBySession: %v", err)
		}
		if record == nil {
			t.Fatal("GetToolCallBySession returned nil")
		}
		if record.ToolID != "toolu_01" {
			t.Errorf("ToolID = %q, want %q", record.ToolID, "toolu_01")
		}
		if record.SessionID != sessionID {
			t.Errorf("SessionID = %q, want %q", record.SessionID, sessionID)
		}
	})

	t.Run("GetToolCallBySession returns nil for non-existent session", func(t *testing.T) {
		record, err := GetToolCallBySession("toolu_01", "nonexistent-session")
		if err != nil {
			t.Fatalf("GetToolCallBySession: %v", err)
		}
		if record != nil {
			t.Error("expected nil for non-existent session")
		}
	})

	t.Run("GetToolCallBySession returns nil for non-existent tool_id", func(t *testing.T) {
		record, err := GetToolCallBySession("toolu_99", sessionID)
		if err != nil {
			t.Fatalf("GetToolCallBySession: %v", err)
		}
		if record != nil {
			t.Error("expected nil for non-existent tool_id")
		}
	})
}

// initTestDB creates a test database in the given directory
func initTestDB(dbDir string) error {
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	model.BinDir = dbDir
	model.DataDir = filepath.Join(dbDir, ".clawbench")
	defer func() { model.BinDir = origBinDir; model.DataDir = origDataDir }()

	return InitDB(false)
}

func TestInitDB_MigratesChatToolCallsDurationColumn(t *testing.T) {
	// Simulate an existing database created before chat_tool_calls.duration_ms existed.
	dbDir := t.TempDir()
	clawDir := filepath.Join(dbDir, ".clawbench")
	if err := os.MkdirAll(clawDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dbPath := filepath.Join(clawDir, "ClawBench.db")
	oldDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	if _, err := oldDB.Exec(`
		CREATE TABLE chat_tool_calls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL,
			session_id TEXT NOT NULL,
			tool_id TEXT NOT NULL,
			name TEXT NOT NULL,
			input TEXT NOT NULL DEFAULT '{}',
			output TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			done INTEGER NOT NULL DEFAULT 0,
			summary TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(tool_id, message_id)
		);
	`); err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	_ = oldDB.Close()

	// InitDB runs the schema migration that adds duration_ms.
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	var hasDuration int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_tool_calls') WHERE name='duration_ms'").Scan(&hasDuration); err != nil {
		t.Fatalf("query column info: %v", err)
	}
	if hasDuration != 1 {
		t.Fatal("expected duration_ms column to be added by InitDB migration")
	}

	// New column should default to 0 for pre-existing rows.
	if _, err := db.Exec(`INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('s', '/p', 'b', 'T')`); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	res, err := db.Exec(`INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES ('/p', 'assistant', '{}', 's', 'b')`)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name) VALUES (?, 's', 't1', 'Read')`, msgID); err != nil {
		t.Fatalf("insert old-format tool call: %v", err)
	}
	var dur int
	if err := db.QueryRow(`SELECT duration_ms FROM chat_tool_calls WHERE tool_id = 't1'`).Scan(&dur); err != nil {
		t.Fatalf("select duration_ms: %v", err)
	}
	if dur != 0 {
		t.Errorf("expected default duration_ms=0, got %d", dur)
	}
}
