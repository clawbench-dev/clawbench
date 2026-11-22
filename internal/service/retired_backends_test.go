package service

import (
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The agents of the two removed backends cannot start a turn any more, so the
// upgrade deletes them. Their conversations must survive: they are handed to a
// surviving agent exactly like DELETE /api/agents does, rather than being left
// with an agent id that matches nothing.
func TestMigrateRemoveRetiredBackends_HandsOverAndDeletes(t *testing.T) {
	setupTransportMigrationDB(t)

	_, err := store.WriteExec(`INSERT INTO projects (id, path) VALUES (1, '/proj')`)
	require.NoError(t, err)
	_, err = store.WriteExec(`INSERT INTO agents (id, name, backend, sort_order) VALUES
		('copilot-agent', 'Copilot', 'copilot', 1),
		('vecli-agent', 'VeCLI', 'vecli', 2),
		('claude-agent', 'Claude', 'claude', 3)`)
	require.NoError(t, err)
	_, err = store.WriteExec(`INSERT INTO chat_sessions
			(id, project_id, backend, title, agent_id, model, external_session_id, context_state)
		VALUES
			('s-copilot', 1, 'copilot', 'chat', 'copilot-agent', 'gpt-5', 'native-1', '{"mode":"plan","keep":"me"}'),
			('s-vecli', 1, 'vecli', 'chat', 'vecli-agent', 'doubao', '', ''),
			('s-claude', 1, 'claude', 'chat', 'claude-agent', 'sonnet', 'native-2', '')`)
	require.NoError(t, err)
	_, err = store.WriteExec(`INSERT INTO scheduled_tasks (project_id, name, cron_expr, agent_id, prompt)
		VALUES (1, 'daily', '0 9 * * *', 'copilot-agent', 'summarize')`)
	require.NoError(t, err)

	require.True(t, MigrateRemoveRetiredBackends())

	var retired int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM agents WHERE backend IN ('vecli','copilot')",
	).Scan(&retired))
	assert.Zero(t, retired, "both retired-backend agents must be gone")

	var exists int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM agents WHERE id = 'claude-agent'",
	).Scan(&exists))
	assert.Equal(t, 1, exists, "a live backend must not be touched")

	type sessionRow struct {
		agentID, backend, model, externalID, takeoverFrom, contextState string
	}
	sessionOf := func(id string) sessionRow {
		var r sessionRow
		require.NoError(t, store.ReadDB().QueryRow(`
			SELECT agent_id, backend, model, external_session_id, agent_takeover_from, context_state
			  FROM chat_sessions WHERE id = ?`, id).
			Scan(&r.agentID, &r.backend, &r.model, &r.externalID, &r.takeoverFrom, &r.contextState))
		return r
	}

	handed := sessionOf("s-copilot")
	assert.Equal(t, "claude-agent", handed.agentID)
	assert.Equal(t, "claude", handed.backend)
	assert.Empty(t, handed.model, "a model id only means something to the backend that issued it")
	assert.Empty(t, handed.externalID, "the native session belonged to the deleted process")
	assert.Equal(t, "Copilot", handed.takeoverFrom, "the badge names the agent that went away")
	assert.NotContains(t, handed.contextState, `"mode"`)
	assert.Contains(t, handed.contextState, `"keep":"me"`, "unrelated context keys survive")

	assert.Equal(t, "claude-agent", sessionOf("s-vecli").agentID)

	untouched := sessionOf("s-claude")
	assert.Equal(t, "claude-agent", untouched.agentID)
	assert.Equal(t, "sonnet", untouched.model)
	assert.Equal(t, "native-2", untouched.externalID)
	assert.Empty(t, untouched.takeoverFrom)

	var taskAgent string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT agent_id FROM scheduled_tasks WHERE name = 'daily'",
	).Scan(&taskAgent))
	assert.Equal(t, "claude-agent", taskAgent, "a task left on a deleted agent would fail every run")
}

// With nothing to hand the conversations to, deleting would strand every
// session on a dangling agent id. The migration reports "not done" so the
// ledger retries on a later start instead of pretending the cleanup finished.
func TestMigrateRemoveRetiredBackends_NoSurvivorLeavesRows(t *testing.T) {
	setupTransportMigrationDB(t)

	_, err := store.WriteExec(`INSERT INTO agents (id, name, backend) VALUES ('copilot-agent', 'Copilot', 'copilot')`)
	require.NoError(t, err)

	require.False(t, MigrateRemoveRetiredBackends())

	var n int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM agents WHERE id = 'copilot-agent'",
	).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestMigrateRemoveRetiredBackends_NoRetiredAgentsIsNoop(t *testing.T) {
	setupTransportMigrationDB(t)

	_, err := store.WriteExec(`INSERT INTO agents (id, name, backend) VALUES ('claude-agent', 'Claude', 'claude')`)
	require.NoError(t, err)

	require.True(t, MigrateRemoveRetiredBackends())

	var n int
	require.NoError(t, store.ReadDB().QueryRow("SELECT COUNT(*) FROM agents").Scan(&n))
	assert.Equal(t, 1, n)
}
