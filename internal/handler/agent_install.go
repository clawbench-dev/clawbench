package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
	"clawbench/internal/ws"
)

// fieldCommand is the JSON key for the install command echoed back to the
// caller. Several install/terminal payloads use it, so it is named once.
const fieldCommand = "command"

// ServeBackendInstall handles POST /api/backends/{id}/install — runs a
// backend's built-in install command on this machine.
//
// Keyed by BACKEND (not agent id) because the whole point is to install
// something that may have no agent row yet: the welcome panel lists every
// registry backend, installed or not. A backend that IS already installed
// reaches the same endpoint through its agent's settings page (agent.backend),
// which makes this a "reinstall / upgrade" in that case.
//
// The command is looked up in the registry here and never accepted from the
// request body: a client-supplied command would turn an authenticated endpoint
// into arbitrary remote execution.
func ServeBackendInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	// Path: /api/backends/{id}/install
	path := strings.TrimPrefix(r.URL.Path, "/api/backends/")
	if !strings.HasSuffix(path, "/install") {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound")
		return
	}
	backendID := strings.TrimSuffix(path, "/install")

	spec := model.FindSpecByBackend(backendID)
	if spec == nil || spec.ID != backendID {
		// Match on spec.ID rather than Backend so the id in the URL is the same
		// one GET /api/backends reports. (They are equal for every built-in
		// backend today; the distinction only matters for a future one whose
		// two names diverge, where silently installing the wrong package would
		// be worse than a 404.)
		writeLocalizedErrorf(w, r, http.StatusNotFound, "BackendNotFound")
		return
	}
	if strings.TrimSpace(spec.InstallCmd) == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InstallCommandUnavailable")
		return
	}

	// Same China-mirror rewrite the welcome panel shows, so what the user
	// copied by hand and what this button runs are the same command.
	command := prepareInstallCmd(spec.InstallCmd)

	res, err := service.InstallAgent(spec.ID, command)
	if err != nil {
		writeAgentInstallError(w, r, spec.ID, err)
		return
	}

	// Auto-rescan: the user asked for one click to end with a usable agent, not
	// "installed, now press rescan". A failure here does not undo the install —
	// the binary is on disk either way — so it is reported as a flag rather than
	// an error.
	refreshed := rescanAfterInstall(spec.Backend)

	out := map[string]any{
		"ok":         true,
		"backend":    spec.ID,
		fieldCommand: res.Command,
		"output":     res.Output,
		"durationMs": res.DurationMs,
		"refreshed":  refreshed,
	}
	// Return the same list GET /api/agents reports (project-scoped) so the
	// caller can render the new agent immediately without a second request.
	projectPath := middleware.GetProjectFromCookie(r)
	agents := visibleAgentsInProject(model.GetAgentList(), projectPath)
	out["agents"] = agents
	out["defaultAgent"] = visibleDefaultAgent(model.GetDefaultAgentID(), agents, projectPath)

	writeJSON(w, http.StatusOK, out)
}

// rescanAfterInstall re-runs the discovery pipeline and tells every client the
// agent list changed. Reports whether the rescan succeeded.
//
// Only the installed backend is probed. A full refresh forks every backend's CLI
// (~18s serial, antigravity alone hangs out its timeout when logged out) to
// learn nothing — the other backends did not change — and it did so while
// holding configMutex, so every concurrent PATCH /api/config, agent edit and
// project switch queued behind an install. The refresh still has to run
// synchronously: it is what inserts the agent row for the backend that was just
// installed, and the response below returns that list.
func rescanAfterInstall(backend string) bool {
	configMutex.Lock()
	model.InvalidateDiscoveredModels(backend)
	_, err := model.RefreshAgents(store.WriteDB(), model.RefreshOptions{
		DiscoveryBackends: []string{backend},
	})
	configMutex.Unlock()
	if err != nil {
		slog.Warn("agent rescan after install failed", "backend", backend, "error", err)
		return false
	}
	if mgr := ws.GetManager(); mgr != nil {
		mgr.BroadcastEvent(ws.ServerMessage{
			Type:  ws.MessageTypeEvent,
			ID:    ws.GenerateEventID(),
			Event: "agents_updated",
		})
	}
	return true
}

// writeAgentInstallError maps the service's error kinds onto actionable
// responses. The distinction that matters to the user is "your machine is
// missing something" versus "the command itself failed", so those get different
// message keys rather than one generic "install failed".
func writeAgentInstallError(w http.ResponseWriter, r *http.Request, backendID string, err error) {
	switch {
	case errors.Is(err, service.ErrAgentInstallBusy):
		writeLocalizedErrorf(w, r, http.StatusConflict, "AgentInstallBusy")
		return
	case errors.Is(err, service.ErrAgentInstallEmptyCommand):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InstallCommandUnavailable")
		return
	}

	var nodeErr *service.AgentInstallNodeMissingError
	if errors.As(err, &nodeErr) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NodeRuntimeMissing",
			map[string]any{"Tool": nodeErr.Tool})
		return
	}

	var cmdErr *service.AgentInstallCommandError
	if errors.As(err, &cmdErr) {
		slog.Warn("agent install command failed", "backend", backendID, "error", cmdErr.Err)
		// A command that failed silently has no output to show. Saying
		// "install failed: " with nothing after the colon reads like a UI bug,
		// so that case gets its own sentence.
		if strings.TrimSpace(cmdErr.Output) == "" {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "AgentInstallFailedNoOutput")
			return
		}
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "AgentInstallFailed",
			map[string]any{"Output": cmdErr.Output})
		return
	}

	slog.Error("agent install failed", "backend", backendID, "error", err)
	writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
}
