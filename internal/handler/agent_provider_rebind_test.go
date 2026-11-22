package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// bindingProvider creates a codebuddy config-directory provider (the path that
// needs no consent file writing when the secret stays in the environment).
func bindingProvider(t *testing.T, models []string, def string) *model.Provider {
	t.Helper()
	p, err := service.CreateProvider(model.Provider{
		Name: "GW", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://gw.example", APIKey: "sk-1",
		Models: models, DefaultModel: def,
	})
	require.NoError(t, err)
	return p
}

// A stale pin is replaced by the provider's default at bind time. Before this,
// the settings page kept showing the old model while every turn silently ran the
// provider default — and the self-test failed for an agent that worked.
func TestAgentPatch_BindingReplacesStalePreferredModel(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider := bindingProvider(t, []string{"new-a", "new-b"}, "new-b")
	agent := &model.Agent{ID: "cb", Name: "CodeBuddy", Backend: "codebuddy", PreferredModel: "old-model"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": provider.ID,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	updated := model.GetAgent(agent.ID)
	require.NotNil(t, updated.ProviderID)
	assert.Equal(t, "new-b", updated.PreferredModel)
	// The stored pin and the model a turn would pick must agree.
	assert.Equal(t, "new-b", updated.DefaultModelID())
}

// A pin the new provider actually serves survives the binding untouched.
func TestAgentPatch_BindingKeepsServedPreferredModel(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider := bindingProvider(t, []string{"new-a", "new-b"}, "new-b")
	agent := &model.Agent{ID: "cb", Name: "CodeBuddy", Backend: "codebuddy", PreferredModel: "new-a"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": provider.ID,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "new-a", model.GetAgent(agent.ID).PreferredModel)
}

// An explicit preferred_model in the same patch wins over the rewrite — the
// caller asked for that model, so the binding must not overwrite it.
func TestAgentPatch_BindingDoesNotOverrideExplicitPreferredModel(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider := bindingProvider(t, []string{"new-a", "new-b"}, "new-b")
	agent := &model.Agent{
		ID: "cb", Name: "CodeBuddy", Backend: "codebuddy", PreferredModel: "old-model",
		Models: []model.AgentModel{{ID: "native-a"}},
	}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": provider.ID, "preferred_model": "new-b",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "new-b", model.GetAgent(agent.ID).PreferredModel)
}

// Binding a provider and picking a model in ONE request must work: the model is
// checked against the provider being bound, not the catalog the agent is
// leaving. This is the shape a settings save actually takes — before, the check
// read the stored row (no provider yet) and rejected a name the endpoint serves.
func TestAgentPatch_BindingAcceptsModelFromProviderInSamePatch(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider := bindingProvider(t, []string{"new-a", "new-b"}, "new-b")
	agent := &model.Agent{
		ID: "cb", Name: "CodeBuddy", Backend: "codebuddy",
		Models: []model.AgentModel{{ID: "native-a"}},
	}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": provider.ID, "preferred_model": "new-a",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "new-a", model.GetAgent(agent.ID).PreferredModel)
}

// Clearing the binding in the same patch removes the advisory state: an unknown
// model name is a typo again, because nothing will be able to serve it.
func TestAgentPatch_ClearingBindingRevalidatesModel(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider := bindingProvider(t, []string{"new-a"}, "new-a")
	pid := provider.ID
	agent := &model.Agent{
		ID: "cb", Name: "CodeBuddy", Backend: "codebuddy", ProviderID: &pid,
		Models: []model.AgentModel{{ID: "native-a"}},
	}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": nil, "preferred_model": "new-a",
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// An unbound agent still rejects a model nobody listed: the guard is relaxed
// only when a provider is (or becomes) bound.
func TestAgentPatch_UnboundAgentStillRejectsUnknownModel(t *testing.T) {
	setupProvidersHandlerTest(t)

	agent := &model.Agent{
		ID: "cb", Name: "CodeBuddy", Backend: "codebuddy",
		Models: []model.AgentModel{{ID: "native-a"}},
	}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "preferred_model": "ghost-model",
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}
