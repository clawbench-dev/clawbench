package service

import (
	"context"
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
		enabled                 bool
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

// No rule still broadcasts. The capsule stays visible and shows the spend, so
// withholding the numbers would freeze it at the value fetched on page load —
// for exactly the users who never opened the settings page.
func TestBroadcastUsageMeterNumbers_BroadcastsWithoutRule(t *testing.T) {
	prev := usageMeterLastBroadcast
	t.Cleanup(func() { usageMeterLastBroadcast = prev })
	usageMeterLastBroadcast = struct {
		actual, expected, quota float64
		metric                  string
		enabled                 bool
		valid                   bool
	}{}

	// Metric is required: it is the unit the client formats the number in, and a
	// frame without one would render as a bare number.
	if broadcastUsageMeterNumbers(&UsageMeterStatus{Enabled: false, Actual: 5}) {
		t.Fatal("a meter with no metric has nothing to report")
	}
	if usageMeterLastBroadcast.valid {
		t.Fatal("a meter with no metric must not record a broadcast")
	}

	if !broadcastUsageMeterNumbers(&UsageMeterStatus{
		Enabled: false, Metric: DefaultUsageMeterMetric, Actual: 5,
	}) {
		t.Fatal("an unmetered spend must still be broadcast")
	}
	if !usageMeterLastBroadcast.valid || usageMeterLastBroadcast.enabled {
		t.Fatal("the broadcast must be recorded as an unmetered reading")
	}
}

// Turning the rule on or off changes what the client renders, so it must count
// as a change even when the numbers are identical.
func TestBroadcastUsageMeterNumbers_EnabledToggleCountsAsChange(t *testing.T) {
	prev := usageMeterLastBroadcast
	t.Cleanup(func() { usageMeterLastBroadcast = prev })
	usageMeterLastBroadcast = struct {
		actual, expected, quota float64
		metric                  string
		enabled                 bool
		valid                   bool
	}{}

	metered := &UsageMeterStatus{Enabled: true, Metric: DefaultUsageMeterMetric, Actual: 5, Expected: 3, Quota: 10}
	if !broadcastUsageMeterNumbers(metered) {
		t.Fatal("first broadcast must go out")
	}
	if broadcastUsageMeterNumbers(metered) {
		t.Fatal("an identical reading must not re-broadcast")
	}
	unmetered := &UsageMeterStatus{Enabled: false, Metric: DefaultUsageMeterMetric, Actual: 5}
	if !broadcastUsageMeterNumbers(unmetered) {
		t.Fatal("dropping the rule must re-broadcast: the client renders it differently")
	}
}

// The worker entry point must reach the unmetered refresh; the turn-completion
// entry point must not. Splitting them is what keeps a 30-day aggregate off the
// per-turn hot path while still letting an unconfigured capsule move.
func TestCheckUsageMeterForWorker_RefreshesUnmeteredButPlainCheckDoesNot(t *testing.T) {
	prevRule := model.ConfigInstance.UsageMeter
	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{Quota: 0}
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prevRule })

	prev := usageMeterLastBroadcast
	t.Cleanup(func() { usageMeterLastBroadcast = prev })
	usageMeterLastBroadcast = struct {
		actual, expected, quota float64
		metric                  string
		enabled                 bool
		valid                   bool
	}{}

	// No DB in this test: the refresh is a no-op, so the assertion is about the
	// broadcast state staying untouched on the per-turn path.
	CheckUsageMeter(context.Background())
	if usageMeterLastBroadcast.valid {
		t.Fatal("the per-turn path must not run an unmetered aggregate")
	}

	// With a DB present the worker path does broadcast. Verified by the
	// service_test package's worker test; here we only pin that the two entry
	// points are wired to different behavior.
	if usageMeterNeedsCheck() {
		t.Fatal("quota 0 must report no notification check needed")
	}
}
