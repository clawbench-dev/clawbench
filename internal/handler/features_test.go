package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/plugins"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// featuresResponse mirrors the JSON body so the test pins the wire shape
// (field names are the contract with the frontend).
type featuresResponse struct {
	Features []struct {
		ID        string `json:"id"`
		Enabled   bool   `json:"enabled"`
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
		DefaultOn bool   `json:"defaultOn"`
	} `json:"features"`
}

func serveFeaturesForTest(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	ServeFeatures(w, httptest.NewRequest(http.MethodGet, "/api/features", http.NoBody))
	return w
}

func TestServeFeatures_ReportsAllThreeAxes(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)

	plugins.Register(&plugins.Plugin{ID: "alpha", DefaultOn: true})
	plugins.Register(&plugins.Plugin{
		ID:        "beta",
		Section:   "mail",
		Enabled:   func() bool { return false },
		DefaultOn: true,
	})
	plugins.Register(&plugins.Plugin{
		ID:        "gamma",
		Enabled:   func() bool { return true },
		Available: func() bool { return false },
	})

	w := serveFeaturesForTest(t)
	require.Equal(t, http.StatusOK, w.Code)

	var body featuresResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Features, 3)

	// Order is the registry's (sorted by ID), so /api/features is stable.
	assert.Equal(t, "alpha", body.Features[0].ID)
	assert.Equal(t, "beta", body.Features[1].ID)
	assert.Equal(t, "gamma", body.Features[2].ID)

	// No Enabled/Available funcs at all: always on, always available.
	assert.True(t, body.Features[0].Enabled)
	assert.True(t, body.Features[0].Available)
	assert.Equal(t, "", body.Features[0].Reason)
	assert.True(t, body.Features[0].DefaultOn)

	// Switched off.
	assert.False(t, body.Features[1].Enabled)
	assert.True(t, body.Features[1].Available)
	assert.Equal(t, "disabled", body.Features[1].Reason)

	// On, but the host cannot run it.
	assert.True(t, body.Features[2].Enabled)
	assert.False(t, body.Features[2].Available)
	assert.Equal(t, "capability", body.Features[2].Reason)
}

func TestServeFeatures_DisabledWinsOverCapability(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)

	// A switched-off feature that ALSO cannot run here must report "disabled":
	// the user's fix is to turn it on, not to change platform.
	plugins.Register(&plugins.Plugin{
		ID:        "terminal",
		Section:   "terminal",
		Enabled:   func() bool { return false },
		Available: func() bool { return false },
	})

	w := serveFeaturesForTest(t)
	require.Equal(t, http.StatusOK, w.Code)

	var body featuresResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Features, 1)
	assert.Equal(t, "disabled", body.Features[0].Reason)
}

func TestServeFeatures_EmptyRegistrySerialisesAsArray(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)

	w := serveFeaturesForTest(t)
	require.Equal(t, http.StatusOK, w.Code)
	// `null` would make the frontend's `for (const f of features)` throw.
	assert.Contains(t, w.Body.String(), `"features":[]`)
}

func TestServeFeatures_RejectsNonGet(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)

	w := httptest.NewRecorder()
	ServeFeatures(w, httptest.NewRequest(http.MethodPost, "/api/features", http.NoBody))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
