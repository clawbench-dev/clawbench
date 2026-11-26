package service_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// TestDuplicateAgent_CopiesConfigOptionPicks pins that a duplicated agent keeps
// the "高级" tab picks (issue #746) of its source.
//
// The options are advertised by the BACKEND and the clone runs the same models,
// so the choices describe the same thing. Dropping them made a project copy of
// a configured agent open on the agent's own defaults (CodeBuddy: sandbox ON),
// which reads as "my settings did not carry over".
func TestDuplicateAgent_CopiesConfigOptionPicks(t *testing.T) {
	db := setupTestDBForAgents(t)

	require.NoError(t, service.SaveAgent(db, &model.Agent{
		ID: "codebuddy", Name: "Codebuddy", Backend: "codebuddy",
		PreferredConfigOptions: map[string]map[string]string{
			"custom-local:deepseek-v4.1-flash": {"sandbox": "false", "multitask": "false"},
		},
	}))
	require.NoError(t, service.LoadAgentsIntoMemory())
	t.Cleanup(func() { model.Agents = map[string]*model.Agent{} })

	clone, err := service.DuplicateAgent("codebuddy", "Codebuddy Copy")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"sandbox": "false", "multitask": "false"},
		clone.ConfigOptionsForModel("custom-local:deepseek-v4.1-flash"))

	// It must survive the round trip, not just exist in memory.
	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	var found *model.Agent
	for _, a := range agents {
		if a.ID == clone.ID {
			found = a
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, map[string]string{"sandbox": "false", "multitask": "false"},
		found.ConfigOptionsForModel("custom-local:deepseek-v4.1-flash"))

	// The nested maps must be independent: editing the copy's picks must not
	// rewrite the source's (both are published as shared *Agent values).
	clone.SetConfigOptionsForModel("custom-local:deepseek-v4.1-flash", map[string]string{"sandbox": "true"})
	src := model.GetAgent("codebuddy")
	require.NotNil(t, src)
	assert.Equal(t, "false", src.ConfigOptionsForModel("custom-local:deepseek-v4.1-flash")["sandbox"],
		"the clone's map must be a deep copy")
}
