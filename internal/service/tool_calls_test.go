package service

import (
	"encoding/json"
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
		DB.Close()
		DBRead.Close()
	}()

	// Create a session and message first (FK dependency)
	sessionID := "test-session-001"
	_, _ = DB.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Test Session")

	var msgID int64
	res, err := DB.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ = res.LastInsertId()

	t.Run("insert new tool call", func(t *testing.T) {
		input := json.RawMessage(`{"file_path":"/src/main.go"}`)
		err := UpsertToolCall(msgID, sessionID, "toolu_01", "Read", input, "file contents...", "success", "main.go", true)
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
	})

	t.Run("update existing tool call (UPSERT)", func(t *testing.T) {
		// Update with new input (merged) and output
		input := json.RawMessage(`{"file_path":"/src/main.go","description":"Read main file"}`)
		err := UpsertToolCall(msgID, sessionID, "toolu_01", "Read", input, "updated contents...", "success", "Read main file", true)
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
	})

	t.Run("upsert with empty output preserves existing", func(t *testing.T) {
		// Simulate tool_use event (no output yet) after tool_result already set output
		input := json.RawMessage(`{"file_path":"/src/main.go"}`)
		err := UpsertToolCall(msgID, sessionID, "toolu_01", "Read", input, "", "success", "main.go", false)
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
}

// initTestDB creates a test database in the given directory
func initTestDB(dbDir string) error {
	origBinDir := model.BinDir
	model.BinDir = dbDir
	defer func() { model.BinDir = origBinDir }()

	return InitDB(false)
}
