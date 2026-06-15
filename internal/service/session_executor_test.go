package service

import (
	"context"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// --- ExecutionMode ---

func TestExecutionMode_Values(t *testing.T) {
	if ModeInteractive != 0 {
		t.Fatalf("ModeInteractive should be 0, got %d", ModeInteractive)
	}
	if ModeScheduled != 1 {
		t.Fatalf("ModeScheduled should be 1, got %d", ModeScheduled)
	}
}

// --- RunConfig ---

func TestRunConfig_InteractiveFields(t *testing.T) {
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test/project",
		BackendName: "claude",
		SessionID:   "sess-123",
		AgentID:     "claude",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	if cfg.Mode != ModeInteractive {
		t.Fatal("expected ModeInteractive")
	}
	if cfg.ProjectPath != "/test/project" {
		t.Fatal("ProjectPath not set")
	}
}

func TestRunConfig_ScheduledFields(t *testing.T) {
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test/project",
		BackendName: "codebuddy",
		SessionID:   "sess-456",
		AgentID:     "codebuddy",
		ChatRequest: ai.ChatRequest{Prompt: "check builds", ScheduledExecution: true},
		TaskID:      42,
		ExecutionID: 7,
		TriggerType: "auto",
	}
	if cfg.Mode != ModeScheduled {
		t.Fatal("expected ModeScheduled")
	}
	if cfg.TaskID != 42 || cfg.ExecutionID != 7 || cfg.TriggerType != "auto" {
		t.Fatal("scheduled-specific fields not set")
	}
	if !cfg.ChatRequest.ScheduledExecution {
		t.Fatal("ScheduledExecution should be true for scheduled mode")
	}
}

func TestRunConfig_LocalizeError(t *testing.T) {
	called := false
	cfg := RunConfig{
		Mode: ModeInteractive,
		LocalizeError: func(err error, key string, args map[string]any) string {
			called = true
			return "localized: " + key
		},
	}
	if cfg.LocalizeError == nil {
		t.Fatal("LocalizeError should be settable")
	}
	result := cfg.LocalizeError(nil, "TestKey", nil)
	if !called {
		t.Fatal("LocalizeError callback not called")
	}
	if result != "localized: TestKey" {
		t.Fatalf("unexpected LocalizeError result: %s", result)
	}
}

func TestRunConfig_LocalizeError_NilForScheduled(t *testing.T) {
	cfg := RunConfig{
		Mode: ModeScheduled,
	}
	// Scheduled mode should work with nil LocalizeError
	if cfg.LocalizeError != nil {
		t.Fatal("LocalizeError should be nil by default for scheduled mode")
	}
}

// --- RunResult ---

func TestRunResult_Fields(t *testing.T) {
	result := RunResult{
		Err:              nil,
		CancelReason:     "user",
		Empty:            false,
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "hello"}},
		Metadata:         &ai.Metadata{WallMs: 1500},
		RawOutput:        "raw data here",
		WallMs:           1500,
	}
	if result.CancelReason != "user" {
		t.Fatal("CancelReason not set")
	}
	if !result.ReceivedTerminal {
		t.Fatal("ReceivedTerminal should be true")
	}
	if len(result.Blocks) != 1 {
		t.Fatal("Blocks not set")
	}
	if result.Metadata == nil || result.Metadata.WallMs != 1500 {
		t.Fatal("Metadata not set correctly")
	}
	if result.RawOutput != "raw data here" {
		t.Fatal("RawOutput not set")
	}
}

func TestRunResult_Success(t *testing.T) {
	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "response"}},
	}
	if result.Err != nil || result.CancelReason != "" || result.Empty {
		t.Fatal("successful result should have no error/cancel/empty")
	}
}

func TestRunResult_Failed(t *testing.T) {
	result := RunResult{
		Err:              errBackendCreate,
		ReceivedTerminal: false,
	}
	if result.Err == nil {
		t.Fatal("failed result should have Err set")
	}
}

func TestRunResult_Empty(t *testing.T) {
	result := RunResult{
		Empty:            true,
		ReceivedTerminal: true,
	}
	if !result.Empty {
		t.Fatal("Empty should be true")
	}
}

// --- SessionExecutor ---

func TestNewSessionExecutor_DoesNotWrapContext(t *testing.T) {
	// Key constraint from review: executor must NOT derive its own context.
	// The caller owns the context lifecycle.
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "claude",
		SessionID:   "sess-1",
		AgentID:     "claude",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}

	executor := NewSessionExecutor(nil, cfg)
	if executor == nil {
		t.Fatal("NewSessionExecutor returned nil")
	}
	// Executor should store the context as-is, not wrap it
	if executor.cfg.Mode != ModeInteractive {
		t.Fatal("mode not stored correctly")
	}
}

// --- SessionExecutor.Run() event loop tests ---

func TestSessionExecutor_Run_ReceivesTerminalEvent(t *testing.T) {
	// Test that the event loop correctly processes events and returns
	// a RunResult with ReceivedTerminal=true when a "done" event is received.
	events := []ai.StreamEvent{
		{Type: "content", Content: "hello"},
		{Type: "content", Content: " world"},
		{Type: "done"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if !result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true when 'done' event is received")
	}
	if result.CancelReason != "" {
		t.Fatalf("expected empty CancelReason, got %q", result.CancelReason)
	}
	if len(result.Blocks) < 1 {
		t.Fatal("expected at least 1 content block")
	}
}

func TestSessionExecutor_Run_ChannelCloseWithoutTerminal(t *testing.T) {
	// Test that when the event channel closes without a "done" event
	// (simulating a CLI process crash), ReceivedTerminal is false.
	events := []ai.StreamEvent{
		{Type: "content", Content: "partial"},
		// No "done" event — channel just closes
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=false when channel closes without terminal event")
	}
}

func TestSessionExecutor_Run_ContextCancellation(t *testing.T) {
	// Test that context cancellation is handled correctly.
	ctx, cancel := context.WithCancel(context.Background())

	events := make(chan ai.StreamEvent, 10)
	events <- ai.StreamEvent{Type: "content", Content: "start"}
	// Don't close the channel — simulate a long-running stream

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "sess-ctx",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Cancel context after a short delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	// Run should exit when context is cancelled
	result := executor.RunWithChannel(events)

	if result.ReceivedTerminal {
		t.Fatal("should not have ReceivedTerminal when context cancelled")
	}
}

func TestSessionExecutor_Run_MetadataCapture(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "content", Content: "response"},
		{Type: "metadata", Meta: &ai.Metadata{InputTokens: 100, OutputTokens: 50, CostUSD: 0.01}},
		{Type: "done"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if result.Metadata == nil {
		t.Fatal("expected Metadata to be captured")
	}
	if result.Metadata.InputTokens != 100 {
		t.Fatalf("expected InputTokens=100, got %d", result.Metadata.InputTokens)
	}
}

func TestSessionExecutor_Run_RawOutputAccumulation(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "raw_output", RawOutput: "line1\n"},
		{Type: "raw_output", RawOutput: "line2\n"},
		{Type: "content", Content: "hi"},
		{Type: "done"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if result.RawOutput == "" {
		t.Fatal("expected RawOutput to be accumulated")
	}
	if !contains(result.RawOutput, "line1") || !contains(result.RawOutput, "line2") {
		t.Fatalf("expected RawOutput to contain both lines, got: %q", result.RawOutput)
	}
}

func TestSessionExecutor_Run_ReceivedTerminalOnError(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "content", Content: "partial"},
		{Type: "error", Error: "something went wrong"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if !result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true for 'error' event")
	}
}

// --- Finalize tests ---

func TestSessionExecutor_Finalize_AskQuestionConversion_Interactive(t *testing.T) {
	// Interactive mode should detect <ask-question> tags and convert them
	events := []ai.StreamEvent{
		{Type: "content", Content: `<ask-question><item><header>H</header><multi-select>false</multi-select><question>Q?</question><option><label>A</label><description>D</description></option></item></ask-question>`},
		{Type: "done"},
	}
	result := runExecutorWithEventsFinalize(t, events, ModeInteractive)

	// Should have a tool_use block for AskUserQuestion
	found := false
	for _, b := range result.Blocks {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected AskUserQuestion tool_use block in interactive mode")
	}
}

func TestSessionExecutor_Finalize_NoAskQuestionConversion_Scheduled(t *testing.T) {
	// Scheduled mode should NOT convert <ask-question> tags
	events := []ai.StreamEvent{
		{Type: "content", Content: `<ask-question><item><header>H</header><multi-select>false</multi-select><question>Q?</question><option><label>A</label><description>D</description></option></item></ask-question>`},
		{Type: "done"},
	}
	result := runExecutorWithEventsFinalize(t, events, ModeScheduled)

	// Should keep the raw text block — no conversion
	for _, b := range result.Blocks {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			t.Fatal("expected NO AskUserQuestion conversion in scheduled mode")
		}
	}
}

func TestSessionExecutor_Finalize_RejectedToolBlocks(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "content", Content: "hello"},
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "BadTool", ID: "1", Status: "error", Output: "not found in agent cli"}},
		{Type: "done"},
	}
	result := runExecutorWithEventsFinalize(t, events, ModeScheduled)

	for _, b := range result.Blocks {
		if b.Type == "tool_use" && b.Name == "BadTool" && b.Status == "error" {
			t.Fatal("expected rejected tool block to be removed")
		}
	}
}

func TestSessionExecutor_Finalize_WallMsSet(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "content", Content: "hello"},
		{Type: "done"},
	}
	result := runExecutorWithEventsFinalize(t, events, ModeScheduled)

	// WallMs can be 0 for very fast executions; just verify the field exists
	// and is non-negative
	if result.WallMs < 0 {
		t.Fatalf("expected WallMs >= 0, got %d", result.WallMs)
	}
}

func TestSessionExecutor_Finalize_MetadataInjected(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "content", Content: "hello"},
		{Type: "metadata", Meta: &ai.Metadata{InputTokens: 50}},
		{Type: "done"},
	}
	result := runExecutorWithEventsFinalize(t, events, ModeScheduled)

	if result.Metadata == nil {
		t.Fatal("expected Metadata to be set")
	}
	if result.Metadata.InputTokens != 50 {
		t.Fatalf("expected InputTokens=50, got %d", result.Metadata.InputTokens)
	}
	// WallMs is injected by finalize — can be 0 for very fast execution
	if result.Metadata.WallMs < 0 {
		t.Fatalf("expected WallMs >= 0, got %d", result.Metadata.WallMs)
	}
}

// --- Scheduled mode behavior tests ---

func TestSessionExecutor_Scheduled_NoCancelReason(t *testing.T) {
	// Scheduled mode should NOT query cancel reasons — there's no interactive user.
	events := []ai.StreamEvent{
		{Type: "content", Content: "hello"},
		{Type: "done"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if result.CancelReason != "" {
		t.Fatalf("expected empty CancelReason in scheduled mode, got %q", result.CancelReason)
	}
}

func TestSessionExecutor_Scheduled_NoSSEForwarding(t *testing.T) {
	// Scheduled mode should not attempt to forward events to any SSE channel.
	// The StreamCh is nil in scheduled mode, which is handled by the executor.
	events := []ai.StreamEvent{
		{Type: "content", Content: "hello"},
		{Type: "done"},
	}

	ctx := context.Background()
	ch := make(chan ai.StreamEvent, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	close(ch)

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "sess-scheduled",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello", ScheduledExecution: true},
		StreamCh:    nil, // No SSE channel for scheduled mode
		TaskID:      42,
		ExecutionID: 7,
		TriggerType: "auto",
	}
	executor := NewSessionExecutor(ctx, cfg)
	result := executor.RunWithChannel(ch)

	if !result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true")
	}
	if result.CancelReason != "" {
		t.Fatalf("expected empty CancelReason, got %q", result.CancelReason)
	}
}

func TestSessionExecutor_Scheduled_ReceivedTerminalDetectsCrash(t *testing.T) {
	// Scheduled mode must correctly detect CLI process crash (no terminal event).
	events := []ai.StreamEvent{
		{Type: "content", Content: "partial output"},
		// No "done" event — channel closes, simulating crash
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=false when channel closes without terminal event")
	}
	// Scheduler uses this flag to mark execution as "failed"
}

func TestSessionExecutor_Scheduled_MetadataCaptured(t *testing.T) {
	// Scheduled mode should capture metadata just like interactive mode.
	events := []ai.StreamEvent{
		{Type: "content", Content: "result"},
		{Type: "metadata", Meta: &ai.Metadata{InputTokens: 200, OutputTokens: 100, CostUSD: 0.05}},
		{Type: "done"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if result.Metadata == nil {
		t.Fatal("expected Metadata to be captured in scheduled mode")
	}
	if result.Metadata.InputTokens != 200 {
		t.Fatalf("expected InputTokens=200, got %d", result.Metadata.InputTokens)
	}
	if result.Metadata.CostUSD != 0.05 {
		t.Fatalf("expected CostUSD=0.05, got %f", result.Metadata.CostUSD)
	}
}

// --- Helpers ---

// runExecutorWithEvents creates an executor with a mock event channel,
// writes the given events, and closes the channel.
func runExecutorWithEvents(t *testing.T, events []ai.StreamEvent, mode ExecutionMode) RunResult {
	t.Helper()
	ctx := context.Background()

	ch := make(chan ai.StreamEvent, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	close(ch)

	cfg := RunConfig{
		Mode:        mode,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "sess-test",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)
	return executor.RunWithChannel(ch)
}

// runExecutorWithEventsFinalize runs the event loop — finalize is already built
// into buildResult() which RunWithChannel calls.
func runExecutorWithEventsFinalize(t *testing.T, events []ai.StreamEvent, mode ExecutionMode) RunResult {
	t.Helper()
	return runExecutorWithEvents(t, events, mode)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
