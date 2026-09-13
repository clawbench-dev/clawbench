package ai

import (
	"context"
	"encoding/json"
	"log/slog"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// Mid-turn injection seam
// ---------------------------------------------------------------------------
//
// Normally a user message that arrives while a turn is running is queued and
// executed as its own turn afterwards. Some agents can do better: they expose a
// private method that injects the message into the CURRENT turn, so the model
// sees it before its next request (CodeBuddy's `session/steer`).
//
// This file defines the seam. It is deliberately split so that neither side
// knows more than it must:
//
//   - core flow (handler/service) asks "can this message go into the running
//     turn?" without knowing which backend, or how;
//   - a backend owns the policy — when injection is possible, how to phrase the
//     request, how to read the reply — in its own package;
//   - the transport capability it needs is a two-method interface, so policies
//     are unit-testable without a live agent.
//
// Core flow never names a backend; the policy is looked up by backend id through
// a function variable wired by internal/ai/backends (the same anti-import-cycle
// pattern as LookupACPRemapsFn).

// RawRPCTransport is the narrow transport capability a mid-turn policy needs:
// the ability to call a method the ACP SDK will not send, plus the agent-side
// session id. *ACPConn implements it.
type RawRPCTransport interface {
	CallRaw(ctx context.Context, method string, params any) (json.RawMessage, error)
	AcpSessionID() string
}

// MidTurnInjectRequest describes a user message that arrived mid-turn.
type MidTurnInjectRequest struct {
	SessionID           string // ClawBench session id
	AgentID             string
	Content             string
	Files               []model.FileEntry
	ClientUserMessageID string // stable correlation id (ClawBench queueID)
}

// MidTurnInjectResult reports whether the message landed inside the running turn.
//
// Injected == false means the caller MUST fall back to its normal path (queueing
// the message for the next turn). A false result is not an error — it is the
// common case for backends without this capability, or when the turn just ended.
type MidTurnInjectResult struct {
	Injected       bool
	OwnerRequestID string // backend-opaque id of the turn the message joined
	Reason         string // "" | idle | stale | rejected | unsupported | error
}

// MidTurnInjector is implemented by a backend that can inject a message into a
// running turn. This is the ONLY seam between core flow and backend-private
// mid-turn capability.
type MidTurnInjector interface {
	Inject(ctx context.Context, transport RawRPCTransport, req MidTurnInjectRequest) (MidTurnInjectResult, error)
}

// SteerEchoTracker is an OPTIONAL capability a RawRPCTransport may implement:
// "I expect the agent to echo this injected message id back at the point it
// entered the turn". A policy that knows its backend emits such an echo
// registers the id so the transport can turn the echo into a boundary event.
//
// Kept as a separate interface (rather than a method on RawRPCTransport) so the
// generic transport contract stays free of steer-specific semantics, and a
// transport that cannot observe echoes degrades to "no boundary" instead of
// failing to compile. *ACPConn implements it.
type SteerEchoTracker interface {
	// ExpectSteerEcho registers an id whose echo should be observed. Called
	// BEFORE sending, because the echo can arrive before the call returns.
	ExpectSteerEcho(clientUserMessageID string)
	// ForgetSteerEcho drops a registration when the injection did not land, so
	// a declined steer cannot leave an entry behind for the connection's life.
	ForgetSteerEcho(clientUserMessageID string)
}

// LookupMidTurnInjectorFn resolves a backend's mid-turn policy. Wired by
// internal/ai/backends to backends.LookupMidTurnInjector to avoid an import
// cycle. Nil (or returning nil) means no backend supports injection.
var LookupMidTurnInjectorFn func(backendID string) MidTurnInjector

// InjectMidTurn is the single generic entry point used by core flow. It resolves
// the backend's policy and the session's live ACP connection, then delegates.
//
// It reports Injected == false — never an error — when no policy is registered,
// no live connection exists, or the policy declines. Callers fall back to
// queueing, so a missing capability degrades silently rather than failing the
// user's message.
func InjectMidTurn(
	ctx context.Context,
	backendID, sessionID, agentID, content string,
	files []model.FileEntry,
	clientUserMessageID string,
) MidTurnInjectResult {
	if LookupMidTurnInjectorFn == nil {
		return MidTurnInjectResult{}
	}
	injector := LookupMidTurnInjectorFn(backendID)
	if injector == nil {
		return MidTurnInjectResult{}
	}

	conn := GetACPConnManager().GetConn(sessionID)
	if conn == nil {
		return MidTurnInjectResult{}
	}

	res, err := injector.Inject(ctx, conn, MidTurnInjectRequest{
		SessionID:           sessionID,
		AgentID:             agentID,
		Content:             content,
		Files:               files,
		ClientUserMessageID: clientUserMessageID,
	})
	if err != nil {
		slog.Warn("ai: mid-turn injection failed",
			"backend", backendID, "session", sessionID, "err", err)
		return MidTurnInjectResult{Reason: "error"}
	}
	return res
}
