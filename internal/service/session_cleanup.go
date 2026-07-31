package service

import (
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/model"
)

// sessionCleanupSvc defines the interface for session cleanup operations.
// This allows testing without a real database connection.
type sessionCleanupSvc interface {
	GetExpiredDeletedSessions(cutoff time.Time) ([]string, error)
	PurgeDeletedData(sessionIDs []string) (sessionsPurged, messagesPurged int64, err error)
}

// realSessionCleanupSvc implements sessionCleanupSvc using the real service package.
type realSessionCleanupSvc struct{}

func (r *realSessionCleanupSvc) GetExpiredDeletedSessions(cutoff time.Time) ([]string, error) {
	return GetExpiredDeletedSessions(cutoff)
}

func (r *realSessionCleanupSvc) PurgeDeletedData(sessionIDs []string) (int64, int64, error) {
	return PurgeDeletedData(sessionIDs)
}

// SessionCleanupWorker periodically purges archived sessions that have exceeded
// the configured retention period. When ArchiveRetentionEnabled is true and
// ArchiveRetentionDays > 0, it finds all soft-deleted sessions whose updated_at
// (set to archive time) is older than the cutoff and hard-deletes them along
// with all associated data (messages, tool calls, raw responses, task executions).
type SessionCleanupWorker struct {
	cfg      model.Config
	svc      sessionCleanupSvc // abstracted service layer for testability
	stopCh   chan struct{}
	doneCh   chan struct{}
	mu       sync.Mutex
	running  bool
	startup  time.Duration // delay before first cleanup run
	interval time.Duration // interval between cleanup runs
}

// globalSessionCleanup is the running instance, protected by mu.
var globalSessionCleanup *SessionCleanupWorker
var sessionCleanupMu sync.Mutex

// NewSessionCleanupWorker creates a new session cleanup worker.
func NewSessionCleanupWorker(cfg model.Config) *SessionCleanupWorker {
	return &SessionCleanupWorker{
		cfg:      cfg,
		svc:      &realSessionCleanupSvc{},
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		startup:  5 * time.Minute,
		interval: 24 * time.Hour,
	}
}

// newSessionCleanupWorkerWithSvc creates a worker with a custom service implementation (for testing).
func newSessionCleanupWorkerWithSvc(cfg model.Config, svc sessionCleanupSvc) *SessionCleanupWorker {
	return &SessionCleanupWorker{
		cfg:      cfg,
		svc:      svc,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		startup:  100 * time.Millisecond, // shorter for tests
		interval: 1 * time.Hour,
	}
}

// Start begins the cleanup loop in a goroutine.
func (w *SessionCleanupWorker) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	go w.run()
	slog.Info("session cleanup worker started",
		slog.Bool("archive_retention_enabled", w.cfg.Session.ArchiveRetentionEnabled),
		slog.Int("archive_retention_days", w.cfg.Session.ArchiveRetentionDays),
	)
}

// Stop signals the cleanup worker to stop and waits for it to finish.
func (w *SessionCleanupWorker) Stop() {
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

	slog.Info("session cleanup worker stopped")
}

// run is the main cleanup loop.
func (w *SessionCleanupWorker) run() {
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

// cleanup performs one purge cycle.
func (w *SessionCleanupWorker) cleanup() {
	if !w.cfg.Session.ArchiveRetentionEnabled {
		slog.Debug("session cleanup: archive retention disabled, skipping")
		return
	}
	if w.cfg.Session.ArchiveRetentionDays <= 0 {
		slog.Debug("session cleanup: archive retention days is 0 (keep forever), skipping")
		return
	}

	cutoff := time.Now().AddDate(0, 0, -w.cfg.Session.ArchiveRetentionDays)

	sessionIDs, err := w.svc.GetExpiredDeletedSessions(cutoff)
	if err != nil {
		slog.Error("session cleanup: failed to query expired sessions", slog.String("err", err.Error()))
		return
	}
	if len(sessionIDs) == 0 {
		slog.Debug("session cleanup: no expired sessions to purge")
		return
	}

	// Delete RAG chunks for these sessions first (FTS + vec0 vectors)
	chunksPurged, err := purgeRAGChunksBySessionIDs(sessionIDs)
	if err != nil {
		slog.Error("session cleanup: failed to delete RAG chunks", slog.String("err", err.Error()))
		// Continue with session data purge even if chunk deletion fails
	}

	// Purge session data (ai_raw_responses, chat_tool_calls, chat_history, task_executions, chat_sessions)
	sessionsPurged, messagesPurged, err := w.svc.PurgeDeletedData(sessionIDs)
	if err != nil {
		slog.Error("session cleanup: failed to purge session data", slog.String("err", err.Error()))
		return
	}

	slog.Info("session cleanup: purged expired archived sessions",
		slog.Int64("sessions", sessionsPurged),
		slog.Int64("messages", messagesPurged),
		slog.Int64("chunks", chunksPurged),
		slog.Int("retention_days", w.cfg.Session.ArchiveRetentionDays),
	)
}

// purgeRAGChunksBySessionIDs deletes RAG chunks for the given session IDs.
func purgeRAGChunksBySessionIDs(sessionIDs []string) (int64, error) {
	if purgeRAGChunksFn != nil {
		return purgeRAGChunksFn(sessionIDs)
	}
	return 0, nil
}

// purgeRAGChunksFn is the callback for deleting RAG chunks by session IDs.
// Set by main.go during startup. Defaults to nil (RAG chunks not purged).
var purgeRAGChunksFn func(sessionIDs []string) (int64, error)

// SetPurgeRAGChunksFn sets the callback for deleting RAG chunks by session IDs.
func SetPurgeRAGChunksFn(fn func(sessionIDs []string) (int64, error)) {
	purgeRAGChunksFn = fn
}

// PurgeRAGChunksBySessionIDs deletes RAG chunks for the given session IDs.
// This is a public wrapper around the callback, used by DestroySession handler
// to clean up RAG data when physically deleting a session.
func PurgeRAGChunksBySessionIDs(sessionIDs []string) (int64, error) {
	if purgeRAGChunksFn != nil {
		return purgeRAGChunksFn(sessionIDs)
	}
	return 0, nil
}

// StartSessionCleanupWorker starts the global session cleanup worker.
func StartSessionCleanupWorker(cfg model.Config) {
	sessionCleanupMu.Lock()
	globalSessionCleanup = NewSessionCleanupWorker(cfg)
	globalSessionCleanup.Start()
	sessionCleanupMu.Unlock()
}

// StopSessionCleanupWorker stops the global session cleanup worker.
func StopSessionCleanupWorker() {
	sessionCleanupMu.Lock()
	if globalSessionCleanup != nil {
		globalSessionCleanup.Stop()
		globalSessionCleanup = nil
	}
	sessionCleanupMu.Unlock()
}

// ReconfigureSessionCleanup recreates the session cleanup worker with new config.
func ReconfigureSessionCleanup(cfg model.Config) {
	StopSessionCleanupWorker()
	StartSessionCleanupWorker(cfg)
}
