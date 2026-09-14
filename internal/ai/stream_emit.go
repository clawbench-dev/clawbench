package ai

import (
	"log/slog"
	"time"
)

// criticalStreamEvents are the event types whose loss is user-visible.
//
// Losing `done` leaves the UI streaming forever (the stop button spins and the
// input stays disabled until a `session_update` fallback or a manual refresh).
// Losing `stream_start` leaves content with no bubble to render into. Losing an
// error hides a failure. These must not be dropped just because the consumer is
// momentarily behind.
//
// High-frequency deltas (content/thinking/tool_use) are deliberately NOT here:
// dropping one delta is imperceptible, and blocking on them would let a slow
// consumer stall the agent's output entirely — trading a cosmetic loss for a
// real one.
var criticalStreamEvents = map[string]struct{}{
	"stream_start":  {},
	"done":          {},
	"cancelled":     {},
	"error":         {},
	"content_reset": {},
	"user_message":  {},
	"queue_drain":   {},
	"queue_cancel":  {},
	"stream_split":  {},
}

// IsCriticalStreamEvent reports whether losing this event type is user-visible.
func IsCriticalStreamEvent(eventType string) bool {
	_, ok := criticalStreamEvents[eventType]
	return ok
}

// criticalSendTimeout bounds how long a critical event waits for room in a full
// channel before giving up. It is a backstop, not a pacing mechanism: the normal
// case is that the consumer drains within microseconds.
//
// The bound is small on purpose. A blocking send on the WRONG goroutine is far
// worse than a lost event:
//
//   - ACP notifications are processed by a SINGLE goroutine shared by every
//     session on the connection (SDK processNotifications). Blocking it stalls
//     all of them.
//   - That goroutine's queue is bounded (1024) and its enqueue path is
//     non-blocking: overflow calls shutdownReceive and KILLS the connection.
//     At the ~450 events/s seen in the 09-11 incident, a 5s stall would enqueue
//     ~2250 events and take the agent down.
//
// So the wait must stay short enough that even a pathological stall cannot fill
// the SDK queue. notificationQueueSafetyBudget documents that constraint, and
// TestCriticalSendTimeout_FitsNotificationQueueBudget enforces it: at 10x the
// observed burst rate a 200ms wait queues 900 events, still under the 1024 cap.
var criticalSendTimeout = 200 * time.Millisecond

// notificationQueueSafetyBudget is the SDK's defaultMaxQueuedNotifications.
// criticalSendTimeout must be small enough that events produced during one wait
// cannot exceed it; see the comment above.
const notificationQueueSafetyBudget = 1024

// criticalEventBlockingSafe reports whether emitStreamEvent may block while
// delivering this event.
//
// Events that travel through the ACP notification path must NEVER block: that
// goroutine is shared across sessions and its queue kills the connection on
// overflow. The ACP notification path only ever emits deltas
// (content/thinking/thinking_done and session-state updates), so in practice no
// critical event is affected — but this guard keeps that true if someone later
// adds a critical emit there, rather than relying on it staying that way.
//
// Producers on a dedicated per-run goroutine (CLI parsers, the ACP prompt
// goroutine) may block: nothing else shares their queue.
func criticalEventBlockingSafe(source string) bool {
	return source != acpNotificationSource
}

// acpNotificationSource is the source token used by forwardACPEvent, which runs
// on the SDK's shared notification-processing goroutine.
const acpNotificationSource = "acp"

// emitStreamEvent sends a StreamEvent to the stream channel.
//
// Critical events (see criticalStreamEvents) wait for room when the channel is
// full, up to criticalSendTimeout. Everything else keeps the original
// non-blocking behaviour and is dropped on a full channel.
//
// The split matters because the two losses are not equivalent. Before it, a
// burst of thinking deltas could fill the 512-slot channel and cause the
// terminal `done` — the single event the UI needs to stop loading — to be
// dropped alongside them.
//
// source identifies the producer for log correlation (e.g. "claude", "codex",
// "cli"). It must be a short, stable lowercase token; parsers shared across
// backends (StreamParser/claude_tool) use "cli" since they run on CLI stream
// channels.
//
// A send to a closed channel panics; the recover makes it safe for producer
// goroutines that may outlive the channel close on cancellation (mirrors
// forwardACPEvent).
func emitStreamEvent(ch chan<- StreamEvent, source string, event StreamEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Debug(source+": send on closed stream channel, ignoring", "type", event.Type)
		}
	}()

	if IsCriticalStreamEvent(event.Type) && criticalEventBlockingSafe(source) {
		emitCriticalStreamEvent(ch, source, event)
		return
	}

	select {
	case ch <- event:
	default:
		slog.Warn(source+": stream channel full, dropping event",
			"type", event.Type,
			"source", source)
	}
}

// emitCriticalStreamEvent delivers an event that must not be silently lost.
// It tries a non-blocking send first (the overwhelmingly common case, no
// latency added), then waits for room up to criticalSendTimeout.
func emitCriticalStreamEvent(ch chan<- StreamEvent, source string, event StreamEvent) {
	select {
	case ch <- event:
		return
	default:
	}

	timer := time.NewTimer(criticalSendTimeout)
	defer timer.Stop()

	select {
	case ch <- event:
		slog.Warn(source+": stream channel full, critical event delivered after waiting",
			"type", event.Type, "source", source)
	case <-timer.C:
		// The consumer is not just behind, it is stuck. Dropping is still a
		// failure, but blocking here would hang the agent's output goroutine, so
		// surface it loudly and move on.
		slog.Error(source+": stream channel full, DROPPED critical event after timeout",
			"type", event.Type, "source", source, "timeout", criticalSendTimeout.String())
	}
}
