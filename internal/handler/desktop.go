package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"clawbench/internal/service"
)

// fetchDesktopLatest is injectable for tests.
var fetchDesktopLatest = service.FetchDesktopLatest

// ServeDesktopLatest returns the latest desktop app version and per-platform
// download URLs. Public endpoint — no auth required.
func ServeDesktopLatest(w http.ResponseWriter, r *http.Request) {
	res, err := fetchDesktopLatest()
	if err != nil {
		slog.Error("desktop latest: fetch failed", "error", err)
		http.Error(w, "failed to fetch latest desktop version", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(res)
}
