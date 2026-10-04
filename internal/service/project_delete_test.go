package service

import (
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/rag"
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
	// A second project whose rows must survive untouched. Without it, a delete
	// that wiped the whole table would still pass every "no rows left" assert.
	otherID := seedProjectRow(t, "/proj/keep-me")

	// Seed one row into every table the delete is supposed to cover.
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := store.WriteExec(q, args...)
		require.NoError(t, err)
	}
	exec("INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived) VALUES ('sess-1', ?, 'claude', 't', 'chat', 0)", id)
	exec("INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived) VALUES ('sess-other', ?, 'claude', 't', 'chat', 0)", otherID)
	exec("INSERT INTO chat_history (id, project_id, backend, session_id, role, content) VALUES (101, ?, 'claude', 'sess-1', 'user', 'hi')", id)
	exec("INSERT INTO chat_history (id, project_id, backend, session_id, role, content) VALUES (201, ?, 'claude', 'sess-other', 'user', 'yo')", otherID)
	// A historic row whose project_id disagrees with its session's project:
	// belongs to the deleted project's session but carries project_id=0. Its
	// summary/TTS rows must still be removed.
	exec("INSERT INTO chat_history (id, project_id, backend, session_id, role, content) VALUES (102, 0, 'claude', 'sess-1', 'assistant', 'mis-attributed')")
	exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', 101, 's1')")
	exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', 102, 's-mis')")
	exec("INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', 201, 's-other')")
	exec("INSERT INTO tts_summaries (message_id, tts_summary) VALUES (101, 'tts1')")
	exec("INSERT INTO tts_summaries (message_id, tts_summary) VALUES (102, 'tts-mis')")
	exec("INSERT INTO tts_summaries (message_id, tts_summary) VALUES (201, 'tts-other')")
	exec("INSERT INTO chat_metadata (message_id, project_id) VALUES (101, ?)", id)
	exec("INSERT INTO chat_metadata (message_id, project_id) VALUES (201, ?)", otherID)
	exec("INSERT INTO chat_recommendations (session_id, project_id, message_id, recommendation) VALUES ('sess-1', ?, 101, 'r')", id)
	exec("INSERT INTO chat_quick_send (project_id, label, command) VALUES (?, 'l', 'c')", id)
	exec("INSERT INTO queued_messages (session_id, project_id, backend, queue_id, content) VALUES ('sess-1', ?, 'claude', 'q1', 'x')", id)
	exec("INSERT INTO recent_projects (project_id) VALUES (?)", id)
	exec("INSERT INTO file_shares (token, path, name, project_id) VALUES ('tok', '/f', 'f', ?)", id)
	exec("INSERT INTO project_forges (project_id, platform, host, owner, repo) VALUES (?, 'github', 'h', 'o', 'r')", id)
	exec("INSERT INTO btw_questions (session_id, project_id, question, answer) VALUES ('sess-1', ?, 'q', 'a')", id)
	exec("INSERT INTO session_tags (name, scope, project_id) VALUES ('tag', 'project', ?)", id)
	exec("INSERT INTO scheduled_tasks (project_id, name, cron_expr, agent_id, prompt) VALUES (?, 'task', '* * * * *', 'a', 'p')", id)
	exec("INSERT INTO terminal_quick_commands (project_id, label, command) VALUES (?, 'l', 'c')", id)
	exec("INSERT INTO chat_thinking (message_id, session_id, think_id, seq, text) VALUES (101, 'sess-1', 'th', 0, 'thinking')")
	exec("INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name) VALUES (101, 'sess-1', 'tool', 'n')")
	exec("INSERT INTO task_executions (task_id, session_id) VALUES ((SELECT id FROM scheduled_tasks WHERE project_id = ?), 'sess-1')", id)
	exec("INSERT INTO session_shares (token, session_id, payload) VALUES ('stok', 'sess-1', '{}')")
	exec("INSERT INTO session_tag_links (session_id, tag_id) VALUES ('sess-1', (SELECT id FROM session_tags WHERE project_id = ?))", id)
	// Rows belonging to the OTHER project, to prove isolation.
	exec("INSERT INTO chat_history (project_id, backend, session_id, role, content) VALUES (?, 'claude', 'sess-other', 'assistant', 'keep')", otherID)
	exec("INSERT INTO recent_projects (project_id) VALUES (?)", otherID)
	exec("INSERT INTO session_tags (name, scope, project_id) VALUES ('other-tag', 'project', ?)", otherID)

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

	// summaries / tts_summaries key on chat_history.id — including the
	// mis-attributed row (project_id=0 but session in the deleted project).
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM summaries WHERE target_id IN (101, 102)").Scan(&n))
	assert.Zero(t, n, "summaries of the project's messages must be deleted, including the mis-attributed one")
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM tts_summaries WHERE message_id IN (101, 102)").Scan(&n))
	assert.Zero(t, n, "tts_summaries of the project's messages must be deleted, including the mis-attributed one")

	// The OTHER project's rows must be untouched.
	assert.Equal(t, 1, countRows(t, "chat_sessions", otherID), "other project's session must survive")
	assert.Equal(t, 2, countRows(t, "chat_history", otherID), "other project's history must survive")
	assert.Equal(t, 1, countRows(t, "recent_projects", otherID), "other project's recent entry must survive")
	assert.Equal(t, 1, countRows(t, "session_tags", otherID), "other project's tag must survive")
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM summaries WHERE target_id = 201").Scan(&n))
	assert.Equal(t, 1, n, "other project's summary must survive")
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM tts_summaries WHERE message_id = 201").Scan(&n))
	assert.Equal(t, 1, n, "other project's tts summary must survive")

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
		"INSERT INTO chat_sessions (id, project_id, backend, title, session_type, archived) VALUES ('sess-run', ?, 'claude', 't', 'chat', 0)", id,
	)
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

// TestDeleteProjectData_RAGPurgeDoesNotDeadlock guards the ordering between the
// DB transaction and the RAG purge. The RAG store opened by rag.Init serializes
// on the SAME global mutex as store.WriteBegin (via serviceWriteLocker), and
// that mutex is non-reentrant — so calling rag.DeleteProjectData while the
// write lock is still held self-deadlocks and wedges every later write in the
// process. The purge must run only after the lock is released.
//
// This needs a store whose locker is the production one: the default
// NewSQLiteStoreForTest uses a no-op locker and would never reproduce the
// deadlock, which is exactly how this regression slipped through the other
// tests (they leave rag.GlobalStore nil, so the purge is a no-op).
func TestDeleteProjectData_RAGPurgeDoesNotDeadlock(t *testing.T) {
	setupProjectDeleteDB(t)
	id := seedProjectRow(t, "/proj/rag-deadlock")

	rs, err := rag.NewSQLiteStore(":memory:") // production locker → store.WriteLock
	require.NoError(t, err)
	defer func() { _ = rs.Close() }()

	origStore := rag.GlobalStore
	rag.GlobalStore = rs
	t.Cleanup(func() { rag.GlobalStore = origStore })

	done := make(chan error, 1)
	go func() { done <- DeleteProjectData(id) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("DeleteProjectData deadlocked: the RAG purge ran while the write lock was still held")
	}
}
