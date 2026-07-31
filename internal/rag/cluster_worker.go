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
	Status       string `json:"status"`       // "idle" | "computing" | "done" | "error"
	Phase        string `json:"phase"`        // "extracting" | "clustering" | "saving"
	MsgCount     int    `json:"msg_count"`
	ClusterCount int    `json:"cluster_count"`
	ElapsedMs    int64  `json:"elapsed_ms"`
	Mode         string `json:"mode"`         // available only when done
	Error        string `json:"error,omitempty"`
}

// ClusterWorker manages on-demand cluster computation with progress tracking.
// It runs a goroutine triggered by the user (not a cron job) and broadcasts
// progress updates via the WebSocket StreamHub.
type ClusterWorker struct {
	mu        sync.Mutex
	running   bool
	cancelFn  context.CancelFunc
	generation uint64 // incremented on each ComputeOnce, goroutine defer only clears if still same gen
	hub       *ws.StreamHub
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
	service.SaveClusterMeta("computing", "", 0, 0, 0)

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
	mode, _, progress, phase, msgCount, clusterCount, elapsedMs, errMsg := service.GetClusterMeta()
	return ClusterProgress{
		Status:       progress,
		Phase:        phase,
		MsgCount:     msgCount,
		ClusterCount: clusterCount,
		ElapsedMs:    int64(elapsedMs),
		Mode:         mode,
		Error:        errMsg,
	}
}

// Stop cancels any running computation and marks the worker as not running.
func (cw *ClusterWorker) Stop() {
	cw.mu.Lock()
	if cw.cancelFn != nil {
		cw.cancelFn()
	}
	cw.running = false
	cw.cancelFn = nil
	cw.generation++ // bump generation so stale goroutine defer won't clear new state
	cw.mu.Unlock()
}

// compute executes the three-phase cluster computation:
// 1. extracting: fetch user message stats
// 2. clustering: cluster messages using best available method
// 3. saving: save cluster cache and final meta
func (cw *ClusterWorker) compute(ctx context.Context, myGen uint64) {
	start := time.Now()

	defer func() {
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
	stats, err := service.GetUserMessageStats(5000)
	if err != nil {
		elapsedMs := int(time.Since(start).Milliseconds())
		service.SaveClusterMetaError("error", "extracting", err.Error())
		cw.broadcastProgress("error", "extracting", 0, 0, int64(elapsedMs), "")
		slog.Error("cluster worker: extracting failed", slog.String("err", err.Error()))
		return
	}
	elapsedMs := int(time.Since(start).Milliseconds())
	service.SaveClusterMeta("computing", "", len(stats), 0, elapsedMs)
	cw.broadcastProgress("computing", "extracting", len(stats), 0, int64(elapsedMs), "")

	// Check for context cancellation
	if ctx.Err() != nil {
		service.SaveClusterMetaError("error", "extracting", "cancelled")
		cw.broadcastProgress("error", "extracting", len(stats), 0, int64(elapsedMs), "")
		slog.Info("cluster worker: cancelled during extracting")
		return
	}

	// Phase 2: clustering
	ragStats := make([]MessageStat, len(stats))
	for i, s := range stats {
		ragStats[i] = MessageStat{Text: s.Text, Count: s.Count}
	}
	clusters, mode := ClusterMessagesWithEmbeddings(ctx, ragStats, GlobalEmbedder, 0.65)
	elapsedMs = int(time.Since(start).Milliseconds())
	service.SaveClusterMeta("computing", "", len(stats), len(clusters), elapsedMs)
	cw.broadcastProgress("computing", "clustering", len(stats), len(clusters), int64(elapsedMs), "")

	if ctx.Err() != nil {
		service.SaveClusterMetaError("error", "clustering", "cancelled")
		cw.broadcastProgress("error", "clustering", len(stats), len(clusters), int64(elapsedMs), "")
		slog.Info("cluster worker: cancelled during clustering")
		return
	}

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
			Variants:             string(variantsJSON),
			TotalCount:           c.TotalCount,
			RepresentativeCount:  c.RepresentativeCount,
			SortOrder:            i,
		}
	}
	if err := service.SaveClusterCache(cacheEntries, mode); err != nil {
		elapsedMs = int(time.Since(start).Milliseconds())
		service.SaveClusterMetaError("error", "saving", err.Error())
		cw.broadcastProgress("error", "saving", len(stats), len(clusters), int64(elapsedMs), "")
		slog.Error("cluster worker: saving failed", slog.String("err", err.Error()))
		return
	}

	// Update meta with final counts (SaveClusterCache writes basic meta,
	// but we need the proper msg_count and elapsed_ms)
	elapsedMs = int(time.Since(start).Milliseconds())
	service.SaveClusterMeta("done", mode, len(stats), len(clusters), elapsedMs)
	cw.broadcastProgress("done", "saving", len(stats), len(clusters), int64(elapsedMs), mode)
	slog.Info("cluster worker: computation complete",
		slog.String("mode", mode),
		slog.Int("msg_count", len(stats)),
		slog.Int("cluster_count", len(clusters)),
		slog.Int("elapsed_ms", elapsedMs),
	)
}

// broadcastProgress sends a cluster_progress event to all connected WS clients.
// If hub is nil, the broadcast is skipped.
func (cw *ClusterWorker) broadcastProgress(status, phase string, msgCount, clusterCount int, elapsedMs int64, mode string) {
	if cw.hub == nil {
		return
	}
	mgr := cw.hub.Manager()
	if mgr == nil {
		return
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
		},
	})
}


