package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/model"
)

// ForgePoller periodically syncs every bound repository. State changes and
// comment activity are polled at different intervals: comments are the noisiest
// and least urgent, so they run less often, while state transitions stay
// responsive. The two tickers share one worker lifecycle.
//
// The poller is keyed by repository (platform, host, owner, repo), not by
// project row, so the same repository bound to two projects is fetched once.
type ForgePoller struct {
	syncer       *ForgeSyncer
	limiter      *forge.Limiter
	stopCh       chan struct{}
	doneCh       chan struct{}
	mu           sync.Mutex
	running      bool
	startup      time.Duration
	stateEvery   time.Duration
	commentEvery time.Duration
	// now is injectable for tests.
	now func() time.Time
	// cfgFn supplies the current config (notification toggles, insecure TLS),
	// re-read each cycle so hot-reload takes effect without a restart.
	cfgFn func() model.Config
}

// NewForgePoller builds a poller. The syncer owns fetching + derivation; the
// poller owns scheduling, rate limiting, and repo enumeration.
func NewForgePoller(syncer *ForgeSyncer, limiter *forge.Limiter, cfgFn func() model.Config) *ForgePoller {
	return &ForgePoller{
		syncer:       syncer,
		limiter:      limiter,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		startup:      15 * time.Second,
		stateEvery:   60 * time.Second,
		commentEvery: 5 * time.Minute,
		now:          time.Now,
		cfgFn:        cfgFn,
	}
}

// Start begins the polling loop in a goroutine.
func (p *ForgePoller) Start() {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()

	go p.run()
	slog.Info(
		"forge poller started",
		slog.Duration("state_interval", p.stateEvery),
		slog.Duration("comment_interval", p.commentEvery),
	)
}

// Stop signals the poller to stop and waits for it to finish.
func (p *ForgePoller) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	close(p.stopCh)
	<-p.doneCh

	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
	slog.Info("forge poller stopped")
}

// run is the main loop. State and comment cycles share the stop channel.
func (p *ForgePoller) run() {
	defer close(p.doneCh)

	select {
	case <-time.After(p.startup):
	case <-p.stopCh:
		return
	}

	// Run once at startup (full pass), then on independent tickers. State
	// transitions are polled frequently; comment activity is folded into the
	// slower ticker only, which is what keeps comment traffic off the fast path.
	p.syncAll(SyncOptions{IncludeComments: true})

	stateTicker := time.NewTicker(p.stateEvery)
	commentTicker := time.NewTicker(p.commentEvery)
	defer stateTicker.Stop()
	defer commentTicker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-stateTicker.C:
			p.syncAll(SyncOptions{IncludeComments: false})
		case <-commentTicker.C:
			p.syncAll(SyncOptions{IncludeComments: true})
		}
	}
}

// syncAll syncs every distinct bound repository. Failures are logged per repo
// and never abort the whole cycle.
func (p *ForgePoller) syncAll(opts SyncOptions) {
	repos, err := ListProjectForges()
	if err != nil {
		slog.Warn("forge poller: list bindings failed", slog.String("err", err.Error()))
		return
	}
	if len(repos) == 0 {
		return
	}

	// Deduplicate by repository so two projects bound to one repo fetch once.
	seen := make(map[string]bool)
	for i := range repos {
		pf := repos[i]
		key := pf.Platform + "|" + pf.Host + "|" + pf.Owner + "/" + pf.Repo
		if seen[key] {
			continue
		}
		seen[key] = true

		// Respect the host's rate budget before each repository.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := p.limiter.Wait(ctx, pf.Host); err != nil {
			cancel()
			slog.Debug("forge poller: rate budget unavailable",
				slog.String("host", pf.Host), slog.String("err", err.Error()))
			continue
		}
		err := p.syncer.SyncRepoWithOptions(ctx, pf, opts)
		p.limiter.Release()
		cancel()

		if err != nil {
			// Classify: a rate-limit error parks the host; an auth error means
			// the credential is bad and the host should stop being polled until
			// the user fixes it.
			classified := p.limiter.WrapError(pf.Host, err)
			slog.Warn("forge poller: sync failed",
				slog.String("repo", key), slog.String("err", classified.Error()))
			continue
		}
		p.limiter.ObserveSuccess(pf.Host)
	}
}

// SyncNow runs one full cycle immediately (used by tests and the manual refresh
// path). It does not affect the ticker schedule.
func (p *ForgePoller) SyncNow() {
	p.syncAll(SyncOptions{IncludeComments: true})
}

// globalForgePoller is the running instance, protected by mu.
var (
	globalForgePoller *ForgePoller
	forgePollerMu     sync.Mutex
)

// StartForgePoller starts the global poller if it is not already running.
func StartForgePoller(syncer *ForgeSyncer, limiter *forge.Limiter, cfgFn func() model.Config) {
	forgePollerMu.Lock()
	defer forgePollerMu.Unlock()
	if globalForgePoller != nil {
		return
	}
	p := NewForgePoller(syncer, limiter, cfgFn)
	p.Start()
	globalForgePoller = p
}

// StopForgePoller stops the global poller.
func StopForgePoller() {
	forgePollerMu.Lock()
	p := globalForgePoller
	globalForgePoller = nil
	forgePollerMu.Unlock()
	if p != nil {
		p.Stop()
	}
}

// ForgePollerRunning reports whether the global poller is active (tests).
func ForgePollerRunning() bool {
	forgePollerMu.Lock()
	defer forgePollerMu.Unlock()
	return globalForgePoller != nil
}
