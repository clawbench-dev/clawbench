package service_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// The CodeBuddy own-token mode is one column on the agent (the switch); the
// token itself is server-wide config, so there is no secret in the agents table.
func TestCodeBuddyTokenMode_RoundTrip(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID: "cb", Name: "CodeBuddy", Backend: model.CodeBuddyBackend,
		CodeBuddyTokenMode: true,
	}
	require.NoError(t, service.SaveAgent(db, agent))

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.True(t, agents[0].CodeBuddyTokenMode)
	assert.True(t, agents[0].CodeBuddyTokenSupported)
}

// The mode is a switch of its own: turning it off must persist, so the agent
// goes back to its CLI login.
func TestCodeBuddyTokenMode_OffPersists(t *testing.T) {
	db := setupTestDBForAgents(t)

	agent := &model.Agent{
		ID: "cb", Name: "CodeBuddy", Backend: model.CodeBuddyBackend,
		CodeBuddyTokenMode: true,
	}
	require.NoError(t, service.SaveAgent(db, agent))

	off := false
	require.NoError(t, service.PatchAgentFields(agent.ID, service.AgentPatch{CodeBuddyTokenMode: &off}))

	agents, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.False(t, agents[0].CodeBuddyTokenMode)
}

// The agent row never carries the token: it is config, and the agent's JSON
// must not leak a credential even if a caller set the field by mistake.
func TestCodeBuddyToken_AgentJSONHasNoSecret(t *testing.T) {
	agent := &model.Agent{
		ID: "cb", Backend: model.CodeBuddyBackend, CodeBuddyTokenMode: true,
	}
	encoded, err := json.Marshal(agent)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "apiKey")
	assert.NotContains(t, string(encoded), "api_key")
	assert.Contains(t, string(encoded), `"codebuddyTokenMode":true`)
}
