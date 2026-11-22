package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/version"
)

func TestServeHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", http.NoBody)
	w := httptest.NewRecorder()

	ServeHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp healthResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	assert.NoError(t, err)
	assert.Equal(t, "clawbench", resp.App)
	assert.NotEmpty(t, resp.Version)
	// The gate compares this number instead of the version string, so a zero
	// here would silently fall back to the strict version comparison on every
	// client.
	assert.Equal(t, version.ClientProtocol, resp.Protocol)
	assert.Greater(t, resp.Protocol, 0)
	// The desktop shell compares this with the child it spawned. A missing or
	// wrong value would make it accept a stale server on the same port.
	assert.Equal(t, os.Getpid(), resp.PID)
	// A standalone server reports false; the desktop shell uses this to decide
	// whether a running server on the shared data directory may be replaced.
	assert.False(t, resp.Managed)

	origManaged := model.ManagedByEmbedder
	model.ManagedByEmbedder = true
	t.Cleanup(func() { model.ManagedByEmbedder = origManaged })
	w = httptest.NewRecorder()
	ServeHealth(w, req)
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.Managed, "an embedded server must identify itself")
}
