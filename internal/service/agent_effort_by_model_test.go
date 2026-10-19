package service_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// These tests pin the per-model thinking-effort memory introduced for issue
// #552: ACP agents report their own default effort per model (OpenCode says
// "low" for most), so the user's explicit pick has to be remembered per model
// and must beat that default on the next session.

// TestThinkingEffortForModel_Priority pins the lookup order in isolation.
func TestThinkingEffortForModel_Priority(t *testing.T) {
	agent := &model.Agent{
		ThinkingEffort:          "minimal",
		PreferredThinkingEffort: "medium",
		ThinkingEffortByModel:   map[string]string{"glm-5.3": "max"},
	}

	assert.Equal(t, "max", agent.ThinkingEffortForModel("glm-5.3"),
		"the pick remembered for this model wins")
	assert.Equal(t, "medium", agent.ThinkingEffortForModel("deepseek-v4-pro"),
		"a model with no pick falls back to the model-independent default")
	assert.Equal(t, "medium", agent.ThinkingEffortForModel(""),
		"no model yet -> the model-independent default (never a per-model guess)")
}

// TestThinkingEffortForModel_NoPreferredFallsBackToAgentDefault covers an agent
// whose default is configured but has no user preference at all.
func TestThinkingEffortForModel_NoPreferredFallsBackToAgentDefault(t *testing.T) {
	agent := &model.Agent{ThinkingEffort: "low"}
	assert.Equal(t, "low", agent.ThinkingEffortForModel("glm-5.3"))
	assert.Equal(t, "", (&model.Agent{}).ThinkingEffortForModel("glm-5.3"),
		"nothing configured at all -> empty (the agent keeps its own default)")
}

// TestSetThinkingEffortForModel_ClearsWithEmptyLevel pins that an empty level
// REMOVES the entry instead of storing a blank override, so the global default
// applies again rather than the model being pinned to "".
func TestSetThinkingEffortForModel_ClearsWithEmptyLevel(t *testing.T) {
	agent := &model.Agent{PreferredThinkingEffort: "high", ThinkingEffortByModel: map[string]string{"glm-5.3": "max"}}
	agent.SetThinkingEffortForModel("glm-5.3", "")
	assert.Equal(t, "high", agent.ThinkingEffortForModel("glm-5.3"),
		"clearing the per-model pick must fall back to the global default")
	_, present := agent.ThinkingEffortByModel["glm-5.3"]
	assert.False(t, present, "the entry must be deleted, not blanked")
}

// TestThinkingEffortByModel_RoundTrip is the persistence contract: the map must
// survive SaveAgent -> LoadAgentsFromDB, or the memory is lost on every restart.
func TestThinkingEffortByModel_RoundTrip(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID:                    "opencode",
		Name:                  "OpenCode",
		Backend:               "opencode",
		ThinkingEffortLevels:  []string{"low", "high", "max"},
		ThinkingEffortByModel: map[string]string{"glm-5.3": "max", "deepseek-v4-pro": "high"},
	}
	require.NoError(t, service.SaveAgent(db, agent))

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, map[string]string{"glm-5.3": "max", "deepseek-v4-pro": "high"}, agents[0].ThinkingEffortByModel)
}

// TestThinkingEffortByModel_UpsertReplaces guards against the map surviving a
// save that explicitly cleared it: SaveAgent writes the whole row, so a nil map
// must clear the column rather than leave stale entries behind.
func TestThinkingEffortByModel_UpsertReplaces(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID: "opencode", Name: "OpenCode", Backend: "opencode",
		ThinkingEffortByModel: map[string]string{"glm-5.3": "max"},
	}
	require.NoError(t, service.SaveAgent(db, agent))

	agent.ThinkingEffortByModel = nil
	require.NoError(t, service.SaveAgent(db, agent))

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Empty(t, agents[0].ThinkingEffortByModel, "a cleared map must not leave stale entries")
}

// TestPatchThinkingEffortForModel_MergesAndClears exercises the partial-update
// path used by the chat picker: it must add/remove ONE model's entry without
// touching the others, and without disturbing the other agent fields.
func TestPatchThinkingEffortForModel_MergesAndClears(t *testing.T) {
	db := setupTestDBForAgents(t)

	require.NoError(t, service.SaveAgent(db, &model.Agent{
		ID: "opencode", Name: "OpenCode", Backend: "opencode",
		PreferredThinkingEffort: "medium",
		ThinkingEffortByModel:   map[string]string{"glm-5.3": "max"},
	}))

	level := "high"
	require.NoError(t, service.PatchAgentFields("opencode", service.AgentPatch{
		ThinkingEffortForModel: &service.ThinkingEffortModelPatch{ModelID: "deepseek-v4-pro", Level: &level},
	}))

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, map[string]string{"glm-5.3": "max", "deepseek-v4-pro": "high"}, agents[0].ThinkingEffortByModel)
	assert.Equal(t, "medium", agents[0].PreferredThinkingEffort,
		"a per-model write must not touch the model-independent default")

	// Clearing one entry leaves the other in place.
	require.NoError(t, service.PatchAgentFields("opencode", service.AgentPatch{
		ThinkingEffortForModel: &service.ThinkingEffortModelPatch{ModelID: "glm-5.3"},
	}))
	agents, err = service.LoadAgentsFromDB()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"deepseek-v4-pro": "high"}, agents[0].ThinkingEffortByModel)
}

// TestPatchThinkingEffortForModel_CorruptValueRebuilds pins that an unreadable
// stored value cannot permanently disable the feature: the write still succeeds
// and produces a valid map (losing only the unreadable picks).
func TestPatchThinkingEffortForModel_CorruptValueRebuilds(t *testing.T) {
	db := setupTestDBForAgents(t)

	require.NoError(t, service.SaveAgent(db, &model.Agent{ID: "opencode", Name: "OpenCode", Backend: "opencode"}))
	_, err := db.Exec("UPDATE agents SET thinking_effort_by_model = 'not json' WHERE id = 'opencode'")
	require.NoError(t, err)

	level := "max"
	require.NoError(t, service.PatchAgentFields("opencode", service.AgentPatch{
		ThinkingEffortForModel: &service.ThinkingEffortModelPatch{ModelID: "glm-5.3", Level: &level},
	}))

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"glm-5.3": "max"}, agents[0].ThinkingEffortByModel)
}

// TestDuplicateAgent_CopiesThinkingEffortByModel pins that a duplicate keeps the
// per-model picks: the copy runs the same models, so dropping the picks would
// make it fall back to the agent's own per-model defaults — the behavior this
// feature exists to override.
func TestDuplicateAgent_CopiesThinkingEffortByModel(t *testing.T) {
	db := setupTestDBForAgents(t)

	require.NoError(t, service.SaveAgent(db, &model.Agent{
		ID: "opencode", Name: "OpenCode", Backend: "opencode",
		ThinkingEffortByModel: map[string]string{"glm-5.3": "max"},
	}))
	require.NoError(t, service.LoadAgentsIntoMemory())
	t.Cleanup(func() { model.Agents = map[string]*model.Agent{} })

	clone, err := service.DuplicateAgent("opencode", "OpenCode Copy")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"glm-5.3": "max"}, clone.ThinkingEffortByModel)

	// And it must survive the round trip, not just exist in memory.
	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	var found *model.Agent
	for _, a := range agents {
		if a.ID == clone.ID {
			found = a
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, map[string]string{"glm-5.3": "max"}, found.ThinkingEffortByModel)
}

// TestPatchThinkingEffortForModel_MissingAgentFails pins that a write against a
// vanished agent row reports an error instead of success.
//
// Reporting nil told the caller the pick was remembered when nothing was
// written: the level still applied to the live session (the in-memory agent was
// updated), so the only symptom appeared later — a new session or a restart
// silently fell back to the agent's own default, with nothing to explain it.
func TestPatchThinkingEffortForModel_MissingAgentFails(t *testing.T) {
	setupTestDBForAgents(t)

	level := "max"
	err := service.PatchAgentFields("no-such-agent", service.AgentPatch{
		ThinkingEffortForModel: &service.ThinkingEffortModelPatch{ModelID: "glm-5.3", Level: &level},
	})
	require.Error(t, err, "a missing agent row must not be reported as a successful save")
	assert.ErrorIs(t, err, sql.ErrNoRows, "the caller needs to recognize the missing-row case")
}
