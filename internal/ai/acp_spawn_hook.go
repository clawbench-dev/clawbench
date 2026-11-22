package ai

import "clawbench/internal/model"

// ACPSpawnContext is what a backend's spawn hooks may read. It is deliberately
// small: a hook exists to add argv/env for one backend, not to re-implement the
// spawn path.
type ACPSpawnContext struct {
	// Agent is the agent being spawned.
	Agent *model.Agent
	// SessionID is the ClawBench session this connection serves, for log
	// correlation. It is the same id the shared lifecycle logs as
	// "clawbench_sid".
	SessionID string
	// MCPServerCount is how many MCP servers the running configuration
	// declares. 0 means ClawBench injects nothing over the protocol.
	MCPServerCount int
}

// ACPSpawnHooks lets a backend customize its ACP child's argv and environment.
//
// This is the seam that keeps backend-specific spawn workarounds out of the
// shared lifecycle: reading a backend's own MCP config file, pointing it at an
// own-service endpoint, or setting a permission variable are all "this backend's
// business", and the shared code should not name them.
type ACPSpawnHooks struct {
	// Args appends arguments to the spawn command line.
	Args func(ACPSpawnContext) []string
	// Env appends "K=V" environment entries.
	Env func(ACPSpawnContext) []string
}

// LookupACPSpawnHooksFn resolves a backend's spawn hooks. Wired by
// internal/ai/backends to backends.LookupACPSpawnHooks to avoid an import
// cycle. Nil (or returning nil) means the backend needs no spawn customization.
var LookupACPSpawnHooksFn func(backendID string) *ACPSpawnHooks

// acpSpawnHooks returns the backend's hooks, or nil when it declares none.
func acpSpawnHooks(backendID string) *ACPSpawnHooks {
	if LookupACPSpawnHooksFn == nil || backendID == "" {
		return nil
	}
	return LookupACPSpawnHooksFn(backendID)
}
