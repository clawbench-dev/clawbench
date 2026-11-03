package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
)

const catalogSnapshotFixture = `{
  "fetched_at": "2026-10-31T00:00:00Z",
  "source": "https://models.dev/api.json",
  "providers": {
    "anthropic": {
      "id": "anthropic", "name": "Anthropic", "protocol": "anthropic",
      "baseUrl": "https://api.anthropic.com",
      "models": [
        {"id": "claude-a", "name": "Claude A"},
        {"id": "claude-b", "name": "Claude B"}
      ]
    }
  }
}`

// installCatalog writes a snapshot and loads it, so handler tests exercise the
// same file → memory path the server uses at boot.
func installCatalog(t *testing.T) {
	t.Helper()
	path := model.CatalogPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(catalogSnapshotFixture), 0o600))
	require.NoError(t, model.LoadCatalogFromDisk())
}

func TestCatalogEndpoint_Status(t *testing.T) {
	setupProvidersHandlerTest(t)
	installCatalog(t)

	rec := doProviderRequest(t, ServeCatalog, http.MethodGet, "/api/catalog", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		FetchedAt string `json:"fetchedAt"`
		Stale     bool   `json:"stale"`
		Providers []struct {
			ID         string `json:"id"`
			ModelCount int    `json:"modelCount"`
		} `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.False(t, body.Stale)
	require.Len(t, body.Providers, 1)
	assert.Equal(t, "anthropic", body.Providers[0].ID)
	assert.Equal(t, 2, body.Providers[0].ModelCount)
}

func TestCatalogEndpoint_EmptySnapshotIsNotAnError(t *testing.T) {
	setupProvidersHandlerTest(t)
	// A corrupt/absent snapshot must still answer 200 with stale=true; the
	// settings page decides whether to offer a refresh.
	model.ReplaceCatalog(nil)
	require.NoError(t, os.RemoveAll(model.CatalogPath()))

	rec := doProviderRequest(t, ServeCatalog, http.MethodGet, "/api/catalog", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Stale     bool  `json:"stale"`
		Providers []any `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Empty(t, body.Providers)
}

func TestProviderModels_FallsBackToCatalog(t *testing.T) {
	setupProvidersHandlerTest(t)
	installCatalog(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no catalog", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := service.CreateProvider(model.Provider{
		Name: "Anthropic GW", Protocol: model.ProviderProtocolAnthropic,
		BaseURL: srv.URL, APIKey: "k", PresetID: "anthropic",
	})
	require.NoError(t, err)

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodGet, "/api/providers/1/models", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Models []string `json:"models"`
		Source string   `json:"source"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, []string{"claude-a", "claude-b"}, body.Models)
	assert.Equal(t, "catalog", body.Source)
}

func TestAgents_ExposeCatalogModelsBeforeACPReport(t *testing.T) {
	setupProvidersHandlerTest(t)
	installCatalog(t)

	provider, err := service.CreateProvider(model.Provider{
		Name: "Anthropic GW", Protocol: model.ProviderProtocolAnthropic,
		BaseURL: "https://gw.example", APIKey: "k", PresetID: "anthropic",
	})
	require.NoError(t, err)

	agent := &model.Agent{
		ID: "acp-catalog-agent", Name: "ACP Agent", Backend: "claude",
		Transport: "acp-stdio", AcpCommand: "claude-code-acp", ProviderID: &provider.ID,
	}
	require.NoError(t, service.SaveAgent(store.WriteDB(), agent))
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	rec := doProviderRequest(t, ServeAgents, http.MethodGet, "/api/agents", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Agents []struct {
			ID            string `json:"id"`
			Models        []any  `json:"models"`
			CatalogModels []struct {
				ID string `json:"id"`
			} `json:"catalogModels"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Agents, 1)
	assert.Empty(t, body.Agents[0].Models, "unconfirmed ACP models must stay hidden")
	require.Len(t, body.Agents[0].CatalogModels, 2)
	assert.Equal(t, "claude-a", body.Agents[0].CatalogModels[0].ID)
}

func TestAgents_ACPModelsWinOverCatalog(t *testing.T) {
	setupProvidersHandlerTest(t)
	installCatalog(t)

	provider, err := service.CreateProvider(model.Provider{
		Name: "Anthropic GW", Protocol: model.ProviderProtocolAnthropic,
		BaseURL: "https://gw.example", APIKey: "k", PresetID: "anthropic",
	})
	require.NoError(t, err)

	agent := &model.Agent{
		ID: "acp-confirmed-agent", Name: "Confirmed", Backend: "claude",
		Transport: "acp-stdio", AcpCommand: "claude-code-acp", ProviderID: &provider.ID,
	}
	require.NoError(t, service.SaveAgent(store.WriteDB(), agent))
	model.ReplaceAgents(map[string]*model.Agent{agent.ID: agent}, []*model.Agent{agent})

	// The ACP runtime has reported a list: it is authoritative, and the
	// catalog must not appear at all.
	ai.GetAgentCapabilityRegistry().Update(agent.ID, &ai.AgentCapability{
		AvailableModels: []model.AgentModel{{ID: "real-model", Name: "Real Model", Default: true}},
	})

	rec := doProviderRequest(t, ServeAgents, http.MethodGet, "/api/agents", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Agents []struct {
			Models        []struct{ ID string } `json:"models"`
			CatalogModels []struct{ ID string } `json:"catalogModels"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Agents, 1)
	require.Len(t, body.Agents[0].Models, 1)
	assert.Equal(t, "real-model", body.Agents[0].Models[0].ID)
	assert.Empty(t, body.Agents[0].CatalogModels, "a confirmed ACP list must not be mixed with the catalog")
}

// A forced refresh that fails upstream must be a 502 with the reason, so the
// settings page can show it instead of silently keeping a stale list.
func TestCatalogEndpoint_RefreshFailureIsBadGateway(t *testing.T) {
	setupProvidersHandlerTest(t)

	// An already-cancelled request context fails before any network call: the
	// refresh reports the context error without dialing models.dev.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodPost, "/api/catalog", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	ServeCatalog(w, r)

	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "context canceled")
}

func TestCatalogEndpoint_MethodNotAllowed(t *testing.T) {
	setupProvidersHandlerTest(t)
	rec := doProviderRequest(t, ServeCatalog, http.MethodDelete, "/api/catalog", nil)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, rec.Body.String())
}

// Providers with the same display name are ordered by id, so the settings list
// does not reshuffle between two identical requests (map iteration order is
// random, so without the tie-break the order would change every call).
func TestCatalogEndpoint_OrdersSameNameProvidersByID(t *testing.T) {
	setupProvidersHandlerTest(t)
	model.ReplaceCatalog(&model.Catalog{
		FetchedAt: time.Now(),
		Source:    "test",
		Providers: map[string]model.CatalogProvider{
			"z-last": {ID: "z-last", Name: "Same", Protocol: model.ProviderProtocolOpenAI},
			"a-mid":  {ID: "a-mid", Name: "Same", Protocol: model.ProviderProtocolOpenAI},
			"first":  {ID: "first", Name: "Aardvark", Protocol: model.ProviderProtocolOpenAI},
		},
	})
	t.Cleanup(func() { model.ReplaceCatalog(nil) })

	rec := doProviderRequest(t, ServeCatalog, http.MethodGet, "/api/catalog", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Providers []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Providers, 3)
	assert.Equal(t, []string{"first", "a-mid", "z-last"}, []string{
		body.Providers[0].ID, body.Providers[1].ID, body.Providers[2].ID,
	})
}
