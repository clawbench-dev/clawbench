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

// --- Batched tool-call upserts ---

// TestExecutor_BatchedToolCallsFlushedTogether verifies that multiple tool
// events arriving between flush windows are upserted as a batch by the flush
// rather than written per event, and that the batch is cleared after flushing.
func TestExecutor_BatchedToolCallsFlushedTogether(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})
	// Keep the package-global activeStreams registry clean for later tests.
	defer executor.unregisterActiveStream()

	// Two distinct tools, each emitting several incremental events. The first
	// event triggers an immediate flush (lastFlush starts zeroed), but the
	// second tool's events land in the pending batch and stay queued.
	executor.handleNonTerminalEvent(ai.StreamEvent{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "bt-1", Input: `{"file_path":"/a.go"}`, Done: false}})
	executor.handleNonTerminalEvent(ai.StreamEvent{Type: "tool_use", Tool: &ai.ToolCall{Name: "Bash", ID: "bt-2", Input: `{"command":"ls"}`, Done: false}})
	executor.handleNonTerminalEvent(ai.StreamEvent{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "bt-1", Input: `{"file_path":"/a.go"}`, Done: true}})

	// The second tool's row is queued, not written — it landed after the first
	// event's flush and no flush window elapsed since.
	r2, err := GetToolCall("bt-2", msgID)
	require.NoError(t, err)
	assert.Nil(t, r2, "tool-call upsert must be deferred to the flush window")

	// One flush persists the queued row (and re-upserts bt-1 with its final
	// done=true state).
	executor.flushStreamingMessage()

	r1, err := GetToolCall("bt-1", msgID)
	require.NoError(t, err)
	require.NotNil(t, r1)
	assert.True(t, r1.Done, "bt-1 final state must reflect the last update")
	r2, err = GetToolCall("bt-2", msgID)
	require.NoError(t, err)
	require.NotNil(t, r2)

	// Queue is cleared after the flush: a second flush writes nothing new.
	executor.flushStreamingMessage()
	assert.Len(t, executor.pendingToolCalls, 0, "pending tool-call set must be drained by flush")
}

// TestExecutor_BatchedContextStateFlushed verifies that context-state updates
// (usage/mode) are accumulated in the pending map and applied atomically by
// the flush, rather than written per event.
func TestExecutor_BatchedContextStateFlushed(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
	})
	// Keep the package-global activeStreams registry clean for later tests.
	defer executor.unregisterActiveStream()

	// Burst of usage updates — the pending map keeps only the latest per key.
	for i := range 5 {
		executor.persistContextStateToPending(ai.StreamEvent{
			Type: "usage_update",
			Usage: &ai.UsageState{
				InputTokens:  i * 100,
				OutputTokens: i * 10,
			},
		})
	}
	executor.persistContextStateToPending(ai.StreamEvent{
		Type: "mode_update",
		Mode: &ai.ModeState{CurrentModeID: "code"},
	})

	// Not yet in DB.
	assert.Nil(t, GetContextState(sid), "context state must be deferred to the flush window")

	executor.flushStreamingMessage()

	state := GetContextState(sid)
	require.NotNil(t, state)
	require.NotNil(t, state.Usage)
	assert.Equal(t, 400, state.Usage.InputTokens, "latest usage value must win")
	require.NotNil(t, state.Mode)
	assert.Equal(t, "code", state.Mode.CurrentModeID)

	// Pending map drained.
	assert.Len(t, executor.pendingContextPatches, 0, "pending context patches must be drained by flush")
}

// TestExecutor_FlushSkipsUnchangedContent verifies that a rate-limited flush
// does not re-write the streaming row when the marshaled content is unchanged,
// while a content change does write.
func TestExecutor_FlushSkipsUnchangedContent(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
	})
	// Keep the package-global activeStreams registry clean for later tests.
	defer executor.unregisterActiveStream()

	// First flush writes the initial content.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "content", Content: "hello"})
	executor.flushStreamingMessage()
	require.NotEmpty(t, executor.lastWrittenContent, "first flush must record written content")

	// Second flush with no change must not re-write (lastWrittenContent unchanged).
	before := executor.lastWrittenContent
	executor.flushStreamingMessage()
	assert.Equal(t, before, executor.lastWrittenContent, "unchanged flush must not rewrite")

	// A change is picked up by the marshaled comparison.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "content", Content: " world"})
	executor.flushStreamingMessage()
	assert.NotEqual(t, before, executor.lastWrittenContent, "changed content must be written")
}

// TestExecutor_FlushStreamingNow_IncludeThinkingAndBatched verifies that the
// graceful-shutdown flush persists both the batched tool-call rows and the
// streaming content (with thinking) in one shot.
func TestExecutor_FlushStreamingNow_IncludeThinkingAndBatched(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})
	defer executor.unregisterActiveStream()

	// Accumulate blocks + queue a tool-call via the event path.
	executor.handleNonTerminalEvent(ai.StreamEvent{Type: "content", Content: "hello"})
	executor.handleNonTerminalEvent(ai.StreamEvent{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "shutdown-batch-1", Done: true}})

	// Graceful shutdown flush.
	FlushStreamingNow()

	// Tool-call row persisted.
	record, err := GetToolCall("shutdown-batch-1", msgID)
	require.NoError(t, err)
	require.NotNil(t, record, "graceful flush must persist batched tool-call rows")

	// Streaming row content persisted: text + tool_use blocks.
	content := readStreamingContent(t, msgID)
	blocks, ok := content["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, blocks, 2)
}

// --- Batched thinking upserts ---

// TestExecutor_ThinkingFlushedPeriodically verifies that a growing thinking
// block is persisted to chat_thinking on every flush window with a stable
// think_id and INCREMENTAL seq chunks (each flush appends only the delta grown
// since the previous window — the reader concatenates them), while the content
// row EXCLUDES the thinking block entirely (no slim marker, no text) — the
// completed message's think_id markers are produced once at finalization by
// persistThinkingToDB.
func TestExecutor_ThinkingFlushedPeriodically(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})
	// Must unregister: the activeStreams registry is package-global, and a
	// leftover executor's flush would run against the NEXT test's DB (where
	// AUTOINCREMENT message IDs collide), deleting that test's chat_thinking
	// rows via DeleteThinkingByMessage.
	defer executor.unregisterActiveStream()

	// Thinking grows: flush1 persists "part1" at seq0, flush2 appends "part2"
	// at seq1 under the SAME think_id.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "part1"})
	executor.flushStreamingMessage()
	require.Len(t, executor.blocks, 1)
	firstID := executor.blocks[0].ThinkID
	require.NotEmpty(t, firstID, "thinking block must get a stable think_id on flush")

	rec, err := GetThinking(firstID, msgID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "part1", rec.Text)

	// Second flush with more thinking — same think_id, text grows.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "part2"})
	executor.flushStreamingMessage()
	assert.Equal(t, firstID, executor.blocks[0].ThinkID, "think_id must be stable across flushes")
	rec, err = GetThinking(firstID, msgID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "part1part2", rec.Text, "reader concatenates seq chunks into the full text")

	// Two chunks now: seq0="part1", seq1="part2" — the flush appends only the
	// delta grown since the last window instead of rewriting the full text.
	count := 0
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&count)
	assert.Equal(t, 2, count, "periodic flush must append incremental segments, not rewrite one row")
	var seqs []int
	rows, err := dbRead.Query("SELECT seq FROM chat_thinking WHERE message_id = ? ORDER BY seq", msgID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var s int
		require.NoError(t, rows.Scan(&s))
		seqs = append(seqs, s)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int{0, 1}, seqs, "segments must be stored at seq 0 then seq 1")

	// Content row EXCLUDES the thinking block entirely — a slim think_id marker
	// here would leak an "empty thinking block" into the frontend's live
	// placeholder via mergeStreamBlocks (db_load after stream_start), rendering
	// a perpetual loading spinner until the message finalizes.
	content := readStreamingContent(t, msgID)
	assert.NotContains(t, content, "thinking", "streaming content must not carry any thinking block")
	assert.NotContains(t, content, "part1part2", "content must NOT carry the full thinking text")
	assert.NotContains(t, content, firstID, "content must NOT carry the think_id marker during streaming")
}

// TestExecutor_ThinkingFlush_FinalizeReusesID verifies that Finalize reuses the
// periodic-flush think_id (no orphan/duplicate rows) and that the final text
// (after any merge) overwrites the periodic-flush row.
func TestExecutor_ThinkingFlush_FinalizeReusesID(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})

	// Periodic flush first, capturing the stable think_id.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "early"})
	executor.flushStreamingMessage()
	firstID := executor.blocks[0].ThinkID
	require.NotEmpty(t, firstID)

	// Finalize: full blocks (thinking text + think_id) → slimmed into
	// chat_thinking under the SAME id.
	result := executor.buildResult(true, time.Now())
	finalized := executor.Finalize(result, nil)
	require.NotZero(t, finalized.MsgID)

	rec, err := GetThinking(firstID, finalized.MsgID)
	require.NoError(t, err)
	require.NotNil(t, rec, "Finalize must keep the periodic-flush think_id")
	assert.Equal(t, "early", rec.Text)

	// No duplicate rows for that id.
	count := 0
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE think_id = ?", firstID).Scan(&count)
	assert.Equal(t, 1, count)

	// The finalized content's slim thinking block must reference the SAME
	// periodic-flush think_id, so the frontend's lazy-load (think_id +
	// message_id) resolves to the row that the periodic flush kept warm.
	var content string
	err = dbRead.QueryRow("SELECT content FROM chat_history WHERE id = ?", finalized.MsgID).Scan(&content)
	require.NoError(t, err)
	var contentMap map[string]any
	require.NoError(t, json.Unmarshal([]byte(content), &contentMap))
	blocksRaw, ok := contentMap["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, blocksRaw, 1)
	thinkingBlock, ok := blocksRaw[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "thinking", thinkingBlock["type"])
	assert.Equal(t, firstID, thinkingBlock["think_id"], "finalized slim block must reuse the periodic-flush think_id")
	assert.NotContains(t, thinkingBlock, "text", "finalized content must not carry the thinking text")
}

// TestExecutor_ThinkingFlush_MergeUpdatesText verifies that when
// MergeConsecutiveThinkingBlocks concatenates two thinking fragments in
// Finalize, the merged text overwrites the periodic-flush row (the slim logic
// reuses the existing think_id and re-extracts the final text).
func TestExecutor_ThinkingFlush_MergeUpdatesText(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})

	// Two separate thinking fragments — periodic flush persists the first.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "frag-a"})
	executor.flushStreamingMessage()
	firstID := executor.blocks[0].ThinkID

	// A second thinking block appears (e.g. interleaved after a tool_use).
	executor.blocks = append(executor.blocks, model.ContentBlock{Type: "thinking", Text: "frag-b"})

	// Finalize merges consecutive thinking blocks and must end up with the
	// combined text under the first block's think_id.
	result := executor.buildResult(true, time.Now())
	finalized := executor.Finalize(result, nil)

	rec, err := GetThinking(firstID, finalized.MsgID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "frag-afrag-b", rec.Text, "merged thinking text must overwrite the periodic-flush row")
}

// TestExecutor_ContentReset_ClearsThinkingAndLastWritten verifies that a
// content_reset removes stale chat_thinking rows written by the periodic flush
// (so a retry can't leave the frontend lazy-loading reasoning from the failed
// attempt) and resets lastWrittenContent so the retry's first flush is written.
func TestExecutor_ContentReset_ClearsThinkingAndLastWritten(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})
	defer executor.unregisterActiveStream()

	// First (failed) attempt: thinking is periodically flushed to chat_thinking.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "stale reasoning"})
	executor.flushStreamingMessage()
	require.NotEmpty(t, executor.blocks[0].ThinkID)
	rec, err := GetThinking(executor.blocks[0].ThinkID, msgID)
	require.NoError(t, err)
	require.NotNil(t, rec, "periodic flush must persist the stale thinking")

	// content_reset fires on retry.
	executor.handleNonTerminalEvent(ai.StreamEvent{Type: "content_reset"})

	// Stale chat_thinking row removed.
	var cnt int
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&cnt)
	assert.Zero(t, cnt, "content_reset must delete stale periodic-flush thinking rows")

	// lastWrittenContent reset so the retry's first flush always writes.
	assert.Empty(t, executor.lastWrittenContent, "content_reset must reset lastWrittenContent")
	executor.flushStreamingMessage()
	assert.NotEmpty(t, executor.lastWrittenContent, "first flush after reset must write")

	// No stale thinking visible for the message.
	var cntAfter int
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&cntAfter)
	assert.Zero(t, cntAfter, "no thinking rows must reappear after the retry flush with empty blocks")
}

// TestExecutor_ForceFlush_ThenRateLimitedFlush_KeepsSlimThinking locks the
// forceIncludeThinking sticky semantics: once the graceful-shutdown forced
// flush runs (includeThinking=true), every subsequent rate-limited flush keeps
// writing the thinking blocks (slimmed) into content. This prevents a flush
// racing the process exit from overwriting the just-persisted thinking with a
// thinking-less body while chat_thinking already holds records the frontend
// would lazy-load by think_id.
func TestExecutor_ForceFlush_ThenRateLimitedFlush_KeepsSlimThinking(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})
	defer executor.unregisterActiveStream()

	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "sticky reasoning"})

	// Force flush: full thinking text embedded in content, then slimmed into
	// chat_thinking; forceIncludeThinking becomes sticky.
	executor.flushStreamingLocked(true)
	firstID := executor.blocks[0].ThinkID
	require.NotEmpty(t, firstID)

	rec, err := GetThinking(firstID, msgID)
	require.NoError(t, err)
	require.NotNil(t, rec, "force flush must persist thinking full text")
	assert.Equal(t, "sticky reasoning", rec.Text)

	// Rate-limited flush afterwards: forceIncludeThinking is sticky, so the
	// thinking block stays in content (slimmed — think_id, no text).
	executor.flushStreamingMessage()
	content := readStreamingContent(t, msgID)
	blocksRaw, ok := content["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, blocksRaw, 1)
	thinkingBlock, ok := blocksRaw[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "thinking", thinkingBlock["type"])
	assert.Equal(t, firstID, thinkingBlock["think_id"], "sticky force flush must keep the slim think_id in content")
	assert.NotContains(t, thinkingBlock, "text", "rate-limited flush after force flush must slim the text")

	// The rate-limited flush ran after the forced flush; forceIncludeThinking is
	// sticky so persistThinkingToDB owns persistence (incremental append is
	// disabled). No thinking grew in between, so the single seq=0 row is intact.
	rec, err = GetThinking(firstID, msgID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "sticky reasoning", rec.Text, "no duplicated text after force flush + rate-limited flush")
	var cnt int
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE think_id = ?", firstID).Scan(&cnt)
	assert.Equal(t, 1, cnt, "force flush rewrote seq=0; rate-limited flush must not append a duplicate chunk")
}

// TestExecutor_ForceFlush_ThenGrowth_FullRewrite verifies force mode semantics:
// after a forced full write, subsequent rate-limited flushes keep the thinking
// block in content and persistThinkingToDB (not the incremental cursor) owns
// persistence — the full updated text lives in a single seq=0 row, never
// duplicated by incremental appends (flushPendingThinking is guarded off while
// forceIncludeThinking is sticky).
func TestExecutor_ForceFlush_ThenGrowth_FullRewrite(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})
	defer executor.unregisterActiveStream()

	// Force flush writes the full text as a single seq=0 row.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "base text"})
	executor.flushStreamingLocked(true)
	firstID := executor.blocks[0].ThinkID
	require.NotEmpty(t, firstID)
	rec, err := GetThinking(firstID, msgID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "base text", rec.Text)

	// Thinking grows. The rate-limited flush (forceIncludeThinking sticky)
	// persists the FULL updated text via persistThinkingToDB into the seq=0
	// row — incremental append is disabled in force mode, so no seq=1 chunk.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: " +more"})
	executor.flushStreamingMessage()

	rec, err = GetThinking(firstID, msgID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "base text +more", rec.Text, "full updated text preserved after force-mode growth")

	// Exactly one chunk: force mode rewrites seq=0 rather than appending.
	var seqs []int
	rows, err := dbRead.Query("SELECT seq FROM chat_thinking WHERE think_id = ? ORDER BY seq", firstID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var s int
		require.NoError(t, rows.Scan(&s))
		seqs = append(seqs, s)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int{0}, seqs, "force mode must keep a single seq=0 full rewrite, no incremental chunks")
}

// --- Thinking slim markers in the streaming content row ---
//
// A thinking block leaves a slim {think_id} marker in the streaming content row
// in two shapes: {done:true} once thinking_done arrives, and {in_progress:true}
// while it is still streaming. The in-progress marker is what makes a session
// switch lossless — see ContentBlock.InProgress. A block with no text (nothing
// persisted to chat_thinking) gets no marker at all: it would lazy-load to a 404.

// TestExecutor_DoneThinking_WritesSlimMarkerInContent verifies that a thinking
// block marked done mid-stream gets a slim marker in the streaming content row
// (text stripped, think_id + done preserved), and that the text is retrievable
// via GetThinking — the exact contract a page refresh relies on.
func TestExecutor_DoneThinking_WritesSlimMarkerInContent(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})
	defer executor.unregisterActiveStream()

	// Thinking streams and gets persisted; capture the stable think_id.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "reasoned"})
	executor.flushStreamingMessage()
	require.Len(t, executor.blocks, 1)
	firstID := executor.blocks[0].ThinkID
	require.NotEmpty(t, firstID)

	// In-progress: the content row carries an in_progress marker (not a done one),
	// so a session switch keeps the block's identity and can lazy-load the prefix.
	content := readStreamingContent(t, msgID)
	blocksRaw, ok := content["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, blocksRaw, 1, "an in-progress block must be represented in the streaming row")
	inProg, ok := blocksRaw[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, firstID, inProg["think_id"])
	assert.Equal(t, true, inProg["in_progress"], "still-streaming block must be marked in_progress")
	assert.NotEqual(t, true, inProg["done"], "an in-progress block must not claim to be done")

	// thinking_done arrives → next flush rewrites the marker as done.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking_done"})
	executor.flushStreamingMessage()

	content = readStreamingContent(t, msgID)
	doneBlocks, ok := content["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, doneBlocks, 1)
	thinkingBlock, ok := doneBlocks[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "thinking", thinkingBlock["type"])
	assert.Equal(t, firstID, thinkingBlock["think_id"], "done marker must carry the stable think_id")
	assert.Equal(t, true, thinkingBlock["done"], "done marker must carry done=true")
	assert.NotContains(t, thinkingBlock, "in_progress", "a done block must not still be flagged in_progress")
	assert.NotContains(t, thinkingBlock, "text", "content must not carry the thinking text")

	// Refresh-simulation: the marker resolves to the full text via GetThinking.
	rec, err := GetThinking(firstID, msgID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "reasoned", rec.Text)
}

// TestExecutor_InProgressThinking_WritesInProgressMarker pins the A-plan
// contract: a still-streaming block IS represented in the streaming row, as an
// in_progress marker. Omitting it entirely (the old behavior) cost the
// already-streamed prefix on a session switch and made the next delta open a
// second, done-less block that spun forever.
func TestExecutor_InProgressThinking_WritesInProgressMarker(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})
	defer executor.unregisterActiveStream()

	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "part1"})
	executor.flushStreamingMessage()
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "part2"})
	executor.flushStreamingMessage()

	content := readStreamingContent(t, msgID)
	blocksRaw, ok := content["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, blocksRaw, 1, "in-progress thinking must be represented in the streaming row")
	blk, ok := blocksRaw[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "thinking", blk["type"])
	assert.Equal(t, true, blk["in_progress"], "must be flagged in_progress, not done")
	assert.NotEqual(t, true, blk["done"], "an unfinished block must not be marked done")
	// The marker must not carry the text — that lives in chat_thinking.
	assert.NotContains(t, blk, "text")

	// The prefix so far is retrievable, which is what the frontend lazy-loads.
	rec, err := GetThinking(blk["think_id"].(string), msgID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "part1part2", rec.Text)
}

// TestExecutor_EmptyDoneThinking_NoMarker verifies that a thinking block with
// no text (never persisted to chat_thinking) does NOT produce a slim marker —
// the marker would lazy-load to a 404 on expand.
func TestExecutor_EmptyDoneThinking_NoMarker(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})
	defer executor.unregisterActiveStream()

	// An empty thinking block that completes: no text ever reached chat_thinking.
	executor.blocks = append(executor.blocks, model.ContentBlock{Type: "thinking"})
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking_done"})
	executor.flushStreamingMessage()

	content := readStreamingContent(t, msgID)
	blocksRaw, ok := content["blocks"].([]any)
	require.True(t, ok)
	assert.Empty(t, blocksRaw, "empty done thinking block must not leave a marker (no row to lazy-load)")
}

// TestExecutor_DoneMarkerThenFinalize_NoDuplicates verifies that a slim marker
// written mid-stream survives into the finalized message under the same
// think_id with exactly one seq=0 row — Finalize is idempotent over the
// mid-stream marker and leaves no orphan/duplicate rows.
func TestExecutor_DoneMarkerThenFinalize_NoDuplicates(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})

	// Stream → done → marker appears mid-stream.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "final reasoning"})
	executor.flushStreamingMessage()
	firstID := executor.blocks[0].ThinkID
	require.NotEmpty(t, firstID)
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking_done"})
	executor.flushStreamingMessage()

	mid := readStreamingContent(t, msgID)
	midBlocks, ok := mid["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, midBlocks, 1, "mid-stream content must carry the done marker")
	midThinking, ok := midBlocks[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, firstID, midThinking["think_id"], "done marker must be present mid-stream")

	// Finalize: message completes, marker overwritten under the same think_id.
	result := executor.buildResult(true, time.Now())
	finalized := executor.Finalize(result, nil)
	require.NotZero(t, finalized.MsgID)

	var content string
	err := dbRead.QueryRow("SELECT content FROM chat_history WHERE id = ?", finalized.MsgID).Scan(&content)
	require.NoError(t, err)
	assert.Contains(t, content, firstID, "finalized slim marker must keep the mid-stream think_id")

	var contentMap map[string]any
	require.NoError(t, json.Unmarshal([]byte(content), &contentMap))
	blocksRaw, ok := contentMap["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, blocksRaw, 1)
	thinkingBlock, ok := blocksRaw[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "thinking", thinkingBlock["type"])
	assert.Equal(t, firstID, thinkingBlock["think_id"])
	assert.NotContains(t, thinkingBlock, "text", "finalized content must stay slim")

	// Exactly one seq=0 row — no duplicate/orphan chunks.
	count := 0
	_ = dbRead.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE think_id = ? AND message_id = ?", firstID, finalized.MsgID).Scan(&count)
	assert.Equal(t, 1, count, "Finalize must collapse the streaming chunks into one seq=0 row")

	rec, err := GetThinking(firstID, finalized.MsgID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "final reasoning", rec.Text, "full text must survive Finalize")
}

// TestExecutor_DoneAndInProgressThinking_SameFlush verifies that a single flush
// can carry a slim marker for a DONE block while an in-progress block stays
// excluded — the two rules compose.
func TestExecutor_DoneAndInProgressThinking_SameFlush(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	msgID := getStreamingMsgIDForTest(t, sid)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          sid,
		AgentID:            "test-agent",
		StreamingMessageID: msgID,
	})
	defer executor.unregisterActiveStream()

	// First thinking block completes.
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking", Content: "first done reasoning"})
	executor.flushStreamingMessage()
	doneID := executor.blocks[0].ThinkID
	require.NotEmpty(t, doneID)
	ai.AccumulateBlock(&executor.blocks, ai.StreamEvent{Type: "thinking_done"})

	// Second thinking block (after a tool boundary) is still in progress.
	executor.blocks = append(executor.blocks, model.ContentBlock{Type: "thinking"})
	executor.blocks[len(executor.blocks)-1].Text = "second ongoing"
	executor.flushStreamingMessage()

	content := readStreamingContent(t, msgID)
	blocksRaw, ok := content["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, blocksRaw, 2, "both the done and the in-progress block leave a marker")
	doneBlock, ok := blocksRaw[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, doneID, doneBlock["think_id"], "done block's marker must be in content")
	assert.Equal(t, true, doneBlock["done"], "the finished block must be marked done")
	inProgBlock, ok := blocksRaw[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, inProgBlock["in_progress"], "the unfinished block must be marked in_progress")
	assert.NotEqual(t, true, inProgBlock["done"])
	assert.NotContains(t, content, "second ongoing", "in-progress block's text must not be in content")
}
