package service

import (
	"context"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// The generation-span accounting is what makes the reported speed honest:
// tool execution and waiting time must never end up in the denominator.
// Synthetic timestamps keep this test deterministic — a sleep-based version
// would assert machine load, not behavior.
func TestSessionExecutor_TrackGenerationSpan(t *testing.T) {
	e := &SessionExecutor{}
	t0 := time.Unix(1_700_000_000, 0)

	// Talk for 100ms, then hand over to a tool.
	e.trackGenerationSpan(ai.StreamEvent{Type: "content", Content: "hello"}, t0)
	e.trackGenerationSpan(ai.StreamEvent{Type: "content", Content: " world"}, t0.Add(50*time.Millisecond))
	e.trackGenerationSpan(ai.StreamEvent{Type: eventTypeToolUse, Tool: &ai.ToolCall{ID: "tool-1"}}, t0.Add(100*time.Millisecond))
	require.Equal(t, int64(100), e.genMs)

	// Tool runs for 800ms — none of it may count. The tool_result event closes
	// nothing because no span is open.
	e.trackGenerationSpan(ai.StreamEvent{Type: eventTypeToolResult, Tool: &ai.ToolCall{ID: "tool-1"}}, t0.Add(900*time.Millisecond))
	require.Equal(t, int64(100), e.genMs)

	// Second burst, closed by the turn ending (not by a tool).
	e.trackGenerationSpan(ai.StreamEvent{Type: "thinking", Content: "hmm"}, t0.Add(time.Second))
	e.closeGenerationSpan(t0.Add(1150 * time.Millisecond))
	require.Equal(t, int64(250), e.genMs)

	// Idempotent: a second close must not add anything.
	e.closeGenerationSpan(t0.Add(2 * time.Second))
	require.Equal(t, int64(250), e.genMs)
}

// An empty delta must not open a span: agents emit empty chunks to mark block
// boundaries, and counting them would attribute pure waiting to the model.
func TestSessionExecutor_TrackGenerationSpan_IgnoresEmptyDeltas(t *testing.T) {
	e := &SessionExecutor{}
	t0 := time.Unix(1_700_000_000, 0)
	e.trackGenerationSpan(ai.StreamEvent{Type: "content", Content: ""}, t0)
	e.trackGenerationSpan(ai.StreamEvent{Type: "thinking", Content: ""}, t0.Add(time.Second))
	require.True(t, e.genSpanStart.IsZero(), "empty deltas must not open a generation span")
	require.Zero(t, e.genMs)
}

func TestSessionExecutor_ResetGenerationSpans(t *testing.T) {
	e := &SessionExecutor{genMs: 900}
	e.trackGenerationSpan(ai.StreamEvent{Type: "content", Content: "x"}, time.Now())
	e.resetGenerationSpans()
	require.Zero(t, e.genMs)
	require.True(t, e.genSpanStart.IsZero())
}

// Wiring: a real event loop must open/close spans on the raw stream. Only the
// lower bound is asserted (a busy machine can only make the window longer).
func TestSessionExecutor_RunWithChannel_MeasuresGenerationWindow(t *testing.T) {
	ch := make(chan ai.StreamEvent, 4)
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   "sess-gen-span",
		AgentID:     "test",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}
	executor := NewSessionExecutor(context.Background(), cfg)
	go func() {
		ch <- ai.StreamEvent{Type: "content", Content: "hello"}
		time.Sleep(40 * time.Millisecond)
		ch <- ai.StreamEvent{Type: "content", Content: " world"}
		ch <- ai.StreamEvent{Type: "done"}
		close(ch)
	}()

	result := executor.RunWithChannel(ch)
	require.NotNil(t, result.Metadata)
	require.Greater(t, result.Metadata.GenMs, 0, "a streamed reply must have a measured generation window")
}

// A turn that only ran tools (no streamed text) has no denominator at all.
func TestSessionExecutor_RunWithChannel_ToolOnlyTurnHasNoGenerationWindow(t *testing.T) {
	result := runExecutorWithEvents(t, []ai.StreamEvent{
		{Type: eventTypeToolUse, Tool: &ai.ToolCall{ID: "tool-1", Name: "bash", Done: true}},
		{Type: eventTypeToolResult, Tool: &ai.ToolCall{ID: "tool-1", Name: "bash", Done: true}},
		{Type: "done"},
	}, ModeScheduled)

	require.NotNil(t, result.Metadata)
	require.Zero(t, result.Metadata.GenMs)
}

// Finalize must publish the speed in the message metadata (read by the single
// chat popup) and on the session row (read by the settings drawer, which is the
// only path that works for a group member — its own row has no chat_history).
func TestSessionExecutor_Finalize_PersistsTokenRate(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	streamMsgID := GetStreamingMessageID(sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: streamMsgID,
	})
	executor.blocks = []model.ContentBlock{{Type: "text", Text: "ok"}}

	finalized := executor.Finalize(RunResult{
		ReceivedTerminal: true,
		Metadata:         &ai.Metadata{OutputTokens: 100, GenMs: 2000},
	}, nil)

	require.InDelta(t, 50, finalized.Metadata.TokensPerSecond, 0.001)
	stat := GetSessionTokenRate(sid)
	require.NotNil(t, stat)
	require.InDelta(t, 50, stat.TokensPerSecond, 0.001)
	require.Equal(t, 2000, stat.GenMs)
}

// An unmeasurable turn clears the previous value: the UI labels the row as the
// LATEST reply's speed, so keeping an older number would be a lie.
func TestPersistSessionTokenRate_ClearsWhenUnmeasurable(t *testing.T) {
	setupExecutorDB(t)
	sid := setupExecutorSession(t, "test-agent")

	PersistSessionTokenRate(sid, &ai.Metadata{TokensPerSecond: 12.5, GenMs: 4000})
	stat := GetSessionTokenRate(sid)
	require.NotNil(t, stat)
	require.InDelta(t, 12.5, stat.TokensPerSecond, 0.001)

	PersistSessionTokenRate(sid, &ai.Metadata{})
	require.Nil(t, GetSessionTokenRate(sid), "the latest turn had no measurable speed, so the row must disappear")
}

func TestGetSessionTokenRate_UnknownSession(t *testing.T) {
	setupExecutorDB(t)
	require.Nil(t, GetSessionTokenRate("does-not-exist"))
	require.Nil(t, GetSessionTokenRate(""))
}
