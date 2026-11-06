package service

import (
	"context"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// task_restart_resume.go resumes the scheduled-task executions a server restart
// cut short.
//
// It is the sibling of restart_resume.go, which deliberately handles only
// interactive sessions (chat + group). A task run is not a conversation: its
// unit of work is the task_executions row, and that row is what has to be
// carried across the restart. So this worker resumes at the EXECUTION level —
// same session, same execution row, same task — instead of inventing a new
// interactive turn.
//
// Why it has to exist at all: an event-triggered task (forge PR/issue, mail)
// consumes its trigger exactly once. A restart in the middle of the run fires
// nothing again, so without this the work is lost silently — the run shows as
// "cancelled" and nobody ever picks it back up. Cron tasks hit the same wall
// when the next tick is hours away.
//
// The trigger is EXACT, not a heuristic: CancelAllRunning (the graceful-shutdown
// path only) stamps restart_interrupted=1 on the row it is about to cancel. A
// user cancel, a timeout or an ordinary failure never sets it, so those runs are
// never resurrected.
//
// Like every "spend tokens without me asking" path this is gated on the
// auto-continue switch. It is deliberately NOT limited to one resume per run:
// the user asked for the same behavior interactive sessions have, and those
// resume again after every restart.

// StartTaskRestartResume resumes the task executions the previous process exit
// interrupted. Returns the execution ids it scheduled (nil when the feature is
// off or there was nothing to do) — the return value is what lets tests assert
// the gate without racing the goroutine.
func StartTaskRestartResume() []int64 {
	return startTaskRestartResume(defaultRestartResumeDelay, ResumeTaskExecution)
}

// startTaskRestartResume is the parameterized form: delay and the per-execution
// resume function are arguments rather than package state, so tests can drive it
// deterministically (same reasoning as startRestartResume).
func startTaskRestartResume(delay time.Duration, resume func(int64) error) []int64 {
	ids := interruptedExecutionIDs()
	if len(ids) == 0 {
		return nil
	}
	if !AutoContinueEnabled() {
		// Clear the flags even though nothing runs: they describe "interrupted
		// by the restart that just happened", and that restart is accounted for
		// now. Leaving them set would make a later switch-on resurrect a run
		// from an older boot.
		clearInterruptedExecutions(ids)
		slog.Info("task restart resume: skipped, auto-continue is disabled",
			slog.Int("executions", len(ids)))
		return nil
	}
	slog.Info("task restart resume: resuming interrupted executions",
		slog.Int("count", len(ids)),
		slog.Duration("delay", delay))
	go func() {
		time.Sleep(delay)
		for _, execID := range ids {
			// Checked per execution: a shutdown landing during the loop must
			// stop the remaining ones too (see startRestartResume for the
			// streaming-row consequence of claiming work after the drain).
			if IsShuttingDown() {
				slog.Info("task restart resume: aborted, server is shutting down",
					slog.Int64("execution", execID))
				return
			}
			resumeExecutionSafely(execID, resume)
		}
	}()
	return ids
}

// resumeExecutionSafely isolates one execution's resume behind a panic barrier.
// This chain runs in a bare goroutine seconds after startup against historical
// rows; an uncaught panic here would take down the whole process.
func resumeExecutionSafely(execID int64, resume func(int64) error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("task restart resume: panic while resuming execution",
				slog.Int64("execution", execID),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())))
		}
	}()
	if err := resume(execID); err != nil {
		slog.Warn("task restart resume: execution not resumed",
			slog.Int64("execution", execID),
			slog.String("err", err.Error()))
	}
}

// interruptedExecutionIDs returns the executions the previous shutdown stamped
// as interrupted, oldest first.
func interruptedExecutionIDs() []int64 {
	if !store.DBReady() {
		return nil
	}
	rows, err := store.ReadDB().Query(
		"SELECT id FROM task_executions WHERE restart_interrupted = 1 ORDER BY id",
	)
	if err != nil {
		slog.Warn("task restart resume: could not scan interrupted executions",
			slog.String("err", err.Error()))
		return nil
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("task restart resume: scan was partial", slog.String("err", err.Error()))
	}
	return ids
}

// clearInterruptedExecutions drops the flags so a row is resumed at most once.
func clearInterruptedExecutions(ids []int64) {
	if len(ids) == 0 {
		return
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	if _, err := store.WriteExec(
		"UPDATE task_executions SET restart_interrupted = 0 WHERE id IN ("+placeholders+")", args...,
	); err != nil {
		slog.Warn("task restart resume: could not clear interrupted flags",
			slog.Int("executions", len(ids)),
			slog.String("err", err.Error()))
	}
}

// ResumeTaskExecution continues one interrupted execution through the global
// scheduler. Exported (and routed through GlobalScheduler) so the startup worker
// does not need to know how the scheduler is wired.
func ResumeTaskExecution(execID int64) error {
	if GlobalScheduler == nil {
		return nil
	}
	return GlobalScheduler.resumeExecution(execID)
}

// resumeTarget is an interrupted execution that passed every eligibility check
// and is ready to run again.
type resumeTarget struct {
	task        *model.ScheduledTask
	agent       *model.Agent
	sessionID   string
	triggerType string
}

// validateResumeTarget reads the row and answers "may this run continue?".
//
// Every rejection is a normal outcome (the user deleted the task, paused it, or
// removed the agent while the server was down), so it returns nil rather than an
// error and logs the reason once. It also consumes the restart marker: the
// interruption is accounted for at THIS startup whether or not the run resumes,
// which is what keeps a skipped row from being resurrected by a later restart.
func validateResumeTarget(execID int64) *resumeTarget {
	var (
		taskID      int64
		sessionID   string
		triggerType string
		flag        int
	)
	if err := store.ReadDB().QueryRow(
		"SELECT task_id, session_id, trigger_type, restart_interrupted FROM task_executions WHERE id = ?", execID,
	).Scan(&taskID, &sessionID, &triggerType, &flag); err != nil {
		return nil
	}
	if flag == 0 || sessionID == "" {
		return nil
	}
	clearInterruptedExecutions([]int64{execID})

	task, err := GetTaskByID(taskID)
	if err != nil {
		slog.Info("task restart resume: task no longer exists",
			slog.Int64("task_id", taskID), slog.Int64("execution", execID))
		return nil
	}
	if task.Status != taskStatusActive {
		// Paused, completed or deleted by the user while the server was down —
		// that decision outranks an automatic resume.
		slog.Info("task restart resume: task is not active",
			slog.Int64("task_id", task.ID), slog.String("status", task.Status))
		return nil
	}
	agent := model.GetAgent(task.AgentID)
	if agent == nil {
		slog.Info("task restart resume: agent not available",
			slog.Int64("task_id", task.ID), slog.String("agent_id", task.AgentID))
		return nil
	}
	// A session that was deleted (or archived) while the server was down has
	// nowhere to continue: resuming would stream into a row the user put away.
	var archived int
	if err := store.ReadDB().QueryRow(
		"SELECT archived FROM chat_sessions WHERE id = ?", sessionID,
	).Scan(&archived); err != nil || archived != 0 {
		slog.Info("task restart resume: session is gone or archived",
			slog.Int64("task_id", task.ID), slog.String("session_id", sessionID))
		return nil
	}
	return &resumeTarget{task: task, agent: agent, sessionID: sessionID, triggerType: triggerType}
}

// resumeExecution continues a run that a restart cut short.
//
// The run is NOT replayed: the original prompt is already in the timeline and
// may have changed files. The agent is told the previous attempt was
// interrupted and asked to check the current state and carry on — the same
// instruction the interactive resume and the abnormal-end retry use.
//
// Everything that belongs to the run rather than to one attempt is preserved:
// the session (so the agent keeps its context), the execution row (so the task
// list shows one run instead of two) and the trigger type.
func (s *Scheduler) resumeExecution(execID int64) error {
	target := validateResumeTarget(execID)
	if target == nil {
		return nil
	}
	task, agent, sessionID, triggerType := target.task, target.agent, target.sessionID, target.triggerType

	// Claim the task so a cron tick or a fresh event cannot start a second run
	// while this one continues. Losing the claim means the task is already
	// running — that run supersedes the interrupted one.
	if _, loaded := s.taskRunning.LoadOrStore(task.ID, struct{}{}); loaded {
		slog.Info("task restart resume: task is already running",
			slog.Int64("task_id", task.ID))
		return nil
	}
	defer s.taskRunning.Delete(task.ID)

	// Put the row back to "running" before the turn starts, so the task list and
	// the unread query see one run in progress rather than a cancelled one.
	if _, err := store.WriteExec(
		"UPDATE task_executions SET status = 'running' WHERE id = ?", execID,
	); err != nil {
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			slog.Error("task restart resume: panic during resumed run",
				slog.Int64("task_id", task.ID),
				slog.String("session_id", sessionID),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())))
			_ = UpdateExecutionStatus(sessionID, statusFailed)
			SetSessionRunning(sessionID, false, true)
		}
	}()

	backendName := agent.Backend
	if backendName == "" {
		backendName = "codebuddy"
	}
	// A scheduled run has nobody to approve permission requests; the session
	// already carries this flag, but re-asserting it costs one UPDATE and keeps
	// the resumed run from blocking on a prompt nobody will answer.
	if agent.Transport == transportACPStdio {
		if err := UpdateSessionAutoApprove(sessionID, true); err != nil {
			slog.Warn("task restart resume: failed to enable auto-approve",
				slog.String("session_id", sessionID), slog.String("err", err.Error()))
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// One row for the user ("the run was interrupted, continuing"), the
	// imperative instruction goes to the agent as the turn's prompt and is never
	// persisted — the same split restart_resume.go makes.
	notice := RestartResumeNotice()
	if msgID, err := AddSystemMessage(task.ProjectPath, sessionID, notice); err != nil {
		slog.Warn("task restart resume: failed to write the resume notice",
			slog.String("session_id", sessionID), slog.String("err", err.Error()))
	} else {
		emitGroupSystemMessage(sessionID, msgID, notice)
	}

	userModel := GetLatestUserModel(task.AgentID, task.ProjectPath)
	effectiveModel := userModel
	if effectiveModel == "" {
		effectiveModel = agent.DefaultModelID()
	}
	effectiveThinking := EffectiveThinkingEffortForModel(agent, effectiveModel, agent.Transport == transportACPStdio)

	req := BuildChatRequest(
		AutoContinuePrompt(), sessionID, task.ProjectPath, backendName, task.AgentID,
		effectiveModel, effectiveThinking, "", "", task.ProjectPath, false,
	)
	// BuildChatRequest is the interactive builder; a resumed task run is still a
	// scheduled execution (pi's --no-session and the prompt layers key on it).
	req.ScheduledExecution = true

	slog.Info("task restart resume: resuming execution",
		slog.Int64("task_id", task.ID),
		slog.Int64("execution", execID),
		slog.String("session_id", sessionID),
		slog.String("name", task.Name))

	s.runTaskTurns(ctx, taskTurnSpec{
		Task:        task,
		ProjectPath: task.ProjectPath,
		BackendName: backendName,
		SessionID:   sessionID,
		ExecutionID: execID,
		// The run keeps the trigger it started with: the continuation is the same
		// execution, and the timeline notice already says why it was resumed.
		TriggerType: triggerType,
		ChatReq:     req,
		// The interrupted attempt already counted as a run (UpdateTaskStats on
		// the cancel path); this turn finishes that same run.
		CountRun: false,
	})
	return nil
}
