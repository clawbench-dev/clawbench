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

// The worker's tick must short-circuit when no quota is configured, so the
// default (feature off) install costs nothing but a config read per interval.
func TestUsageMeterWorker_SkipsCheckWhenDisabled(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{Quota: 0}
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })

	var calls atomic.Int32
	w := service.NewUsageMeterWorkerForTest(10*time.Millisecond, 0, func(context.Context) string {
		calls.Add(1)
		return ""
	})
	w.Start()
	time.Sleep(60 * time.Millisecond)
	w.Stop()

	assert.Zero(t, calls.Load(), "a disabled rule must not run the aggregate query")
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
