package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── ServeUpgradeCheck ──

func TestServeUpgradeCheck_Success(t *testing.T) {
	defer func() {
		upgradeCheckForUpgrade = service.CheckForUpgrade
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
	}()

	upgradeCheckForUpgrade = func() (string, string, error) {
		return "1.0.0", "1.1.0", nil
	}
	upgradeCompareVersions = func(a, b string) int {
		if a < b {
			return -1
		}
		return 0
	}
	upgradeIsDevBuild = func(v string) bool { return false }

	req := newRequest(t, http.MethodGet, "/api/upgrade/check", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeCheck, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "1.0.0", resp["current_version"])
	assert.Equal(t, "1.1.0", resp["latest_version"])
	assert.Equal(t, true, resp["has_upgrade"])
}

func TestServeUpgradeCheck_NoUpgrade(t *testing.T) {
	defer func() {
		upgradeCheckForUpgrade = service.CheckForUpgrade
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
	}()

	upgradeCheckForUpgrade = func() (string, string, error) {
		return "1.1.0", "1.1.0", nil
	}
	upgradeCompareVersions = func(a, b string) int { return 0 }
	upgradeIsDevBuild = func(v string) bool { return false }

	req := newRequest(t, http.MethodGet, "/api/upgrade/check", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeCheck, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["has_upgrade"])
}

func TestServeUpgradeCheck_DevBuild(t *testing.T) {
	defer func() {
		upgradeCheckForUpgrade = service.CheckForUpgrade
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
	}()

	upgradeCheckForUpgrade = func() (string, string, error) {
		return "dev", "1.0.0", nil
	}
	upgradeCompareVersions = func(a, b string) int { return 1 } // dev > 1.0.0 lexicographically
	upgradeIsDevBuild = func(v string) bool { return v == "dev" }

	req := newRequest(t, http.MethodGet, "/api/upgrade/check", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeCheck, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["has_upgrade"], "dev build should always show has_upgrade=true")
}

func TestServeUpgradeCheck_Error(t *testing.T) {
	defer func() { upgradeCheckForUpgrade = service.CheckForUpgrade }()

	upgradeCheckForUpgrade = func() (string, string, error) {
		return "", "", errors.New("registry unreachable")
	}

	req := newRequest(t, http.MethodGet, "/api/upgrade/check", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeCheck, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestServeUpgradeCheck_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodPost, "/api/upgrade/check", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeCheck, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ── ServeUpgradeStart ──

func TestServeUpgradeStart_Success(t *testing.T) {
	defer func() {
		upgradeIsInProgress = service.IsUpgradeInProgress
		upgradePerformUpgrade = service.PerformUpgrade
	}()

	upgradeIsInProgress = func() bool { return false }
	upgradeCalled := false
	upgradePerformUpgrade = func() { upgradeCalled = true }

	req := newRequest(t, http.MethodPost, "/api/upgrade/start", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStart, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, upgradeCalled, "PerformUpgrade should have been called")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "started", resp["status"])
}

func TestServeUpgradeStart_AlreadyInProgress(t *testing.T) {
	defer func() { upgradeIsInProgress = service.IsUpgradeInProgress }()

	upgradeIsInProgress = func() bool { return true }

	req := newRequest(t, http.MethodPost, "/api/upgrade/start", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStart, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestServeUpgradeStart_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/upgrade/start", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStart, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ── ServeUpgradeStatus ──

func TestServeUpgradeStatus_Idle(t *testing.T) {
	defer func() { upgradeGetUpgradeState = service.GetUpgradeState }()

	upgradeGetUpgradeState = func() service.UpgradeState {
		return service.UpgradeState{
			Phase:    service.UpgradePhaseIdle,
			Progress: 0,
			Message:  "",
		}
	}

	req := newRequest(t, http.MethodGet, "/api/upgrade/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStatus, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "", resp["phase"])
	assert.Equal(t, float64(0), resp["progress"])
}

func TestServeUpgradeStatus_Downloading(t *testing.T) {
	defer func() { upgradeGetUpgradeState = service.GetUpgradeState }()

	upgradeGetUpgradeState = func() service.UpgradeState {
		return service.UpgradeState{
			Phase:      service.UpgradePhaseDownloading,
			CurrentVer: "1.0.0",
			LatestVer:  "1.1.0",
			Progress:   50,
			Message:    "Downloading...",
		}
	}

	req := newRequest(t, http.MethodGet, "/api/upgrade/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStatus, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "downloading", resp["phase"])
	assert.Equal(t, "1.0.0", resp["current_version"])
	assert.Equal(t, "1.1.0", resp["latest_version"])
	assert.Equal(t, float64(50), resp["progress"])
	assert.Equal(t, "Downloading...", resp["message"])
}

func TestServeUpgradeStatus_Failed(t *testing.T) {
	defer func() { upgradeGetUpgradeState = service.GetUpgradeState }()

	upgradeGetUpgradeState = func() service.UpgradeState {
		return service.UpgradeState{
			Phase: service.UpgradePhaseFailed,
			Error: "Download failed: connection timeout",
		}
	}

	req := newRequest(t, http.MethodGet, "/api/upgrade/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStatus, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "failed", resp["phase"])
	assert.Equal(t, "Download failed: connection timeout", resp["error"])
}

func TestServeUpgradeStatus_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodPost, "/api/upgrade/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStatus, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
