package service

import (
	"encoding/json"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setMoveTestAgents swaps the in-memory agent registry for the duration of one
// test (the move validates group members against it).
func setMoveTestAgents(t *testing.T, agents map[string]*model.Agent) {
	t.Helper()
	orig := model.Agents
	model.Agents = agents
	t.Cleanup(func() { model.Agents = orig })
}

// memberCursor reads a member row's stored seen_cursor.
func memberCursor(t *testing.T, memberID string) int64 {
	t.Helper()
	var raw string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COALESCE(json_extract(context_state, '$.seen_cursor'), '') FROM chat_sessions WHERE id = ?",
		memberID,
	).Scan(&raw))
	if raw == "" {
		return 0
	}
	var cursor int64
	require.NoError(t, json.Unmarshal([]byte(raw), &cursor))
	return cursor
}

// setupMoveTest opens a throwaway store with the tables a move touches.
func setupMoveTest(t *testing.T) (srcPath, dstPath string, srcID, dstID int64) {
	t.Helper()
	db := setupChatTestDB(t)
	require.NoError(t, db.Ping())
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			backend TEXT NOT NULL DEFAULT 'claude',
			title TEXT NOT NULL,
			session_type TEXT NOT NULL DEFAULT 'chat',
			sort_order INTEGER NOT NULL DEFAULT 0,
			archived INTEGER NOT NULL DEFAULT 0,
			external_session_id TEXT DEFAULT '',
			context_state TEXT DEFAULT '',
			group_id TEXT DEFAULT NULL,
			removed INTEGER NOT NULL DEFAULT 0,
			disabled INTEGER NOT NULL DEFAULT 0,
			agent_id TEXT DEFAULT '',
			agent_name TEXT DEFAULT '',
			agent_avatar TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS chat_metadata (
			message_id INTEGER PRIMARY KEY,
			project_id INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS queued_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			project_id INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS btw_questions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			project_id INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS chat_recommendations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			project_id INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS session_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			scope TEXT NOT NULL DEFAULT 'project',
			project_id INTEGER
		);
		CREATE TABLE IF NOT EXISTS session_tag_links (
			session_id TEXT NOT NULL,
			tag_id INTEGER NOT NULL,
			PRIMARY KEY (session_id, tag_id)
		)`)
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(cleanup)

	root := t.TempDir()
	srcPath = root + "/src"
	dstPath = root + "/dst"
	srcID, err = store.ProjectIDForPath(srcPath)
	require.NoError(t, err)
	dstID, err = store.ProjectIDForPath(dstPath)
	require.NoError(t, err)
	return srcPath, dstPath, srcID, dstID
}

func TestMoveSessionToProject_MovesSessionMessagesAndLedger(t *testing.T) {
	_, dstPath, srcID, dstID := setupMoveTest(t)

	_, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, sort_order, external_session_id)
		 VALUES ('s1', ?, 'claude', 't', 5, 'native-old-cwd')`,
		srcID,
	)
	require.NoError(t, err)
	_, err = store.WriteExec(
		"INSERT INTO chat_history (project_id, role, content, session_id, backend) VALUES (?, 'user', 'hi', 's1', 'claude')",
		srcID,
	)
	require.NoError(t, err)
	_, err = store.WriteExec(
		`INSERT INTO chat_history (project_id, role, content, session_id, backend)
		 VALUES (?, 'assistant', '{"blocks":[{"type":"text","text":"hello there"}]}', 's1', 'claude')`,
		srcID,
	)
	require.NoError(t, err)
	var msgID int64
	require.NoError(t, store.ReadDB().QueryRow("SELECT id FROM chat_history WHERE session_id = 's1'").Scan(&msgID))
	_, err = store.WriteExec(
		"INSERT INTO chat_metadata (message_id, project_id) VALUES (?, ?)", msgID, srcID,
	)
	require.NoError(t, err)
	// The other rows that denormalize the project id.
	for _, q := range []string{
		"INSERT INTO queued_messages (session_id, project_id) VALUES ('s1', ?)",
		"INSERT INTO btw_questions (session_id, project_id) VALUES ('s1', ?)",
		"INSERT INTO chat_recommendations (session_id, project_id) VALUES ('s1', ?)",
	} {
		_, err = store.WriteExec(q, srcID)
		require.NoError(t, err)
	}
	// One tag definition from the source project, one global tag.
	localTag, err := store.WriteExec(
		"INSERT INTO session_tags (name, scope, project_id) VALUES ('local', 'project', ?)", srcID)
	require.NoError(t, err)
	localTagID, err := localTag.LastInsertId()
	require.NoError(t, err)
	globalTag, err := store.WriteExec(
		"INSERT INTO session_tags (name, scope, project_id) VALUES ('global', 'global', NULL)")
	require.NoError(t, err)
	globalTagID, err := globalTag.LastInsertId()
	require.NoError(t, err)
	for _, tagID := range []int64{localTagID, globalTagID} {
		_, err = store.WriteExec("INSERT INTO session_tag_links (session_id, tag_id) VALUES ('s1', ?)", tagID)
		require.NoError(t, err)
	}

	moved, err := MoveSessionToProject("s1", dstPath, dstID)
	require.NoError(t, err)
	assert.True(t, moved)

	var sessionProject, historyProject, metaProject int64
	var sortOrder int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT project_id, sort_order FROM chat_sessions WHERE id = 's1'").Scan(&sessionProject, &sortOrder))
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT project_id FROM chat_history WHERE session_id = 's1'").Scan(&historyProject))
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT project_id FROM chat_metadata WHERE message_id = ?", msgID).Scan(&metaProject))

	assert.Equal(t, dstID, sessionProject)
	assert.Equal(t, dstID, historyProject, "history queries filter on project_id too — the messages must follow")
	assert.Equal(t, dstID, metaProject, "the usage ledger is re-attributed to the new project")
	assert.Less(t, sortOrder, 0, "a moved conversation joins the target list above its existing rows")

	// A queued message materializes with the id captured at enqueue time, and
	// /btw + recommendation rows render/query their stored project — all three
	// would point at the OLD project (or vanish) if left behind.
	for _, q := range []string{
		"SELECT project_id FROM queued_messages WHERE session_id = 's1'",
		"SELECT project_id FROM btw_questions WHERE session_id = 's1'",
		"SELECT project_id FROM chat_recommendations WHERE session_id = 's1'",
	} {
		var got int64
		require.NoError(t, store.ReadDB().QueryRow(q).Scan(&got))
		assert.Equal(t, dstID, got, "%s must follow the conversation", q)
	}

	// The source project's tag cannot exist in the target project; the global
	// tag is project-independent and stays attached.
	rows, err := store.ReadDB().Query(
		"SELECT tag_id FROM session_tag_links WHERE session_id = 's1' ORDER BY tag_id")
	require.NoError(t, err)
	defer rows.Close()
	var linked []int64
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		linked = append(linked, id)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int64{globalTagID}, linked, "project tags are dropped, global tags stay")

	// The native agent session belongs to the old directory and must not be
	// resumed there: it is cleared and the timeline becomes the pending context
	// the next turn injects (the fork mechanism).
	var externalID string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT external_session_id FROM chat_sessions WHERE id = 's1'").Scan(&externalID))
	assert.Empty(t, externalID, "the old native session must not be resumed under the new directory")
	assert.NotEmpty(t, GetForkContext("s1"), "the next turn must carry the existing history as context")
}

func TestMoveSessionToProject_SameProjectIsNoOp(t *testing.T) {
	srcPath, _, srcID, _ := setupMoveTest(t)
	_, err := store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('s1', ?, 'claude', 't')", srcID,
	)
	require.NoError(t, err)

	moved, err := MoveSessionToProject("s1", srcPath, srcID)
	require.NoError(t, err)
	assert.True(t, moved)
}

func TestMoveSessionToProject_RefusesNonConversation(t *testing.T) {
	// A scheduled session belongs to its task; a group-member row only moves
	// together with its group.
	for _, sessionType := range []string{store.SessionTypeScheduled, store.SessionTypeGroupMember} {
		t.Run(sessionType, func(t *testing.T) {
			_, dstPath, srcID, dstID := setupMoveTest(t)
			_, err := store.WriteExec(
				"INSERT INTO chat_sessions (id, project_id, backend, title, session_type) VALUES ('s1', ?, 'claude', 't', ?)",
				srcID, sessionType,
			)
			require.NoError(t, err)

			moved, err := MoveSessionToProject("s1", dstPath, dstID)
			require.NoError(t, err)
			assert.False(t, moved)

			var projectID int64
			require.NoError(t, store.ReadDB().QueryRow(
				"SELECT project_id FROM chat_sessions WHERE id = 's1'").Scan(&projectID))
			assert.Equal(t, srcID, projectID, "a refused move must not touch the row")
		})
	}
}

func TestMoveSessionToProject_RefusesGlobalScopeTarget(t *testing.T) {
	_, dstPath, srcID, _ := setupMoveTest(t)
	_, err := store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, backend, title) VALUES ('s1', ?, 'claude', 't')", srcID,
	)
	require.NoError(t, err)

	_, err = MoveSessionToProject("s1", dstPath, store.GlobalScopeProjectID)
	require.Error(t, err)
}

// seedGroupForMove inserts a group timeline row and member rows. Global agents
// are expected to be present in model.Agents (see setMoveTestAgents).
func seedGroupForMove(t *testing.T, srcID int64) {
	t.Helper()
	_, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, session_type, external_session_id)
		 VALUES ('g1', ?, 'claude', 'group', 'group', 'native-group')`, srcID)
	require.NoError(t, err)
	_, err = store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, session_type, group_id, agent_id, agent_name, external_session_id, context_state)
		 VALUES ('m1', ?, 'claude', 'Alice', 'group_member', 'g1', 'global-a', 'Alice', 'native-m1', '{"seen_cursor": 42}')`, srcID)
	require.NoError(t, err)
	// A REMOVED member: its row must follow (chat_history.agent_id points at it)
	// but its cursor is irrelevant, so it keeps whatever it had.
	_, err = store.WriteExec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, session_type, group_id, agent_id, agent_name, removed, external_session_id, context_state)
		 VALUES ('m2', ?, 'claude', 'Bob', 'group_member', 'g1', 'global-b', 'Bob', 1, 'native-m2', '{"seen_cursor": 7}')`, srcID)
	require.NoError(t, err)
	_, err = store.WriteExec(
		"INSERT INTO chat_history (project_id, role, content, session_id, backend) VALUES (?, 'user', 'group hello', 'g1', 'claude')",
		srcID)
	require.NoError(t, err)
}

func TestMoveSessionToProject_MovesWholeGroup(t *testing.T) {
	_, dstPath, srcID, dstID := setupMoveTest(t)
	setMoveTestAgents(t, map[string]*model.Agent{
		"global-a": {ID: "global-a"},
		"global-b": {ID: "global-b"},
	})
	seedGroupForMove(t, srcID)

	moved, err := MoveSessionToProject("g1", dstPath, dstID)
	require.NoError(t, err)
	assert.True(t, moved)

	// The timeline row.
	var groupProject int64
	var groupExternal string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT project_id, external_session_id FROM chat_sessions WHERE id = 'g1'").Scan(&groupProject, &groupExternal))
	assert.Equal(t, dstID, groupProject)
	assert.Empty(t, groupExternal)

	// Members: both rows moved, their old native sessions are gone, and the
	// visible member re-reads the whole timeline on its next speech.
	var memberProject int64
	var memberExternal string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT project_id, external_session_id FROM chat_sessions WHERE id = 'm1'").Scan(&memberProject, &memberExternal))
	assert.Equal(t, dstID, memberProject, "member rows must not be left under the old project")
	assert.Empty(t, memberExternal, "a member's native session was created for the old directory")
	assert.Equal(t, int64(0), memberCursor(t, "m1"), "a moved member re-reads the timeline like a newly joined one")

	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT project_id, external_session_id FROM chat_sessions WHERE id = 'm2'").Scan(&memberProject, &memberExternal))
	assert.Equal(t, dstID, memberProject, "removed members must follow too (their ids attribute past speech)")
	assert.Empty(t, memberExternal)
	assert.Equal(t, int64(7), memberCursor(t, "m2"), "a removed member's cursor is irrelevant and untouched")

	// The group timeline's messages keep rendering after the move.
	var historyProject int64
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT project_id FROM chat_history WHERE session_id = 'g1'").Scan(&historyProject))
	assert.Equal(t, dstID, historyProject)
}

func TestMoveSessionToProject_RefusesGroupWithUnavailableMembers(t *testing.T) {
	_, dstPath, srcID, dstID := setupMoveTest(t)
	setMoveTestAgents(t, map[string]*model.Agent{
		"global-a": {ID: "global-a"},
		"scoped":   {ID: "scoped", ProjectID: &srcID},
	})
	seedGroupForMove(t, srcID)
	// Point the visible member at an agent that only exists in the source
	// project: the target project's pickers cannot see it, so the group could
	// never re-add or replace that member there.
	_, err := store.WriteExec("UPDATE chat_sessions SET agent_id = 'scoped' WHERE id = 'm1'")
	require.NoError(t, err)

	_, err = MoveSessionToProject("g1", dstPath, dstID)
	require.Error(t, err)
	var unavailable *MoveUnavailableMembersError
	require.ErrorAs(t, err, &unavailable)
	assert.Equal(t, []string{"Alice"}, unavailable.Names)

	// Refused as a whole: neither the group nor any member moved.
	for _, id := range []string{"g1", "m1", "m2"} {
		var projectID int64
		require.NoError(t, store.ReadDB().QueryRow(
			"SELECT project_id FROM chat_sessions WHERE id = ?", id).Scan(&projectID))
		assert.Equal(t, srcID, projectID, "%s must stay put when the move is refused", id)
	}
}

func TestMoveSessionToProject_RemovedMemberDoesNotBlockMove(t *testing.T) {
	_, dstPath, srcID, dstID := setupMoveTest(t)
	setMoveTestAgents(t, map[string]*model.Agent{"global-a": {ID: "global-a"}})
	seedGroupForMove(t, srcID)
	// m2 is removed and its agent no longer exists; a removed member can never
	// speak again, so it must not veto the move (only its row has to follow).
	_, err := store.WriteExec("UPDATE chat_sessions SET agent_id = 'deleted-agent' WHERE id = 'm2'")
	require.NoError(t, err)

	moved, err := MoveSessionToProject("g1", dstPath, dstID)
	require.NoError(t, err)
	assert.True(t, moved)
}
