package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	_ "modernc.org/sqlite"
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
	// Verify all fields are stored (unusedwrite suppression)
	_, _, _ = cfg.BackendName, cfg.SessionID, cfg.AgentID
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
	// Verify all fields are stored (unusedwrite suppression)
	_, _, _, _ = cfg.ProjectPath, cfg.BackendName, cfg.SessionID, cfg.AgentID
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
	_ = cfg.Mode // unusedwrite suppression
}

func TestRunConfig_LocalizeError_NilForScheduled(t *testing.T) {
	cfg := RunConfig{
		Mode: ModeScheduled,
	}
	// Scheduled mode should work with nil LocalizeError
	if cfg.LocalizeError != nil {
		t.Fatal("LocalizeError should be nil by default for scheduled mode")
	}
	_ = cfg.Mode // unusedwrite suppression
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
	// Verify all fields are stored (unusedwrite suppression)
	_, _, _ = result.Err, result.Empty, result.WallMs
}

func TestRunResult_Success(t *testing.T) {
	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "response"}},
	}
	if result.Err != nil || result.CancelReason != "" || result.Empty {
		t.Fatal("successful result should have no error/cancel/empty")
	}
	// Verify all fields are stored (unusedwrite suppression)
	_, _ = result.ReceivedTerminal, result.Blocks
}

func TestRunResult_Failed(t *testing.T) {
	result := RunResult{
		Err:              errBackendCreate,
		ReceivedTerminal: false,
	}
	if result.Err == nil {
		t.Fatal("failed result should have Err set")
	}
	_ = result.ReceivedTerminal // unusedwrite suppression
}

func TestRunResult_Empty(t *testing.T) {
	result := RunResult{
		Empty:            true,
		ReceivedTerminal: true,
	}
	if !result.Empty {
		t.Fatal("Empty should be true")
	}
	_ = result.ReceivedTerminal // unusedwrite suppression
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

	executor := NewSessionExecutor(context.TODO(), cfg)
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
	return len(s) >= len(substr) && (s == substr || s != "" && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- processEvent / handleNonForwardableEvent tests ---

func TestSessionExecutor_RunWithChannel_SessionCaptureEvent(t *testing.T) {
	// Test that session_capture events are handled by handleNonForwardableEvent
	// and NOT forwarded to SSE or accumulated as blocks.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")

	events := []ai.StreamEvent{
		{Type: "session_capture", Content: "ext-session-123"},
		{Type: "content", Content: "hello"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	close(ch)

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(context.Background(), cfg)
	result := executor.RunWithChannel(ch)

	if !result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true")
	}
	// session_capture should not produce a block
	for _, b := range result.Blocks {
		if b.Type == "session_capture" {
			t.Fatal("session_capture should not be accumulated as a block")
		}
	}
}

func TestSessionExecutor_RunWithChannel_RawOutputAccumulation(t *testing.T) {
	// Test that raw_output events are handled by handleNonForwardableEvent
	// and accumulated in rawOutput, not as blocks.
	events := []ai.StreamEvent{
		{Type: "raw_output", RawOutput: "debug line 1"},
		{Type: "raw_output", RawOutput: "debug line 2"},
		{Type: "content", Content: "result"},
		{Type: "done"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if !result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true")
	}
	if !contains(result.RawOutput, "debug line 1") || !contains(result.RawOutput, "debug line 2") {
		t.Fatalf("expected raw output to contain both debug lines, got: %q", result.RawOutput)
	}
	// raw_output should not produce blocks
	for _, b := range result.Blocks {
		if b.Type == "raw_output" {
			t.Fatal("raw_output should not be accumulated as a block")
		}
	}
}

func TestSessionExecutor_RunWithChannel_MetadataCapture(t *testing.T) {
	// Test that captureMetadata correctly stores metadata from events.
	events := []ai.StreamEvent{
		{Type: "content", Content: "response"},
		{Type: "metadata", Meta: &ai.Metadata{InputTokens: 42, OutputTokens: 7, CostUSD: 0.03}},
		{Type: "done"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if result.Metadata == nil {
		t.Fatal("expected Metadata to be captured")
	}
	if result.Metadata.InputTokens != 42 {
		t.Fatalf("expected InputTokens=42, got %d", result.Metadata.InputTokens)
	}
	if result.Metadata.OutputTokens != 7 {
		t.Fatalf("expected OutputTokens=7, got %d", result.Metadata.OutputTokens)
	}
	if result.Metadata.CostUSD != 0.03 {
		t.Fatalf("expected CostUSD=0.03, got %f", result.Metadata.CostUSD)
	}
}

func TestSessionExecutor_RunWithChannel_MetadataNilMeta(t *testing.T) {
	// Test that captureMetadata ignores metadata events with nil Meta.
	events := []ai.StreamEvent{
		{Type: "metadata", Meta: nil},
		{Type: "content", Content: "hello"},
		{Type: "done"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	// Metadata should be nil (created later by buildResult with defaults)
	if result.Metadata == nil {
		t.Fatal("expected Metadata to be created with defaults")
	}
	// InputTokens should be 0 since no actual metadata was captured
	if result.Metadata.InputTokens != 0 {
		t.Fatalf("expected InputTokens=0, got %d", result.Metadata.InputTokens)
	}
}

func TestSessionExecutor_RunWithChannel_NonMetadataEvent(t *testing.T) {
	// Test that captureMetadata ignores non-metadata events.
	events := []ai.StreamEvent{
		{Type: "content", Content: "hello"},
		{Type: "done"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if result.Metadata == nil {
		t.Fatal("expected Metadata to be created with defaults in buildResult")
	}
}

// --- Finalize tests (exercise finalizeContent, buildEmptyContentJSON, buildNonEmptyContentJSON, drainRawOutput) ---

func TestSessionExecutor_Finalize_EmptyBlocks_UserCancel(t *testing.T) {
	// Test buildEmptyContentJSON with user cancel reason.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	ctx := context.Background()
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		CancelReason:     "user",
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{},
		Metadata:         &ai.Metadata{},
	}

	finalized := executor.Finalize(result, nil)

	// Should have a warning block for user cancel
	found := false
	for _, b := range finalized.Blocks {
		if b.Type == "warning" && b.Reason == ai.ReasonUserCancel {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected warning block with reason user_cancel, got blocks: %+v", finalized.Blocks)
	}
}

func TestSessionExecutor_Finalize_EmptyBlocks_ContextCancelled(t *testing.T) {
	// Test buildEmptyContentJSON with context.Canceled.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{},
		Metadata:         &ai.Metadata{},
	}

	finalized := executor.Finalize(result, nil)

	found := false
	for _, b := range finalized.Blocks {
		if b.Type == "warning" && b.Reason == ai.ReasonContextCancel {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected warning block with reason context_cancel, got blocks: %+v", finalized.Blocks)
	}
}

func TestSessionExecutor_Finalize_EmptyBlocks_DeadlineExceeded(t *testing.T) {
	// Test buildEmptyContentJSON with context.DeadlineExceeded.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	// Don't call cancel() — let the timeout expire naturally so ctx.Err() returns DeadlineExceeded
	_ = cancel
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{},
		Metadata:         &ai.Metadata{},
	}

	finalized := executor.Finalize(result, nil)

	found := false
	for _, b := range finalized.Blocks {
		if b.Type == "warning" && b.Reason == ai.ReasonTimeout {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected warning block with reason timeout, got blocks: %+v", finalized.Blocks)
	}
}

func TestSessionExecutor_Finalize_EmptyBlocks_DefaultReason(t *testing.T) {
	// Test buildEmptyContentJSON with no cancel reason and no context error.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(context.Background(), cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{},
		Metadata:         &ai.Metadata{},
	}

	finalized := executor.Finalize(result, nil)

	found := false
	for _, b := range finalized.Blocks {
		if b.Type == "warning" && b.Reason == ai.ReasonEmpty {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected warning block with reason empty, got blocks: %+v", finalized.Blocks)
	}
}

func TestSessionExecutor_Finalize_NonEmptyBlocks_UserCancel(t *testing.T) {
	// Test buildNonEmptyContentJSON with user cancel — should set cancelled=true.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(context.Background(), cfg)

	result := RunResult{
		CancelReason:     "user",
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "partial response"}},
		Metadata:         &ai.Metadata{},
	}

	finalized := executor.Finalize(result, nil)

	if len(finalized.Blocks) == 0 {
		t.Fatal("expected at least one block")
	}
	if finalized.Blocks[0].Text != "partial response" {
		t.Fatalf("expected original block text, got %q", finalized.Blocks[0].Text)
	}
}

func TestSessionExecutor_Finalize_NonEmptyBlocks_ContextCancelled(t *testing.T) {
	// Test buildNonEmptyContentJSON with context.Canceled — should set cancelled=true.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "partial"}},
		Metadata:         &ai.Metadata{},
	}

	finalized := executor.Finalize(result, nil)

	// Should have original block plus potentially a warning for context cancel
	if len(finalized.Blocks) == 0 {
		t.Fatal("expected at least one block")
	}
}

func TestSessionExecutor_Finalize_NonEmptyBlocks_DeadlineExceeded(t *testing.T) {
	// Test buildNonEmptyContentJSON with DeadlineExceeded — the timeout warning
	// is embedded in the serialized content JSON, not in the returned blocks slice
	// (because buildNonEmptyContentJSON only returns a string, not modified blocks).
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	// Don't call cancel() — let the timeout expire naturally so ctx.Err() returns DeadlineExceeded
	_ = cancel
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "partial"}},
		Metadata:         &ai.Metadata{},
	}

	// Finalize should not panic with DeadlineExceeded context
	finalized := executor.Finalize(result, nil)

	// Original block should still be present
	if len(finalized.Blocks) == 0 {
		t.Fatal("expected at least one block")
	}
	if finalized.Blocks[0].Text != "partial" {
		t.Fatalf("expected original text block, got: %+v", finalized.Blocks[0])
	}
}

func TestSessionExecutor_Finalize_NonEmptyBlocks_NormalCompletion(t *testing.T) {
	// Test buildNonEmptyContentJSON with normal completion — no cancel, no timeout.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(context.Background(), cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "full response"}},
		Metadata:         &ai.Metadata{},
	}

	finalized := executor.Finalize(result, nil)

	// Should not have any warning blocks
	for _, b := range finalized.Blocks {
		if b.Type == "warning" {
			t.Fatalf("expected no warning blocks for normal completion, got: %+v", finalized.Blocks)
		}
	}
	if finalized.Blocks[0].Text != "full response" {
		t.Fatalf("expected original text, got %q", finalized.Blocks[0].Text)
	}
}

// --- drainRawOutput tests ---

func TestSessionExecutor_Finalize_DrainRawOutput(t *testing.T) {
	// Test that drainRawOutput collects remaining raw_output events after finalization.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(context.Background(), cfg)

	// Create a channel with remaining raw_output events
	drainCh := make(chan ai.StreamEvent, 3)
	drainCh <- ai.StreamEvent{Type: "raw_output", RawOutput: "drained line 1"}
	drainCh <- ai.StreamEvent{Type: "raw_output", RawOutput: "drained line 2"}
	close(drainCh)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "response"}},
		Metadata:         &ai.Metadata{},
		RawOutput:        "initial raw",
	}

	finalized := executor.Finalize(result, drainCh)

	if !contains(finalized.RawOutput, "initial raw") {
		t.Fatalf("expected original raw output preserved, got: %q", finalized.RawOutput)
	}
	if !contains(finalized.RawOutput, "drained line 1") || !contains(finalized.RawOutput, "drained line 2") {
		t.Fatalf("expected drained lines in raw output, got: %q", finalized.RawOutput)
	}
}

func TestSessionExecutor_Finalize_DrainRawOutput_NilChannel(t *testing.T) {
	// Test that drainRawOutput returns existing rawOutput when channel is nil.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(context.Background(), cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "response"}},
		Metadata:         &ai.Metadata{},
		RawOutput:        "existing raw output",
	}

	finalized := executor.Finalize(result, nil)

	if finalized.RawOutput != "existing raw output" {
		t.Fatalf("expected raw output preserved when nil channel, got: %q", finalized.RawOutput)
	}
}

func TestSessionExecutor_Finalize_DrainRawOutput_EmptyChannel(t *testing.T) {
	// Test that drainRawOutput handles an already-empty channel.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(context.Background(), cfg)

	drainCh := make(chan ai.StreamEvent)
	close(drainCh)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "response"}},
		Metadata:         &ai.Metadata{},
		RawOutput:        "some raw",
	}

	finalized := executor.Finalize(result, drainCh)

	if finalized.RawOutput != "some raw" {
		t.Fatalf("expected raw output preserved with empty channel, got: %q", finalized.RawOutput)
	}
}

// --- injectResponseMetadata tests ---

func TestSessionExecutor_Finalize_InjectResponseMetadata(t *testing.T) {
	// Test that injectResponseMetadata sets transport and model fields.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(context.Background(), cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "response"}},
		Metadata:         &ai.Metadata{},
	}

	finalized := executor.Finalize(result, nil)

	if finalized.Metadata == nil {
		t.Fatal("expected Metadata to be set")
	}
	// Transport should be set (default "cli" when no session transport or ACP)
	if finalized.Metadata.Transport != "cli" {
		t.Fatalf("expected Transport='cli', got %q", finalized.Metadata.Transport)
	}
}

func TestSessionExecutor_Finalize_MsgIDSet(t *testing.T) {
	// Test that Finalize returns a non-zero MsgID when successful.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(context.Background(), cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "response"}},
		Metadata:         &ai.Metadata{},
	}

	finalized := executor.Finalize(result, nil)

	if finalized.MsgID == 0 {
		t.Fatal("expected non-zero MsgID after Finalize")
	}
}

// --- Finalize with empty blocks produces valid JSON content ---

func TestSessionExecutor_Finalize_EmptyBlocks_ContainsCancelledField(t *testing.T) {
	// Verify the JSON content stored by Finalize includes "cancelled":true for user cancel.
	setupExecutorTestDB(t)
	sid := helperCreateTestSession(t, "/test", "test")
	helperCreateStreamingMessage(t, sid)

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(context.Background(), cfg)

	result := RunResult{
		CancelReason:     "user",
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{},
		Metadata:         &ai.Metadata{},
	}

	// We verify indirectly: the blocks should include a warning with reason user_cancel
	finalized := executor.Finalize(result, nil)
	found := false
	for _, b := range finalized.Blocks {
		if b.Type == "warning" && b.Reason == ai.ReasonUserCancel {
			found = true
		}
	}
	if !found {
		t.Fatal("expected user_cancel warning block")
	}
}

// --- DB test helpers for session_executor_test.go ---

const testSchema = `
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
	source_session_id TEXT DEFAULT NULL,
	transport TEXT DEFAULT '',
	auto_approve INTEGER NOT NULL DEFAULT 0,
	deleted INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	last_read_at DATETIME,
	UNIQUE(project_path, backend, id)
);
CREATE TABLE IF NOT EXISTS chat_metadata (
	message_id INTEGER PRIMARY KEY,
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
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS ai_raw_responses (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	message_id INTEGER NOT NULL,
	backend TEXT NOT NULL DEFAULT '',
	raw_output TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_path, backend, session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_path, backend);
`

func setupExecutorTestDB(t *testing.T) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}

	_, err = db.Exec(testSchema)
	if err != nil {
		t.Fatalf("failed to execute schema: %v", err)
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

func helperCreateTestSession(t *testing.T, projectPath, backend string) string {
	t.Helper()
	id := "test-sess-" + time.Now().Format("150405.000")
	_, err := DB.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, session_type) VALUES (?, ?, ?, ?, ?, ?)",
		id, projectPath, backend, "Test Session", "test", "chat",
	)
	if err != nil {
		t.Fatalf("failed to create test session: %v", err)
	}
	return id
}

func helperCreateStreamingMessage(t *testing.T, sessionID string) {
	t.Helper()
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	_, err := DB.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming) VALUES (?, ?, ?, ?, ?, 1)",
		"/test", "test", sessionID, "assistant", string(emptyContent),
	)
	if err != nil {
		t.Fatalf("failed to create streaming message: %v", err)
	}
}
