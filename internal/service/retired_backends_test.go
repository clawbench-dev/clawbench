package service

import (
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The agents of a removed backend cannot start a turn any more, so the upgrade
// deletes them. Their conversations must survive: they are handed to a
// surviving agent exactly like DELETE /api/agents does, rather than being left
// with an agent id that matches nothing.
func TestMigrateRemoveRetiredBackends_HandsOverAndDeletes(t *testing.T) {
	setupTransportMigrationDB(t)

	_, err := store.WriteExec(`INSERT INTO projects (id, path) VALUES (1, '/proj')`)
	require.NoError(t, err)
	_, err = store.WriteExec(`INSERT INTO agents (id, name, backend, sort_order, acp_command) VALUES
		('vecli-agent', 'VeCLI', 'vecli', 1, ''),
		('claude-agent', 'Claude', 'claude', 3, 'claude --acp')`)
	require.NoError(t, err)
	_, err = store.WriteExec(`INSERT INTO chat_sessions
			(id, project_id, backend, title, agent_id, model, external_session_id, context_state)
		VALUES
			('s-vecli', 1, 'vecli', 'chat', 'vecli-agent', 'doubao', 'native-1', '{"mode":"plan","keep":"me"}'),
			('s-claude', 1, 'claude', 'chat', 'claude-agent', 'sonnet', 'native-2', '')`)
	require.NoError(t, err)
	_, err = store.WriteExec(`INSERT INTO scheduled_tasks (project_id, name, cron_expr, agent_id, prompt)
		VALUES (1, 'daily', '0 9 * * *', 'vecli-agent', 'summarize')`)
	require.NoError(t, err)

	require.True(t, MigrateRemoveRetiredBackends())

	var retired int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM agents WHERE backend = 'vecli'",
	).Scan(&retired))
	assert.Zero(t, retired, "the retired-backend agent must be gone")

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

	handed := sessionOf("s-vecli")
	assert.Equal(t, "claude-agent", handed.agentID)
	assert.Equal(t, "claude", handed.backend)
	assert.Empty(t, handed.model, "a model id only means something to the backend that issued it")
	assert.Empty(t, handed.externalID, "the native session belonged to the deleted process")
	assert.Equal(t, "VeCLI", handed.takeoverFrom, "the badge names the agent that went away")
	assert.NotContains(t, handed.contextState, `"mode"`)
	assert.Contains(t, handed.contextState, `"keep":"me"`, "unrelated context keys survive")

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

// copilot is NOT retired: `copilot --acp` was verified against the real CLI, so
// an upgraded install keeps its agents. They are fixed up by the spec sync
// (which writes acp_command), not deleted.
func TestMigrateRemoveRetiredBackends_KeepsCopilot(t *testing.T) {
	setupTransportMigrationDB(t)

	_, err := store.WriteExec(`INSERT INTO projects (id, path) VALUES (1, '/proj')`)
	require.NoError(t, err)
	_, err = store.WriteExec(`INSERT INTO agents (id, name, backend, sort_order, acp_command) VALUES
		('copilot-agent', 'Copilot', 'copilot', 1, '')`)
	require.NoError(t, err)
	_, err = store.WriteExec(`INSERT INTO chat_sessions
			(id, project_id, backend, title, agent_id, model)
		VALUES ('s-copilot', 1, 'copilot', 'chat', 'copilot-agent', 'gpt-5')`)
	require.NoError(t, err)

	require.True(t, MigrateRemoveRetiredBackends())

	var n int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM agents WHERE id = 'copilot-agent'",
	).Scan(&n))
	assert.Equal(t, 1, n, "copilot is a live backend again; its agent must survive")

	var agentID, model string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT agent_id, model FROM chat_sessions WHERE id = 's-copilot'",
	).Scan(&agentID, &model))
	assert.Equal(t, "copilot-agent", agentID)
	assert.Equal(t, "gpt-5", model)
}

// A surviving agent that cannot start a turn is not a valid takeover target.
// Handing the conversations to it would replace one permanently broken agent
// with another — the exact outcome this migration exists to prevent.
func TestMigrateRemoveRetiredBackends_SkipsUnrunnableSurvivor(t *testing.T) {
	setupTransportMigrationDB(t)

	_, err := store.WriteExec(`INSERT INTO projects (id, path) VALUES (1, '/proj')`)
	require.NoError(t, err)
	// "broken" sorts first but has neither an ACP command nor the in-process
	// implementation, so it can never run a turn.
	_, err = store.WriteExec(`INSERT INTO agents (id, name, backend, sort_order, acp_command) VALUES
		('vecli-agent', 'VeCLI', 'vecli', 1, ''),
		('broken', 'Broken', 'claude', 2, ''),
		('runnable', 'Runnable', 'claude', 3, 'claude --acp')`)
	require.NoError(t, err)
	_, err = store.WriteExec(`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id)
		VALUES ('s-vecli', 1, 'vecli', 'chat', 'vecli-agent')`)
	require.NoError(t, err)

	require.True(t, MigrateRemoveRetiredBackends())

	var agentID string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT agent_id FROM chat_sessions WHERE id = 's-vecli'",
	).Scan(&agentID))
	assert.Equal(t, "runnable", agentID, "the takeover target must be able to run a turn")
}

// With nothing to hand the conversations to, deleting would strand every
// session on a dangling agent id. The migration reports "not done" so the
// ledger retries on a later start instead of pretending the cleanup finished.
func TestMigrateRemoveRetiredBackends_NoSurvivorLeavesRows(t *testing.T) {
	setupTransportMigrationDB(t)

	_, err := store.WriteExec(`INSERT INTO agents (id, name, backend) VALUES ('vecli-agent', 'VeCLI', 'vecli')`)
	require.NoError(t, err)

	require.False(t, MigrateRemoveRetiredBackends())

	var n int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM agents WHERE id = 'vecli-agent'",
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
