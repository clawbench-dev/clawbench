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

// The `available` field is the one part of the FRP API surface that must behave
// identically in BOTH build variants, so its tests are untagged and run under
// `nofrp` too.
//
// It reports a property of the BUILD (was the frp library compiled in?), not of
// the runtime config, and it is what the frontend reads to hide the FRP
// settings panel. Asserting against frp.Available() rather than a literal `true`
// is deliberate: the same test then pins the correct value in either build,
// and a build that silently reports the wrong one fails here.

func frpInfoResponse(t *testing.T) map[string]any {
	t.Helper()
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

	req := httptest.NewRequest(http.MethodGet, "/api/frp/info", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := httptest.NewRecorder()
	ServeFRPInfo(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestServeFRPInfo_ReportsBuildAvailability(t *testing.T) {
	resp := frpInfoResponse(t)

	// Presence and value together: a dropped field decodes to nil and fails
	// just as loudly as a wrong value.
	assert.Equal(t, frp.Available(), resp["available"],
		"`available` must mirror frp.Available() for this build")
}

func TestServeFRPStatus_ReportsBuildAvailability(t *testing.T) {
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

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, frp.Available(), resp["available"])
}

// The config endpoint is what the settings page loads on startup, so it carries
// the same flag — without it the FRP row would render for a moment and then
// vanish, or never hide at all on a build that has no FRP.
func TestServeConfigGet_ReportsFRPAvailability(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/config", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := httptest.NewRecorder()
	ServeConfig(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	frpSection, ok := resp["frp"].(map[string]any)
	require.True(t, ok, "GET /api/config must include an frp section")
	assert.Equal(t, frp.Available(), frpSection["available"],
		"the config response must carry the same availability flag as /api/frp/info")
}
