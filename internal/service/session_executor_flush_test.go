package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- FlushStreamingNow (graceful shutdown flush) ---

// helper: creates a session with a streaming placeholder and an executor that
// has already accumulated a text + thinking block in memory.
func newFlushableExecutor(t *testing.T, agentID string) (*SessionExecutor, string, int64) {
	t.Helper()
	sid := setupExecutorSession(t, agentID)
	ctx := context.Background()
	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            agentID,
		ChatRequest:        ai.ChatRequest{Prompt: "hello"},
		StreamingMessageID: getStreamingMsgIDForTest(t, sid),
	}
	executor := NewSessionExecutor(ctx, cfg)
	// Simulate an in-flight stream: one text block + one thinking block.
	executor.blocks = []model.ContentBlock{
		{Type: "text", Text: "partial answer"},
		{Type: "thinking", Text: "secret reasoning"},
	}
	return executor, sid, cfg.StreamingMessageID
}

func getStreamingMsgIDForTest(t *testing.T, sessionID string) int64 {
	t.Helper()
	var id int64
	err := dbRead.QueryRow(
		"SELECT id FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 1 ORDER BY id DESC LIMIT 1",
		sessionID,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func readStreamingContent(t *testing.T, msgID int64) map[string]any {
	t.Helper()
	var content string
	err := dbRead.QueryRow("SELECT content FROM chat_history WHERE id = ?", msgID).Scan(&content)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(content), &m))
	return m
}

func TestFlushStreamingNow_PersistsBlocksAndThinking(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	executor, _, msgID := newFlushableExecutor(t, "test-agent")

	// Graceful shutdown fires: force-flush every active stream.
	FlushStreamingNow()

	content := readStreamingContent(t, msgID)
	blocks, ok := content["blocks"].([]any)
	require.True(t, ok, "content should have blocks array")
	require.Len(t, blocks, 2, "force flush must include thinking blocks")

	types := map[string]bool{}
	var thinkIDFromBlock string
	for _, b := range blocks {
		bm, ok := b.(map[string]any)
		require.True(t, ok)
		types[bm["type"].(string)] = true
		if bm["type"] == "text" {
			assert.Equal(t, "partial answer", bm["text"])
		}
		if bm["type"] == "thinking" {
			// Slimmed: the streaming row keeps think_id, the full text lives in
			// chat_thinking — same representation Finalize produces.
			_, hasText := bm["text"]
			assert.False(t, hasText, "thinking text must be slimmed out of content")
			thinkIDFromBlock, _ = bm["think_id"].(string)
			assert.NotEmpty(t, thinkIDFromBlock)
		}
	}
	assert.True(t, types["text"], "text block must be flushed")
	assert.True(t, types["thinking"], "thinking block must be flushed")

	// Thinking must also be recorded in chat_thinking so the frontend can lazy-load it.
	records, err := GetThinkingBySessionAll(executor.cfg.SessionID)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, thinkIDFromBlock, records[0].ThinkID)
	assert.Equal(t, "secret reasoning", records[0].Text)

	// Executor finished → unregistered → a second flush must be a no-op.
	executor.unregisterActiveStream()
	FlushStreamingNow()
	// Content unchanged after the post-unregister flush.
	content2 := readStreamingContent(t, msgID)
	assert.Equal(t, content, content2)
}

func TestFlushStreamingNow_MultipleStreams(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	e1, _, msgID1 := newFlushableExecutor(t, "test-agent")
	e2, _, msgID2 := newFlushableExecutor(t, "test-agent")
	// Keep the registry clean across tests.
	defer e1.unregisterActiveStream()
	defer e2.unregisterActiveStream()

	// Distinct content per stream so a cross-stream mix-up is detectable.
	e1.mu.Lock()
	e1.blocks = []model.ContentBlock{{Type: "text", Text: "stream one"}}
	e1.mu.Unlock()
	e2.mu.Lock()
	e2.blocks = []model.ContentBlock{{Type: "text", Text: "stream two"}}
	e2.mu.Unlock()

	FlushStreamingNow()

	c1 := readStreamingContent(t, msgID1)
	c2 := readStreamingContent(t, msgID2)
	b1 := c1["blocks"].([]any)[0].(map[string]any)
	b2 := c2["blocks"].([]any)[0].(map[string]any)
	assert.Equal(t, "stream one", b1["text"])
	assert.Equal(t, "stream two", b2["text"])
}

// TestFlushStreamingNow_ConcurrentWithEventLoop exercises the real shutdown
// race: an event-loop goroutine appending blocks / rate-limiting flushing while
// FlushStreamingNow force-flushes from another goroutine. Run under -race this
// verifies the mutex protects the accumulated state.
func TestFlushStreamingNow_ConcurrentWithEventLoop(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	executor, _, _ := newFlushableExecutor(t, "test-agent")
	defer executor.unregisterActiveStream()

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Event-loop goroutine: keep appending blocks and rate-limited flushing.
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			executor.handleNonTerminalEvent(ai.StreamEvent{Type: "content", Content: "tick"})
			i++
			if i%10 == 0 {
				executor.flushStreamingMessage()
			}
		}
	}()

	// Shutdown goroutine: repeated force flushes while the loop runs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			FlushStreamingNow()
		}
	}()

	close(stop)
	wg.Wait()

	// No panic and no data race under -race. Content is best-effort: the preset
	// blocks may have flushed before any "tick" landed, so just require a valid
	// streaming row with our preset text preserved.
	var content string
	err := dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? AND streaming = 1 ORDER BY id DESC LIMIT 1",
		executor.cfg.SessionID,
	).Scan(&content)
	require.NoError(t, err)
	assert.Contains(t, content, "partial answer")
}

func TestFlushStreamingNow_NoRegisteredStreams(t *testing.T) {
	setupExecutorDB(t)

	// Registry starts empty — must be a safe no-op.
	assert.NotPanics(t, FlushStreamingNow)
}
