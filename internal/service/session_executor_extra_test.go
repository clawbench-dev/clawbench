package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- SessionExecutor handleNonTerminalEvent coverage ---

func TestSessionExecutor_HandleNonTerminalEvent_RawOutput(t *testing.T) {
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "sess-raw",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// First raw_output
	event := ai.StreamEvent{Type: "raw_output", RawOutput: "line1"}
	executor.handleNonTerminalEvent(event)
	assert.Equal(t, "line1", executor.rawOutput)

	// Second raw_output should prepend newline
	event2 := ai.StreamEvent{Type: "raw_output", RawOutput: "line2"}
	executor.handleNonTerminalEvent(event2)
	assert.Contains(t, executor.rawOutput, "line1")
	assert.Contains(t, executor.rawOutput, "line2")
}

func TestSessionExecutor_HandleNonTerminalEvent_SessionCaptureEmpty(t *testing.T) {
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "sess-cap",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Empty session_capture should not capture
	event := ai.StreamEvent{Type: "session_capture", Content: ""}
	executor.handleNonTerminalEvent(event)
}

func TestSessionExecutor_HandleNonTerminalEvent_MetadataCapture(t *testing.T) {
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
	executor := NewSessionExecutor(ctx, cfg)

	event := ai.StreamEvent{Type: "metadata", Meta: &ai.Metadata{InputTokens: 500, SessionID: "ext-123"}}
	executor.handleNonTerminalEvent(event)
	assert.NotNil(t, executor.responseMetadata)
	assert.Equal(t, 500, executor.responseMetadata.InputTokens)
}

func TestSessionExecutor_HandleNonTerminalEvent_MetadataWithoutSessionID(t *testing.T) {
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "sess-meta2",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	event := ai.StreamEvent{Type: "metadata", Meta: &ai.Metadata{InputTokens: 100}}
	executor.handleNonTerminalEvent(event)
	assert.NotNil(t, executor.responseMetadata)
	assert.Equal(t, 100, executor.responseMetadata.InputTokens)
}

func TestSessionExecutor_HandleNonTerminalEvent_IncrementalPersistence(t *testing.T) {
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

	// The first event flushes immediately because lastFlush starts as the zero
	// value (time.Since(zero) is huge). Subsequent events within flushInterval
	// are rate-limited. Explicitly flush to verify the accumulated content is
	// persisted to the streaming message row.
	for range 5 {
		event := ai.StreamEvent{Type: "content", Content: "msg"}
		executor.handleNonTerminalEvent(event)
	}
	executor.flushStreamingMessage()
	// Verify by checking DB
	var content string
	err := dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? AND streaming = 1",
		sid,
	).Scan(&content)
	require.NoError(t, err)
	assert.Contains(t, content, "msg")
}

func TestSessionExecutor_HandleNonTerminalEvent_InteractiveSSEForward(t *testing.T) {
	// After removing StreamCh, events are forwarded to StreamHub instead.
	// With no StreamHub subscribers, handleNonTerminalEvent just does nothing.
	// This test verifies no panic occurs in interactive mode.
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "sess-sse",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	event := ai.StreamEvent{Type: "content", Content: "hello SSE"}
	executor.handleNonTerminalEvent(event)
	// No panic = success
}

func TestSessionExecutor_HandleNonTerminalEvent_SSESendFailure(t *testing.T) {
	// After removing StreamCh, handleNonTerminalEvent is void and always
	// forwards to StreamHub. With no subscribers, it simply does nothing.
	// This test verifies no panic occurs when StreamHub has no subscribers.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "sess-sse-fail",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	event := ai.StreamEvent{Type: "content", Content: "hello"}
	executor.handleNonTerminalEvent(event)
	// No panic = success
}

func TestSessionExecutor_HandleNonTerminalEvent_ToolUseMetaExtraction(t *testing.T) {
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "sess-tool-meta",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// tool_use event should have meta extracted and forwarded to StreamHub
	event := ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Read", ID: "tool-1", Input: `{"file_path":"/src/main.go"}`},
	}
	executor.handleNonTerminalEvent(event)

	// Verify ToolMeta was extracted correctly on the event
	meta := ai.ExtractToolCallMeta(event)
	assert.Equal(t, "tool-1", meta.ToolID)
	assert.Equal(t, "/src/main.go", meta.FilePath) // extracted from tool_use Input
}

// --- SessionExecutor RunWithChannel additional coverage ---

func TestSessionExecutor_RunWithChannel_ErrorEvent(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "content", Content: "partial"},
		{Type: "error", Error: "something went wrong"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	assert.True(t, result.ReceivedTerminal)
	// Error event should be accumulated as a warning block
	found := false
	for _, b := range result.Blocks {
		if b.Type == "warning" {
			found = true
		}
	}
	assert.True(t, found, "expected warning block to be accumulated from error event")
}

func TestSessionExecutor_RunWithChannel_FirstContentMs(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "content", Content: "first content"},
		{Type: "done"},
	}
	result := runExecutorWithEvents(t, events, ModeScheduled)

	// FirstContentMs should be non-negative
	assert.GreaterOrEqual(t, result.FirstContentMs, 0)
}

// --- drainRemainingEvents ---

func TestDrainRemainingEvents_NilChannel(t *testing.T) {
	ctx := context.Background()
	cfg := RunConfig{SessionID: "test", BackendName: "test", ProjectPath: "/test", AgentID: "test", ChatRequest: ai.ChatRequest{Prompt: "hello"}}
	executor := NewSessionExecutor(ctx, cfg)
	result := executor.drainRemainingEvents(nil, "existing")
	assert.Equal(t, "existing", result)
}

func TestDrainRemainingEvents_EmptyChannel(t *testing.T) {
	ctx := context.Background()
	cfg := RunConfig{SessionID: "test", BackendName: "test", ProjectPath: "/test", AgentID: "test", ChatRequest: ai.ChatRequest{Prompt: "hello"}}
	executor := NewSessionExecutor(ctx, cfg)
	ch := make(chan ai.StreamEvent)
	close(ch)
	result := executor.drainRemainingEvents(ch, "existing")
	assert.Equal(t, "existing", result)
}

func TestDrainRemainingEvents_WithRawOutputEvents(t *testing.T) {
	ctx := context.Background()
	cfg := RunConfig{SessionID: "test", BackendName: "test", ProjectPath: "/test", AgentID: "test", ChatRequest: ai.ChatRequest{Prompt: "hello"}}
	executor := NewSessionExecutor(ctx, cfg)
	ch := make(chan ai.StreamEvent, 3)
	ch <- ai.StreamEvent{Type: "raw_output", RawOutput: "drained1"}
	ch <- ai.StreamEvent{Type: "raw_output", RawOutput: "drained2"}
	ch <- ai.StreamEvent{Type: "content", Content: "not raw"} // should be skipped
	close(ch)

	result := executor.drainRemainingEvents(ch, "")
	assert.Contains(t, result, "drained1")
	assert.Contains(t, result, "drained2")
	assert.NotContains(t, result, "not raw")
}

func TestDrainRemainingEvents_ToolUseEvents(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID, err := AddChatMessage("/test", "test", sid, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: msgID,
	}
	executor := NewSessionExecutor(ctx, cfg)

	ch := make(chan ai.StreamEvent, 2)
	ch <- ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Read", ID: "tool-drain-1", Input: `{"file_path":"/src/main.go"}`},
	}
	ch <- ai.StreamEvent{Type: "raw_output", RawOutput: "raw data"}
	close(ch)

	result := executor.drainRemainingEvents(ch, "")
	assert.Contains(t, result, "raw data")

	// Verify tool call block was accumulated
	found := false
	for _, b := range executor.blocks {
		if b.Type == "tool_use" && b.ID == "tool-drain-1" {
			found = true
			assert.Equal(t, "Read", b.Name)
		}
	}
	assert.True(t, found, "tool_use event should be accumulated into blocks")

	// Verify tool call was persisted to DB
	record, err := GetToolCall("tool-drain-1", msgID)
	require.NoError(t, err)
	require.NotNil(t, record, "tool call should be persisted via drainRemainingEvents")
	assert.Equal(t, "Read", record.Name)
}

func TestDrainRemainingEvents_ToolResultEvents(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID, err := AddChatMessage("/test", "test", sid, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: msgID,
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Pre-populate a tool_use block so tool_result can find it
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Bash", ID: "tool-drain-2", Input: `{"command":"ls"}`},
	})

	ch := make(chan ai.StreamEvent, 1)
	ch <- ai.StreamEvent{
		Type: "tool_result",
		Tool: &ai.ToolCall{Name: "Bash", ID: "tool-drain-2", Output: "file1.go", Done: true, Status: "success"},
	}
	close(ch)

	executor.drainRemainingEvents(ch, "")

	// Verify tool result was accumulated into the existing block
	found := false
	for _, b := range executor.blocks {
		if b.Type == "tool_use" && b.ID == "tool-drain-2" {
			found = true
			assert.Equal(t, "file1.go", b.Output)
			assert.True(t, b.Done)
		}
	}
	assert.True(t, found, "tool_result should update existing block")
}

func TestDrainRemainingEvents_SessionCaptureEvent(t *testing.T) {
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

	ch := make(chan ai.StreamEvent, 2)
	ch <- ai.StreamEvent{Type: "session_capture", Content: "ext-drain-captured"}
	ch <- ai.StreamEvent{Type: "raw_output", RawOutput: "raw"}
	close(ch)

	result := executor.drainRemainingEvents(ch, "")
	assert.Contains(t, result, "raw")

	// Verify external session ID was persisted
	extID := GetExternalSessionID(sid)
	assert.Equal(t, "ext-drain-captured", extID)
}

func TestDrainRemainingEvents_MetadataWithSessionID(t *testing.T) {
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

	ch := make(chan ai.StreamEvent, 2)
	ch <- ai.StreamEvent{Type: "metadata", Meta: &ai.Metadata{SessionID: "ext-drain-meta"}}
	ch <- ai.StreamEvent{Type: "raw_output", RawOutput: "raw"}
	close(ch)

	result := executor.drainRemainingEvents(ch, "")
	assert.Contains(t, result, "raw")

	// Verify external session ID from metadata was persisted
	extID := GetExternalSessionID(sid)
	assert.Equal(t, "ext-drain-meta", extID)
}

func TestDrainRemainingEvents_MetadataWithNilMeta(t *testing.T) {
	ctx := context.Background()
	cfg := RunConfig{SessionID: "test", BackendName: "test", ProjectPath: "/test", AgentID: "test", ChatRequest: ai.ChatRequest{Prompt: "hello"}}
	executor := NewSessionExecutor(ctx, cfg)

	ch := make(chan ai.StreamEvent, 1)
	ch <- ai.StreamEvent{Type: "metadata", Meta: nil}
	close(ch)

	result := executor.drainRemainingEvents(ch, "")
	assert.Equal(t, "", result)
}

// --- buildContentJSON additional coverage ---

func TestSessionExecutor_BuildContentJSON_EmptyWithUserCancel(t *testing.T) {
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

	result := RunResult{CancelReason: "user"}
	meta := &ai.Metadata{}
	content, blocks := executor.buildContentJSON(nil, result, meta)

	assert.NotEmpty(t, content)
	assert.Empty(t, blocks, "user cancel should not add warning blocks")
	assert.Contains(t, content, "cancelled")
}

func TestSessionExecutor_BuildContentJSON_EmptyWithContextCancel(t *testing.T) {
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

	result := RunResult{}
	meta := &ai.Metadata{}
	content, blocks := executor.buildContentJSON(nil, result, meta)

	assert.NotEmpty(t, content)
	assert.Len(t, blocks, 1)
	assert.Equal(t, "warning", blocks[0].Type)
	assert.Contains(t, content, "cancelled")
}

func TestSessionExecutor_BuildContentJSON_WithBlocksAndCancel(t *testing.T) {
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
	result := RunResult{CancelReason: "user", Blocks: blocks}
	meta := &ai.Metadata{}
	content, finalBlocks := executor.buildContentJSON(blocks, result, meta)

	assert.NotEmpty(t, content)
	assert.Contains(t, content, "cancelled")
	assert.Len(t, finalBlocks, 1) // original block, no extra warning when blocks present + cancel
}

func TestSessionExecutor_BuildContentJSON_DefaultEmptyReason(t *testing.T) {
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

	result := RunResult{}
	meta := &ai.Metadata{}
	content, blocks := executor.buildContentJSON(nil, result, meta)

	assert.NotEmpty(t, content)
	assert.Len(t, blocks, 1)
	assert.Equal(t, "warning", blocks[0].Type)
	assert.Equal(t, ai.ReasonEmpty, blocks[0].Reason)
}

// --- injectSessionMetadata additional coverage ---

func TestSessionExecutor_InjectSessionMetadata_NilModeAndEffort(t *testing.T) {
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

	meta := &ai.Metadata{}
	executor.injectSessionMetadata(meta)

	// No ACP conn, no session transport → should default to "cli"
	assert.Equal(t, "cli", meta.Transport)
}

func TestSessionExecutor_InjectSessionMetadata_WithSessionModel(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	_ = UpdateSessionModel(sid, "custom-model")

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

	meta := &ai.Metadata{}
	executor.injectSessionMetadata(meta)

	assert.Equal(t, "custom-model", meta.Model)
}

// --- captureExternalSessionID additional coverage ---

func TestSessionExecutor_CaptureExternalSessionID_AlreadySet(t *testing.T) {
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

	// First capture
	executor.captureExternalSessionID("ext-first")
	extID := GetExternalSessionID(sid)
	assert.Equal(t, "ext-first", extID)

	// Second capture with different ID — should NOT overwrite
	// (existingExtID != e.cfg.SessionID, so it should be kept)
	executor.captureExternalSessionID("ext-second")
	extID = GetExternalSessionID(sid)
	assert.Equal(t, "ext-first", extID, "should not overwrite existing external session ID")
}

// --- upsertToolCallToDB coverage ---

func TestSessionExecutor_UpsertToolCallToDB_EmptySessionIDEarlyReturn(t *testing.T) {
	ctx := context.Background()
	cfg := RunConfig{SessionID: "", StreamingMessageID: 1}
	executor := NewSessionExecutor(ctx, cfg)

	event := ai.StreamEvent{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "t1"}}
	executor.upsertToolCallToDB(event)
	// Early return — no panic, no DB call
}

// --- Finalize with metadata save ---

func TestSessionExecutor_Finalize_SavesMetadata(t *testing.T) {
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
		Metadata:         &ai.Metadata{InputTokens: 100, OutputTokens: 50, CostUSD: 0.01},
	}
	finalized := executor.Finalize(result, nil)
	assert.True(t, finalized.MsgID > 0)

	// Verify metadata was saved
	var inputTokens int
	err := dbRead.QueryRow("SELECT input_tokens FROM chat_metadata WHERE message_id = ?", finalized.MsgID).Scan(&inputTokens)
	require.NoError(t, err)
	assert.Equal(t, 100, inputTokens)
}

// --- buildResult empty response detection ---

func TestSessionExecutor_BuildResult_EmptyWithCancelReason(t *testing.T) {
	// When blocks is empty but there IS a cancel reason, Empty should be false
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

	SetCancelReason(sid, "user")

	events := []ai.StreamEvent{{Type: "done"}}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(ctx, cfg)
	result := executor.RunWithChannel(ch)

	// Empty blocks + cancel reason = NOT marked as Empty
	assert.False(t, result.Empty, "should not be marked Empty when there's a cancel reason")
	assert.Equal(t, "user", result.CancelReason)
}

// --- Scheduled mode: no cancel reason lookup ---

func TestSessionExecutor_BuildResult_ScheduledNoCancelReasonLookup(t *testing.T) {
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

	events := []ai.StreamEvent{{Type: "done"}}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(ctx, cfg)
	result := executor.RunWithChannel(ch)

	assert.True(t, result.Empty, "should be marked Empty when no blocks and no cancel in scheduled mode")
	assert.Empty(t, result.CancelReason, "scheduled mode should not have cancel reason")
}

// --- upsertToolCallToDB with StreamingMessageID set and matching block ---

func TestSessionExecutor_UpsertToolCallToDB_WithStreamingMessageID(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: 0, // will be set by AddChatMessage
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Create a streaming assistant message to get a valid StreamingMessageID
	msgID, err := AddChatMessage("/test", "test", sid, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)
	executor.cfg.StreamingMessageID = msgID

	// Accumulate a tool_use block first
	toolEvent := ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Read", ID: "tool-upsert-1", Input: `{"file_path":"/src/main.go"}`},
	}
	ai.AccumulateBlock(&executor.blocks, toolEvent)

	// Now call upsertToolCallToDB — should find the block and persist it
	executor.upsertToolCallToDB(toolEvent)

	// Verify the tool call was persisted
	record, err := GetToolCall("tool-upsert-1", msgID)
	require.NoError(t, err)
	require.NotNil(t, record, "tool call should be persisted in DB")
	assert.Equal(t, "Read", record.Name)
	assert.Equal(t, sid, record.SessionID)
}

func TestSessionExecutor_UpsertToolCallToDB_NoMatchingBlock(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID, err := AddChatMessage("/test", "test", sid, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: msgID,
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Tool event with no matching block in executor.blocks
	toolEvent := ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Bash", ID: "tool-no-match"},
	}
	executor.upsertToolCallToDB(toolEvent)

	// Should not have persisted — no matching block
	record, err := GetToolCall("tool-no-match", msgID)
	require.NoError(t, err)
	assert.Nil(t, record, "should not persist tool call without matching block")
}

func TestSessionExecutor_UpsertToolCallToDB_EmptySessionID(t *testing.T) {
	ctx := context.Background()
	cfg := RunConfig{SessionID: "", StreamingMessageID: 1}
	executor := NewSessionExecutor(ctx, cfg)

	event := ai.StreamEvent{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "t1"}}
	executor.upsertToolCallToDB(event)
	// Early return — no panic, no DB call
}

// --- buildResult AskUserQuestion persistence ---

func TestSessionExecutor_BuildResult_AskUserQuestionPersisted(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID, err := AddChatMessage("/test", "test", sid, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: msgID,
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Set up blocks with an AskUserQuestion block (prefixed with "ask-")
	executor.blocks = []model.ContentBlock{
		{Type: "tool_use", ID: "ask-001", Name: "AskUserQuestion", Input: map[string]any{"question": "Continue?"}, Output: "", Status: "pending", Summary: "Asking user", Done: false},
		{Type: "text", Text: "partial response"},
	}

	// buildResult processes the blocks (pure transformation, no DB side effects).
	wallStart := time.Now()
	result := executor.buildResult(true, wallStart)

	// AskUserQuestion tool calls are persisted only in Finalize, not buildResult.
	// Simulate the full pipeline by calling persistAskToolCalls on the result blocks.
	executor.persistAskToolCalls(result.Blocks)

	// Verify the AskUserQuestion tool call was persisted
	record, err := GetToolCall("ask-001", msgID)
	require.NoError(t, err)
	require.NotNil(t, record, "AskUserQuestion tool call should be persisted after persistAskToolCalls")
	assert.Equal(t, "AskUserQuestion", record.Name)
	_ = result
}

// --- tool_result SSE forwarding with meta extraction ---

func TestSessionExecutor_HandleNonTerminalEvent_ToolResultMetaExtraction(t *testing.T) {
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "sess-tool-result",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(ctx, cfg)

	// tool_result event should have meta extracted and forwarded to StreamHub
	event := ai.StreamEvent{
		Type: "tool_result",
		Tool: &ai.ToolCall{Name: "Read", ID: "tool-2", Output: "file contents"},
	}
	executor.handleNonTerminalEvent(event)

	// Verify ToolMeta was extracted correctly on the event
	meta := ai.ExtractToolCallMeta(event)
	assert.Equal(t, "tool-2", meta.ToolID)
}

// --- handleNonTerminalEvent upserts tool call when StreamingMessageID set ---

func TestSessionExecutor_HandleNonTerminalEvent_UpsertToolCall(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID, err := AddChatMessage("/test", "test", sid, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: msgID,
	}
	executor := NewSessionExecutor(ctx, cfg)

	// tool_use event should be accumulated and upserted to DB
	event := ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Bash", ID: "tool-handle-1", Input: `{"command":"ls"}`},
	}
	executor.handleNonTerminalEvent(event)

	// Verify tool call was persisted
	record, err := GetToolCall("tool-handle-1", msgID)
	require.NoError(t, err)
	require.NotNil(t, record, "tool call should be persisted via handleNonTerminalEvent")
	assert.Equal(t, "Bash", record.Name)
}

// --- UpsertToolCall and GetToolCall direct tests ---

func TestSessionExecutor_TrackToolDuration(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID, err := AddChatMessage("/test", "test", sid, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: msgID,
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Tool starts (done=false) — records the start time
	executor.handleNonTerminalEvent(ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Bash", ID: "tool-dur-1", Input: `{"command":"ls"}`, Done: false},
	})
	time.Sleep(30 * time.Millisecond)
	// Tool completes via tool_result — computes duration
	executor.handleNonTerminalEvent(ai.StreamEvent{
		Type: "tool_result",
		Tool: &ai.ToolCall{Name: "Bash", ID: "tool-dur-1", Output: "ok", Status: "success"},
	})

	// The accumulated block should carry the duration
	require.Len(t, executor.blocks, 1, "expected one accumulated block")
	block := executor.blocks[0]
	assert.True(t, block.Done)
	assert.GreaterOrEqual(t, block.DurationMs, 25, "block duration should reflect elapsed wall-clock time")

	// The persisted tool call record should carry the duration
	record, err := GetToolCall("tool-dur-1", msgID)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.GreaterOrEqual(t, record.DurationMs, 25, "DB duration should reflect elapsed wall-clock time")
}

func TestSessionExecutor_TrackToolDuration_StartEventAlreadyDone(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID, err := AddChatMessage("/test", "test", sid, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: msgID,
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Backends that emit a single done tool_use without a start event:
	// duration stays 0 (unknown) rather than a garbage value.
	executor.handleNonTerminalEvent(ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Read", ID: "tool-dur-2", Input: `{"file_path":"/a.go"}`, Done: true},
	})

	record, err := GetToolCall("tool-dur-2", msgID)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, 0, record.DurationMs)
}

func TestSessionExecutor_TrackToolDuration_InterimThenFinal(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID, err := AddChatMessage("/test", "test", sid, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: msgID,
	}
	executor := NewSessionExecutor(ctx, cfg)

	// Claude-style flow: start → input complete (done=true, interim duration) → tool_result (final).
	executor.handleNonTerminalEvent(ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Edit", ID: "tool-dur-3", Input: `{"file_path":"/a.go"}`, Done: false},
	})
	time.Sleep(10 * time.Millisecond)
	executor.handleNonTerminalEvent(ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Edit", ID: "tool-dur-3", Input: `{"file_path":"/a.go"}`, Done: true},
	})
	time.Sleep(40 * time.Millisecond)
	executor.handleNonTerminalEvent(ai.StreamEvent{
		Type: "tool_result",
		Tool: &ai.ToolCall{Name: "Edit", ID: "tool-dur-3", Output: "done", Status: "success"},
	})

	// The final duration should cover the full span (start → tool_result), not just input streaming.
	require.Len(t, executor.blocks, 1)
	block := executor.blocks[0]
	assert.GreaterOrEqual(t, block.DurationMs, 40, "final duration should reflect full elapsed time")

	record, err := GetToolCall("tool-dur-3", msgID)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.GreaterOrEqual(t, record.DurationMs, 40, "DB duration should be the final value")
}

// --- UpsertToolCall and GetToolCall direct tests ---

func TestUpsertToolCall_InsertAndGet(t *testing.T) {
	setupExecutorDB(t)

	sid := "test-session-upsert"
	_, err := CreateSession("/test", "test", "Test", "test", sid, "default", "chat")
	require.NoError(t, err)

	msgID, err := AddChatMessage("/test", "test", sid, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)

	// Insert
	err = UpsertToolCall(msgID, sid, "toolu_direct_1", "Read", json.RawMessage(`{"file_path":"/tmp/test.go"}`), "contents here", "success", "test.go", true, 0)
	require.NoError(t, err)

	// Get
	record, err := GetToolCall("toolu_direct_1", msgID)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, "Read", record.Name)
	assert.Equal(t, "contents here", record.Output)
	assert.Equal(t, "success", record.Status)
	assert.True(t, record.Done)
	assert.Equal(t, sid, record.SessionID)
}

func TestUpsertToolCall_UpdateExisting(t *testing.T) {
	setupExecutorDB(t)

	sid := "test-session-update"
	_, err := CreateSession("/test", "test", "Test", "test", sid, "default", "chat")
	require.NoError(t, err)

	msgID, err := AddChatMessage("/test", "test", sid, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)

	// Insert
	err = UpsertToolCall(msgID, sid, "toolu_update_1", "Bash", json.RawMessage(`{"command":"ls"}`), "", "running", "", false, 0)
	require.NoError(t, err)

	// Update with output
	err = UpsertToolCall(msgID, sid, "toolu_update_1", "Bash", json.RawMessage(`{"command":"ls"}`), "file1.go\nfile2.go", "completed", "listing", true, 0)
	require.NoError(t, err)

	// Get updated record
	record, err := GetToolCall("toolu_update_1", msgID)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, "completed", record.Status)
	assert.Equal(t, "file1.go\nfile2.go", record.Output)
	assert.Equal(t, "listing", record.Summary)
	assert.True(t, record.Done)
}

func TestGetToolCall_NotFound(t *testing.T) {
	setupExecutorDB(t)

	record, err := GetToolCall("nonexistent", 99999)
	require.NoError(t, err)
	assert.Nil(t, record)
}

func TestUpsertToolCall_EmptyOutputNotOverwritten(t *testing.T) {
	setupExecutorDB(t)

	sid := "test-session-output"
	_, err := CreateSession("/test", "test", "Test", "test", sid, "default", "chat")
	require.NoError(t, err)

	msgID, err := AddChatMessage("/test", "test", sid, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)

	// Insert with output
	err = UpsertToolCall(msgID, sid, "toolu_output_1", "Read", json.RawMessage(`{}`), "existing output", "success", "", true, 0)
	require.NoError(t, err)

	// Update with empty output — should keep existing output
	err = UpsertToolCall(msgID, sid, "toolu_output_1", "Read", json.RawMessage(`{}`), "", "updated", "", true, 0)
	require.NoError(t, err)

	record, err := GetToolCall("toolu_output_1", msgID)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, "existing output", record.Output, "empty output should not overwrite existing output")
	assert.Equal(t, "updated", record.Status)
}

// --- SessionHasRealAssistantContent tests ---

func TestSessionHasRealAssistantContent_EmptyPlaceholder(t *testing.T) {
	setupExecutorDB(t)
	sid, err := CreateSession("/test", "claude", "test-real-content-empty", "", "", "claude", "chat")
	require.NoError(t, err)

	// Finalize an empty placeholder (cancelled stream, no AI content)
	emptyBlocks, _ := json.Marshal(map[string]any{
		"blocks":    []any{},
		"metadata":  nil,
		"cancelled": true,
	})
	_, err = AddChatMessage("/test", "claude", sid, "assistant", string(emptyBlocks), nil, false, "")
	require.NoError(t, err)

	assert.False(t, SessionHasRealAssistantContent(sid), "empty cancelled placeholder should not count as real content")
}

func TestSessionHasRealAssistantContent_WarningOnly(t *testing.T) {
	setupExecutorDB(t)
	sid, err := CreateSession("/test", "claude", "test-real-content-warning", "", "", "claude", "chat")
	require.NoError(t, err)

	// Finalize a warning-only placeholder (context cancelled)
	warningBlocks, _ := json.Marshal(map[string]any{
		"blocks": []any{
			map[string]any{"type": "warning", "text": "AI response cancelled", "reason": "context_cancel"},
		},
		"metadata":  nil,
		"cancelled": true,
	})
	_, err = AddChatMessage("/test", "claude", sid, "assistant", string(warningBlocks), nil, false, "")
	require.NoError(t, err)

	assert.False(t, SessionHasRealAssistantContent(sid), "warning-only placeholder should not count as real content")
}

func TestSessionHasRealAssistantContent_RealTextBlock(t *testing.T) {
	setupExecutorDB(t)
	sid, err := CreateSession("/test", "claude", "test-real-content-text", "", "", "claude", "chat")
	require.NoError(t, err)

	// Finalize a message with real content
	realBlocks, _ := json.Marshal(map[string]any{
		"blocks": []any{
			map[string]any{"type": "text", "text": "Hello, I can help you with that."},
		},
		"metadata": map[string]any{"session_id": "ext-123"},
	})
	_, err = AddChatMessage("/test", "claude", sid, "assistant", string(realBlocks), nil, false, "")
	require.NoError(t, err)

	assert.True(t, SessionHasRealAssistantContent(sid), "message with text block should count as real content")
}

func TestSessionHasRealAssistantContent_ToolUseBlock(t *testing.T) {
	setupExecutorDB(t)
	sid, err := CreateSession("/test", "claude", "test-real-content-tool", "", "", "claude", "chat")
	require.NoError(t, err)

	// Tool use block counts as real content even if there's no text
	toolBlocks, _ := json.Marshal(map[string]any{
		"blocks": []any{
			map[string]any{"type": "tool_use", "id": "tu-1", "name": "Read"},
			map[string]any{"type": "warning", "text": "AI response cancelled", "reason": "context_cancel"},
		},
		"cancelled": true,
	})
	_, err = AddChatMessage("/test", "claude", sid, "assistant", string(toolBlocks), nil, false, "")
	require.NoError(t, err)

	assert.True(t, SessionHasRealAssistantContent(sid), "message with tool_use block should count as real content")
}

func TestSessionHasRealAssistantContent_PlainTextError(t *testing.T) {
	setupExecutorDB(t)
	sid, err := CreateSession("/test", "claude", "test-real-content-err", "", "", "claude", "chat")
	require.NoError(t, err)

	// Error messages from handler are plain text (not JSON)
	_, err = AddChatMessage("/test", "claude", sid, "assistant", "Backend create failed: exit 1", nil, false, "")
	require.NoError(t, err)

	assert.False(t, SessionHasRealAssistantContent(sid), "plain text error message should not count as real content")
}

func TestSessionHasRealAssistantContent_NoAssistantMessages(t *testing.T) {
	setupExecutorDB(t)
	sid, err := CreateSession("/test", "claude", "test-real-content-none", "", "", "claude", "chat")
	require.NoError(t, err)

	// Only user message, no assistant messages
	_, err = AddChatMessage("/test", "claude", sid, "user", "hello", nil, false, "")
	require.NoError(t, err)

	assert.False(t, SessionHasRealAssistantContent(sid), "session without assistant messages should not have real content")
}
