package handler

import (
	"net/http"

	"clawbench/internal/frp"
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

// ServeFRPInfo returns full FRP tunnel status. Requires authentication.
// GET /api/frp/info
func ServeFRPInfo(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if frpManagerRef == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": frpEnabled,
			"running": false,
			"state":   "disabled",
			"message": "FRP is not enabled",
		})
		return
	}

	status := frpManagerRef.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":         status.Enabled,
		"running":         status.Running,
		"state":           status.State,
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

	if frpManagerRef == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": frpEnabled,
			"running": false,
			"state":   "disabled",
		})
		return
	}

	status := frpManagerRef.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": status.Enabled,
		"running": status.Running,
		"state":   status.State,
	})
}
