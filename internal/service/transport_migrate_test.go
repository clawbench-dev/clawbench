package service

import (
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTransportMigrationDB(t *testing.T) {
	t.Helper()
	db, err := InitInMemoryDB()
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
}

// An upgraded install kept "cli" in the column for every agent the user had
// ever switched, and the PATCH handler no longer rewrites it. The migration is
// what makes the column agree with the only transport that exists.
func TestMigrateAgentTransportFromCommand(t *testing.T) {
	setupTransportMigrationDB(t)

	_, err := store.WriteExec(`INSERT INTO agents (id, name, backend, transport, acp_command) VALUES
		('with-cmd', 'A', 'claude', 'cli', 'claude --acp'),
		('already', 'B', 'codebuddy', 'acp-stdio', 'codebuddy --acp'),
		('builtin', 'C', 'builtin', 'cli', '')`)
	require.NoError(t, err)

	require.True(t, MigrateAgentTransportFromCommand())

	transportOf := func(id string) string {
		var v string
		require.NoError(t, store.ReadDB().QueryRow("SELECT transport FROM agents WHERE id = ?", id).Scan(&v))
		return v
	}
	assert.Equal(t, "acp-stdio", transportOf("with-cmd"), "a command decides the transport")
	assert.Equal(t, "acp-stdio", transportOf("already"), "an already-correct row is untouched behaviourally")
	assert.Equal(t, "cli", transportOf("builtin"), "the in-process agent must keep its only value")
}

// A stale per-session pin made clients keep the CLI-facing gates for a session
// that in fact runs over ACP.
func TestMigrateSessionTransportColumns(t *testing.T) {
	setupTransportMigrationDB(t)

	_, err := store.WriteExec(`INSERT INTO projects (id, path) VALUES (1, '/proj')`)
	require.NoError(t, err)
	_, err = store.WriteExec(`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, transport) VALUES
		('pinned', 1, 'claude', 'T', 'claude', 'cli'),
		('empty', 1, 'claude', 'T2', 'claude', '')`)
	require.NoError(t, err)

	require.True(t, MigrateSessionTransportColumns())

	transportOf := func(id string) string {
		var v string
		require.NoError(t, store.ReadDB().QueryRow("SELECT transport FROM chat_sessions WHERE id = ?", id).Scan(&v))
		return v
	}
	assert.Empty(t, transportOf("pinned"))
	assert.Empty(t, transportOf("empty"))
}
