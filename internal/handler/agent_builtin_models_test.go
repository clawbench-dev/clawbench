package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "clawbench/internal/ai/backends/builtin"
)

// withBuiltinAgent registers a built-in agent bound to a provider whose curated
// list is providerModels, and publishes it so the handler can find it.
//
// The built-in backend is imported by this file (blank import) because the
// handler resolves it through the in-process factory registry, and the
// production binary pulls it in via internal/ai/backends/all.
func withBuiltinAgent(t *testing.T, providerModels []string, preferred string) *model.Agent {
	t.Helper()

	const providerID = int64(77)
	model.ReplaceProviders([]*model.Provider{{
		ID: providerID, Name: "builtin-refresh-test",
		Protocol: model.ProviderProtocolOpenAI,
		BaseURL:  "http://127.0.0.1:1", APIKey: "k",
		Models: providerModels, DefaultModel: preferred,
	}})
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	pid := providerID
	agent := &model.Agent{
		ID: "builtin-refresh-" + t.Name(), Name: "ClawBench Agent",
		Backend: "builtin", BuiltinBackend: true,
		ProviderID: &pid, PreferredModel: preferred,
	}
	if model.Agents == nil {
		model.Agents = map[string]*model.Agent{}
	}
	model.Agents[agent.ID] = agent
	// serveAgentsGet marshals model.GetAgentList(), not the map, so the agent
	// has to be appended there too — and removing it from the slice on cleanup
	// keeps the fixture out of every later test.
	prevList := model.AgentList
	model.AgentList = append(append([]*model.Agent{}, prevList...), agent)
	t.Cleanup(func() {
		delete(model.Agents, agent.ID)
		model.AgentList = prevList
	})

	// The list must go through the capability registry, which every consumer
	// (drawer, GET /api/agents, the ACP gate) reads.
	state := ai.GetAgentCapabilityRegistry().GetModelListState(agent.ID, "")
	require.NotNil(t, state, "a bound built-in agent must have a model list before any message")
	return agent
}

// TestAgentGet_BuiltinAgentReportsItsModelsBeforeAnyMessage pins the reported
// defect: the built-in agent's model list was empty until the first turn, and
// the chip fell back to "Default".
//
// The ACP gate (gateUnconfirmedACPModels) withholds an unconfirmed ACP agent's
// list. The built-in agent has no handshake to confirm anything, so it must be
// treated as confirmed and keep the models derived from its provider binding.
func TestAgentGet_BuiltinAgentReportsItsModelsBeforeAnyMessage(t *testing.T) {
	defer setupAgentTestEnv(t)()
	agent := withBuiltinAgent(t, []string{"profile-model", "other-model"}, "profile-model")

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Agents []struct {
			ID      string             `json:"id"`
			Models  []model.AgentModel `json:"models"`
			Catalog []model.AgentModel `json:"catalogModels"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	var got *struct {
		ID      string             `json:"id"`
		Models  []model.AgentModel `json:"models"`
		Catalog []model.AgentModel `json:"catalogModels"`
	}
	for i := range resp.Agents {
		if resp.Agents[i].ID == agent.ID {
			got = &resp.Agents[i]
		}
	}
	require.NotNil(t, got, "the built-in agent must be in the response")
	require.Len(t, got.Models, 2, "the picker needs the list before the first message")
	assert.Equal(t, "profile-model", got.Models[0].ID)
	assert.True(t, got.Models[0].Default, "the model a turn would run must be flagged default")
	assert.Empty(t, got.Catalog, "a confirmed list must not also ship the catalogue fallback")
}

// TestAgentRefreshModels_BuiltinAgentAnswersFromItsProvider pins the drawer's
// refresh button: the built-in agent has no CLI to probe and no BackendSpec
// model source, so the discovery path answered 400 ModelDiscoveryNotSupported
// even though its list is derived and always available.
func TestAgentRefreshModels_BuiltinAgentAnswersFromItsProvider(t *testing.T) {
	defer setupAgentTestEnv(t)()
	agent := withBuiltinAgent(t, []string{"profile-model", "other-model"}, "profile-model")

	req := newRequest(t, http.MethodPost, "/api/agents/"+agent.ID+"/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp struct {
		Models []model.AgentModel `json:"models"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Models, 2)
	assert.Equal(t, "profile-model", resp.Models[0].ID)
}

// TestAgentRefreshModels_BuiltinAgentWithoutProviderIsRejected pins fail-closed:
// an unbound built-in agent has no trustworthy list, so a refresh must say so
// rather than invent one (or fall through to CLI discovery).
func TestAgentRefreshModels_BuiltinAgentWithoutProviderIsRejected(t *testing.T) {
	defer setupAgentTestEnv(t)()

	agent := &model.Agent{ID: "builtin-unbound-" + t.Name(), Backend: "builtin", BuiltinBackend: true}
	model.Agents[agent.ID] = agent
	t.Cleanup(func() { delete(model.Agents, agent.ID) })

	req := newRequest(t, http.MethodPost, "/api/agents/"+agent.ID+"/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
