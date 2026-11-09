package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
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

// The settings card lists who uses each provider, so the list response has to
// carry agent names — but still never the key.
func TestProvidersAPI_ListCarriesAgentNames(t *testing.T) {
	setupProvidersHandlerTest(t)

	p, err := service.CreateProvider(model.Provider{
		Name: "Named", Protocol: model.ProviderProtocolAnthropic, APIKey: "sk-hidden",
	})
	require.NoError(t, err)
	require.NoError(t, service.SaveAgent(store.WriteDB(), &model.Agent{
		ID: "a1", Name: "Writer", Backend: "claude", ProviderID: &p.ID,
	}))

	rec := doProviderRequest(t, ServeProviders, http.MethodGet, "/api/providers", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list struct {
		Providers []providerResponse `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Providers, 1)
	assert.Equal(t, []string{"Writer"}, list.Providers[0].Agents)
	assert.Equal(t, 1, list.Providers[0].AgentCount)
	assert.NotContains(t, rec.Body.String(), "sk-hidden")
}

// The edit dialog's eye button is the only caller of reveal=1: the key must
// stay out of the plain detail read, or every settings page load would carry
// every stored secret.
func TestProvidersAPI_RevealKeyOnlyOnRequest(t *testing.T) {
	setupProvidersHandlerTest(t)

	p, err := service.CreateProvider(model.Provider{
		Name: "Reveal", Protocol: model.ProviderProtocolOpenAI, APIKey: "sk-reveal",
	})
	require.NoError(t, err)
	path := fmt.Sprintf("/api/providers/%d", p.ID)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "sk-reveal")
	assert.Contains(t, rec.Body.String(), `"hasKey":true`)

	rec = doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, path+"?reveal=1", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"apiKey":"sk-reveal"`)
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

// A key-only backend never reads the endpoint, so binding it to a provider that
// has one produced a binding that failed on every turn. The UI now offers only
// bare credentials, and the API has to agree.
func TestAgentPatch_KeyOnlyBackendRequiresProviderWithoutEndpoint(t *testing.T) {
	setupProvidersHandlerTest(t)

	withEndpoint, err := service.CreateProvider(model.Provider{
		Name: "WithEndpoint", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x/v1", APIKey: "k",
	})
	require.NoError(t, err)
	bare, err := service.CreateProvider(model.Provider{
		Name: "Bare", Protocol: model.ProviderProtocolOpenAI, APIKey: "k",
	})
	require.NoError(t, err)

	agent := &model.Agent{ID: "cursor-agent", Name: "Cursor", Backend: "cursor"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": withEndpoint.ID,
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderUnsupported")
	assert.Nil(t, model.GetAgent(agent.ID).ProviderID)

	rec = doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": bare.ID,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotNil(t, model.GetAgent(agent.ID).ProviderID)
	assert.Equal(t, bare.ID, *model.GetAgent(agent.ID).ProviderID)
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

// A saved selection must not hide the rest of the catalog: the picker's list is
// what the endpoint advertises, with the curated subset reported separately.
// Regression: the second open of the picker showed exactly what was saved, so
// there was no way left to add another model.
func TestProvidersAPI_ModelListStillProbesAfterCuration(t *testing.T) {
	setupProvidersHandlerTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"},{"id":"model-b"}]}`))
	}))
	defer srv.Close()

	_, err := service.CreateProvider(model.Provider{
		Name: "Curated", Protocol: model.ProviderProtocolOpenAI, BaseURL: srv.URL + "/v1", APIKey: "k",
		Models: []string{"model-a"},
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, "/api/providers/1/models", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Models  []string `json:"models"`
		Curated []string `json:"curated"`
		Source  string   `json:"source"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, []string{"model-a", "model-b"}, body.Models)
	assert.Equal(t, []string{"model-a"}, body.Curated)
	assert.Equal(t, "live", body.Source)
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

// ── Per-model test ───────────────────────────────────────────

type modelTestResponse struct {
	OK        bool   `json:"ok"`
	Model     string `json:"model"`
	Status    int    `json:"status"`
	LatencyMS int64  `json:"latencyMs"`
	Reply     string `json:"reply"`
	Error     string `json:"error"`
}

func runModelTest(t *testing.T, providerID int64, body map[string]any) modelTestResponse {
	t.Helper()
	if body == nil {
		body = map[string]any{}
	}
	body["id"] = providerID
	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodPost, "/api/providers/test", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out modelTestResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

// A model test has to be a real chat call: a /models listing can succeed for a
// model the endpoint cannot actually serve.
func TestProvidersAPI_TestModelSendsChatRequest(t *testing.T) {
	setupProvidersHandlerTest(t)

	var gotPath, gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		gotModel = payload.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	p, err := service.CreateProvider(model.Provider{
		Name: "Chat", Protocol: model.ProviderProtocolOpenAI, BaseURL: srv.URL + "/v1", APIKey: "sk-chat",
	})
	require.NoError(t, err)

	out := runModelTest(t, p.ID, map[string]any{"model": "gpt-x"})
	assert.True(t, out.OK, out.Error)
	assert.Equal(t, "/v1/chat/completions", gotPath)
	assert.Equal(t, "Bearer sk-chat", gotAuth)
	assert.Equal(t, "gpt-x", gotModel)
	assert.Equal(t, "gpt-x", out.Model)
	assert.Equal(t, http.StatusOK, out.Status)
	assert.Equal(t, "ok", out.Reply)
}

func TestProvidersAPI_TestModelReportsUpstreamError(t *testing.T) {
	setupProvidersHandlerTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"The model does not exist"}}`))
	}))
	defer srv.Close()

	p, err := service.CreateProvider(model.Provider{
		Name: "Broken", Protocol: model.ProviderProtocolOpenAI, BaseURL: srv.URL, APIKey: "k",
	})
	require.NoError(t, err)

	out := runModelTest(t, p.ID, map[string]any{"model": "ghost-model"})
	assert.False(t, out.OK)
	assert.Equal(t, http.StatusNotFound, out.Status)
	assert.Contains(t, out.Error, "The model does not exist",
		"the vendor message is the whole point of the test")
}

// The dialog tests what is typed, not what was saved — otherwise a wrong key
// has to be persisted before the user can find out it is wrong.
func TestProvidersAPI_TestModelUsesDraftOverrides(t *testing.T) {
	setupProvidersHandlerTest(t)

	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	p, err := service.CreateProvider(model.Provider{
		Name: "Draft", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://saved.example/v1", APIKey: "sk-saved",
	})
	require.NoError(t, err)

	out := runModelTest(t, p.ID, map[string]any{
		"model": "m", "baseUrl": srv.URL + "/v1", "apiKey": "sk-draft",
	})
	assert.True(t, out.OK, out.Error)
	assert.Equal(t, "/v1/chat/completions", gotPath)
	assert.Equal(t, "Bearer sk-draft", gotAuth)

	// The saved row is untouched by the probe.
	saved, err := service.GetProvider(p.ID)
	require.NoError(t, err)
	assert.Equal(t, "https://saved.example/v1", saved.BaseURL)
	assert.Equal(t, "sk-saved", saved.APIKey)
}

// Anthropic-compatible gateways disagree about the header: auto means
// x-api-key, bearer means Authorization. The probe has to follow the saved mode.
func TestProvidersAPI_TestModelAnthropicAuthMode(t *testing.T) {
	setupProvidersHandlerTest(t)

	type captured struct{ apiKey, auth, version string }
	cases := []struct {
		name    string
		mode    string
		wantKey string
		wantHdr string
	}{
		{name: "auto uses x-api-key", mode: model.ProviderAuthAuto, wantKey: "sk-a"},
		{name: "bearer uses Authorization", mode: model.ProviderAuthBearer, wantHdr: "Bearer sk-a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := captured{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = captured{
					apiKey:  r.Header.Get("x-api-key"),
					auth:    r.Header.Get("Authorization"),
					version: r.Header.Get("anthropic-version"),
				}
				_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
			}))
			defer srv.Close()

			p, err := service.CreateProvider(model.Provider{
				Name: "Anthropic " + tc.mode, Protocol: model.ProviderProtocolAnthropic,
				BaseURL: srv.URL, APIKey: "sk-a", AuthMode: tc.mode,
			})
			require.NoError(t, err)

			out := runModelTest(t, p.ID, map[string]any{"model": "claude-x"})
			assert.True(t, out.OK, out.Error)
			assert.Equal(t, tc.wantKey, got.apiKey)
			assert.Equal(t, tc.wantHdr, got.auth)
			assert.Equal(t, "2023-06-01", got.version)
			assert.Equal(t, "ok", out.Reply)
		})
	}
}

// Gemini carries the model in the path, so the probe cannot reuse the fixed
// endpoint builder the other protocols share.
func TestProvidersAPI_TestModelGeminiPath(t *testing.T) {
	setupProvidersHandlerTest(t)

	var gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	defer srv.Close()

	p, err := service.CreateProvider(model.Provider{
		Name: "Gem", Protocol: model.ProviderProtocolGemini, BaseURL: srv.URL, APIKey: "g-key",
	})
	require.NoError(t, err)

	out := runModelTest(t, p.ID, map[string]any{"model": "gemini-2.0-flash"})
	assert.True(t, out.OK, out.Error)
	assert.Equal(t, "/v1beta/models/gemini-2.0-flash:generateContent", gotPath)
	assert.Equal(t, "g-key", gotKey)
	assert.Equal(t, "ok", out.Reply)
}

// Without a model the endpoint keeps its old meaning: probe the catalog.
func TestProvidersAPI_TestWithoutModelStillProbesCatalog(t *testing.T) {
	setupProvidersHandlerTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("catalog probe must stay a GET, got %s", r.Method)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"a"}]}`))
	}))
	defer srv.Close()

	p, err := service.CreateProvider(model.Provider{
		Name: "Catalog", Protocol: model.ProviderProtocolOpenAI, BaseURL: srv.URL, APIKey: "k",
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodPost, "/api/providers/test",
		map[string]any{"id": p.ID})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		OK     bool     `json:"ok"`
		Models []string `json:"models"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.True(t, out.OK)
	assert.Equal(t, []string{"a"}, out.Models)
}

func TestProvidersAPI_TestModelRejectsUnknownProtocolOverride(t *testing.T) {
	setupProvidersHandlerTest(t)

	p, err := service.CreateProvider(model.Provider{
		Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x/v1", APIKey: "k",
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodPost, "/api/providers/test",
		map[string]any{"id": p.ID, "model": "m", "protocol": "cohere"})
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderInvalid")
}

// Gemini is the one protocol whose endpoint carries the model, so its URL
// builder has to handle every base shape the other protocols get from
// summarize.BuildEndpointURL.
func TestGeminiGenerateURL(t *testing.T) {
	cases := []struct{ base, want string }{
		{"", ""},
		{
			"https://generativelanguage.googleapis.com",
			"https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent",
		},
		{
			"https://gw.example/v1beta/",
			"https://gw.example/v1beta/models/gemini-2.0-flash:generateContent",
		},
		{
			// A full endpoint keeps its address but must test the clicked model.
			"https://gw.example/v1beta/models/old-model:generateContent",
			"https://gw.example/v1beta/models/gemini-2.0-flash:generateContent",
		},
		{
			"https://gw.example/gemini",
			"https://gw.example/gemini/v1beta/models/gemini-2.0-flash:generateContent",
		},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, geminiGenerateURL(tc.base, "gemini-2.0-flash"), tc.base)
	}
}

// Every vendor wraps the reason differently, and the reason is what the user
// needs to see — an opaque "HTTP 401" sends them hunting for the wrong bug.
func TestProviderProbeErrorShapes(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusUnauthorized, `{"error":{"message":"bad key"}}`, "HTTP 401: bad key"},
		{http.StatusBadGateway, `{"error":"gateway down"}`, "HTTP 502: gateway down"},
		{http.StatusBadRequest, `{"message":"nope"}`, "HTTP 400: nope"},
		{http.StatusInternalServerError, "<html>oops</html>", "HTTP 500: <html>oops</html>"},
		{http.StatusServiceUnavailable, "", "HTTP 503: Service Unavailable"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, providerProbeError(tc.status, []byte(tc.body)).Error(), tc.body)
	}
}

func TestClipProbeText(t *testing.T) {
	assert.Equal(t, "a b", clipProbeText("  a\n b  ", 10))
	assert.Equal(t, "aaaa…", clipProbeText("aaaa bbbb", 4))
	assert.Equal(t, "short", clipProbeText("short", 5))
}

// A row that predates the auth-mode rule can still be in the database, and the
// binding is the last gate before it reaches a spawn path that fails every turn.
func TestAgentPatch_RejectsProviderWhoseAuthModeTheBackendCannotExpress(t *testing.T) {
	setupProvidersHandlerTest(t)

	// Inserted directly: CreateProvider now refuses this combination, but rows
	// created before that check existed are still around.
	_, err := store.WriteExec(`INSERT INTO providers
		(name, protocol, base_url, api_key, auth_mode, preset_id, models, default_model, wire_api)
		VALUES ('Legacy', 'openai', 'https://gw.example', 'k', 'bearer', '', '[]', '', 'responses')`)
	require.NoError(t, err)
	require.NoError(t, service.LoadProvidersIntoMemory())
	legacy := model.GetProvider(1)
	require.NotNil(t, legacy)
	require.Equal(t, model.ProviderAuthBearer, legacy.AuthMode)

	agent := &model.Agent{ID: "ds-agent", Name: "DeepSeek", Backend: "deepseek"}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": legacy.ID,
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderInvalid", "the error must be locatable, not a bare invalid_value")
	assert.Contains(t, rec.Body.String(), `"field":"authMode"`,
		"the settings form branches on detail.field; without it its message is dead code")
	assert.Contains(t, rec.Body.String(), "Bearer")
	assert.Contains(t, rec.Body.String(), "openai")
	assert.Nil(t, model.GetAgent(agent.ID).ProviderID, "a refused binding must not be persisted")

	// The same provider with the mode it can express binds fine.
	_, err = store.WriteExec(`UPDATE providers SET auth_mode = 'auto' WHERE id = ?`, legacy.ID)
	require.NoError(t, err)
	require.NoError(t, service.LoadProvidersIntoMemory())

	rec = doProviderRequest(t, ServeAgents, http.MethodPatch, "/api/agents", map[string]any{
		"id": agent.ID, "provider_id": legacy.ID,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotNil(t, model.GetAgent(agent.ID).ProviderID)
}

// A gateway answering /models with an HTML error page used to hand up to 1 MiB
// of markup to the settings dialog and the model picker.
func TestProbeProviderModels_TruncatesUpstreamBody(t *testing.T) {
	huge := strings.Repeat("<html><body>upstream gateway failure</body></html>", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, huge)
	}))
	defer srv.Close()

	p := &model.Provider{
		Protocol: model.ProviderProtocolOpenAI, BaseURL: srv.URL,
		APIKey: "k", AuthMode: model.ProviderAuthAuto,
	}
	_, status, err := probeProviderModels(context.Background(), p)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadGateway, status)
	assert.Contains(t, err.Error(), "HTTP 502")
	assert.Less(t, len(err.Error()), 400, "the body must be clipped, not echoed: %d bytes", len(err.Error()))
}

// A JSON envelope still wins over the raw body: the reason the user needs is in
// the message, not in the wrapper.
func TestProbeProviderModels_PrefersJSONErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()

	p := &model.Provider{
		Protocol: model.ProviderProtocolOpenAI, BaseURL: srv.URL,
		APIKey: "k", AuthMode: model.ProviderAuthAuto,
	}
	_, _, err := probeProviderModels(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid api key")
	assert.Contains(t, err.Error(), "HTTP 401")
}

// The ai_summary provider checks used bare fmt.Errorf, so the API helper (which
// keeps only `error`) showed "invalid_value" and the user could not tell which
// field was wrong.
func TestValidatePatchValues_AISummaryProviderErrorsAreLocatable(t *testing.T) {
	origConfig := model.ConfigInstance
	model.ConfigInstance = model.Config{}
	t.Cleanup(func() { model.ConfigInstance = origConfig })

	model.ReplaceProviders([]*model.Provider{{
		ID: 9, Name: "Gem", Protocol: model.ProviderProtocolGemini,
		BaseURL: "https://gem.example", APIKey: "k", AuthMode: model.ProviderAuthAuto,
	}})
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	err := validatePatchValues(map[string]any{
		"ai_summary": map[string]any{"provider_id": float64(9)},
	})
	var valueErr *configValueError
	require.ErrorAs(t, err, &valueErr, "must be a configValueError so serveConfigPatch localizes it")
	assert.Equal(t, "ProviderInvalid", valueErr.MsgKey)
	assert.Equal(t, "ai_summary.provider_id", valueErr.Field)
	assert.Contains(t, fmt.Sprint(valueErr.templateData()["Reason"]), "gemini")
	assert.Equal(t, "ai_summary.provider_id", valueErr.templateData()["field"])
}

// The settings form replaces the raw reason with its own wording when the
// server says WHICH field is wrong. That branch (`msgKey === 'ProviderInvalid'
// && detail.field === 'authMode'`) never fired because the response carried no
// field at all — the reason named internal identifiers instead.
func TestProvidersAPI_InvalidAuthModeCarriesField(t *testing.T) {
	setupProvidersHandlerTest(t)

	rec := doProviderRequest(t, ServeProviders, http.MethodPost, "/api/providers", map[string]any{
		"name": "Bearer", "protocol": "openai", "baseUrl": "https://gw.example",
		"apiKey": "sk", "authMode": "bearer",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"field":"authMode"`)

	var body struct {
		MsgKey string         `json:"msgKey"`
		Detail map[string]any `json:"detail"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ProviderInvalid", body.MsgKey)
	assert.Equal(t, "authMode", body.Detail["field"], "this is the exact key the form reads")
	assert.Contains(t, body.Detail["Reason"], "bearer")
}

// A rejected protocol change names its own field, so the form can point at the
// right control instead of blaming the auth mode.
func TestProvidersAPI_InvalidProtocolCarriesField(t *testing.T) {
	setupProvidersHandlerTest(t)

	created, err := service.CreateProvider(model.Provider{Name: "P", Protocol: model.ProviderProtocolAnthropic, APIKey: "k"})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodPatch, fmt.Sprintf("/api/providers/%d", created.ID),
		map[string]any{"protocol": "bogus"})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"field":"protocol"`)
}

// clearSummaryBinding detaches the shared AI-summary model from any provider.
// Other tests in this package set it globally without restoring it, and a stale
// binding makes DeleteProvider refuse with ErrProviderUsedBySummary.
func clearSummaryBinding(t *testing.T) {
	t.Helper()
	prev := model.ConfigInstance.AISummary
	model.ConfigInstance.AISummary.ProviderID = nil
	t.Cleanup(func() { model.ConfigInstance.AISummary = prev })
}

// ── Method and routing guards ───────────────────────────────

func TestProvidersAPI_MethodGuards(t *testing.T) {
	setupProvidersHandlerTest(t)

	p, err := service.CreateProvider(model.Provider{Name: "P", Protocol: model.ProviderProtocolOpenAI})
	require.NoError(t, err)
	base := fmt.Sprintf("/api/providers/%d", p.ID)

	cases := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
	}{
		{"collection", ServeProviders, http.MethodDelete, "/api/providers"},
		{"sub-resource", ServeProviderSubRoutes, http.MethodPost, base},
		{"presets", ServeProviderSubRoutes, http.MethodPost, "/api/providers/presets"},
		{"test", ServeProviderSubRoutes, http.MethodGet, "/api/providers/test"},
		{"models", ServeProviderSubRoutes, http.MethodPost, base + "/models"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doProviderRequest(t, tc.handler, tc.method, tc.path, nil)
			assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, rec.Body.String())
		})
	}
}

// A malformed body must be a 400 on every write entry point, not a panic or a
// silently-created row with zero fields.
func TestProvidersAPI_MalformedBodies(t *testing.T) {
	setupProvidersHandlerTest(t)

	p, err := service.CreateProvider(model.Provider{Name: "P", Protocol: model.ProviderProtocolOpenAI})
	require.NoError(t, err)
	base := fmt.Sprintf("/api/providers/%d", p.ID)

	cases := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
	}{
		{"create", ServeProviders, http.MethodPost, "/api/providers"},
		{"update", ServeProviderSubRoutes, http.MethodPatch, base},
		{"test", ServeProviderSubRoutes, http.MethodPost, "/api/providers/test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doProviderRequest(t, tc.handler, tc.method, tc.path, "not-an-object")
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

// Anything that is not a positive integer id, or a sub-resource other than
// /models, is a 404: these paths are user-visible URLs, not an internal API.
func TestProviderSubRoutes_InvalidPathsAreNotFound(t *testing.T) {
	setupProvidersHandlerTest(t)

	for _, path := range []string{
		"/api/providers/abc",
		"/api/providers/0",
		"/api/providers/-1",
		"/api/providers/abc/models",
		"/api/providers/0/models",
		"/api/providers/1/unknown-subresource",
	} {
		rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, path, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
	}
}

// ── Update / delete success paths ───────────────────────────

func TestProvidersAPI_UpdateReturnsRowWithoutKey(t *testing.T) {
	setupProvidersHandlerTest(t)

	p, err := service.CreateProvider(model.Provider{
		Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://a/v1", APIKey: "sk-hidden",
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodPatch, fmt.Sprintf("/api/providers/%d", p.ID),
		map[string]any{"name": "Renamed", "baseUrl": "https://b/v1", "clearApiKey": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"name":"Renamed"`)
	assert.Contains(t, rec.Body.String(), `"baseUrl":"https://b/v1"`)
	assert.Contains(t, rec.Body.String(), `"hasKey":false`)
	assert.NotContains(t, rec.Body.String(), "sk-hidden")

	// PUT is the same handler.
	rec = doProviderRequest(t, ServeProviderSubRoutes, http.MethodPut, fmt.Sprintf("/api/providers/%d", p.ID),
		map[string]any{"name": "RenamedAgain"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"name":"RenamedAgain"`)
}

func TestProvidersAPI_DeleteSucceeds(t *testing.T) {
	setupProvidersHandlerTest(t)
	clearSummaryBinding(t)

	p, err := service.CreateProvider(model.Provider{Name: "P", Protocol: model.ProviderProtocolOpenAI})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodDelete, fmt.Sprintf("/api/providers/%d", p.ID), nil)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	_, err = service.GetProvider(p.ID)
	assert.ErrorIs(t, err, service.ErrProviderNotFound)
}

func TestProvidersAPI_DetailNotFound(t *testing.T) {
	setupProvidersHandlerTest(t)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, "/api/providers/404", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderNotFound")

	rec = doProviderRequest(t, ServeProviderSubRoutes, http.MethodDelete, "/api/providers/404", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderNotFound")
}

// The summary model's binding must block the delete: it is the only
// configuration the summarizer has, so removing the row breaks it silently.
func TestProvidersAPI_DeleteRefusedWhenSummaryUsesIt(t *testing.T) {
	setupProvidersHandlerTest(t)

	p, err := service.CreateProvider(model.Provider{
		Name: "Summary", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x/v1", APIKey: "k",
	})
	require.NoError(t, err)

	prev := model.ConfigInstance.AISummary
	pid := p.ID
	model.ConfigInstance.AISummary.ProviderID = &pid
	t.Cleanup(func() { model.ConfigInstance.AISummary = prev })

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodDelete, fmt.Sprintf("/api/providers/%d", p.ID), nil)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderUsedBySummary")
}

// A protocol change that the bound agents cannot speak must name them, so the
// user knows which agents block the edit.
func TestProvidersAPI_ProtocolConflictNamesAgents(t *testing.T) {
	setupProvidersHandlerTest(t)

	p, err := service.CreateProvider(model.Provider{Name: "P", Protocol: model.ProviderProtocolAnthropic, APIKey: "k"})
	require.NoError(t, err)
	require.NoError(t, service.SaveAgent(store.WriteDB(), &model.Agent{
		ID: "claude-a", Name: "Claude Agent", Backend: "claude", ProviderID: &p.ID,
	}))

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodPatch, fmt.Sprintf("/api/providers/%d", p.ID),
		map[string]any{"protocol": "openai"})
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderProtocolConflict")
	assert.Contains(t, rec.Body.String(), "Claude Agent")

	stored, err := service.GetProvider(p.ID)
	require.NoError(t, err)
	assert.Equal(t, model.ProviderProtocolAnthropic, stored.Protocol, "the rejected change must not be stored")
}

// A storage failure has no sentinel to map, so it must be a generic 500 — never
// a 4xx that would tell the user to fix their input.
func TestProvidersAPI_StorageFailureIsInternalError(t *testing.T) {
	setupProvidersHandlerTest(t)
	clearSummaryBinding(t)

	p, err := service.CreateProvider(model.Provider{Name: "P", Protocol: model.ProviderProtocolOpenAI})
	require.NoError(t, err)
	_, err = store.WriteExec(`CREATE TRIGGER reject_provider_delete BEFORE DELETE ON providers
		BEGIN SELECT RAISE(ABORT, 'delete blocked'); END`)
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodDelete, fmt.Sprintf("/api/providers/%d", p.ID), nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "InternalError")
}

// ── List read failures ──────────────────────────────────────

// The list is the settings page's only data source; a schema failure must be a
// 500 rather than an empty list that looks like "no providers configured".
func TestProvidersAPI_ListReportsReadFailures(t *testing.T) {
	setupProvidersHandlerTest(t)

	_, err := store.WriteExec("DROP TABLE providers")
	require.NoError(t, err)
	rec := doProviderRequest(t, ServeProviders, http.MethodGet, "/api/providers", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())

	// Usage counts come from the agents table; a broken one is also a 500.
	setupProvidersHandlerTest(t)
	_, err = service.CreateProvider(model.Provider{Name: "P", Protocol: model.ProviderProtocolOpenAI})
	require.NoError(t, err)
	_, err = store.WriteExec("DROP TABLE agents")
	require.NoError(t, err)
	rec = doProviderRequest(t, ServeProviders, http.MethodGet, "/api/providers", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

// The agent names are decoration on the card: when only they fail, the list
// must still render (with no names) instead of failing wholesale.
func TestProvidersAPI_ListSurvivesAgentNameFailure(t *testing.T) {
	setupProvidersHandlerTest(t)

	_, err := service.CreateProvider(model.Provider{Name: "P", Protocol: model.ProviderProtocolOpenAI})
	require.NoError(t, err)

	// A table that still supports the usage aggregate but has no `name` column.
	_, err = store.WriteExec("DROP TABLE agents")
	require.NoError(t, err)
	_, err = store.WriteExec("CREATE TABLE agents (provider_id INTEGER)")
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviders, http.MethodGet, "/api/providers", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list struct {
		Providers []providerResponse `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Providers, 1)
	assert.Empty(t, list.Providers[0].Agents)
}

// ── Test endpoint: input guards ─────────────────────────────

func TestProvidersAPI_TestEndpointGuards(t *testing.T) {
	setupProvidersHandlerTest(t)

	p, err := service.CreateProvider(model.Provider{
		Name: "P", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x/v1", APIKey: "k",
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodPost, "/api/providers/test",
		map[string]any{"id": 404, "model": "m"})
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	rec = doProviderRequest(t, ServeProviderSubRoutes, http.MethodPost, "/api/providers/test",
		map[string]any{"id": p.ID, "model": "m", "authMode": "oauth"})
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderInvalid")
}

// A provider saved without an endpoint cannot be probed; the answer must say
// so instead of reporting a connection error to nowhere.
func TestProvidersAPI_TestWithoutBaseURL(t *testing.T) {
	setupProvidersHandlerTest(t)

	p, err := service.CreateProvider(model.Provider{Name: "NoURL", Protocol: model.ProviderProtocolOpenAI, APIKey: "k"})
	require.NoError(t, err)

	out := runModelTest(t, p.ID, map[string]any{"model": "m"})
	assert.False(t, out.OK)
	assert.Contains(t, out.Error, "no base URL")
}

// The catalog probe failing is reported in the payload (HTTP 200): the picker
// still opens, it just has nothing to offer.
func TestProvidersAPI_TestCatalogProbeFailureIsReported(t *testing.T) {
	setupProvidersHandlerTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	p, err := service.CreateProvider(model.Provider{
		Name: "Broken", Protocol: model.ProviderProtocolOpenAI, BaseURL: srv.URL, APIKey: "k",
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodPost, "/api/providers/test",
		map[string]any{"id": p.ID})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		OK     bool     `json:"ok"`
		Status int      `json:"status"`
		Models []string `json:"models"`
		Error  string   `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.False(t, out.OK)
	assert.Equal(t, http.StatusUnauthorized, out.Status)
	assert.Empty(t, out.Models)
	assert.Contains(t, out.Error, "bad key")
}

// The dialog's unsaved protocol/auth fields must reach the probe: a gateway
// that only accepts one header turns "test what is typed" into the difference
// between a green test and a failing turn.
func TestProvidersAPI_TestDraftProtocolAndAuthMode(t *testing.T) {
	setupProvidersHandlerTest(t)

	var gotPath, gotAuth, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotVersion = r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()

	p, err := service.CreateProvider(model.Provider{
		Name: "Draft", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://saved.example/v1", APIKey: "sk",
	})
	require.NoError(t, err)

	out := runModelTest(t, p.ID, map[string]any{
		"model": "claude-x", "baseUrl": srv.URL, "protocol": "anthropic", "authMode": "bearer",
	})
	assert.True(t, out.OK, out.Error)
	assert.Equal(t, "/v1/messages", gotPath, "the draft protocol must decide the request shape")
	assert.Equal(t, "Bearer sk", gotAuth)
	assert.Equal(t, "2023-06-01", gotVersion)
}

// Gemini's bearer mode is a real gateway configuration; the probe must send the
// Authorization header rather than x-goog-api-key.
func TestProvidersAPI_TestGeminiBearerAuth(t *testing.T) {
	setupProvidersHandlerTest(t)

	var gotAuth, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotKey = r.Header.Get("x-goog-api-key")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	defer srv.Close()

	// Inserted directly: CreateProvider refuses bearer for the gemini
	// protocol, but rows written before that rule existed are still around.
	_, err := store.WriteExec(`INSERT INTO providers
		(name, protocol, base_url, api_key, auth_mode, preset_id, models, default_model, wire_api)
		VALUES ('GemBearer', 'gemini', ?, 'g-key', 'bearer', '', '[]', '', 'responses')`, srv.URL)
	require.NoError(t, err)
	require.NoError(t, service.LoadProvidersIntoMemory())

	out := runModelTest(t, 1, map[string]any{"model": "gemini-x"})
	assert.True(t, out.OK, out.Error)
	assert.Equal(t, "Bearer g-key", gotAuth)
	assert.Empty(t, gotKey)
}

// An endpoint that cannot even be turned into a request must be reported as a
// probe failure, not a panic or an empty reply.
func TestProvidersAPI_TestUnbuildableRequest(t *testing.T) {
	setupProvidersHandlerTest(t)

	p, err := service.CreateProvider(model.Provider{
		Name: "BadURL", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://x/v1", APIKey: "k",
	})
	require.NoError(t, err)

	out := runModelTest(t, p.ID, map[string]any{"model": "m", "protocol": "gemini", "baseUrl": "http://%zz"})
	assert.False(t, out.OK)
	assert.NotEmpty(t, out.Error)
}

// ── Reply extraction / model-list parsing ───────────────────

// An empty answer is not an error (a model capped at a few tokens can return
// nothing), but a body that is not JSON cannot be mistaken for a reply.
func TestProviderReplyText_HandlesEveryShape(t *testing.T) {
	assert.Equal(t, "a b", providerReplyText([]byte(`{"choices":[{"message":{"content":"a"}},{"message":{"content":" b"}}]}`)))
	assert.Equal(t, "c", providerReplyText([]byte(`{"content":[{"text":"c"}]}`)))
	assert.Equal(t, "d e", providerReplyText([]byte(`{"candidates":[{"content":{"parts":[{"text":"d"},{"text":" e"}]}}]}`)))
	assert.Equal(t, "", providerReplyText([]byte(`<html>not json</html>`)))
	assert.Equal(t, "", providerReplyText([]byte(`{"choices":[{"message":{"content":"  "}}]}`)))
}

func TestParseProviderModelIDs(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, parseProviderModelIDs([]byte(`{"data":[{"id":"a"},{"id":""},{"id":"b"}]}`)))
	assert.Equal(t, []string{"g1"}, parseProviderModelIDs([]byte(`{"models":[{"name":"g1"},{"name":""}]}`)))
	assert.Empty(t, parseProviderModelIDs([]byte(`{"data":[]}`)))
	assert.Nil(t, parseProviderModelIDs([]byte(`not json`)))
}

// A gateway whose host is not even a valid URL: the catalog probe must report
// the request-construction failure instead of silently returning "no models".
func TestProbeProviderModels_UnbuildableRequest(t *testing.T) {
	p := &model.Provider{Protocol: model.ProviderProtocolOpenAI, BaseURL: "http://exa mple.com", APIKey: "k"}
	_, _, err := probeProviderModels(context.Background(), p)
	require.Error(t, err)
}

// A transport failure is reported with the last status (0 here) so the caller
// can tell "unreachable" from "answered with an error".
func TestProbeProviderModels_TransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead := srv.URL
	srv.Close()

	p := &model.Provider{Protocol: model.ProviderProtocolOpenAI, BaseURL: dead, APIKey: "k"}
	models, status, err := probeProviderModels(context.Background(), p)
	require.Error(t, err)
	assert.Empty(t, models)
	assert.Zero(t, status)
}

// ── Models sub-resource ────────────────────────────────────

// A provider saved without an endpoint has nothing to probe: the curated list
// is returned as-is with an empty live list.
func TestProvidersAPI_ModelListWithoutBaseURL(t *testing.T) {
	setupProvidersHandlerTest(t)

	_, err := service.CreateProvider(model.Provider{
		Name: "NoURL", Protocol: model.ProviderProtocolOpenAI, APIKey: "k", Models: []string{"m1"},
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, "/api/providers/1/models", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Models  []string `json:"models"`
		Curated []string `json:"curated"`
		Source  string   `json:"source"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Empty(t, body.Models)
	assert.Equal(t, []string{"m1"}, body.Curated)
	assert.Empty(t, body.Source)
}

// A row with an empty models column (written before the JSON default existed)
// reads as nil; the response must still carry an empty array, not null.
func TestProvidersAPI_ModelListEmptyCuratedIsAnArray(t *testing.T) {
	setupProvidersHandlerTest(t)

	_, err := store.WriteExec(`INSERT INTO providers
		(name, protocol, base_url, api_key, auth_mode, preset_id, models, default_model, wire_api)
		VALUES ('Legacy', 'openai', '', 'k', 'auto', '', '', '', 'responses')`)
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, "/api/providers/1/models", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"curated":[]`)
	assert.Contains(t, rec.Body.String(), `"models":[]`)
}

func TestProvidersAPI_ModelListUnknownID(t *testing.T) {
	setupProvidersHandlerTest(t)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, "/api/providers/404/models", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderNotFound")
}

// Gemini-shaped model lists carry `name`, not `id`.
func TestProvidersAPI_ModelListGeminiShape(t *testing.T) {
	setupProvidersHandlerTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-a"},{"name":"models/gemini-b"}]}`))
	}))
	defer srv.Close()

	_, err := service.CreateProvider(model.Provider{
		Name: "Gem", Protocol: model.ProviderProtocolGemini, BaseURL: srv.URL, APIKey: "k",
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, "/api/providers/1/models", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Models []string `json:"models"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, []string{"models/gemini-a", "models/gemini-b"}, body.Models)
}

// A transport failure on the chat probe is reported in the payload: the model
// test is a diagnostic, so an unreachable endpoint is an answer, not a 500.
func TestProvidersAPI_TestModelUnreachable(t *testing.T) {
	setupProvidersHandlerTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead := srv.URL
	srv.Close()

	p, err := service.CreateProvider(model.Provider{
		Name: "Dead", Protocol: model.ProviderProtocolOpenAI, BaseURL: dead, APIKey: "k",
	})
	require.NoError(t, err)

	out := runModelTest(t, p.ID, map[string]any{"model": "m"})
	assert.False(t, out.OK)
	assert.NotEmpty(t, out.Error)
	assert.Zero(t, out.Status, "no response means no status")
}

// The provider model picker renders a price tag per row (issue #643). It deals
// in bare ids with no display names, so only exact-id matches can apply; ids the
// catalog does not know must be absent from the map rather than zero-valued,
// otherwise every unknown model would render "$0 / $0".
func TestProvidersAPI_ModelListCarriesPrices(t *testing.T) {
	setupProvidersHandlerTest(t)
	model.ReplaceCatalog(&model.Catalog{
		Providers: map[string]model.CatalogProvider{
			"zai": {
				ID: "zai", Protocol: model.ProviderProtocolOpenAI,
				Models: []model.CatalogModel{
					{ID: "glm-5.2", Name: "GLM-5.2", InputPrice: 1.4, OutputPrice: 4.4},
				},
			},
		},
	})
	t.Cleanup(func() { model.ReplaceCatalog(nil) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"glm-5.2"},{"id":"model-b"}]}`))
	}))
	defer srv.Close()

	_, err := service.CreateProvider(model.Provider{
		Name: "Priced", Protocol: model.ProviderProtocolOpenAI, BaseURL: srv.URL + "/v1", APIKey: "k",
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, "/api/providers/1/models", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Models []string                    `json:"models"`
		Prices map[string]model.ModelPrice `json:"prices"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	priced, ok := body.Prices["glm-5.2"]
	require.True(t, ok, "glm-5.2 must carry a price")
	assert.Equal(t, 1.4, priced.Input)
	assert.Equal(t, 4.4, priced.Output)

	_, ok = body.Prices["model-b"]
	assert.False(t, ok, "an unknown model must be absent, not zero-valued")
}

// A provider with no endpoint still lists its curated ids, so those need prices
// too — the picker shows them.
func TestProvidersAPI_ModelListPricesCuratedWithoutEndpoint(t *testing.T) {
	setupProvidersHandlerTest(t)
	model.ReplaceCatalog(&model.Catalog{
		Providers: map[string]model.CatalogProvider{
			"zai": {
				ID: "zai", Protocol: model.ProviderProtocolOpenAI,
				Models: []model.CatalogModel{
					{ID: "glm-5.2", Name: "GLM-5.2", InputPrice: 3, OutputPrice: 15},
				},
			},
		},
	})
	t.Cleanup(func() { model.ReplaceCatalog(nil) })

	_, err := service.CreateProvider(model.Provider{
		Name: "NoEndpoint", Protocol: model.ProviderProtocolOpenAI, Models: []string{"glm-5.2"},
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, "/api/providers/1/models", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Prices map[string]model.ModelPrice `json:"prices"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Contains(t, body.Prices, "glm-5.2")
	assert.Equal(t, 3.0, body.Prices["glm-5.2"].Input)
}

// opencode addresses a model as "<provider>/<model>". A provider's curated list
// stores bare ids (that is what the endpoint serves), so the picker entries must
// be namespaced — offering the bare id lets the user pick something opencode
// refuses to resolve (issue #651).
func TestAgents_ProviderModelsAreNamespacedForOpenCode(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider, err := service.CreateProvider(model.Provider{
		Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1", APIKey: "k",
		Models: []string{"glm-5.3-flash"}, DefaultModel: "glm-5.3-flash",
	})
	require.NoError(t, err)

	agent := &model.Agent{
		ID: "oc-bound", Name: "OC", Backend: "opencode", ProviderID: &provider.ID,
		Models: []model.AgentModel{{ID: "openai/gpt-6", Default: true}},
	}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodGet, "/api/agents", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Agents []struct {
			Models []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Default bool   `json:"default"`
			} `json:"models"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Agents, 1)
	require.Len(t, body.Agents[0].Models, 1)
	assert.Equal(t, "clawbench-openai/glm-5.3-flash", body.Agents[0].Models[0].ID,
		"the picker value must be the form opencode accepts")
	assert.Equal(t, "glm-5.3-flash", body.Agents[0].Models[0].Name,
		"the display name stays the model id the endpoint serves")
	assert.True(t, body.Agents[0].Models[0].Default)
}

// A bound provider's models must survive an ACP model report. The ACP list is
// authoritative for what the agent advertises, but a provider endpoint is
// user-chosen: its models are exactly what the user configured the agent to
// run, so replacing the list with the agent's own report made the binding
// unusable (issue #651, "provider models never appear in the picker").
func TestAgents_ProviderModelsSurviveACPReport(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider, err := service.CreateProvider(model.Provider{
		Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1", APIKey: "k",
		Models: []string{"glm-5.3-flash"}, DefaultModel: "glm-5.3-flash",
	})
	require.NoError(t, err)

	agent := &model.Agent{
		ID: "oc-acp", Name: "OC", Backend: "opencode", Transport: "acp-stdio",
		AcpCommand: "opencode acp", ProviderID: &provider.ID,
		Models: []model.AgentModel{{ID: "openai/gpt-6", Default: true}},
	}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	reg := ai.GetAgentCapabilityRegistry()
	reg.UpdateModels(agent.ID, []model.AgentModel{{ID: "openai/gpt-6", Name: "GPT-6"}})
	t.Cleanup(func() { reg.UpdateModels(agent.ID, nil) })

	rec := doProviderRequest(t, ServeAgents, http.MethodGet, "/api/agents", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Agents []struct {
			Models         []model.AgentModel `json:"models"`
			ProviderModels []model.AgentModel `json:"providerModels"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Agents, 1)

	ids := make([]string, 0, len(body.Agents[0].Models))
	for _, m := range body.Agents[0].Models {
		ids = append(ids, m.ID)
	}
	assert.Contains(t, ids, "clawbench-openai/glm-5.3-flash", "the provider model must be selectable")
	assert.Contains(t, ids, "openai/gpt-6", "the ACP-reported model must stay")

	// The picker groups by source, so the provider subset must be identifiable.
	require.Len(t, body.Agents[0].ProviderModels, 1)
	assert.Equal(t, "clawbench-openai/glm-5.3-flash", body.Agents[0].ProviderModels[0].ID)
}

// A gateway that namespaces its own ids ("openai/gpt-4o") must keep them: the
// picker value is what gets sent to the endpoint, so re-prefixing or stripping
// would produce an id the endpoint rejects (issue #651 review).
func TestAgents_ProviderModelsKeepForeignNamespace(t *testing.T) {
	setupProvidersHandlerTest(t)

	provider, err := service.CreateProvider(model.Provider{
		Name: "GW", Protocol: model.ProviderProtocolOpenAI, BaseURL: "https://gw.example/v1", APIKey: "k",
		Models: []string{"openai/gpt-4o"},
	})
	require.NoError(t, err)

	agent := &model.Agent{
		ID: "oc-foreign", Name: "OC", Backend: "opencode", ProviderID: &provider.ID,
		Models: []model.AgentModel{{ID: "openai/gpt-6", Default: true}},
	}
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodGet, "/api/agents", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Agents []struct {
			ProviderModels []model.AgentModel `json:"providerModels"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Agents, 1)
	require.Len(t, body.Agents[0].ProviderModels, 1)
	assert.Equal(t, "clawbench-openai/openai/gpt-4o", body.Agents[0].ProviderModels[0].ID,
		"an id the endpoint namespaces itself must not be re-prefixed")
}
