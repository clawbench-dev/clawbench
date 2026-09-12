package service

import (
	"context"
	"log/slog"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// Mid-turn injection — core-flow side of the seam
// ---------------------------------------------------------------------------
//
// When a message arrives for a session that is already running, the normal path
// queues it and runs it as its own turn afterwards. A backend may instead be
// able to inject it into the turn that is running right now (CodeBuddy's
// session/steer). Which backends can do that, and how, is decided entirely by
// the backend's ai.MidTurnInjector policy — nothing here names a backend.
//
// If injection is declined for any reason, callers fall back to the ordinary
// queue path, so a missing capability changes nothing.

// injectMidTurn is an indirection over ai.InjectMidTurn so tests can substitute
// the backend seam without a live ACP connection.
var injectMidTurn = ai.InjectMidTurn

// SetInjectMidTurnForTest swaps the mid-turn seam and returns the previous one,
// so tests (including those in the external service_test package) can drive the
// core-flow decision without a live agent. Pass nil to restore the default.
func SetInjectMidTurnForTest(fn func(context.Context, string, string, string, string, []model.FileEntry, string) ai.MidTurnInjectResult) func(context.Context, string, string, string, string, []model.FileEntry, string) ai.MidTurnInjectResult {
	prev := injectMidTurn
	if fn == nil {
		injectMidTurn = ai.InjectMidTurn
	} else {
		injectMidTurn = fn
	}
	return prev
}

// TryInjectMidTurn attempts to deliver cfg.Message into the session's running
// turn through the backend's mid-turn policy.
//
// On success it persists the message as a normal user message (queued=0, so it
// is not picked up by the drain loop) and returns (true, msgID).
//
// On any decline it returns (false, 0) and the caller queues the message as
// usual. The only subtle case is "injected but the DB write failed": the message
// has already reached the model, so re-queueing it would deliver it twice. In
// that case it returns (true, 0) — success without a DB row.
func TryInjectMidTurn(cfg EnqueueStartConfig, clientID string) (bool, int64) {
	res := injectMidTurn(context.Background(), cfg.BackendName, cfg.SessionID,
		cfg.AgentID, cfg.Message, cfg.Files, cfg.QueueID)
	if !res.Injected {
		return false, 0
	}

	msgID, err := AddChatMessage(cfg.ProjectPath, cfg.BackendName, cfg.SessionID,
		"user", cfg.Message, cfg.Files, false, "", cfg.QueueID)
	if err != nil {
		slog.Warn("midturn: injected into running turn but persisting the message failed; "+
			"not re-queueing to avoid double delivery",
			"session", cfg.SessionID, "backend", cfg.BackendName, "err", err)
		return true, 0
	}

	slog.Info("midturn: injected message into running turn",
		"session", cfg.SessionID, "backend", cfg.BackendName,
		"owner_request_id", res.OwnerRequestID, "msg_id", msgID)
	return true, msgID
}
