package handler

import (
	"net/http"

	"clawbench/internal/frp"
)

// FRP info JSON response keys
const (
	frpKeyEnabled   = "enabled"
	frpKeyRunning   = "running"
	frpKeyState     = "state"
	frpKeyMessage   = "message"
	frpKeyAvailable = "available"
)

// frpManagerRef holds a reference to the FRP manager, set from main.go.
var frpManagerRef *frp.Manager

// frpEnabled tracks whether FRP is enabled in config (even if manager is nil).
var frpEnabled bool

// SetFRPManager stores a reference to the FRP manager for handler access.
func SetFRPManager(m *frp.Manager, enabled bool) {
	frpManagerRef = m
	frpEnabled = enabled
}

// GetFRPManager returns the current FRP manager reference (for hot-reload).
func GetFRPManager() *frp.Manager {
	return frpManagerRef
}

// ServeFRPInfo returns full FRP tunnel status. Requires authentication.
// GET /api/frp/info
func ServeFRPInfo(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// `available` is a property of the build, not of the current config: a
	// binary compiled with the `nofrp` tag has no FRP client at all, so the
	// client hides the settings panel rather than offering a switch that can
	// never take effect.
	available := frp.Available()

	if frpManagerRef == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			frpKeyEnabled:   frpEnabled,
			frpKeyRunning:   false,
			frpKeyState:     "disabled",
			frpKeyMessage:   "FRP is not enabled",
			frpKeyAvailable: available,
		})
		return
	}

	status := frpManagerRef.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		frpKeyEnabled:     status.Enabled,
		frpKeyRunning:     status.Running,
		frpKeyState:       status.State,
		frpKeyAvailable:   available,
		"server_addr":     status.ServerAddr,
		"remote_port":     status.RemotePort,
		"ssh_remote_port": status.SSHRemotePort,
		"remote_url":      status.RemoteURL,
	})
}

// ServeFRPStatus returns minimal FRP status without authentication.
// Only exposes enabled + running state; no addresses or ports.
// GET /api/frp/status
func ServeFRPStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	available := frp.Available()

	if frpManagerRef == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			frpKeyEnabled:   frpEnabled,
			frpKeyRunning:   false,
			frpKeyState:     "disabled",
			frpKeyAvailable: available,
		})
		return
	}

	status := frpManagerRef.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		frpKeyEnabled:   status.Enabled,
		frpKeyRunning:   status.Running,
		frpKeyState:     status.State,
		frpKeyAvailable: available,
	})
}
