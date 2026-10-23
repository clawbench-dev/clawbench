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

// A disabled rule must not broadcast: with quota 0 the capsule is hidden, and
// A disabled frame MUST be broadcast: it is how a client learns the rule was
// turned off. Withholding it leaves the capsule showing the old quota until
// something else makes the client refetch — in practice, the user clicking it.
func TestBroadcastUsageMeterNumbers_BroadcastsDisabled(t *testing.T) {
	prev := usageMeterLastBroadcast
	t.Cleanup(func() { usageMeterLastBroadcast = prev })
	usageMeterLastBroadcast = struct {
		actual, expected, quota float64
		metric                  string
		enabled                 bool
		valid                   bool
	}{}

	if !broadcastUsageMeterNumbers(&UsageMeterStatus{Enabled: false}) {
		t.Fatal("turning the rule off must broadcast")
	}
	if !usageMeterLastBroadcast.valid || usageMeterLastBroadcast.enabled {
		t.Fatal("the broadcast must be recorded as a disabled reading")
	}
	// Idempotent: a second identical disabled frame sends nothing, so the
	// periodic tick cannot spam clients.
	if broadcastUsageMeterNumbers(&UsageMeterStatus{Enabled: false}) {
		t.Fatal("a repeated disabled frame must not re-broadcast")
	}
}

// Turning the rule off must re-broadcast even when every number is unchanged:
// `enabled` is part of the change key because it changes what the client renders.
func TestBroadcastUsageMeterNumbers_EnabledToggleIsAChange(t *testing.T) {
	prev := usageMeterLastBroadcast
	t.Cleanup(func() { usageMeterLastBroadcast = prev })
	usageMeterLastBroadcast = struct {
		actual, expected, quota float64
		metric                  string
		enabled                 bool
		valid                   bool
	}{}

	metered := &UsageMeterStatus{Enabled: true, Metric: "cost", Actual: 5, Expected: 3, Quota: 10}
	if !broadcastUsageMeterNumbers(metered) {
		t.Fatal("first broadcast must go out")
	}
	if broadcastUsageMeterNumbers(metered) {
		t.Fatal("an identical metered reading must not re-broadcast")
	}
	if !broadcastUsageMeterNumbers(&UsageMeterStatus{Enabled: false}) {
		t.Fatal("dropping the rule must re-broadcast")
	}
}

// End-to-end for the reported bug: turning the quota to 0 must reach connected
// clients without waiting for a client-side action.
//
// The chain is CheckUsageMeter -> broadcastDisabledUsageMeter -> WS frame. This
// pins the whole thing because each link was independently capable of swallowing
// it: the check used to return early on quota 0, the broadcast used to reject
// !Enabled, and the worker tick used to skip the call entirely.
func TestCheckUsageMeter_BroadcastsWhenRuleTurnedOff(t *testing.T) {
	prevRule := model.ConfigInstance.UsageMeter
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prevRule })

	prevBroadcast := usageMeterLastBroadcast
	t.Cleanup(func() { usageMeterLastBroadcast = prevBroadcast })

	// Start from a metered state, as a user who had a rule would be.
	usageMeterLastBroadcast = struct {
		actual, expected, quota float64
		metric                  string
		enabled                 bool
		valid                   bool
	}{9.5, 6.67, 20, model.UsageMeterMetricCost, true, true}

	ws.SetManagerForTest(nil)

	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{Quota: 0}
	CheckUsageMeter(context.Background())

	if !usageMeterLastBroadcast.valid {
		t.Fatal("the disabled state must be recorded")
	}
	if usageMeterLastBroadcast.enabled {
		t.Fatal("turning the rule off must be broadcast as a disabled reading; " +
			"otherwise the client keeps showing the old quota")
	}
}
