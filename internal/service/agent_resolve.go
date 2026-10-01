package service

import "clawbench/internal/model"

// ResolveAgentID picks the agent ID for one turn, in priority order:
//
//  1. requested — the caller's explicit pick (request body / launch config);
//  2. the session's persisted agent_id — a queued or drained turn carries no
//     explicit pick, so it must inherit what the session was created with;
//  3. the configured default agent.
//
// Every entry point that can create a backend must resolve the agent the same
// way. The ACP/CLI decision lives inside ai.NewBackendForAgentWithTransport and
// is driven by the agent ID: an empty ID skips the ACP branch entirely, so a
// pure-ACP backend (no CLI factory) fails outright with "unsupported backend
// type". That is why this resolution is centralized here instead of being
// re-derived at each call site — the previous copies drifted, and a fix to one
// (the queue path) left the others still passing an empty ID.
func ResolveAgentID(sessionID, requested string) string {
	if requested != "" {
		return requested
	}
	if sessionID != "" {
		if id := GetSessionAgentID(sessionID); id != "" {
			return id
		}
	}
	return model.GetDefaultAgentID()
}
