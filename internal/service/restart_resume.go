package service

import (
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/i18n"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// restart_resume.go resumes the sessions a server restart interrupted.
//
// The trigger is an EXACT signal rather than a heuristic: a chat_history row
// still carrying streaming=1 when the process starts can only have been left by
// a process that died mid-turn (a clean shutdown finalizes every active stream,
// and a user cancel finalizes with streaming=0). InitDB already rewrites those
// rows to "cancelled + restart warning"; it also records their session ids here,
// so the decision "this session was interrupted" is made at the one place that
// can know it.
//
// Resuming does NOT replay the interrupted turn's request. The AI is told what
// happened and asked to re-evaluate, because the turn may have stopped after a
// tool call that already changed files: replaying the original prompt would redo
// work that is already done, while a fresh "check the current state and
// continue" turn lets the model read the filesystem and decide.
//
// The instruction is a normal turn for the AI but is NOT persisted as a user
// message — the timeline gets exactly ONE row, the localized system notice that
// the session was recovered. For a group the same notice doubles as the
// instruction: role='system' rows are injected to every member as
// "[系统] <text>", the only channel that reaches the host and the members
// without inventing a fake user bubble.

// restartResumeDelay lets clients reconnect and re-subscribe before the resumed
// turns start streaming. Their WS subscribers are the only way the output is
// rendered live; starting before they attach would run the turn into the void
// (the reply still lands in the DB, but nothing shows until a reload).
//
// A var so tests can drop it to zero; production never changes it.
var restartResumeDelay = 5 * time.Second

var (
	interruptedMu         sync.Mutex
	interruptedSessionIDs []string
)

// recordInterruptedSessions stores the sessions InitDB found mid-stream. Called
// once per server start, before any resume goroutine runs.
func recordInterruptedSessions(sessionIDs []string) {
	interruptedMu.Lock()
	defer interruptedMu.Unlock()
	interruptedSessionIDs = append([]string(nil), sessionIDs...)
}

// takeInterruptedSessions drains the recorded set, so a session is resumed at
// most once per process start.
func takeInterruptedSessions() []string {
	interruptedMu.Lock()
	defer interruptedMu.Unlock()
	ids := interruptedSessionIDs
	interruptedSessionIDs = nil
	return ids
}

// StartRestartResume resumes the sessions the previous process exit interrupted.
//
// A no-op unless "auto-continue on abnormal end" is enabled: that switch is the
// user's opt-in for "spend tokens without me asking", which is exactly what this
// does.
//
// Must be called once, after InitDB and after the config has been applied.
func StartRestartResume() {
	ids := takeInterruptedSessions()
	if len(ids) == 0 {
		return
	}
	if !AutoContinueEnabled() {
		slog.Info("restart resume: skipped, auto-continue is disabled",
			slog.Int("sessions", len(ids)))
		return
	}
	slog.Info("restart resume: resuming interrupted sessions",
		slog.Int("count", len(ids)),
		slog.Duration("delay", restartResumeDelay))
	go func() {
		time.Sleep(restartResumeDelay)
		for _, sessionID := range ids {
			if err := ResumeInterruptedSession(sessionID); err != nil {
				slog.Warn("restart resume: session not resumed",
					slog.String("session", sessionID),
					slog.String("err", err.Error()))
			}
		}
	}()
}

// RestartResumePrompt returns the localized instruction sent to a session whose
// turn the restart interrupted. Rendered from a background goroutine with no
// HTTP request in scope, so it reads the persisted UI language (same reasoning
// as AutoContinuePrompt) and is persisted in that language — switching UI
// language later does not retroactively translate history.
func RestartResumePrompt() string {
	return i18n.T(i18n.LocalizerForLocale(model.Language), "RestartResume")
}

// ResumeInterruptedSession writes the recovery notice and starts one turn asking
// the AI to continue the interrupted work.
//
// Returns an error only for the caller's log. Every "nothing to do" case
// (session gone or archived, already running, work already queued) returns nil
// because none of them is a failure.
func ResumeInterruptedSession(sessionID string) error {
	if sessionID == "" || !store.DBReady() {
		return nil
	}
	info := GetSessionFullInfo(sessionID)
	if info == nil {
		// Deleted or archived since the restart — nothing to resume against.
		return nil
	}
	// A session with queued messages is already owned by the queue reaper, which
	// re-runs the queue itself. Starting a resume turn here would put a second
	// consumer on the same session.
	if GetQueuedCount(sessionID) > 0 {
		slog.Info("restart resume: deferred to the queue reaper",
			slog.String("session", sessionID))
		return nil
	}

	// Claim before writing anything: a user who re-opened the session and
	// pressed "continue" in the meantime already owns a runner, and this pass
	// must neither start a second turn nor leave a stray system row behind.
	runCtx, created := TryClaimSessionRun(sessionID)
	if !created {
		slog.Info("restart resume: session already running",
			slog.String("session", sessionID))
		return nil
	}

	prompt := RestartResumePrompt()

	// The single persisted row: a centered system notice that the session was
	// recovered. For a group it is also what reaches the members (system rows
	// are injected as "[系统] ..."), so it carries the instruction verbatim.
	msgID, err := AddSystemMessage(info.ProjectPath, sessionID, prompt)
	if err != nil {
		// Release the claim so a later pass (or the user) can still run it.
		SetSessionRunning(sessionID, false, true)
		return err
	}
	emitGroupSystemMessage(sessionID, msgID, prompt)

	if IsGroupSession(sessionID) {
		// Group chats resume through the orchestrator, never as a host-only
		// single-agent turn (the same rule the queue reaper follows): the host
		// routes and the members speak. firstMsgID is 0 because no user message
		// was materialized for this turn.
		go func() {
			defer FinishSessionRun(sessionID)
			runGroupDrainLoopFn(runCtx, sessionID, info.ProjectPath, 0, prompt, nil)
		}()
		return nil
	}

	launchConsumerExecution(LaunchConfig{
		SessionID:   sessionID,
		ProjectPath: info.ProjectPath,
		BackendName: info.Backend,
		AgentID:     info.AgentID,
		Message:     prompt,
		RunCtx:      runCtx,
	})
	return nil
}
