package service

import (
	"log/slog"
	"runtime/debug"
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
//
// Scope is INTERACTIVE sessions only (chat + group). A task execution
// (session_type='scheduled') is deliberately excluded: the scheduler has already
// marked its task_executions row 'failed' at startup, the session is not in the
// session list, and re-running it here would (a) produce an interactive-mode
// turn inside an unattended execution, (b) contradict the execution row, and
// (c) duplicate the work when cron fires the task again. Resuming tasks is the
// scheduler's business, not this worker's.

// defaultRestartResumeDelay lets clients reconnect and re-subscribe before the
// resumed turns start streaming. Their WS subscribers are the only way the
// output is rendered live; starting before they attach would run the turn into
// the void (the reply still lands in the DB, but nothing shows until a reload).
const defaultRestartResumeDelay = 5 * time.Second

var (
	interruptedMu         sync.Mutex
	interruptedSessionIDs []string
)

// recordInterruptedSessions stores the sessions InitDB found mid-stream. Called
// once per server start, before any resume goroutine runs. It REPLACES the set
// rather than appending, so a repeated InitDB (tests, restart flows) cannot
// accumulate stale ids.
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

// resumeSessionFn is the per-session resume seam. A var so tests can observe the
// worker's scheduling decisions without starting a real AI backend.
var resumeSessionFn = ResumeInterruptedSession

// StartRestartResume resumes the sessions the previous process exit interrupted.
// It returns the session ids it scheduled (nil when the feature is off or there
// was nothing to do) — the return value is what lets tests assert the gate
// without racing a goroutine.
//
// A no-op unless "auto-continue on abnormal end" is enabled: that switch is the
// user's opt-in for "spend tokens without me asking", which is exactly what this
// does.
//
// The recorded set is drained BEFORE the gate is consulted, so turning the
// switch on later in the same process does not resurrect a restart that already
// happened — the sessions were already accounted for at startup.
//
// Must be called once, after InitDB, after the config has been applied, and
// after the main listener is bound (otherwise the resumed turns start streaming
// before any client can subscribe).
func StartRestartResume() []string {
	return startRestartResume(defaultRestartResumeDelay, resumeSessionFn)
}

// startRestartResume is the parameterized form: delay and the per-session resume
// function are arguments rather than package state, so tests can drive it
// deterministically (a package-level delay var would be read from a goroutine
// while a test writes it — a data race waiting for the first positive-path test).
func startRestartResume(delay time.Duration, resume func(string) error) []string {
	ids := takeInterruptedSessions()
	if len(ids) == 0 {
		return nil
	}
	if !AutoContinueEnabled() {
		slog.Info("restart resume: skipped, auto-continue is disabled",
			slog.Int("sessions", len(ids)))
		return nil
	}
	slog.Info("restart resume: resuming interrupted sessions",
		slog.Int("count", len(ids)),
		slog.Duration("delay", delay))
	go func() {
		time.Sleep(delay)
		for _, sessionID := range ids {
			resumeOneSafely(sessionID, resume)
		}
	}()
	return ids
}

// resumeOneSafely isolates one session's resume behind a panic barrier.
//
// This chain runs in a bare goroutine 5s after startup, against arbitrary
// historical sessions. An uncaught panic here would terminate the WHOLE process
// (dropping every session, task and WS connection) — the same reason
// LaunchSessionExecution has handleSessionPanic and the group drain has
// recoverGroupDrainPanic. Isolating per session also means one bad session
// cannot stop the rest from being recovered.
func resumeOneSafely(sessionID string, resume func(string) error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("restart resume: panic while resuming session",
				slog.String("session", sessionID),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())))
			// The claim may have been taken before the panic; release it so the
			// session is not left "running" with nothing driving it.
			FinishSessionRun(sessionID)
		}
	}()
	if err := resume(sessionID); err != nil {
		slog.Warn("restart resume: session not resumed",
			slog.String("session", sessionID),
			slog.String("err", err.Error()))
	}
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
// (session gone or archived, not an interactive session, already running, work
// already queued) returns nil because none of them is a failure.
func ResumeInterruptedSession(sessionID string) error {
	if sessionID == "" || !store.DBReady() {
		return nil
	}
	info := GetSessionFullInfo(sessionID)
	if info == nil {
		// Deleted or archived since the restart — nothing to resume against.
		return nil
	}

	// Interactive sessions only. A task execution is excluded on purpose (see the
	// file comment): its task_executions row is already 'failed', its session is
	// not user-visible, and cron will run the task again — resuming it here would
	// duplicate the work under the wrong execution mode.
	sessionType := GetSessionType(sessionID)
	isGroup := sessionType == groupSessionType
	if !isGroup && sessionType != store.SessionTypeChat {
		slog.Info("restart resume: skipping non-interactive session",
			slog.String("session", sessionID),
			slog.String("type", sessionType))
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

	// Resolved before the claim: it is a pure lookup, and doing it first keeps the
	// claim window free of anything that could fail.
	prompt := RestartResumePrompt()

	// Claim before writing anything: a user who re-opened the session and
	// pressed "continue" in the meantime already owns a runner, and this pass
	// must neither start a second turn nor leave a stray system row behind.
	runCtx, created := TryClaimSessionRun(sessionID)
	if !created {
		slog.Info("restart resume: session already running",
			slog.String("session", sessionID))
		return nil
	}

	// The single persisted row: a centered system notice that the session was
	// recovered. For a group it is also what reaches the members (system rows
	// are injected as "[系统] ..."), so it carries the instruction verbatim.
	msgID, err := AddSystemMessage(info.ProjectPath, sessionID, prompt)
	if err != nil {
		// Release the claim AND announce the terminal state: tryClaimSessionRun
		// already broadcast "running", so dropping the runner silently would leave
		// the client showing a spinner forever. FinishSessionRun (rather than
		// SetSessionRunning) also cancels the claim's context instead of leaking
		// it.
		FinishSessionRun(sessionID)
		EmitSessionEventWSOnly(sessionID, statusCompleted, false)
		return err
	}
	emitGroupSystemMessage(sessionID, msgID, prompt)

	if isGroup {
		// Group chats resume through the orchestrator, never as a host-only
		// single-agent turn (the same rule the queue reaper follows): the host
		// routes and the members speak. firstMsgID is passed as 0 because no user
		// message was materialized for this turn — the orchestrator does not use
		// it (see RunGroupTurnDrain).
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
