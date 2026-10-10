package service

import (
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRawDBForMigration points model.BinDir/DataDir at a temp dir, opens the DB
// pools WITHOUT running the full InitDB, and returns a cleanup. It lets a test
// build an "old" schema by hand (no agent_name) and then run InitDB to migrate.
func setupRawDBForMigration(t *testing.T) {
	t.Helper()
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
}

// TestSchema_AgentNameColumnExists verifies the additive migration that gives a
// group member its own authoritative display-name column.
func TestSchema_AgentNameColumnExists(t *testing.T) {
	setupRawDBForMigration(t)
	require.NoError(t, InitDB())
	defer store.Close()

	columns := getTableColumns(t, store.UnsafeDBForTest(), "chat_sessions")
	assert.Contains(t, columns, "agent_name", "chat_sessions should have agent_name column")
}

// TestSchema_AgentNameMigration_Idempotent verifies InitDB can run twice without
// erroring on the already-present column.
func TestSchema_AgentNameMigration_Idempotent(t *testing.T) {
	setupRawDBForMigration(t)
	require.NoError(t, InitDB())
	require.NoError(t, InitDB())
	defer store.Close()

	columns := getTableColumns(t, store.UnsafeDBForTest(), "chat_sessions")
	assert.Contains(t, columns, "agent_name")
}

// TestSchema_AgentNameBackfillsMemberRows verifies the one-time data migration:
// an existing member row (which stored its display name in `title`) has that
// name moved into agent_name and its title cleared, while a non-member row is
// left untouched.
func TestSchema_AgentNameBackfillsMemberRows(t *testing.T) {
	setupRawDBForMigration(t)
	require.NoError(t, InitDB())

	db := store.UnsafeDBForTest()
	// Simulate the pre-migration state: a member row whose display name is in
	// `title` and agent_name empty, plus a plain chat row.
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id)
		 VALUES ('m-old', 1, 'claude', 'Old Member', '', 'claude', 'group_member', 'g1')`)
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type)
		 VALUES ('c-plain', 1, 'claude', 'A Chat', '', 'claude', 'chat')`)
	require.NoError(t, err)

	// Re-run InitDB: the guarded backfill runs because agent_name is still ''.
	// InitDB re-opens the pools, so re-acquire the handle afterwards.
	require.NoError(t, InitDB())
	defer store.Close()

	db = store.UnsafeDBForTest()
	var memberName, memberTitle string
	require.NoError(t, db.QueryRow(
		"SELECT agent_name, title FROM chat_sessions WHERE id = 'm-old'").Scan(&memberName, &memberTitle))
	assert.Equal(t, "Old Member", memberName, "member display name must be moved into agent_name")
	assert.Equal(t, "", memberTitle, "member title must be cleared")

	var chatName, chatTitle string
	require.NoError(t, db.QueryRow(
		"SELECT agent_name, title FROM chat_sessions WHERE id = 'c-plain'").Scan(&chatName, &chatTitle))
	assert.Equal(t, "", chatName, "a non-member row must not get an agent_name")
	assert.Equal(t, "A Chat", chatTitle, "a non-member row's title must be untouched")

	// Idempotent: running again must not re-clear or re-copy anything.
	require.NoError(t, InitDB())
	db = store.UnsafeDBForTest()
	require.NoError(t, db.QueryRow(
		"SELECT agent_name, title FROM chat_sessions WHERE id = 'm-old'").Scan(&memberName, &memberTitle))
	assert.Equal(t, "Old Member", memberName)
	assert.Equal(t, "", memberTitle)
}
