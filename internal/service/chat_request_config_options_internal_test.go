package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// resolveConfigOptionPicks is the turn-start source of the agent's generic
// config options (issue #746): the session's own pick wins, and the per-model
// default remembered on the agent is the fallback that makes a NEW session
// start with the user's last choice instead of the agent's own default.
func TestResolveConfigOptionPicks_SessionPickWinsOverModelDefault(t *testing.T) {
	db, err := InitInMemoryDB()
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	agent := &model.Agent{
		ID: "cfg-pick-agent", Backend: "codex", AcpCommand: "codex",
		Models: []model.AgentModel{{ID: "m1", Default: true}},
	}
	// ReplaceAgents is the only supported way to publish a set; the global map
	// starts nil in a fresh test binary.
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})
	t.Cleanup(func() { model.ReplaceAgents(map[string]*model.Agent{}, nil) })
	agent.SetConfigOptionsForModel("m1", map[string]string{"fast-mode": "off"})

	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, model)
		VALUES ('cfg-pick-session', 0, 'codex', 't', 'cfg-pick-agent', 'm1')`)
	require.NoError(t, err)

	// No session pick yet: the remembered per-model default applies.
	picks := resolveConfigOptionPicks("cfg-pick-session", agent.ID, "m1")
	require.Equal(t, "off", picks["fast-mode"])

	// A pick made in this session wins from then on.
	PersistConfigOptionPicks("cfg-pick-session", map[string]string{"fast-mode": "on"})
	picks = resolveConfigOptionPicks("cfg-pick-session", agent.ID, "m1")
	require.Equal(t, "on", picks["fast-mode"])

	// A session pick is not model-scoped: it follows the session, and the turn
	// drops ids the model now running does not advertise (see
	// applyExtraConfigOptionsAtTurn). The per-model DEFAULT, by contrast, is.
	assert.Nil(t, model.GetAgent(agent.ID).ConfigOptionsForModel("m2"))
}
