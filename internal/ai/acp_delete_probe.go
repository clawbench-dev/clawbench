package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ---------------------------------------------------------------------------
// session/delete probe — "does it work?" vs "does it advertise?" (issue #707)
// ---------------------------------------------------------------------------

// acpDeleteProbeTimeout bounds the probe. It is one request after Initialize on
// a process that just answered the handshake, so 3s is generous.
const acpDeleteProbeTimeout = 3 * time.Second

// probeDeleteSession asks the agent whether it can serve session/delete.
//
// The Initialize advertisement is not the answer: CodeBuddy implements the call
// and advertises no sessionCapabilities at all, so gating the permanent-delete
// path on the advertisement silently skipped it for every CodeBuddy session.
//
// The probe therefore exercises the METHOD, not a real session:
//
//   - a "session not found" style refusal means the agent HAS the method (it
//     looked the id up and rejected it) → confirmed
//   - method-not-found (-32601) means it does NOT → not confirmed
//   - anything else (our request never reached the agent: spawn failed,
//     timeout, transport error) answers nothing → answered=false, so the
//     registry keeps whatever it knew before
//
// A made-up session id is used so a live conversation can never be deleted by
// accident.
//
// Returns (confirmed, answered). Callers store the result with
// AgentCapabilityRegistry.RecordDeleteSessionProbe.
func probeDeleteSession(ctx context.Context, conn *ACPConn) (confirmed, answered bool) {
	pctx, cancel := context.WithTimeout(ctx, acpDeleteProbeTimeout)
	defer cancel()

	sid := fmt.Sprintf("__clawbench_capability_probe__:%d", time.Now().UnixNano())
	_, err := conn.CallRaw(pctx, "session/delete", map[string]any{"sessionId": sid})
	if err == nil {
		return true, true
	}

	var rpcErr *RawRPCError
	if errors.As(err, &rpcErr) {
		switch rpcErr.Code {
		case -32601: // method not found
			return false, true
		case -32602: // invalid params — the agent took the call and looked at it
			return true, true
		}
		// Any other JSON-RPC answer is the agent speaking about the METHOD
		// (unknown session, busy, ...), i.e. the method exists.
		return true, true
	}
	// No JSON-RPC answer at all: the agent never saw the request.
	slog.Debug("acp: session/delete probe got no answer", "error", err)
	return false, false
}

// refreshDeleteSessionCapability probes and records the result for the agent.
// Called once right after Initialize, on every spawn: the answer is cached per
// AGENT (the registry persists it), so later connections inherit it.
//
// It runs on a goroutine on purpose. The caller (spawnLocked) holds c.mu and
// runs on the session-creation path, so a slow agent would otherwise add its
// own timeout to every first turn; and a spawn that dies before the probe
// finishes simply yields answered=false, which records nothing.
func refreshDeleteSessionCapability(ctx context.Context, conn *ACPConn, agentID string) {
	if agentID == "" || conn == nil {
		return
	}
	go func() {
		confirmed, answered := probeDeleteSession(ctx, conn)
		if !answered {
			return
		}
		GetAgentCapabilityRegistry().RecordDeleteSessionProbe(agentID, confirmed, true)
		slog.Info("acp: session/delete probe", "agent_id", agentID, "confirmed", confirmed)
	}()
}
