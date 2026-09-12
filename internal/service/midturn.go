package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// Mid-turn injection — core-flow side of the seam
// ---------------------------------------------------------------------------
//
// When a message arrives for a session that is already running, it is ALWAYS
// queued and runs as its own turn afterwards — for every backend. Joining the
// turn that is running right now is a separate, explicit action the user takes
// on the queued message (CodeBuddy's session/steer, via InjectQueuedMessage).
// Which backends can do that, and how, is decided entirely by the backend's
// ai.MidTurnInjector policy — nothing here names a backend.
//
// If the action is declined for any reason the message stays queued, so a
// missing capability changes nothing about the normal flow.

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

// InjectQueuedMessage delivers an ALREADY QUEUED message into the running turn.
//
// This backs the queued-bubble "insert into the current reply" action: the user
// queued a message (the default), then explicitly asked for it to join the turn
// that is running right now instead of waiting for the next one.
//
// Ordering is free here. The queued row was persisted when it was enqueued, so
// it already holds a DB id between the running turn's assistant row and any
// later rows. Claiming it (queued=1 → 0) rather than deleting and re-inserting
// keeps that id, so the conversation order stays
//
//	Q1(1) → assistant·before(2) → Q2 inserted(3) → assistant·after(4)
//
// with no re-sorting — the same property that makes the steer split work.
//
// Returns:
//   - (true,  msgID, nil)  the message joined the running turn; the caller
//     should announce it (user_message) so every device drops the pending bubble.
//   - (false, 0, nil)      declined (turn ended, backend can't inject). The row
//     was restored to queued=1, so the caller reports "could not insert" and
//     the normal drain delivers it later. Nothing was lost.
//   - (false, 0, err)      the claim succeeded but the restore failed: the row
//     is neither queued nor delivered and the drain loop cannot see it. This is
//     the one genuinely bad outcome and MUST be surfaced as an error — reporting
//     it as a benign decline would tell the user their message is still queued
//     when it is in fact stranded (ErrMessageStranded).
func InjectQueuedMessage(sessionID, queueID string) (bool, int64, error) {
	// Claim atomically: the row must still be queued (a concurrent drain may
	// have picked it up already, in which case there is nothing to insert).
	msg, ok, err := DequeueQueuedMessageByQueueID(sessionID, queueID)
	if err != nil {
		slog.Warn("midturn: failed to claim queued message for injection",
			"session", sessionID, "queue_id", queueID, "err", err)
		return false, 0, nil // claim failed cleanly: the row is untouched
	}
	if !ok {
		// Already drained (or cancelled) — the message is either running as its
		// own turn or gone. Either way there is nothing to insert.
		slog.Info("midturn: queued message no longer claimable for injection",
			"session", sessionID, "queue_id", queueID)
		return false, 0, nil
	}

	res := injectMidTurn(context.Background(), msg.Backend, sessionID,
		GetSessionAgentID(sessionID), msg.Content, msg.Files, queueID)
	if !res.Injected {
		// Put it back so the drain loop still delivers it. Restoring (rather
		// than leaving it claimed) is what keeps a decline harmless.
		if rerr := requeueWithRetry(msg.ID); rerr != nil {
			// The row is now neither queued nor delivered. Surface it as a
			// hard error so the caller can tell the user to resend; the drain
			// loop can never recover a row it cannot see (it selects queued=1).
			slog.Error("midturn: injection declined AND requeue failed; message is stranded",
				"session", sessionID, "queue_id", queueID, "msg_id", msg.ID, "err", rerr)
			return false, 0, fmt.Errorf("%w: %v", ErrMessageStranded, rerr)
		}
		slog.Info("midturn: injection declined, message restored to the queue",
			"session", sessionID, "queue_id", queueID, "reason", res.Reason)
		return false, 0, nil
	}

	slog.Info("midturn: inserted queued message into running turn",
		"session", sessionID, "backend", msg.Backend,
		"owner_request_id", res.OwnerRequestID, "msg_id", msg.ID)
	return true, msg.ID, nil
}

// ErrMessageStranded reports that a claimed queued message could not be put
// back into the queue after a declined insertion. The row exists but is
// invisible to the drain loop, so the user must be told to resend rather than
// being reassured that it is still queued.
var ErrMessageStranded = errors.New("queued message stranded: claim succeeded but requeue failed")

// requeueWithRetry puts a claimed message back into the queue, retrying a few
// times on transient DB contention (SQLITE_BUSY is a realistic cause here).
// The UPDATE is idempotent, so retrying is free of side effects.
func requeueWithRetry(msgID int64) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = RequeueMessage(msgID); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return err
}
