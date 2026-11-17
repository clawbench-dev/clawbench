package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// CodeBuddy's own account token is an ordinary provider row with no endpoint.
// Binding it must not ask for a config-file confirmation and must not touch the
// config directory: the credential is injected through the environment.
func TestAgentPatch_CodeBuddyCredentialOnlyProvider(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider, err := service.CreateProvider(model.Provider{
		Name: "CodeBuddy Token", Protocol: model.ProviderProtocolPrivate, APIKey: "ck-secret",
	})
	require.NoError(t, err)
	agent := &model.Agent{ID: "cb", Name: "CodeBuddy", Backend: "codebuddy"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": provider.ID,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	updated := model.GetAgent(agent.ID)
	require.NotNil(t, updated.ProviderID)
	assert.Equal(t, provider.ID, *updated.ProviderID)

	// The token is a provider secret: it never appears in the agent payload.
	assert.NotContains(t, rec.Body.String(), "ck-secret")
}

// A provider with an endpoint is still a normal config-directory binding on
// codebuddy: it needs models, so the credential-only shortcut must not swallow it.
func TestAgentPatch_CodeBuddyEndpointProviderStillNeedsModels(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider, err := service.CreateProvider(model.Provider{
		Name: "GW", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://gw.example", APIKey: "sk-1",
	})
	require.NoError(t, err)
	agent := &model.Agent{ID: "cb", Name: "CodeBuddy", Backend: "codebuddy"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": provider.ID,
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderInvalid")
	assert.Nil(t, model.GetAgent(agent.ID).ProviderID)
}
