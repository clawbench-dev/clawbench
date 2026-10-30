package service_test

import (
	"database/sql"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTakeoverDB builds the production-shaped fixture the takeover path touches:
// the chat/session/task tables from `schema` plus the real agents DDL (the
// deletion removes the agent row) and one project so the "changed hands" system
// rows can be persisted (AddSystemMessage resolves a project id from the path).
func setupTakeoverDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupDB(t)
	_, err := db.Exec(service.AgentDDL)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO projects (id, path) VALUES (1, '/tmp/takeover')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO agents (id, name, backend) VALUES ('claude', 'Claude', 'claude')`)
	require.NoError(t, err)
	return db
}

func takeoverReplacement() *model.Agent {
	return &model.Agent{ID: "codebuddy", Name: "Test", Backend: "codebuddy"}
}

// takeoverSource is the agent being deleted: a DIFFERENT backend from
// takeoverReplacement, so the native session cannot be reused.
func takeoverSource() *model.Agent {
	return &model.Agent{ID: "claude", Name: "Claude", Backend: "claude"}
}

// TestDeleteAgentWithTakeover_RebindsSessionState is the core contract: the row
// keeps its history but loses every piece of state that described the OLD agent.
func TestDeleteAgentWithTakeover_RebindsSessionState(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO chat_sessions
			(id, project_id, backend, title, agent_id, agent_source, model, transport,
			 external_session_id, context_state, session_type, archived)
		VALUES ('s1', 1, 'claude', 'chat', 'claude', 'user', 'claude-sonnet-4-6', 'cli',
		        'ext-1', '{"mode":"plan","thinkingEffort":{"currentId":"high"},"usage":{"used":42},"keep":"me"}', 'chat', 0)`)
	require.NoError(t, err)

	res, err := service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Sessions)
	assert.Equal(t, 0, res.Tasks)
	assert.Equal(t, 0, res.Groups)

	var agentID, backend, source, modelName, transport, extID, takeoverFrom, ctx string
	require.NoError(t, db.QueryRow(`
		SELECT agent_id, backend, agent_source, model, transport, external_session_id,
		       agent_takeover_from, context_state
		  FROM chat_sessions WHERE id = 's1'`).
		Scan(&agentID, &backend, &source, &modelName, &transport, &extID, &takeoverFrom, &ctx))

	assert.Equal(t, "codebuddy", agentID)
	assert.Equal(t, "codebuddy", backend)
	assert.Equal(t, "default", source)
	// A model id / transport / native session id all describe the deleted agent.
	assert.Empty(t, modelName)
	assert.Empty(t, transport)
	assert.Empty(t, extID)
	// The OLD agent's name: this is the badge text and the turn-path switch.
	assert.Equal(t, "Claude", takeoverFrom)
	// mode / thinkingEffort are the old capability set; usage is the old window.
	// The unrelated key must SURVIVE — that is what separates a targeted
	// json_remove from a blunt "wipe the column" that would also pass above.
	assert.NotContains(t, ctx, "mode")
	assert.NotContains(t, ctx, "thinkingEffort")
	assert.NotContains(t, ctx, "usage")
	assert.Contains(t, ctx, `"keep":"me"`)

	// The agent itself is gone.
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = 'claude'").Scan(&count))
	assert.Zero(t, count)
}

// TestDeleteAgentWithTakeover_RecomputesGroupIdentity pins the trap: a group row
// mirrors its host (host mode) or first member (free mode), so rebinding the
// member row alone would leave the group naming a deleted agent.
func TestDeleteAgentWithTakeover_RecomputesGroupIdentity(t *testing.T) {
	db := setupTakeoverDB(t)
	_, err := db.Exec(`INSERT INTO agents (id, name, backend) VALUES ('other', 'Other', 'claude')`)
	require.NoError(t, err)

	// Host mode: g1's host is m1 (the deleted agent).
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type, context_state)
		VALUES ('g1', 1, 'claude', 'group', 'claude', 'group', '{"group_mode":"host","host_member_id":"m1"}')`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id)
		VALUES ('m1', 1, 'claude', '', 'Claude', 'claude', 'group_member', 'g1')`)
	require.NoError(t, err)

	// Free mode: g2 has no host pointer, so its row follows the FIRST member —
	// which is the deleted agent's row. Two members, inserted in the order
	// "deleted agent first", and their ids are chosen so a lexical ORDER BY id
	// would pick the OTHER member: only insertion order (rowid) matches what
	// groupIdentityFields did at creation time.
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type, context_state)
		VALUES ('g2', 1, 'claude', 'group2', 'claude', 'group', '{"group_mode":"free"}')`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id)
		VALUES ('zzz-first', 1, 'claude', '', 'Claude', 'claude', 'group_member', 'g2')`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id)
		VALUES ('aaa-second', 1, 'claude', '', 'Other', 'other', 'group_member', 'g2')`)
	require.NoError(t, err)

	res, err := service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.NoError(t, err)
	assert.Equal(t, 2, res.Groups)

	for _, groupID := range []string{"g1", "g2"} {
		var agentID, backend string
		require.NoError(t, db.QueryRow(
			"SELECT agent_id, backend FROM chat_sessions WHERE id = ?", groupID,
		).Scan(&agentID, &backend))
		assert.Equal(t, "codebuddy", agentID, groupID)
		assert.Equal(t, "codebuddy", backend, groupID)
	}

	// The member instance keeps its own display name — renaming it would rewrite
	// the group's attribution and could collide with an existing instance.
	var memberName string
	require.NoError(t, db.QueryRow("SELECT agent_name FROM chat_sessions WHERE id = 'm1'").Scan(&memberName))
	assert.Equal(t, "Claude", memberName)
}

func TestDeleteAgentWithTakeover_ReassignsTasks(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO scheduled_tasks (project_id, name, cron_expr, agent_id, prompt)
		VALUES (1, 'daily', '0 8 * * *', 'claude', 'do it')`)
	require.NoError(t, err)

	res, err := service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Tasks)

	var agentID string
	require.NoError(t, db.QueryRow("SELECT agent_id FROM scheduled_tasks WHERE name = 'daily'").Scan(&agentID))
	assert.Equal(t, "codebuddy", agentID)
}

// TestDeleteAgentWithTakeover_RefusesRunningSession is the ONE refusal: a live
// turn must not have its identity rewritten underneath it.
func TestDeleteAgentWithTakeover_RefusesRunningSession(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type)
		VALUES ('s1', 1, 'claude', 'chat', 'claude', 'chat')`)
	require.NoError(t, err)

	service.SetSessionRunning("s1", true, true)
	defer service.SetSessionRunning("s1", false, true)

	_, err = service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.ErrorIs(t, err, service.ErrAgentHasRunningSessions)

	// Nothing moved, and the agent row survives.
	var agentID string
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_sessions WHERE id = 's1'").Scan(&agentID))
	assert.Equal(t, "claude", agentID)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = 'claude'").Scan(&count))
	assert.Equal(t, 1, count)
}

// TestDeleteAgentWithTakeover_WritesTimelineNotices covers both notice shapes:
// a single chat gets its own row, a group membership announces itself on the
// GROUP timeline (member rows have no timeline of their own).
func TestDeleteAgentWithTakeover_WritesTimelineNotices(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type)
		VALUES ('s1', 1, 'claude', 'chat', 'claude', 'chat')`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type, context_state)
		VALUES ('g1', 1, 'claude', 'group', 'claude', 'group', '{"group_mode":"host","host_member_id":"m1"}')`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id)
		VALUES ('m1', 1, 'claude', '', 'Claude', 'claude', 'group_member', 'g1')`)
	require.NoError(t, err)

	// Pin the locale so the two wordings are distinguishable at all: they share
	// both names, so asserting on "Claude"/"Test" alone would pass even if the
	// wrong branch wrote to the wrong timeline.
	prevLang := model.Language
	model.Language = "zh"
	t.Cleanup(func() { model.Language = prevLang })

	_, err = service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.NoError(t, err)

	// One row per timeline, both role='system' (rendered centered, never as a
	// user bubble) and both naming the replacement.
	for _, sessionID := range []string{"s1", "g1"} {
		var role, content string
		require.NoError(t, db.QueryRow(
			`SELECT role, content FROM chat_history WHERE session_id = ? ORDER BY id DESC LIMIT 1`,
			sessionID,
		).Scan(&role, &content), sessionID)
		assert.Equal(t, "system", role, sessionID)
		assert.Contains(t, content, "Test", sessionID)
	}

	// The single chat gets the "this conversation" wording; the group gets the
	// "this member" wording on the GROUP timeline (member rows have none).
	var chatNotice, groupNotice string
	require.NoError(t, db.QueryRow(
		`SELECT content FROM chat_history WHERE session_id = 's1' ORDER BY id DESC LIMIT 1`,
	).Scan(&chatNotice))
	require.NoError(t, db.QueryRow(
		`SELECT content FROM chat_history WHERE session_id = 'g1' ORDER BY id DESC LIMIT 1`,
	).Scan(&groupNotice))
	assert.Contains(t, chatNotice, "本会话", "1:1 notice must use the conversation wording")
	assert.NotContains(t, chatNotice, "驱动")
	assert.Contains(t, groupNotice, "驱动", "membership notice must use the member wording")
	assert.Contains(t, groupNotice, "Claude")
}

// An archived chat is skipped: AddSystemMessage refuses archived sessions, and
// nobody is looking at that timeline anyway. The row itself still moves.
func TestDeleteAgentWithTakeover_SkipsArchivedNotices(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type, archived)
		VALUES ('s1', 1, 'claude', 'old', 'claude', 'chat', 1)`)
	require.NoError(t, err)

	_, err = service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = 's1'").Scan(&count))
	assert.Zero(t, count)

	var agentID string
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_sessions WHERE id = 's1'").Scan(&agentID))
	assert.Equal(t, "codebuddy", agentID)
}

// TestDeleteAgentWithTakeover_ReusesNativeSessionForSameBackend covers the
// "same AI backend" shortcut: the native session id still names a real session
// in that CLI, so it is kept and the next turn resumes instead of rebuilding the
// whole conversation from chat_history.
func TestDeleteAgentWithTakeover_ReusesNativeSessionForSameBackend(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type,
		                           external_session_id, compacted)
		VALUES ('s1', 1, 'codebuddy', 'chat', 'cb-old', 'chat', 'native-1', 0)`)
	require.NoError(t, err)

	prevLang := model.Language
	model.Language = "zh"
	t.Cleanup(func() { model.Language = prevLang })

	old := &model.Agent{ID: "cb-old", Name: "Old Buddy", Backend: "codebuddy"}
	replacement := &model.Agent{ID: "cb-new", Name: "New Buddy", Backend: "codebuddy"}

	_, err = service.DeleteAgentWithTakeover(old, replacement)
	require.NoError(t, err)

	var agentID, extID, takeoverFrom string
	var compacted int
	require.NoError(t, db.QueryRow(
		`SELECT agent_id, external_session_id, agent_takeover_from, compacted
		   FROM chat_sessions WHERE id = 's1'`,
	).Scan(&agentID, &extID, &takeoverFrom, &compacted))

	assert.Equal(t, "cb-new", agentID)
	// The id survives: it is still a valid session in the same CLI.
	assert.Equal(t, "native-1", extID)
	assert.Equal(t, "Old Buddy", takeoverFrom)
	// A resume skips the system prompt, so the flag forces one re-announcement of
	// the replacement's own instructions.
	assert.Equal(t, 1, compacted)

	// The notice must say the session was REUSED, not that the history was
	// re-injected — otherwise the user goes looking for an injection that never
	// happened.
	var notice string
	require.NoError(t, db.QueryRow(
		`SELECT content FROM chat_history WHERE session_id = 's1' ORDER BY id DESC LIMIT 1`,
	).Scan(&notice))
	assert.Contains(t, notice, "沿用")
	assert.NotContains(t, notice, "重新注入")
}

// A different backend must NOT keep the id: the session lives in the other
// tool's storage and would fail (or silently attach to the wrong transcript).
func TestDeleteAgentWithTakeover_ClearsNativeSessionAcrossBackends(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type,
		                           external_session_id, compacted)
		VALUES ('s1', 1, 'claude', 'chat', 'claude', 'chat', 'native-1', 0)`)
	require.NoError(t, err)

	_, err = service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.NoError(t, err)

	var extID string
	var compacted int
	require.NoError(t, db.QueryRow(
		"SELECT external_session_id, compacted FROM chat_sessions WHERE id = 's1'",
	).Scan(&extID, &compacted))
	assert.Empty(t, extID, "a cross-backend takeover must rebuild from chat_history")
	// The rebuild path sends the system prompt anyway (resume=false), so the flag
	// must stay untouched — setting it would be a lie about a compaction.
	assert.Zero(t, compacted)
}

// A session that carried its own transport override may have created its native
// session in the OTHER mode (a CLI id from an ACP adapter, or the reverse), which
// the replacement cannot resume. Those rows must be rebuilt even on the same
// backend.
func TestDeleteAgentWithTakeover_ClearsNativeSessionWithTransportOverride(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type,
		                           transport, external_session_id, compacted)
		VALUES ('s1', 1, 'codebuddy', 'chat', 'cb-old', 'chat', 'acp-stdio', 'native-1', 0)`)
	require.NoError(t, err)

	old := &model.Agent{ID: "cb-old", Name: "Old Buddy", Backend: "codebuddy"}
	replacement := &model.Agent{ID: "cb-new", Name: "New Buddy", Backend: "codebuddy"}
	_, err = service.DeleteAgentWithTakeover(old, replacement)
	require.NoError(t, err)

	var extID, transport string
	var compacted int
	require.NoError(t, db.QueryRow(
		"SELECT external_session_id, transport, compacted FROM chat_sessions WHERE id = 's1'",
	).Scan(&extID, &transport, &compacted))
	assert.Empty(t, extID)
	assert.Empty(t, transport)
	assert.Zero(t, compacted)
}
