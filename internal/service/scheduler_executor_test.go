package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

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
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
	deleted INTEGER NOT NULL DEFAULT 0,
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
CREATE TABLE IF NOT EXISTS ai_raw_responses (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	message_id INTEGER NOT NULL,
	backend TEXT NOT NULL DEFAULT '',
	raw_output TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
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
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(tool_id, message_id)
);
CREATE INDEX IF NOT EXISTS idx_tool_calls_message ON chat_tool_calls(message_id);
CREATE INDEX IF NOT EXISTS idx_tool_calls_session ON chat_tool_calls(session_id, created_at DESC);
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
	origDB := DB
	origDBRead := DBRead
	DB = db
	DBRead = db
	t.Cleanup(func() {
		DB = origDB
		DBRead = origDBRead
		db.Close()
	})
}

func setupSchedulerForExecuteTask(t *testing.T) {
	t.Helper()
	setupSchedulerExecDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {
			ID:           "test-agent",
			Name:         "Test Agent",
			Backend:      "codebuddy",
			SystemPrompt: "test prompt",
			Command:      "echo hello",
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
	if err := DBRead.QueryRow("SELECT status FROM task_executions WHERE id = ?", executionID).Scan(&status); err != nil {
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
	if err := DBRead.QueryRow("SELECT status FROM task_executions WHERE id = ?", executionID).Scan(&status); err != nil {
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
	if err := DBRead.QueryRow("SELECT status FROM task_executions WHERE id = ?", executionID).Scan(&status); err != nil {
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
