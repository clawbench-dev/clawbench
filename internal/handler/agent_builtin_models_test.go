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
	// Ids carry the provider they belong to: the built-in agent can run every
	// configured provider, so a bare id could not say which endpoint to call.
	assert.Equal(t, model.ProviderScopedModelID(77, "profile-model"), got.Models[0].ID)
	assert.Equal(t, "profile-model", got.Models[0].Name, "the row still shows the model's own name")
	assert.True(t, got.Models[0].Default, "the model a turn would run must be flagged default")
	assert.Empty(t, got.Catalog, "a confirmed list must not also ship the catalog fallback")
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
	assert.Equal(t, model.ProviderScopedModelID(77, "profile-model"), resp.Models[0].ID)
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

// TestAgentGet_BuiltinAgentOffersEveryUsableProvider pins the feature: the
// built-in agent calls the endpoint itself, so its picker lists models from
// every configured provider it can talk to — not only the bound one. That is
// what lets a user switch provider from the model list instead of going back to
// the settings page and refreshing the session.
//
// The same list must stay honest: a protocol this backend has no client for,
// and another backend's own-service token, are both excluded.
func TestAgentGet_BuiltinAgentOffersEveryUsableProvider(t *testing.T) {
	defer setupAgentTestEnv(t)()

	bound := int64(81)
	model.ReplaceProviders([]*model.Provider{
		{
			ID: 81, Name: "MiniMax", Protocol: model.ProviderProtocolOpenAI,
			BaseURL: "http://127.0.0.1:1", APIKey: "k1",
			Models: []string{"MiniMax-M2"}, DefaultModel: "MiniMax-M2",
		},
		{
			ID: 82, Name: "Claude 代理", Protocol: model.ProviderProtocolAnthropic,
			BaseURL: "http://127.0.0.1:1", APIKey: "k2",
			Models: []string{"claude-sonnet-4-5"}, DefaultModel: "claude-sonnet-4-5",
		},
		{
			// No Gemini client in this backend.
			ID: 83, Name: "Google", Protocol: model.ProviderProtocolGemini,
			BaseURL: "http://127.0.0.1:1", APIKey: "k3",
			Models: []string{"gemini-2.5-pro"},
		},
		{
			// Own-service token for another backend.
			ID: 84, Name: "Cursor 官方", Protocol: model.ProviderProtocolPrivate,
			Backend: "cursor", APIKey: "k4",
			Models: []string{"cursor-fast"},
		},
	})
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	agent := &model.Agent{
		ID: "builtin-multi-" + t.Name(), Name: "ClawBench Agent",
		Backend: "builtin", BuiltinBackend: true,
		ProviderID: &bound, PreferredModel: "MiniMax-M2",
	}
	model.Agents[agent.ID] = agent
	prevList := model.AgentList
	model.AgentList = append(append([]*model.Agent{}, prevList...), agent)
	t.Cleanup(func() {
		delete(model.Agents, agent.ID)
		model.AgentList = prevList
	})

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Agents []struct {
			ID     string             `json:"id"`
			Models []model.AgentModel `json:"models"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	var got []model.AgentModel
	for i := range resp.Agents {
		if resp.Agents[i].ID == agent.ID {
			got = resp.Agents[i].Models
		}
	}
	require.Len(t, got, 2, "both usable providers' models, and nothing else")

	byID := map[string]model.AgentModel{}
	for _, m := range got {
		byID[m.ID] = m
	}
	mm, ok := byID[model.ProviderScopedModelID(81, "MiniMax-M2")]
	require.True(t, ok, "the bound provider's model must be listed")
	assert.Equal(t, "MiniMax", mm.Provider, "the picker groups by this name")
	assert.True(t, mm.Default, "the model the turn runs is flagged")

	cl, ok := byID[model.ProviderScopedModelID(82, "claude-sonnet-4-5")]
	require.True(t, ok, "the other provider's model must be listed too")
	assert.Equal(t, "Claude 代理", cl.Provider)
	assert.False(t, cl.Default, "only one row is the current default")

	for _, m := range got {
		assert.NotEqual(t, int64(83), m.ProviderID, "no Gemini client in this backend")
		assert.NotEqual(t, int64(84), m.ProviderID, "another backend's token must not leak in")
	}
}
