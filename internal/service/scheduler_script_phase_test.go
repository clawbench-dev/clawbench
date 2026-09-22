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
const schedulerScriptSchema = `
CREATE TABLE IF NOT EXISTS chat_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
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
	project_path TEXT NOT NULL,
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
	UNIQUE(project_path, backend, id)
);
CREATE TABLE IF NOT EXISTS scheduled_tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
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
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_executions_task ON task_executions(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_path, backend, session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_path, backend);
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

// TestExecuteTask_ScriptSkip_NoSessionNoEvent is the core skip-path guard: a
// script that exits 0 with no output must not create a session, must not call
// the AI, must record a `skipped` execution with an empty session_id, must
// advance run_count/next_run_at, and must emit nothing at all.
func TestExecuteTask_ScriptSkip_NoSessionNoEvent(t *testing.T) {
	skipOnWindows(t)
	setupSchedulerScriptDB(t)
	// No AI backend registered: the skip path must not need one.

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
		Name:        "Skip Task",
		CronExpr:    "0 * * * *",
		AgentID:     "script-agent",
		Prompt:      "do work",
		RepeatMode:  "unlimited",
		Script:      "true", // exits 0, no output
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
	sub := mgr.Subscribe(nil, &writeMu, "script-skip-client", "")

	s.executeTask(task, task.ProjectPath, "auto", nil)

	// No session was created.
	var sessionCount int
	require.NoError(t, dbRead.QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE project_path = ?", task.ProjectPath).Scan(&sessionCount))
	assert.Zero(t, sessionCount, "a skipped run must not create a chat session")

	// No chat message was written.
	var msgCount int
	require.NoError(t, dbRead.QueryRow("SELECT COUNT(*) FROM chat_history").Scan(&msgCount))
	assert.Zero(t, msgCount, "a skipped run must not write any chat message")

	// An execution row with status skipped and an empty session_id exists.
	var status, sessionID string
	require.NoError(t, dbRead.QueryRow(
		"SELECT status, session_id FROM task_executions WHERE task_id = ? ORDER BY id DESC LIMIT 1",
		task.ID).Scan(&status, &sessionID))
	assert.Equal(t, "skipped", status)
	assert.Empty(t, sessionID, "a skipped run has no session")

	// run_count incremented and next_run_at advanced.
	after, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	assert.Equal(t, before.RunCount+1, after.RunCount, "run_count must be incremented by a skip")
	require.NotNil(t, after.NextRunAt)
	assert.True(t, after.NextRunAt.After(*before.NextRunAt),
		"next_run_at must advance (before=%v after=%v)", before.NextRunAt, after.NextRunAt)

	// No task_update event was emitted.
	assert.Empty(t, sub.GetBufferedEvents(), "a skipped run must emit no task_update event")

	// runningCount stays 0 for the task.
	counts := s.GetRunningCounts()
	assert.Equal(t, 0, counts[task.ID], "a script-phase run must not count as running")
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

// TestExecuteTask_ScriptOutput_InjectedIntoPrompt asserts that a script which
// prints something has its output injected into the prompt, visible in the
// persisted user chat message.
func TestExecuteTask_ScriptOutput_InjectedIntoPrompt(t *testing.T) {
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
		Prompt:      "summarize the findings",
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

	assert.Contains(t, content, "script-ran-here", "the script output must be injected into the prompt")
	assert.Contains(t, content, "<<<TASK_SCRIPT_OUTPUT>>>", "the injected block must be delimited")
	assert.Contains(t, content, "summarize the findings", "the task prompt must still follow the script block")
	// Script output precedes the task prompt.
	assert.Less(t, strings.Index(content, "script-ran-here"), strings.Index(content, "summarize the findings"),
		"the script block must come before the task prompt")
}

// TestBuildScriptPromptBlock_Outcomes covers the prompt-block builder in
// isolation, so the failure/timeout branches need no real backend.
func TestBuildScriptPromptBlock_Outcomes(t *testing.T) {
	produced := buildScriptPromptBlock(ScriptResult{
		Outcome: ScriptProduced, Stdout: "hello\n", Stderr: "warn\n",
	})
	assert.Contains(t, produced, "hello")
	assert.Contains(t, produced, "warn")
	assert.Contains(t, produced, "<<<TASK_SCRIPT_OUTPUT>>>")
	assert.Contains(t, produced, "<<<END_TASK_SCRIPT_OUTPUT>>>")

	failed := buildScriptPromptBlock(ScriptResult{
		Outcome: ScriptFailed, Stdout: "partial\n", ExitCode: 2, Err: assertErr("exit status 2"),
	})
	assert.Contains(t, failed, "failed")
	assert.Contains(t, failed, "partial")

	timedOut := buildScriptPromptBlock(ScriptResult{Outcome: ScriptTimedOut, Err: assertErr("deadline exceeded")})
	assert.Contains(t, timedOut, "timed out")

	// Output beyond the cap is truncated with a marker.
	big := buildScriptPromptBlock(ScriptResult{
		Outcome: ScriptProduced, Stdout: string(make([]byte, scriptOutputCap+100)),
	})
	assert.Contains(t, big, "output truncated")

	// A stream that reached exactly the cap was also cut at capture time
	// (cappedBuffer retains at most cap bytes), so the marker must still appear
	// — otherwise the model is handed a silently-truncated output.
	atCap := buildScriptPromptBlock(ScriptResult{
		Outcome: ScriptProduced, Stdout: strings.Repeat("x", scriptOutputCap),
	})
	assert.Contains(t, atCap, "output truncated")
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
		// A script is stored but must be ignored for event tasks.
		Script: "true",
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

// assertErr is a tiny error helper so tests do not need fmt.
type assertErr string

func (e assertErr) Error() string { return string(e) }
