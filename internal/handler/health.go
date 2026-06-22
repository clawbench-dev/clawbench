package handler

import (
	"encoding/json"
	"net/http"

	"clawbench/internal/version"
)

// ServeHealth returns a JSON object identifying this server as a ClawBench instance.
// This endpoint does NOT require authentication — it is used by the Android app
// to verify that a URL points to a real ClawBench server before loading the WebView.
func ServeHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{
		App:     "clawbench",
		Version: version.Get(),
	})
}

type healthResponse struct {
	App     string `json:"app"`
	Version string `json:"version"`
}
