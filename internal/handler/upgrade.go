package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"clawbench/internal/service"
	"clawbench/internal/version"
)

// upgradeStatusStarted is the value of the "status" key in the upgrade start response.
const upgradeStatusStarted = "started"

// Package-level function variables for testability.
var (
	upgradeCheckForUpgradeInfo = service.CheckForUpgradeInfo
	upgradeIsInProgress        = service.IsUpgradeInProgress
	upgradePerformUpgrade      = service.PerformUpgrade
	upgradeGetUpgradeState     = service.GetUpgradeState
	upgradeCompareVersions     = version.CompareVersions
	upgradeIsDevBuild          = version.IsDevBuild
	upgradeCheckInstallDirWrit = service.CheckInstallDirWritable
	upgradeIsDocker            = service.IsDocker
)

// ServeUpgradeCheck handles GET /api/upgrade/check
// Returns current version, latest version, and whether an upgrade is available.
func ServeUpgradeCheck(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	info, err := upgradeCheckForUpgradeInfo()
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	currentVer, latestVer := info.CurrentVersion, info.LatestVersion

	hasUpgrade := upgradeCompareVersions(currentVer, latestVer) < 0 || upgradeIsDevBuild(currentVer)

	// Report install-directory writability so the UI can warn before the user
	// starts an upgrade that cannot succeed.
	//
	// An empty install_dir means the executable path could not be resolved, so
	// the probe is inconclusive rather than a permission verdict — reporting
	// "not writable" there would show a misleading warning with no directory.
	// Leave install_writable true (no warning); the upgrade itself will surface
	// the real error if it is attempted.
	installDir, installErr := upgradeCheckInstallDirWrit()
	installWritable := installErr == nil || installDir == ""

	writeJSON(w, http.StatusOK, map[string]any{
		"current_version":  currentVer,
		"latest_version":   latestVer,
		"has_upgrade":      hasUpgrade,
		"install_writable": installWritable,
		"install_dir":      installDir,
		// is_docker tells the UI to show an advisory (non-blocking) hint
		// recommending an image-based upgrade. Self-replace still works in a
		// container, but a later rebuild from the unchanged image reverts it.
		"is_docker": upgradeIsDocker(),
		// verification_warning is the human-readable text shown to the user when
		// this release cannot be fully verified. Display only.
		"verification_warning": info.VerificationWarning,
		// verification_issues is the stable identity of the same problems, as
		// sorted issue codes. The client echoes this back on start; the service
		// compares codes rather than the message, which varies with registry
		// routing and error detail.
		"verification_issues": info.VerificationIssues,
	})
}

// ServeUpgradeStart handles POST /api/upgrade/start
// Initiates the upgrade process. Returns error if already in progress.
// Note: version verification is done inside PerformUpgrade(), so we don't
// re-query the registry here (avoids TOCTOU race and redundant latency).
//
// The body carries verification_issues: the issue fingerprint the client
// displayed and got consent for, or omitted/empty when the client saw none. The
// service compares it against what the registry reports and refuses a mismatch,
// so an unverified install cannot happen without a decision that matches the
// metadata.
func ServeUpgradeStart(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if upgradeIsInProgress() {
		writeLocalizedErrorf(w, r, http.StatusConflict, "UpgradeInProgress")
		return
	}

	// The body is optional: callers that saw no warning send none. An empty
	// body is therefore valid, not a malformed request.
	var req struct {
		VerificationIssues string `json:"verification_issues"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
	}

	upgradePerformUpgrade(req.VerificationIssues)

	writeJSON(w, http.StatusOK, map[string]any{
		jsonKeyStatus: upgradeStatusStarted,
	})
}

// ServeUpgradeStatus handles GET /api/upgrade/status
// Returns the current upgrade state.
func ServeUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	state := upgradeGetUpgradeState()
	writeJSON(w, http.StatusOK, state)
}
