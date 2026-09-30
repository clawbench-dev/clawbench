package service

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schedulerScriptSchema is the schema needed for the script-phase executeTask
// tests. It mirrors schedulerExecSchema but is declared here so the tests can
// evolve independently.
const schedulerScriptSchema = ProjectsDDL + `
CREATE TABLE IF NOT EXISTS chat_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id INTEGER NOT NULL,
	role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
	content TEXT NOT NULL,
	files TEXT,
	session_id TEXT,
	backend TEXT NOT NULL DEFAULT 'claude',
	streaming INTEGER NOT NULL DEFAULT 0,
	indexed INTEGER NOT NULL DEFAULT 0,
	queue_id TEXT DEFAULT '',
	queued INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	completed_at DATETIME
);
CREATE TABLE IF NOT EXISTS chat_sessions (
	id TEXT PRIMARY KEY,
	project_id INTEGER NOT NULL,
	backend TEXT NOT NULL,
	title TEXT NOT NULL,
	agent_id TEXT DEFAULT '',
	agent_source TEXT DEFAULT 'default',
	model TEXT DEFAULT '',
	session_type TEXT NOT NULL DEFAULT 'chat',
	external_session_id TEXT DEFAULT '',
	transport TEXT DEFAULT '',
	title_renamed INTEGER NOT NULL DEFAULT 0,
	title_source TEXT NOT NULL DEFAULT '',
	compacted INTEGER NOT NULL DEFAULT 0,
	archived INTEGER NOT NULL DEFAULT 0,
	context_state TEXT DEFAULT '',
	last_read_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(backend, id)
);
CREATE TABLE IF NOT EXISTS scheduled_tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id INTEGER NOT NULL,
	name TEXT NOT NULL,
	cron_expr TEXT NOT NULL,
	agent_id TEXT NOT NULL,
	prompt TEXT NOT NULL,
	script TEXT NOT NULL DEFAULT '',
	script_timeout INTEGER NOT NULL DEFAULT 0,
	session_id TEXT,
	trigger_mode TEXT NOT NULL DEFAULT 'cron',
	event_types TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'active',
	repeat_mode TEXT NOT NULL DEFAULT 'unlimited',
	max_runs INTEGER DEFAULT 0,
	last_run_at DATETIME,
	next_run_at DATETIME,
	run_count INTEGER DEFAULT 0,
	last_read_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS task_executions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	trigger_type TEXT NOT NULL DEFAULT 'auto',
	status TEXT NOT NULL DEFAULT 'running',
	read_at DATETIME,
	summary TEXT,
	event_url TEXT NOT NULL DEFAULT '',
	event_summary TEXT NOT NULL DEFAULT '',
	script_exit_code INTEGER,
	script_outcome TEXT NOT NULL DEFAULT '',
	script_stdout TEXT NOT NULL DEFAULT '',
	script_stderr TEXT NOT NULL DEFAULT '',
	script_duration_ms INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_executions_task ON task_executions(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_id, backend, session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_id, backend);
CREATE INDEX IF NOT EXISTS idx_executions_session ON task_executions(session_id);
`

func setupSchedulerScriptDB(t *testing.T) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(schedulerScriptSchema)
	require.NoError(t, err)
	cleanup := SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
}

// mockScriptPhaseBackend returns a short stream once its gate closes. Used to
// keep the AI phase running long enough to observe the runningExecutions state.
type mockScriptPhaseBackend struct {
	gate chan struct{}
}

func (m *mockScriptPhaseBackend) Name() string { return "test-script-phase" }
func (m *mockScriptPhaseBackend) ExecuteStream(_ context.Context, _ ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	<-m.gate
	ch := make(chan ai.StreamEvent, 4)
	ch <- ai.StreamEvent{Type: "content", Content: "ai done"}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)
	return ch, nil
}

// registerGatedBackend registers a gated mock backend under a unique id (each
// test must not re-register an existing id — RegisterBackend panics) and
// returns the gate that unblocks the AI stream.
func registerGatedBackend(t *testing.T, id string) chan struct{} {
	t.Helper()
	gate := make(chan struct{})
	ai.RegisterBackend(id, func() ai.AIBackend { return &mockScriptPhaseBackend{gate: gate} })
	return gate
}

// registerScriptAgent installs an agent backed by the given backend id.
func registerScriptAgent(t *testing.T, backend string) {
	t.Helper()
	orig := model.Agents
	model.Agents = map[string]*model.Agent{
		"script-agent": {
			ID:                  "script-agent",
			Name:                "Script Agent",
			Backend:             backend,
			RuntimeSystemPrompt: "sys",
		},
	}
	t.Cleanup(func() { model.Agents = orig })
}

// TestExecuteTask_ScriptGateClosed_NoSessionNoEvent is the core gate guard: a
// script that exits non-zero must not create a session, must not call the AI,
// must record a `skipped` execution with an empty session_id, must persist the
// script result, must advance run_count/next_run_at, and must emit nothing.
func TestExecuteTask_ScriptGateClosed_NoSessionNoEvent(t *testing.T) {
	skipOnWindows(t)
	setupSchedulerScriptDB(t)
	// No AI backend registered: the closed-gate path must not need one.

	origAgents := model.Agents
	// The agent must exist (executeTask looks it up before the script phase),
	// but its backend is never reached.
	model.Agents = map[string]*model.Agent{
		"script-agent": {ID: "script-agent", Name: "Script Agent", Backend: "nonexistent-backend-xyz"},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(nil) })

	s := NewScheduler()
	t.Cleanup(s.Stop)

	// The script runs with the project path as its working directory, so it
	// must exist for the shell to chdir into it.
	projectPath := t.TempDir()

	task := &model.ScheduledTask{
		ProjectPath: projectPath,
		Name:        "Gated Task",
		CronExpr:    "0 * * * *",
		AgentID:     "script-agent",
		Prompt:      "do work",
		RepeatMode:  "unlimited",
		Script:      "echo 'nothing changed'; exit 1", // non-zero closes the gate
		Status:      "active",
	}
	require.NoError(t, s.AddTask(task))

	// Simulate the cron having just fired: rewind next_run_at into the past so
	// "advanced" is observable regardless of where in the hour the test runs.
	firedAt := time.Now().Add(-time.Minute)
	_, err := WriteExec("UPDATE scheduled_tasks SET next_run_at = ? WHERE id = ?", firedAt, task.ID)
	require.NoError(t, err)

	before, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	require.NotNil(t, before.NextRunAt, "a cron task must have a next_run_at after AddTask")

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "script-gate-client", "")

	s.executeTask(task, task.ProjectPath, "auto", nil)

	// No session was created.
	var sessionCount int
	require.NoError(t, dbRead.QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE project_id = ?", ProjectIDForTest(t, task.ProjectPath)).Scan(&sessionCount))
	assert.Zero(t, sessionCount, "a gate-closed run must not create a chat session")

	// No chat message was written.
	var msgCount int
	require.NoError(t, dbRead.QueryRow("SELECT COUNT(*) FROM chat_history").Scan(&msgCount))
	assert.Zero(t, msgCount, "a gate-closed run must not write any chat message")

	// An execution row with status skipped and an empty session_id exists, and
	// carries the script's result so the UI can show why the gate closed.
	var status, sessionID, scriptStdout string
	var scriptExit int
	require.NoError(t, dbRead.QueryRow(
		"SELECT status, session_id, script_exit_code, script_stdout FROM task_executions WHERE task_id = ? ORDER BY id DESC LIMIT 1",
		task.ID).Scan(&status, &sessionID, &scriptExit, &scriptStdout))
	assert.Equal(t, "skipped", status)
	assert.Empty(t, sessionID, "a gate-closed run has no session")
	assert.Equal(t, 1, scriptExit, "the script exit code must be persisted")
	assert.Contains(t, scriptStdout, "nothing changed", "the script output must be persisted")

	// run_count incremented and next_run_at advanced.
	after, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	assert.Equal(t, before.RunCount+1, after.RunCount, "run_count must be incremented by a gate-closed run")
	require.NotNil(t, after.NextRunAt)
	assert.True(t, after.NextRunAt.After(*before.NextRunAt),
		"next_run_at must advance (before=%v after=%v)", before.NextRunAt, after.NextRunAt)

	// No task_update event was emitted.
	assert.Empty(t, sub.GetBufferedEvents(), "a gate-closed run must emit no task_update event")

	// runningCount stays 0 for the task.
	counts := s.GetRunningCounts()
	assert.Equal(t, 0, counts[task.ID], "a script-phase run must not count as running")
}

// TestExecuteTask_ScriptGateOpen_RunsAI is the converse guard: exit 0 opens
// the gate and the AI turn runs, with the script result persisted on the
// AI-phase execution row.
func TestExecuteTask_ScriptGateOpen_RunsAI(t *testing.T) {
	skipOnWindows(t)
	setupSchedulerScriptDB(t)

	gate := registerGatedBackend(t, "test-script-gate-open")
	registerScriptAgent(t, "test-script-gate-open")

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(nil) })

	s := NewScheduler()
	t.Cleanup(s.Stop)

	task := &model.ScheduledTask{
		ProjectPath: t.TempDir(),
		Name:        "Open Gate",
		CronExpr:    "0 * * * *",
		AgentID:     "script-agent",
		Prompt:      "run the job",
		RepeatMode:  "unlimited",
		Script:      "echo changed",
		Status:      "active",
	}
	require.NoError(t, s.AddTask(task))

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.executeTask(task, task.ProjectPath, "auto", nil)
	}()

	// The AI turn must have started (a session exists) while it blocks on the gate.
	require.Eventually(t, func() bool {
		var n int
		_ = dbRead.QueryRow("SELECT COUNT(*) FROM chat_sessions").Scan(&n)
		return n > 0
	}, 2*time.Second, 10*time.Millisecond, "exit 0 must open the gate and create a session")

	close(gate)
	<-done

	var status string
	var scriptExit int
	require.NoError(t, dbRead.QueryRow(
		"SELECT status, script_exit_code FROM task_executions WHERE task_id = ? ORDER BY id DESC LIMIT 1",
		task.ID).Scan(&status, &scriptExit))
	assert.Equal(t, 0, scriptExit, "the AI-phase row must carry the script's exit code")
}

// TestExecuteTask_ScriptPhase_VisibleButNotCounted asserts the design point:
// during the script phase GetRunningExecutions reports the "script" entry while
// GetRunningCounts reports 0.
func TestExecuteTask_ScriptPhase_VisibleButNotCounted(t *testing.T) {
	skipOnWindows(t)
	setupSchedulerScriptDB(t)

	registerGatedBackend(t, "test-script-visibility")
	registerScriptAgent(t, "test-script-visibility")

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(nil) })

	s := NewScheduler()
	t.Cleanup(s.Stop)

	projectPath := t.TempDir()

	task := &model.ScheduledTask{
		ProjectPath: projectPath,
		Name:        "Visibility Task",
		CronExpr:    "0 * * * *",
		AgentID:     "script-agent",
		Prompt:      "do work",
		RepeatMode:  "unlimited",
		// Sleep so the script phase is observable from another goroutine.
		Script: "sleep 1; echo produced",
		Status: "active",
	}
	require.NoError(t, s.AddTask(task))

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.executeTask(task, task.ProjectPath, "auto", nil)
	}()

	// While the script runs, the script-phase entry must be visible...
	require.Eventually(t, func() bool {
		return len(s.GetRunningExecutions(task.ID)) == 1
	}, 2*time.Second, 10*time.Millisecond, "the script-phase entry must be registered")

	execs := s.GetRunningExecutions(task.ID)
	require.Len(t, execs, 1)
	assert.Equal(t, RunningPhaseScript, execs[0].Phase)
	assert.Equal(t, "script-"+strconv.FormatInt(task.ID, 10), execs[0].ID)

	// ...but must not count as a running task.
	assert.Equal(t, 0, s.GetRunningCounts()[task.ID],
		"the script phase must not be counted by GetRunningCounts")

	// Unblock/abandon: cancel so the AI phase does not hang on the closed gate.
	s.CancelExecution("script-" + strconv.FormatInt(task.ID, 10))
	<-done
}

// TestExecuteTask_ScriptPhase_Cancel asserts that cancelling the synthetic
// script execution terminates the run and records it as cancelled (with an
// empty session id) rather than continuing into the AI phase.
func TestExecuteTask_ScriptPhase_Cancel(t *testing.T) {
	skipOnWindows(t)
	setupSchedulerScriptDB(t)
	// The AI backend must never be reached on the cancel path.
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"script-agent": {ID: "script-agent", Name: "Script Agent", Backend: "nonexistent-backend-xyz"},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(nil) })

	s := NewScheduler()
	t.Cleanup(s.Stop)

	projectPath := t.TempDir()

	task := &model.ScheduledTask{
		ProjectPath: projectPath,
		Name:        "Cancel Task",
		CronExpr:    "0 * * * *",
		AgentID:     "script-agent",
		Prompt:      "do work",
		RepeatMode:  "unlimited",
		Script:      "sleep 30",
		Status:      "active",
	}
	require.NoError(t, s.AddTask(task))

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "script-cancel-client", "")

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.executeTask(task, task.ProjectPath, "auto", nil)
	}()

	// Wait for the script-phase entry, then cancel it mid-run.
	require.Eventually(t, func() bool {
		return s.CancelExecution("script-"+strconv.FormatInt(task.ID, 10)) == nil
	}, 2*time.Second, 10*time.Millisecond, "the script execution must be cancellable")

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the run did not terminate after cancelling the script phase")
	}

	// Recorded as cancelled with no session.
	var status, sessionID string
	require.NoError(t, dbRead.QueryRow(
		"SELECT status, session_id FROM task_executions WHERE task_id = ? ORDER BY id DESC LIMIT 1",
		task.ID).Scan(&status, &sessionID))
	assert.Equal(t, "cancelled", status)
	assert.Empty(t, sessionID)

	// A cancelled event must be emitted.
	var sawCancelled bool
	for _, ev := range sub.GetBufferedEvents() {
		data, ok := ev.Data.(*ws.TaskUpdateData)
		if ok && data.Status == "cancelled" {
			sawCancelled = true
		}
	}
	assert.True(t, sawCancelled, "the cancel path must emit a cancelled task_update")

	// run_count advanced just like a normal run.
	after, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, after.RunCount)
}

// TestExecuteTask_ScriptPhase_CancelWithoutExecutionRow asserts the emitted
// cancelled event carries no execution id when the execution row could not be
// written. Emitting the zero-value "0" would deep-link the notification to a
// row that does not exist.
func TestExecuteTask_ScriptPhase_CancelWithoutExecutionRow(t *testing.T) {
	skipOnWindows(t)
	setupSchedulerScriptDB(t)

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"script-agent": {ID: "script-agent", Name: "Script Agent", Backend: "nonexistent-backend-xyz"},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(nil) })

	s := NewScheduler()
	t.Cleanup(s.Stop)

	task := &model.ScheduledTask{
		ProjectPath: t.TempDir(),
		Name:        "Cancel No Row",
		CronExpr:    "0 * * * *",
		AgentID:     "script-agent",
		Prompt:      "do work",
		RepeatMode:  "unlimited",
		Script:      "sleep 30",
		Status:      "active",
	}
	require.NoError(t, s.AddTask(task))

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "script-cancel-norow", "")

	// Drop the execution table so AddTaskExecutionWithStatus fails while the
	// task row itself survives (the scheduler needs it to advance the run).
	_, err := WriteExec("ALTER TABLE task_executions RENAME TO task_executions_hidden")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = WriteExec("ALTER TABLE task_executions_hidden RENAME TO task_executions")
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.executeTask(task, task.ProjectPath, "auto", nil)
	}()

	require.Eventually(t, func() bool {
		return s.CancelExecution("script-"+strconv.FormatInt(task.ID, 10)) == nil
	}, 2*time.Second, 10*time.Millisecond, "the script execution must be cancellable")

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the run did not terminate after cancelling the script phase")
	}

	// The cancelled event still fires, but must not claim an execution id.
	var cancelled *ws.TaskUpdateData
	for _, ev := range sub.GetBufferedEvents() {
		if data, ok := ev.Data.(*ws.TaskUpdateData); ok && data.Status == "cancelled" {
			cancelled = data
		}
	}
	require.NotNil(t, cancelled, "the cancel path must still emit a cancelled task_update")
	assert.Empty(t, cancelled.ExecutionID, "no execution row was written, so no id may be advertised")
}

// TestExecuteTask_ScriptTemplate_InjectedIntoPrompt asserts that the prompt's
// template variables are substituted with the gating script's result, and that
// the substituted prompt is what gets persisted as the user chat message.
func TestExecuteTask_ScriptTemplate_InjectedIntoPrompt(t *testing.T) {
	skipOnWindows(t)
	setupSchedulerScriptDB(t)

	gate := registerGatedBackend(t, "test-script-inject")
	registerScriptAgent(t, "test-script-inject")

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(nil) })

	s := NewScheduler()
	t.Cleanup(s.Stop)

	projectPath := t.TempDir()

	task := &model.ScheduledTask{
		ProjectPath: projectPath,
		Name:        "Inject Task",
		CronExpr:    "0 * * * *",
		AgentID:     "script-agent",
		Prompt:      "script said {{stdout}} and exited {{code}}; then summarize",
		RepeatMode:  "unlimited",
		Script:      "echo script-ran-here",
		Status:      "active",
	}
	require.NoError(t, s.AddTask(task))

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.executeTask(task, task.ProjectPath, "auto", nil)
	}()

	// The user message is written before the AI turn blocks on the gate.
	var content string
	require.Eventually(t, func() bool {
		err := dbRead.QueryRow(
			"SELECT content FROM chat_history WHERE role = 'user' ORDER BY id DESC LIMIT 1").Scan(&content)
		return err == nil && content != ""
	}, 2*time.Second, 10*time.Millisecond, "the user prompt must be persisted")

	close(gate)
	<-done

	assert.Contains(t, content, "script-ran-here", "{{stdout}} must be substituted")
	assert.Contains(t, content, "exited 0", "{{code}} must be substituted")
	assert.NotContains(t, content, "{{stdout}}", "the variable must not survive substitution")
	assert.NotContains(t, content, "{{code}}", "the variable must not survive substitution")
	assert.Contains(t, content, "then summarize", "the task prompt must still follow the substitution")
}

// TestRenderScriptPrompt covers the template renderer in isolation.
func TestRenderScriptPrompt(t *testing.T) {
	res := ScriptResult{
		Outcome: ScriptSucceeded, ExitCode: 0,
		Stdout: "out-line\n", Stderr: "err-line\n",
	}

	// All four variables are substituted.
	got := renderScriptPrompt("code={{code}} out={{stdout}} err={{stderr}} all={{output}}", res)
	assert.Contains(t, got, "code=0")
	assert.Contains(t, got, "out=out-line")
	assert.Contains(t, got, "err=err-line")
	assert.Contains(t, got, "out-line")
	assert.Contains(t, got, "err-line")
	assert.NotContains(t, got, "{{", "no variable may survive")

	// {{output}} merges both streams, labeling stderr.
	outOnly := renderScriptPrompt("{{output}}", res)
	assert.Contains(t, outOnly, "out-line")
	assert.Contains(t, outOnly, "err-line")
	assert.Contains(t, outOnly, "--- stderr ---")

	// A clean stdout-only run has no stderr label.
	clean := renderScriptPrompt("{{output}}", ScriptResult{ExitCode: 0, Stdout: "only\n"})
	assert.Contains(t, clean, "only")
	assert.NotContains(t, clean, "stderr")

	// A non-zero exit code is available to the prompt.
	failed := renderScriptPrompt("exit={{code}}", ScriptResult{ExitCode: 2})
	assert.Contains(t, failed, "exit=2")

	// A prompt with no variables is returned unchanged.
	assert.Equal(t, "no vars here", renderScriptPrompt("no vars here", res))

	// Output beyond the cap is truncated with a marker.
	big := renderScriptPrompt("{{stdout}}", ScriptResult{
		ExitCode: 0, Stdout: strings.Repeat("x", scriptOutputCap+100), StdoutTruncated: true,
	})
	assert.Contains(t, big, "output truncated")

	// A stream that reached exactly the cap is NOT truncated, so it must not
	// carry the marker: the flag comes from cappedBuffer, not from the length,
	// which cannot distinguish "cut" from "ended exactly here".
	atCap := renderScriptPrompt("{{stdout}}", ScriptResult{
		ExitCode: 0, Stdout: strings.Repeat("x", scriptOutputCap),
	})
	assert.NotContains(t, atCap, "output truncated",
		"a stream that ended exactly at the cap was not cut and must not be marked")
}

// TestExecuteTask_EventTask_IgnoresScript asserts the guard: an event task
// never runs a script even if one is stored.
func TestExecuteTask_EventTask_IgnoresScript(t *testing.T) {
	skipOnWindows(t)
	setupSchedulerScriptDB(t)

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"script-agent": {ID: "script-agent", Name: "Script Agent", Backend: "nonexistent-backend-xyz"},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	s := NewScheduler()
	t.Cleanup(s.Stop)

	task := &model.ScheduledTask{
		ProjectPath: t.TempDir(),
		Name:        "Event Task",
		AgentID:     "script-agent",
		Prompt:      "react to event",
		RepeatMode:  "unlimited",
		TriggerMode: "event",
		EventTypes:  "issue.opened",
		// A script is stored but must be ignored for event tasks. It exits
		// non-zero, so if it ever ran it would close the gate and write a
		// `skipped` row — the assertion below is then meaningful.
		Script: "exit 1",
		Status: "active",
	}
	require.NoError(t, s.AddTask(task))

	// The event task goes straight to the AI phase, which fails because the
	// backend does not exist. The point is that no `skipped` row is written.
	s.executeTask(task, task.ProjectPath, "auto", nil)

	var skipped int
	require.NoError(t, dbRead.QueryRow(
		"SELECT COUNT(*) FROM task_executions WHERE task_id = ? AND status = 'skipped'", task.ID).Scan(&skipped))
	assert.Zero(t, skipped, "an event task must not run the script")
}

// TestExecuteTask_ScriptPhase_CancelDoesNotConsumeRun guards the cancel
// bookkeeping: cancelling during the script phase aborts a run that never
// produced a result, so it must NOT exhaust a `once` task. Advancing the
// schedule there would mark the task completed (and null next_run_at) without
// the AI ever executing, silently swallowing the user's one-shot task.
func TestExecuteTask_ScriptPhase_CancelDoesNotConsumeRun(t *testing.T) {
	skipOnWindows(t)
	setupSchedulerScriptDB(t)

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"script-agent": {ID: "script-agent", Name: "Script Agent", Backend: "nonexistent-backend-xyz"},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(nil) })

	s := NewScheduler()
	t.Cleanup(s.Stop)

	task := &model.ScheduledTask{
		ProjectPath: t.TempDir(),
		Name:        "Once Cancel",
		CronExpr:    "0 * * * *",
		AgentID:     "script-agent",
		Prompt:      "do work",
		RepeatMode:  "once",
		Script:      "sleep 30",
		Status:      "active",
	}
	require.NoError(t, s.AddTask(task))

	before, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	require.NotNil(t, before.NextRunAt, "a cron task must have a next_run_at after AddTask")

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.executeTask(task, task.ProjectPath, "auto", nil)
	}()

	require.Eventually(t, func() bool {
		return s.CancelExecution("script-"+strconv.FormatInt(task.ID, 10)) == nil
	}, 2*time.Second, 10*time.Millisecond, "the script execution must be cancellable")
	<-done

	after, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", after.Status,
		"a script-phase cancel must not complete a once task the AI never ran")
	require.NotNil(t, after.NextRunAt,
		"a script-phase cancel must leave the schedule intact")
	assert.Equal(t, before.RunCount+1, after.RunCount,
		"the aborted run is still recorded")

	// The cancelled execution row is still written, so the user can see it.
	var status string
	require.NoError(t, dbRead.QueryRow(
		"SELECT status FROM task_executions WHERE task_id = ? ORDER BY id DESC LIMIT 1", task.ID).Scan(&status))
	assert.Equal(t, "cancelled", status)
}

// TestExecuteTask_ScriptGateClosed_StillConsumesRun is the converse guard: a
// gate-closed run is a completed decision ("nothing to do"), so it must keep
// advancing the schedule — including completing a `once` task, which has then
// legitimately run its course.
func TestExecuteTask_ScriptGateClosed_StillConsumesRun(t *testing.T) {
	skipOnWindows(t)
	setupSchedulerScriptDB(t)

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"script-agent": {ID: "script-agent", Name: "Script Agent", Backend: "nonexistent-backend-xyz"},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	s := NewScheduler()
	t.Cleanup(s.Stop)

	task := &model.ScheduledTask{
		ProjectPath: t.TempDir(),
		Name:        "Once Gate Closed",
		CronExpr:    "0 * * * *",
		AgentID:     "script-agent",
		Prompt:      "do work",
		RepeatMode:  "once",
		Script:      "exit 1", // non-zero closes the gate -> skip
		Status:      "active",
	}
	require.NoError(t, s.AddTask(task))

	s.executeTask(task, task.ProjectPath, "auto", nil)

	after, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", after.Status, "a gate-closed run is a completed decision for a once task")
	assert.Equal(t, 1, after.RunCount)
}
