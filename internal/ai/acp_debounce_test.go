package ai

import (
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// helper: create a debouncer with a buffered channel for testing
func newTestDebouncer() (*toolCallDebouncer, <-chan StreamEvent) {
	ch := make(chan StreamEvent, 64)
	agent := &model.Agent{ID: "test-debounce-agent", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-debounce")
	return newToolCallDebouncer(ch, conn), ch
}

// helper: build a minimal SessionToolCallUpdate
func makeToolCallUpdate(toolCallID string, done bool) acp.SessionToolCallUpdate {
	status := acp.ToolCallStatusInProgress
	if done {
		status = acp.ToolCallStatusCompleted
	}
	return acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId(toolCallID),
		Status:     &status,
	}
}

// --- newToolCallDebouncer ---

func TestNewToolCallDebouncer_CreatesEmptyPending(t *testing.T) {
	d, _ := newTestDebouncer()
	assert.NotNil(t, d.pending)
	assert.Empty(t, d.pending)
}

// --- handleToolCallUpdate: first update starts timer ---

func TestHandleToolCallUpdate_FirstUpdateBuffersEvent(t *testing.T) {
	d, ch := newTestDebouncer()

	tcu := makeToolCallUpdate("tool-1", false)
	result := d.handleToolCallUpdate(tcu)

	assert.True(t, result, "first update should be buffered")

	// Pending map should have an entry
	d.mu.Lock()
	_, ok := d.pending["tool-1"]
	d.mu.Unlock()
	assert.True(t, ok, "pending map should contain tool-1")

	// No event forwarded yet (timer hasn't fired)
	select {
	case <-ch:
		t.Fatal("event should not be forwarded before timer fires")
	default:
		// expected
	}
}

// --- handleToolCallUpdate: callback invoked after delay ---

func TestHandleToolCallUpdate_EventForwardedAfterDelay(t *testing.T) {
	d, ch := newTestDebouncer()

	tcu := makeToolCallUpdate("tool-1", false)
	d.handleToolCallUpdate(tcu)

	// Wait for the debounce interval to pass
	select {
	case ev := <-ch:
		assert.Equal(t, "tool_use", ev.Type)
		assert.NotNil(t, ev.Tool)
		assert.Equal(t, "tool-1", ev.Tool.ID)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("event was not forwarded after debounce interval")
	}

	// Pending map should be cleared
	d.mu.Lock()
	_, ok := d.pending["tool-1"]
	d.mu.Unlock()
	assert.False(t, ok, "pending map should be cleared after flush")
}

// --- handleToolCallUpdate: callback NOT invoked before delay ---

func TestHandleToolCallUpdate_NotForwardedBeforeDelay(t *testing.T) {
	d, ch := newTestDebouncer()

	tcu := makeToolCallUpdate("tool-1", false)
	d.handleToolCallUpdate(tcu)

	// Check immediately — should not have fired yet
	select {
	case <-ch:
		t.Fatal("event should not be forwarded before delay")
	case <-time.After(10 * time.Millisecond):
		// expected: 10ms < 50ms debounce interval
	}
}

// --- handleToolCallUpdate: subsequent updates merge (timer reset not needed) ---

func TestHandleToolCallUpdate_SubsequentUpdateMerges(t *testing.T) {
	d, ch := newTestDebouncer()

	// First update
	tcu1 := makeToolCallUpdate("tool-1", false)
	d.handleToolCallUpdate(tcu1)

	// Second update for same tool ID — should merge, not create new timer
	tcu2 := makeToolCallUpdate("tool-1", false)
	result := d.handleToolCallUpdate(tcu2)
	assert.True(t, result, "subsequent update should be buffered")

	// Only one pending entry
	d.mu.Lock()
	pendingCount := len(d.pending)
	d.mu.Unlock()
	assert.Equal(t, 1, pendingCount, "should have exactly one pending entry")

	// When timer fires, we get exactly one event
	select {
	case <-ch:
		// got the flushed event
	case <-time.After(500 * time.Millisecond):
		t.Fatal("event was not forwarded after debounce interval")
	}

	// No more events
	select {
	case <-ch:
		t.Fatal("should only receive one event for merged updates")
	default:
		// expected
	}
}

// --- handleToolCallUpdate: terminal event (Done) flushes immediately ---

func TestHandleToolCallUpdate_DoneEventFlushesImmediately(t *testing.T) {
	d, ch := newTestDebouncer()

	// First, buffer a pending update
	tcu1 := makeToolCallUpdate("tool-1", false)
	d.handleToolCallUpdate(tcu1)

	// Now send a terminal (Done) event
	tcu2 := makeToolCallUpdate("tool-1", true)
	d.handleToolCallUpdate(tcu2)

	// Should receive the pending flushed event first, then the terminal event
	events := collectEvents(ch, 2, 500*time.Millisecond)
	require.Len(t, events, 2, "should receive flushed pending + terminal event")

	// First event is the flushed pending (from flushToolID)
	assert.Equal(t, "tool_use", events[0].Type)
	// Second event is the terminal (forwarded directly)
	assert.Equal(t, "tool_result", events[1].Type)
	assert.True(t, events[1].Tool.Done)
}

// --- handleToolCallUpdate: terminal event with no prior pending ---

func TestHandleToolCallUpdate_DoneEventNoPending(t *testing.T) {
	d, ch := newTestDebouncer()

	// Send a terminal event without any prior pending updates
	tcu := makeToolCallUpdate("tool-1", true)
	d.handleToolCallUpdate(tcu)

	// Should receive the terminal event directly (flushToolID is a no-op)
	select {
	case ev := <-ch:
		assert.Equal(t, "tool_result", ev.Type)
		assert.True(t, ev.Tool.Done)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("terminal event should be forwarded immediately")
	}
}

// --- handleToolCall: flushes pending for the same tool ID ---

func TestHandleToolCall_FlushesPending(t *testing.T) {
	d, ch := newTestDebouncer()

	// Buffer a pending update
	tcu := makeToolCallUpdate("tool-1", false)
	d.handleToolCallUpdate(tcu)

	// handleToolCall flushes the pending batch
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tool-1"),
	}
	d.handleToolCall(tc)

	// Should receive the flushed event
	select {
	case ev := <-ch:
		assert.Equal(t, "tool_use", ev.Type)
		assert.Equal(t, "tool-1", ev.Tool.ID)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("pending event should be flushed by handleToolCall")
	}

	// No more events
	assertNoEvents(t, ch)
}

func TestHandleToolCall_NoPendingIsNoOp(t *testing.T) {
	d, ch := newTestDebouncer()

	// handleToolCall with no pending entry should not panic or send events
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tool-999"),
	}
	assert.NotPanics(t, func() {
		d.handleToolCall(tc)
	})

	assertNoEvents(t, ch)
}

// --- flushToolID ---

func TestFlushToolID_SendsPendingEvent(t *testing.T) {
	d, ch := newTestDebouncer()

	tcu := makeToolCallUpdate("tool-1", false)
	d.handleToolCallUpdate(tcu)

	d.flushToolID("tool-1")

	select {
	case ev := <-ch:
		assert.Equal(t, "tool_use", ev.Type)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("flushToolID should forward pending event")
	}
}

func TestFlushToolID_NonExistentIsNoOp(t *testing.T) {
	d, ch := newTestDebouncer()

	assert.NotPanics(t, func() {
		d.flushToolID("nonexistent")
	})

	assertNoEvents(t, ch)
}

func TestFlushToolID_StopsTimer(t *testing.T) {
	d, ch := newTestDebouncer()

	tcu := makeToolCallUpdate("tool-1", false)
	d.handleToolCallUpdate(tcu)

	// Flush before timer fires — timer should be stopped
	d.flushToolID("tool-1")

	// Should receive exactly one event (from flush)
	select {
	case <-ch:
		// expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("should receive flushed event")
	}

	// Timer was stopped, so no duplicate event from the AfterFunc
	assertNoEvents(t, ch)
}

// --- flushAll ---

func TestFlushAll_SendsAllPendingEvents(t *testing.T) {
	d, ch := newTestDebouncer()

	// Buffer multiple pending updates
	d.handleToolCallUpdate(makeToolCallUpdate("tool-1", false))
	d.handleToolCallUpdate(makeToolCallUpdate("tool-2", false))
	d.handleToolCallUpdate(makeToolCallUpdate("tool-3", false))

	d.flushAll()

	events := collectEvents(ch, 3, 500*time.Millisecond)
	require.Len(t, events, 3, "flushAll should forward all pending events")

	ids := make(map[string]bool)
	for _, ev := range events {
		ids[ev.Tool.ID] = true
	}
	assert.True(t, ids["tool-1"])
	assert.True(t, ids["tool-2"])
	assert.True(t, ids["tool-3"])
}

func TestFlushAll_ClearsPendingMap(t *testing.T) {
	d, ch := newTestDebouncer()

	d.handleToolCallUpdate(makeToolCallUpdate("tool-1", false))
	d.handleToolCallUpdate(makeToolCallUpdate("tool-2", false))

	d.flushAll()

	d.mu.Lock()
	pendingCount := len(d.pending)
	d.mu.Unlock()
	assert.Zero(t, pendingCount, "pending map should be empty after flushAll")

	// Drain the channel
	collectEvents(ch, 2, 100*time.Millisecond)
}

func TestFlushAll_NoPendingIsNoOp(t *testing.T) {
	d, ch := newTestDebouncer()

	assert.NotPanics(t, func() {
		d.flushAll()
	})

	assertNoEvents(t, ch)
}

func TestFlushAll_StopsTimers(t *testing.T) {
	d, ch := newTestDebouncer()

	d.handleToolCallUpdate(makeToolCallUpdate("tool-1", false))
	d.handleToolCallUpdate(makeToolCallUpdate("tool-2", false))

	// flushAll should stop all timers, preventing duplicate events
	d.flushAll()

	events := collectEvents(ch, 2, 500*time.Millisecond)
	require.Len(t, events, 2, "should receive exactly 2 events from flushAll")

	// No duplicate events from AfterFunc timers
	assertNoEvents(t, ch)
}

// --- Multiple sequential debounce cycles ---

func TestHandleToolCallUpdate_MultipleSequentialCycles(t *testing.T) {
	d, ch := newTestDebouncer()

	// First cycle: tool-1
	d.handleToolCallUpdate(makeToolCallUpdate("tool-1", false))
	select {
	case ev := <-ch:
		assert.Equal(t, "tool-1", ev.Tool.ID)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first cycle event not received")
	}

	// Second cycle: tool-2
	d.handleToolCallUpdate(makeToolCallUpdate("tool-2", false))
	select {
	case ev := <-ch:
		assert.Equal(t, "tool-2", ev.Tool.ID)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second cycle event not received")
	}

	assertNoEvents(t, ch)
}

// --- Different tool IDs are independent ---

func TestHandleToolCallUpdate_DifferentToolIDsAreIndependent(t *testing.T) {
	d, ch := newTestDebouncer()

	d.handleToolCallUpdate(makeToolCallUpdate("tool-1", false))
	// Small delay to offset timers
	time.Sleep(10 * time.Millisecond)
	d.handleToolCallUpdate(makeToolCallUpdate("tool-2", false))

	// Both should eventually be forwarded
	events := collectEvents(ch, 2, 500*time.Millisecond)
	require.Len(t, events, 2)

	ids := make(map[string]bool)
	for _, ev := range events {
		ids[ev.Tool.ID] = true
	}
	assert.True(t, ids["tool-1"])
	assert.True(t, ids["tool-2"])
}

// --- RawInput merging ---

func TestHandleToolCallUpdate_RawInputMerging(t *testing.T) {
	d, ch := newTestDebouncer()

	// First update with RawInput
	tcu1 := makeToolCallUpdate("tool-1", false)
	tcu1.RawInput = map[string]any{"file": "a.txt"}
	d.handleToolCallUpdate(tcu1)

	// Second update with new RawInput — should overwrite
	tcu2 := makeToolCallUpdate("tool-1", false)
	tcu2.RawInput = map[string]any{"file": "b.txt"}
	d.handleToolCallUpdate(tcu2)

	// Verify the pending entry has the latest rawInput
	d.mu.Lock()
	pending, ok := d.pending["tool-1"]
	d.mu.Unlock()
	require.True(t, ok)
	require.NotNil(t, pending.rawInput)

	rawInput, ok := pending.rawInput.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "b.txt", rawInput["file"])

	// Drain the flushed event
	d.flushToolID("tool-1")
	collectEvents(ch, 1, 200*time.Millisecond)
}

func TestHandleToolCallUpdate_RawInputNilPreserved(t *testing.T) {
	d, ch := newTestDebouncer()

	// First update with RawInput
	tcu1 := makeToolCallUpdate("tool-1", false)
	tcu1.RawInput = map[string]any{"file": "a.txt"}
	d.handleToolCallUpdate(tcu1)

	// Second update without RawInput — should preserve existing
	tcu2 := makeToolCallUpdate("tool-1", false)
	tcu2.RawInput = nil
	d.handleToolCallUpdate(tcu2)

	d.mu.Lock()
	pending, ok := d.pending["tool-1"]
	d.mu.Unlock()
	require.True(t, ok)
	require.NotNil(t, pending.rawInput)

	rawInput, ok := pending.rawInput.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "a.txt", rawInput["file"])

	// Drain
	d.flushToolID("tool-1")
	collectEvents(ch, 1, 200*time.Millisecond)
}

// --- Concurrent safety ---

func TestHandleToolCallUpdate_ConcurrentSafety(t *testing.T) {
	d, ch := newTestDebouncer()

	// Fire many concurrent updates for different tool IDs
	done := make(chan struct{})
	for range 50 {
		go func() {
			tcu := makeToolCallUpdate("tool-concurrent", false)
			d.handleToolCallUpdate(tcu)
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines to finish
	for range 50 {
		<-done
	}

	// Should not panic, and pending map should be consistent
	d.mu.Lock()
	_, ok := d.pending["tool-concurrent"]
	d.mu.Unlock()
	assert.True(t, ok, "should have a pending entry for concurrent tool ID")

	// Flush and drain
	d.flushToolID("tool-concurrent")
	collectEvents(ch, 1, 500*time.Millisecond)
}

// --- Debounce interval constant ---

func TestDebounceInterval(t *testing.T) {
	assert.Equal(t, 50*time.Millisecond, toolDebounceInterval)
}

// --- helpers ---

func collectEvents(ch <-chan StreamEvent, count int, timeout time.Duration) []StreamEvent {
	var events []StreamEvent
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(events) < count {
		select {
		case ev := <-ch:
			events = append(events, ev)
		case <-timer.C:
			return events
		}
	}
	return events
}

func assertNoEvents(t *testing.T, ch <-chan StreamEvent) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("expected no events, but got one")
	case <-time.After(80 * time.Millisecond):
		// expected — slightly longer than debounce interval to be sure
	}
}
