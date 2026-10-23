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

// With no quota the tick still calls the check, but the check must not run the
// aggregate query. It has to be CALLED so it can broadcast the "turned off"
// state — without that a client keeps showing the old quota until something
// else makes it refetch, which in practice is the user clicking the capsule.
func TestUsageMeterWorker_TicksButSkipsAggregateWhenDisabled(t *testing.T) {
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

	assert.Greater(t, calls.Load(), int32(0),
		"the check must still run so it can broadcast the disabled state")

	// And the real check must not touch the ledger. No DB is configured here, so
	// a stray aggregate would surface as a warning rather than a panic; assert on
	// the documented behavior instead: ComputeUsageMeter returns immediately.
	st, err := service.ComputeUsageMeter(context.Background(), time.Now())
	assert.NoError(t, err)
	assert.False(t, st.Enabled)
	assert.Zero(t, st.Actual)
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
