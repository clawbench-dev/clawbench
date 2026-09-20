package service

import (
	"strings"
	"testing"

	"clawbench/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCoalescer returns a coalescer plus a pointer to the slice of events it
// emitted, so tests can assert on both content and ORDER.
func newTestCoalescer() (*streamCoalescer, *[]ai.StreamEvent) {
	var emitted []ai.StreamEvent
	c := &streamCoalescer{emit: func(ev ai.StreamEvent) { emitted = append(emitted, ev) }}
	return c, &emitted
}

func content(text string) ai.StreamEvent {
	return ai.StreamEvent{Type: "content", Content: text}
}

func thinking(text string) ai.StreamEvent {
	return ai.StreamEvent{Type: "thinking", Content: text}
}

// A run of same-block deltas must collapse into ONE emitted event carrying the
// concatenated text. This is the entire point of the type.
func TestCoalescer_MergesConsecutiveContentDeltas(t *testing.T) {
	c, emitted := newTestCoalescer()

	c.add(content("Hello"))
	c.add(content(" "))
	c.add(content("world"))
	// Nothing may be emitted before the flush window closes.
	assert.Empty(t, *emitted, "deltas must stay buffered until flush")

	c.flush()

	require.Len(t, *emitted, 1, "three deltas must merge into one frame")
	assert.Equal(t, "Hello world", (*emitted)[0].Content)
	assert.Equal(t, "content", (*emitted)[0].Type)
}

// Same as above for thinking, which takes a different payload key downstream
// (simpleTextPayload maps thinking→"text", content→"content").
func TestCoalescer_MergesConsecutiveThinkingDeltas(t *testing.T) {
	c, emitted := newTestCoalescer()

	c.add(thinking("step one "))
	c.add(thinking("step two"))
	c.flush()

	require.Len(t, *emitted, 1)
	assert.Equal(t, "step one step two", (*emitted)[0].Content)
	assert.Equal(t, "thinking", (*emitted)[0].Type)
}

// The frontend appends by (type, parent): findBlockByTypeBackward(blocks, type,
// parent). Merging across parents would attribute a sub-agent's text to the
// top-level block, so this is a correctness boundary, not a tuning knob.
func TestCoalescer_DoesNotMergeAcrossParentToolCallIDs(t *testing.T) {
	c, emitted := newTestCoalescer()

	top := content("top-level ")
	sub := content("sub-agent ")
	sub.ParentToolCallID = "call_sub"

	c.add(top)
	c.add(sub)

	// Buffering `sub` (different parent) must have flushed `top` first.
	require.Len(t, *emitted, 1)
	assert.Equal(t, "top-level ", (*emitted)[0].Content)
	assert.Empty(t, (*emitted)[0].ParentToolCallID)

	c.flush()
	require.Len(t, *emitted, 2)
	assert.Equal(t, "sub-agent ", (*emitted)[1].Content)
	assert.Equal(t, "call_sub", (*emitted)[1].ParentToolCallID,
		"the sub-agent delta must keep its parent id")
}

// Two sub-agents interleaving is the case that fragments a message into
// thousands of tiny blocks. Each parent must get its own frame.
func TestCoalescer_InterleavedSubAgentsDoNotMergeIntoEachOther(t *testing.T) {
	c, emitted := newTestCoalescer()

	a := content("A1")
	a.ParentToolCallID = "call_a"
	a2 := content("A2")
	a2.ParentToolCallID = "call_a"
	b := content("B1")
	b.ParentToolCallID = "call_b"

	c.add(a)
	c.add(b) // different parent -> flushes A1
	c.add(a2)
	c.flush()

	require.Len(t, *emitted, 3)
	assert.Equal(t, "A1", (*emitted)[0].Content)
	assert.Equal(t, "call_a", (*emitted)[0].ParentToolCallID)
	assert.Equal(t, "B1", (*emitted)[1].Content)
	assert.Equal(t, "call_b", (*emitted)[1].ParentToolCallID)
	assert.Equal(t, "A2", (*emitted)[2].Content)
	assert.Equal(t, "call_a", (*emitted)[2].ParentToolCallID)
}

// content and thinking land in different blocks; merging them would concatenate
// reasoning into the visible answer.
func TestCoalescer_DoesNotMergeContentWithThinking(t *testing.T) {
	c, emitted := newTestCoalescer()

	c.add(content("answer "))
	c.add(thinking("reasoning"))
	c.flush()

	require.Len(t, *emitted, 2)
	assert.Equal(t, "content", (*emitted)[0].Type)
	assert.Equal(t, "answer ", (*emitted)[0].Content)
	assert.Equal(t, "thinking", (*emitted)[1].Type)
	assert.Equal(t, "reasoning", (*emitted)[1].Content)
}

// A non-delta event is an ordering barrier: it must not overtake buffered text,
// or the frontend would render a tool card above the prose that precedes it.
func TestCoalescer_NonDeltaFlushesBufferedTextFirst(t *testing.T) {
	c, emitted := newTestCoalescer()

	c.add(content("prose before tool"))
	c.add(ai.StreamEvent{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "t1"}})

	require.Len(t, *emitted, 2, "tool_use must not overtake the buffered content")
	assert.Equal(t, "content", (*emitted)[0].Type)
	assert.Equal(t, "prose before tool", (*emitted)[0].Content)
	assert.Equal(t, "tool_use", (*emitted)[1].Type)

	// And the tool event itself is forwarded untouched.
	require.NotNil(t, (*emitted)[1].Tool)
	assert.Equal(t, "t1", (*emitted)[1].Tool.ID)
}

// A barrier arriving with an empty buffer must not emit a spurious empty frame.
func TestCoalescer_BarrierWithEmptyBufferEmitsOnlyTheBarrier(t *testing.T) {
	c, emitted := newTestCoalescer()

	c.add(ai.StreamEvent{Type: "done"})

	require.Len(t, *emitted, 1)
	assert.Equal(t, "done", (*emitted)[0].Type)
}

// flush must be idempotent — the executor calls it from both the ticker and the
// exit defer, and an empty flush must not emit an empty content frame.
func TestCoalescer_FlushIsIdempotentAndEmptySafe(t *testing.T) {
	c, emitted := newTestCoalescer()

	c.flush() // empty
	assert.Empty(t, *emitted)
	assert.False(t, c.hasPending())

	c.add(content("once"))
	assert.True(t, c.hasPending())
	c.flush()
	c.flush() // second flush: nothing buffered

	require.Len(t, *emitted, 1)
	assert.Equal(t, "once", (*emitted)[0].Content)
	assert.False(t, c.hasPending())
}

// The size cap bounds a single frame. Without it a long burst (a big streamed
// code block with no intervening tool call) would merge into a multi-MB frame.
func TestCoalescer_SizeCapFlushesEarly(t *testing.T) {
	c, emitted := newTestCoalescer()

	chunk := strings.Repeat("x", 8*1024) // 8KB
	// The buffer fills to 4 chunks (32KB, exactly the cap). The 5th would push
	// it past the cap, so that add must flush the accumulated 32KB first.
	for range 5 {
		c.add(content(chunk))
	}

	require.Len(t, *emitted, 1, "exceeding the cap must flush before buffering more")
	assert.Len(t, (*emitted)[0].Content, 4*len(chunk))

	c.flush()

	total := 0
	for _, ev := range *emitted {
		total += len(ev.Content)
		assert.LessOrEqual(t, len(ev.Content), maxCoalescedBytes,
			"no single frame may exceed the cap")
	}
	assert.Equal(t, 5*len(chunk), total, "no bytes may be lost across the split")
}

// Exactly at the cap is still merged (the check is `>`); one byte past it
// flushes. Pinning this keeps the boundary from silently drifting.
func TestCoalescer_SizeCapBoundary(t *testing.T) {
	chunk := strings.Repeat("z", 8*1024)

	c, emitted := newTestCoalescer()
	// 4 x 8KB == 32768 == the cap exactly: merged into one frame.
	for range 4 {
		c.add(content(chunk))
	}
	assert.Empty(t, *emitted, "a frame exactly at the cap is still allowed")
	c.flush()
	require.Len(t, *emitted, 1)
	assert.Len(t, (*emitted)[0].Content, maxCoalescedBytes)

	c2, emitted2 := newTestCoalescer()
	// 32769 bytes: the 4th add must flush first.
	for range 3 {
		c2.add(content(chunk))
	}
	c2.add(content(chunk + "z"))
	require.Len(t, *emitted2, 1, "one byte past the cap must flush")
	c2.flush()
}

// A single delta larger than the cap must still be forwarded (as its own frame)
// rather than dropped — losing content is far worse than one oversized frame.
func TestCoalescer_OversizedSingleDeltaIsStillEmitted(t *testing.T) {
	c, emitted := newTestCoalescer()

	huge := strings.Repeat("y", maxCoalescedBytes+1024)
	c.add(content(huge))
	c.flush()

	require.Len(t, *emitted, 1)
	assert.Len(t, (*emitted)[0].Content, len(huge), "an oversized delta must not be truncated")
}

// Backends do emit empty-content deltas (keep-alives / no-op frames). An empty
// delta must not manufacture state: merging one into an existing buffer must
// leave the content unchanged, and flushing after only empty deltas must not
// emit a frame the frontend would have to append "" to.
func TestCoalescer_EmptyContentDeltasAreHarmless(t *testing.T) {
	c, emitted := newTestCoalescer()

	// Empty deltas before any content: nothing worth sending.
	c.add(content(""))
	c.add(content(""))
	assert.False(t, c.hasPending(), "empty deltas must not open a buffer")
	c.flush()
	assert.Empty(t, *emitted, "no frame should be emitted for empty-only deltas")

	// Empty delta interleaved with real content: merged, content unchanged.
	c.add(content("real"))
	c.add(content(""))
	c.add(content(" text"))
	c.flush()

	require.Len(t, *emitted, 1)
	assert.Equal(t, "real text", (*emitted)[0].Content)
}

// isCoalescableDelta must classify exactly the same types the coalescer
// buffers — otherwise either deltas would bypass coalescing or barrier
// events would re-order buffered text.
func TestIsCoalescableDelta_MatchesAddBehaviour(t *testing.T) {
	for _, typ := range []string{"content", "thinking"} {
		assert.True(t, isCoalescableDelta(ai.StreamEvent{Type: typ}), typ)
	}
	for _, typ := range []string{
		"tool_use", "tool_result", "done", "error", "cancelled",
		"content_reset", "steer_boundary", "stream_start", "metadata",
		"usage_update", "user_message", "queue_drain", "replay_done",
	} {
		assert.False(t, isCoalescableDelta(ai.StreamEvent{Type: typ}), typ)
	}
}
