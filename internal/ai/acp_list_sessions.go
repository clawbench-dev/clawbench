package ai

import (
	"errors"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// On-disk ACP session listing — fallback for agents that don't implement the
// session/list RPC (e.g. CodeBuddy). The backends package registers a scanner
// per backend during init.
// ---------------------------------------------------------------------------

// ListSessionsFromDiskFn scans a specific backend's on-disk session storage to
// enumerate ACP sessions as a fallback when session/list RPC is unavailable.
// The cwd is the current project root used to scope the scan to the project's
// own session directory (avoiding a full-tree walk of all projects).
type ListSessionsFromDiskFn func(agent *model.Agent, cwd string) ([]acp.SessionInfo, error)

// listSessionsFromDiskFns maps backend IDs to their on-disk session scanners.
// Only backends that register a scanner (via ListSessionsFromDiskRegister)
// get the on-disk fallback; others rely on the ACP session/list RPC.
var listSessionsFromDiskFns = map[string]ListSessionsFromDiskFn{}

// ListSessionsFromDiskRegister registers an on-disk session scanner for the
// given backend. Called by the backends package during init. Passing nil
// deregisters (used by tests to clean up).
func ListSessionsFromDiskRegister(backend string, fn ListSessionsFromDiskFn) {
	if fn == nil {
		delete(listSessionsFromDiskFns, backend)
		return
	}
	listSessionsFromDiskFns[backend] = fn
}

// HasListSessionsFromDisk reports whether the given backend has an on-disk
// session scanner registered (i.e. it supports ListSessions without the
// session/list RPC).
func HasListSessionsFromDisk(backend string) bool {
	_, ok := listSessionsFromDiskFns[backend]
	return ok
}

// ListSessionsFromDisk returns the agent's sessions via its backend's
// registered on-disk scanner. Returns an error if the backend has no scanner.
// cwd scopes the scan to a single project directory when non-empty.
func ListSessionsFromDisk(agent *model.Agent, cwd string) ([]acp.SessionInfo, error) {
	fn, ok := listSessionsFromDiskFns[agent.Backend]
	if !ok {
		return nil, &errNoOnDiskListSessions{backend: agent.Backend}
	}
	return fn(agent, cwd)
}

// errNoOnDiskListSessions is returned when a backend has no registered on-disk
// session scanner.
type errNoOnDiskListSessions struct {
	backend string
}

func (e *errNoOnDiskListSessions) Error() string {
	return errors.New("acp: no on-disk ListSessions implementation registered for backend " + e.backend).Error()
}
