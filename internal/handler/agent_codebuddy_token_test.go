package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// CodeBuddy's own-token mode is a switch on the agent; the token itself is one
// shared config value. The PATCH has to accept the switch on a codebuddy agent
// and refuse it everywhere it cannot work.

func TestAgentPatch_CodeBuddyTokenMode(t *testing.T) {
	setupProvidersHandlerTest(t)

	agent := &model.Agent{ID: "cb", Name: "CodeBuddy", Backend: model.CodeBuddyBackend}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "codebuddy_token_mode": true,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"codebuddyTokenMode":true`)

	updated := model.GetAgent(agent.ID)
	require.NotNil(t, updated)
	assert.True(t, updated.CodeBuddyTokenMode)

	// Turning the mode off goes back to the CLI login.
	rec = doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "codebuddy_token_mode": false,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.False(t, model.GetAgent(agent.ID).CodeBuddyTokenMode)
}

func TestAgentPatch_CodeBuddyTokenRejectedForOtherBackends(t *testing.T) {
	setupProvidersHandlerTest(t)

	agent := &model.Agent{ID: "cl", Name: "Claude", Backend: "claude"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "codebuddy_token_mode": true,
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "CodeBuddyTokenUnsupported")
	assert.False(t, model.GetAgent(agent.ID).CodeBuddyTokenMode)
}

// The token mode and a provider supply the same credential slot, so the server
// refuses a request that would leave both on.
func TestAgentPatch_CodeBuddyTokenConflictsWithProvider(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider, err := service.CreateProvider(model.Provider{
		Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw", APIKey: "k",
	})
	require.NoError(t, err)
	agent := &model.Agent{ID: "cb", Name: "CodeBuddy", Backend: model.CodeBuddyBackend, ProviderID: &provider.ID}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "codebuddy_token_mode": true,
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "CodeBuddyTokenProviderConflict")

	// Clearing the provider in the SAME patch is the supported way to switch.
	rec = doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": nil, "codebuddy_token_mode": true,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, model.GetAgent(agent.ID).ProviderID)
	assert.True(t, model.GetAgent(agent.ID).CodeBuddyTokenMode)
}
