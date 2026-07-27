package handler

import (
	"net/http"

	"clawbench/internal/system"
)

// ServeSystemResources returns current system resource metrics.
// Requires authentication (applied via middleware.Auth in route registration).
// Note: exposes system-level metrics (CPU, memory, disk, network) to all
// authenticated users. Acceptable for single-user ClawBench; if multi-tenancy
// is added, this should be admin-only.
func ServeSystemResources(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	res, err := system.GetResources()
	if err != nil {
		http.Error(w, "failed to collect system resources", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}
