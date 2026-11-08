package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/store"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingTaskBackend answers every turn immediately and remembers the prompt,
// so a test can assert WHAT the resumed run was asked to do.
type recordingTaskBackend struct {
	name   string
	mu     sync.Mutex
	prompt string
}

func (b *recordingTaskBackend) Name() string { return b.name }

// resumeBackendSeq keeps every test's backend name unique: RegisterBackend
// panics on a duplicate name, and these tests all live in one process.
var resumeBackendSeq atomic.Int64

func newResumeBackend() *recordingTaskBackend {
	name := fmt.Sprintf("test-task-resume-%d", resumeBackendSeq.Add(1))
	b := &recordingTaskBackend{name: name}
	ai.RegisterBackend(name, func() ai.AIBackend { return b })
	return b
}

func (b *recordingTaskBackend) ExecuteStream(_ context.Context, req ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	b.mu.Lock()
	b.prompt = req.Prompt
	b.mu.Unlock()
	ch := make(chan ai.StreamEvent, 4)
	ch <- ai.StreamEvent{Type: "content", Content: "continued"}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)
	return ch, nil
}

func (b *recordingTaskBackend) lastPrompt() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.prompt
}

// seedInterruptedExecution builds the state a restart leaves behind: a task, the
// session it was running in (with history, so the agent resumes it), and an
// execution row that the shutdown marked as interrupted.
func seedInterruptedExecution(t *testing.T, backend *recordingTaskBackend, taskStatus string) (*Scheduler, *model.ScheduledTask, string, int64) {
	t.Helper()
	setupSchedulerExecDB(t)

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:                  "test-agent",
			Name:                "Test Agent",
			Backend:             backend.Name(),
			RuntimeSystemPrompt: "test prompt",
		},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	origMgr := ws.GetManager()
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(origMgr) })

	s := NewScheduler()
	t.Cleanup(s.Stop)

	const projectPath = "/tmp/task-restart-resume"
	task := &model.ScheduledTask{
		ProjectPath: projectPath,
		Name:        "Resume Task",
		CronExpr:    "0 * * * *",
		AgentID:     "test-agent",
		Prompt:      "do the thing",
		Status:      "active",
		RepeatMode:  "unlimited",
	}
	require.NoError(t, s.AddTask(task))

	sessionID, err := CreateSessionWithLockedTitle(projectPath, backend.Name(), "⏰ "+task.Name, "test-agent", "", "default", "scheduled")
	require.NoError(t, err)
	_, err = AddChatMessage(projectPath, backend.Name(), sessionID, "user", "do the thing", nil, false, "")
	require.NoError(t, err)
	_, err = AddChatMessage(projectPath, backend.Name(), sessionID, "assistant",
		`{"blocks":[{"type":"text","text":"partial work","done":true}]}`, nil, false, "")
	require.NoError(t, err)

	execID, err := AddTaskExecution(task.ID, sessionID, "event")
	require.NoError(t, err)
	// What the graceful shutdown leaves: cancelled + the exact restart marker.
	_, err = store.WriteExec(
		"UPDATE task_executions SET status = 'cancelled', restart_interrupted = 1 WHERE id = ?", execID,
	)
	require.NoError(t, err)
	// The cancel path already counted the run (UpdateTaskStats).
	_, err = store.WriteExec("UPDATE scheduled_tasks SET run_count = 1, status = ? WHERE id = ?", taskStatus, task.ID)
	require.NoError(t, err)
	task.Status = taskStatus

	return s, task, sessionID, execID
}

func executionRow(t *testing.T, execID int64) (status string, flag int) {
	t.Helper()
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT status, restart_interrupted FROM task_executions WHERE id = ?", execID,
	).Scan(&status, &flag))
	return status, flag
}

// TestResumeExecution_ContinuesTheSameRun is the behavior the feature exists
// for: the interrupted execution continues in the SAME session (so the agent
// keeps its context), on the SAME row (so the task list shows one run), with
// the "carry on from where you stopped" instruction rather than the original
// prompt replayed.
func TestResumeExecution_ContinuesTheSameRun(t *testing.T) {
	backend := newResumeBackend()
	s, task, sessionID, execID := seedInterruptedExecution(t, backend, "active")

	require.NoError(t, s.resumeExecution(execID))

	status, flag := executionRow(t, execID)
	assert.Equal(t, "completed", status, "the resumed run finishes the original execution row")
	assert.Equal(t, 0, flag, "the restart marker must be consumed")

	var sessions int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE session_type = 'scheduled'",
	).Scan(&sessions))
	assert.Equal(t, 1, sessions, "the run must continue the existing session, not open a new one")

	var executions int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM task_executions WHERE task_id = ?", task.ID,
	).Scan(&executions))
	assert.Equal(t, 1, executions, "the continuation must reuse the execution row, not add a second run")

	assert.Equal(t, AutoContinuePrompt(), backend.lastPrompt(),
		"the agent is asked to continue, not to redo the original prompt")

	var runCount int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT run_count FROM scheduled_tasks WHERE id = ?", task.ID,
	).Scan(&runCount))
	assert.Equal(t, 1, runCount, "finishing the same run must not count a second run")

	var noticeRows int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'system'", sessionID,
	).Scan(&noticeRows))
	assert.Equal(t, 1, noticeRows, "the timeline gets exactly one recovery notice")

	assert.False(t, s.HasRunningExecutions(task.ID), "the claim must be released")
}

// TestResumeExecution_SkipsUnusableTargets covers the remaining ways a row can
// stop being resumable while the server was down. None of them is an error —
// they are decisions the user already made, and the marker is consumed either
// way so a later restart does not try again.
func TestResumeExecution_SkipsUnusableTargets(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, s *Scheduler, task *model.ScheduledTask, sessionID string)
	}{
		{
			name: "task deleted",
			mutate: func(t *testing.T, _ *Scheduler, task *model.ScheduledTask, _ string) {
				_, err := store.WriteExec("DELETE FROM scheduled_tasks WHERE id = ?", task.ID)
				require.NoError(t, err)
			},
		},
		{
			name: "agent removed",
			mutate: func(t *testing.T, _ *Scheduler, _ *model.ScheduledTask, _ string) {
				model.Agents = map[string]*model.Agent{}
			},
		},
		{
			name: "session archived",
			mutate: func(t *testing.T, _ *Scheduler, _ *model.ScheduledTask, sessionID string) {
				_, err := store.WriteExec("UPDATE chat_sessions SET archived = 1 WHERE id = ?", sessionID)
				require.NoError(t, err)
			},
		},
		{
			name: "task already running",
			mutate: func(t *testing.T, s *Scheduler, task *model.ScheduledTask, _ string) {
				s.taskRunning.Store(task.ID, struct{}{})
				t.Cleanup(func() { s.taskRunning.Delete(task.ID) })
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := newResumeBackend()
			s, task, sessionID, execID := seedInterruptedExecution(t, backend, "active")
			tc.mutate(t, s, task, sessionID)

			require.NoError(t, s.resumeExecution(execID))

			status, flag := executionRow(t, execID)
			assert.Equal(t, "cancelled", status, "a skipped resume leaves the row untouched")
			assert.Equal(t, 0, flag, "the marker is consumed even when nothing runs")
			assert.Empty(t, backend.lastPrompt(), "no turn may run for an unusable target")
		})
	}
}

// TestResumeTaskExecution_RoutesThroughGlobalScheduler covers the wiring main()
// uses: the startup worker must reach the live scheduler, and a missing one must
// be a no-op rather than a panic.
func TestResumeTaskExecution_RoutesThroughGlobalScheduler(t *testing.T) {
	prev := GlobalScheduler
	t.Cleanup(func() { GlobalScheduler = prev })

	GlobalScheduler = nil
	assert.NoError(t, ResumeTaskExecution(1))

	backend := newResumeBackend()
	s, _, _, execID := seedInterruptedExecution(t, backend, "active")
	GlobalScheduler = s

	require.NoError(t, ResumeTaskExecution(execID))
	status, _ := executionRow(t, execID)
	assert.Equal(t, "completed", status, "the row must be resumed through the global scheduler")
}

// TestResumeExecution_SkipsInactiveTask: a task the user paused (or that
// finished) while the server was down must not be resurrected.
func TestResumeExecution_SkipsInactiveTask(t *testing.T) {
	backend := newResumeBackend()
	s, task, _, execID := seedInterruptedExecution(t, backend, "paused")

	require.NoError(t, s.resumeExecution(execID))

	status, flag := executionRow(t, execID)
	assert.Equal(t, "cancelled", status, "a skipped resume leaves the row as it was")
	assert.Equal(t, 0, flag, "the marker is consumed even when the resume is skipped")
	assert.Empty(t, backend.lastPrompt(), "no turn may run for an inactive task")
	assert.False(t, s.HasRunningExecutions(task.ID))
}

// TestResumeExecution_IgnoresClearedFlag: only rows stamped by the shutdown are
// eligible. A user cancel or an ordinary failure must never be resumed.
func TestResumeExecution_IgnoresClearedFlag(t *testing.T) {
	backend := newResumeBackend()
	s, _, _, execID := seedInterruptedExecution(t, backend, "active")
	_, err := store.WriteExec("UPDATE task_executions SET restart_interrupted = 0 WHERE id = ?", execID)
	require.NoError(t, err)

	require.NoError(t, s.resumeExecution(execID))

	status, _ := executionRow(t, execID)
	assert.Equal(t, "cancelled", status)
	assert.Empty(t, backend.lastPrompt(), "a run without the restart marker is not resumed")
}

// TestStartTaskRestartResume_DisabledClearsFlags: the switch is the user's
// opt-in for spending tokens unattended, and turning it on later must not
// resurrect a restart that already happened.
func TestStartTaskRestartResume_DisabledClearsFlags(t *testing.T) {
	prev := model.ChatAutoContinueEnabled
	model.ChatAutoContinueEnabled = false
	t.Cleanup(func() { model.ChatAutoContinueEnabled = prev })

	_, _, _, execID := seedInterruptedExecution(t, newResumeBackend(), "active")

	ids := startTaskRestartResume(0, func(int64) error {
		t.Error("no execution may be resumed while the switch is off")
		return nil
	})
	assert.Nil(t, ids)

	_, flag := executionRow(t, execID)
	assert.Equal(t, 0, flag, "flags are consumed even when the feature is off")
}

// TestStartTaskRestartResume_SchedulesTheRow: with the switch on, the row is
// handed to the resume function exactly once.
func TestStartTaskRestartResume_SchedulesTheRow(t *testing.T) {
	prev := model.ChatAutoContinueEnabled
	model.ChatAutoContinueEnabled = true
	t.Cleanup(func() { model.ChatAutoContinueEnabled = prev })

	_, _, _, execID := seedInterruptedExecution(t, newResumeBackend(), "active")

	resumed := make(chan int64, 4)
	ids := startTaskRestartResume(0, func(id int64) error {
		resumed <- id
		return nil
	})
	require.Equal(t, []int64{execID}, ids)

	select {
	case got := <-resumed:
		assert.Equal(t, execID, got)
	case <-time.After(2 * time.Second):
		t.Fatal("the interrupted execution was never resumed")
	}
}

// TestCancelAllRunning_MarksAIExecutions: the marker has to be written by the
// shutdown itself — that is what makes the signal exact instead of a guess about
// which cancelled rows "look interrupted".
func TestCancelAllRunning_MarksAIExecutions(t *testing.T) {
	setupSchedulerExecDB(t)
	s := NewScheduler()
	t.Cleanup(s.Stop)

	const sessionID = "sess-shutdown-marker"
	execID, err := AddTaskExecution(1, sessionID, "auto")
	require.NoError(t, err)

	s.AddRunningExecution(&RunningExecution{
		ID: sessionID, TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(),
		TriggerType: "auto", Phase: RunningPhaseAI,
	})
	s.CancelAllRunning()

	_, flag := executionRow(t, execID)
	assert.Equal(t, 1, flag, "a graceful shutdown must mark its AI executions")
}

// TestStartTaskRestartResume_UsesProductionDefaults pins the exported entry
// point to the parameterized implementation.
//
// StartTaskRestartResume is what main calls at boot, and it is the only place
// the production delay and resume function are chosen. Every other case drives
// startTaskRestartResume directly, so a swap of those two arguments (or an
// accidental early return) would leave the whole suite green while startup
// resumed nothing.
func TestStartTaskRestartResume_UsesProductionDefaults(t *testing.T) {
	prev := model.ChatAutoContinueEnabled
	model.ChatAutoContinueEnabled = false // keeps the goroutine out of this test
	t.Cleanup(func() { model.ChatAutoContinueEnabled = prev })

	_, _, _, execID := seedInterruptedExecution(t, newResumeBackend(), "active")

	// The switch is off, so the production call must still find and consume the
	// row — that is the only observable side effect available without letting a
	// real resume run.
	ids := StartTaskRestartResume()
	assert.Nil(t, ids, "auto-continue is off, so nothing is scheduled")

	_, flag := executionRow(t, execID)
	assert.Equal(t, 0, flag, "the production entry point must reach interruptedExecutionIDs")
}

// A shutdown that lands during the delay must stop the remaining executions:
// resuming one after the DB starts closing leaves a fresh streaming row that
// the next boot resumes again.
func TestStartTaskRestartResume_AbortsOnShutdown(t *testing.T) {
	prev := model.ChatAutoContinueEnabled
	model.ChatAutoContinueEnabled = true
	t.Cleanup(func() { model.ChatAutoContinueEnabled = prev })

	prevShutdown := ResetShuttingDownForTest()
	if prevShutdown {
		t.Fatal("setup: the package must not already be marked as shutting down")
	}
	t.Cleanup(func() { ResetShuttingDownForTest() })

	_, _, _, execID := seedInterruptedExecution(t, newResumeBackend(), "active")

	resumed := make(chan int64, 4)
	ids := startTaskRestartResume(120*time.Millisecond, func(id int64) error {
		resumed <- id
		return nil
	})
	require.Equal(t, []int64{execID}, ids)
	MarkShuttingDown()

	select {
	case got := <-resumed:
		t.Fatalf("execution %d must not resume after shutdown began", got)
	case <-time.After(400 * time.Millisecond):
	}
}

// A panicking resume must not take the process down: this runs in a bare
// goroutine seconds after startup, against historical rows.
func TestResumeExecutionSafely_SwallowsPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		resumeExecutionSafely(42, func(int64) error {
			panic("boom")
		})
	})
}

// A resume that returns an error is logged, not propagated: the loop must keep
// going with the remaining executions.
func TestResumeExecutionSafely_ContinuesPastAnError(t *testing.T) {
	assert.NotPanics(t, func() {
		resumeExecutionSafely(42, func(int64) error { return errors.New("nope") })
	})
}

// interruptedExecutionIDs is a no-op before the DB is up (early startup paths
// call the resume chain unconditionally).
func TestInterruptedExecutionIDs_WithoutDB(t *testing.T) {
	// A nil write handle is exactly the "not opened yet" state the guard checks.
	t.Cleanup(store.SetDBForTest(nil, nil))

	assert.Nil(t, interruptedExecutionIDs())
}
