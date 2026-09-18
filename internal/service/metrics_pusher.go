package service

import (
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/system"
	"clawbench/internal/ws"
)

// MetricsPusher samples system resources and pushes them over the WS channel,
// but only while at least one connected client has declared interest, and at
// the fastest rate any of them asked for.
//
// Why a worker instead of client polling: `/api/system/resources` was the
// highest-volume endpoint in the server (12 requests/minute per open tab,
// every 5s, forever). The client cannot be trusted to poll at exactly the rate
// it needs, and a disconnected tab cannot tell the server to stop. Gating on
// declared demand makes the cost proportional to actual viewers.
//
// The push deliberately bypasses the reconnect replay buffer (see
// ws.BroadcastToMetricsWatchers): at 1Hz a buffered telemetry stream would
// evict real chat/task events from the 50-entry buffer within a minute.
type MetricsPusher struct {
	// tick is the sampling granularity. A fixed 1s tick is enough for every
	// rate we allow (the fastest is 1s), so a rate change never needs to
	// recreate the ticker — a skipped tick costs one select plus a map scan
	// over at most maxSubscriptions entries.
	tick time.Duration

	stopCh chan struct{}
	doneCh chan struct{}

	mu      sync.Mutex
	running bool

	// stopOnce guards close(stopCh): Stop is safe to call from more than one
	// goroutine (shutdown paths can race), and an unguarded close would panic.
	stopOnce sync.Once

	// lastSampleAt is the time of the last emitted sample, used to honor a
	// slower requested interval on a faster tick. Zeroed when demand drops so
	// a returning client gets a frame on the very next tick.
	lastSampleAt time.Time

	// Indirections so tests can drive the worker without real sampling,
	// a real WS manager, or real sockets.
	sampleFn func() (*system.ResourceResponse, error)
	demandFn func() (count int, minIntervalMs int)
	pushFn   func(*system.ResourceResponse)
}

// defaultMetricsPusherTick is the sampling granularity. 1s matches the fastest
// client rate, and GetResources has a 500ms cache so a faster tick would only
// re-read cached values.
const defaultMetricsPusherTick = time.Second

// globalMetricsPusher is the running instance, protected by metricsPusherMu.
var (
	globalMetricsPusher *MetricsPusher
	metricsPusherMu     sync.Mutex
)

// NewMetricsPusher creates a pusher with the default tick and the production
// sampling / demand / push wiring.
func NewMetricsPusher() *MetricsPusher {
	return &MetricsPusher{
		tick:     defaultMetricsPusherTick,
		sampleFn: system.GetResources,
		demandFn: func() (int, int) {
			mgr := ws.GetManager()
			if mgr == nil {
				return 0, 0
			}
			return mgr.MetricsDemand()
		},
		pushFn: func(resp *system.ResourceResponse) {
			mgr := ws.GetManager()
			if mgr == nil {
				return
			}
			mgr.BroadcastToMetricsWatchers(ws.ServerMessage{
				Type:  ws.MessageTypeEvent,
				Event: "system_resources",
				Data:  resp,
			})
		},
	}
}

// Start begins the push loop in a goroutine.
//
// Reusable after Stop: fresh channels are created per run, so a restart cannot
// close an already-closed channel.
func (w *MetricsPusher) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.stopCh = make(chan struct{})
	w.doneCh = make(chan struct{})
	w.stopOnce = sync.Once{}
	w.running = true
	stopCh, doneCh := w.stopCh, w.doneCh
	w.mu.Unlock()

	go w.run(stopCh, doneCh)
	slog.Info("metrics pusher started", slog.Duration("tick", w.tick))
}

// Stop signals the pusher to stop and waits for it to finish.
//
// Safe to call concurrently and repeatedly. Closing stopCh is guarded by
// stopOnce so a second caller cannot close an already-closed channel and panic.
func (w *MetricsPusher) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	stopCh, doneCh := w.stopCh, w.doneCh
	stopOnce := &w.stopOnce
	w.mu.Unlock()

	stopOnce.Do(func() { close(stopCh) })
	<-doneCh

	w.mu.Lock()
	w.running = false
	w.mu.Unlock()

	slog.Info("metrics pusher stopped")
}

// run is the push loop. The channels are passed in rather than read from the
// receiver so a restart cannot make a running loop observe the next
// generation's channels.
func (w *MetricsPusher) run(stopCh <-chan struct{}, doneCh chan<- struct{}) {
	defer close(doneCh)

	// Prime the shared sampler once: GetResources returns all-zero rates on its
	// first call for each metric (the first sample only records the baseline).
	// Priming here means the first demand-triggered frame already carries real
	// rates, which removes the frontend's old "fetch twice, 200ms apart"
	// workaround. The sample also seeds the response cache.
	if _, err := w.sampleFn(); err != nil {
		slog.Warn("metrics pusher: startup prime failed", slog.String("err", err.Error()))
	}

	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			w.tickOnce()
		}
	}
}

// tickOnce samples and pushes at most once per tick, honoring the slowest
// applicable rate. Runs on the worker goroutine, so lastSampleAt needs no lock.
func (w *MetricsPusher) tickOnce() {
	count, intervalMs := w.demandFn()
	if count == 0 {
		// No viewers: stop sampling. Clearing lastSampleAt means a client that
		// comes back gets a frame on the next tick instead of waiting out the
		// previous interval.
		w.lastSampleAt = time.Time{}
		return
	}

	now := time.Now()
	if !w.lastSampleAt.IsZero() && now.Sub(w.lastSampleAt) < time.Duration(intervalMs)*time.Millisecond {
		return
	}

	resp, err := w.sampleFn()
	if err != nil {
		// A failed sample is not fatal — the next tick retries. GetResources
		// only errors when every sampler failed.
		slog.Warn("metrics pusher: sample failed", slog.String("err", err.Error()))
		return
	}
	w.lastSampleAt = now
	w.pushFn(resp)
}

// StartMetricsPusher starts the global pusher (idempotent).
func StartMetricsPusher() {
	metricsPusherMu.Lock()
	defer metricsPusherMu.Unlock()
	if globalMetricsPusher != nil {
		return
	}
	globalMetricsPusher = NewMetricsPusher()
	globalMetricsPusher.Start()
}

// StopMetricsPusher stops the global pusher. Safe when never started.
func StopMetricsPusher() {
	metricsPusherMu.Lock()
	defer metricsPusherMu.Unlock()
	if globalMetricsPusher == nil {
		return
	}
	globalMetricsPusher.Stop()
	globalMetricsPusher = nil
}
