package handler

import (
	"net/http"

	"clawbench/internal/service"
	"clawbench/internal/version"
)

// ServeUpgradeCheck handles GET /api/upgrade/check
// Returns current version, latest version, and whether an upgrade is available.
func ServeUpgradeCheck(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	currentVer, latestVer, err := service.CheckForUpgrade()
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	hasUpgrade := version.CompareVersions(currentVer, latestVer) < 0 || version.IsDevBuild(currentVer)

	writeJSON(w, http.StatusOK, map[string]any{
		"current_version": currentVer,
		"latest_version":  latestVer,
		"has_upgrade":     hasUpgrade,
	})
}

// ServeUpgradeStart handles POST /api/upgrade/start
// Initiates the upgrade process. Returns error if already in progress.
func ServeUpgradeStart(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if service.IsUpgradeInProgress() {
		writeLocalizedErrorf(w, r, http.StatusConflict, "UpgradeInProgress")
		return
	}

	// Verify upgrade is available
	currentVer, latestVer, err := service.CheckForUpgrade()
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	if version.CompareVersions(currentVer, latestVer) >= 0 && !version.IsDevBuild(currentVer) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "AlreadyLatestVersion")
		return
	}

	service.PerformUpgrade()

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "started",
	})
}

// ServeUpgradeStatus handles GET /api/upgrade/status
// Returns the current upgrade state.
func ServeUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	state := service.GetUpgradeState()
	writeJSON(w, http.StatusOK, state)
}
