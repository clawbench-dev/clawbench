package handler

// Tests that the model picker actually receives prices over the API
// (issue #643). The lookup itself is covered in internal/model; what these
// pin is the wiring — a price computed but never attached to the response is
// the failure mode that leaves the picker unchanged.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// pricedCatalog installs a catalog with one priced model and restores the
// previous snapshot afterwards, so the rest of the package's tests are not
// affected by a leftover catalog.
func pricedCatalog(t *testing.T) {
	t.Helper()
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
}

// The CLI agent path: models come from the agent's own list, and the response
// must carry a price tag for the ones the catalog knows.
func TestServeAgentsGet_AttachesModelPrices(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pricedCatalog(t)

	agentID := "cli-priced"
	model.Agents = map[string]*model.Agent{
		agentID: {
			ID: agentID, Backend: "mock", Transport: "cli",
			Models: []model.AgentModel{
				{ID: "glm-5.2", Name: "GLM-5.2", Default: true},
				{ID: "no-such-model", Name: "No Such Model"},
			},
		},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeAgents(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Agents []struct {
			ID     string `json:"id"`
			Models []struct {
				ID    string            `json:"id"`
				Price *model.ModelPrice `json:"price"`
			} `json:"models"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Agents, 1)

	byID := make(map[string]*model.ModelPrice, len(resp.Agents[0].Models))
	for _, m := range resp.Agents[0].Models {
		byID[m.ID] = m.Price
	}

	priced, ok := byID["glm-5.2"]
	require.True(t, ok, "the priced model must still be in the list")
	require.NotNil(t, priced, "glm-5.2 must carry a price")
	assert.Equal(t, 1.4, priced.Input)
	assert.Equal(t, 4.4, priced.Output)
	assert.False(t, priced.Approx, "a vendor price is not approximate")

	// A model the catalog does not know must be left untouched rather than
	// given a made-up number — and must still be selectable.
	unpriced, ok := byID["no-such-model"]
	require.True(t, ok, "unknown models must not be dropped from the list")
	assert.Nil(t, unpriced, "an unknown model must have no price")
}
