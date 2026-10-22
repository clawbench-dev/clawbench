package service

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// The capsule must track usage as it lands, not only when a threshold is
// crossed: a completed turn changes `actual`, and waiting for the 5-minute
// worker (or for the user to open the popover) leaves a visibly stale number.
func TestBroadcastUsageMeterNumbers_OnlyOnChange(t *testing.T) {
	prev := usageMeterLastBroadcast
	t.Cleanup(func() { usageMeterLastBroadcast = prev })
	usageMeterLastBroadcast = struct {
		actual, expected, quota float64
		metric                  string
		valid                   bool
	}{}

	// With no manager wired the function must be a no-op rather than panic;
	// it still records the numbers so a later change is detected.
	ws.SetManagerForTest(nil)

	st := &UsageMeterStatus{
		Enabled: true, Metric: model.UsageMeterMetricCost,
		Actual: 9.5, Expected: 6.67, Quota: 20, DayInCycle: 5, PeriodDays: 30,
	}
	if !broadcastUsageMeterNumbers(st) {
		t.Fatal("the first reading did not broadcast")
	}
	if !usageMeterLastBroadcast.valid || usageMeterLastBroadcast.actual != 9.5 {
		t.Fatalf("recorded state = %+v, want actual 9.5", usageMeterLastBroadcast)
	}

	// Same numbers again: nothing to send. Without this guard every 5-minute
	// tick would push an identical frame to every connected client.
	if broadcastUsageMeterNumbers(st) {
		t.Fatal("an unchanged reading re-broadcast")
	}

	// A new reading must broadcast.
	st.Actual = 12.5
	if !broadcastUsageMeterNumbers(st) {
		t.Fatal("a changed reading did not broadcast")
	}
	if usageMeterLastBroadcast.actual != 12.5 {
		t.Fatalf("recorded actual = %v, want 12.5", usageMeterLastBroadcast.actual)
	}
}

// A disabled rule must not broadcast: with quota 0 the capsule is hidden, and
// shipping numbers for it would be noise.
func TestBroadcastUsageMeterNumbers_SkipsDisabled(t *testing.T) {
	prev := usageMeterLastBroadcast
	t.Cleanup(func() { usageMeterLastBroadcast = prev })
	usageMeterLastBroadcast = struct {
		actual, expected, quota float64
		metric                  string
		valid                   bool
	}{}

	if broadcastUsageMeterNumbers(&UsageMeterStatus{Enabled: false, Actual: 5}) {
		t.Fatal("a disabled meter must not broadcast")
	}
	if usageMeterLastBroadcast.valid {
		t.Fatal("a disabled meter must not broadcast")
	}
}
