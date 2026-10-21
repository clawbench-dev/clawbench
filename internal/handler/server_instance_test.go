package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// instanceResponse mirrors the JSON written by ServeServerInstance.
type instanceResponse struct {
	InstanceID string `json:"instance_id"`
	StartedAt  string `json:"started_at"`
	Version    string `json:"version"`
}

func callInstance(t *testing.T) instanceResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/server/instance", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeServerInstance, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp instanceResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

// TestServeServerInstance_StableIDWithinProcess is the property the restart
// poller depends on: the id must NOT change between two calls served by the
// same process. If it did, a poll would see a "new" instance and reload the
// page while the old process is still running.
func TestServeServerInstance_StableIDWithinProcess(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	first := callInstance(t)
	second := callInstance(t)

	assert.NotEmpty(t, first.InstanceID, "instance id must be non-empty")
	assert.Equal(t, first.InstanceID, second.InstanceID,
		"the same process must report the same instance id")
	assert.Equal(t, first.StartedAt, second.StartedAt)
}

// TestServeServerInstance_ReportsVersion pins that the endpoint reports the
// build identity alongside the process identity, so a caller can distinguish
// "restarted same build" from "restarted onto a new build".
func TestServeServerInstance_ReportsVersion(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	resp := callInstance(t)
	assert.Equal(t, version.Get(), resp.Version)
	assert.NotEmpty(t, resp.StartedAt)
}

// TestServeServerInstance_MethodNotAllowed: the poller only ever GETs, so any
// other verb is a client bug worth surfacing rather than silently accepting.
func TestServeServerInstance_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/server/instance", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeServerInstance, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestNewInstanceID_Unique verifies the identity actually differs per process.
// A constant (e.g. the version string) would make every restart look like a
// no-op to the poller.
func TestNewInstanceID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		id := newInstanceID()
		assert.NotEmpty(t, id)
		assert.False(t, seen[id], "duplicate instance id %q", id)
		seen[id] = true
	}
}
