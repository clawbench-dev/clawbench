package service

import (
	"context"
	"log/slog"

	"clawbench/internal/i18n"
	"clawbench/internal/model"
)

// cruise.go owns the SINGLE-CHAT "auto-cruise" policy: keep the AI working
// until it explicitly confirms the task is complete, instead of handing the
// floor back to the user the moment a reply finishes.
//
// It is the single-chat counterpart of the group orchestrator's cruise mode
// (design §15). Group cruise intercepts four "hand back" points (host end tag,
// relay drain, @User, a question); single chat has exactly ONE — the reply
// finishing — so its cruise is a loop of CONFIRMATION rounds:
//
//   - turn 1 is the user's own message (with the cruise addendum riding its
//     prompt, see BuildChatRequest);
//   - every turn after that is a hidden confirmation round (the confirmation
//     prompt is prepended to the prompt, never persisted);
//   - a CONFIRMATION round that emits <clawbench-goal-satisfied/> ends the run
//     (the STRICT rule: an ordinary reply's tag does NOT end it — see below);
//   - the confirmation-round count is bounded by the existing global
//     chat.group_max_speeches cap, so a model that never confirms cannot spin
//     forever.
//
// The policy lives here (not in the drain loop) for the same reason
// auto_continue.go does: the interactive drain loop and the shared launch path
// both need it, and two copies would drift.

// CruiseRunnerConfig describes how to run one session's confirmation rounds.
type CruiseRunnerConfig struct {
	// Ctx is the session's execution context — the same one CancelSession
	// cancels. The runner checks it before starting a confirmation round so a
	// cancel landing during the previous turn's teardown does not launch one.
	Ctx context.Context

	SessionID   string
	ProjectPath string
	BackendName string

	// RunTurn executes exactly one confirmation round and returns its outcome.
	// The caller supplies it because the interactive handler and the shared
	// launch path build their requests differently; everything else (gating,
	// system lines, the per-turn boundary, the confirmation prompt) is shared.
	RunTurn func(prompt string) DrainResult
}

// NewCruiseRunner builds the DrainConfig.CruiseContinue hook for a session.
//
// It is called by the drain loop after a turn finished CLEANLY, with `attempt`
// being the 1-based index of the confirmation round about to run (1 = the first
// confirmation round, right after the user's own message). It returns the
// confirmation round's result and ok=true to continue the loop, or ok=false to
// stop (the caller then runs its normal terminal handling on prev).
//
// The strict rule: only a CONFIRMATION round's <clawbench-goal-satisfied/>
// counts. A tag in the user's own turn (attempt == 1) is ignored — letting the
// model end the run by its own say-so is exactly the premature wrap-up cruise
// exists to prevent. So the runner NEVER stops on attempt == 1; it always runs
// at least one confirmation round.
func NewCruiseRunner(cfg CruiseRunnerConfig) func(attempt int, prev DrainResult) (DrainResult, bool) {
	return func(attempt int, prev DrainResult) (DrainResult, bool) {
		if !GetSessionCruise(cfg.SessionID) {
			return prev, false
		}

		// A PREVIOUS confirmation round declared the goal met → stop. attempt>1
		// because attempt==1's `prev` is the user's own turn, whose tag must NOT
		// end the run (the strict rule above).
		if attempt > 1 && prev.GoalSatisfied {
			writeGroupSystemMessage(cfg.ProjectPath, cfg.SessionID, i18n.T(
				i18n.LocalizerForLocale(model.Language), "CruiseSatisfied",
				map[string]any{"Rounds": attempt - 1},
			))
			return prev, false
		}

		// Budget: at most GetGroupMaxSpeeches() confirmation rounds. attempt is
		// the round we are about to run, so `attempt-1` rounds have already run;
		// stop once they reach the cap. The cap is the same global setting group
		// chat uses (chat.group_max_speeches, default 100) — no new knob.
		if attempt-1 >= GetGroupMaxSpeeches() {
			writeGroupSystemMessage(cfg.ProjectPath, cfg.SessionID, i18n.T(
				i18n.LocalizerForLocale(model.Language), "CruiseMaxRounds"))
			slog.Info("cruise: confirmation-round cap reached",
				slog.String("session", cfg.SessionID),
				slog.Int("rounds", attempt-1),
				slog.Int("cap", GetGroupMaxSpeeches()))
			return prev, false
		}

		if cfg.Ctx.Err() != nil || !IsSessionRunning(cfg.SessionID) {
			slog.Info("cruise: skipped, session no longer running",
				slog.String("session", cfg.SessionID),
				slog.Int("attempt", attempt))
			return prev, false
		}

		// Visible trace: the ONLY user-facing signal that the AI is confirming
		// rather than answering a new message (the confirmation prompt itself is
		// never persisted). Written before the round so the timeline reads
		// naturally: line, then the AI's confirmation reply.
		writeGroupSystemMessage(cfg.ProjectPath, cfg.SessionID, i18n.T(
			i18n.LocalizerForLocale(model.Language), "CruiseConfirming"))

		// Close the previous turn's bubble BEFORE the confirmation round starts,
		// so the frontend finalizes it without ending the run (the loading state
		// and stop button stay armed). Without this per-turn boundary the prior
		// bubble keeps its spinner until the whole run's terminal `done`.
		emitTurnFinalize(cfg.SessionID, prev.MsgID)

		return cfg.RunTurn(BuildCruiseSingleConfirmPrompt()), true
	}
}
