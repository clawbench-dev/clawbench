package ws

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// delivery_stats.go tracks events that were produced but not delivered.
//
// Delivery used to be entirely silent: EmitToSession returned early when a
// session had no subscribers, and nothing recorded it. A dropped terminal event
// therefore looked identical to a backend that never produced one, which made
// "the UI is stuck / the reply never appeared" impossible to diagnose from
// server logs.
//
// These counters make the loss visible and quantifiable. They are deliberately
// cheap (atomics, no locks) because they sit on the hot streaming path.

// Delivery drop reasons.
const (
	// DropReasonNoSubscribers — the event was emitted for a session that had no
	// live subscriber. The most diagnostically important case: it usually means
	// the client's subscription was lost (WS reconnect) while the UI still
	// believes it is subscribed.
	DropReasonNoSubscribers = "no_subscribers"
	// DropReasonNoManager — the WS manager is not initialized (early startup or
	// shutdown). Rare and expected in those windows.
	DropReasonNoManager = "no_manager"
)

// criticalEventTypes are the events the UI needs in order to leave its streaming
// state or to create the assistant bubble at all. Losing one of these is a
// user-visible defect, so they are logged at WARN; high-frequency deltas are
// logged at DEBUG so a burst cannot flood the log.
var criticalEventTypes = map[string]struct{}{
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

// IsCriticalEvent reports whether losing this event type is user-visible.
func IsCriticalEvent(eventType string) bool {
	_, ok := criticalEventTypes[eventType]
	return ok
}

// deliveryStats holds per-reason drop counters.
type deliveryStats struct {
	noSubscribers atomic.Int64
	noManager     atomic.Int64
}

var globalDeliveryStats deliveryStats

// deliveryLogState rate-limits the per-event-type log lines so a long burst of
// dropped deltas cannot flood the log.
//
// The rate limit is per (reason, type) per WINDOW, not forever. A permanent
// one-shot would hide the second incident: the whole point is to notice a stuck
// UI, and the first drop at 09:00 must not silence the one at 17:00. Within a
// window only the first occurrence is logged; the counter carries the magnitude
// for the rest.
var (
	deliveryLoggedMu  sync.Mutex
	deliveryLoggedAt  = map[string]time.Time{}
	deliveryLogWindow = 5 * time.Minute
)

// recordDeliveryDrop counts a dropped event and logs it once per (reason, type)
// per deliveryLogWindow.
func recordDeliveryDrop(reason, sessionID, eventType string) {
	switch reason {
	case DropReasonNoSubscribers:
		globalDeliveryStats.noSubscribers.Add(1)
	case DropReasonNoManager:
		globalDeliveryStats.noManager.Add(1)
	}

	key := reason + "|" + eventType
	now := time.Now()

	deliveryLoggedMu.Lock()
	last, seen := deliveryLoggedAt[key]
	suppressed := seen && now.Sub(last) < deliveryLogWindow
	if !suppressed {
		deliveryLoggedAt[key] = now
	}
	deliveryLoggedMu.Unlock()

	if suppressed {
		return
	}

	attrs := []any{
		slog.String("reason", reason),
		slog.String("session", sessionID),
		slog.String("event_type", eventType),
	}
	if IsCriticalEvent(eventType) {
		slog.Warn("ws: dropped critical stream event (see /api/ws/delivery-stats for totals)", attrs...)
	} else {
		slog.Debug("ws: dropped stream event", attrs...)
	}
}

// DeliveryStats is a snapshot of dropped-event counters.
type DeliveryStats struct {
	// NoSubscribers counts events emitted for a session with no live subscriber.
	NoSubscribers int64 `json:"no_subscribers"`
	// NoManager counts events dropped because the WS manager was unavailable.
	NoManager int64 `json:"no_manager"`
	// Total is the sum, for a quick "is anything being lost" check.
	Total int64 `json:"total"`
}

// GetDeliveryStats returns a snapshot of the drop counters.
func GetDeliveryStats() DeliveryStats {
	noSubs := globalDeliveryStats.noSubscribers.Load()
	noMgr := globalDeliveryStats.noManager.Load()
	return DeliveryStats{
		NoSubscribers: noSubs,
		NoManager:     noMgr,
		Total:         noSubs + noMgr,
	}
}

// ResetDeliveryStatsForTest clears the counters and the log-dedup state.
// Production code must not call this.
func ResetDeliveryStatsForTest() {
	globalDeliveryStats.noSubscribers.Store(0)
	globalDeliveryStats.noManager.Store(0)
	deliveryLoggedMu.Lock()
	deliveryLoggedAt = map[string]time.Time{}
	deliveryLoggedMu.Unlock()
}
