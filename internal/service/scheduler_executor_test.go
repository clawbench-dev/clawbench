package service

import (
	"context"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	_ "modernc.org/sqlite"
)

// --- Scheduler executeTask delegation tests ---
// These tests cover the code paths in scheduler.executeTask that delegate
// to SessionExecutor: createStreamingPlaceholder, NewSessionExecutor,
// RunWithChannel, cancel/crash checks, and Finalize.

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

	// Call createStreamingPlaceholder (extracted from executeTask line 691-692)
	createStreamingPlaceholder("/test", "codebuddy", sid)

	// Verify the placeholder message was created
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

func TestScheduler_ExecuteTask_DelegatesToSessionExecutor(t *testing.T) {
	setupSchedulerForExecuteTask(t)

	sid, err := CreateSession("/test", "codebuddy", "Delegate Test", "test-agent", "", "default", "scheduled")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Create streaming placeholder (same as scheduler.executeTask does)
	createStreamingPlaceholder("/test", "codebuddy", sid)

	// Create SessionExecutor in scheduled mode (same as executeTask lines 696-706)
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "scheduled prompt", ScheduledExecution: true},
	}
	executor := NewSessionExecutor(context.Background(), cfg)

	// RunWithChannel with normal completion events (line 707)
	events := []ai.StreamEvent{
		{Type: "text", Content: "task result"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	runResult := executor.RunWithChannel(ch)

	// Verify normal completion path
	if !runResult.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true for normal completion")
	}

	// Finalize (line 740)
	finalized := executor.Finalize(runResult, nil)
	if len(finalized.Blocks) == 0 {
		t.Fatal("expected blocks after finalization")
	}
}

func TestScheduler_ExecuteTask_CancelledContext(t *testing.T) {
	setupSchedulerForExecuteTask(t)

	sid, err := CreateSession("/test", "codebuddy", "Cancel Test", "test-agent", "", "default", "scheduled")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	createStreamingPlaceholder("/test", "codebuddy", sid)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "scheduled prompt", ScheduledExecution: true},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Channel closes without terminal event (simulates cancellation)
	ch := make(chan ai.StreamEvent)
	close(ch)

	runResult := executor.RunWithChannel(ch)

	// Scheduler checks ctx.Err() == context.Canceled (line 710)
	if ctx.Err() != context.Canceled {
		t.Fatal("expected context.Canceled")
	}
	if runResult.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=false for cancelled context")
	}
}

func TestScheduler_ExecuteTask_CrashedProcess(t *testing.T) {
	setupSchedulerForExecuteTask(t)

	sid, err := CreateSession("/test", "codebuddy", "Crash Test", "test-agent", "", "default", "scheduled")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	createStreamingPlaceholder("/test", "codebuddy", sid)

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "scheduled prompt", ScheduledExecution: true},
	}
	executor := NewSessionExecutor(context.Background(), cfg)

	// Channel closes without done/error (simulates CLI crash)
	ch := make(chan ai.StreamEvent)
	close(ch)

	runResult := executor.RunWithChannel(ch)

	// Scheduler checks !runResult.ReceivedTerminal → marks as failed (line 726)
	if runResult.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=false for crashed process")
	}
	// Verify finalize still works for crashed execution
	_ = executor.Finalize(runResult, nil)
}

func TestScheduler_ExecuteTask_WithMetadata(t *testing.T) {
	setupSchedulerForExecuteTask(t)

	sid, err := CreateSession("/test", "codebuddy", "Metadata Test", "test-agent", "", "default", "scheduled")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	createStreamingPlaceholder("/test", "codebuddy", sid)

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "scheduled prompt", ScheduledExecution: true},
	}
	executor := NewSessionExecutor(context.Background(), cfg)

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

	runResult := executor.RunWithChannel(ch)
	finalized := executor.Finalize(runResult, nil)

	if !runResult.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true")
	}
	if finalized.Metadata == nil || finalized.Metadata.Model != "test-model" {
		t.Fatal("expected metadata with model")
	}
}
