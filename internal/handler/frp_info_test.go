package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/frp"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ServeFRPInfo tests ---

func TestServeFRPInfo_NilManager(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// No FRP manager set
	origRef := frpManagerRef
	origEnabled := frpEnabled
	frpManagerRef = nil
	frpEnabled = false
	defer func() {
		frpManagerRef = origRef
		frpEnabled = origEnabled
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/frp/info", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := httptest.NewRecorder()
	ServeFRPInfo(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, false, resp["enabled"])
	assert.Equal(t, false, resp["running"])
	assert.Equal(t, "disabled", resp["state"])
}

func TestServeFRPInfo_NilManager_EnabledInConfig(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origRef := frpManagerRef
	origEnabled := frpEnabled
	frpManagerRef = nil
	frpEnabled = true // config says enabled but manager is nil (not started yet)
	defer func() {
		frpManagerRef = origRef
		frpEnabled = origEnabled
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/frp/info", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := httptest.NewRecorder()
	ServeFRPInfo(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, true, resp["enabled"])
	assert.Equal(t, false, resp["running"])
}

func TestServeFRPInfo_WithManager(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "120.26.168.245",
		ServerPort: 7000,
	}
	mgr := frp.NewManager(cfg, 20000, 0)

	origRef := frpManagerRef
	origEnabled := frpEnabled
	SetFRPManager(mgr, true)
	defer func() {
		frpManagerRef = origRef
		frpEnabled = origEnabled
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/frp/info", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := httptest.NewRecorder()
	ServeFRPInfo(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, true, resp["enabled"])
	assert.Equal(t, "120.26.168.245", resp["server_addr"])
}

func TestServeFRPInfo_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/frp/info", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := httptest.NewRecorder()
	ServeFRPInfo(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- ServeFRPStatus tests ---

func TestServeFRPStatus_NilManager(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origRef := frpManagerRef
	origEnabled := frpEnabled
	frpManagerRef = nil
	frpEnabled = false
	defer func() {
		frpManagerRef = origRef
		frpEnabled = origEnabled
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/frp/status", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := httptest.NewRecorder()
	ServeFRPStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, false, resp["enabled"])
	assert.Equal(t, "disabled", resp["state"])
}

func TestServeFRPStatus_WithManager(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.FRPConfig{
		Enabled:    true,
		ServerAddr: "frp-server",
	}
	mgr := frp.NewManager(cfg, 20000, 0)

	origRef := frpManagerRef
	origEnabled := frpEnabled
	SetFRPManager(mgr, true)
	defer func() {
		frpManagerRef = origRef
		frpEnabled = origEnabled
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/frp/status", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := httptest.NewRecorder()
	ServeFRPStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, true, resp["enabled"])
	// ServeFRPStatus only exposes enabled + running + state (no addresses)
	assert.NotContains(t, resp, "server_addr")
}

func TestServeFRPStatus_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodDelete, "/api/frp/status", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := httptest.NewRecorder()
	ServeFRPStatus(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- SetFRPManager / GetFRPManager ---

func TestSetFRPManager_GetFRPManager(t *testing.T) {
	origRef := frpManagerRef
	origEnabled := frpEnabled
	defer func() {
		frpManagerRef = origRef
		frpEnabled = origEnabled
	}()

	// Initially nil
	assert.Nil(t, GetFRPManager())

	// Set a manager
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "server"}
	mgr := frp.NewManager(cfg, 20000, 0)
	SetFRPManager(mgr, true)

	assert.Equal(t, mgr, GetFRPManager())
	assert.Equal(t, mgr, frpManagerRef)
	assert.True(t, frpEnabled)

	// Reset
	SetFRPManager(nil, false)
	assert.Nil(t, GetFRPManager())
	assert.False(t, frpEnabled)
}
