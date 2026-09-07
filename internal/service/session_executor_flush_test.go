package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- FlushStreamingNow (graceful shutdown flush) ---

// helper: creates a session with a streaming placeholder and an executor that
// has already accumulated a text + thinking block in memory.
func newFlushableExecutor(t *testing.T) (*SessionExecutor, int64) {
	t.Helper()
	agentID := "test-agent"
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
	return executor, cfg.StreamingMessageID
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

	executor, msgID := newFlushableExecutor(t)

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

	e1, msgID1 := newFlushableExecutor(t)
	e2, msgID2 := newFlushableExecutor(t)
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

	executor, _ := newFlushableExecutor(t)
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

// --- WaitStreamsDrained (graceful shutdown wait for finalize) ---

// TestWaitStreamsDrained_ReturnsImmediatelyWhenEmpty verifies that with no
// active streams the wait does not block.
func TestWaitStreamsDrained_ReturnsImmediatelyWhenEmpty(t *testing.T) {
	// activeStreams is a package-global registry; earlier tests may have left
	// registered executors behind. Snapshot and clear so this test measures
	// the empty-registry path deterministically.
	var leftovers []*SessionExecutor
	activeStreams.Range(func(_, value any) bool {
		if e, ok := value.(*SessionExecutor); ok {
			leftovers = append(leftovers, e)
		}
		return true
	})
	for _, e := range leftovers {
		e.unregisterActiveStream()
	}
	t.Cleanup(func() {
		for _, e := range leftovers {
			activeStreams.Store(e.cfg.SessionID, e)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	WaitStreamsDrained(ctx)
	assert.Less(t, time.Since(start), 500*time.Millisecond,
		"empty registry must not block")
}

// TestWaitStreamsDrained_ReturnsAfterUnregister verifies that the wait returns
// once the last active executor is unregistered (the semantics used by the
// graceful-shutdown path: an executor unregisters AFTER Finalize has persisted
// streaming=0).
func TestWaitStreamsDrained_ReturnsAfterUnregister(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	executor, _ := newFlushableExecutor(t)
	defer executor.unregisterActiveStream() // no-op if already unregistered

	// Simulate the graceful-shutdown path: flush first, then a goroutine
	// completes Finalize (here: unregister) shortly after.
	FlushStreamingNow()

	done := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		executor.unregisterActiveStream()
		close(done)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	WaitStreamsDrained(ctx)

	select {
	case <-done:
	default:
		t.Fatal("WaitStreamsDrained returned before the stream unregistered")
	}
}

// TestWaitStreamsDrained_DeadlineWithStuckStream verifies that when a stream
// never finalizes (e.g. a hung backend), the wait returns at the deadline
// instead of blocking forever — the graceful shutdown must not hang.
func TestWaitStreamsDrained_DeadlineWithStuckStream(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	executor, _ := newFlushableExecutor(t)
	// Intentionally leave the executor registered (simulating a stream stuck
	// in the event loop waiting for a backend that never returns).
	defer executor.unregisterActiveStream()

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	WaitStreamsDrained(ctx)

	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 150*time.Millisecond,
		"must wait until the deadline with a stuck stream")
	assert.Less(t, elapsed, time.Second,
		"must not block past the deadline")
}

// --- WaitSessionStreamDrained (per-session wait before in-place truncation) ---

// TestWaitSessionStreamDrained_ReturnsImmediatelyWhenNoExecutor verifies that
// the per-session wait does not block when the session has no active executor.
func TestWaitSessionStreamDrained_ReturnsImmediatelyWhenNoExecutor(t *testing.T) {
	start := time.Now()
	WaitSessionStreamDrained("session-with-no-executor", time.Second)
	assert.Less(t, time.Since(start), 500*time.Millisecond,
		"no active executor must not block")
}

// TestWaitSessionStreamDrained_ReturnsAfterUnregister verifies that the wait
// returns once THAT session's executor unregisters (Finalize completed), even
// while other sessions' executors remain registered.
func TestWaitSessionStreamDrained_ReturnsAfterUnregister(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	target, _ := newFlushableExecutor(t)
	defer target.unregisterActiveStream() // no-op if already unregistered
	other, _ := newFlushableExecutor(t)
	defer other.unregisterActiveStream() // stays registered for the whole wait

	// Unregister the target executor shortly after the wait starts.
	go func() {
		time.Sleep(50 * time.Millisecond)
		target.unregisterActiveStream()
	}()

	done := make(chan struct{})
	go func() {
		WaitSessionStreamDrained(target.cfg.SessionID, time.Second)
		close(done)
	}()

	select {
	case <-done:
		// Returned before the deadline — the "other" executor being active must
		// not block this session's wait.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("WaitSessionStreamDrained did not return after the session's executor unregistered")
	}
}

// TestWaitSessionStreamDrained_DeadlineWithStuckExecutor verifies that the wait
// returns at the deadline when the session's executor never unregisters.
func TestWaitSessionStreamDrained_DeadlineWithStuckExecutor(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	executor, _ := newFlushableExecutor(t)
	// Intentionally leave the executor registered (stuck stream).
	defer executor.unregisterActiveStream()

	start := time.Now()
	WaitSessionStreamDrained(executor.cfg.SessionID, 150*time.Millisecond)
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 150*time.Millisecond,
		"must wait until the deadline with a stuck executor")
	assert.Less(t, elapsed, time.Second,
		"must not block past the deadline")
}
