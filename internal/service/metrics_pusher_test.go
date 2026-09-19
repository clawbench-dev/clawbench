package service

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/system"
	"clawbench/internal/ws"
)

// metricsTestRig wires a MetricsPusher to counting fakes so the loop can be
// driven without real sampling or a real WS manager.
type metricsTestRig struct {
	pusher *MetricsPusher

	mu          sync.Mutex
	sampleCalls int
	pushCalls   int

	// demand is read by the worker on every tick.
	demandCount    atomic.Int64
	demandInterval atomic.Int64

	sampleErr error
}

// testMetricsTick is deliberately much faster than the production 1s so the
// loop's rate logic is observable within a test's lifetime.
const testMetricsTick = 10 * time.Millisecond

func newMetricsTestRig() *metricsTestRig {
	rig := &metricsTestRig{}
	rig.pusher = &MetricsPusher{
		tick: testMetricsTick,
		sampleFn: func() (*system.ResourceResponse, error) {
			rig.mu.Lock()
			rig.sampleCalls++
			rig.mu.Unlock()
			if rig.sampleErr != nil {
				return nil, rig.sampleErr
			}
			return &system.ResourceResponse{}, nil
		},
		demandFn: func() (int, int) {
			return int(rig.demandCount.Load()), int(rig.demandInterval.Load())
		},
		pushFn: func(*system.ResourceResponse) {
			rig.mu.Lock()
			rig.pushCalls++
			rig.mu.Unlock()
		},
	}
	return rig
}

func (r *metricsTestRig) counts() (samples, pushes int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sampleCalls, r.pushCalls
}

func (r *metricsTestRig) setDemand(count, intervalMs int) {
	r.demandCount.Store(int64(count))
	r.demandInterval.Store(int64(intervalMs))
}

// TestMetricsPusher_PrimesSamplerAtStartup pins that the sampler is primed once
// before the loop: GetResources returns all-zero rates on its first call, and
// priming is what lets the frontend drop its "fetch twice" workaround.
func TestMetricsPusher_PrimesSamplerAtStartup(t *testing.T) {
	rig := newMetricsTestRig()
	// No demand at all — the only sample must be the startup prime.
	rig.pusher.Start()
	defer rig.pusher.Stop()

	time.Sleep(60 * time.Millisecond)

	samples, pushes := rig.counts()
	if samples != 1 {
		t.Fatalf("expected exactly 1 startup-prime sample, got %d", samples)
	}
	if pushes != 0 {
		t.Fatalf("expected no pushes without demand, got %d", pushes)
	}
}

// TestMetricsPusher_NoDemandNoSampling is the cost-control guarantee: with no
// declared watchers the worker must not touch the sampler.
func TestMetricsPusher_NoDemandNoSampling(t *testing.T) {
	rig := newMetricsTestRig()
	rig.pusher.Start()
	defer rig.pusher.Stop()

	time.Sleep(30 * time.Millisecond) // let the prime happen
	rig.mu.Lock()
	before := rig.sampleCalls
	rig.mu.Unlock()

	time.Sleep(100 * time.Millisecond)

	samples, pushes := rig.counts()
	if samples != before {
		t.Fatalf("sampler ran with no demand: %d -> %d", before, samples)
	}
	if pushes != 0 {
		t.Fatalf("expected no pushes without demand, got %d", pushes)
	}
}

// TestMetricsPusher_PushesAtRequestedRate verifies a fast declared rate is
// honored on a faster tick.
func TestMetricsPusher_PushesAtRequestedRate(t *testing.T) {
	rig := newMetricsTestRig()
	rig.setDemand(1, 20)
	rig.pusher.Start()
	defer rig.pusher.Stop()

	time.Sleep(150 * time.Millisecond)

	_, pushes := rig.counts()
	if pushes < 3 {
		t.Fatalf("expected several pushes at a 20ms rate over 150ms, got %d", pushes)
	}
}

// TestMetricsPusher_HonorsSlowerInterval verifies a slower declared rate is not
// over-served just because the tick is faster.
func TestMetricsPusher_HonorsSlowerInterval(t *testing.T) {
	rig := newMetricsTestRig()
	rig.setDemand(1, 100)
	rig.pusher.Start()
	defer rig.pusher.Stop()

	time.Sleep(250 * time.Millisecond)

	_, pushes := rig.counts()
	// ~2 pushes fit in 250ms at a 100ms interval; a broken implementation that
	// ignored the interval would emit ~25.
	if pushes < 1 || pushes > 4 {
		t.Fatalf("expected ~2 pushes at a 100ms rate over 250ms, got %d", pushes)
	}
}

// TestMetricsPusher_StopsWhenDemandDrops verifies sampling halts once the last
// watcher leaves.
func TestMetricsPusher_StopsWhenDemandDrops(t *testing.T) {
	rig := newMetricsTestRig()
	rig.setDemand(1, 10)
	rig.pusher.Start()
	defer rig.pusher.Stop()

	time.Sleep(60 * time.Millisecond)
	rig.setDemand(0, 0)
	time.Sleep(30 * time.Millisecond) // let the drop be observed

	rig.mu.Lock()
	before := rig.sampleCalls
	rig.mu.Unlock()

	time.Sleep(80 * time.Millisecond)

	if samples, _ := rig.counts(); samples != before {
		t.Fatalf("sampler kept running after demand dropped: %d -> %d", before, samples)
	}
}

// TestMetricsPusher_ResumesImmediatelyOnNewDemand verifies that after a period
// of no demand the first frame is pushed on the very next tick, rather than
// waiting out the previous interval.
func TestMetricsPusher_ResumesImmediatelyOnNewDemand(t *testing.T) {
	rig := newMetricsTestRig()
	rig.setDemand(1, 5000) // slow rate: one push, then nothing for a long time
	rig.pusher.Start()
	defer rig.pusher.Stop()

	time.Sleep(50 * time.Millisecond)
	rig.mu.Lock()
	pushesBefore := rig.pushCalls
	rig.mu.Unlock()
	if pushesBefore != 1 {
		t.Fatalf("expected exactly one push at the slow rate, got %d", pushesBefore)
	}

	// Drop demand (clears lastSampleAt), then demand again.
	rig.setDemand(0, 0)
	time.Sleep(40 * time.Millisecond)
	rig.setDemand(1, 5000)
	time.Sleep(50 * time.Millisecond)

	rig.mu.Lock()
	pushesAfter := rig.pushCalls
	rig.mu.Unlock()
	if pushesAfter != pushesBefore+1 {
		t.Fatalf("expected an immediate push on resumed demand, got %d -> %d", pushesBefore, pushesAfter)
	}
}

// TestMetricsPusher_SampleErrorIsNotFatal verifies a failing sample does not
// stop the loop.
func TestMetricsPusher_SampleErrorIsNotFatal(t *testing.T) {
	rig := newMetricsTestRig()
	rig.sampleErr = errors.New("all resource samplers failed")
	rig.setDemand(1, 10)
	rig.pusher.Start()
	defer rig.pusher.Stop()

	time.Sleep(100 * time.Millisecond)

	if samples, pushes := rig.counts(); samples < 3 {
		t.Fatalf("expected the loop to keep sampling after errors, got %d samples", samples)
	} else if pushes != 0 {
		t.Fatalf("expected no pushes while sampling fails, got %d", pushes)
	}
}

// TestMetricsPusher_StartStop_IdempotentAndRestartable mirrors the QueueReaper
// lifecycle tests: repeated Start/Stop must not panic, and a restart must work.
func TestMetricsPusher_StartStop_IdempotentAndRestartable(t *testing.T) {
	rig := newMetricsTestRig()
	rig.setDemand(1, 10)

	rig.pusher.Start()
	rig.pusher.Start() // idempotent
	time.Sleep(40 * time.Millisecond)
	rig.pusher.Stop()
	rig.pusher.Stop() // idempotent

	// Restart must not close an already-closed channel.
	rig.pusher.Start()
	time.Sleep(40 * time.Millisecond)
	rig.pusher.Stop()

	if _, pushes := rig.counts(); pushes == 0 {
		t.Fatal("expected pushes across the restart")
	}
}

// TestStopMetricsPusher_SafeWhenNeverStarted verifies the shutdown defer is safe
// on a process that never started the worker.
func TestStopMetricsPusher_SafeWhenNeverStarted(t *testing.T) {
	StopMetricsPusher() // must not panic
}

// TestStartStopMetricsPusher_GlobalSingleton verifies the global wrappers are
// idempotent and leave no instance behind.
func TestStartStopMetricsPusher_GlobalSingleton(t *testing.T) {
	StartMetricsPusher()
	first := globalMetricsPusher
	if first == nil {
		t.Fatal("expected a global pusher")
	}
	StartMetricsPusher() // idempotent: must not replace the instance
	if globalMetricsPusher != first {
		t.Fatal("StartMetricsPusher must not replace a running instance")
	}
	StopMetricsPusher()
	if globalMetricsPusher != nil {
		t.Fatal("StopMetricsPusher must clear the global")
	}
}

// TestNewMetricsPusher_DefaultWiring pins the production wiring: the defaults
// must be non-nil and must tolerate a nil WS manager (the worker can start
// before/after the manager exists).
func TestNewMetricsPusher_DefaultWiring(t *testing.T) {
	p := NewMetricsPusher()
	if p.tick != defaultMetricsPusherTick {
		t.Fatalf("expected the default tick %v, got %v", defaultMetricsPusherTick, p.tick)
	}
	if p.sampleFn == nil || p.demandFn == nil || p.pushFn == nil {
		t.Fatal("expected all production hooks to be wired")
	}

	// demandFn and pushFn must degrade gracefully with no manager installed.
	prev := ws.GetManager()
	ws.SetManagerForTest(nil)
	t.Cleanup(func() { ws.SetManagerForTest(prev) })

	if count, interval := p.demandFn(); count != 0 || interval != 0 {
		t.Fatalf("expected no demand without a manager, got (%d,%d)", count, interval)
	}
	p.pushFn(&system.ResourceResponse{}) // must not panic
}
