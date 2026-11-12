package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
		upgradeCheckForUpgradeInfo = service.CheckForUpgradeInfo
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
		upgradeCheckInstallDirWrit = service.CheckInstallDirWritable
		upgradeIsDocker = service.IsDocker
	}()

	upgradeCheckInstallDirWrit = func() (string, error) { return "/usr/local/bin", nil }
	upgradeIsDocker = func() bool { return false }

	upgradeCheckForUpgradeInfo = func() (*service.UpgradeInfo, error) {
		return &service.UpgradeInfo{CurrentVersion: "1.0.0", LatestVersion: "1.1.0"}, nil
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
	assert.Equal(t, true, resp["install_writable"])
	assert.Equal(t, "/usr/local/bin", resp["install_dir"])
	assert.Equal(t, false, resp["is_docker"])
	assert.Equal(t, "", resp["verification_warning"], "a verified release must carry no warning")
	assert.Equal(t, "", resp["verification_issues"], "a verified release must carry no fingerprint")
}

// TestServeUpgradeCheck_VerificationWarning guards that a downgraded signature
// check is reported to the UI. The user must learn before starting that the
// download will only be integrity-checked.
func TestServeUpgradeCheck_VerificationWarning(t *testing.T) {
	defer func() {
		upgradeCheckForUpgradeInfo = service.CheckForUpgradeInfo
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
		upgradeCheckInstallDirWrit = service.CheckInstallDirWritable
		upgradeIsDocker = service.IsDocker
	}()

	upgradeCheckInstallDirWrit = func() (string, error) { return "/usr/local/bin", nil }
	upgradeIsDocker = func() bool { return false }
	upgradeCompareVersions = func(a, b string) int { return -1 }
	upgradeIsDevBuild = func(v string) bool { return false }

	const warning = "The release signature could not be verified because npm's signing keys were unreachable."
	const issues = "signature_keys_unreachable"
	upgradeCheckForUpgradeInfo = func() (*service.UpgradeInfo, error) {
		return &service.UpgradeInfo{
			CurrentVersion:      "1.0.0",
			LatestVersion:       "1.1.0",
			VerificationWarning: warning,
			VerificationIssues:  issues,
		}, nil
	}

	req := newRequest(t, http.MethodGet, "/api/upgrade/check", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeCheck, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, warning, resp["verification_warning"])
	// The fingerprint is the value the client echoes back, so it must be exposed
	// alongside the message the user reads.
	assert.Equal(t, issues, resp["verification_issues"])
	assert.Equal(t, true, resp["has_upgrade"], "a signature warning must not block the upgrade")
}

// TestServeUpgradeCheck_DockerAdvisory guards that a container deployment is
// reported to the UI (so it can show a non-blocking "pull the image" hint)
// without affecting has_upgrade — the upgrade must remain available.
func TestServeUpgradeCheck_DockerAdvisory(t *testing.T) {
	defer func() {
		upgradeCheckForUpgradeInfo = service.CheckForUpgradeInfo
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
		upgradeCheckInstallDirWrit = service.CheckInstallDirWritable
		upgradeIsDocker = service.IsDocker
	}()

	upgradeCheckForUpgradeInfo = func() (*service.UpgradeInfo, error) {
		return &service.UpgradeInfo{CurrentVersion: "1.0.0", LatestVersion: "1.1.0"}, nil
	}
	upgradeCompareVersions = func(a, b string) int { return -1 }
	upgradeIsDevBuild = func(v string) bool { return false }
	upgradeCheckInstallDirWrit = func() (string, error) { return "/app", nil }
	upgradeIsDocker = func() bool { return true }

	req := newRequest(t, http.MethodGet, "/api/upgrade/check", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeCheck, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["is_docker"])
	assert.Equal(t, true, resp["has_upgrade"], "Docker must not block the upgrade")
}

func TestServeUpgradeCheck_InstallDirNotWritable(t *testing.T) {
	defer func() {
		upgradeCheckForUpgradeInfo = service.CheckForUpgradeInfo
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
		upgradeCheckInstallDirWrit = service.CheckInstallDirWritable
	}()

	upgradeCheckForUpgradeInfo = func() (*service.UpgradeInfo, error) {
		return &service.UpgradeInfo{CurrentVersion: "1.0.0", LatestVersion: "1.1.0"}, nil
	}
	upgradeCompareVersions = func(a, b string) int { return -1 }
	upgradeIsDevBuild = func(v string) bool { return false }
	upgradeCheckInstallDirWrit = func() (string, error) {
		return "/usr/local/bin", errors.New("permission denied")
	}

	req := newRequest(t, http.MethodGet, "/api/upgrade/check", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeCheck, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["install_writable"])
	assert.Equal(t, "/usr/local/bin", resp["install_dir"])
}

// TestServeUpgradeCheck_ProbeInconclusive covers an unresolved executable path:
// with no directory to report, the probe must not assert "not writable" (that
// would show a misleading warning with an empty dir).
func TestServeUpgradeCheck_ProbeInconclusive(t *testing.T) {
	defer func() {
		upgradeCheckForUpgradeInfo = service.CheckForUpgradeInfo
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
		upgradeCheckInstallDirWrit = service.CheckInstallDirWritable
	}()

	upgradeCheckForUpgradeInfo = func() (*service.UpgradeInfo, error) {
		return &service.UpgradeInfo{CurrentVersion: "1.0.0", LatestVersion: "1.1.0"}, nil
	}
	upgradeCompareVersions = func(a, b string) int { return -1 }
	upgradeIsDevBuild = func(v string) bool { return false }
	upgradeCheckInstallDirWrit = func() (string, error) {
		return "", errors.New("failed to resolve executable path")
	}

	req := newRequest(t, http.MethodGet, "/api/upgrade/check", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeCheck, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["install_writable"], "inconclusive probe must not warn")
	assert.Equal(t, "", resp["install_dir"])
}

func TestServeUpgradeCheck_NoUpgrade(t *testing.T) {
	defer func() {
		upgradeCheckForUpgradeInfo = service.CheckForUpgradeInfo
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
	}()

	upgradeCheckForUpgradeInfo = func() (*service.UpgradeInfo, error) {
		return &service.UpgradeInfo{CurrentVersion: "1.1.0", LatestVersion: "1.1.0"}, nil
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
		upgradeCheckForUpgradeInfo = service.CheckForUpgradeInfo
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
	}()

	upgradeCheckForUpgradeInfo = func() (*service.UpgradeInfo, error) {
		return &service.UpgradeInfo{CurrentVersion: "dev", LatestVersion: "1.0.0"}, nil
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
	defer func() { upgradeCheckForUpgradeInfo = service.CheckForUpgradeInfo }()

	upgradeCheckForUpgradeInfo = func() (*service.UpgradeInfo, error) {
		return nil, errors.New("registry unreachable")
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
	upgradePerformUpgrade = func(string) { upgradeCalled = true }

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

// The client's acknowledgment must reach the service, since that is what the
// service compares against the registry to decide whether the install may
// proceed unverified.
func TestServeUpgradeStart_PassesAcknowledgmentThrough(t *testing.T) {
	defer func() { upgradePerformUpgrade = service.PerformUpgrade }()

	const issues = "signature_missing,no_integrity_hash"
	var got string
	upgradePerformUpgrade = func(ack string) { got = ack }

	req := newRequest(t, http.MethodPost, "/api/upgrade/start",
		map[string]string{"verification_issues": issues})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStart, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, issues, got)
}

// An absent body is the normal case for a fully verified release, not a
// malformed request.
func TestServeUpgradeStart_EmptyBodyIsAccepted(t *testing.T) {
	defer func() { upgradePerformUpgrade = service.PerformUpgrade }()

	called := false
	var got string
	upgradePerformUpgrade = func(ack string) { called = true; got = ack }

	req := newRequest(t, http.MethodPost, "/api/upgrade/start", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStart, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, called)
	assert.Equal(t, "", got)
}

func TestServeUpgradeStart_MalformedBodyRejected(t *testing.T) {
	defer func() { upgradePerformUpgrade = service.PerformUpgrade }()

	called := false
	upgradePerformUpgrade = func(string) { called = true }

	// Built by hand: newRequest marshals its argument, so it cannot produce
	// malformed JSON.
	req := httptest.NewRequest(http.MethodPost, "/api/upgrade/start", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStart, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, called, "a malformed body must not start an upgrade")
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

func TestServeUpgradeStatus_FailedWithErrorCode(t *testing.T) {
	defer func() { upgradeGetUpgradeState = service.GetUpgradeState }()

	upgradeGetUpgradeState = func() service.UpgradeState {
		return service.UpgradeState{
			Phase:     service.UpgradePhaseFailed,
			ErrorCode: service.UpgradeErrInstallDirNotWritable,
			Error:     "Install directory /usr/local/bin is not writable",
		}
	}

	req := newRequest(t, http.MethodGet, "/api/upgrade/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStatus, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "failed", resp["phase"])
	assert.Equal(t, "install_dir_not_writable", resp["error_code"])
}

// TestServeUpgradeStatus_ErrorCodeAlwaysPresent guards the wire contract that
// error_code is sent even when empty. The frontend merges updates with
// Object.assign (which cannot delete keys), so omitting the field on a generic
// failure would let a stale code from a previous attempt survive a retry.
func TestServeUpgradeStatus_ErrorCodeAlwaysPresent(t *testing.T) {
	defer func() { upgradeGetUpgradeState = service.GetUpgradeState }()

	upgradeGetUpgradeState = func() service.UpgradeState {
		return service.UpgradeState{
			Phase: service.UpgradePhaseFailed,
			Error: "generic failure",
			// ErrorCode intentionally empty.
		}
	}

	req := newRequest(t, http.MethodGet, "/api/upgrade/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStatus, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	_, present := resp["error_code"]
	assert.True(t, present, "error_code must be present even when empty")
	assert.Equal(t, "", resp["error_code"])
}

func TestServeUpgradeStatus_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodPost, "/api/upgrade/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeStatus, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ── ServeUpgradeSkip / skipped_version ──

// stubUpgradeSkip replaces the two skip-record functions for the duration of a
// test and returns a pointer to the value the write recorded.
func stubUpgradeSkip(t *testing.T, initial string) *string {
	t.Helper()
	origRead := upgradeReadSkippedVersion
	origWrite := upgradeWriteSkippedVersion
	t.Cleanup(func() {
		upgradeReadSkippedVersion = origRead
		upgradeWriteSkippedVersion = origWrite
	})

	stored := initial
	upgradeReadSkippedVersion = func() string { return stored }
	upgradeWriteSkippedVersion = func(version string) (string, error) {
		stored = strings.TrimPrefix(strings.TrimSpace(version), "v")
		return stored, nil
	}
	return &stored
}

func TestServeUpgradeCheck_ReportsSkippedVersion(t *testing.T) {
	defer func() {
		upgradeCheckForUpgradeInfo = service.CheckForUpgradeInfo
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
		upgradeCheckInstallDirWrit = service.CheckInstallDirWritable
		upgradeIsDocker = service.IsDocker
	}()
	upgradeCheckForUpgradeInfo = func() (*service.UpgradeInfo, error) {
		return &service.UpgradeInfo{CurrentVersion: "1.0.0", LatestVersion: "1.1.0"}, nil
	}
	upgradeCompareVersions = func(a, b string) int { return -1 }
	upgradeIsDevBuild = func(v string) bool { return false }
	upgradeCheckInstallDirWrit = func() (string, error) { return "/usr/local/bin", nil }
	upgradeIsDocker = func() bool { return false }
	stubUpgradeSkip(t, "1.1.0")

	req := newRequest(t, http.MethodGet, "/api/upgrade/check", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeCheck, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "1.1.0", resp["skipped_version"])
	// Skipping suppresses the startup prompt, not the availability: the
	// settings dialog must still be able to install this version.
	assert.Equal(t, true, resp["has_upgrade"])
}

func TestServeUpgradeSkip_StoresVersion(t *testing.T) {
	stored := stubUpgradeSkip(t, "")

	req := newRequest(t, http.MethodPost, "/api/upgrade/skip", map[string]any{"version": "1.2.3"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeSkip, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "1.2.3", *stored)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "1.2.3", resp["skipped_version"])
}

// An empty version clears the record, so a future "unskip" needs no separate
// endpoint.
func TestServeUpgradeSkip_EmptyClears(t *testing.T) {
	stored := stubUpgradeSkip(t, "1.2.3")

	req := newRequest(t, http.MethodPost, "/api/upgrade/skip", map[string]any{"version": ""})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeSkip, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", *stored)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "", resp["skipped_version"])
}

// A body-less request is valid and clears the record (same as an empty
// version), matching how /api/upgrade/start treats an absent body.
func TestServeUpgradeSkip_MissingBodyClears(t *testing.T) {
	stored := stubUpgradeSkip(t, "1.2.3")

	req := newRequest(t, http.MethodPost, "/api/upgrade/skip", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeSkip, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", *stored)
}

func TestServeUpgradeSkip_InvalidBody(t *testing.T) {
	stubUpgradeSkip(t, "")

	req := httptest.NewRequest(http.MethodPost, "/api/upgrade/skip", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeSkip, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// The client must be able to tell "not remembered" from "remembered", so a
// failed write is an error rather than a 200 with an empty value.
func TestServeUpgradeSkip_WriteFailure(t *testing.T) {
	origRead := upgradeReadSkippedVersion
	origWrite := upgradeWriteSkippedVersion
	defer func() {
		upgradeReadSkippedVersion = origRead
		upgradeWriteSkippedVersion = origWrite
	}()
	upgradeWriteSkippedVersion = func(string) (string, error) {
		return "", errors.New("data directory is not configured")
	}

	req := newRequest(t, http.MethodPost, "/api/upgrade/skip", map[string]any{"version": "1.2.3"})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeSkip, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// A version the service refuses to store is the caller's mistake, not a server
// fault — 400 so the client does not retry a request that cannot succeed.
func TestServeUpgradeSkip_InvalidVersion(t *testing.T) {
	origWrite := upgradeWriteSkippedVersion
	defer func() { upgradeWriteSkippedVersion = origWrite }()
	upgradeWriteSkippedVersion = func(string) (string, error) {
		return "", service.ErrSkippedVersionInvalid
	}

	req := newRequest(t, http.MethodPost, "/api/upgrade/skip", map[string]any{"version": strings.Repeat("9", 200)})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeSkip, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeUpgradeSkip_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/upgrade/skip", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeSkip, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// A commit-describe build ahead of the registry must NOT be told an "upgrade"
// exists: the only thing on offer is an older release, and offering it both
// invites a downgrade and nags on every launch.
func TestServeUpgradeCheck_DevBuildAheadOfRegistry(t *testing.T) {
	defer func() {
		upgradeCheckForUpgradeInfo = service.CheckForUpgradeInfo
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
		upgradeIsVersionedBuild = version.IsVersionedBuild
		upgradeCheckInstallDirWrit = service.CheckInstallDirWritable
		upgradeIsDocker = service.IsDocker
	}()
	upgradeCheckForUpgradeInfo = func() (*service.UpgradeInfo, error) {
		return &service.UpgradeInfo{
			CurrentVersion: "v0.139.0-20-g9037e1d5a-11121522",
			LatestVersion:  "0.139.0",
		}, nil
	}
	upgradeCompareVersions = version.CompareVersions
	upgradeIsDevBuild = version.IsDevBuild
	upgradeIsVersionedBuild = version.IsVersionedBuild
	upgradeCheckInstallDirWrit = func() (string, error) { return "/usr/local/bin", nil }
	upgradeIsDocker = func() bool { return false }
	stubUpgradeSkip(t, "")

	req := newRequest(t, http.MethodGet, "/api/upgrade/check", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeCheck, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["has_upgrade"], "a build ahead of the registry has nothing to upgrade to")
}

// The same dev build, once the registry moves past it, is upgradeable again.
func TestServeUpgradeCheck_DevBuildBehindRegistry(t *testing.T) {
	defer func() {
		upgradeCheckForUpgradeInfo = service.CheckForUpgradeInfo
		upgradeCompareVersions = version.CompareVersions
		upgradeIsDevBuild = version.IsDevBuild
		upgradeIsVersionedBuild = version.IsVersionedBuild
		upgradeCheckInstallDirWrit = service.CheckInstallDirWritable
		upgradeIsDocker = service.IsDocker
	}()
	upgradeCheckForUpgradeInfo = func() (*service.UpgradeInfo, error) {
		return &service.UpgradeInfo{
			CurrentVersion: "v0.139.0-20-g9037e1d5a-11121522",
			LatestVersion:  "0.140.0",
		}, nil
	}
	upgradeCompareVersions = version.CompareVersions
	upgradeIsDevBuild = version.IsDevBuild
	upgradeIsVersionedBuild = version.IsVersionedBuild
	upgradeCheckInstallDirWrit = func() (string, error) { return "/usr/local/bin", nil }
	upgradeIsDocker = func() bool { return false }
	stubUpgradeSkip(t, "")

	req := newRequest(t, http.MethodGet, "/api/upgrade/check", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeUpgradeCheck, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["has_upgrade"])
}
