package rag

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/ws"
)

// FTSRebuildStatus is a snapshot of the FTS rebuild state, returned by the
// status endpoint so the UI can show progress without holding a request open.
type FTSRebuildStatus struct {
	Status      string `json:"status"` // "idle" | "running" | "done" | "error" | "cancelled"
	Phase       string `json:"phase"`  // "resegmenting" | "indexing"
	Total       int    `json:"total"`
	Processed   int    `json:"processed"`
	Resegmented int64  `json:"resegmented"`
	Indexed     int64  `json:"indexed"`
	ElapsedMs   int64  `json:"elapsed_ms"`
	ProgressPct int    `json:"progress_pct"`
	Error       string `json:"error,omitempty"`
}

// FTSRebuildWorker runs a full-text index rebuild off the request path.
//
// The rebuild re-segments every chunk with gse, which measured ~149s on a
// 44k-chunk production store — far longer than any sane HTTP request timeout
// (the frontend aborts at 10s). Running it inline made a successful rebuild
// surface as a failure in the UI, so the handler now only starts the work and
// the client polls GetStatus.
//
// Mirrors ClusterWorker: at most one run at a time, a generation counter so a
// stale goroutine cannot clear newer state, and optional WebSocket progress
// broadcasts.
type FTSRebuildWorker struct {
	mu         sync.Mutex
	running    bool
	cancelFn   context.CancelFunc
	generation uint64
	hub        *ws.StreamHub

	status FTSRebuildStatus
}

// NewFTSRebuildWorker creates a worker. If hub is nil, progress broadcasts are
// skipped (useful for tests).
func NewFTSRebuildWorker(hub *ws.StreamHub) *FTSRebuildWorker {
	return &FTSRebuildWorker{
		hub:    hub,
		status: FTSRebuildStatus{Status: "idle"},
	}
}

// Start begins a rebuild in the background.
//
// Returns false when a rebuild is already running; the caller maps that to 409.
// The segmenter check happens inside the goroutine as well, but is done here
// first so the handler can return a precise error synchronously instead of
// making the client poll to discover the failure.
func (w *FTSRebuildWorker) Start(store *Store) bool {
	if !SegmenterAvailable() {
		w.mu.Lock()
		w.status = FTSRebuildStatus{
			Status: "error",
			Error:  ErrSegmenterUnavailable.Error(),
		}
		w.mu.Unlock()
		return false
	}

	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancelFn = cancel
	w.running = true
	w.generation++
	myGen := w.generation
	w.status = FTSRebuildStatus{Status: "running", Phase: "resegmenting"}
	w.mu.Unlock()

	go w.run(ctx, myGen, store)
	return true
}

// IsRunning reports whether a rebuild is currently active.
func (w *FTSRebuildWorker) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// GetStatus returns a snapshot of the current rebuild state.
func (w *FTSRebuildWorker) GetStatus() FTSRebuildStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

// Cancel stops an in-progress rebuild. The running goroutine observes the
// cancelled context between chunks and leaves the index untouched.
func (w *FTSRebuildWorker) Cancel() {
	w.mu.Lock()
	if w.cancelFn != nil {
		w.cancelFn()
	}
	w.running = false
	w.cancelFn = nil
	w.generation++
	w.status.Status = "cancelled"
	w.status.Phase = ""
	w.mu.Unlock()
}

// run performs the rebuild and records the outcome.
func (w *FTSRebuildWorker) run(ctx context.Context, myGen uint64, store *Store) {
	start := time.Now()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("fts rebuild worker: recovered from panic", slog.Any("err", r))
			w.mu.Lock()
			if w.generation == myGen {
				w.running = false
				w.cancelFn = nil
				w.status = FTSRebuildStatus{Status: "error", Error: "internal error"}
			}
			w.mu.Unlock()
		}
	}()

	progressCb := func(processed, total int) {
		w.mu.Lock()
		if w.generation != myGen {
			w.mu.Unlock()
			return
		}
		w.status.Processed = processed
		w.status.Total = total
		w.status.ElapsedMs = time.Since(start).Milliseconds()
		if total > 0 {
			w.status.ProgressPct = processed * 100 / total
		}
		snapshot := w.status
		hub := w.hub
		w.mu.Unlock()
		broadcastFTSRebuild(hub, snapshot)
	}

	indexed, resegmented, err := store.RebuildFTS(ctx, progressCb)

	w.mu.Lock()
	defer w.mu.Unlock()
	// A newer run (or a Cancel) bumped the generation; do not clobber its state.
	if w.generation != myGen {
		return
	}
	w.running = false
	w.cancelFn = nil

	elapsed := time.Since(start).Milliseconds()
	switch {
	case ctx.Err() != nil:
		w.status.Status = "cancelled"
		w.status.Phase = ""
		w.status.ElapsedMs = elapsed
		slog.Info("fts rebuild worker: cancelled", slog.Int64("elapsed_ms", elapsed))
	case err != nil:
		w.status.Status = "error"
		w.status.Phase = ""
		w.status.ElapsedMs = elapsed
		w.status.Error = err.Error()
		slog.Error("fts rebuild worker: failed", slog.String("err", err.Error()))
	default:
		w.status.Status = "done"
		w.status.Phase = "indexing"
		w.status.Indexed = indexed
		w.status.Resegmented = resegmented
		w.status.ProgressPct = 100
		w.status.ElapsedMs = elapsed
		slog.Info("fts rebuild worker: complete",
			slog.Int64("indexed", indexed),
			slog.Int64("resegmented", resegmented),
			slog.Int64("elapsed_ms", elapsed))
	}
	broadcastFTSRebuild(w.hub, w.status)
}

// broadcastFTSRebuild pushes a status snapshot to WebSocket clients, if a hub
// is configured. Progress is best-effort: polling the status endpoint is the
// authoritative path, so a missing hub is not an error.
func broadcastFTSRebuild(hub *ws.StreamHub, status FTSRebuildStatus) {
	if hub == nil {
		return
	}
	mgr := hub.Manager()
	if mgr == nil {
		return
	}
	mgr.BroadcastEvent(ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		ID:    ws.GenerateEventID(),
		Event: "rag_fts_rebuild",
		Data:  status,
	})
}
