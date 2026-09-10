package handler

import (
	"net/http"

	"clawbench/internal/service"
	"clawbench/internal/version"
)

// upgradeStatusStarted is the value of the "status" key in the upgrade start response.
const upgradeStatusStarted = "started"

// Package-level function variables for testability.
var (
	upgradeCheckForUpgrade     = service.CheckForUpgrade
	upgradeIsInProgress        = service.IsUpgradeInProgress
	upgradePerformUpgrade      = service.PerformUpgrade
	upgradeGetUpgradeState     = service.GetUpgradeState
	upgradeCompareVersions     = version.CompareVersions
	upgradeIsDevBuild          = version.IsDevBuild
	upgradeCheckInstallDirWrit = service.CheckInstallDirWritable
)

// ServeUpgradeCheck handles GET /api/upgrade/check
// Returns current version, latest version, and whether an upgrade is available.
func ServeUpgradeCheck(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	currentVer, latestVer, err := upgradeCheckForUpgrade()
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	hasUpgrade := upgradeCompareVersions(currentVer, latestVer) < 0 || upgradeIsDevBuild(currentVer)

	// Report install-directory writability so the UI can warn before the user
	// starts an upgrade that cannot succeed. A failed probe (e.g. os.Executable
	// error) is treated as not writable — the upgrade would fail anyway.
	installDir, installErr := upgradeCheckInstallDirWrit()
	installWritable := installErr == nil

	writeJSON(w, http.StatusOK, map[string]any{
		"current_version":  currentVer,
		"latest_version":   latestVer,
		"has_upgrade":      hasUpgrade,
		"install_writable": installWritable,
		"install_dir":      installDir,
	})
}

// ServeUpgradeStart handles POST /api/upgrade/start
// Initiates the upgrade process. Returns error if already in progress.
// Note: version verification is done inside PerformUpgrade(), so we don't
// re-query the registry here (avoids TOCTOU race and redundant latency).
func ServeUpgradeStart(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if upgradeIsInProgress() {
		writeLocalizedErrorf(w, r, http.StatusConflict, "UpgradeInProgress")
		return
	}

	upgradePerformUpgrade()

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
