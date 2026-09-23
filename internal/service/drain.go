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
}

// DrainConfig holds the parameters for the drain loop.
type DrainConfig struct {
	SessionID   string
	ProjectPath string
	BackendName string

	// ExecuteRunWithMessage runs one AI stream execution for the given queued message.
	// The drain loop calls this after dequeuing the message from DB and sending
	// the queue_drain event. The message is a ChatMessage with queued=0 (already
	// claimed) and QueueID preserved.
	ExecuteRunWithMessage func(msg model.ChatMessage) DrainResult

	// MarkDoneAndSendFinal sends the terminal event (done/cancelled/error).
	MarkDoneAndSendFinal func(event ai.StreamEvent)

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

// dequeueQueuedMessage is an indirection over DequeueQueuedMessage so tests can
// inject persistent/transient failures into the drain loop.
var dequeueQueuedMessage = DequeueQueuedMessage

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
// on the error branch left later queued messages stuck at queued=1 forever —
// the drain loop had already exited, so nothing would ever dequeue them and
// the UI's pending bubbles could not be cancelled (ISS-239).
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
// It checks terminal conditions, dequeues messages from chat_history (queued=1),
// and executes them, until the queue is empty or a terminal condition is met.
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

	for {
		// Resume an abnormally terminated turn before deciding the loop is
		// done. This must run BEFORE drainHandleTerminal, which treats Err and
		// Empty as terminal — but only when the user has nothing queued, so an
		// explicit message always wins over an automatic resume (otherwise the
		// auto message would be persisted after the user's row and delay their
		// actual request).
		if shouldAutoContinue(cfg, result, autoContinueAttempts) {
			autoContinueAttempts++
			if next, ok := runAutoContinue(cfg, autoContinueAttempts, result); ok {
				result = next
				continue
			}
		}

		if drainHandleTerminal(cfg, result) {
			return
		}

		// Normal completion — check DB queue for next message
		msg, ok, err := dequeueQueuedMessage(cfg.SessionID)
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

		// A message was successfully dequeued — the DB recovered, reset the
		// failure counter.
		dequeueFailures = 0
		// A real user turn is starting: give it its own auto-continue budget.
		autoContinueAttempts = 0

		// Queue has next message — drain it (row already persisted with queued=0)
		slog.Info("drain: draining queued message",
			slog.String("session", cfg.SessionID),
			slog.String("queueId", msg.QueueID),
			slog.Int64("msgId", msg.ID),
			slog.String("text", msg.Content))

		// Emit queue_drain event to WS clients
		emitDrainEvent(cfg.SessionID, ai.StreamEvent{
			Type: "queue_drain",
			QueueEvent: &ai.QueueEventData{
				SessionID: cfg.SessionID,
				QueueID:   msg.QueueID,
				Text:      msg.Content,
				MessageID: msg.ID,
				FilePaths: filePathsFromFiles(msg.Files),
				Files:     msg.Files,
			},
		})
		slog.Info("drain: emitted queue_drain",
			slog.String("session", cfg.SessionID),
			slog.String("queueId", msg.QueueID),
			slog.Int64("msgId", msg.ID))

		// Execute next stream run with the dequeued message
		result = cfg.ExecuteRunWithMessage(msg)
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
