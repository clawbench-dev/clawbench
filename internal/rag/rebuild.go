package rag

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/service"
	"clawbench/internal/ws"
)

// RebuildKind identifies which layer of the index a rebuild invalidates.
//
// The three kinds are distinguished by WHAT they discard, which is what decides
// how much work the indexer has to redo:
//
//	fts    — chunk_text_segmented only. Re-segments existing chunks; chunking and
//	         embeddings are untouched. Used after a segmenter/dictionary change.
//	vector — embeddings only. Re-embeds existing chunks; chunking and segmentation
//	         are untouched. Used after switching embedding models.
//	full   — everything. Deletes all chunks so the indexer re-chunks, re-segments,
//	         and re-embeds from chat_history. The only kind that re-chunks, so it
//	         is the only one that honors a chunk_size/chunk_overlap change.
type RebuildKind string

const (
	RebuildFTS    RebuildKind = "fts"
	RebuildVector RebuildKind = "vector"
	RebuildFull   RebuildKind = "full"
)

// Valid reports whether k is a known rebuild kind.
func (k RebuildKind) Valid() bool {
	switch k {
	case RebuildFTS, RebuildVector, RebuildFull:
		return true
	}
	return false
}

// RebuildStatus is a snapshot of the current rebuild, returned by the status
// endpoint so the UI can show progress without holding a request open.
type RebuildStatus struct {
	Kind   string `json:"kind"`   // "fts" | "vector" | "full" | ""
	Status string `json:"status"` // "idle" | "running" | "done" | "error" | "cancelled" | "blocked"
	Phase  string `json:"phase"`  // "resegmenting" | "embedding" | "indexing"

	// Total/Processed describe the queue the rebuild must drain. They are derived
	// from the store's pending counters rather than tracked incrementally, so a
	// concurrent indexer pass cannot make them drift.
	Total     int `json:"total"`
	Processed int `json:"processed"`

	ProgressPct int    `json:"progress_pct"`
	ElapsedMs   int64  `json:"elapsed_ms"`
	Error       string `json:"error,omitempty"`
}

// RebuildCoordinator serializes index rebuilds and reports their progress.
//
// All three kinds follow the same shape — mark the affected layer stale, then
// wake the indexer to redo that work — so this type owns no index logic of its
// own. It exists to give the three operations a single mutex (previously the FTS
// path and the vector path used separate flags and could run concurrently) and a
// single progress contract for the UI.
//
// The work itself is done by the indexer, which already drains pending queues in
// bounded batches. That is what keeps a rebuild interruptible and observable:
// a 44k-chunk re-segmentation takes ~149s, far beyond any HTTP timeout, so it
// must not be tied to a request or performed in one uninterruptible step.
type RebuildCoordinator struct {
	mu         sync.Mutex
	running    bool
	kind       RebuildKind
	generation uint64
	hub        *ws.StreamHub
	indexer    *Indexer

	// done stops the watch goroutine of the CURRENT run. It is recreated by each
	// Start and closed by Cancel/finish. Without prompt teardown the goroutine
	// would keep polling until its next tick, which can land after the store is
	// closed at shutdown — a use-after-close that panics.
	done chan struct{}

	status RebuildStatus
}

// NewRebuildCoordinator creates a coordinator bound to an indexer. If hub is nil,
// WebSocket progress broadcasts are skipped (useful for tests).
func NewRebuildCoordinator(hub *ws.StreamHub) *RebuildCoordinator {
	return &RebuildCoordinator{
		hub:    hub,
		status: RebuildStatus{Status: "idle"},
	}
}

// SetIndexer binds the indexer that performs the rebuild work. Called at startup
// after both exist; the coordinator tolerates a nil indexer so handlers can
// report "not available" rather than panicking.
func (c *RebuildCoordinator) SetIndexer(idx *Indexer) {
	c.mu.Lock()
	c.indexer = idx
	c.mu.Unlock()
}

// Start marks the requested layer stale and wakes the indexer.
//
// Returns an error when a rebuild is already running (the caller maps it to 409)
// or when the request cannot be honored (unknown kind, segmenter missing).
//
// The marking step is synchronous and cheap (one UPDATE, or a table clear for a
// full rebuild); the expensive redo happens in the indexer afterwards.
func (c *RebuildCoordinator) Start(store *Store, kind RebuildKind) error {
	if !kind.Valid() {
		return fmt.Errorf("unknown rebuild kind %q", kind)
	}
	if store == nil {
		return ErrStoreUnavailable
	}

	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return ErrRebuildInProgress
	}
	idx := c.indexer
	c.mu.Unlock()

	if idx == nil {
		return ErrIndexerUnavailable
	}

	// Refuse a re-segmentation rebuild without a segmenter: SegmentText degrades
	// to an identity function, so the "repair" would overwrite correctly split
	// text with unsplittable text. Checked before any state is mutated.
	if kind == RebuildFTS && !SegmenterAvailable() {
		return ErrSegmenterUnavailable
	}

	// Mark stale BEFORE taking the running slot, so a failure leaves no
	// half-started rebuild behind.
	total, err := c.markStale(store, kind)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.running = true
	c.kind = kind
	c.generation++
	myGen := c.generation
	done := make(chan struct{})
	c.done = done
	c.status = RebuildStatus{
		Kind:      string(kind),
		Status:    "running",
		Phase:     phaseFor(kind),
		Total:     total,
		Processed: 0,
	}
	c.mu.Unlock()

	// Wake the indexer so progress starts immediately rather than after up to one
	// poll interval. Nothing else drives the rebuild; the indexer's own loop is
	// what drains the queue.
	idx.Trigger()
	c.broadcast()

	go c.watch(myGen, store, kind, done)
	slog.Info("rag: rebuild started",
		slog.String("kind", string(kind)), slog.Int("queued", total))
	return nil
}

// markStale invalidates the layer the rebuild targets and returns the number of
// units the indexer must now process (chunks for fts/vector, messages for full).
func (c *RebuildCoordinator) markStale(store *Store, kind RebuildKind) (int, error) {
	switch kind {
	case RebuildFTS:
		n, err := store.MarkAllChunksForResegment()
		return int(n), err
	case RebuildVector:
		n, err := store.ResetVectorOnly(0)
		return int(n), err
	case RebuildFull:
		if _, err := store.ResetAllChunksForFullRebuild(); err != nil {
			return 0, err
		}
		// The chunks are gone, so the indexer must re-chunk from the source
		// messages: clear the indexed flags to put them back in its queue.
		n, err := service.ResetAllIndexed()
		return int(n), err
	}
	return 0, fmt.Errorf("unknown rebuild kind %q", kind)
}

// phaseFor maps a kind to the phase label shown while it runs.
func phaseFor(kind RebuildKind) string {
	switch kind {
	case RebuildFTS:
		return "resegmenting"
	case RebuildVector:
		return "embedding"
	default:
		return "indexing"
	}
}

// watch polls the queue the rebuild is draining and finishes once it is empty.
//
// Progress is derived from the store's pending counters rather than accumulated
// locally: the indexer is draining the same queue concurrently, so deriving keeps
// the two in agreement and needs no coordination between them.
//
// done is closed by finish/Cancel to stop the loop promptly. That matters beyond
// tidiness: the store is closed at shutdown, and a poll that lands after Close
// dereferences a closed *sql.DB and panics.
func (c *RebuildCoordinator) watch(myGen uint64, store *Store, kind RebuildKind, done <-chan struct{}) {
	start := time.Now()
	ticker := time.NewTicker(rebuildPollInterval)
	defer ticker.Stop()

	defer func() {
		// The store may already be gone by the time a late tick fires; a panic
		// here would take down the process, and the rebuild's real work is done
		// by the indexer regardless of whether we managed to report it.
		if r := recover(); r != nil {
			slog.Warn("rag: rebuild watcher recovered from panic", slog.Any("err", r))
		}
	}()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
		}

		c.mu.Lock()
		if c.generation != myGen {
			c.mu.Unlock()
			return // superseded by Cancel or a newer run
		}
		c.mu.Unlock()

		remaining, err := c.remaining(store, kind)
		if err != nil {
			c.finish(myGen, "error", err.Error(), start)
			return
		}

		// A vector rebuild cannot finish without an embedder, so report the
		// blockage instead of sitting at 0% forever.
		if kind == RebuildVector && remaining > 0 && !EmbedderHealthy() {
			c.finish(myGen, "blocked", "embedding service unavailable", start)
			return
		}

		if remaining == 0 {
			c.finish(myGen, "done", "", start)
			return
		}

		c.mu.Lock()
		if c.generation == myGen {
			// Total is fixed for the run; Processed is derived from what is left.
			// (Deriving rather than accumulating keeps this correct even though the
			// indexer drains the same queue concurrently.)
			processed := c.status.Total - remaining
			if processed < 0 {
				processed = 0
			}
			c.status.Processed = processed
			if c.status.Total > 0 {
				c.status.ProgressPct = processed * 100 / c.status.Total
			}
			c.status.ElapsedMs = time.Since(start).Milliseconds()
		}
		c.mu.Unlock()
		c.broadcast()
	}
}

// remaining returns how many units the rebuild still has to process.
func (c *RebuildCoordinator) remaining(store *Store, kind RebuildKind) (int, error) {
	switch kind {
	case RebuildFTS:
		return store.PendingResegmentCount()
	case RebuildVector:
		return store.PendingEmbeddingCount()
	case RebuildFull:
		return service.UnindexedCount()
	}
	return 0, fmt.Errorf("unknown rebuild kind %q", kind)
}

// finish records a terminal state and clears the running slot.
//
// Called from the watch goroutine itself, so it must not close `done` (that
// channel exists to stop this goroutine); it only clears the state.
func (c *RebuildCoordinator) finish(myGen uint64, status, errMsg string, start time.Time) {
	c.mu.Lock()
	if c.generation != myGen {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.status.Status = status
	c.status.Error = errMsg
	c.status.ElapsedMs = time.Since(start).Milliseconds()
	if status == "done" {
		c.status.ProgressPct = 100
	}
	final := c.status
	hub := c.hub
	c.mu.Unlock()

	slog.Info("rag: rebuild finished",
		slog.String("kind", final.Kind),
		slog.String("status", status),
		slog.Int64("elapsed_ms", final.ElapsedMs))
	broadcastRebuild(hub, final)
}

// Cancel stops the running rebuild. The stale marking is already committed, so
// the indexer will still finish the work in the background — this only ends the
// client-facing progress tracking.
//
// It also stops the watch goroutine so nothing touches the store afterwards,
// which is what makes it safe to call during shutdown.
func (c *RebuildCoordinator) Cancel() {
	c.mu.Lock()
	if c.done != nil {
		close(c.done)
		c.done = nil
	}
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.generation++ // supersede the watch goroutine
	c.running = false
	c.status.Status = "cancelled"
	c.status.Phase = ""
	final := c.status
	hub := c.hub
	c.mu.Unlock()

	broadcastRebuild(hub, final)
}

// IsRunning reports whether a rebuild is currently being tracked.
func (c *RebuildCoordinator) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// GetStatus returns a snapshot of the current rebuild state.
func (c *RebuildCoordinator) GetStatus() RebuildStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// broadcast pushes the current status to WebSocket clients, if any.
func (c *RebuildCoordinator) broadcast() {
	c.mu.Lock()
	snapshot := c.status
	hub := c.hub
	c.mu.Unlock()
	broadcastRebuild(hub, snapshot)
}

// rebuildPollInterval is how often the coordinator re-reads the pending counter.
// One second is frequent enough for a progress bar while costing one indexed
// COUNT per second — negligible next to the segmentation it is tracking.
const rebuildPollInterval = time.Second

// broadcastRebuild pushes a status snapshot to WebSocket clients. Best-effort:
// polling the status endpoint is authoritative, so a missing hub is not an error.
func broadcastRebuild(hub *ws.StreamHub, status RebuildStatus) {
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
		Event: "rag_rebuild",
		Data:  status,
	})
}
