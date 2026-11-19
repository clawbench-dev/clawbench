package handler

import (
	"encoding/json"
	"net/http"
	"os"

	"clawbench/internal/version"
)

// ServeHealth returns a JSON object identifying this server as a ClawBench instance.
// This endpoint does NOT require authentication — it is used by the Android app
// to verify that a URL points to a real ClawBench server before loading the WebView.
func ServeHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{
		App:      "clawbench",
		Version:  version.Get(),
		Protocol: version.ClientProtocol,
		PID:      os.Getpid(),
	})
}

type healthResponse struct {
	App     string `json:"app"`
	Version string `json:"version"`
	// Protocol is the client/server compatibility number. The Android gate
	// compares it with the value baked into the APK instead of comparing
	// version strings, so a release that did not change the contract can keep
	// shipping the previous APK.
	Protocol int `json:"protocol"`
	// PID identifies the process that answered. The desktop shell starts its
	// own server and probes this endpoint to wait for it; without this field
	// an older instance still listening on the same port answers first and
	// looks like "our server is ready", so the window is pointed at a server
	// that is about to exit (its page can then never reconnect). Additive:
	// clients that do not know it ignore it, and an older server omits it.
	PID int `json:"pid"`
}
