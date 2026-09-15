package ai

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emitStreamEvent splits event delivery by importance: critical events wait for
// room in a full channel, high-frequency deltas are dropped. These tests pin
// both halves of that split, and the guard that keeps the blocking behaviour off
// the shared ACP notification goroutine.

// TestEmitStreamEvent_SendsWhenBufferAvailable verifies a normal (non-full)
// channel accepts the event.
func TestEmitStreamEvent_SendsWhenBufferAvailable(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	emitStreamEvent(ch, "test-src", StreamEvent{Type: "content", Content: "hello"})

	select {
	case ev := <-ch:
		assert.Equal(t, "content", ev.Type)
		assert.Equal(t, "hello", ev.Content)
	case <-time.After(time.Second):
		t.Fatal("event was not delivered")
	}
}

// TestEmitStreamEvent_DropsAndWarnsOnFullChannel verifies that a full channel
// does NOT block the caller for a high-frequency delta, and logs a WARN naming
// the event type + source.
//
// Deltas keep the original non-blocking behaviour on purpose: dropping one is
// imperceptible, and blocking on them would let a slow consumer stall the
// agent's output entirely.
func TestEmitStreamEvent_DropsAndWarnsOnFullChannel(t *testing.T) {
	// Capture slog output.
	var buf bytes.Buffer
	origHandler := slog.Default().Handler()
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)
	defer slog.SetDefault(slog.New(origHandler))

	// Fill the channel completely.
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "content", Content: "occupied"}

	// Must return immediately (non-blocking) despite the full buffer.
	done := make(chan struct{})
	go func() {
		emitStreamEvent(ch, "test-src", StreamEvent{Type: "thinking", Content: "dropped"})
		close(done)
	}()

	select {
	case <-done:
		// Non-blocking: returned without waiting for space.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("emitStreamEvent blocked on a full channel for a non-critical event — must be non-blocking")
	}

	// The original event is untouched; the dropped event never arrived.
	select {
	case ev := <-ch:
		assert.Equal(t, "occupied", ev.Content, "only the pre-existing event should be present")
	default:
		t.Fatal("pre-existing event missing")
	}

	// WARN must be logged with type + source.
	assert.Contains(t, buf.String(), "stream channel full, dropping event")
	assert.Contains(t, buf.String(), "thinking")
	assert.Contains(t, buf.String(), "test-src")
}

// TestEmitStreamEvent_NoPanicOnClosedChannel verifies that sending to an
// already-closed channel is recovered (no panic), mirroring forwardACPEvent's
// safety for producer goroutines that outlive the channel close.
func TestEmitStreamEvent_NoPanicOnClosedChannel(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	close(ch)

	assert.NotPanics(t, func() {
		emitStreamEvent(ch, "test-src", StreamEvent{Type: "done"})
	})
}

// TestForwardACPEvent_ReusesEmitStreamEvent verifies forwardACPEvent keeps its
// non-blocking, no-panic behaviour. It runs on the SDK's shared notification
// goroutine, whose bounded queue kills the connection on overflow — so it must
// never block, not even for a critical event type.
func TestForwardACPEvent_ReusesEmitStreamEvent(t *testing.T) {
	// Full channel → drops, no block, no panic.
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "content", Content: "x"}
	assert.NotPanics(t, func() {
		forwardACPEvent(ch, StreamEvent{Type: "tool_use"})
	})
	require.Len(t, ch, 1, "forwardACPEvent must not overwrite the buffered event")

	// A critical event type on the acp source must also not block, because
	// blocking the shared notification goroutine can overflow the SDK's
	// notification queue and shut the connection down.
	assert.NotPanics(t, func() {
		forwardACPEvent(ch, StreamEvent{Type: "done"})
	})
	require.Len(t, ch, 1, "the acp source must not block even for a critical event")

	// Closed channel → no panic.
	close(ch)
	assert.NotPanics(t, func() {
		forwardACPEvent(ch, StreamEvent{Type: "done"})
	})
}

func TestIsCriticalStreamEvent(t *testing.T) {
	for _, ev := range []string{
		"stream_start", "done", "cancelled", "error", "content_reset",
		"user_message", "queue_drain", "queue_cancel", "stream_split",
	} {
		assert.True(t, IsCriticalStreamEvent(ev), "%s must be critical", ev)
	}
	for _, ev := range []string{"content", "thinking", "thinking_done", "tool_use", "tool_result", "metadata"} {
		assert.False(t, IsCriticalStreamEvent(ev), "%s must not be critical", ev)
	}
}

// TestEmitStreamEvent_DropsDeltaOnFullChannel verifies the high-frequency path
// keeps its original non-blocking behaviour: a full channel must not stall the
// producer for a delta the user cannot perceive.
func TestEmitStreamEvent_DropsDeltaOnFullChannel(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "content", Content: "fills the buffer"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		emitStreamEvent(ch, "cli", StreamEvent{Type: "thinking", Content: "dropped"})
	}()

	select {
	case <-done:
		// Returned immediately — correct.
	case <-time.After(time.Second):
		t.Fatal("a non-critical event must not block on a full channel")
	}
}

// TestEmitStreamEvent_CriticalWaitsForRoom is the core Stage B behaviour: a
// terminal event must not be lost just because the consumer is momentarily
// behind. Before this, a burst of deltas could fill the channel and the `done`
// event — the one the UI needs to stop loading — was dropped with them.
func TestEmitStreamEvent_CriticalWaitsForRoom(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "thinking", Content: "fills the buffer"}

	delivered := make(chan struct{})
	go func() {
		emitStreamEvent(ch, "cli", StreamEvent{Type: "done"})
		close(delivered)
	}()

	// The send must be waiting, not dropped.
	select {
	case <-delivered:
		t.Fatal("the critical event should still be waiting for room")
	case <-time.After(50 * time.Millisecond):
	}

	// Make room: the critical event must now land.
	<-ch
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("the critical event must be delivered once room appears")
	}

	got := <-ch
	assert.Equal(t, "done", got.Type, "the terminal event must reach the consumer")
}

// TestEmitStreamEvent_CriticalDropsAfterTimeout verifies the bounded wait: a
// genuinely stuck consumer must not hang the producer goroutine forever. The
// wait has to stay short so it cannot fill the SDK's notification queue.
func TestEmitStreamEvent_CriticalDropsAfterTimeout(t *testing.T) {
	orig := criticalSendTimeout
	criticalSendTimeout = 50 * time.Millisecond
	t.Cleanup(func() { criticalSendTimeout = orig })

	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "thinking", Content: "never drained"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		emitStreamEvent(ch, "cli", StreamEvent{Type: "done"})
	}()

	select {
	case <-done:
		// Gave up after the timeout — correct, the producer must not hang.
	case <-time.After(2 * time.Second):
		t.Fatal("a critical event must not block indefinitely on a stuck consumer")
	}
}

// TestEmitStreamEvent_ACPNotificationSourceNeverBlocks is the safety guard.
//
// ACP events are delivered from the SDK's single, shared notification goroutine,
// whose queue (1024) kills the connection on overflow. Blocking there would turn
// "one lost delta" into "the agent connection dies", so that source must always
// use the non-blocking path — even for a critical event type.
func TestEmitStreamEvent_ACPNotificationSourceNeverBlocks(t *testing.T) {
	assert.False(t, criticalEventBlockingSafe(acpNotificationSource),
		"the shared ACP notification goroutine must never block")
	assert.True(t, criticalEventBlockingSafe("cli"),
		"a CLI producer on its own goroutine may block")

	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "content", Content: "full"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		// "done" is critical, but the acp source must still not block.
		emitStreamEvent(ch, acpNotificationSource, StreamEvent{Type: "done"})
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the acp notification source must not block even for a critical event")
	}
}

// TestCriticalSendTimeout_FitsNotificationQueueBudget documents the constraint
// the timeout exists to satisfy: a wait long enough to overflow the SDK's
// notification queue during a burst would kill the connection.
func TestCriticalSendTimeout_FitsNotificationQueueBudget(t *testing.T) {
	// ~450 events/s was the observed burst rate in the 09-11 incident. Even at a
	// generous 10x that rate, a wait must not be able to fill the queue.
	const observedBurstPerSecond = 450
	const safetyFactor = 10
	worstCaseEvents := int(criticalSendTimeout.Seconds() * observedBurstPerSecond * safetyFactor)

	assert.Less(t, worstCaseEvents, notificationQueueSafetyBudget,
		"criticalSendTimeout is long enough that a burst could overflow the SDK notification queue and kill the connection")
}
