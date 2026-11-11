package handler

import (
	"net/http"
	"strings"

	"clawbench/internal/service"
)

// Package-level function variables for testability (same pattern as upgrade.go).
var (
	drawioGetStatus    = service.GetDrawioStatus
	drawioStartInstall = service.StartDrawioInstall
	drawioIsReady      = service.DrawioReady
)

// ServeDrawioStatus handles GET /api/drawio/status.
//
// The client polls this while an install runs and once on mount to decide
// between "show the editor" and "offer to download it".
func ServeDrawioStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, drawioGetStatus())
}

// ServeDrawioInstall handles POST /api/drawio/install.
//
// Returns 202 as soon as the download is scheduled — the unpack takes a while
// and the client follows progress through the status endpoint.
func ServeDrawioInstall(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if drawioIsReady() {
		writeJSON(w, http.StatusOK, drawioGetStatus())
		return
	}
	if err := drawioStartInstall(); err != nil {
		// Only one install can run: a second request would unpack into the same
		// directory. Report it as a conflict with the live status attached.
		writeJSON(w, http.StatusConflict, drawioGetStatus())
		return
	}
	writeJSON(w, http.StatusAccepted, drawioGetStatus())
}

// ServeDrawioAsset serves the unpacked editor under /vendor/drawio/<version>/.
//
// Public like the other static assets (the app shell, /assets/): the files hold
// no user data, and the iframe loads them with the same-origin cookies anyway.
// Only the pinned version's directory is reachable, so a stale install of a
// previous version cannot be served by guessing a path.
func ServeDrawioAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	prefix := service.DrawioBasePath()
	if !strings.HasPrefix(r.URL.Path, prefix) || !drawioIsReady() {
		http.NotFound(w, r)
		return
	}
	// The version is part of the path, so these files never change in place —
	// they can be cached hard. drawio ships fixed filenames (app.min.js), so
	// without this a version bump would keep serving the old editor from cache.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.StripPrefix(prefix, http.FileServer(http.Dir(service.DrawioInstallDir()))).ServeHTTP(w, r)
}
