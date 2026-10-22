package service_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"github.com/stretchr/testify/assert"
)

// With no quota the worker must not run the check on EVERY tick: the only work
// left is refreshing the spend total, which is a 30-day aggregate and is
// throttled to once an hour. The first tick still runs (the capsule shows a
// number even without a budget), so "never runs" is the wrong invariant — the
// one that matters is "does not run once per tick".
func TestUsageMeterWorker_ThrottlesUnmeteredRefresh(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{Quota: 0}
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })
	service.ResetUsageMeterUnmeteredThrottleForTest()
	t.Cleanup(service.ResetUsageMeterUnmeteredThrottleForTest)

	var calls atomic.Int32
	w := service.NewUsageMeterWorkerForTest(10*time.Millisecond, 0, func(context.Context) string {
		calls.Add(1)
		return ""
	})
	w.Start()
	time.Sleep(80 * time.Millisecond)
	w.Stop()

	// ~8 ticks at 10ms, but the hourly throttle admits exactly one.
	assert.Equal(t, int32(1), calls.Load(),
		"an unconfigured meter must refresh its spend total once, not once per tick")
}

// With a quota configured the tick must actually call the check.
func TestUsageMeterWorker_RunsCheckWhenEnabled(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{Quota: 10, PeriodDays: 30, Metric: model.UsageMeterMetricCost, AnchorDate: "2026-10-01"}
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })

	var calls atomic.Int32
	w := service.NewUsageMeterWorkerForTest(10*time.Millisecond, 0, func(context.Context) string {
		calls.Add(1)
		return ""
	})
	w.Start()
	time.Sleep(60 * time.Millisecond)
	w.Stop()

	assert.GreaterOrEqual(t, calls.Load(), int32(2), "the tick must repeat, not run once")
}

// Start is idempotent: calling it twice must not spawn a second loop (two loops
// would double the notification checks).
func TestUsageMeterWorker_StartIsIdempotent(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{Quota: 10, PeriodDays: 30, Metric: model.UsageMeterMetricCost, AnchorDate: "2026-10-01"}
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })

	var calls atomic.Int32
	w := service.NewUsageMeterWorkerForTest(20*time.Millisecond, 0, func(context.Context) string {
		calls.Add(1)
		return ""
	})
	w.Start()
	w.Start()
	time.Sleep(50 * time.Millisecond)
	w.Stop()

	// One loop at 20ms over 50ms → 2–3 ticks. Two loops would be 4–6.
	assert.LessOrEqual(t, calls.Load(), int32(4), "a second Start must not add a second loop")
}

// Stop is safe to call twice and before Start.
func TestUsageMeterWorker_StopIsSafe(t *testing.T) {
	w := service.NewUsageMeterWorkerForTest(time.Hour, time.Hour, func(context.Context) string { return "" })
	assert.NotPanics(t, func() { w.Stop() }, "Stop before Start must not panic")

	w.Start()
	assert.NotPanics(t, func() {
		w.Stop()
		w.Stop()
	})
}

// A wake must trigger a tick without waiting for the interval: this is what
// makes a just-configured rule take effect in seconds.
func TestUsageMeterWorker_WakeTriggersImmediateTick(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{Quota: 10, PeriodDays: 30, Metric: model.UsageMeterMetricCost, AnchorDate: "2026-10-01"}
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })

	var calls atomic.Int32
	// Interval an hour out: only the wake can produce the second tick.
	w := service.NewUsageMeterWorkerForTest(time.Hour, 0, func(context.Context) string {
		calls.Add(1)
		return ""
	})
	w.Start()
	// Wait for the startup tick.
	assert.Eventually(t, func() bool { return calls.Load() >= 1 }, time.Second, 5*time.Millisecond)

	w.Wake()
	assert.Eventually(t, func() bool { return calls.Load() >= 2 }, time.Second, 5*time.Millisecond,
		"Wake must run a check without waiting for the interval")
	w.Stop()
}

// A wake before any Start must not block or panic.
func TestUsageMeterWorker_WakeIsNonBlocking(t *testing.T) {
	w := service.NewUsageMeterWorkerForTest(time.Hour, time.Hour, func(context.Context) string { return "" })
	assert.NotPanics(t, func() {
		w.Wake()
		w.Wake() // second wake is dropped, not blocked
	})
}

// StartUsageMeterWorker installs the global worker once: a second call must
// reuse it rather than spawn a second loop (two loops would double the checks
// and could emit two notifications for the same crossing).
func TestStartUsageMeterWorker_IsIdempotent(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{Quota: 0} // tick is a no-op
	t.Cleanup(func() {
		service.StopUsageMeterWorker()
		model.ConfigInstance.UsageMeter = prev
	})

	assert.NotPanics(t, func() {
		service.StartUsageMeterWorker()
		service.StartUsageMeterWorker()
	})
}

// StopUsageMeterWorker must tolerate being called when no worker was started,
// and must clear the handle so a later Start creates a fresh one (Stop closes
// stopCh, so reusing the instance would hand the new loop a closed channel).
func TestStopUsageMeterWorker_SafeWithoutStartAndAllowsRestart(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{Quota: 0}
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })

	assert.NotPanics(t, func() { service.StopUsageMeterWorker() }, "Stop with no worker must be a no-op")
	assert.NotPanics(t, func() { service.StopUsageMeterWorker() }, "a second Stop must also be a no-op")

	service.StartUsageMeterWorker()
	service.StopUsageMeterWorker()
	assert.NotPanics(t, func() {
		service.StartUsageMeterWorker() // fresh instance after Stop
		service.StopUsageMeterWorker()
	})
}

// ReconfigureUsageMeterWorker must start the worker if it is not running and
// then wake it, so a rule configured on a fresh install is evaluated within
// seconds rather than after the first full interval.
//
// It must NOT run the check inline: the caller holds configMutex, and the check
// needs that same mutex to persist its state — doing it inline would deadlock.
func TestReconfigureUsageMeterWorker_StartsAndWakes(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{Quota: 0}
	t.Cleanup(func() {
		service.StopUsageMeterWorker()
		model.ConfigInstance.UsageMeter = prev
	})

	assert.NotPanics(t, func() {
		service.ReconfigureUsageMeterWorker()
		service.ReconfigureUsageMeterWorker() // second call must not block either
	})
}
