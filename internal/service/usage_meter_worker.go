package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// UsageMeterWorker periodically evaluates the usage-meter rule.
//
// Why a worker at all: the turn-completion trigger only fires while a turn
// finishes, so a rule that is already over its baseline when the server starts
// (or one that crosses the line because a long-running scheduled task wrote its
// ledger row without going through the interactive terminal path) would never be
// noticed. The tick closes both gaps.
//
// The interval is deliberately coarse. Usage is recorded once per completed
// turn, so a 5-minute tick sees the same data a 10-second tick would; a faster
// tick would only add aggregate queries.
type UsageMeterWorker struct {
	interval time.Duration
	startup  time.Duration

	stopCh chan struct{}
	doneCh chan struct{}
	// wakeCh nudges the loop to tick now instead of waiting out the interval.
	// Buffered (size 1) so a burst of PATCHes collapses into one wake without
	// blocking the caller.
	wakeCh chan struct{}

	mu      sync.Mutex
	running bool

	// stopOnce guards close(stopCh): Stop can be called from more than one
	// goroutine (config reconfigure and shutdown race), and an unguarded close
	// would panic.
	stopOnce sync.Once

	// checkFn is overridable in tests so the loop can be verified without a
	// database.
	checkFn func(ctx context.Context) string
}

const (
	defaultUsageMeterInterval = 5 * time.Minute
	// A short startup delay lets the DB finish opening. The check is a no-op
	// without a DB, but running it against a half-initialized handle would log
	// a spurious warning on every boot.
	defaultUsageMeterStartup = 10 * time.Second
)

// NewUsageMeterWorker builds a worker with the production timings.
func NewUsageMeterWorker() *UsageMeterWorker {
	return &UsageMeterWorker{
		interval: defaultUsageMeterInterval,
		startup:  defaultUsageMeterStartup,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		wakeCh:   make(chan struct{}, 1),
		checkFn:  CheckUsageMeter,
	}
}

// NewUsageMeterWorkerForTest builds a worker with explicit timings and an
// injectable check, so the loop can be verified without a database. Exported
// because the tests live in the external service_test package.
func NewUsageMeterWorkerForTest(interval, startup time.Duration, check func(context.Context) string) *UsageMeterWorker {
	if check == nil {
		check = CheckUsageMeter
	}
	return &UsageMeterWorker{
		interval: interval,
		startup:  startup,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		wakeCh:   make(chan struct{}, 1),
		checkFn:  check,
	}
}

// Start begins the loop in a goroutine. Idempotent while running, and safe to
// call again after Stop.
//
// The channels and the once are re-created here rather than in the constructor:
// Stop closes stopCh, so a second Start on the same instance would otherwise
// hand the new loop an already-closed channel (and run's `defer close(doneCh)`
// would panic on a channel closed by the previous loop).
func (w *UsageMeterWorker) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.doneCh = make(chan struct{})
	w.stopOnce = sync.Once{}
	stopCh, doneCh := w.stopCh, w.doneCh
	w.mu.Unlock()

	go w.run(stopCh, doneCh)
	slog.Info("usage meter worker started", slog.Duration("interval", w.interval))
}

// Stop signals the loop to stop and waits for it to return. Safe to call more
// than once, and safe before Start.
func (w *UsageMeterWorker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	stopCh, doneCh, once := w.stopCh, w.doneCh, &w.stopOnce
	w.mu.Unlock()

	once.Do(func() { close(stopCh) })
	<-doneCh

	w.mu.Lock()
	w.running = false
	w.mu.Unlock()

	slog.Info("usage meter worker stopped")
}

// run is the loop body. The channels are passed in rather than read from the
// receiver so a Stop+Start cycle cannot race a stale channel.
func (w *UsageMeterWorker) run(stopCh <-chan struct{}, doneCh chan struct{}) {
	defer close(doneCh)

	select {
	case <-time.After(w.startup):
	case <-stopCh:
		return
	}
	w.tick()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-w.wakeCh:
			w.tick()
		case <-ticker.C:
			w.tick()
		}
	}
}

func (w *UsageMeterWorker) tick() {
	// Cheap pre-check. With a rule, every tick runs the cycle aggregate (the
	// capsule must track usage as it lands). Without one there is no
	// notification to send and no aggregate to run — but the clients still have
	// to be TOLD the meter is off, which is what the disabled broadcast does.
	// It is idempotent, so repeated ticks send nothing after the first.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w.checkFn(ctx)
}

// Global worker handle, mirroring the session-cleanup worker's shape.
var (
	usageMeterWorkerMu sync.Mutex
	usageMeterWorker   *UsageMeterWorker
)

// StartUsageMeterWorker starts the global usage-meter worker.
func StartUsageMeterWorker() {
	usageMeterWorkerMu.Lock()
	defer usageMeterWorkerMu.Unlock()
	if usageMeterWorker != nil {
		return
	}
	usageMeterWorker = NewUsageMeterWorker()
	usageMeterWorker.Start()
}

// StopUsageMeterWorker stops the global usage-meter worker.
func StopUsageMeterWorker() {
	usageMeterWorkerMu.Lock()
	w := usageMeterWorker
	usageMeterWorker = nil
	usageMeterWorkerMu.Unlock()
	if w != nil {
		w.Stop()
	}
}

// ReconfigureUsageMeterWorker nudges the worker after a config PATCH so a newly
// configured rule is evaluated within seconds instead of up to one interval.
//
// It does NOT stop and recreate the worker, and it does NOT run the check
// inline:
//
//   - Stopping would deadlock. The caller (the PATCH path) holds configMutex,
//     and a tick that is mid-check needs that same mutex to persist its
//     notification state.
//   - Running the check inline would read a half-applied rule: the PATCH holds
//     configMutex, and applyHotReloadGlobals runs inside that window.
//
// So the worker is woken asynchronously instead. By the time the loop picks up
// the wake, the PATCH has released configMutex and the new rule is in place.
func ReconfigureUsageMeterWorker() {
	StartUsageMeterWorker()

	usageMeterWorkerMu.Lock()
	w := usageMeterWorker
	usageMeterWorkerMu.Unlock()
	if w == nil {
		return
	}
	w.Wake()
}

// Wake asks the loop to run a check now. Non-blocking: if a wake is already
// pending, this one collapses into it.
func (w *UsageMeterWorker) Wake() {
	select {
	case w.wakeCh <- struct{}{}:
	default:
	}
}
