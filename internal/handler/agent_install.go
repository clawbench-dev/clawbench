package handler

import (
	"context"
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

// probeNpmPackage is a variable so tests can drive source selection without a
// real npm.
var probeNpmPackage = service.ProbeNpmPackage

// resolveInstallCmd picks the npm source for one install and returns the
// command to run. It is the only place that probes a registry; the commands
// reported by GET /api/agents and GET /api/backends are the bare registry
// command and never touch the network.
//
// The candidate order is source.Npm(): user-configured first, then the region
// mirror, then the other public registry. A source that answers wins. A 401/403
// stops the walk and uses that source — the user configured it, and silently
// bypassing a private registry would be wrong. Only 404/network failures move
// on.
func resolveInstallCmd(ctx context.Context, installCmd string) string {
	if !isNpmInstallCommand(installCmd) {
		return installCmd
	}
	candidates := installSourceCandidates()
	if len(candidates) == 0 {
		return installCmd
	}
	pkg := npmInstallPackage(installCmd)
	if pkg == "" {
		// A local path, tarball, git URL or range spec: no metadata probe, but
		// still point npm at the first source so a public package in a range
		// spec uses the same mirror as everything else.
		return installCmdWithSource(installCmd, candidates[0].Base)
	}
	for _, candidate := range candidates {
		err := probeNpmPackage(ctx, pkg, candidate.Base)
		if err == nil {
			return installCmdWithSource(installCmd, candidate.Base)
		}
		if errors.Is(err, service.ErrNpmAuthRequired) {
			slog.Warn("agent install: source requires authentication; using it",
				"source", candidate.Name, "base", candidate.Base)
			return installCmdWithSource(installCmd, candidate.Base)
		}
		slog.Warn("agent install: source probe failed, trying next",
			"source", candidate.Name, "base", candidate.Base, "error", err)
	}
	// Nothing answered. Use the first candidate so npm reports the real error
	// instead of the probe masking it.
	return installCmdWithSource(installCmd, candidates[0].Base)
}

// isNpmInstallCommand reports whether this is an `npm install` command with no
// explicit registry. Only that shape is rewritten.
func isNpmInstallCommand(installCmd string) bool {
	return strings.HasPrefix(installCmd, "npm install") && !strings.Contains(installCmd, "--registry")
}

// npmInstallPackage extracts the single registry package spec from an
// `npm install` command, or "" when the spec is not probeable (local path,
// tarball, git URL, version range). The caller still installs it, just without
// a metadata probe.
func npmInstallPackage(installCmd string) string {
	fields := strings.Fields(installCmd)
	if len(fields) < 3 || fields[0] != "npm" || fields[1] != "install" {
		return ""
	}
	for _, field := range fields[2:] {
		if strings.HasPrefix(field, "-") {
			continue
		}
		if !service.IsSafeNpmProbeSpec(field) {
			return ""
		}
		return field
	}
	return ""
}

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

	// Probe the candidate sources once, then run the install with the selected
	// one. Listing the backend never probes.
	command := resolveInstallCmd(r.Context(), spec.InstallCmd)

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
		// The automatic Node.js install already ran and did not produce a
		// working runtime, so the reason it failed is the actionable part.
		reason := "the automatic install failed"
		if nodeErr.Cause != nil {
			reason = nodeErr.Cause.Error()
		}
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NodeRuntimeMissing",
			map[string]any{"Tool": nodeErr.Tool, errDetailKey: reason})
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
