package service

import (
	"context"
	"fmt"
	"log/slog"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// taskTurnSpec is everything one execution's turn loop needs once its session,
// execution row and first prompt have been prepared.
//
// It exists because a run resumed after a server restart must go through the
// EXACT same loop as a fresh run: retries, the cancel branch, the terminal
// bookkeeping and the completion notification are all part of "what a task run
// is". The two callers differ only in that preparation, never here.
type taskTurnSpec struct {
	Task        *model.ScheduledTask
	ProjectPath string
	BackendName string
	SessionID   string
	ExecutionID int64
	TriggerType string
	// ChatReq is the FIRST turn's request. Retries rebuild their own from the
	// session, exactly as they did before this split.
	ChatReq ai.ChatRequest
	// CountRun is false for a run resumed after a restart: the interrupted
	// attempt already incremented run_count when it was cancelled, and this turn
	// finishes that same run instead of starting a new one. Counting again would
	// make a "limited" task exhaust its budget one run early.
	CountRun bool
}

// runTaskTurns runs the AI turn(s) of one execution and writes the outcome.
//
// The local aliases below are deliberate: this body was moved verbatim out of
// executeTask, and keeping the original names is what makes the split a pure
// move (no behavior change) instead of a rewrite.
func (s *Scheduler) runTaskTurns(ctx context.Context, spec taskTurnSpec) {
	task := spec.Task
	projectPath := spec.ProjectPath
	backendName := spec.BackendName
	sessionID := spec.SessionID
	executionID := spec.ExecutionID
	triggerType := spec.TriggerType
	chatReq := spec.ChatReq
	effectiveModel := spec.ChatReq.Model
	effectiveThinking := spec.ChatReq.ThinkingEffort

	// Run the turn through the shared implementation. The scheduler emits its
	// own stream_start (its subscribers assert on the exact id), and it handles
	// the abort/terminal branches below itself because a task failure must also
	// update the execution row and emit task events — behavior the interactive
	// paths do not have.
	//
	// The turn is repeated when it ends abnormally (agent crash, empty reply,
	// backend error) and the user enabled auto-continue. The task prompt has
	// already been consumed by then, so a retry sends a localized "continue"
	// instead of re-running the task from scratch.
	//
	// Everything that belongs to the EXECUTION rather than to one attempt — the
	// session, the execution row, the registered cancel context and the
	// "running" event — is set up once above. Only the turn itself repeats, and
	// the execution row stays "running" across attempts: marking it failed
	// mid-retry would count the run as finished in the unread query and push a
	// notification for a task that is still going.
	// finalRunResult carries the last attempt's finalized result out of the
	// loop so the post-completion summarization below still has the assistant
	// message id and blocks (it used to read the single turn's result).
	var finalRunResult RunResult

	attempt := 0
	for {
		attempt++

		// Retries resume the existing CLI/ACP session instead of starting a
		// new one. The first attempt already persisted an assistant row, so
		// BuildChatRequest resolves resume=true and the agent keeps the context
		// it had built before it died. (For pi, `Resume && SessionID != ""`
		// outranks ScheduledExecution, so --no-session cannot discard it.)
		turnReq := chatReq
		if attempt > 1 {
			turnReq = BuildChatRequest(AutoContinuePrompt(), sessionID, projectPath, backendName,
				task.AgentID, effectiveModel, effectiveThinking, "", "", projectPath, false)
		}

		// The "running" event rides OnStarted rather than being emitted after this
		// call: runTurnStart's last step is the blocking event loop, so emitting it
		// afterwards sent "running" once the task had already finished (subscribers
		// saw started → completed with nothing in between). OnStarted fires after
		// the backend and placeholder exist but before the loop, so the event is
		// both truthful and correctly ordered. It also preserves ISS-128: a turn
		// that fails before starting never reaches the hook, so no "running" event
		// is emitted for it — only "failed".
		at := runTurnStart(TurnSpec{
			Ctx:         ctx,
			Mode:        ModeScheduled,
			ProjectPath: projectPath,
			BackendName: backendName,
			SessionID:   sessionID,
			AgentID:     task.AgentID,
			ChatReq:     turnReq,
			FileDir:     projectPath,
			TaskID:      task.ID,
			ExecutionID: executionID,
			TriggerType: triggerType,
			OnStarted: func() {
				// Only the first attempt announces "running". A retry is a
				// continuation of the same execution, so re-emitting would make
				// the task list flicker back into a state it never left.
				if attempt == 1 {
					emitTaskEvent(fmt.Sprintf("%d", task.ID), "running", fmt.Sprintf("%d", executionID), sessionID, projectPath, task.Name)
				}
			},
		})

		// Backend creation / stream start failed. ISS-128: no "running" event may
		// be emitted for a task that fails before it starts, or the frontend shows a
		// running state that immediately fails. So the failure branch comes first
		// and emits only "failed". (OnStarted never ran in this case.)
		//
		// Deliberately never retried: these failures are deterministic (missing
		// binary, unusable working directory), so retrying could only spin.
		if !at.started() {
			at.release()
			slog.Error("failed to start task execution", slog.String("err", at.earlyFails.Err))
			_ = UpdateExecutionStatus(sessionID, statusFailed)
			SetSessionRunning(sessionID, false, true)
			ws.EmitToSession(sessionID, ai.StreamEvent{Type: "error", Error: "task execution failed"})
			emitTaskEvent(fmt.Sprintf("%d", task.ID), statusFailed, fmt.Sprintf("%d", executionID), sessionID, projectPath, task.Name)
			return
		}

		executor := at.executor
		eventCh := at.eventCh
		runResult := at.runResult

		// If context was cancelled, mark execution as cancelled and update stats
		if ctx.Err() == context.Canceled {
			at.release()
			slog.Info(
				"task execution cancelled",
				slog.Int64("task_id", task.ID),
				slog.String("session_id", sessionID),
			)
			_ = UpdateExecutionStatus(sessionID, statusCancelled)
			s.runningExecutions.Delete(sessionID)
			SetSessionRunning(sessionID, false, true)
			// Stop the client's stream FIRST: this event is what closes the
			// frontend's streaming bubble, and it must not wait behind the DB
			// work below (a busy write can take up to the busy_timeout).
			// Same wire value as statusCancelled; the client keys the closed
			// bubble on it either way.
			ws.EmitToSession(sessionID, ai.StreamEvent{Type: statusCancelled})
			// Persist any batched tool-call/context-state writes accumulated since
			// the last flush window, so the cancelled task's final content survives,
			// then finalize the orphaned streaming row since executor.Finalize() was
			// skipped. cancelReason="user": this is a user-initiated/graceful cancel
			// — suppress the misleading "Finalization failed, AI response may be
			// incomplete" warning block (the stream is truncated by intent, not by
			// a DB failure).
			//
			// This MUST run before emitTaskEvent. That event's response preview and
			// message id are read back out of the assistant row, and the row only
			// carries `cancelled:true` (and stops being a streaming row, which the
			// preview query filters out) once this has stamped it. Emitting first
			// made the notification describe the interrupted reply as if it had
			// finished normally.
			abandonTurn(executor, eventCh, sessionID, cancelReasonUser)
			emitTaskEvent(fmt.Sprintf("%d", task.ID), statusCancelled, fmt.Sprintf("%d", executionID), sessionID, projectPath, task.Name)
			// Only update stats, not status — don't overwrite user-initiated pauses (ISS-013)
			UpdateTaskStats(task)
			return
		}

		// The attempt is over. Persist what it produced, then classify the
		// outcome to decide whether to resume.
		var abnormal string
		if runResult.ReceivedTerminal {
			// Finalize persists blocks to DB, saves metadata and drains remaining
			// events — shared with the interactive paths so the persisted shape
			// cannot drift.
			abnormal = at.runTurnFinalize().AbnormalReason
			// at.runResult was updated by Finalize, so it carries the persisted
			// MsgID and blocks the completion path needs.
			finalRunResult = at.runResult
		} else {
			// If the event channel closed without a terminal event (done/error),
			// the CLI process likely crashed or was killed (e.g. SIGKILL, OOM).
			// Finalize is skipped so the partial content is persisted and the
			// orphaned row finalized by hand, matching the historical behavior.
			slog.Warn(
				"task execution ended without terminal event (CLI process crashed?)",
				slog.Int64("task_id", task.ID),
				slog.String("session_id", sessionID),
			)
			abandonTurn(executor, eventCh, sessionID, "")
			abnormal = classifyTurnAbnormality(runResult.CancelReason, runResult.ReceivedTerminal, runResult.Empty, runResult.Blocks)
		}
		at.release()

		// Clean finish — fall through to the completion bookkeeping below.
		if abnormal == "" {
			break
		}

		// Abnormal end. Resume it when the user opted in and the budget allows.
		// attempt-1 is the number of RETRIES already spent: attempt counts turns
		// (the first turn is attempt 1 and is not a retry), while max_retries
		// counts resumes. Passing attempt here would spend one retry too few.
		if AutoContinueEnabled() && AutoContinueAttemptsAllowed(attempt-1, model.ChatAutoContinueMaxRetries) {
			if autoContinueSleep(ctx, autoContinueDelay) && ctx.Err() == nil {
				// Close the failed turn's bubble before resuming, mirroring the
				// interactive runner. Without this per-turn boundary the live
				// preview's streaming bubble keeps its spinner until the whole
				// task's terminal `done`. at.msgID is the placeholder row the
				// finalize just wrote to (0 when the turn never opened one).
				emitTurnFinalize(sessionID, at.msgID)
				if _, err := PrepareAutoContinueNotice(sessionID, projectPath); err != nil {
					slog.Error("task auto-continue: failed to persist continue notice",
						slog.Int64("task_id", task.ID),
						slog.String("session_id", sessionID),
						slog.String("err", err.Error()))
				} else {
					slog.Info("task auto-continue: resuming abnormally terminated execution",
						slog.Int64("task_id", task.ID),
						slog.String("session_id", sessionID),
						slog.Int("attempt", attempt),
						slog.String("reason", abnormal))
					continue
				}
			} else {
				slog.Info("task auto-continue: cancelled during delay",
					slog.Int64("task_id", task.ID),
					slog.String("session_id", sessionID),
					slog.Int("attempt", attempt))
			}
		}

		// No retry available — report the failure.
		_ = UpdateExecutionStatus(sessionID, statusFailed)
		s.runningExecutions.Delete(sessionID)
		SetSessionRunning(sessionID, false, true)
		ws.EmitToSession(sessionID, ai.StreamEvent{Type: "error", Error: "task execution ended unexpectedly"})
		emitTaskEvent(fmt.Sprintf("%d", task.ID), statusFailed, fmt.Sprintf("%d", executionID), sessionID, projectPath, task.Name)
		// Only update stats, not status — don't overwrite user-initiated pauses (ISS-013)
		UpdateTaskStats(task)
		return
	}

	// Mark execution as completed
	_ = UpdateExecutionStatus(sessionID, statusCompleted)

	// Remove from runningExecutions BEFORE emitting task_update event.
	// Without this, the frontend's loadTasks() (triggered by the WS event)
	// sees runningCount > 0 via GetRunningCounts(), causing the task list
	// to show "running" even though the execution is already completed.
	s.runningExecutions.Delete(sessionID)
	// defer also calls Delete — that's harmless (double-delete is a no-op)
	// and serves as a safety net for error/cancel paths.

	// Send terminal "done" event to WS chat_stream subscribers so that
	// useTaskExecStream (live preview) stops streaming. Without this,
	// the frontend stays in streaming state indefinitely.
	SetSessionRunning(sessionID, false, true) // skip event — we emit directly
	ws.EmitToSession(sessionID, ai.StreamEvent{Type: "done"})

	emitTaskEvent(fmt.Sprintf("%d", task.ID), statusCompleted, fmt.Sprintf("%d", executionID), sessionID, projectPath, task.Name)

	// Update task execution stats
	// Read current DB status to avoid overwriting user-initiated changes (e.g. pause).
	// See ISS-013: using task.Status (in-memory snapshot) can revert "paused" back to "active".
	newStatus := s.advanceTaskAfterRun(task, spec.CountRun)
	if task.IsEventTriggered() {
		slog.Info("event task execution completed",
			slog.Int64("task_id", task.ID),
			slog.String("session_id", sessionID))
		return
	}

	slog.Info(
		"task execution completed",
		slog.Int64("task_id", task.ID),
		slog.String("session_id", sessionID),
		slog.String("status", newStatus),
	)

	// Generate summary asynchronously — use the shared summarizeMessage so that
	// tasks follow the exact same strategy as interactive chat
	// (respecting chatSummaryMode, AI with simple fallback). Keyed by the
	// assistant message ID (runResult.MsgID), same as interactive chat sessions.
	// This unifies the summary storage model so ContinueFromExecution no longer
	// needs to convert between target_types.
	if finalRunResult.MsgID > 0 {
		_ = summarizeMessage(finalRunResult.MsgID, finalRunResult.Blocks, task.ProjectPath, sessionID)
	}
}
