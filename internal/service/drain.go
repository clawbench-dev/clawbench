package service

import (
	"log/slog"
	"runtime/debug"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// DrainResult is a generic result type for the drain loop.
// Both handler/chat.go streamRunResult and service/session_command.go streamRunResultShared
// map to this type.
type DrainResult struct {
	CancelReason string
	Err          string
	Empty        bool
	// AbnormalReason is non-empty when the turn ended abnormally in a way that
	// is worth resuming (see classifyTurnAbnormality). Empty means "do not
	// auto-continue" — which covers both a clean finish and a non-retryable
	// failure such as a user cancel.
	AbnormalReason string
	// MsgID is the finalized assistant row id this turn produced (0 when the
	// turn never opened one). Auto-continue needs it to emit a stream_finalize
	// for the failed turn before resuming: without that per-turn boundary the
	// frontend's still-streaming bubble keeps its spinner until the whole run's
	// terminal `done` sweeps every streaming bubble (or a session switch rebuilds
	// from the DB).
	MsgID int64

	// GoalSatisfied is true when this turn's output declared the auto-cruise
	// goal met (<clawbench-goal-satisfied/>). Single-chat cruise reads it to
	// decide whether a CONFIRMATION round ends the run (see cruise.go). It is
	// only consulted for a clean turn.
	GoalSatisfied bool
}

// DrainConfig holds the parameters for the drain loop.
type DrainConfig struct {
	SessionID   string
	ProjectPath string
	BackendName string

	// ExecuteRunWithMessage runs one AI stream execution for the given queued
	// message. The drain loop calls this after claiming the message from the
	// queue and materializing it into chat_history, so msgID is the chat_history
	// user-message id (already persisted) and row carries the prompt/attachments.
	ExecuteRunWithMessage func(msgID int64, row QueuedRow) DrainResult

	// MarkDoneAndSendFinal sends the terminal event (done/cancelled/error).
	MarkDoneAndSendFinal func(event ai.StreamEvent)

	// OnTurnAnswered is called when a turn finished NORMALLY and more queued
	// work remains — i.e. the drain loop is about to start the next message.
	// Callers use it to notify the user that this answer landed, instead of
	// waiting for the whole queue to finish.
	//
	// It is deliberately NOT called for the last turn: that one exits through
	// MarkDoneAndSendFinal, whose push claims the once-per-run terminal guard.
	// Firing here for it too would either double-notify or — because that guard
	// permits a single push per run — silence the terminal push and lose the
	// last turn's notification entirely. So N messages produce exactly N
	// notifications: N-1 here plus the terminal one.
	//
	// Nil means no callback.
	OnTurnAnswered func()

	// AutoContinue, when non-nil, is called to resume a turn that ended
	// abnormally instead of letting the loop terminate. It returns the result
	// of the resumed turn, or ok=false to refuse the retry — in which case the
	// loop falls through to its normal terminal handling of prev.
	//
	// The caller owns the policy (is the feature enabled, how many attempts
	// remain) and the mechanics (persist the message, wait, run one turn),
	// because those differ between the interactive and scheduled paths. The
	// loop owns only the liveness guards it alone can enforce: it stops
	// retrying when the session is no longer running, and it never retries
	// while the user has queued work waiting.
	AutoContinue func(attempt int, prev DrainResult) (DrainResult, bool)

	// CruiseContinue, when non-nil, is called after a turn finished CLEANLY to
	// run the next SINGLE-CHAT auto-cruise confirmation round instead of
	// terminating (see cruise.go). attempt is the 1-based index of the
	// confirmation round about to run (1 = right after the user's own turn). It
	// returns the round's result and ok=true to continue, or ok=false to stop —
	// in which case the loop falls through to its normal terminal handling.
	//
	// The loop owns the same liveness guards as AutoContinue: it never cruises
	// while the user has queued work waiting, and never after the session stops
	// being running. The caller (NewCruiseRunner) owns the switch check, the
	// budget, the "already satisfied" decision, and the confirmation prompt.
	//
	// Nil means the session never cruises (every non-cruise path).
	CruiseContinue func(attempt int, prev DrainResult) (DrainResult, bool)
}

// emitDrainEvent emits a stream event to WS clients via StreamHub.
func emitDrainEvent(sessionID string, event ai.StreamEvent) {
	ws.EmitToSession(sessionID, event)
}

// maxDequeueRetries bounds how many consecutive DB dequeue failures the drain
// loop tolerates (5 × 100ms ≈ 500ms) before giving up on the queue. A brief
// transient DB hiccup is retried without losing messages; a persistent failure
// aborts the loop so the session is not stuck in "loading" forever with the
// queue silently dead.
const maxDequeueRetries = 5

// dequeueQueuedMessage is an indirection over ClaimNextAndMaterialize so tests
// can inject persistent/transient failures into the drain loop.
var dequeueQueuedMessage = ClaimNextAndMaterialize

// emitUserMessage announces a user message that has just been materialized into
// chat_history. It is the single source of a user bubble's content: queue_drain
// carries no text/files and never creates a bubble, so a client can never render
// two bubbles for the same message.
//
// injected marks a message that joined the RUNNING assistant reply (mid-turn
// steering) rather than starting its own turn; only a merge-only injection
// (claude/codex) sets it, so the frontend can annotate the bubble.
func emitUserMessage(sessionID string, msgID int64, row QueuedRow, injected bool) {
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: eventTypeUserMessage,
		UserMessage: &ai.UserMessageData{
			MessageID: msgID,
			Content:   row.Content,
			Files:     row.Files,
			QueueID:   row.QueueID,
			Injected:  injected,
		},
	})
}

// abortDrain is the drain loop's failure exit after dequeue keeps failing past
// the retry window. It mirrors the user-cancel branch: collect queue IDs,
// clear the queue so nothing is left stuck, emit queue_cancel so the frontend
// removes pending bubbles, then send a terminal error event so the session
// leaves the loading state with a visible failure instead of hanging.
func abortDrain(cfg DrainConfig, cause error) {
	queueIDs, _ := GetQueuedQueueIDs(cfg.SessionID)
	_ = ClearQueuedMessages(cfg.SessionID)
	if len(queueIDs) > 0 {
		emitDrainEvent(cfg.SessionID, ai.StreamEvent{
			Type: "queue_cancel",
			QueueEvent: &ai.QueueEventData{
				SessionID: cfg.SessionID,
				QueueIDs:  queueIDs,
			},
		})
	}
	slog.Error("drain: dequeue kept failing past retry window, aborting queue",
		slog.String("session", cfg.SessionID),
		slog.String("error", cause.Error()))
	cfg.MarkDoneAndSendFinal(ai.StreamEvent{
		Type:  eventTypeError,
		Error: "drain: dequeue failed: " + cause.Error(),
	})
}

// clearQueueAndEmitCancel drops the session's remaining queued messages and
// emits queue_cancel so the frontend immediately removes its pending bubbles.
// It is shared by every terminal branch that must not leave the queue alive:
// user cancel, the drained message failing, or an empty result. Skipping this
// on the error branch left later queued messages stuck in the queue forever —
// the drain loop had already exited, so nothing would ever claim them and
// the UI's queue entries could not be cancelled (ISS-239).
func clearQueueAndEmitCancel(cfg DrainConfig) {
	// Collect queue IDs before clearing for queue_cancel event
	queueIDs, _ := GetQueuedQueueIDs(cfg.SessionID)
	_ = ClearQueuedMessages(cfg.SessionID)

	// Emit queue_cancel so frontend can immediately remove pending messages
	if len(queueIDs) > 0 {
		emitDrainEvent(cfg.SessionID, ai.StreamEvent{
			Type: "queue_cancel",
			QueueEvent: &ai.QueueEventData{
				SessionID: cfg.SessionID,
				QueueIDs:  queueIDs,
			},
		})
	}
}

// drainHandleTerminal emits the final event and reports true when the previous
// stream run ended in a terminal state (user cancel / error / empty / other
// cancel). Every branch that abandons the queue (user cancel, error, empty)
// clears the DB queue and emits queue_cancel so the frontend can immediately
// remove pending bubbles — see clearQueueAndEmitCancel for why.
func drainHandleTerminal(cfg DrainConfig, result DrainResult) bool {
	// Interrupt: the user stopped the CURRENT turn to send something else, but
	// the queue is exactly what they want to keep — the interrupted turn is
	// replaced by the next queued message. So this branch must NOT clear the
	// queue and must NOT send a terminal event: returning false lets
	// RunDrainLoop fall through and dequeue the next message.
	//
	// Distinct from the cancelReasonUser branch below, which drops the queue
	// (cancel semantics are "I do not want any of this").
	if result.CancelReason == cancelReasonInterrupt {
		return false
	}
	if result.CancelReason == cancelReasonUser {
		clearQueueAndEmitCancel(cfg)
		cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: statusCancelled})
		return true
	}
	if result.Err != "" {
		// A run failed — the queue must not keep later messages stuck as
		// pending. Drop the rest and surface the error so the user sees why
		// their queued messages were cancelled.
		clearQueueAndEmitCancel(cfg)
		cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: eventTypeError, Error: result.Err})
		return true
	}
	if result.Empty {
		clearQueueAndEmitCancel(cfg)
		cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: eventTypeError, Error: "AI returned no content", Reason: ai.ReasonEmpty})
		return true
	}
	if result.CancelReason != "" {
		cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: statusCancelled})
		return true
	}
	return false
}

// shouldAutoContinue reports whether the loop may hand this result to the
// caller's AutoContinue hook.
//
// It enforces the two liveness guards that belong to the loop rather than to
// any individual caller, because only the loop can see both:
//
//   - the session must still be running. CancelSession removes the runner, so a
//     user who pressed stop while the previous attempt was being torn down must
//     not have a new turn launched for them.
//   - the user must have nothing queued. Their explicit message outranks an
//     automatic resume.
func shouldAutoContinue(cfg DrainConfig, result DrainResult, attempts int) bool {
	if cfg.AutoContinue == nil || result.AbnormalReason == "" {
		return false
	}
	if GetQueuedCount(cfg.SessionID) > 0 {
		slog.Info("auto-continue: skipped, user has queued messages",
			slog.String("session", cfg.SessionID),
			slog.Int("attempts", attempts))
		return false
	}
	if !IsSessionRunning(cfg.SessionID) {
		slog.Info("auto-continue: skipped, session no longer running",
			slog.String("session", cfg.SessionID),
			slog.Int("attempts", attempts))
		return false
	}
	return true
}

// runAutoContinue invokes the caller's hook with a panic barrier.
//
// A panic here would otherwise unwind the whole drain goroutine: the session
// would be left with a runner registered but no loop, and its terminal event
// would never be sent — the frontend would sit in "streaming" until a reload.
// Recovering and refusing the retry degrades to the pre-feature behavior.
func runAutoContinue(cfg DrainConfig, attempt int, prev DrainResult) (result DrainResult, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("auto-continue: hook panicked, giving up",
				slog.String("session", cfg.SessionID),
				slog.Int("attempt", attempt),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())))
			result, ok = prev, false
		}
	}()
	return cfg.AutoContinue(attempt, prev)
}

// shouldCruiseContinue reports whether the loop may hand this result to the
// caller's CruiseContinue hook (single-chat auto-cruise).
//
// It is the counterpart of shouldAutoContinue and enforces the same two
// liveness guards (session still running; nothing queued by the user), plus one
// cruise-specific rule: the turn must have finished CLEANLY. Cruise takes over
// the "reply finished" point only — a cancelled, empty or abnormal turn is a
// terminal condition the loop must handle as usual (the user stopped, the model
// died), never something to wrap in a confirmation round.
func shouldCruiseContinue(cfg DrainConfig, result DrainResult) bool {
	if cfg.CruiseContinue == nil {
		return false
	}
	if result.CancelReason != "" || result.Err != "" || result.Empty || result.AbnormalReason != "" {
		return false
	}
	if GetQueuedCount(cfg.SessionID) > 0 {
		slog.Info("cruise: skipped, user has queued messages",
			slog.String("session", cfg.SessionID))
		return false
	}
	if !IsSessionRunning(cfg.SessionID) {
		slog.Info("cruise: skipped, session no longer running",
			slog.String("session", cfg.SessionID))
		return false
	}
	return true
}

// runCruiseContinue invokes the caller's hook with a panic barrier, mirroring
// runAutoContinue: a panic here must not unwind the drain goroutine and leave
// the session running with no loop and no terminal event. Recovering and
// refusing the round degrades to a normal finish.
func runCruiseContinue(cfg DrainConfig, attempt int, prev DrainResult) (result DrainResult, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("cruise: hook panicked, giving up",
				slog.String("session", cfg.SessionID),
				slog.Int("attempt", attempt),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())))
			result, ok = prev, false
		}
	}()
	return cfg.CruiseContinue(attempt, prev)
}

// resumeBeforeTerminal runs the two "the turn is over but the loop is not"
// hooks, in order, and reports whether either produced a result the loop should
// continue with:
//
//  1. auto-continue resumes an ABNORMALLY terminated turn (a crash, a backend
//     error). Its budget counter is incremented per attempt.
//  2. single-chat auto-cruise runs the next confirmation round after a CLEANLY
//     finished turn. Its round counter is incremented per round.
//
// Order matters: an abnormal turn is auto-continue's job (its AbnormalReason
// makes shouldCruiseContinue false), so the two never both fire for one result.
// Both are gated on the user having nothing queued and the session still
// running, which only the loop can see — hence the guards live in the
// shouldXxx helpers, not in the hooks.
//
// Extracted from RunDrainLoop so the loop body stays readable (and under the
// cyclomatic-complexity gate); the loop owns the `continue` that a non-ok=false
// return implies.
func resumeBeforeTerminal(cfg DrainConfig, result DrainResult, autoContinueAttempts, cruiseRounds *int) (DrainResult, bool) {
	if shouldAutoContinue(cfg, result, *autoContinueAttempts) {
		*autoContinueAttempts++
		if next, ok := runAutoContinue(cfg, *autoContinueAttempts, result); ok {
			return next, true
		}
	}
	if shouldCruiseContinue(cfg, result) {
		*cruiseRounds++
		if next, ok := runCruiseContinue(cfg, *cruiseRounds, result); ok {
			return next, true
		}
	}
	return result, false
}

// retryDequeueAfterError accounts for one failed dequeue and reports whether
// the drain loop may keep going. A real DB error must not exit as if the queue
// were empty (that would silently lose messages): transient blips are retried
// up to maxDequeueRetries; a persistent failure aborts the queue with an
// explicit error instead of spinning forever.
func retryDequeueAfterError(cfg DrainConfig, dequeueFailures *int, err error) bool {
	*dequeueFailures++
	slog.Error("drain: dequeue failed", slog.String("session", cfg.SessionID), slog.String("error", err.Error()))
	if *dequeueFailures >= maxDequeueRetries {
		abortDrain(cfg, err)
		return false
	}
	time.Sleep(100 * time.Millisecond)
	return true
}

// RunDrainLoop runs the session's turn loop after an initial stream execution.
// It checks terminal conditions, claims messages from queued_messages
// (materializing each into chat_history), and executes them, until the queue is
// empty or a terminal condition is met.
//
// Exiting is the delicate part. The loop must not decide "queue is empty" and
// leave while a concurrent send is mid-flight: that send would have seen a live
// runner, handed its message to this loop, and be left with nobody to run it —
// the "message never gets an answer until I cancel and re-send" failure.
//
// That window is closed by retireRunner, which re-checks for late work under the
// same lock the sender used to submit. This replaces the previous
// SignalDrain/WaitForEnqueue dance plus a 100ms self-heal goroutine: the exit
// decision and the late-work check are now one atomic step.
func RunDrainLoop(cfg DrainConfig, result DrainResult) {
	// Consecutive dequeue failures since the last successful drain. Reset on
	// every successful dequeue (or empty-queue done), so transient DB blips
	// that recover are retried without losing messages.
	dequeueFailures := 0

	// Auto-continue attempts spent on the CURRENT user turn. Reset whenever a
	// real queued user message is dequeued: the budget belongs to a user turn,
	// not to the session, so a long conversation gets a fresh allowance per
	// message instead of exhausting it once and never resuming again.
	autoContinueAttempts := 0

	// Confirmation rounds run for the CURRENT user turn (single-chat cruise).
	// Reset alongside autoContinueAttempts on every dequeue: the cruise budget
	// is per user turn too. attempt is passed to CruiseContinue as the 1-based
	// index of the round about to run (1 = right after the user's own turn).
	cruiseRounds := 0

	// hooksDeclined records that the CURRENT `result` has already been offered
	// to the resume hooks and both declined.
	//
	// The loop must offer a given turn result to the hooks EXACTLY ONCE. Every
	// `continue` that does not produce a fresh turn — the retire-late-work
	// re-check, a dequeue retry — loops back to the top, and without this flag
	// it would re-offer the same result. That is not merely wasteful: the cruise
	// hook writes its terminal "task confirmed complete" system line every time
	// it is asked and answers "stop", so each extra pass wrote the line again
	// and inflated the reported round count (the reported "ran two rounds, the
	// second one empty"). Reset whenever `result` is replaced by a fresh turn.
	hooksDeclined := false

	for {
		// Resume an abnormally terminated turn, or run the next single-chat
		// cruise confirmation round, before deciding the loop is done. Both
		// must run BEFORE drainHandleTerminal, which treats Err/Empty as
		// terminal; a resumed/continued turn restarts the iteration.
		if !hooksDeclined {
			if next, ok := resumeBeforeTerminal(cfg, result, &autoContinueAttempts, &cruiseRounds); ok {
				result = next
				hooksDeclined = false
				continue
			}
			// Both hooks declined this result — do not ask them again unless a
			// fresh turn replaces it (see the flag's comment).
			hooksDeclined = true
		}

		if drainHandleTerminal(cfg, result) {
			return
		}

		// Normal completion — claim the next queued message and materialize it
		// into chat_history (id allocated now, before its reply).
		row, msgID, ok, err := dequeueQueuedMessage(cfg.SessionID)
		if err != nil {
			if !retryDequeueAfterError(cfg, &dequeueFailures, err) {
				return
			}
			continue
		}
		if !ok {
			// The queue looks empty. Retire the runner: if a message arrived
			// since we looked, retireRunner reports it and we keep going rather
			// than exit and strand it.
			if !retireRunner(cfg.SessionID) {
				continue
			}
			cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: eventTypeDone})
			return
		}

		// A message was successfully claimed — the DB recovered, reset the
		// failure counter.
		dequeueFailures = 0
		// A real user turn is starting: give it its own auto-continue budget and
		// its own cruise budget (the confirmation-round count restarts, so a
		// long conversation gets a fresh allowance per message).
		autoContinueAttempts = 0
		cruiseRounds = 0
		// The dequeued turn below REPLACES `result`, so the new result must be
		// offered to the hooks afresh (see hooksDeclined).
		hooksDeclined = false

		// The PREVIOUS turn just finished normally and more work is waiting, so
		// this is where an intermediate answer is reported. The loop's terminal
		// `done` only fires once the whole queue is drained, which is why a user
		// with several queued messages used to be notified a single time — after
		// the last one. Reporting here gives every intermediate answer its own
		// notification (the final one is covered by the terminal push, so N
		// messages still produce exactly N).
		//
		// Gated on a CLEAN completion: an interrupted or abnormally-terminated
		// turn is not an answer worth announcing (the user cut it short, or it
		// was abandoned), and `result` still holds that turn at this point.
		if cfg.OnTurnAnswered != nil &&
			result.CancelReason == "" && result.Err == "" && !result.Empty && result.AbnormalReason == "" {
			cfg.OnTurnAnswered()
		}

		slog.Info("drain: draining queued message",
			slog.String("session", cfg.SessionID),
			slog.String("queueId", row.QueueID),
			slog.Int64("msgId", msgID),
			slog.String("text", row.Content))

		// 1. Announce the real user message (content lives here, and only here).
		emitUserMessage(cfg.SessionID, msgID, row, false)

		// 2. Signal the turn boundary: finalize the previous reply, adopt the
		//    bubble, open a placeholder. Carries no content by design.
		emitDrainEvent(cfg.SessionID, ai.StreamEvent{
			Type: "queue_drain",
			QueueEvent: &ai.QueueEventData{
				SessionID: cfg.SessionID,
				QueueID:   row.QueueID,
				MessageID: msgID,
			},
		})

		// Execute next stream run with the dequeued message
		result = cfg.ExecuteRunWithMessage(msgID, row)
		// Loop continues
	}
}

// filePathsFromFiles extracts the file paths from FileEntry list for the
// queue_drain event payload (the frontend drains by queueId; filePaths keep
// the legacy field populated).
//
// Quote entries are skipped: their Path is a label (often empty), and emitting
// it would surface a bogus "" path in the legacy channel.
func filePathsFromFiles(files []model.FileEntry) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		if f.IsQuote() {
			continue
		}
		paths = append(paths, f.Path)
	}
	return paths
}
