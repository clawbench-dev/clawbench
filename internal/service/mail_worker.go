package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	// mailSyncInterval is how often the background worker checks every enabled
	// account. Deliberately a constant rather than a config knob: the value has
	// to be sane for everyone, and a hidden setting nobody can find is worse
	// than a fixed three minutes.
	mailSyncInterval = 3 * time.Minute
	// mailSyncStartupDelay lets the server finish booting before the first
	// IMAP connection is opened.
	mailSyncStartupDelay = 20 * time.Second
	// mailSyncAccountTimeout bounds one account's whole sync pass.
	mailSyncAccountTimeout = 5 * time.Minute
)

// MailSyncWorker polls every enabled account in the background.
type MailSyncWorker struct {
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex
	running  bool
	interval time.Duration
	startup  time.Duration
}

var (
	globalMailWorker   *MailSyncWorker
	globalMailWorkerMu sync.Mutex
)

// StartMailSyncWorker starts the singleton poller. It is a no-op when the
// database has no mail accounts, and safe to call twice.
func StartMailSyncWorker() {
	globalMailWorkerMu.Lock()
	defer globalMailWorkerMu.Unlock()
	if globalMailWorker != nil {
		return
	}
	w := &MailSyncWorker{
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		interval: mailSyncInterval,
		startup:  mailSyncStartupDelay,
	}
	globalMailWorker = w
	w.start()
}

// StopMailSyncWorker stops the poller and waits for the current pass to end.
func StopMailSyncWorker() {
	globalMailWorkerMu.Lock()
	w := globalMailWorker
	globalMailWorker = nil
	globalMailWorkerMu.Unlock()
	if w != nil {
		w.stop()
	}
}

func (w *MailSyncWorker) start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()
	go w.run()
}

func (w *MailSyncWorker) stop() {
	w.stopOnce.Do(func() { close(w.stopCh) })
	<-w.doneCh
	w.mu.Lock()
	w.running = false
	w.mu.Unlock()
}

func (w *MailSyncWorker) run() {
	defer close(w.doneCh)

	select {
	case <-time.After(w.startup):
	case <-w.stopCh:
		return
	}

	w.tick()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.tick()
		}
	}
}

func (w *MailSyncWorker) tick() {
	accounts, err := ListMailAccounts()
	if err != nil {
		slog.Warn("mail: list accounts failed", "error", err)
		return
	}
	for _, a := range accounts {
		if !a.Enabled {
			continue
		}
		select {
		case <-w.stopCh:
			return
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), mailSyncAccountTimeout)
		result, err := SyncMailAccount(ctx, a.ID)
		cancel()
		if err != nil {
			slog.Warn("mail: sync failed", "account", a.ID, "error", err)
			continue
		}
		if result.NewMessages > 0 {
			slog.Info("mail: synced", "account", a.ID, "new", result.NewMessages,
				"folders", result.Folders)
		}
	}
}
