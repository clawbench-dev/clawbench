package rag

import (
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// cleanupService defines the interface for session cleanup operations.
// This allows testing without a real database connection.
type cleanupService interface {
	GetExpiredArchivedSessions(cutoff time.Time) ([]string, error)
	PurgeArchivedData(sessionIDs []string) (sessionsPurged, messagesPurged int64, err error)
}

// CleanupWorker periodically purges archived data that has exceeded
// the configured retention period. It deletes from both SQLite (chunks)
// and SQLite (messages, sessions, raw responses).
type CleanupWorker struct {
	store    *Store
	cfg      model.RAGConfig
	stopCh   chan struct{}
	doneCh   chan struct{}
	mu       sync.Mutex
	running  bool
	svc      cleanupService // abstracted service layer for testability
	startup  time.Duration  // delay before first cleanup run
	interval time.Duration  // interval between cleanup runs
}

// NewCleanupWorker creates a new cleanup worker.
func NewCleanupWorker(store *Store, cfg model.RAGConfig) *CleanupWorker {
	return &CleanupWorker{
		store:    store,
		cfg:      cfg,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		svc:      &realCleanupService{},
		startup:  5 * time.Minute,
		interval: 24 * time.Hour,
	}
}

// Start begins the cleanup loop in a goroutine.
func (w *CleanupWorker) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	go w.run()
	slog.Info(
		"rag cleanup worker started",
		slog.Int("retention_days", w.cfg.RetentionDays),
	)
}

// Stop signals the cleanup worker to stop and waits for it to finish.
func (w *CleanupWorker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	close(w.stopCh)
	<-w.doneCh

	w.mu.Lock()
	w.running = false
	w.mu.Unlock()

	slog.Info("rag cleanup worker stopped")
}

// run is the main cleanup loop. Runs once after a startup delay,
// then every interval.
func (w *CleanupWorker) run() {
	defer close(w.doneCh)

	select {
	case <-time.After(w.startup):
	case <-w.stopCh:
		return
	}

	w.cleanup()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.cleanup()
		}
	}
}

// cleanup performs one purge cycle: find expired archived sessions,
// then delete chunks and SQLite data.
func (w *CleanupWorker) cleanup() {
	cutoff := time.Now().AddDate(0, 0, -w.cfg.RetentionDays)

	sessionIDs, err := w.svc.GetExpiredArchivedSessions(cutoff)
	if err != nil {
		slog.Error("rag cleanup: failed to query expired sessions", slog.String("err", err.Error()))
		return
	}
	if len(sessionIDs) == 0 {
		slog.Debug("rag cleanup: no expired sessions to purge")
		return
	}

	// 1. Delete SQLite chunks for these sessions (FTS synced in same transaction)
	var chunksPurged int64
	if w.store != nil {
		chunksPurged, err = w.store.DeleteChunksBySessionIDs(sessionIDs)
		if err != nil {
			slog.Error("rag cleanup: failed to delete chunks", slog.String("err", err.Error()))
		}
	}

	// 2. Delete SQLite data (ai_raw_responses → chat_history → chat_sessions)
	sessionsPurged, messagesPurged, err := w.svc.PurgeArchivedData(sessionIDs)
	if err != nil {
		slog.Error("rag cleanup: failed to purge data", slog.String("err", err.Error()))
		return
	}

	slog.Info(
		"rag cleanup: purged expired data",
		slog.Int64("sessions", sessionsPurged),
		slog.Int64("messages", messagesPurged),
		slog.Int64("chunks", chunksPurged),
		slog.Int("retention_days", w.cfg.RetentionDays),
	)
}

// realCleanupService implements cleanupService using the real service package.
type realCleanupService struct{}

func (r *realCleanupService) GetExpiredArchivedSessions(cutoff time.Time) ([]string, error) {
	return service.GetExpiredArchivedSessions(cutoff)
}

func (r *realCleanupService) PurgeArchivedData(sessionIDs []string) (int64, int64, error) {
	return service.PurgeArchivedData(sessionIDs)
}
