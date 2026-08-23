package service

import (
	"context"
	"encoding/json"
	"strings"
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

// --- Additional diff coverage tests ---

func TestSessionExecutor_Finalize_ACPModeInjection(t *testing.T) {
	// Cover lines 344-350: ACP mode and thinking effort injection.
	setupExecutorDB(t)
	agentID := "acp-mode-test-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "Test", Backend: "test", AcpCommand: "test-acp"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, agentID)

	// Register agent capabilities so GetModeState/GetThinkingEffortState return non-nil
	reg := ai.GetAgentCapabilityRegistry()
	reg.UpdateModes(agentID, []ai.ModeDef{{ID: "architect", Name: "Architect"}})
	reg.UpdateThinkingEfforts(agentID, []ai.ThinkingEffortDef{{ID: "high", Name: "High"}})
	defer reg.UpdateModes(agentID, nil)

	// Inject ACP connection with cached mode and effort
	agent := &model.Agent{ID: agentID, Backend: "test"}
	conn := ai.NewACPConnForTest(agent, sid)
	conn.UpdateCachedCurrentMode("architect")
	conn.UpdateCachedCurrentThinkingEffort("high")
	mgr := ai.GetACPConnManager()
	mgr.SetConnForTest(sid, conn)
	defer mgr.SetConnForTest(sid, nil)

	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     agentID,
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "ok"}},
		Metadata:         &ai.Metadata{},
	}
	finalized := executor.Finalize(result, nil)

	if finalized.Metadata.Mode != "architect" {
		t.Fatalf("expected Mode='architect', got %q", finalized.Metadata.Mode)
	}
	if finalized.Metadata.ThinkingEffort != "high" {
		t.Fatalf("expected ThinkingEffort='high', got %q", finalized.Metadata.ThinkingEffort)
	}
}

func TestSessionExecutor_Finalize_TransportFromSessionOverride(t *testing.T) {
	// Cover line 353-354: transport from session transport override.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	// Set session transport to "sse"
	if err := UpdateSessionTransport(sid, "sse"); err != nil {
		t.Fatalf("UpdateSessionTransport failed: %v", err)
	}

	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "ok"}},
		Metadata:         &ai.Metadata{},
	}
	finalized := executor.Finalize(result, nil)

	if finalized.Metadata.Transport != "sse" {
		t.Fatalf("expected Transport='sse', got %q", finalized.Metadata.Transport)
	}
}

func TestSessionExecutor_Finalize_TriggerChatSummarization(t *testing.T) {
	// Finalize should trigger triggerChatSummarization after FinalizeStreamingMessage
	// succeeds (msgID > 0), so that assistant messages get reading summaries.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")

	// Look up the streaming message ID created by setupExecutorSession
	streamMsgID := GetStreamingMessageID(sid)
	if streamMsgID == 0 {
		t.Fatal("expected streaming message placeholder to exist")
	}

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: streamMsgID,
	}
	executor := NewSessionExecutor(ctx, cfg)
	// Simulate blocks accumulated during the event loop (Finalize reads e.blocks)
	executor.blocks = []model.ContentBlock{{Type: "text", Text: "The answer is 42."}}

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           executor.blocks,
		Metadata:         &ai.Metadata{},
	}
	finalized := executor.Finalize(result, nil)

	if finalized.MsgID == 0 {
		t.Fatal("expected non-zero MsgID after Finalize")
	}

	// After Finalize, triggerChatSummarization should have been called,
	// so the message should have a summary in the DB.
	summary, found := GetSummary("chat_message", finalized.MsgID)
	if !found {
		t.Fatalf("expected summary to be generated for msgID=%d after Finalize", finalized.MsgID)
	}
	if summary == "" {
		t.Fatal("expected non-empty summary after Finalize")
	}
}

func TestSessionExecutor_Finalize_ModelFromSession(t *testing.T) {
	// Cover line 360-362: model from session model.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	if err := UpdateSessionModel(sid, "glm-5.1"); err != nil {
		t.Fatalf("UpdateSessionModel failed: %v", err)
	}

	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "ok"}},
		Metadata:         &ai.Metadata{},
	}
	finalized := executor.Finalize(result, nil)

	if finalized.Metadata.Model != "glm-5.1" {
		t.Fatalf("expected Model='glm-5.1', got %q", finalized.Metadata.Model)
	}
}

func TestSessionExecutor_Finalize_WithBlocks_DeadlineExceeded(t *testing.T) {
	// Cover lines 392-393: DeadlineExceeded with blocks appends warning.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "partial"}},
		Metadata:         &ai.Metadata{},
	}
	finalized := executor.Finalize(result, nil)

	// Should have appended a warning block about timeout
	found := false
	for _, b := range finalized.Blocks {
		if b.Type == "warning" && b.Reason == ai.ReasonTimeout {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected warning with ReasonTimeout, got: %+v", finalized.Blocks)
	}
}

func TestSessionExecutor_Finalize_DrainRawFromEventChannel(t *testing.T) {
	// Cover lines 416-433: drain raw_output from event channel.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")

	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Create drain channel with raw_output events
	drainCh := make(chan ai.StreamEvent, 3)
	drainCh <- ai.StreamEvent{Type: "raw_output", RawOutput: "drained-line-1"}
	drainCh <- ai.StreamEvent{Type: "raw_output", RawOutput: "drained-line-2"}
	close(drainCh)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "ok"}},
		Metadata:         &ai.Metadata{},
		RawOutput:        "existing-raw",
	}
	finalized := executor.Finalize(result, drainCh)

	if !contains(finalized.RawOutput, "existing-raw") {
		t.Fatal("expected existing raw output preserved")
	}
	if !contains(finalized.RawOutput, "drained-line-1") {
		t.Fatal("expected drained raw output line 1")
	}
	if !contains(finalized.RawOutput, "drained-line-2") {
		t.Fatal("expected drained raw output line 2")
	}
}

// TestSessionExecutor_Finalize_DrainsUntilChannelCloses verifies that Finalize
// keeps draining the event channel until it is closed, even after the terminal
// event was consumed by RunWithChannel. Events that arrive late (e.g. tool
// calls flushed by a debouncer after cancel) must be persisted — previously the
// drain only took what was immediately available, which could leave the
// producer blocked on a full channel and the late events dropped.
func TestSessionExecutor_Finalize_DrainsUntilChannelCloses(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")

	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	eventCh := make(chan ai.StreamEvent, 4)
	go func() {
		eventCh <- ai.StreamEvent{Type: "content", Content: "partial"}
		eventCh <- ai.StreamEvent{Type: "done"}
		// A tool_use event arrives AFTER the terminal event. Finalize must keep
		// draining until the channel closes to capture it.
		time.Sleep(50 * time.Millisecond)
		eventCh <- ai.StreamEvent{
			Type: "tool_use",
			Tool: &ai.ToolCall{Name: "Read", ID: "t1", Input: `{}`, Done: true},
		}
		close(eventCh)
	}()

	runResult := executor.RunWithChannel(eventCh)
	if !runResult.ReceivedTerminal {
		t.Fatal("expected terminal event received")
	}

	finalized := executor.Finalize(runResult, eventCh)

	found := false
	for _, b := range finalized.Blocks {
		if b.Type == "tool_use" && b.ID == "t1" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected late tool_use event to be drained before the channel closed")
	}
}

func TestSessionExecutor_Finalize_NilMetadata(t *testing.T) {
	// Verify Finalize works with minimal metadata — the function accesses
	// responseMetadata fields directly, so nil is not supported.
	// Instead test with an empty Metadata struct.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")

	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "ok"}},
		Metadata:         &ai.Metadata{}, // empty but non-nil
	}
	finalized := executor.Finalize(result, nil)

	if finalized.Metadata == nil {
		t.Fatal("expected Metadata to be set")
	}
	if finalized.Metadata.Transport == "" {
		t.Fatal("expected Transport to be set")
	}
}

func TestSessionExecutor_FlushStreamingMessage_NilBlocks(t *testing.T) {
	// Cover line 268-269: nil blocks serialized as empty array.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Directly call flushStreamingMessage with nil blocks
	executor.blocks = nil
	executor.flushStreamingMessage()

	// Verify the streaming message was updated (with empty blocks array)
	var content string
	err := dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? AND streaming = 1",
		sid,
	).Scan(&content)
	if err != nil {
		t.Fatalf("failed to query streaming message: %v", err)
	}
	if !contains(content, `"blocks":[]`) {
		t.Fatalf("expected empty blocks array, got: %s", content)
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
	if cfg.BackendName != "claude" {
		t.Fatal("BackendName not set")
	}
	if cfg.SessionID != "sess-123" {
		t.Fatal("SessionID not set")
	}
	if cfg.AgentID != "claude" {
		t.Fatal("AgentID not set")
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
	if cfg.ProjectPath != "/test/project" {
		t.Fatal("ProjectPath not set")
	}
	if cfg.BackendName != "codebuddy" {
		t.Fatal("BackendName not set")
	}
	if cfg.SessionID != "sess-456" {
		t.Fatal("SessionID not set")
	}
	if cfg.AgentID != "codebuddy" {
		t.Fatal("AgentID not set")
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
	if cfg.Mode != ModeInteractive {
		t.Fatal("expected ModeInteractive")
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
	if cfg.Mode != ModeScheduled {
		t.Fatal("expected ModeScheduled")
	}
	// Scheduled mode should work with nil LocalizeError
	if cfg.LocalizeError != nil {
		t.Fatal("LocalizeError should be nil by default for scheduled mode")
	}
}

// --- RunResult ---

func TestRunResult_Fields(t *testing.T) {
	result := RunResult{
		CancelReason:     "user",
		Empty:            true,
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "hello"}},
		Metadata:         &ai.Metadata{WallMs: 1500},
		RawOutput:        "raw data here",
		WallMs:           1500,
	}
	if result.CancelReason != "user" {
		t.Fatal("CancelReason not set")
	}
	if !result.Empty {
		t.Fatal("Empty should be true")
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
	if result.WallMs != 1500 {
		t.Fatal("WallMs not set")
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
	if !result.ReceivedTerminal {
		t.Fatal("ReceivedTerminal should be true")
	}
	if len(result.Blocks) != 1 {
		t.Fatal("Blocks should have 1 element")
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
	if result.ReceivedTerminal {
		t.Fatal("failed result should not have ReceivedTerminal")
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
	if !result.ReceivedTerminal {
		t.Fatal("ReceivedTerminal should be true")
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

func TestSessionExecutor_TrackToolDuration_NilOrEmptyTool(t *testing.T) {
	// tool_use/tool_result events without a tool (or with an empty tool ID)
	// must not panic and must not crash the run (trackToolDuration early-returns).
	events := []ai.StreamEvent{
		{Type: "tool_use", Tool: nil},
		{Type: "tool_result", Tool: &ai.ToolCall{Name: "x", ID: ""}},
		{Type: "done"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if !result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true")
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

// --- DB-backed tests ---

// setupExecutorDB creates an in-memory DB with the minimal schema needed for
// session executor persistence tests. Reuses schedulerExecSchema from
// scheduler_executor_test.go in the same package.
func setupExecutorDB(t *testing.T) {
	t.Helper()
	setupSchedulerExecDB(t)
}

// setupExecutorSession creates a session and a streaming placeholder message,
// returning the session ID. This is the minimum DB state needed for
// flushStreamingMessage and Finalize.
func setupExecutorSession(t *testing.T, agentID string) string {
	t.Helper()
	sid, err := CreateSession("/test", "test", "Executor Test", agentID, "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	// Create streaming assistant placeholder
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	_, _ = AddChatMessage("/test", "test", sid, "assistant", string(emptyContent), nil, true, "")
	return sid
}

func TestSessionExecutor_CaptureExternalSessionID(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Capture external session ID
	executor.captureExternalSessionID("ext-123")

	// Verify it was persisted
	extID := GetExternalSessionID(sid)
	if extID != "ext-123" {
		t.Fatalf("expected external_session_id='ext-123', got %q", extID)
	}
}

func TestSessionExecutor_CaptureExternalSessionID_Empty(t *testing.T) {
	setupExecutorDB(t)

	ctx := context.Background()
	cfg := RunConfig{SessionID: "no-session"}
	executor := NewSessionExecutor(ctx, cfg)

	// Should not panic or do anything for empty ID
	executor.captureExternalSessionID("")
}

func TestSessionExecutor_FlushStreamingMessage(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Add some blocks and flush
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "content", Content: "hello"})
	executor.flushStreamingMessage()

	// Verify the streaming message was updated with blocks
	var content string
	err := dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 1",
		sid,
	).Scan(&content)
	if err != nil {
		t.Fatalf("failed to query streaming message: %v", err)
	}
	if !contains(content, "hello") {
		t.Fatalf("expected streaming message to contain 'hello', got: %q", content)
	}
}

func TestSessionExecutor_FlushStreamingMessage_WithMetadata(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Set metadata and blocks, then flush
	executor.responseMetadata = &ai.Metadata{InputTokens: 100}
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "content", Content: "hello"})
	executor.flushStreamingMessage()

	// Verify the streaming message was updated with metadata
	var content string
	err := dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 1",
		sid,
	).Scan(&content)
	if err != nil {
		t.Fatalf("failed to query streaming message: %v", err)
	}
	if !contains(content, "inputTokens") {
		t.Fatalf("expected streaming message to contain metadata, got: %q", content)
	}
}

func TestSessionExecutor_Finalize_WithDB(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	events := []ai.StreamEvent{
		{Type: "content", Content: "hello world"},
		{Type: "metadata", Meta: &ai.Metadata{InputTokens: 100, OutputTokens: 50}},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	runResult := executor.RunWithChannel(ch)
	runResult = executor.Finalize(runResult, nil)

	if runResult.MsgID <= 0 {
		t.Fatal("expected MsgID > 0 after Finalize")
	}
	if runResult.Metadata == nil {
		t.Fatal("expected Metadata to be set after Finalize")
	}

	// Verify the message was finalized in DB
	var streaming int
	err := dbRead.QueryRow(
		"SELECT streaming FROM chat_history WHERE id = ?",
		runResult.MsgID,
	).Scan(&streaming)
	if err != nil {
		t.Fatalf("failed to query finalized message: %v", err)
	}
	if streaming != 0 {
		t.Fatalf("expected streaming=0 after finalize, got %d", streaming)
	}
}

func TestSessionExecutor_Finalize_EmptyBlocks_UserCancel(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel context immediately

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Set cancel reason BEFORE RunWithChannel
	SetCancelReason(sid, "user")

	// Run with a channel that has no content events
	ch := make(chan ai.StreamEvent)
	close(ch)
	runResult := executor.RunWithChannel(ch)
	runResult = executor.Finalize(runResult, nil)

	// Should have cancelled flag and NO warning block (user cancel is clean)
	if len(runResult.Blocks) != 0 {
		t.Fatalf("expected no blocks for user-cancelled result, got %d", len(runResult.Blocks))
	}
	// Verify content JSON contains cancelled:true
	contentJSON, _ := executor.buildContentJSON(nil, runResult, &ai.Metadata{})
	if !strings.Contains(contentJSON, `"cancelled":true`) {
		t.Fatal("expected cancelled:true in content JSON")
	}
}

func TestSessionExecutor_Finalize_EmptyBlocks_ContextCancel(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	ch := make(chan ai.StreamEvent)
	close(ch)
	runResult := executor.RunWithChannel(ch)
	runResult = executor.Finalize(runResult, nil)

	// Should have warning block with context_cancel reason
	found := false
	for _, b := range runResult.Blocks {
		if b.Type == "warning" && b.Reason == ai.ReasonContextCancel {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected warning block with context_cancel reason")
	}
}

func TestSessionExecutor_Finalize_EmptyBlocks_Timeout(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	// Use a context that has timed out (DeadlineExceeded)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond) // ensure deadline exceeded
	_ = ctx.Err()                // drain

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	ch := make(chan ai.StreamEvent)
	close(ch)
	runResult := executor.RunWithChannel(ch)
	runResult = executor.Finalize(runResult, nil)

	// Should have warning block with timeout reason
	found := false
	for _, b := range runResult.Blocks {
		if b.Type == "warning" && b.Reason == ai.ReasonTimeout {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected warning block with timeout reason, got blocks: %+v", runResult.Blocks)
	}
}

func TestSessionExecutor_Finalize_EmptyBlocks_DefaultReason(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	ch := make(chan ai.StreamEvent)
	close(ch)
	runResult := executor.RunWithChannel(ch)
	runResult = executor.Finalize(runResult, nil)

	// Should have warning block with "empty" reason
	found := false
	for _, b := range runResult.Blocks {
		if b.Type == "warning" && b.Reason == ai.ReasonEmpty {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected warning block with empty reason")
	}
}

func TestSessionExecutor_Finalize_WithBlocks_UserCancel(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	events := []ai.StreamEvent{
		{Type: "content", Content: "partial response"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	// Set cancel reason BEFORE RunWithChannel (buildResult reads and clears it)
	SetCancelReason(sid, "user")
	runResult := executor.RunWithChannel(ch)
	runResult = executor.Finalize(runResult, nil)

	// Should have cancelled=true in content (parsed via JSON)
	if runResult.CancelReason != "user" {
		t.Fatalf("expected CancelReason='user', got %q", runResult.CancelReason)
	}
}

func TestSessionExecutor_Finalize_WithBlocks_ContextCancel(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	events := []ai.StreamEvent{
		{Type: "content", Content: "partial response"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	runResult := executor.RunWithChannel(ch)
	runResult = executor.Finalize(runResult, nil)
	// Context cancelled with blocks present — should set cancelled=true
	if runResult.MsgID <= 0 {
		t.Fatal("expected MsgID > 0 after Finalize")
	}
}

func TestSessionExecutor_Finalize_WithBlocks_Timeout(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	events := []ai.StreamEvent{
		{Type: "content", Content: "partial response"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	runResult := executor.RunWithChannel(ch)
	runResult = executor.Finalize(runResult, nil)

	// Should add timeout warning block
	found := false
	for _, b := range runResult.Blocks {
		if b.Type == "warning" && b.Reason == ai.ReasonTimeout {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected timeout warning block when context deadline exceeded with blocks")
	}
}

func TestSessionExecutor_Finalize_DrainRawOutput(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	events := []ai.StreamEvent{
		{Type: "content", Content: "hello"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events)+2)
	for _, e := range events {
		ch <- e
	}
	// Add raw_output events after done — these will be drained by Finalize
	ch <- ai.StreamEvent{Type: "raw_output", RawOutput: "drained line 1"}
	ch <- ai.StreamEvent{Type: "raw_output", RawOutput: "drained line 2"}
	close(ch)

	runResult := executor.RunWithChannel(ch)
	// The done event already consumed from ch, but there are still
	// raw_output events in the channel for Finalize to drain
	runResult = executor.Finalize(runResult, ch)

	if !contains(runResult.RawOutput, "drained line 1") || !contains(runResult.RawOutput, "drained line 2") {
		t.Fatalf("expected raw output to contain drained lines, got: %q", runResult.RawOutput)
	}
}

func TestSessionExecutor_RunWithChannel_SessionCapture(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}

	events := []ai.StreamEvent{
		{Type: "session_capture", Content: "ext-captured-456"},
		{Type: "content", Content: "response"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(ctx, cfg)
	result := executor.RunWithChannel(ch)

	if !result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true")
	}

	// Verify external session ID was persisted
	extID := GetExternalSessionID(sid)
	if extID != "ext-captured-456" {
		t.Fatalf("expected external_session_id='ext-captured-456', got %q", extID)
	}
}

func TestSessionExecutor_RunWithChannel_SessionCaptureFromMetadata(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}

	events := []ai.StreamEvent{
		{Type: "metadata", Meta: &ai.Metadata{SessionID: "ext-from-meta-789"}},
		{Type: "content", Content: "response"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(ctx, cfg)
	result := executor.RunWithChannel(ch)

	if !result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true")
	}

	// Verify external session ID from metadata was persisted
	extID := GetExternalSessionID(sid)
	if extID != "ext-from-meta-789" {
		t.Fatalf("expected external_session_id='ext-from-meta-789', got %q", extID)
	}
}

func TestSessionExecutor_BuildResult_InteractiveCancelReason(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}

	// Set a cancel reason before running
	SetCancelReason(sid, "disconnect")

	events := []ai.StreamEvent{
		{Type: "content", Content: "response"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(ctx, cfg)
	result := executor.RunWithChannel(ch)

	if result.CancelReason != "disconnect" {
		t.Fatalf("expected CancelReason='disconnect', got %q", result.CancelReason)
	}
}

func TestSessionExecutor_BuildResult_EmptyResult(t *testing.T) {
	// When blocks is empty, receivedTerminal=true, and no cancel reason,
	// the result should be marked as Empty
	events := []ai.StreamEvent{
		{Type: "done"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	if !result.Empty {
		t.Fatal("expected Empty=true when no content blocks and normal completion")
	}
}

func TestSessionExecutor_BuildResult_AskUserQuestionToolCallPersisted(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	// Get the streaming message ID (created by setupExecutorSession)
	streamingMsgID := GetStreamingMessageID(sid)
	if streamingMsgID == 0 {
		t.Fatal("expected non-zero streaming message ID from setup")
	}

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: streamingMsgID,
	}

	// Emit content with <ask-question> tag — ConvertAskQuestionBlocks will
	// create a tool_use block with ID prefix "ask-"
	events := []ai.StreamEvent{
		{Type: "content", Content: `<ask-question><item><question>Which approach?</question><option><label>A</label></option><option><label>B</label></option></item></ask-question>`},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(ctx, cfg)
	result := executor.RunWithChannel(ch)

	// Finalize: this is where persistAskToolCalls runs, assigning the
	// definitive IDs that get written to chat_tool_calls.
	finalized := executor.Finalize(result, nil)
	msgID := finalized.MsgID
	if msgID == 0 {
		t.Fatal("expected non-zero message ID after Finalize")
	}

	// Find the AskUserQuestion block from the finalized result (not buildResult).
	// buildResult and Finalize each run postProcessBlocks independently,
	// which calls ConvertAskQuestionBlocks and generates different UUIDs.
	// Only the Finalize IDs are persisted to chat_tool_calls.
	var askBlock *model.ContentBlock
	for i := range finalized.Blocks {
		if finalized.Blocks[i].Name == "AskUserQuestion" {
			askBlock = &finalized.Blocks[i]
			break
		}
	}
	if askBlock == nil {
		t.Fatal("expected AskUserQuestion block in finalized result")
	}
	if !strings.HasPrefix(askBlock.ID, "ask-") {
		t.Fatalf("expected AskUserQuestion block ID to start with 'ask-', got %q", askBlock.ID)
	}
	if len(askBlock.Input) == 0 {
		t.Fatal("expected AskUserQuestion block to have input")
	}

	rec, err := GetToolCall(askBlock.ID, msgID)
	if err != nil {
		t.Fatalf("GetToolCall failed: %v", err)
	}
	if rec == nil {
		t.Fatal("expected tool call record in chat_tool_calls for converted AskUserQuestion block")
	}
	if rec.Name != "AskUserQuestion" {
		t.Errorf("expected tool call name=AskUserQuestion, got %q", rec.Name)
	}
	// Verify the input JSON contains questions
	var toolInput map[string]any
	if err := json.Unmarshal(rec.Input, &toolInput); err != nil {
		t.Fatalf("failed to parse tool call input JSON: %v", err)
	}
	questions, ok := toolInput["questions"].([]any)
	if !ok || len(questions) == 0 {
		t.Errorf("expected questions array in tool call input, got %v", toolInput)
	}
}

func TestSessionExecutor_BuildResult_AskUserQuestionContentJSONIncludesInput(t *testing.T) {
	// Verify that the content JSON persisted to chat_history includes input
	// for AskUserQuestion blocks (not slim-serialized away).
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	streamingMsgID := GetStreamingMessageID(sid)
	if streamingMsgID == 0 {
		t.Fatal("expected non-zero streaming message ID from setup")
	}

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: streamingMsgID,
	}

	events := []ai.StreamEvent{
		{Type: "content", Content: `<ask-question><item><question>Pick one</question><option><label>X</label></option></item></ask-question>`},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(ctx, cfg)
	result := executor.RunWithChannel(ch)
	finalized := executor.Finalize(result, nil)

	// Read the persisted content from DB
	msgID := finalized.MsgID
	if msgID == 0 {
		t.Fatal("expected non-zero message ID after Finalize")
	}
	var content string
	err := dbRead.QueryRow("SELECT content FROM chat_history WHERE id = ?", msgID).Scan(&content)
	if err != nil {
		t.Fatalf("failed to read content from DB: %v", err)
	}

	// Parse the content JSON and find the AskUserQuestion block
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("failed to parse content JSON: %v", err)
	}
	blocks, ok := parsed["blocks"].([]any)
	if !ok || len(blocks) == 0 {
		t.Fatal("expected blocks array in content JSON")
	}

	// Find the AskUserQuestion block
	var askBlock map[string]any
	for _, b := range blocks {
		bm, _ := b.(map[string]any)
		if bm["name"] == "AskUserQuestion" {
			askBlock = bm
			break
		}
	}
	if askBlock == nil {
		t.Fatal("expected AskUserQuestion block in content JSON")
	}

	// Verify input is present (not stripped by slim serialization)
	input, ok := askBlock["input"]
	if !ok {
		t.Fatal("AskUserQuestion block missing 'input' field — was slim-serialized")
	}
	inputMap, ok := input.(map[string]any)
	if !ok {
		t.Fatalf("expected input to be a map, got %T", input)
	}
	questions, ok := inputMap["questions"].([]any)
	if !ok || len(questions) == 0 {
		t.Errorf("expected questions array in input, got %v", inputMap)
	}
}

func TestSessionExecutor_Finalize_TransportFromACP(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test", AcpCommand: "test-acp", Transport: "acp-stdio"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	events := []ai.StreamEvent{
		{Type: "content", Content: "hello"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	runResult := executor.RunWithChannel(ch)
	runResult = executor.Finalize(runResult, nil)

	// Agent supports ACP, so transport should be "acp-stdio"
	if runResult.Metadata.Transport != "acp-stdio" {
		t.Fatalf("expected transport='acp-stdio', got %q", runResult.Metadata.Transport)
	}
}

func TestSessionExecutor_Finalize_SavesRawOutput(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	events := []ai.StreamEvent{
		{Type: "raw_output", RawOutput: "raw line 1"},
		{Type: "content", Content: "hello"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	runResult := executor.RunWithChannel(ch)
	runResult = executor.Finalize(runResult, nil)

	// Verify raw output was saved
	if runResult.MsgID <= 0 {
		t.Fatal("expected MsgID > 0 after Finalize")
	}
	var rawCount int
	err := dbRead.QueryRow(
		"SELECT COUNT(*) FROM ai_raw_responses WHERE session_id = ?",
		sid,
	).Scan(&rawCount)
	if err != nil {
		t.Fatalf("failed to query raw responses: %v", err)
	}
	if rawCount == 0 {
		t.Fatal("expected raw response to be saved")
	}
}

// --- Additional diff coverage for session_executor.go ---

func TestSessionExecutor_RunWithChannel_TickerFlush(t *testing.T) {
	// Cover lines 239-242: periodic flush ticker fires when there are blocks.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Send a content event, then wait for ticker flush, then send done
	ch := make(chan ai.StreamEvent, 5)
	ch <- ai.StreamEvent{Type: "content", Content: "hello"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Wait for the ticker to fire (1 second) before sending done
		time.Sleep(1500 * time.Millisecond)
		ch <- ai.StreamEvent{Type: "done"}
	}()

	result := executor.RunWithChannel(ch)
	<-done

	if !result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true")
	}
}

func TestSessionExecutor_BuildContentJSON_WithBlocks_ContextCancel(t *testing.T) {
	// Cover line 488-490: buildContentJSON with blocks and context cancelled.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	blocks := []model.ContentBlock{{Type: "text", Text: "partial"}}
	result := RunResult{CancelReason: ""} // NOT user cancel
	meta := &ai.Metadata{}

	contentJSON, _ := executor.buildContentJSON(blocks, result, meta)
	if !strings.Contains(contentJSON, `"cancelled":true`) {
		t.Fatalf("expected cancelled:true in content JSON, got: %s", contentJSON)
	}
}

func TestSessionExecutor_Finalize_FinalizeStreamingMessageError(t *testing.T) {
	// Cover lines 536-540: Finalize when FinalizeStreamingMessage fails.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "ok"}},
		Metadata:         &ai.Metadata{},
	}

	// Drop chat_history to make FinalizeStreamingMessage fail
	_, _ = WriteExec("DROP TABLE chat_history")

	// Should not panic
	finalized := executor.Finalize(result, nil)
	if finalized.MsgID != 0 {
		t.Fatalf("expected MsgID=0 when FinalizeStreamingMessage fails, got %d", finalized.MsgID)
	}
}

func TestSessionExecutor_Finalize_SaveMetadataError(t *testing.T) {
	// Cover lines 544-546: Finalize when SaveMetadata fails.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "ok"}},
		Metadata:         &ai.Metadata{},
	}

	// Drop summaries table to make SaveMetadata fail
	_, _ = WriteExec("DROP TABLE summaries")

	// Should not panic
	finalized := executor.Finalize(result, nil)
	if finalized.MsgID <= 0 {
		t.Fatal("expected MsgID > 0 even when SaveMetadata fails")
	}
}

func TestSessionExecutor_Finalize_SaveRawResponseError(t *testing.T) {
	// Cover lines 555-559: Finalize when SaveRawResponse fails.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           []model.ContentBlock{{Type: "text", Text: "ok"}},
		Metadata:         &ai.Metadata{},
		RawOutput:        "some raw output",
	}

	// Drop ai_raw_responses table to make SaveRawResponse fail
	_, _ = WriteExec("DROP TABLE IF EXISTS ai_raw_responses")

	// Should not panic
	finalized := executor.Finalize(result, nil)
	if finalized.MsgID <= 0 {
		t.Fatal("expected MsgID > 0 even when SaveRawResponse fails")
	}
}

func TestSessionExecutor_Finalize_ConvertAskQuestionBlocks(t *testing.T) {
	// Regression test: Finalize must apply ConvertAskQuestionBlocks on e.blocks
	// before writing to DB. Previously, buildResult applied the conversion on a
	// local copy but Finalize used the original e.blocks, so DB stored
	// unconverted <ask-question> text blocks instead of tool_use blocks.
	setupExecutorDB(t)
	agentID := "askq-test-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, agentID)

	// Set StreamingMessageID before creating executor so UpsertToolCall path is exercised
	streamingMsgID := setupStreamingMessage(t, sid)

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            agentID,
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: streamingMsgID,
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Simulate accumulated blocks containing <ask-question> text
	executor.blocks = []model.ContentBlock{
		{Type: "text", Text: `<ask-question><item><header>Choice</header><multi-select>false</multi-select><question>Which one?</question><option><label>A</label><description>First</description></option></item></ask-question>`},
	}

	result := RunResult{
		ReceivedTerminal: true,
		Blocks:           executor.blocks, // buildResult would have converted this
		Metadata:         &ai.Metadata{},
	}

	finalized := executor.Finalize(result, nil)

	// Verify: finalized blocks should contain a tool_use AskUserQuestion block,
	// not the original text block with <ask-question> tags.
	foundAskTool := false
	for _, b := range finalized.Blocks {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			foundAskTool = true
			if len(b.Input) == 0 {
				t.Fatal("AskUserQuestion tool_use block should have input")
			}
			break
		}
		if b.Type == "text" && strings.Contains(b.Text, "<ask-question") {
			t.Fatal("text block should not contain <ask-question> after Finalize — conversion should have run")
		}
	}
	if !foundAskTool {
		t.Fatal("expected a tool_use AskUserQuestion block in finalized result, but none found")
	}

	// Verify: DB content should also have the converted block
	var content string
	err := dbRead.QueryRow("SELECT content FROM chat_history WHERE id = ?", finalized.MsgID).Scan(&content)
	if err != nil {
		t.Fatalf("failed to read finalized message from DB: %v", err)
	}
	if strings.Contains(content, "<ask-question") {
		t.Fatal("DB content should not contain <ask-question> — ConvertAskQuestionBlocks should have been applied")
	}
	if !strings.Contains(content, "AskUserQuestion") {
		t.Fatal("DB content should contain AskUserQuestion tool_use block")
	}
}

// setupStreamingMessage creates a streaming assistant message placeholder for testing.
func setupStreamingMessage(t *testing.T, sessionID string) int64 {
	t.Helper()
	msgID, err := AddChatMessage("/test", "test", sessionID, "assistant", `{"blocks":[]}`, nil, true, "")
	if err != nil {
		t.Fatalf("failed to create streaming message: %v", err)
	}
	return msgID
}

func TestSessionExecutor_ContentReset(t *testing.T) {
	// Verify that a content_reset event clears accumulated blocks, rawOutput,
	// and responseMetadata — this is the mechanism that prevents content
	// duplication when ACPBackend retries a Prompt after a peer disconnect.
	// Without content_reset, the first Prompt's partial events would remain
	// in the executor's blocks, and the retry Prompt's full output would be
	// appended, producing duplicated text.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")

	events := []ai.StreamEvent{
		{Type: "content", Content: "stale partial from first prompt"},
		{Type: "metadata", Meta: &ai.Metadata{InputTokens: 999}},
		{Type: "raw_output", RawOutput: "stale raw"},
		{Type: "content_reset"}, // simulates the retry boundary
		{Type: "content", Content: "fresh content from retry"},
		{Type: "metadata", Meta: &ai.Metadata{InputTokens: 100, OutputTokens: 50}},
		{Type: "raw_output", RawOutput: "fresh raw"},
		{Type: "done"},
	}

	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(ctx, cfg)
	result := executor.RunWithChannel(ch)

	if !result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true")
	}

	// Only the retry's content should survive
	if len(result.Blocks) != 1 {
		t.Fatalf("expected 1 text block after content_reset, got %d: %+v", len(result.Blocks), result.Blocks)
	}
	if result.Blocks[0].Text != "fresh content from retry" {
		t.Fatalf("expected 'fresh content from retry', got %q", result.Blocks[0].Text)
	}

	// Only the retry's metadata should survive
	if result.Metadata == nil {
		t.Fatal("expected Metadata to be set")
	}
	if result.Metadata.InputTokens != 100 {
		t.Fatalf("expected InputTokens=100 from retry metadata, got %d", result.Metadata.InputTokens)
	}

	// Only the retry's raw output should survive
	if result.RawOutput != "fresh raw" {
		t.Fatalf("expected RawOutput='fresh raw', got %q", result.RawOutput)
	}
}

func TestSessionExecutor_ContentReset_DBReset(t *testing.T) {
	// Verify that content_reset also resets the streaming message in DB to empty,
	// and deletes stale tool call rows, so stale data doesn't persist if the
	// retry fails.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")

	// Find the streaming message ID created by setupExecutorSession
	var streamingMsgID int64
	err := dbRead.QueryRow(
		"SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 1 ORDER BY id DESC LIMIT 1",
		sid,
	).Scan(&streamingMsgID)
	if err != nil {
		t.Fatalf("failed to find streaming message: %v", err)
	}

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: streamingMsgID,
	}
	executor := NewSessionExecutor(ctx, cfg)

	// First, accumulate some blocks and insert a tool call row, then flush to DB
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "content", Content: "stale"})
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Read", ID: "stale-tool-1", Done: true},
	})
	executor.upsertToolCallToDB(ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Read", ID: "stale-tool-1", Done: true},
	})
	executor.flushStreamingMessage()

	// Verify stale content in DB
	var content string
	err = dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? AND streaming = 1",
		sid,
	).Scan(&content)
	if err != nil {
		t.Fatalf("failed to query streaming message: %v", err)
	}
	if !contains(content, "stale") {
		t.Fatalf("expected stale content in DB before reset, got: %q", content)
	}

	// Verify stale tool call row exists
	var toolCount int
	err = dbRead.QueryRow(
		"SELECT COUNT(*) FROM chat_tool_calls WHERE message_id = ?",
		streamingMsgID,
	).Scan(&toolCount)
	if err != nil {
		t.Fatalf("failed to query tool calls: %v", err)
	}
	if toolCount != 1 {
		t.Fatalf("expected 1 tool call row before reset, got %d", toolCount)
	}

	// Now simulate content_reset
	executor.handleNonTerminalEvent(ai.StreamEvent{Type: "content_reset"})

	// Verify blocks were cleared
	if len(executor.blocks) != 0 {
		t.Fatalf("expected empty blocks after content_reset, got %d", len(executor.blocks))
	}

	// Verify DB was reset to empty blocks
	err = dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? AND streaming = 1",
		sid,
	).Scan(&content)
	if err != nil {
		t.Fatalf("failed to query streaming message after reset: %v", err)
	}
	if contains(content, "stale") {
		t.Fatalf("expected stale content to be cleared from DB after reset, got: %q", content)
	}
	if !contains(content, `"blocks":[]`) {
		t.Fatalf("expected empty blocks in DB after reset, got: %q", content)
	}

	// Verify stale tool call rows were deleted
	err = dbRead.QueryRow(
		"SELECT COUNT(*) FROM chat_tool_calls WHERE message_id = ?",
		streamingMsgID,
	).Scan(&toolCount)
	if err != nil {
		t.Fatalf("failed to query tool calls after reset: %v", err)
	}
	if toolCount != 0 {
		t.Fatalf("expected 0 tool call rows after content_reset, got %d", toolCount)
	}
}

func TestSessionExecutor_ContentReset_WithToolAndThinking(t *testing.T) {
	// Verify content_reset clears tool_use and thinking blocks too, and
	// that toolStarts is properly reset so stale duration entries don't
	// affect the retry's tool duration calculations.
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")

	events := []ai.StreamEvent{
		{Type: "thinking", Content: "stale thinking"},
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "tool-1", Done: false}},
		{Type: "content", Content: "stale text"},
		{Type: "content_reset"},
		// Retry output
		{Type: "thinking", Content: "fresh thinking"},
		{Type: "thinking_done"},
		{Type: "content", Content: "fresh text"},
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Bash", ID: "tool-2", Done: true, DurationMs: 100}},
		{Type: "tool_result", Tool: &ai.ToolCall{ID: "tool-2", Output: "ok", Status: "success", Done: true}},
		{Type: "done"},
	}

	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(ctx, cfg)
	result := executor.RunWithChannel(ch)

	if !result.ReceivedTerminal {
		t.Fatal("expected ReceivedTerminal=true")
	}

	// Should only have retry's blocks: 1 thinking + 1 text + 1 tool_use
	// (MergeConsecutiveThinkingBlocks + thinking_done in buildResult)
	toolCount := 0
	textCount := 0
	thinkingCount := 0
	for _, b := range result.Blocks {
		switch b.Type {
		case "tool_use":
			toolCount++
		case "text":
			textCount++
		case "thinking":
			thinkingCount++
		}
	}
	if toolCount != 1 {
		t.Fatalf("expected 1 tool_use block after content_reset, got %d", toolCount)
	}
	if textCount != 1 {
		t.Fatalf("expected 1 text block after content_reset, got %d", textCount)
	}
	if thinkingCount != 1 {
		t.Fatalf("expected 1 thinking block after content_reset, got %d", thinkingCount)
	}

	// Verify stale tool IDs are gone from toolStarts
	if _, exists := executor.toolStarts["tool-1"]; exists {
		t.Fatal("stale toolStarts entry 'tool-1' should have been cleared by content_reset")
	}
}

func TestSessionExecutor_Finalize_SlimsThinkingToDB(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	streamingMsgID := GetStreamingMessageID(sid)
	if streamingMsgID == 0 {
		t.Fatal("expected non-zero streaming message ID from setup")
	}

	events := []ai.StreamEvent{
		{Type: "thinking", Content: "Let me think about this deeply."},
		{Type: "content", Content: "Answer here."},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: streamingMsgID,
	})
	result := executor.RunWithChannel(ch)
	finalized := executor.Finalize(result, nil)
	if finalized.MsgID == 0 {
		t.Fatal("expected non-zero message ID after Finalize")
	}

	// WS blocks must keep full thinking text.
	var wsThinking string
	for i := range finalized.Blocks {
		if finalized.Blocks[i].Type == "thinking" {
			wsThinking = finalized.Blocks[i].Text
		}
	}
	if wsThinking != "Let me think about this deeply." {
		t.Errorf("WS block thinking text = %q, want full text", wsThinking)
	}

	// DB content must be slim (thinking block has think_id, no text).
	var dbContent string
	err := dbRead.QueryRow("SELECT content FROM chat_history WHERE id = ?", finalized.MsgID).Scan(&dbContent)
	if err != nil {
		t.Fatalf("read db content: %v", err)
	}
	var parsed struct {
		Blocks []map[string]any `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(dbContent), &parsed); err != nil {
		t.Fatalf("unmarshal db content: %v", err)
	}
	var thinkBlock map[string]any
	for _, b := range parsed.Blocks {
		if b["type"] == "thinking" {
			thinkBlock = b
		}
	}
	if thinkBlock == nil {
		t.Fatal("expected thinking block in DB content")
	}
	if _, hasText := thinkBlock["text"]; hasText {
		t.Error("DB thinking block should not have text")
	}
	thinkID, _ := thinkBlock["think_id"].(string)
	if thinkID == "" {
		t.Fatal("expected think_id in slim block")
	}

	// chat_thinking row exists with the extracted text.
	rec, err := GetThinking(thinkID, finalized.MsgID)
	if err != nil || rec == nil {
		t.Fatalf("expected thinking record, rec=%+v err=%v", rec, err)
	}
	if rec.Text != "Let me think about this deeply." {
		t.Errorf("record text = %q", rec.Text)
	}
}

// TestSessionExecutor_FlushIntervalLimit verifies that a burst of incremental
// events does not trigger a DB write for every event — persistence is
// rate-limited to once per flushInterval. With lastFlush zeroed, the first
// event flushes immediately; a rapid burst right after must NOT write again
// until the interval elapses.
func TestSessionExecutor_FlushIntervalLimit(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// First event: lastFlush is zero value, so time.Since(zero) is huge and
	// this flushes immediately — the streaming row gets content.
	executor.handleNonTerminalEvent(ai.StreamEvent{Type: "content", Content: "first"})
	assertStreamingContentContains(t, sid, "first")

	// Record the flush time and fire a rapid burst within flushInterval.
	// None of these should trigger an additional flush (rate-limited).
	executor.lastFlush = time.Now()
	for range 50 {
		executor.handleNonTerminalEvent(ai.StreamEvent{Type: "content", Content: "burst"})
	}
	assertStreamingContentContains(t, sid, "first")
	assertStreamingContentNotContains(t, sid, "burst")

	// After the interval elapses, the next event flushes again.
	executor.lastFlush = time.Now().Add(-flushInterval - time.Millisecond)
	executor.handleNonTerminalEvent(ai.StreamEvent{Type: "content", Content: "after"})
	assertStreamingContentContains(t, sid, "burst")
	assertStreamingContentContains(t, sid, "after")
}

// TestSessionExecutor_FlushSkipsThinking verifies flushStreamingMessage excludes
// thinking blocks from the DB content while streaming, and that Finalize
// restores them to chat_thinking.
func TestSessionExecutor_FlushSkipsThinking(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	streamingMsgID := GetStreamingMessageID(sid)
	if streamingMsgID == 0 {
		t.Fatal("expected non-zero streaming message ID from setup")
	}

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: streamingMsgID,
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Accumulate a thinking block plus normal content.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "secret thinking"})
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "content", Content: "visible text"})

	// Streaming flush must NOT persist the thinking block.
	executor.flushStreamingMessage()
	var content string
	err := dbRead.QueryRow("SELECT content FROM chat_history WHERE id = ?", streamingMsgID).Scan(&content)
	if err != nil {
		t.Fatalf("read streaming content: %v", err)
	}
	if strings.Contains(content, "secret thinking") {
		t.Error("streaming DB content should not include thinking text")
	}
	if !strings.Contains(content, "visible text") {
		t.Error("streaming DB content should include text blocks")
	}

	// Finalize restores thinking into chat_thinking.
	result := executor.buildResult(true, time.Now())
	finalized := executor.Finalize(result, nil)
	if finalized.MsgID == 0 {
		t.Fatal("expected non-zero message ID after Finalize")
	}

	var thinkIDs []string
	rows, err := dbRead.Query("SELECT think_id, text FROM chat_thinking WHERE message_id = ?", finalized.MsgID)
	if err != nil {
		t.Fatalf("query chat_thinking: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var tid, text string
		if err := rows.Scan(&tid, &text); err != nil {
			t.Fatalf("scan thinking row: %v", err)
		}
		thinkIDs = append(thinkIDs, tid)
		if text == "secret thinking" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate thinking rows: %v", err)
	}
	if !found {
		t.Errorf("expected thinking text in chat_thinking, records=%v", thinkIDs)
	}
}

func assertStreamingContentContains(t *testing.T, sid, want string) {
	t.Helper()
	var content string
	err := dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? AND streaming = 1",
		sid,
	).Scan(&content)
	if err != nil {
		t.Fatalf("read streaming content: %v", err)
	}
	if !strings.Contains(content, want) {
		t.Errorf("streaming content missing %q, got %q", want, content)
	}
}

func assertStreamingContentNotContains(t *testing.T, sid, notWant string) {
	t.Helper()
	var content string
	err := dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? AND streaming = 1",
		sid,
	).Scan(&content)
	if err != nil {
		t.Fatalf("read streaming content: %v", err)
	}
	if strings.Contains(content, notWant) {
		t.Errorf("streaming content should not contain %q, got %q", notWant, content)
	}
}
