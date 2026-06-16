package service

import (
	"context"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	_ "modernc.org/sqlite"
)

// --- Scheduler executeTask delegation tests ---
// These tests cover the extracted processScheduledStreamEvents function,
// which handles the core streaming event loop for scheduled tasks.

func setupSchedulerForExecuteTask(t *testing.T) {
	t.Helper()
	setupExecutorTestDB(t)
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

func TestCreateStreamingPlaceholder(t *testing.T) {
	setupSchedulerForExecuteTask(t)

	sid, err := CreateSession("/test", "codebuddy", "Placeholder Test", "test-agent", "", "default", "scheduled")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	createStreamingPlaceholder("/test", "codebuddy", sid)

	var count int
	if err := DBRead.QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 1",
		sid,
	).Scan(&count); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 streaming assistant message, got %d", count)
	}
}

func TestProcessScheduledStreamEvents_NormalCompletion(t *testing.T) {
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
		{Type: "text", Content: "task result"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	_, completed := processScheduledStreamEvents(context.Background(), ch, cfg, task, executionID)

	if !completed {
		t.Fatal("expected completed=true for normal completion")
	}

	// Verify execution status was updated
	var status string
	if err := DBRead.QueryRow("SELECT status FROM task_executions WHERE id = ?", executionID).Scan(&status); err != nil {
		t.Fatalf("failed to query execution status: %v", err)
	}
	if status != "completed" {
		t.Fatalf("expected status=completed, got %s", status)
	}
}

func TestProcessScheduledStreamEvents_CancelledContext(t *testing.T) {
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

	_, completed := processScheduledStreamEvents(ctx, ch, cfg, task, executionID)

	if completed {
		t.Fatal("expected completed=false for cancelled context")
	}

	// Verify execution status was set to "cancelled"
	var status string
	if err := DBRead.QueryRow("SELECT status FROM task_executions WHERE id = ?", executionID).Scan(&status); err != nil {
		t.Fatalf("failed to query execution status: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("expected status=cancelled, got %s", status)
	}
}

func TestProcessScheduledStreamEvents_CrashedProcess(t *testing.T) {
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

	_, completed := processScheduledStreamEvents(context.Background(), ch, cfg, task, executionID)

	if completed {
		t.Fatal("expected completed=false for crashed process")
	}

	// Verify execution status was set to "failed"
	var status string
	if err := DBRead.QueryRow("SELECT status FROM task_executions WHERE id = ?", executionID).Scan(&status); err != nil {
		t.Fatalf("failed to query execution status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected status=failed, got %s", status)
	}
}

func TestProcessScheduledStreamEvents_WithMetadata(t *testing.T) {
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

	events := []ai.StreamEvent{
		{Type: "metadata", Meta: &ai.Metadata{Model: "test-model", SessionID: "ext-123"}},
		{Type: "session_capture", Content: "ext-session-456"},
		{Type: "text", Content: "response"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	_, completed := processScheduledStreamEvents(context.Background(), ch, cfg, task, executionID)

	if !completed {
		t.Fatal("expected completed=true with metadata events")
	}
}
