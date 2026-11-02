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

// ---------------------------------------------------------------------------
// Running-session probe
// ---------------------------------------------------------------------------

// TestAgentHasRunningSessions_EmptyAgentIsNeverRunning: the empty id is the
// "no agent" case (an untagged session). It must answer false without touching
// the database at all.
func TestAgentHasRunningSessions_EmptyAgentIsNeverRunning(t *testing.T) {
	setupTakeoverDB(t)

	running, err := service.AgentHasRunningSessions("")
	require.NoError(t, err)
	assert.False(t, running)
}

// TestAgentHasRunningSessions_ChecksEveryRunningSession pins the multi-id
// query: the placeholder list is built by hand, and a missing comma would turn
// "agent owns session B" into a SQL syntax error (or a wrong answer) as soon as
// more than one session is running.
func TestAgentHasRunningSessions_ChecksEveryRunningSession(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type) VALUES
			('busy-a', 1, 'claude', 'a', 'claude', 'chat'),
			('busy-b', 1, 'claude', 'b', 'claude', 'chat')`)
	require.NoError(t, err)

	service.SetSessionRunning("busy-a", true, true)
	service.SetSessionRunning("busy-b", true, true)
	t.Cleanup(func() {
		service.SetSessionRunning("busy-a", false, true)
		service.SetSessionRunning("busy-b", false, true)
	})

	running, err := service.AgentHasRunningSessions("claude")
	require.NoError(t, err)
	assert.True(t, running, "an agent with two live turns is running")

	running, err = service.AgentHasRunningSessions("codebuddy")
	require.NoError(t, err)
	assert.False(t, running, "a running session belongs only to its own agent")
}

// TestAgentHasRunningSessions_QueryFailureIsAnError: a broken lookup must not
// be reported as "nothing is running" — that answer authorizes a deletion that
// would rewrite a live turn's identity.
func TestAgentHasRunningSessions_QueryFailureIsAnError(t *testing.T) {
	db := setupTakeoverDB(t)

	service.SetSessionRunning("busy-orphan", true, true)
	t.Cleanup(func() { service.SetSessionRunning("busy-orphan", false, true) })

	_, err := db.Exec("DROP TABLE chat_sessions")
	require.NoError(t, err)

	running, err := service.AgentHasRunningSessions("claude")
	require.Error(t, err, "a failing query is an error, not a false")
	assert.False(t, running)
}

// ---------------------------------------------------------------------------
// Argument validation and transaction atomicity
// ---------------------------------------------------------------------------

// TestDeleteAgentWithTakeover_RequiresBothAgents: a missing side is a caller
// bug, reported before anything is written.
func TestDeleteAgentWithTakeover_RequiresBothAgents(t *testing.T) {
	db := setupTakeoverDB(t)

	cases := []struct {
		name string
		old  *model.Agent
		repl *model.Agent
	}{
		{"nil source", nil, takeoverReplacement()},
		{"source without id", &model.Agent{Name: "Claude"}, takeoverReplacement()},
		{"nil replacement", takeoverSource(), nil},
		{"replacement without id", takeoverSource(), &model.Agent{Name: "Test"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := service.DeleteAgentWithTakeover(tc.old, tc.repl)
			require.Error(t, err)
			assert.Zero(t, res.Sessions)
			assert.Zero(t, res.Groups)
		})
	}

	// Nothing was touched: the source agent is still there.
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = 'claude'").Scan(&count))
	assert.Equal(t, 1, count)
}

// takeoverFixtureSession inserts one live chat owned by the agent under test.
func takeoverFixtureSession(t *testing.T, db *sql.DB, sessionID string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type)
		VALUES (?, 1, 'claude', 'chat', 'claude', 'chat')`, sessionID)
	require.NoError(t, err)
}

// assertTakeoverRolledBack fails unless the session still names the deleted
// agent and the agent row itself survived — i.e. the transaction did nothing.
func assertTakeoverRolledBack(t *testing.T, db *sql.DB, sessionID string) {
	t.Helper()
	var agentID string
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_sessions WHERE id = ?", sessionID).Scan(&agentID))
	assert.Equal(t, "claude", agentID, "a failed takeover must not leave the session half-swapped")

	var agents int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = 'claude'").Scan(&agents))
	assert.Equal(t, 1, agents, "a failed takeover must not delete the agent")
}

// TestDeleteAgentWithTakeover_RollsBackWhenTaskRewriteFails: the sessions, the
// tasks and the agent row move in ONE transaction. If the task rewrite fails,
// the already-rewritten sessions must roll back with it — otherwise the
// conversations would name an agent that still exists under the old id while
// their tasks point at the replacement.
func TestDeleteAgentWithTakeover_RollsBackWhenTaskRewriteFails(t *testing.T) {
	db := setupTakeoverDB(t)
	takeoverFixtureSession(t, db, "s-rollback-task")

	_, err := db.Exec("DROP TABLE scheduled_tasks")
	require.NoError(t, err)

	_, err = service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reassign tasks")

	assertTakeoverRolledBack(t, db, "s-rollback-task")
}

// TestDeleteAgentWithTakeover_RollsBackWhenAgentDeleteFails: the last statement
// of the transaction can fail too (a locked/broken agents table). The session
// rewrite must not survive it.
func TestDeleteAgentWithTakeover_RollsBackWhenAgentDeleteFails(t *testing.T) {
	db := setupTakeoverDB(t)
	takeoverFixtureSession(t, db, "s-rollback-delete")

	_, err := db.Exec("DROP TABLE agents")
	require.NoError(t, err)

	_, err = service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete agent")

	var agentID string
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_sessions WHERE id = 's-rollback-delete'").Scan(&agentID))
	assert.Equal(t, "claude", agentID, "the session rewrite must roll back with the failed delete")
}

// TestDeleteAgentWithTakeover_NoticeFailureDoesNotFailDeletion: the "changed
// hands" rows are written AFTER the commit and are best-effort. A broken
// timeline table must not turn a completed takeover into an error the caller
// would report as "deletion failed" (the rows are already gone by then).
func TestDeleteAgentWithTakeover_NoticeFailureDoesNotFailDeletion(t *testing.T) {
	db := setupTakeoverDB(t)
	takeoverFixtureSession(t, db, "s-notice-fail")

	_, err := db.Exec("DROP TABLE chat_history")
	require.NoError(t, err)

	res, err := service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.NoError(t, err, "a failed notice must not fail the deletion")
	assert.Equal(t, 1, res.Sessions)

	var agentID string
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_sessions WHERE id = 's-notice-fail'").Scan(&agentID))
	assert.Equal(t, "codebuddy", agentID)

	var agents int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = 'claude'").Scan(&agents))
	assert.Zero(t, agents)
}

// ---------------------------------------------------------------------------
// Group bookkeeping edge cases
// ---------------------------------------------------------------------------

// TestDeleteAgentWithTakeover_SkipsMemberWithoutGroup: a member row whose
// group_id is empty has no timeline to announce on. Announcing on the session
// id "" would write a row nobody can ever see.
func TestDeleteAgentWithTakeover_SkipsMemberWithoutGroup(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id)
		VALUES ('m-lone', 1, 'claude', '', 'Claude', 'claude', 'group_member', '')`)
	require.NoError(t, err)

	res, err := service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.NoError(t, err)
	assert.Zero(t, res.Groups, "a member with no group contributes no group row")

	var rows int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ''").Scan(&rows))
	assert.Zero(t, rows, "nothing may be written to the empty session id")
}

// TestDeleteAgentWithTakeover_ToleratesOrphanGroup: a member can outlive its
// group row (the group was hard-deleted). Re-deriving the identity of a group
// that no longer exists must be a no-op, not a failed deletion.
func TestDeleteAgentWithTakeover_ToleratesOrphanGroup(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id)
		VALUES ('m-orphan', 1, 'claude', '', 'Claude', 'claude', 'group_member', 'ghost-group')`)
	require.NoError(t, err)

	res, err := service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Groups, "the orphaned group id is still counted as affected")

	var agentID string
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_sessions WHERE id = 'm-orphan'").Scan(&agentID))
	assert.Equal(t, "codebuddy", agentID)
}

// TestDeleteAgentWithTakeover_RecomputesGroupFromFirstMember: a group row can
// name a DIFFERENT agent than its first member (the mirrored member left and a
// new one was added, or the host changed). The blanket UPDATE only touches rows
// naming the deleted agent, so such a group is left naming a live-but-wrong
// agent — recomputeGroupIdentityTx is the only thing that fixes it, and it must
// actually write.
func TestDeleteAgentWithTakeover_RecomputesGroupFromFirstMember(t *testing.T) {
	db := setupTakeoverDB(t)
	_, err := db.Exec(`INSERT INTO agents (id, name, backend) VALUES ('other', 'Other', 'claude')`)
	require.NoError(t, err)

	// Free mode (no host pointer). The group row mirrors "other", but the FIRST
	// member (by rowid) is the agent being deleted.
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type, context_state)
		VALUES ('g-drift', 1, 'claude', 'group', 'other', 'group', '{"group_mode":"free"}')`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id)
		VALUES ('m-drift-first', 1, 'claude', '', 'Claude', 'claude', 'group_member', 'g-drift')`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id)
		VALUES ('m-drift-second', 1, 'claude', '', 'Other', 'other', 'group_member', 'g-drift')`)
	require.NoError(t, err)

	res, err := service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Groups)

	var agentID, backend string
	require.NoError(t, db.QueryRow(
		"SELECT agent_id, backend FROM chat_sessions WHERE id = 'g-drift'").Scan(&agentID, &backend))
	assert.Equal(t, "codebuddy", agentID, "the group must follow its first member, not keep naming a stale agent")
	assert.Equal(t, "codebuddy", backend)

	// The surviving member keeps its own identity.
	require.NoError(t, db.QueryRow(
		"SELECT agent_id FROM chat_sessions WHERE id = 'm-drift-second'").Scan(&agentID))
	assert.Equal(t, "other", agentID)
}

// TestDeleteAgentWithTakeover_ToleratesVanishedHostMember: a host pointer can
// dangle (the host member row was removed). That group has no identity to
// re-derive, so it must be skipped silently — and it already follows the
// blanket UPDATE.
func TestDeleteAgentWithTakeover_ToleratesVanishedHostMember(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type, context_state)
		VALUES ('g-no-host', 1, 'claude', 'group', 'claude', 'group',
		        '{"group_mode":"host","host_member_id":"m-gone"}')`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id)
		VALUES ('m-present', 1, 'claude', '', 'Claude', 'claude', 'group_member', 'g-no-host')`)
	require.NoError(t, err)

	res, err := service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Groups)

	var agentID string
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_sessions WHERE id = 'g-no-host'").Scan(&agentID))
	assert.Equal(t, "codebuddy", agentID, "the group still moved with the blanket rewrite")
}

// ---------------------------------------------------------------------------
// Failure propagation from the transaction's own queries
// ---------------------------------------------------------------------------

// TestDeleteAgentWithTakeover_RunningCheckFailureIsWrapped: the in-transaction
// re-check must report a broken query instead of assuming "nothing is running"
// (the pre-check already passed, so a silent false here would rewrite a live
// turn).
func TestDeleteAgentWithTakeover_RunningCheckFailureIsWrapped(t *testing.T) {
	db := setupTakeoverDB(t)

	service.SetSessionRunning("busy-wrapped", true, true)
	t.Cleanup(func() { service.SetSessionRunning("busy-wrapped", false, true) })

	_, err := db.Exec("DROP TABLE chat_sessions")
	require.NoError(t, err)

	_, err = service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check running sessions")
}

// TestDeleteAgentWithTakeover_NoticeQueryFailureIsWrapped: with nothing
// running, the next statement is the notice query. Its failure must abort the
// transaction (no half-done takeover) and name the step that broke.
func TestDeleteAgentWithTakeover_NoticeQueryFailureIsWrapped(t *testing.T) {
	db := setupTakeoverDB(t)

	require.Empty(t, service.GetRunningSessionIDs(), "precondition: no other test left a session running")

	_, err := db.Exec("DROP TABLE chat_sessions")
	require.NoError(t, err)

	_, err = service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list takeover notices")

	var agents int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = 'claude'").Scan(&agents))
	assert.Equal(t, 1, agents)
}

// TestDeleteAgentWithTakeover_SessionRewriteFailureIsWrapped uses a trigger to
// make the session rewrite fail, which is the one way to reach that branch
// without a corrupt database. The error must name the step and the transaction
// must leave everything as it was.
func TestDeleteAgentWithTakeover_SessionRewriteFailureIsWrapped(t *testing.T) {
	db := setupTakeoverDB(t)
	takeoverFixtureSession(t, db, "s-rewrite-fail")

	_, err := db.Exec(`
		CREATE TRIGGER block_session_rewrite BEFORE UPDATE ON chat_sessions
		BEGIN SELECT RAISE(ABORT, 'session rewrite blocked'); END`)
	require.NoError(t, err)

	_, err = service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reassign sessions")
	assert.Contains(t, err.Error(), "session rewrite blocked")

	assertTakeoverRolledBack(t, db, "s-rewrite-fail")
}

// TestDeleteAgentWithTakeover_ClosedDatabaseIsAnError: the write lock cannot be
// taken on a closed handle. That must surface as an error rather than a panic
// or a "nothing to do" result.
func TestDeleteAgentWithTakeover_ClosedDatabaseIsAnError(t *testing.T) {
	db := setupTakeoverDB(t)
	require.NoError(t, db.Close())

	_, err := service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.Error(t, err)
}

// TestDeleteAgentWithTakeover_GroupIdentityReadFailureIsWrapped: a group row
// with an unparseable context_state makes json_extract fail. The takeover must
// abort (and roll back) instead of leaving the group naming the deleted agent —
// this is the one failure the user cannot fix from the UI afterwards.
//
// The group row names ANOTHER agent on purpose: the blanket rewrite would fail
// on the same bad JSON first (it calls json_remove), so only a group the
// blanket UPDATE skips reaches the identity read.
func TestDeleteAgentWithTakeover_GroupIdentityReadFailureIsWrapped(t *testing.T) {
	db := setupTakeoverDB(t)
	_, err := db.Exec(`INSERT INTO agents (id, name, backend) VALUES ('other', 'Other', 'claude')`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type, context_state)
		VALUES ('g-corrupt', 1, 'claude', 'group', 'other', 'group', 'not json')`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id)
		VALUES ('m-corrupt', 1, 'claude', '', 'Claude', 'claude', 'group_member', 'g-corrupt')`)
	require.NoError(t, err)

	_, err = service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read group g-corrupt identity")

	// Rolled back: the group and its member still belong to the deleted agent.
	var agentID string
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_sessions WHERE id = 'm-corrupt'").Scan(&agentID))
	assert.Equal(t, "claude", agentID)
}

// TestDeleteAgentWithTakeover_GroupIdentityWriteFailureIsWrapped: re-deriving a
// group's identity is a write inside the takeover transaction, so its failure
// must roll the whole deletion back (a half-applied takeover would leave the
// group pointing at an agent that no longer exists).
func TestDeleteAgentWithTakeover_GroupIdentityWriteFailureIsWrapped(t *testing.T) {
	db := setupTakeoverDB(t)
	_, err := db.Exec(`INSERT INTO agents (id, name, backend) VALUES ('other', 'Other', 'claude')`)
	require.NoError(t, err)

	// The group row names "other" while its first member is the deleted agent,
	// so recomputeGroupIdentityTx must write.
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type, context_state)
		VALUES ('g-write', 1, 'claude', 'group', 'other', 'group', '{"group_mode":"free"}')`)
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id)
		VALUES ('m-write', 1, 'claude', '', 'Claude', 'claude', 'group_member', 'g-write')`)
	require.NoError(t, err)

	// Block only the identity rewrite: the blanket UPDATE changes
	// agent_takeover_from, this one does not.
	_, err = db.Exec(`
		CREATE TRIGGER block_group_identity BEFORE UPDATE ON chat_sessions
		WHEN NEW.id = 'g-write' AND NEW.agent_takeover_from IS OLD.agent_takeover_from
		BEGIN SELECT RAISE(ABORT, 'group identity blocked'); END`)
	require.NoError(t, err)

	_, err = service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update group g-write identity")
	assert.Contains(t, err.Error(), "group identity blocked")

	var agentID string
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_sessions WHERE id = 'g-write'").Scan(&agentID))
	assert.Equal(t, "other", agentID, "the group must roll back with the failed takeover")

	var agents int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = 'claude'").Scan(&agents))
	assert.Equal(t, 1, agents)
}

// TestDeleteAgentWithTakeover_CorruptArchivedFlagFailsLoudly: the notice query
// scans `archived` into an int, and SQLite's dynamic typing lets a TEXT value
// sit in that column. Such a row cannot be classified as live or archived, so
// the takeover must fail instead of guessing — guessing wrong either skips a
// notice the user needed or writes to an archived timeline.
func TestDeleteAgentWithTakeover_CorruptArchivedFlagFailsLoudly(t *testing.T) {
	db := setupTakeoverDB(t)

	_, err := db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type, archived)
		VALUES ('s-corrupt-flag', 1, 'claude', 'chat', 'claude', 'chat', 'not-a-flag')`)
	require.NoError(t, err)

	_, err = service.DeleteAgentWithTakeover(takeoverSource(), takeoverReplacement())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan takeover notice")

	assertTakeoverRolledBack(t, db, "s-corrupt-flag")
}
