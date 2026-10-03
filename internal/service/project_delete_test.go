package service

import (
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupProjectDeleteDB installs the REAL schema (via InitDB) so the delete
// touches the same tables production does. A hand-written schema would keep
// passing after a table gained a project_id column that the delete forgot to
// cover — the exact class of bug this test exists to catch.
func setupProjectDeleteDB(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir, model.DataDir = tmpDir, filepath.Join(tmpDir, ".clawbench")
	restoreDB := store.SnapshotDBForTest()
	t.Cleanup(func() {
		model.BinDir, model.DataDir = origBinDir, origDataDir
		store.Close()
		restoreDB()
		store.ResetProjectIDCacheForTest()
	})
	require.NoError(t, InitDB())
}

// seedProjectRow registers a project and returns its id.
func seedProjectRow(t *testing.T, path string) int64 {
	t.Helper()
	return store.ProjectIDForTest(t, path)
}

// countRows returns how many rows in table have the given project_id.
func countRows(t *testing.T, table string, projectID int64) int {
	t.Helper()
	var n int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM "+table+" WHERE project_id = ?", projectID,
	).Scan(&n))
	return n
}

func TestDeleteProjectData_RemovesAllProjectScopedRows(t *testing.T) {
	setupProjectDeleteDB(t)
	const path = "/proj/delete-me"
	id := seedProjectRow(t, path)

	// Seed one row into every table the delete is supposed to cover.
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := store.WriteExec(q, args...)
		require.NoError(t, err)
	}
	exec("INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived) VALUES ('sess-1', ?, 'claude', 't', 'chat', 0)", id)
	exec("INSERT INTO chat_history (project_id, backend, session_id, role, content) VALUES (?, 'claude', 'sess-1', 'user', 'hi')", id)
	exec("INSERT INTO chat_metadata (message_id, project_id) VALUES (1, ?)", id)
	exec("INSERT INTO chat_recommendations (session_id, project_id, message_id, recommendation) VALUES ('sess-1', ?, 1, 'r')", id)
	exec("INSERT INTO chat_quick_send (project_id, label, command) VALUES (?, 'l', 'c')", id)
	exec("INSERT INTO queued_messages (session_id, project_id, backend, queue_id, content) VALUES ('sess-1', ?, 'claude', 'q1', 'x')", id)
	exec("INSERT INTO recent_projects (project_id) VALUES (?)", id)
	exec("INSERT INTO file_shares (token, path, name, project_id) VALUES ('tok', '/f', 'f', ?)", id)
	exec("INSERT INTO project_forges (project_id, platform, host, owner, repo) VALUES (?, 'github', 'h', 'o', 'r')", id)
	exec("INSERT INTO btw_questions (session_id, project_id, question, answer) VALUES ('sess-1', ?, 'q', 'a')", id)
	exec("INSERT INTO session_tags (name, scope, project_id) VALUES ('tag', 'project', ?)", id)
	exec("INSERT INTO scheduled_tasks (project_id, name, cron_expr, agent_id, prompt) VALUES (?, 'task', '* * * * *', 'a', 'p')", id)
	exec("INSERT INTO terminal_quick_commands (project_id, label, command) VALUES (?, 'l', 'c')", id)
	exec("INSERT INTO chat_thinking (message_id, session_id, think_id, seq, text) VALUES (1, 'sess-1', 'th', 0, 'thinking')")
	exec("INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name) VALUES (1, 'sess-1', 'tool', 'n')")
	exec("INSERT INTO task_executions (task_id, session_id) VALUES ((SELECT id FROM scheduled_tasks WHERE project_id = ?), 'sess-1')", id)
	exec("INSERT INTO session_shares (token, session_id, payload) VALUES ('stok', 'sess-1', '{}')")
	exec("INSERT INTO session_tag_links (session_id, tag_id) VALUES ('sess-1', (SELECT id FROM session_tags WHERE project_id = ?))", id)

	require.NoError(t, DeleteProjectData(id))

	for _, table := range []string{
		"chat_sessions", "chat_history", "chat_metadata", "chat_recommendations",
		"chat_quick_send", "queued_messages", "recent_projects", "file_shares",
		"project_forges", "btw_questions", "session_tags", "scheduled_tasks",
		"terminal_quick_commands",
	} {
		assert.Zerof(t, countRows(t, table, id), "%s must have no rows left for the project", table)
	}

	// Child tables without project_id, checked via their parent's absence.
	var n int
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE session_id = 'sess-1'").Scan(&n))
	assert.Zero(t, n, "chat_thinking rows must be deleted")
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_tool_calls WHERE session_id = 'sess-1'").Scan(&n))
	assert.Zero(t, n, "chat_tool_calls rows must be deleted")
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM task_executions WHERE session_id = 'sess-1'").Scan(&n))
	assert.Zero(t, n, "task_executions rows must be deleted")
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM session_shares WHERE session_id = 'sess-1'").Scan(&n))
	assert.Zero(t, n, "session_shares rows must be deleted")
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM session_tag_links WHERE session_id = 'sess-1'").Scan(&n))
	assert.Zero(t, n, "session_tag_links rows must be deleted")

	// The registry row itself is gone.
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM projects WHERE id = ?", id).Scan(&n))
	assert.Zero(t, n, "the projects row must be deleted")
}

func TestDeleteProjectData_KeepsGlobalTags(t *testing.T) {
	setupProjectDeleteDB(t)
	id := seedProjectRow(t, "/proj/tagged")

	_, err := store.WriteExec("INSERT INTO session_tags (name, scope, project_id) VALUES ('global', 'global', 0)")
	require.NoError(t, err)
	_, err = store.WriteExec("INSERT INTO session_tags (name, scope, project_id) VALUES ('mine', 'project', ?)", id)
	require.NoError(t, err)

	require.NoError(t, DeleteProjectData(id))

	var n int
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM session_tags WHERE project_id = 0").Scan(&n))
	assert.Equal(t, 1, n, "global tags (project_id=0) must survive a project deletion")
}

func TestDeleteProjectData_RejectsGlobalSentinel(t *testing.T) {
	setupProjectDeleteDB(t)
	err := DeleteProjectData(store.GlobalScopeProjectID)
	assert.ErrorIs(t, err, ErrProjectDeleteForbidden)
}

func TestDeleteProjectData_UnknownID(t *testing.T) {
	setupProjectDeleteDB(t)
	err := DeleteProjectData(999999)
	assert.ErrorIs(t, err, ErrProjectNotFound)
}

func TestDeleteProjectData_RejectsRunningSession(t *testing.T) {
	setupProjectDeleteDB(t)
	id := seedProjectRow(t, "/proj/running")
	_, err := store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived) VALUES ('sess-run', ?, 'claude', 't', 'chat', 0)", id)
	require.NoError(t, err)

	SetSessionRunning("sess-run", true)
	t.Cleanup(func() { SetSessionRunning("sess-run", false) })

	err = DeleteProjectData(id)
	assert.ErrorIs(t, err, ErrProjectHasRunningSessions)

	// Nothing may have been deleted.
	var n int
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE id = 'sess-run'").Scan(&n))
	assert.Equal(t, 1, n, "a refused delete must not remove any rows")
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM projects WHERE id = ?", id).Scan(&n))
	assert.Equal(t, 1, n, "a refused delete must keep the projects row")
}

func TestDeleteProjectData_ForgetsPathCache(t *testing.T) {
	setupProjectDeleteDB(t)
	const path = "/proj/re-register"
	id := seedProjectRow(t, path)

	require.NoError(t, DeleteProjectData(id))

	// Re-registering the same path must produce a FRESH row, not the stale
	// cached id of the deleted one.
	newID, err := store.ProjectIDForPath(path)
	require.NoError(t, err)
	assert.NotEqual(t, id, newID, "the path→id cache must be evicted so re-registration gets a new id")
}
