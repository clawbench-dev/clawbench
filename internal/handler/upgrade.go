package handler

import (
	"encoding/json"
	"errors"
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
	upgradeIsVersionedBuild    = version.IsVersionedBuild
	upgradeCheckInstallDirWrit = service.CheckInstallDirWritable
	upgradeIsDocker            = service.IsDocker
	upgradeReadSkippedVersion  = service.ReadSkippedUpgradeVersion
	upgradeWriteSkippedVersion = service.WriteSkippedUpgradeVersion
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

	// A dev build only counts as "upgradeable regardless" when its version
	// cannot be compared at all ("dev", a bare VCS hash). A commit-describe
	// build ("v0.139.0-20-g9037e1d5a") IS comparable, and when it sits ahead of
	// the registry the "new version" on offer is in fact an older release —
	// offering it invites a downgrade and nags on every launch.
	hasUpgrade := upgradeCompareVersions(currentVer, latestVer) < 0 ||
		(upgradeIsDevBuild(currentVer) && !upgradeIsVersionedBuild(currentVer))

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
		// skipped_version is the version the user asked not to be prompted
		// about again, or "" when nothing is recorded. It is server-side so the
		// choice survives a wiped WebView storage and applies to every device
		// and origin this server is reached from. has_upgrade is deliberately
		// left untouched: a skipped version can still be installed from the
		// settings dialog, so only the startup prompt consults this field.
		"skipped_version": upgradeReadSkippedVersion(),
	})
}

// ServeUpgradeSkip handles POST /api/upgrade/skip
//
// Body: {"version": "1.2.3"} records that version as skipped; an empty or
// missing version clears the record. Returns the stored value, so the client
// can tell a successful write from a silently ignored one.
func ServeUpgradeSkip(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Version string `json:"version"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
	}

	stored, err := upgradeWriteSkippedVersion(req.Version)
	if err != nil {
		// An unusable version is the caller's mistake; anything else means the
		// record could not be written (missing/unwritable data directory), which
		// the client treats as "not remembered" and keeps its local copy.
		if errors.Is(err, service.ErrSkippedVersionInvalid) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"skipped_version": stored,
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
