package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
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

// newConfigPickFixture creates a session owned by a fresh DB for the pick tests
// below, and publishes an agent with the given id. Returns the agent.
func newConfigPickFixture(t *testing.T, agentID string) *model.Agent {
	t.Helper()
	db, err := InitInMemoryDB()
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	agent := &model.Agent{
		ID: agentID, Backend: "codex", AcpCommand: "codex",
		Models: []model.AgentModel{{ID: "m1", Default: true}},
	}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})
	t.Cleanup(func() { model.ReplaceAgents(map[string]*model.Agent{}, nil) })

	_, err = db.Exec(`
		INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, model)
		VALUES (?, 0, 'codex', 't', ?, 'm1')`, "sess-"+agentID, agentID)
	require.NoError(t, err)
	return agent
}

// TestResolveConfigOptionPicks_UnknownAgentYieldsNil pins the fail-safe: a stale
// agent id must not panic and must not invent picks.
func TestResolveConfigOptionPicks_UnknownAgentYieldsNil(t *testing.T) {
	db, err := InitInMemoryDB()
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
	model.ReplaceAgents(map[string]*model.Agent{}, nil)
	t.Cleanup(func() { model.ReplaceAgents(map[string]*model.Agent{}, nil) })

	assert.Nil(t, resolveConfigOptionPicks("no-such-session", "no-such-agent", "m1"))
}

// TestResolveConfigOptionPicks_DropsIdsTheModelStoppedOffering covers the stale
// pick case: the session keeps its value when the model changes, so an id the
// model no longer advertises must not be replayed for the next turn.
func TestResolveConfigOptionPicks_DropsIdsTheModelStoppedOffering(t *testing.T) {
	const agentID = "cfg-filter-agent"
	newConfigPickFixture(t, agentID)

	// The model now advertises only "fast-mode"; the session also remembers a
	// value for "thinking" from a previous model.
	ai.GetAgentCapabilityRegistry().UpdateExtraConfigOptions(agentID, "m1", []ai.ExtraConfigOption{
		{ConfigID: "fast-mode", Category: "model_config"},
	})
	t.Cleanup(func() {
		ai.GetAgentCapabilityRegistry().Update(agentID, &ai.AgentCapability{
			ExtraConfigOptionsByModel: map[string][]ai.ExtraConfigOption{},
		})
	})
	PersistConfigOptionPicks("sess-"+agentID, map[string]string{
		"fast-mode": "on",
		"thinking":  "high",
	})

	picks := resolveConfigOptionPicks("sess-"+agentID, agentID, "m1")
	require.Equal(t, "on", picks["fast-mode"])
	assert.NotContains(t, picks, "thinking", "an id the model no longer offers must be dropped")
}

// TestResolveConfigOptionPicks_SendsAsIsWhenModelNeverReported pins the other
// half of that distinction: nil means "never learned", not "offers nothing", so
// the value is sent unchanged instead of being filtered away.
func TestResolveConfigOptionPicks_SendsAsIsWhenModelNeverReported(t *testing.T) {
	const agentID = "cfg-unreported-agent"
	agent := newConfigPickFixture(t, agentID)
	agent.SetConfigOptionsForModel("m1", map[string]string{"fast-mode": "off"})
	PersistConfigOptionPicks("sess-"+agentID, map[string]string{"fast-mode": "on"})

	picks := resolveConfigOptionPicks("sess-"+agentID, agentID, "m1")
	require.Equal(t, "on", picks["fast-mode"])
}

// TestResolveConfigOptionPicks_EmptyLearnedListFiltersEverything covers the
// non-nil-empty case: the model was learned to offer NO options, so no pick
// survives.
func TestResolveConfigOptionPicks_EmptyLearnedListFiltersEverything(t *testing.T) {
	const agentID = "cfg-empty-agent"
	newConfigPickFixture(t, agentID)

	ai.GetAgentCapabilityRegistry().UpdateExtraConfigOptions(agentID, "m1", []ai.ExtraConfigOption{})
	t.Cleanup(func() {
		ai.GetAgentCapabilityRegistry().Update(agentID, &ai.AgentCapability{
			ExtraConfigOptionsByModel: map[string][]ai.ExtraConfigOption{},
		})
	})
	PersistConfigOptionPicks("sess-"+agentID, map[string]string{"fast-mode": "on"})

	picks := resolveConfigOptionPicks("sess-"+agentID, agentID, "m1")
	assert.Empty(t, picks, "a model offering no options can accept no pick")
}
