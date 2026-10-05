package service

import (
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// newGroupSchemaDB initializes a fresh DB (full schema via InitDB) for the
// group-chat schema tests and returns a cleanup function. Mirrors
// database_test.go's TestSchema_* pattern.
func newGroupSchemaDB(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	t.Cleanup(func() {
		model.BinDir = origBinDir
		model.DataDir = origDataDir
	})

	restoreDB := store.SnapshotDBForTest()
	t.Cleanup(restoreDB)

	if err := InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { store.Close() })
}

func TestChatHistoryHasAgentIDColumn(t *testing.T) {
	newGroupSchemaDB(t)
	cols := getTableColumns(t, store.UnsafeDBForTest(), "chat_history")
	if !cols["agent_id"] {
		t.Fatalf("chat_history.agent_id column missing (cols=%v)", cols)
	}
}

func TestChatSessionsHasGroupIDColumn(t *testing.T) {
	newGroupSchemaDB(t)
	cols := getTableColumns(t, store.UnsafeDBForTest(), "chat_sessions")
	if !cols["group_id"] {
		t.Fatalf("chat_sessions.group_id column missing (cols=%v)", cols)
	}
}

func TestGroupIDMigrationIdempotent(t *testing.T) {
	// Running InitDB twice must not fail on the already-present columns.
	newGroupSchemaDB(t)
	if err := InitDB(); err != nil {
		t.Fatalf("second InitDB: %v", err)
	}
}
