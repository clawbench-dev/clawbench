package rag

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/service"
	"clawbench/internal/ws"
)

// ClusterProgress represents the progress state of a cluster computation.
type ClusterProgress struct {
	Status       string `json:"status"` // "idle" | "computing" | "done" | "error" | "cancelled"
	Phase        string `json:"phase"`  // "extracting" | "clustering" | "saving"
	MsgCount     int    `json:"msg_count"`
	ClusterCount int    `json:"cluster_count"`
	ElapsedMs    int64  `json:"elapsed_ms"`
	Mode         string `json:"mode"`         // available only when done
	ProgressPct  int    `json:"progress_pct"` // 0-100 fine-grained progress within phase
	Error        string `json:"error,omitempty"`
}

// ClusterWorker manages on-demand cluster computation with progress tracking.
// It runs a goroutine triggered by the user (not a cron job) and broadcasts
// progress updates via the WebSocket StreamHub.
type ClusterWorker struct {
	mu         sync.Mutex
	running    bool
	cancelFn   context.CancelFunc
	generation uint64 // incremented on each ComputeOnce and Stop; goroutine defer only clears if still same gen
	hub        *ws.StreamHub
}

// NewClusterWorker creates a ClusterWorker associated with the given StreamHub.
// If hub is nil, progress broadcasts are skipped (useful for testing).
func NewClusterWorker(hub *ws.StreamHub) *ClusterWorker {
	return &ClusterWorker{
		hub: hub,
	}
}

// ComputeOnce starts a single cluster computation in a goroutine.
// If a computation is already running, this is a no-op.
// The goroutine runs with a 600-second timeout context.
func (cw *ClusterWorker) ComputeOnce() {
	cw.mu.Lock()
	if cw.running {
		cw.mu.Unlock()
		slog.Debug("cluster worker: compute already running, skipping")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	cw.cancelFn = cancel
	cw.running = true
	cw.generation++
	myGen := cw.generation
	cw.mu.Unlock()

	// Write initial meta state immediately so /compute/status is consistent
	_ = service.SaveClusterMeta("computing", "", 0, 0, 0, "extracting")

	go cw.compute(ctx, myGen)
}

// IsRunning returns whether a computation is currently active.
func (cw *ClusterWorker) IsRunning() bool {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return cw.running
}

// GetProgress reads the cluster meta from the database and constructs
// a ClusterProgress snapshot.
func (cw *ClusterWorker) GetProgress() ClusterProgress {
	meta := service.GetClusterMeta()
	return ClusterProgress{
		Status:       meta.Progress,
		Phase:        meta.Phase,
		MsgCount:     meta.MsgCount,
		ClusterCount: meta.ClusterCount,
		ElapsedMs:    int64(meta.ElapsedMs),
		Mode:         meta.Mode,
		Error:        meta.ErrorMsg,
	}
}

// Stop cancels any running computation and marks the worker as not running.
// It also broadcasts a "cancelled" event so WS clients know immediately.
func (cw *ClusterWorker) Stop() {
	cw.mu.Lock()
	if cw.cancelFn != nil {
		cw.cancelFn()
	}
	cw.running = false
	cw.cancelFn = nil
	cw.generation++ // bump generation so stale goroutine defer won't clear new state
	hub := cw.hub
	cw.mu.Unlock()

	// Broadcast cancelled event directly (not through broadcastProgressWithGen,
	// which would reject it because generation was bumped).
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
		Event: "cluster_progress",
		Data: ClusterProgress{
			Status: "cancelled",
		},
	})
}

// compute executes the three-phase cluster computation:
// 1. extracting: fetch user message stats
// 2. clustering: cluster messages using best available method
// 3. saving: save cluster cache and final meta
func (cw *ClusterWorker) compute(ctx context.Context, myGen uint64) {
	start := time.Now()

	defer func() {
		// Recover from panics (e.g. nil dbRead after test teardown closes DB).
		if r := recover(); r != nil {
			slog.Warn("cluster worker: goroutine recovered from panic", slog.Any("err", r))
		}
		cw.mu.Lock()
		// Only clear state if this goroutine is still the "current" generation.
		// If Stop() or a new ComputeOnce() bumped generation, don't overwrite.
		if cw.generation == myGen {
			cw.running = false
			cw.cancelFn = nil
		}
		cw.mu.Unlock()
	}()

	// Phase 1: extracting
	stats, err := service.GetUserMessageStats(1000)
	if err != nil {
		elapsedMs := int(time.Since(start).Milliseconds())
		_ = service.SaveClusterMetaError("error", "extracting", err.Error())
		cw.broadcastProgressWithGen(myGen, "error", "extracting", 0, 0, int64(elapsedMs), "")
		slog.Error("cluster worker: extracting failed", slog.String("err", err.Error()))
		return
	}
	elapsedMs := int(time.Since(start).Milliseconds())
	_ = service.SaveClusterMeta("computing", "", len(stats), 0, elapsedMs, "extracting")
	cw.broadcastProgressWithGen(myGen, "computing", "extracting", len(stats), 0, int64(elapsedMs), "")

	// Check for context cancellation
	if ctx.Err() != nil {
		_ = service.SaveClusterMetaError("cancelled", "extracting", "user cancelled")
		cw.broadcastProgressWithGen(myGen, "cancelled", "extracting", len(stats), 0, int64(elapsedMs), "")
		slog.Info("cluster worker: cancelled during extracting")
		return
	}

	// Phase 2: clustering (with fine-grained progress)
	ragStats := make([]MessageStat, len(stats))
	for i, s := range stats {
		ragStats[i] = MessageStat{Text: s.Text, Count: s.Count}
	}
	progressCb := func(done, total int) {
		// If cancelled, don't send stale computing events
		if ctx.Err() != nil {
			return
		}
		pct := 0
		if total > 0 {
			pct = done * 100 / total
		}
		elapsedMsCb := int(time.Since(start).Milliseconds())
		cw.broadcastProgressWithGen(myGen, "computing", "clustering", len(stats), 0, int64(elapsedMsCb), "", pct)
	}
	// Vector threshold is lower because embeddings capture semantic similarity well.
	// FTS threshold is higher because token-based metrics are looser for short texts.
	const vectorThreshold = 0.65
	const ftsThreshold = 0.85
	clusters, mode := ClusterMessagesWithEmbeddings(ctx, ragStats, GlobalEmbedder, vectorThreshold, ftsThreshold, progressCb)

	// Check for context cancellation after clustering completes
	if ctx.Err() != nil {
		elapsedMs = int(time.Since(start).Milliseconds())
		_ = service.SaveClusterMetaError("cancelled", "clustering", "user cancelled")
		cw.broadcastProgressWithGen(myGen, "cancelled", "clustering", len(stats), 0, int64(elapsedMs), "")
		slog.Info("cluster worker: cancelled during clustering")
		return
	}

	elapsedMs = int(time.Since(start).Milliseconds())
	_ = service.SaveClusterMeta("computing", "", len(stats), len(clusters), elapsedMs, "clustering")
	cw.broadcastProgressWithGen(myGen, "computing", "clustering", len(stats), len(clusters), int64(elapsedMs), "")

	// Phase 3: saving
	cacheEntries := make([]service.ClusterCacheEntry, len(clusters))
	for i, c := range clusters {
		variantsJSON, err := json.Marshal(c.Variants)
		if err != nil {
			slog.Error("cluster worker: failed to marshal variants", slog.String("err", err.Error()))
			variantsJSON = []byte("[]")
		}
		cacheEntries[i] = service.ClusterCacheEntry{
			Representative:      c.Representative,
			Variants:            string(variantsJSON),
			TotalCount:          c.TotalCount,
			RepresentativeCount: c.RepresentativeCount,
			SortOrder:           i,
		}
	}
	if err := service.SaveClusterCache(cacheEntries, mode); err != nil {
		elapsedMs = int(time.Since(start).Milliseconds())
		_ = service.SaveClusterMetaError("error", "saving", err.Error())
		cw.broadcastProgressWithGen(myGen, "error", "saving", len(stats), len(clusters), int64(elapsedMs), "")
		slog.Error("cluster worker: saving failed", slog.String("err", err.Error()))
		return
	}

	// Update meta with final counts
	elapsedMs = int(time.Since(start).Milliseconds())
	_ = service.SaveClusterMeta("done", mode, len(stats), len(clusters), elapsedMs, "saving")
	cw.broadcastProgressWithGen(myGen, "done", "saving", len(stats), len(clusters), int64(elapsedMs), mode)
	slog.Info("cluster worker: computation complete",
		slog.String("mode", mode),
		slog.Int("msg_count", len(stats)),
		slog.Int("cluster_count", len(clusters)),
		slog.Int("elapsed_ms", elapsedMs),
	)
}

// broadcastProgressWithGen broadcasts a cluster_progress event only if
// the goroutine's generation matches the current worker generation.
// Stale events from a cancelled/stopped goroutine (whose generation was bumped)
// are silently dropped, preventing them from confusing the frontend.
func (cw *ClusterWorker) broadcastProgressWithGen(gen uint64, status, phase string, msgCount, clusterCount int, elapsedMs int64, mode string, progressPct ...int) {
	cw.mu.Lock()
	currentGen := cw.generation
	hub := cw.hub
	cw.mu.Unlock()

	// Drop stale events from goroutines that are no longer the current generation.
	if gen != currentGen {
		return
	}

	if hub == nil {
		return
	}
	mgr := hub.Manager()
	if mgr == nil {
		return
	}
	pct := 0
	if len(progressPct) > 0 {
		pct = progressPct[0]
	}
	mgr.BroadcastEvent(ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		ID:    ws.GenerateEventID(),
		Event: "cluster_progress",
		Data: ClusterProgress{
			Status:       status,
			Phase:        phase,
			MsgCount:     msgCount,
			ClusterCount: clusterCount,
			ElapsedMs:    elapsedMs,
			Mode:         mode,
			ProgressPct:  pct,
		},
	})
}
