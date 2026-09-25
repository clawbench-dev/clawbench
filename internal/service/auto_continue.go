package service

import (
	"context"
	"log/slog"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/i18n"
	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// auto_continue.go owns the "an AI turn died on its own, resume it" policy.
//
// It exists because that decision has to be made in two places that cannot
// share a caller: the interactive drain loop (handler/chat.go and
// session_command.go both end up in RunDrainLoop) and the scheduler's
// executeTask. Three copies of "is this abnormal, and should I retry it" would
// drift exactly like the three copies of the turn implementation did before
// run_turn.go unified them — so the classification and the message
// construction live here and both callers only supply their own turn runner.

// Abnormal-reason codes produced by classifyTurnAbnormality. They are internal
// (never persisted, never sent to clients) and exist only so the retry loop and
// its logs can name why a turn was resumed.
const (
	// abnormalNoTerminal: the event channel closed without a done/error event —
	// the agent process died (crash, OOM, SIGKILL).
	abnormalNoTerminal = "no_terminal"
	// abnormalEmpty: the turn completed structurally but produced nothing.
	abnormalEmpty = "empty"
)

// retryableWarningReasons is the set of backend-reported warning reasons that
// mean "this turn did not really happen", as opposed to "the model answered
// something the user needs to read".
//
// Deliberately excluded:
//   - user_cancel / context_cancel / disconnect: cancellations, not failures.
//   - restart: the server is going down; there is nobody left to resume.
//   - panic: an internal defect. Retrying a deterministic bug just burns tokens.
//   - timeout: the turn already consumed its full budget (30 min); retrying
//     would spend another one.
//   - agent_init_timeout: the agent never answered the handshake. The source
//     comment on ReasonAgentInitTimeout states the identical handshake would
//     simply time out again, so it is not retryable by design.
var retryableWarningReasons = map[string]bool{
	ai.ReasonBackendExit:   true, // CLI process exited abnormally
	ai.ReasonParseError:    true, // CLI output could not be parsed
	ai.ReasonRequestFailed: true, // Codex turn.failed
	ai.ReasonRefused:       true, // ACP stopReason=refusal (model unavailable / upstream error)
	ai.ReasonAgentNoRun:    true, // agent accepted the prompt but never ran the model
}

// classifyTurnAbnormality reports whether a finished turn ended abnormally in a
// way that is worth resuming, and names the reason.
//
// An empty return means "do not auto-continue". That deliberately covers BOTH a
// clean finish and a non-retryable failure (user cancel, timeout, panic, …):
// from the retry loop's point of view they are the same decision, and collapsing
// them keeps the caller from having to re-derive the exclusion list.
//
// Cancel reasons are checked FIRST and unconditionally. They are the one signal
// that must never be overridden by anything else: a user who pressed stop, or
// redirected the turn with "interrupt and send", must not have their session
// silently resumed — that is the single hardest requirement of this feature.
//
// Partial output does NOT suppress a retry. An earlier version required "no
// readable text yet", on the theory that re-prompting a model which already
// answered would duplicate its answer. That reasoning only holds for a turn
// interrupted while writing its reply; it is wrong for the common case of a
// long agentic run (many tool calls) killed part-way through, where the model
// has narrated plenty but the WORK is unfinished. Refusing to resume there left
// the user to click "继续" by hand — exactly what this feature exists to avoid.
// Resuming is cheap and the user opted in; a redundant "continue" on a finished
// answer is a far smaller cost than a stalled long task.
func classifyTurnAbnormality(cancelReason string, receivedTerminal, empty bool, blocks []model.ContentBlock) string {
	if cancelReason != "" {
		return ""
	}
	// No terminal event at all: the agent process died mid-turn.
	if !receivedTerminal {
		return abnormalNoTerminal
	}
	if empty {
		return abnormalEmpty
	}
	// A backend error event lands as a warning block, not in TurnResult.Err.
	for i := range blocks {
		if retryableWarningReasons[blocks[i].Reason] {
			return blocks[i].Reason
		}
	}
	return ""
}

// AutoContinueEnabled reports whether the user opted into auto-resuming
// abnormally terminated turns.
func AutoContinueEnabled() bool {
	return model.ChatAutoContinueEnabled
}

// autoContinueAttemptCeiling bounds an "unlimited" (-1) configuration.
//
// "Unlimited" must never mean literally unbounded: a deterministic failure
// (agent binary missing, model permanently unavailable) would otherwise
// re-prompt forever, spending tokens and writing a user+assistant row pair every
// few seconds. The ceiling is high enough that no realistic transient outage
// reaches it, and reaching it is logged so the misconfiguration is visible.
const autoContinueAttemptCeiling = 50

// autoContinueDelay is how long the loop waits before resuming. It gives a
// crashed agent process time to be reaped, and a rate-limited upstream time to
// recover, before the same request is issued again.
const autoContinueDelay = 3 * time.Second

// AutoContinueAttemptsAllowed reports whether another attempt may run given how
// many have already happened. maxRetries of -1 means "until the ceiling".
func AutoContinueAttemptsAllowed(attempt, maxRetries int) bool {
	if maxRetries < 0 {
		if attempt >= autoContinueAttemptCeiling {
			slog.Warn("auto-continue: hit internal attempt ceiling for unlimited retries",
				slog.Int("attempts", attempt),
				slog.Int("ceiling", autoContinueAttemptCeiling))
			return false
		}
		return true
	}
	return attempt < maxRetries
}

// AutoContinuePrompt returns the localized text sent to resume the session.
//
// This runs in a background goroutine with no HTTP request in scope, so it
// cannot use the per-request localizer. It reads the language the frontend
// persists to the server (model.Language) instead; without that sync the value
// would stay at its default and a user running the UI in English would get a
// Chinese "继续" persisted into their history.
func AutoContinuePrompt() string {
	return i18n.T(i18n.LocalizerForLocale(model.Language), "AutoContinue")
}

// AutoContinueRunnerConfig describes how to resume one session's turns.
type AutoContinueRunnerConfig struct {
	// Ctx is the session's execution context — the same one CancelSession
	// cancels. The runner checks it before and after its delay so a cancel
	// landing during the wait does not launch a new turn.
	Ctx context.Context

	SessionID   string
	ProjectPath string
	BackendName string

	// RunTurn executes exactly one resume turn and returns its outcome. The
	// caller supplies it because the interactive handler and the queue/push
	// path build their requests differently; everything else (gating, delay,
	// cancel checks, message persistence) is shared here.
	RunTurn func(prompt string) DrainResult
}

// autoContinueSleep waits for d, or returns false early if ctx is cancelled.
// A package var so tests can run the loop without real 3-second waits.
var autoContinueSleep = func(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// NewAutoContinueRunner builds the DrainConfig.AutoContinue hook for a session.
//
// It is the single implementation of "resume an abnormally terminated turn" for
// both interactive entry points; the scheduler has its own loop but reuses the
// same gating, delay and message-construction helpers below.
func NewAutoContinueRunner(cfg AutoContinueRunnerConfig) func(attempt int, prev DrainResult) (DrainResult, bool) {
	return func(attempt int, prev DrainResult) (DrainResult, bool) {
		if !AutoContinueEnabled() {
			return prev, false
		}
		if !AutoContinueAttemptsAllowed(attempt-1, model.ChatAutoContinueMaxRetries) {
			return prev, false
		}
		if cfg.Ctx.Err() != nil {
			slog.Info("auto-continue: skipped, context already cancelled",
				slog.String("session", cfg.SessionID),
				slog.String("reason", prev.AbnormalReason))
			return prev, false
		}

		slog.Info("auto-continue: resuming abnormally terminated turn",
			slog.String("session", cfg.SessionID),
			slog.Int("attempt", attempt),
			slog.String("reason", prev.AbnormalReason),
			slog.Int("max_retries", model.ChatAutoContinueMaxRetries))

		// Let the crashed process be reaped (and a rate-limited upstream cool
		// off) before issuing the same request again.
		if !autoContinueSleep(cfg.Ctx, autoContinueDelay) {
			slog.Info("auto-continue: cancelled during delay",
				slog.String("session", cfg.SessionID),
				slog.Int("attempt", attempt))
			return prev, false
		}
		// Re-check after the wait: the user may have pressed stop while we slept.
		if cfg.Ctx.Err() != nil || !IsSessionRunning(cfg.SessionID) {
			slog.Info("auto-continue: aborted after delay, session no longer running",
				slog.String("session", cfg.SessionID),
				slog.Int("attempt", attempt))
			return prev, false
		}

		prompt := AutoContinuePrompt()
		if _, err := PrepareAutoContinueMessage(cfg.SessionID, cfg.ProjectPath, cfg.BackendName); err != nil {
			slog.Error("auto-continue: failed to persist continue message",
				slog.String("session", cfg.SessionID),
				slog.String("err", err.Error()))
			return prev, false
		}
		return cfg.RunTurn(prompt), true
	}
}

// PrepareAutoContinueMessage persists the "continue" message as a REAL user
// message and announces it to every subscriber.
//
// It writes directly to chat_history (AddChatMessage) rather than enqueueing: a
// queued message is announced with queue_added/queue_drain, and the frontend
// would show it in the queue panel instead of inline. A real user row is exactly
// what a user typing "继续" produces, so the bubble is inline, survives a
// reload, and participates in normal history.
//
// Ordering: the row is written BEFORE the resume turn starts, so its id precedes
// the assistant placeholder it produces — the plain id sort is already correct.
//
// SenderClientID is deliberately omitted so every connected client renders the
// bubble; a client-id would make the originating device skip its own echo.
func PrepareAutoContinueMessage(sessionID, projectPath, backendName string) (int64, error) {
	msgID, err := AddChatMessage(projectPath, backendName, sessionID, roleUser,
		AutoContinuePrompt(), nil, false, "")
	if err != nil {
		return 0, err
	}
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: eventTypeUserMessage,
		UserMessage: &ai.UserMessageData{
			MessageID: msgID,
			Content:   AutoContinuePrompt(),
		},
	})
	return msgID, nil
}
