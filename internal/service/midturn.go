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
// This backs the queued entry's "insert into the current reply" action: the user
// queued a message (the default), then explicitly asked for it to join the turn
// that is running right now instead of waiting for the next one.
//
// Ordering is free here. The claim materializes the message into chat_history
// in the same transaction, so it gets its DB id NOW — between the running turn's
// "before" assistant row and the "after" row the steer split will create:
//
//	Q1(1) → assistant·before(2) → Q2 inserted(3) → assistant·after(4)
//
// with no re-sorting and no queue anchor.
//
// Returns:
//   - (true,  msgID, nil)  the message joined the running turn; the caller
//     should announce it (user_message) so every device drops the queue entry.
//   - (false, 0, nil)      declined (turn ended, backend can't inject). The
//     message was put back into the queue, so the caller reports "could not
//     insert" and the normal drain delivers it later. Nothing was lost.
//   - (false, 0, err)      the claim succeeded but the restore failed: the row
//     is no longer in the queue and the drain loop cannot see it. This is the
//     one genuinely bad outcome and MUST be surfaced as an error — reporting it
//     as a benign decline would tell the user their message is still queued
//     when it is not (ErrMessageStranded).
func InjectQueuedMessage(sessionID, queueID string) (bool, int64, error) {
	// Claim + materialize atomically: the row must still be queued (a concurrent
	// drain may have picked it up already, in which case there is nothing to
	// insert).
	row, msgID, ok, err := ClaimByQueueIDAndMaterialize(sessionID, queueID)
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

	res := injectMidTurn(context.Background(), row.Backend, sessionID,
		GetSessionAgentID(sessionID), row.Content, row.Files, queueID)
	if !res.Injected {
		// Put it back so the drain loop still delivers it. Restoring (rather
		// than leaving it materialized-but-unanswered) is what keeps a decline
		// harmless.
		if rerr := requeueWithRetry(row, msgID); rerr != nil {
			// The message is now visible in chat_history but was never answered
			// and is not in the queue. Surface it as a hard error so the caller
			// can tell the user to resend.
			slog.Error("midturn: injection declined AND requeue failed; message is stranded",
				"session", sessionID, "queue_id", queueID, "msg_id", msgID, "err", rerr)
			return false, 0, fmt.Errorf("%w: %w", ErrMessageStranded, rerr)
		}
		slog.Info("midturn: injection declined, message restored to the queue",
			"session", sessionID, "queue_id", queueID, "reason", res.Reason)
		return false, 0, nil
	}

	// Announce the real user message so every device renders it inline. Emitted
	// here (not by the caller) because this is where the content lives; the
	// caller follows up with queue_inject to drop the entry from the queue panel.
	// No SenderClientID: the message may have been queued by another device.
	emitUserMessage(sessionID, msgID, row)

	slog.Info("midturn: inserted queued message into running turn",
		"session", sessionID, "backend", row.Backend,
		"owner_request_id", res.OwnerRequestID, "msg_id", msgID)
	return true, msgID, nil
}

// ErrMessageStranded reports that a claimed queued message could not be put
// back into the queue after a declined insertion. The message was materialized
// into chat_history but never answered, so the user must be told to resend
// rather than being reassured that it is still queued.
var ErrMessageStranded = errors.New("queued message stranded: claim succeeded but requeue failed")

// requeueWithRetry undoes a claim+materialize, retrying a few times on transient
// DB contention (SQLITE_BUSY is a realistic cause here). The reverse transaction
// is idempotent in the sense that a failed attempt rolls back completely, so
// retrying is free of side effects.
func requeueWithRetry(row QueuedRow, msgID int64) error {
	var err error
	for range 3 {
		if err = RequeueMaterialized(row, msgID); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return err
}
