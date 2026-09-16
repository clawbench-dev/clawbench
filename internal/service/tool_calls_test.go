package service

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"github.com/stretchr/testify/require"
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
		require.NotNil(t, record, "GetToolCall returned nil")
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

func TestGetToolCallsBySession(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	sessionID := "tc-session-all"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Test Session")
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	t.Run("returns empty for session with no tool calls", func(t *testing.T) {
		records, err := GetToolCallsBySession(sessionID)
		if err != nil {
			t.Fatalf("GetToolCallsBySession: %v", err)
		}
		if len(records) != 0 {
			t.Errorf("expected 0 records, got %d", len(records))
		}
	})

	t.Run("returns all tool calls for session", func(t *testing.T) {
		input1 := json.RawMessage(`{"file_path":"/a.go"}`)
		input2 := json.RawMessage(`{"command":"ls"}`)
		if err := UpsertToolCall(msgID, sessionID, "toolu_s1", "Read", input1, "content-a", "success", "", true, 100); err != nil {
			t.Fatalf("UpsertToolCall 1: %v", err)
		}
		if err := UpsertToolCall(msgID, sessionID, "toolu_s2", "Bash", input2, "output", "success", "", true, 200); err != nil {
			t.Fatalf("UpsertToolCall 2: %v", err)
		}
		records, err := GetToolCallsBySession(sessionID)
		if err != nil {
			t.Fatalf("GetToolCallsBySession: %v", err)
		}
		if len(records) != 2 {
			t.Fatalf("expected 2 records, got %d", len(records))
		}
		got := map[string]string{}
		for _, r := range records {
			got[r.ToolID] = r.Name
		}
		if got["toolu_s1"] != "Read" || got["toolu_s2"] != "Bash" {
			t.Errorf("mismatch: %+v", got)
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

func TestInitDB_MigratesChatThinkingSeq(t *testing.T) {
	// Simulate a database created before chat_thinking had the seq column and
	// the (think_id, message_id, seq) unique constraint.
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
		CREATE TABLE chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			streaming INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			agent_id TEXT DEFAULT '',
			model TEXT DEFAULT '',
			external_session_id TEXT DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			archived INTEGER NOT NULL DEFAULT 0,
			last_read_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(project_path, backend, id)
		);
		CREATE TABLE chat_thinking (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL REFERENCES chat_history(id) ON DELETE CASCADE,
			session_id TEXT NOT NULL,
			think_id TEXT NOT NULL,
			text TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(think_id, message_id)
		);
		CREATE INDEX idx_thinking_message ON chat_thinking(message_id);
		CREATE INDEX idx_thinking_session ON chat_thinking(session_id, created_at DESC);
		INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('s', '/p', 'b', 'T');
		INSERT INTO chat_history (id, project_path, role, content, session_id, backend) VALUES (1, '/p', 'assistant', '{"blocks":[]}', 's', 'b');
		INSERT INTO chat_thinking (id, message_id, session_id, think_id, text) VALUES (1, 1, 's', 'th_old1', 'full thinking text');
	`); err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	_ = oldDB.Close()

	// InitDB runs the migration that rebuilds chat_thinking with seq.
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()
	_ = dbRead

	var hasSeq int
	if err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_thinking') WHERE name='seq'").Scan(&hasSeq); err != nil {
		t.Fatalf("query column info: %v", err)
	}
	if hasSeq != 1 {
		t.Fatal("expected seq column to be added by InitDB migration")
	}

	// Existing row migrated as a seq=0 single chunk, text preserved.
	var text string
	if err := db.QueryRow(`SELECT text FROM chat_thinking WHERE think_id = 'th_old1' AND seq = 0`).Scan(&text); err != nil {
		t.Fatalf("query migrated thinking: %v", err)
	}
	if text != "full thinking text" {
		t.Errorf("expected preserved thinking text, got %q", text)
	}

	// New unique constraint allows multiple seq chunks per think_id.
	if _, err := db.Exec(`INSERT INTO chat_thinking (message_id, session_id, think_id, seq, text) VALUES (1, 's', 'th_old1', 1, 'delta')`); err != nil {
		t.Fatalf("insert seq=1 chunk must succeed under new constraint: %v", err)
	}

	// Migration is idempotent — running InitDB again must not reset the chunks.
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("second initTestDB: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat_thinking WHERE think_id = 'th_old1'`).Scan(&count); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 chunks after idempotent re-run, got %d", count)
	}
}
