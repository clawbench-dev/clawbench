package service

import "clawbench/internal/ai"

// streamCoalescer merges consecutive content/thinking deltas into a single
// StreamEvent before they are fanned out over WebSocket.
//
// Why this exists: a 33KB assistant reply arrives from the backend as ~3000
// separate delta events (measured 2026-09-20: 3062 WS frames in 17s, median
// 180-byte payload, one every 5.6ms). Every one of those became its own JSON
// marshal, WS frame header, syscall, and frontend JSON.parse. The frontend
// already throttles rendering to one frame per 300ms, so the extra frames
// bought nothing — they were pure overhead.
//
// Merging them cuts frames ~9x at a 50ms window while leaving the rendered
// result identical: the frontend appends each payload's text to the same block
// (`existingText.text += action.text`), so N deltas and 1 concatenated delta
// produce the same block.
//
// Only ONE event is buffered at a time, because the merge semantics are
// "append to the same block's text" — N deltas collapse to a single event, not
// a list. That keeps this free of queues and maps, and makes the same-parent
// requirement fall out naturally: a delta for a different block flushes the
// buffered one and starts a new buffer.
//
// Not goroutine-safe by design: it is driven exclusively by the executor's
// single event-loop goroutine (RunWithChannel). See SessionExecutor for the
// call-site analysis. No locks, no timers, no IO here — the flush timer lives
// in the executor so this stays independently testable.
type streamCoalescer struct {
	// pending is the buffered delta, or nil when empty. Content accumulates
	// into it in place.
	pending *ai.StreamEvent
	// pendingBytes is the accumulated len(Content) of pending, used to bound a
	// single frame. The payload is a JSON string that has to survive both a WS
	// frame and a frontend JSON.parse, so an unbounded merge would eventually
	// produce a multi-megabyte frame — worse than the many small ones it
	// replaced.
	pendingBytes int
	// emit forwards one event downstream (the real WS fan-out). Injected so the
	// coalescer itself has no dependency on executor state.
	emit func(ai.StreamEvent)
}

// maxCoalescedBytes caps a single merged frame. A long delta burst (a large
// code block streamed without intervening tool calls) would otherwise merge
// indefinitely; 32KB keeps frames comparable to the frontend's own 300ms
// rendering window while still collapsing the common case.
const maxCoalescedBytes = 32 * 1024

// coalescableDeltaTypes are the event types whose payload is appended to an
// existing block, and which therefore may be merged. Every other type is an
// ordering barrier: it must not overtake buffered text.
var coalescableDeltaTypes = map[string]struct{}{
	"content":  {},
	"thinking": {},
}

// isCoalescableDelta reports whether an event may be buffered and merged.
//
// Exported to the executor so the flush-before-dispatch gate can ask the same
// question the coalescer asks. If these two ever disagreed, either deltas would
// be flushed one-per-event (coalescing silently disabled) or a barrier event
// would overtake buffered text (rendered out of order).
func isCoalescableDelta(event ai.StreamEvent) bool {
	_, ok := coalescableDeltaTypes[event.Type]
	return ok
}

// canMerge reports whether next can be appended to the buffered event.
//
// Type AND parent must both match. The parent check is a correctness
// requirement, not an optimization: the frontend locates the target block with
// findBlockByTypeBackward(blocks, 'text', parent), so merging a sub-agent's
// delta into a top-level block would attribute a child's text to its parent.
func canMerge(pending, next *ai.StreamEvent) bool {
	return pending.Type == next.Type &&
		pending.ParentToolCallID == next.ParentToolCallID
}

// add feeds one event through the coalescer.
//
// A coalescable delta is buffered (or appended to the current buffer); anything
// else flushes first and is then emitted immediately, so it can never overtake
// buffered text.
func (c *streamCoalescer) add(event ai.StreamEvent) {
	if !isCoalescableDelta(event) {
		c.flush()
		c.emit(event)
		return
	}

	// Empty deltas carry no content. Most backends filter these at the source
	// (e.g. acp_events.go guards `cb.Text.Text != ""`), but the 12+ parsers do
	// not all agree, and an empty delta would otherwise open a buffer that
	// delays the next real delta by up to a full window — and could flush an
	// empty frame if the following delta belongs to a different block.
	if event.Content == "" {
		return
	}

	if c.pending == nil {
		// Copy: the caller may reuse the value, and we hold a pointer to it.
		pending := event
		c.pending = &pending
		c.pendingBytes = len(event.Content)
		return
	}

	// A different type or a different parent block starts a new buffer — the
	// buffered delta belongs to another block and must go out before this one.
	if !canMerge(c.pending, &event) {
		c.flush()
		pending := event
		c.pending = &pending
		c.pendingBytes = len(event.Content)
		return
	}

	// Bound the frame size before appending, so a single delta larger than the
	// cap still goes out (as its own frame) rather than being dropped.
	if c.pendingBytes+len(event.Content) > maxCoalescedBytes {
		c.flush()
		pending := event
		c.pending = &pending
		c.pendingBytes = len(event.Content)
		return
	}

	// Safe: pending was copied on first buffer, so += does not alias caller's memory.
	c.pending.Content += event.Content
	c.pendingBytes += len(event.Content)
}

// flush emits the buffered delta, if any. Safe to call when empty.
func (c *streamCoalescer) flush() {
	if c.pending == nil {
		return
	}
	event := *c.pending
	c.pending = nil
	c.pendingBytes = 0
	c.emit(event)
}

// hasPending reports whether a delta is currently buffered. Used by tests and
// by the ticker to skip an empty flush.
func (c *streamCoalescer) hasPending() bool {
	return c.pending != nil
}
