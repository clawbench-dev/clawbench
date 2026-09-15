package ws

import (
	"testing"
	"time"

	"clawbench/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Delivery stats exist because dropped events used to be invisible: an event
// emitted for a session with no subscriber vanished with no log and no counter,
// making "the UI is stuck" indistinguishable from "the backend never sent
// anything". These tests pin the counting and the critical/non-critical split.

func TestIsCriticalEvent(t *testing.T) {
	critical := []string{
		"stream_start", "done", "cancelled", "error", "content_reset",
		"user_message", "queue_drain", "queue_cancel", "stream_split",
	}
	for _, ev := range critical {
		assert.True(t, IsCriticalEvent(ev), "%s must be treated as critical", ev)
	}

	// High-frequency deltas are not critical: dropping one is imperceptible, and
	// treating them as critical would mean blocking the producer on every delta.
	for _, ev := range []string{"content", "thinking", "thinking_done", "tool_use", "tool_result", "metadata"} {
		assert.False(t, IsCriticalEvent(ev), "%s must not be treated as critical", ev)
	}
}

func TestEmitToSession_CountsDropWhenNoSubscribers(t *testing.T) {
	ResetDeliveryStatsForTest()
	t.Cleanup(ResetDeliveryStatsForTest)

	mgr := NewManagerForTest()
	SetManagerForTest(mgr)
	t.Cleanup(func() { SetManagerForTest(nil) })

	// No subscriber for this session — the event goes nowhere.
	EmitToSession("session-without-subscriber", ai.StreamEvent{Type: "done"})

	stats := GetDeliveryStats()
	assert.Equal(t, int64(1), stats.NoSubscribers,
		"an event emitted with no subscriber must be counted, not silently dropped")
	assert.Equal(t, int64(1), stats.Total)
}

func TestEmitToSession_NoCountWhenDelivered(t *testing.T) {
	ResetDeliveryStatsForTest()
	t.Cleanup(ResetDeliveryStatsForTest)

	mgr := NewManagerForTest()
	SetManagerForTest(mgr)
	t.Cleanup(func() { SetManagerForTest(nil) })

	const sessionID = "session-with-subscriber"
	mgr.StreamHub().Subscribe("client-1", sessionID)

	EmitToSession(sessionID, ai.StreamEvent{Type: "done"})

	assert.Equal(t, int64(0), GetDeliveryStats().NoSubscribers,
		"a delivered event must not be counted as dropped")
}

func TestEmitToSession_CountsDropWhenNoManager(t *testing.T) {
	ResetDeliveryStatsForTest()
	t.Cleanup(ResetDeliveryStatsForTest)

	SetManagerForTest(nil)

	EmitToSession("any-session", ai.StreamEvent{Type: "done"})

	stats := GetDeliveryStats()
	assert.Equal(t, int64(1), stats.NoManager)
	assert.Equal(t, int64(0), stats.NoSubscribers, "the reason must be distinguished")
	assert.Equal(t, int64(1), stats.Total)
}

func TestDeliveryStats_AccumulateAndTotal(t *testing.T) {
	ResetDeliveryStatsForTest()
	t.Cleanup(ResetDeliveryStatsForTest)

	mgr := NewManagerForTest()
	SetManagerForTest(mgr)
	t.Cleanup(func() { SetManagerForTest(nil) })

	for range 5 {
		EmitToSession("session-a", ai.StreamEvent{Type: "thinking"})
	}
	SetManagerForTest(nil)
	EmitToSession("session-b", ai.StreamEvent{Type: "done"})

	stats := GetDeliveryStats()
	assert.Equal(t, int64(5), stats.NoSubscribers)
	assert.Equal(t, int64(1), stats.NoManager)
	assert.Equal(t, int64(6), stats.Total, "Total must be the sum of the reasons")
}

// TestRecordDeliveryDrop_LogsOncePerType pins the rate-limiting: the 09-11
// incident produced ~9000 dropped deltas in 20 seconds, and one log line each
// would have buried every other entry. The counter carries the magnitude; the
// log carries the fact.
func TestRecordDeliveryDrop_LogsOncePerType(t *testing.T) {
	ResetDeliveryStatsForTest()
	t.Cleanup(ResetDeliveryStatsForTest)

	for range 100 {
		recordDeliveryDrop(DropReasonNoSubscribers, "session-burst", "thinking")
	}

	// All 100 are counted...
	assert.Equal(t, int64(100), GetDeliveryStats().NoSubscribers)

	// ...but the dedup map holds only one entry for this (reason, type).
	deliveryLoggedMu.Lock()
	_, logged := deliveryLoggedAt[DropReasonNoSubscribers+"|thinking"]
	count := len(deliveryLoggedAt)
	deliveryLoggedMu.Unlock()

	assert.True(t, logged)
	assert.Equal(t, 1, count, "each (reason, type) pair is logged once per window")
}

// TestRecordDeliveryDrop_LogsAgainAfterWindow verifies the rate limit is a
// window, not a one-shot. A permanent suppression would hide a later incident:
// the first drop must not silence the next one hours later.
func TestRecordDeliveryDrop_LogsAgainAfterWindow(t *testing.T) {
	ResetDeliveryStatsForTest()
	t.Cleanup(ResetDeliveryStatsForTest)

	orig := deliveryLogWindow
	deliveryLogWindow = time.Nanosecond
	t.Cleanup(func() { deliveryLogWindow = orig })

	key := DropReasonNoSubscribers + "|done"

	recordDeliveryDrop(DropReasonNoSubscribers, "s", "done")
	deliveryLoggedMu.Lock()
	first := deliveryLoggedAt[key]
	deliveryLoggedMu.Unlock()
	require.False(t, first.IsZero())

	// Past the (tiny) window, the same pair is logged again.
	time.Sleep(2 * time.Millisecond)
	recordDeliveryDrop(DropReasonNoSubscribers, "s", "done")
	deliveryLoggedMu.Lock()
	second := deliveryLoggedAt[key]
	deliveryLoggedMu.Unlock()

	assert.True(t, second.After(first), "a drop after the window must be logged again")
	assert.Equal(t, int64(2), GetDeliveryStats().NoSubscribers, "both drops are still counted")
}

func TestRecordDeliveryDrop_DistinctTypesLoggedSeparately(t *testing.T) {
	ResetDeliveryStatsForTest()
	t.Cleanup(ResetDeliveryStatsForTest)

	recordDeliveryDrop(DropReasonNoSubscribers, "s", "thinking")
	recordDeliveryDrop(DropReasonNoSubscribers, "s", "done")
	recordDeliveryDrop(DropReasonNoManager, "s", "thinking")

	deliveryLoggedMu.Lock()
	count := len(deliveryLoggedAt)
	deliveryLoggedMu.Unlock()

	assert.Equal(t, 3, count, "different reasons/types must each be logged")
}

// TestEmitToSession_CriticalDropIsObservable is the regression test for the
// motivating symptom: losing a terminal event must leave a trace.
func TestEmitToSession_CriticalDropIsObservable(t *testing.T) {
	ResetDeliveryStatsForTest()
	t.Cleanup(ResetDeliveryStatsForTest)

	mgr := NewManagerForTest()
	SetManagerForTest(mgr)
	t.Cleanup(func() { SetManagerForTest(nil) })

	// The session's subscription was lost (e.g. WS reconnect) while the run was
	// still producing events. The terminal event is dropped.
	EmitToSession("session-lost-subscription", ai.StreamEvent{Type: "done"})

	stats := GetDeliveryStats()
	require.Equal(t, int64(1), stats.NoSubscribers,
		"a dropped terminal event must be observable — this is what makes the stuck-UI case diagnosable")
}
