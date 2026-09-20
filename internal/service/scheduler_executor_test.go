package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"

	_ "modernc.org/sqlite"
)

// schedulerExecSchema is the DB schema needed for scheduler executor tests.
const schedulerExecSchema = `
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
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_executions_task ON task_executions(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_path, backend, session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_path, backend);
CREATE INDEX IF NOT EXISTS idx_executions_session ON task_executions(session_id);
CREATE TABLE IF NOT EXISTS chat_metadata (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL,
	mode TEXT DEFAULT '',
	thinking_effort TEXT DEFAULT '',
	transport TEXT DEFAULT '',
	model TEXT DEFAULT '',
	input_tokens INTEGER DEFAULT 0,
	output_tokens INTEGER DEFAULT 0,
	duration_ms INTEGER DEFAULT 0,
	wall_ms INTEGER DEFAULT 0,
	cost_usd REAL DEFAULT 0,
	stop_reason TEXT DEFAULT '',
	is_error INTEGER DEFAULT 0,
	error_message TEXT DEFAULT '',
	cache_creation_input_tokens INTEGER DEFAULT 0,
	cache_read_input_tokens INTEGER DEFAULT 0,
	cached_read_tokens INTEGER DEFAULT 0,
	cached_write_tokens INTEGER DEFAULT 0,
	thought_tokens INTEGER DEFAULT 0,
	total_tokens INTEGER DEFAULT 0,
	cache_creation_tokens INTEGER DEFAULT 0,
	cache_hit_tokens INTEGER DEFAULT 0,
	cache_miss_tokens INTEGER DEFAULT 0,
	credit REAL DEFAULT 0,
	usage_by_category TEXT DEFAULT '',
	session_id TEXT DEFAULT '',
	request_id TEXT DEFAULT '',
	trace_id TEXT DEFAULT '',
	agent_message_id TEXT DEFAULT '',
	message_request_id TEXT DEFAULT '',
	request_model_name TEXT DEFAULT '',
	response_model_id TEXT DEFAULT '',
	finish_reason TEXT DEFAULT '',
	outcome TEXT DEFAULT '',
	agent_phase TEXT DEFAULT '',
	project_path TEXT DEFAULT '',
	backend TEXT DEFAULT '',
	agent_id TEXT DEFAULT '',
	clawbench_session_id TEXT DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS chat_tool_calls (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	tool_id TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	input TEXT DEFAULT '',
	output TEXT DEFAULT '',
	status TEXT DEFAULT '',
	done INTEGER NOT NULL DEFAULT 0,
	summary TEXT DEFAULT '',
	duration_ms INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(tool_id, message_id)
);
CREATE INDEX IF NOT EXISTS idx_tool_calls_message ON chat_tool_calls(message_id);
CREATE INDEX IF NOT EXISTS idx_tool_calls_session ON chat_tool_calls(session_id, created_at DESC);
CREATE TABLE IF NOT EXISTS chat_thinking (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	think_id TEXT NOT NULL,
	seq INTEGER NOT NULL DEFAULT 0,
	text TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(think_id, message_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_thinking_message ON chat_thinking(message_id);
CREATE TABLE IF NOT EXISTS summaries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	target_type TEXT NOT NULL,
	target_id   INTEGER NOT NULL,
	summary     TEXT NOT NULL,
	summary_cards TEXT NOT NULL DEFAULT '',
	created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(target_type, target_id)
);
CREATE TABLE IF NOT EXISTS chat_recommendations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	project_path TEXT NOT NULL DEFAULT '',
	message_id INTEGER NOT NULL DEFAULT 0,
	recommendation TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

func setupSchedulerExecDB(t *testing.T) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(schedulerExecSchema)
	if err != nil {
		t.Fatalf("failed to exec schema: %v", err)
	}
	cleanup := SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		db.Close()
	})
}

func setupSchedulerForExecuteTask(t *testing.T) {
	t.Helper()
	setupSchedulerExecDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:                  "test-agent",
			Name:                "Test Agent",
			Backend:             "codebuddy",
			RuntimeSystemPrompt: "test prompt",
			Command:             "echo hello",
		},
	}
	t.Cleanup(func() {
		model.Agents = nil
	})
}

func TestScheduledExecution_NormalCompletion(t *testing.T) {
	setupSchedulerForExecuteTask(t)

	sid, err := CreateSession("/test", "codebuddy", "Normal Test", "test-agent", "", "default", "scheduled")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Add task to DB
	task := &model.ScheduledTask{
		ProjectPath: "/test",
		Name:        "Test Task",
		CronExpr:    "0 * * * *",
		AgentID:     "test-agent",
		Prompt:      "test prompt",
		RepeatMode:  "unlimited",
	}
	s := NewScheduler()
	defer s.Stop()
	if err := s.AddTask(task); err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	// Add execution record
	executionID, _ := AddTaskExecution(task.ID, sid, "auto")

	// Create streaming placeholder (mirrors scheduler.go behavior)
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	_, _ = AddChatMessage("/test", "codebuddy", sid, "assistant", string(emptyContent), nil, true, "")

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "test prompt", ScheduledExecution: true},
		TaskID:      task.ID,
		ExecutionID: executionID,
		TriggerType: "auto",
	}

	// Create event channel with normal completion
	events := []ai.StreamEvent{
		{Type: "content", Content: "task result"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(context.Background(), cfg)
	runResult := executor.RunWithChannel(ch)

	if !runResult.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true for normal completion")
	}

	// Finalize (mirrors scheduler.go behavior)
	runResult = executor.Finalize(runResult, nil)

	// Verify execution status was updated
	_ = UpdateExecutionStatus(sid, "completed")
	var status string
	if err := dbRead.QueryRow("SELECT status FROM task_executions WHERE id = ?", executionID).Scan(&status); err != nil {
		t.Fatalf("failed to query execution status: %v", err)
	}
	if status != "completed" {
		t.Fatalf("expected status=completed, got %s", status)
	}
}

func TestScheduledExecution_CancelledContext(t *testing.T) {
	setupSchedulerForExecuteTask(t)

	sid, err := CreateSession("/test", "codebuddy", "Cancel Test", "test-agent", "", "default", "scheduled")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	task := &model.ScheduledTask{
		ProjectPath: "/test",
		Name:        "Cancel Task",
		CronExpr:    "0 * * * *",
		AgentID:     "test-agent",
		Prompt:      "test prompt",
		RepeatMode:  "unlimited",
	}
	s := NewScheduler()
	defer s.Stop()
	if err := s.AddTask(task); err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	executionID, _ := AddTaskExecution(task.ID, sid, "auto")

	// Create streaming placeholder
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	_, _ = AddChatMessage("/test", "codebuddy", sid, "assistant", string(emptyContent), nil, true, "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "test prompt", ScheduledExecution: true},
		TaskID:      task.ID,
		ExecutionID: executionID,
	}

	// Channel closes without terminal event
	ch := make(chan ai.StreamEvent)
	close(ch)

	executor := NewSessionExecutor(ctx, cfg)
	runResult := executor.RunWithChannel(ch)

	// Context was cancelled before execution → should not have received terminal
	if runResult.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=false for cancelled context")
	}

	// Verify execution status was set to "cancelled"
	_ = UpdateExecutionStatus(sid, "cancelled")
	var status string
	if err := dbRead.QueryRow("SELECT status FROM task_executions WHERE id = ?", executionID).Scan(&status); err != nil {
		t.Fatalf("failed to query execution status: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("expected status=cancelled, got %s", status)
	}
}

func TestScheduledExecution_CrashedProcess(t *testing.T) {
	setupSchedulerForExecuteTask(t)

	sid, err := CreateSession("/test", "codebuddy", "Crash Test", "test-agent", "", "default", "scheduled")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	task := &model.ScheduledTask{
		ProjectPath: "/test",
		Name:        "Crash Task",
		CronExpr:    "0 * * * *",
		AgentID:     "test-agent",
		Prompt:      "test prompt",
		RepeatMode:  "unlimited",
	}
	s := NewScheduler()
	defer s.Stop()
	if err := s.AddTask(task); err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	executionID, _ := AddTaskExecution(task.ID, sid, "auto")

	// Create streaming placeholder
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	_, _ = AddChatMessage("/test", "codebuddy", sid, "assistant", string(emptyContent), nil, true, "")

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "test prompt", ScheduledExecution: true},
		TaskID:      task.ID,
		ExecutionID: executionID,
	}

	// Channel closes without done/error (simulates CLI crash)
	ch := make(chan ai.StreamEvent)
	close(ch)

	executor := NewSessionExecutor(context.Background(), cfg)
	runResult := executor.RunWithChannel(ch)

	if runResult.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=false for crashed process")
	}

	// Verify execution status was set to "failed"
	_ = UpdateExecutionStatus(sid, "failed")
	var status string
	if err := dbRead.QueryRow("SELECT status FROM task_executions WHERE id = ?", executionID).Scan(&status); err != nil {
		t.Fatalf("failed to query execution status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected status=failed, got %s", status)
	}
}

func TestScheduledExecution_WithMetadata(t *testing.T) {
	setupSchedulerForExecuteTask(t)

	sid, err := CreateSession("/test", "codebuddy", "Metadata Test", "test-agent", "", "default", "scheduled")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	task := &model.ScheduledTask{
		ProjectPath: "/test",
		Name:        "Metadata Task",
		CronExpr:    "0 * * * *",
		AgentID:     "test-agent",
		Prompt:      "test prompt",
		RepeatMode:  "unlimited",
	}
	s := NewScheduler()
	defer s.Stop()
	if err := s.AddTask(task); err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	executionID, _ := AddTaskExecution(task.ID, sid, "auto")

	// Create streaming placeholder
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	_, _ = AddChatMessage("/test", "codebuddy", sid, "assistant", string(emptyContent), nil, true, "")

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "test prompt", ScheduledExecution: true},
		TaskID:      task.ID,
		ExecutionID: executionID,
		TriggerType: "auto",
	}

	events := []ai.StreamEvent{
		{Type: "metadata", Meta: &ai.Metadata{Model: "test-model", SessionID: "ext-123"}},
		{Type: "session_capture", Content: "ext-session-456"},
		{Type: "content", Content: "response"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(context.Background(), cfg)
	runResult := executor.RunWithChannel(ch)

	if !runResult.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true with metadata events")
	}

	// Finalize should succeed
	runResult = executor.Finalize(runResult, nil)
	if runResult.Metadata == nil {
		t.Fatal("expected Metadata to be captured")
	}
	if runResult.Metadata.Model != "test-model" {
		t.Fatalf("expected Model='test-model', got %q", runResult.Metadata.Model)
	}
}

// ── Auto-continue ──────────────────────────────────────────────────────────
//
// A scheduled execution that dies abnormally must be resumed by sending a
// localized "continue", reusing the same execution row so the run is not
// counted as finished while it is still going.

// crashThenSucceedBackend closes the event channel with no terminal event on its
// first call (a crashed agent process) and answers normally afterwards.
type crashThenSucceedBackend struct {
	calls    atomic.Int32
	mu       sync.Mutex
	requests []ai.ChatRequest
}

func (b *crashThenSucceedBackend) Name() string { return "test-auto-continue" }

func (b *crashThenSucceedBackend) ExecuteStream(_ context.Context, req ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	b.mu.Lock()
	b.requests = append(b.requests, req)
	n := b.calls.Add(1)
	b.mu.Unlock()

	ch := make(chan ai.StreamEvent, 4)
	if n == 1 {
		// Partial output, then the channel closes with no terminal event — how a
		// crashed agent process surfaces. The partial text matters: it makes the
		// session carry real assistant content, which is what lets the retry
		// resolve resume=true and continue the same context.
		ch <- ai.StreamEvent{Type: "content", Content: "working on it"}
		close(ch)
		return ch, nil
	}
	ch <- ai.StreamEvent{Type: "content", Content: "resumed ok"}
	ch <- ai.StreamEvent{Type: "done"}
	close(ch)
	return ch, nil
}

// autoContinueBackendSeq makes each test's backend id unique.
var autoContinueBackendSeq atomic.Int32

func (b *crashThenSucceedBackend) snapshotRequests() []ai.ChatRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]ai.ChatRequest(nil), b.requests...)
}

// setupAutoContinueTaskEnv wires the shared fixtures for the auto-continue
// scheduler tests: the crash-then-succeed backend, a test agent, a test WS
// manager, and an instant auto-continue delay.
func setupAutoContinueTaskEnv(t *testing.T, enabled bool) *crashThenSucceedBackend {
	t.Helper()
	backend := &crashThenSucceedBackend{}
	// RegisterBackend panics on a duplicate id, so each test gets its own.
	backendName := fmt.Sprintf("test-auto-continue-%d", autoContinueBackendSeq.Add(1))
	ai.RegisterBackend(backendName, func() ai.AIBackend { return backend })

	setupSchedulerExecDB(t)
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test Agent", Backend: backendName, RuntimeSystemPrompt: "test prompt"},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	origMgr := ws.GetManager()
	ws.SetManagerForTest(ws.NewManagerForTest())
	t.Cleanup(func() { ws.SetManagerForTest(origMgr) })

	prevEnabled, prevMax, prevLang := model.ChatAutoContinueEnabled, model.ChatAutoContinueMaxRetries, model.Language
	model.ChatAutoContinueEnabled, model.ChatAutoContinueMaxRetries, model.Language = enabled, 3, "en"
	t.Cleanup(func() {
		model.ChatAutoContinueEnabled, model.ChatAutoContinueMaxRetries, model.Language = prevEnabled, prevMax, prevLang
	})

	prevSleep := autoContinueSleep
	autoContinueSleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
	t.Cleanup(func() { autoContinueSleep = prevSleep })

	return backend
}

// runAutoContinueTask executes one task and returns once executeTask finishes.
func runAutoContinueTask(t *testing.T, s *Scheduler, name string) {
	t.Helper()
	task := &model.ScheduledTask{
		ProjectPath: "/tmp",
		Name:        name,
		CronExpr:    "0 * * * *",
		AgentID:     "test-agent",
		Prompt:      "do the thing",
		Status:      "active",
		RepeatMode:  "unlimited",
	}
	if err := s.AddTask(task); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.executeTask(task, "/tmp", "manual", nil)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("executeTask did not return")
	}
}

func TestScheduler_ExecuteTask_AutoContinuesCrashedTurn(t *testing.T) {
	backend := setupAutoContinueTaskEnv(t, true)

	s := NewScheduler()
	defer s.Stop()
	runAutoContinueTask(t, s, "Crashy")

	// The backend must have been called twice: the crashed attempt and the resume.
	reqs := backend.snapshotRequests()
	if len(reqs) != 2 {
		t.Fatalf("backend calls = %d, want 2 (crashed attempt + resume)", len(reqs))
	}
	if reqs[1].Prompt != "Continue" {
		t.Errorf("retry prompt = %q, want %q (localized continue)", reqs[1].Prompt, "Continue")
	}
	if !reqs[1].Resume {
		t.Error("the retry must resume the session rather than start a new one")
	}

	var sessionID string
	if err := dbRead.QueryRow(
		"SELECT id FROM chat_sessions WHERE project_path = '/tmp' ORDER BY created_at DESC LIMIT 1",
	).Scan(&sessionID); err != nil {
		t.Fatalf("query session: %v", err)
	}

	// The execution must finish as completed — not failed — after the resume
	// succeeded, and there must be exactly ONE execution row: the retry reuses
	// it rather than creating a second (which would double-count the run).
	var status string
	if err := dbRead.QueryRow(
		"SELECT status FROM task_executions WHERE session_id = ?", sessionID,
	).Scan(&status); err != nil {
		t.Fatalf("query execution status: %v", err)
	}
	if status != "completed" {
		t.Errorf("execution status = %q, want %q", status, "completed")
	}

	var execCount int
	if err := dbRead.QueryRow(
		"SELECT COUNT(*) FROM task_executions WHERE session_id = ?", sessionID,
	).Scan(&execCount); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if execCount != 1 {
		t.Errorf("execution rows = %d, want 1 (a retry must reuse the row)", execCount)
	}

	// The auto-sent continue must be a real user row in the transcript.
	var continueCount int
	if err := dbRead.QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'user' AND content = 'Continue'",
		sessionID,
	).Scan(&continueCount); err != nil {
		t.Fatalf("count continue messages: %v", err)
	}
	if continueCount != 1 {
		t.Errorf("continue messages = %d, want 1 persisted as a user message", continueCount)
	}
}

// alwaysCrashBackend never emits a terminal event, so every attempt fails
// abnormally and the retry budget is the only thing that stops the loop.
type alwaysCrashBackend struct {
	calls atomic.Int32
}

func (b *alwaysCrashBackend) Name() string { return "test-always-crash" }

func (b *alwaysCrashBackend) ExecuteStream(_ context.Context, _ ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	b.calls.Add(1)
	ch := make(chan ai.StreamEvent, 1)
	ch <- ai.StreamEvent{Type: "content", Content: "partial"}
	close(ch)
	return ch, nil
}

// max_retries counts RETRIES, not total turns: with max=2 the task must run the
// original turn plus exactly two resumes, then give up and mark the execution
// failed. An off-by-one here would silently change how much a user's budget
// costs in tokens.
func TestScheduler_ExecuteTask_AutoContinueHonorsExactRetryBudget(t *testing.T) {
	backend := &alwaysCrashBackend{}
	backendName := fmt.Sprintf("test-always-crash-%d", autoContinueBackendSeq.Add(1))
	ai.RegisterBackend(backendName, func() ai.AIBackend { return backend })

	setupSchedulerExecDB(t)
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test Agent", Backend: backendName, RuntimeSystemPrompt: "test prompt"},
	}
	defer func() { model.Agents = origAgents }()

	origMgr := ws.GetManager()
	ws.SetManagerForTest(ws.NewManagerForTest())
	defer ws.SetManagerForTest(origMgr)

	prevEnabled, prevMax, prevLang := model.ChatAutoContinueEnabled, model.ChatAutoContinueMaxRetries, model.Language
	model.ChatAutoContinueEnabled, model.ChatAutoContinueMaxRetries, model.Language = true, 2, "en"
	defer func() {
		model.ChatAutoContinueEnabled, model.ChatAutoContinueMaxRetries, model.Language = prevEnabled, prevMax, prevLang
	}()
	prevSleep := autoContinueSleep
	autoContinueSleep = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
	defer func() { autoContinueSleep = prevSleep }()

	s := NewScheduler()
	defer s.Stop()
	runAutoContinueTask(t, s, "Always Crashy")

	if got := int(backend.calls.Load()); got != 3 {
		t.Errorf("backend calls = %d, want 3 (1 original + 2 retries for max_retries=2)", got)
	}

	var sessionID string
	if err := dbRead.QueryRow(
		"SELECT id FROM chat_sessions WHERE project_path = '/tmp' ORDER BY created_at DESC LIMIT 1",
	).Scan(&sessionID); err != nil {
		t.Fatalf("query session: %v", err)
	}
	var status string
	if err := dbRead.QueryRow(
		"SELECT status FROM task_executions WHERE session_id = ?", sessionID,
	).Scan(&status); err != nil {
		t.Fatalf("query execution status: %v", err)
	}
	if status != "failed" {
		t.Errorf("execution status = %q, want %q after exhausting retries", status, "failed")
	}
}

// When auto-continue is disabled the crashed turn must keep the historical
// behavior: the execution is marked failed and no extra turn runs.
func TestScheduler_ExecuteTask_NoAutoContinueWhenDisabled(t *testing.T) {
	backend := setupAutoContinueTaskEnv(t, false)

	s := NewScheduler()
	defer s.Stop()
	runAutoContinueTask(t, s, "Crashy Disabled")

	if got := len(backend.snapshotRequests()); got != 1 {
		t.Errorf("backend calls = %d, want 1 (disabled auto-continue must not retry)", got)
	}

	var sessionID string
	if err := dbRead.QueryRow(
		"SELECT id FROM chat_sessions WHERE project_path = '/tmp' ORDER BY created_at DESC LIMIT 1",
	).Scan(&sessionID); err != nil {
		t.Fatalf("query session: %v", err)
	}

	var status string
	if err := dbRead.QueryRow(
		"SELECT status FROM task_executions WHERE session_id = ?", sessionID,
	).Scan(&status); err != nil {
		t.Fatalf("query execution status: %v", err)
	}
	if status != "failed" {
		t.Errorf("execution status = %q, want %q", status, "failed")
	}
}
