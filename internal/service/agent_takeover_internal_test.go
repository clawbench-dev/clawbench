package service

import (
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentHasRunningSessions_SeesActiveGroupMember covers the branch that no
// exported API can reach: a group turn registers its MEMBER rows, while the
// group ROW's agent_id is the host. A plain member is therefore invisible to
// GetRunningSessionIDs, and only the orchestrator's active-member map can prove
// it is mid-turn. Missing it would let a deletion rewrite the identity of a
// member that is speaking right now.
func TestAgentHasRunningSessions_SeesActiveGroupMember(t *testing.T) {
	db, err := InitInMemoryDB()
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, agent_id, session_type, group_id)
		VALUES ('m1', 'claude', 'group_member', 'g1')`)
	require.NoError(t, err)

	markGroupMembersActive([]string{"m1"})
	t.Cleanup(func() { clearGroupMembersActive([]string{"m1"}) })

	running, err := AgentHasRunningSessions("claude")
	require.NoError(t, err)
	assert.True(t, running, "an active member row must count as running")

	running, err = AgentHasRunningSessions("codebuddy")
	require.NoError(t, err)
	assert.False(t, running, "an unrelated agent must stay deletable")

	// Once the turn ends the same agent is deletable again.
	clearGroupMembersActive([]string{"m1"})
	running, err = AgentHasRunningSessions("claude")
	require.NoError(t, err)
	assert.False(t, running)
}

// TestBuildChatRequest_ForcesFreshSessionAfterTakeover pins the turn-path branch.
//
// A taken-over session has no native session of its own (external_session_id was
// cleared) but DOES have history, which normally makes resume true and lets the
// CLI fall back to --continue — attaching the NEW agent to whatever that CLI ran
// last in the working directory. The takeover flag must override that.
func TestBuildChatRequest_ForcesFreshSessionAfterTakeover(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	seed := func(sessionID, takeoverFrom string) {
		_, err := db.Exec(`
			INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type,
			                           external_session_id, agent_takeover_from)
			VALUES (?, 1, 'codebuddy', 'T', 'codebuddy', 'chat', '', ?)`,
			sessionID, takeoverFrom)
		require.NoError(t, err)
		_, err = db.Exec(
			`INSERT INTO chat_history (project_id, session_id, role, content, backend) VALUES (1, ?, 'user', 'hi', 'codebuddy')`,
			sessionID)
		require.NoError(t, err)
		_, err = db.Exec(
			`INSERT INTO chat_history (project_id, session_id, role, content, backend) VALUES (1, ?, 'assistant', '{"blocks":[{"type":"text","text":"hello"}]}', 'codebuddy')`,
			sessionID)
		require.NoError(t, err)
	}

	// Control: history but no native id and NO takeover → resume stays true (the
	// legacy path, which some CLIs answer with --continue).
	seed("plain", "")
	plain := BuildChatRequest("next", "plain", "/proj", "codebuddy", "codebuddy", "", "", "", "", "/proj", false)
	assert.True(t, plain.Resume)
	assert.NotEmpty(t, plain.ForkContext, "history must still be injected as background")

	// Taken over: identical shape, but the replacement must start a fresh
	// native session while still receiving the history.
	seed("taken", "Claude")
	taken := BuildChatRequest("next", "taken", "/proj", "codebuddy", "codebuddy", "", "", "", "", "/proj", false)
	assert.False(t, taken.Resume, "a taken-over session must not resume a native session")
	assert.NotEmpty(t, taken.ForkContext)
}
