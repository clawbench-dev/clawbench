package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
)

func setupProvidersHandlerTest(t *testing.T) {
	t.Helper()
	// Isolate the home directory: config-file backends (codex) write there.
	home := t.TempDir()
	t.Setenv("HOME", home)
	origDataDir := model.DataDir
	model.DataDir = filepath.Join(home, ".clawbench")
	t.Cleanup(func() { model.DataDir = origDataDir })

	db, err := service.InitInMemoryDB()
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
		model.ReplaceProviders(nil)
		model.ReplaceAgents(nil, nil)
	})
	model.ReplaceProviders(nil)
}

func doProviderRequest(t *testing.T, handler http.HandlerFunc, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestProvidersAPI_CreateListAndNoKeyEcho(t *testing.T) {
	setupProvidersHandlerTest(t)

	rec := doProviderRequest(t, ServeProviders, http.MethodPost, "/api/providers", map[string]any{
		"name": "MiniMax", "protocol": "anthropic", "baseUrl": "https://api.minimax.io/anthropic", "apiKey": "sk-secret",
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "sk-secret", "the API key must never be returned")
	assert.Contains(t, rec.Body.String(), `"hasKey":true`)

	rec = doProviderRequest(t, ServeProviders, http.MethodGet, "/api/providers", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var list struct {
		Providers []providerResponse `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Providers, 1)
	assert.Equal(t, "MiniMax", list.Providers[0].Name)
	assert.True(t, list.Providers[0].HasKey)
}

func TestProvidersAPI_DuplicateNameConflicts(t *testing.T) {
	setupProvidersHandlerTest(t)
	body := map[string]any{"name": "Dup", "protocol": "openai"}
	require.Equal(t, http.StatusCreated, doProviderRequest(t, ServeProviders, http.MethodPost, "/api/providers", body).Code)
	rec := doProviderRequest(t, ServeProviders, http.MethodPost, "/api/providers", body)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "ProviderNameTaken")
}

func TestProvidersAPI_DeleteInUseConflicts(t *testing.T) {
	setupProvidersHandlerTest(t)

	p, err := service.CreateProvider(model.Provider{Name: "InUse", Protocol: model.ProviderProtocolAnthropic})
	require.NoError(t, err)
	require.NoError(t, service.SaveAgent(store.WriteDB(), &model.Agent{
		ID: "a1", Name: "A1", Backend: "claude", ProviderID: &p.ID,
	}))

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodDelete, "/api/providers/1", nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "ProviderInUse")
}

func TestProvidersAPI_Presets(t *testing.T) {
	setupProvidersHandlerTest(t)
	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, "/api/providers/presets", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Presets []model.ProviderPreset `json:"presets"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotEmpty(t, body.Presets, "the embedded models.dev catalog must be present")
	for _, p := range body.Presets {
		assert.NotEmpty(t, p.Name)
		assert.NotEmpty(t, p.BaseURL)
		assert.True(t, model.ValidProtocol(p.Protocol), "preset %s has an unknown protocol", p.ID)
	}
}

func TestAgentPatch_ProviderBinding(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider, err := service.CreateProvider(model.Provider{
		Name: "Claude GW", Protocol: model.ProviderProtocolAnthropic, BaseURL: "https://gw", APIKey: "k",
	})
	require.NoError(t, err)

	agent := &model.Agent{ID: "claude-agent", Name: "Claude Agent", Backend: "claude"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": provider.ID,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	updated := model.GetAgent(agent.ID)
	require.NotNil(t, updated.ProviderID)
	assert.Equal(t, provider.ID, *updated.ProviderID)

	// null clears the binding.
	rec = doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": nil,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, model.GetAgent(agent.ID).ProviderID)
}

func TestAgentPatch_ProviderUnsupportedBackend(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider, err := service.CreateProvider(model.Provider{Name: "P", Protocol: model.ProviderProtocolOpenAI, APIKey: "k"})
	require.NoError(t, err)
	agent := &model.Agent{ID: "copilot-agent", Name: "Copilot", Backend: "copilot"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": provider.ID,
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "ProviderUnsupported")
	assert.Nil(t, model.GetAgent(agent.ID).ProviderID)
}

func TestAgentPatch_ConfigBackendNeedsConsent(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider, err := service.CreateProvider(model.Provider{Name: "Codex P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://api.deepseek.com/v1", APIKey: "k"})
	require.NoError(t, err)
	agent := &model.Agent{ID: "codex-agent", Name: "Codex", Backend: "codex"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": provider.ID,
	})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderConfigConsentRequired")
	assert.Nil(t, model.GetAgent(agent.ID).ProviderID)

	rec = doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": provider.ID, "confirm_config_write": true,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotNil(t, model.GetAgent(agent.ID).ProviderID)
}

func TestProvidersAPI_ModelList(t *testing.T) {
	setupProvidersHandlerTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"},{"id":"model-b"}]}`))
	}))
	defer srv.Close()

	p, err := service.CreateProvider(model.Provider{
		Name: "WithModels", Protocol: model.ProviderProtocolOpenAI, BaseURL: srv.URL + "/v1", APIKey: "k",
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, "/api/providers/1/models", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Models []string `json:"models"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, []string{"model-a", "model-b"}, body.Models)
	assert.NotZero(t, p.ID)
}

func TestProvidersAPI_ModelListUnreachableReturnsEmpty(t *testing.T) {
	setupProvidersHandlerTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no catalog", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := service.CreateProvider(model.Provider{
		Name: "NoModels", Protocol: model.ProviderProtocolOpenAI, BaseURL: srv.URL, APIKey: "k",
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, "/api/providers/1/models", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Models []string `json:"models"`
		Error  string   `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Empty(t, body.Models)
	assert.NotEmpty(t, body.Error, "an unreachable catalog must explain itself")
}

// A provider-backed agent may serve ids neither the CLI discovery nor the ACP
// runtime has listed (BYOK gateways), so a free-form model must be accepted.
func TestAgentPatch_AllowsUnlistedModelWithProvider(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider, err := service.CreateProvider(model.Provider{
		Name: "BYOK", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1", APIKey: "k",
	})
	require.NoError(t, err)
	agent := &model.Agent{ID: "byok-agent", Name: "BYOK", Backend: "claude", ProviderID: &provider.ID}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "preferred_model": "gateway-only-model",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "gateway-only-model", model.GetAgent(agent.ID).PreferredModel)
}

// Without a provider the discovered list stays authoritative: a typo must not
// be saved as a model the CLI cannot run.
func TestAgentPatch_RejectsUnlistedModelWithoutProvider(t *testing.T) {
	setupProvidersHandlerTest(t)

	agent := &model.Agent{
		ID: "plain-agent", Name: "Plain", Backend: "claude",
		Models: []model.AgentModel{{ID: "known-model", Name: "Known", Default: true}},
	}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "preferred_model": "typo-model",
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "InvalidModelForAgent")
}

// A CLI agent bound to a provider inherits the provider's curated models: the
// endpoint is custom, so the CLI's own discovery is not authoritative for it.
func TestAgents_CLIAgentInheritsProviderModels(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider, err := service.CreateProvider(model.Provider{
		Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1", APIKey: "k",
		Models: []string{"gw-a", "gw-b"}, DefaultModel: "gw-b",
	})
	require.NoError(t, err)

	agent := &model.Agent{
		ID: "cli-bound", Name: "CLI", Backend: "claude", ProviderID: &provider.ID,
		Models: []model.AgentModel{{ID: "cli-only", Default: true}},
	}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodGet, "/api/agents", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Agents []struct {
			Models []struct {
				ID      string `json:"id"`
				Default bool   `json:"default"`
			} `json:"models"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Agents, 1)
	require.Len(t, body.Agents[0].Models, 2)
	assert.Equal(t, "gw-a", body.Agents[0].Models[0].ID)
	assert.True(t, body.Agents[0].Models[1].Default, "the provider default must be flagged")
}

// The summary model must be one the provider actually serves.
func TestServeConfigPatch_SummaryModelMustBeServed(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider, err := service.CreateProvider(model.Provider{
		Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1", APIKey: "k",
		Models: []string{"served-model"},
	})
	require.NoError(t, err)

	model.ConfigInstance = model.Config{}
	rec := doProviderRequest(t, ServeConfig, http.MethodPatch, "/api/config", map[string]any{
		"ai_summary": map[string]any{"provider_id": provider.ID, "model": "not-served"},
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "ai_summary.model")

	rec = doProviderRequest(t, ServeConfig, http.MethodPatch, "/api/config", map[string]any{
		"ai_summary": map[string]any{"provider_id": provider.ID, "model": "served-model"},
	})
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}
