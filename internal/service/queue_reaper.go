package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// launchConsumerExecution is an indirection over LaunchSessionExecution so
// tests can assert the recovery path without starting a real AI backend.
// Mirrors the dequeueQueuedMessage indirection in drain.go.
var launchConsumerExecution = LaunchSessionExecution

// QueueReaper is a safety net for stranded queued messages.
//
// A user message is persisted with queued=1 and then handed to a consumer
// (TrySetSessionRunning + a drain loop, or the enqueue self-heal). That handoff
// has historically been lossy: if the session's running flag is stale — or the
// consumer exits in the window between the claim and the handoff — the row stays
// queued=1 with nobody left to dequeue it. The user then sees a message that
// never gets an AI response until they cancel and re-send (cancel clears the
// stuck running flag and the queue, so the re-send starts cleanly).
//
// This worker periodically finds such rows — queued=1, older than a grace
// period, on a session that is not running and not archived — and starts a
// consumer for them. It does not need to know WHY the row was stranded; it only
// needs to notice that it is.
//
// The grace period matters: a freshly inserted row is legitimately queued=1 for
// a moment while the request that inserted it claims the session, so reaping
// immediately would race the normal path and could start a second consumer.
type QueueReaper struct {
	grace    time.Duration
	interval time.Duration

	stopCh chan struct{}
	doneCh chan struct{}

	mu      sync.Mutex
	running bool

	// stopOnce guards close(stopCh): Stop is safe to call from more than one
	// goroutine (shutdown paths can race), and an unguarded close would panic.
	stopOnce sync.Once

	// reapFn is the per-session recovery hook. Overridable in tests so the
	// reaper can be verified without launching a real AI execution.
	reapFn func(sessionID string) bool
}

// Queue reaper defaults. The grace window is deliberately much larger than the
// insert→claim handoff (microseconds to a few ms) so it never races the normal
// path, while still being short enough that a stranded message recovers long
// before a user would notice and retry.
const (
	defaultQueueReapGrace    = 30 * time.Second
	defaultQueueReapInterval = 15 * time.Second
)

// globalQueueReaper is the running instance, protected by mu.
var (
	globalQueueReaper *QueueReaper
	queueReaperMu     sync.Mutex
)

// NewQueueReaper creates a queue reaper with the default windows.
//
// The stop/done channels are created by Start (one generation per run), so a
// stopped instance can be restarted without reusing closed channels.
func NewQueueReaper() *QueueReaper {
	return &QueueReaper{
		grace:    defaultQueueReapGrace,
		interval: defaultQueueReapInterval,
		reapFn:   EnsureConsumer,
	}
}

// Start begins the reap loop in a goroutine.
//
// Reusable after Stop: fresh channels are created per run, so a restart cannot
// close an already-closed channel. That matters because the shutdown and
// startup paths are independent — a Stop/Start pair on the same instance (e.g.
// a reload or a test reusing the worker) previously panicked inside run's
// deferred close(doneCh).
func (w *QueueReaper) Start() {
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
	slog.Info("queue reaper started",
		slog.Duration("grace", w.grace),
		slog.Duration("interval", w.interval))
}

// Stop signals the reaper to stop and waits for it to finish.
//
// Safe to call concurrently and repeatedly. Closing stopCh is guarded by
// stopOnce so a second caller cannot close an already-closed channel and panic;
// every caller that observes running still waits for the same doneCh, so the
// "waits for it to finish" guarantee holds for all of them rather than only the
// first.
func (w *QueueReaper) Stop() {
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

	slog.Info("queue reaper stopped")
}

// run is the main reap loop. The first pass runs after one grace period rather
// than immediately: at startup nothing can be stranded yet (a fresh process has
// no stale running flags), and waiting avoids a burst of DB reads while the
// server is still coming up.
//
// The channels are passed in rather than read from the receiver so a restart
// cannot make a running loop observe the next generation's channels.
func (w *QueueReaper) run(stopCh <-chan struct{}, doneCh chan<- struct{}) {
	defer close(doneCh)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			w.reap()
		}
	}
}

// reap performs one recovery pass.
func (w *QueueReaper) reap() {
	// Tests tear the DB down between cases; without this guard a reap tick
	// racing teardown would panic on a nil pool.
	if !DBReady() {
		return
	}

	sessionIDs, err := findStrandedQueuedSessions(w.grace)
	if err != nil {
		slog.Error("queue reaper: failed to scan stranded queued messages",
			slog.String("err", err.Error()))
		return
	}
	for _, sid := range sessionIDs {
		// Re-check running state here rather than filtering in SQL: the flag
		// lives in memory, and a session may have been claimed since the scan.
		if IsSessionRunning(sid) {
			continue
		}
		if w.reapFn(sid) {
			slog.Warn("queue reaper: recovered stranded queued message(s)",
				slog.String("session", sid))
		}
	}
}

// findStrandedQueuedSessions returns the session IDs that have at least one
// queued=1 message older than grace, skipping archived sessions.
//
// created_at is written by SQLite as CURRENT_TIMESTAMP (UTC, "YYYY-MM-DD
// HH:MM:SS"), so the cutoff is computed in UTC and compared as a string —
// matching how the column is stored.
func findStrandedQueuedSessions(grace time.Duration) ([]string, error) {
	cutoff := time.Now().UTC().Add(-grace).Format("2006-01-02 15:04:05")

	rows, err := dbRead.QueryContext(context.Background(), `
		SELECT DISTINCT h.session_id
		FROM chat_history h
		JOIN chat_sessions s ON s.id = h.session_id
		WHERE h.queued = 1
		  AND h.session_id != ''
		  AND h.created_at < ?
		  AND s.archived = 0
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		ids = append(ids, sid)
	}
	return ids, rows.Err()
}

// EnsureConsumer starts an AI execution for a session that has queued messages
// but no running consumer. It is the reaper's recovery entry point and is safe
// to call concurrently with normal sends.
//
// Returns true when it successfully claimed the session and launched an
// execution, false when the session was already running (nothing to recover)
// or could not be started.
//
// The first queued row is deliberately NOT dequeued here: the launched
// execution's drain loop claims it (via DequeueQueuedMessage) as part of its
// normal loop. That keeps this function free of the insert/claim bookkeeping
// the HTTP paths do, and means a failed launch leaves the row queued for the
// next reap pass instead of losing it.
func EnsureConsumer(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	if !DBReady() {
		return false
	}

	info := GetSessionFullInfo(sessionID)
	if info == nil {
		// Session row is gone or archived — nothing to run against. The
		// archived case is already filtered in SQL, so this is a race with
		// archive/delete; leaving the row alone is correct.
		slog.Debug("queue reaper: session not found or archived, skipping",
			slog.String("session", sessionID))
		return false
	}

	// Claim the session, taking the execution context in the same step. If
	// another consumer won the race, it will drain the queue and this pass has
	// nothing to do.
	runCtx, created := TryClaimSessionRun(sessionID)
	if !created {
		return false
	}

	// Re-verify there is still something to run: the queue may have been
	// cleared (user cancel) or drained between the scan and the claim.
	msg, ok, err := dequeueQueuedMessage(sessionID)
	if err != nil {
		// A real DB error, not an empty queue. Release the claim so the next
		// pass can retry instead of leaving the session stuck as "running"
		// with nothing consuming it.
		slog.Error("queue reaper: dequeue failed, releasing claim",
			slog.String("session", sessionID), slog.String("err", err.Error()))
		SetSessionRunning(sessionID, false, true)
		return false
	}
	if !ok {
		// Queue emptied (cancelled or drained elsewhere) — release the claim
		// rather than launching an execution with nothing to do.
		SetSessionRunning(sessionID, false, true)
		return false
	}

	// We hold the claimed row, so hand it to the execution directly. Using
	// LaunchSessionExecution (rather than letting the drain loop re-dequeue)
	// avoids re-queueing it and keeps the reply anchored to this message.
	//
	// Files MUST be carried: executeStreamRunShared builds the prompt itself and
	// never goes through the handler's builder, so a missing Files silently
	// drops every attachment — the AI answers as if the user had sent text only,
	// while the bubble still shows the files. The drain path sets the same field
	// from its dequeued row (see RunDrainLoop).
	launchConsumerExecution(LaunchConfig{
		SessionID:   sessionID,
		ProjectPath: info.ProjectPath,
		BackendName: info.Backend,
		AgentID:     info.AgentID,
		Message:     msg.Content,
		Files:       msg.Files,
		QueueID:     msg.QueueID,
		RunCtx:      runCtx,
	})
	return true
}

// StartQueueReaper starts the global queue reaper.
func StartQueueReaper() {
	queueReaperMu.Lock()
	globalQueueReaper = NewQueueReaper()
	globalQueueReaper.Start()
	queueReaperMu.Unlock()
}

// StopQueueReaper stops the global queue reaper.
func StopQueueReaper() {
	queueReaperMu.Lock()
	if globalQueueReaper != nil {
		globalQueueReaper.Stop()
		globalQueueReaper = nil
	}
	queueReaperMu.Unlock()
}

// CountStrandedQueuedSessions reports how many sessions currently have a
// stranded queued message older than grace. Used by diagnostics.
func CountStrandedQueuedSessions(grace time.Duration) (int, error) {
	ids, err := findStrandedQueuedSessions(grace)
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}
