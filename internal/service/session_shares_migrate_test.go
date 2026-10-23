package service

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// The session-share table used to carry a frozen JSON payload plus redundant
// title/backend/message_count copies. The live-assembly refactor drops all of
// them, and the rebuild must also DISCARD every existing row: a legacy row's
// selection lived inside the payload, so keeping it would leave message_ids at
// the '[]' default — which reads as "nothing left to show".
func TestInitDB_MigratesSessionSharesToIDOnly(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	t.Cleanup(func() {
		model.BinDir = origBinDir
		model.DataDir = origDataDir
	})
	restoreDB := store.SnapshotDBForTest()
	t.Cleanup(restoreDB)
	t.Cleanup(func() { store.Close() })

	// Build a legacy-shaped table by hand, then let InitDB upgrade it.
	if err := os.MkdirAll(model.DataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	if err := store.Open(filepath.Join(model.DataDir, "ClawBench.db")); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := store.WriteExec(`
		CREATE TABLE session_shares (
			token TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			backend TEXT NOT NULL DEFAULT '',
			message_count INTEGER NOT NULL DEFAULT 0,
			payload TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if _, err := store.WriteExec(
		`INSERT INTO session_shares (token, session_id, title, backend, message_count, payload)
		 VALUES ('legacy', 'sess-1', 'old title', 'codebuddy', 3, '{"version":1}')`,
	); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	if err := InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	for _, col := range []string{"payload", "title", "backend", "message_count"} {
		var n int
		if err := store.ReadDB().QueryRow(
			"SELECT COUNT(*) FROM pragma_table_info('session_shares') WHERE name = ?", col,
		).Scan(&n); err != nil {
			t.Fatalf("pragma_table_info(%s): %v", col, err)
		}
		if n != 0 {
			t.Errorf("column %q must be dropped by the migration", col)
		}
	}

	var rows int
	if err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM session_shares").Scan(&rows); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("legacy rows must be discarded, got %d", rows)
	}

	// The new shape must accept a row, and the index must have survived the
	// table rebuild (DROP TABLE takes indexes with it).
	if _, err := store.WriteExec(
		`INSERT INTO session_shares (token, session_id, message_ids) VALUES ('t', 'sess-1', '[1,2]')`,
	); err != nil {
		t.Fatalf("insert into migrated table: %v", err)
	}
	var idx int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_session_shares_session'",
	).Scan(&idx); err != nil {
		t.Fatalf("probe index: %v", err)
	}
	if idx != 1 {
		t.Errorf("idx_session_shares_session must be recreated after the rebuild")
	}

	// Idempotent: the probe sees no payload column and skips the rebuild.
	if err := InitDB(); err != nil {
		t.Fatalf("second InitDB: %v", err)
	}
	if err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM session_shares").Scan(&rows); err != nil {
		t.Fatalf("count rows after second InitDB: %v", err)
	}
	if rows != 1 {
		t.Errorf("a second InitDB must not touch the migrated table, got %d rows", rows)
	}
}
