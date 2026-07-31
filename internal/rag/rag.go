package rag

import (
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"

	"clawbench/internal/model"
	"clawbench/internal/ws"

	// Blank import registers the sqlite-vec virtual table extension so vec0
	// vector search is available on every RAG store connection.
	_ "modernc.org/sqlite/vec"
)

// mu protects global state mutations during Reconfigure/Shutdown.
// Search paths read GlobalStore/GlobalEmbedder which are pointer-sized
// and effectively atomic on all target platforms (64/32-bit).
// The lock prevents Reconfigure from swapping pointers mid-search-decision.
var mu sync.Mutex

// Global state
var (
	GlobalStore    *Store
	GlobalEmbedder *EmbeddingClient

	globalIndexer      *Indexer
	globalCleanup      *CleanupWorker
	globalClusterWorker *ClusterWorker
	GlobalClusterWorker *ClusterWorker // exposed for handler access
)

var embedderHealthyFlag atomic.Bool

// EmbedderHealthy returns whether the embedding API was last known to be healthy.
func EmbedderHealthy() bool {
	return embedderHealthyFlag.Load()
}

// SetEmbedderHealthy updates the cached embedder health state.
func SetEmbedderHealthy(healthy bool) {
	embedderHealthyFlag.Store(healthy)
}

// Init initializes the RAG subsystem with a SQLite-backed store.
func Init(cfg model.RAGConfig) error {
	// Initialize segmenter
	if err := InitSegmenter(); err != nil {
		slog.Warn("rag: gse segmenter not available, Chinese segmentation disabled", slog.String("err", err.Error()))
	}

	// Determine database path
	dbPath := filepath.Join(model.DataDir, "ClawBench.db")
	slog.Info("rag: opening SQLite store", slog.String("path", dbPath))

	// Open SQLite store (uses the same database file as the main app)
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		return err
	}

	mu.Lock()
	GlobalStore = store

	// Initialize embedding client (only when enabled)
	if cfg.VectorEnabled && cfg.BaseURL != "" && cfg.Model != "" {
		GlobalEmbedder = NewEmbeddingClient(cfg.BaseURL, cfg.Model, cfg.APIKey)
	}
	mu.Unlock()

	if GlobalEmbedder != nil {
		slog.Info("rag: embedding client initialized", slog.String("model", cfg.Model), slog.String("url", cfg.BaseURL))
	}

	return nil
}

// StartIndexer starts the background indexing worker.
func StartIndexer(cfg model.RAGConfig) {
	if GlobalStore == nil {
		return
	}
	mu.Lock()
	globalIndexer = NewIndexer(GlobalStore, GlobalEmbedder, cfg)
	globalIndexer.Start()
	mu.Unlock()
}

// StartCleanupWorker starts the background cleanup worker.
func StartCleanupWorker(cfg model.RAGConfig) {
	if GlobalStore == nil {
		return
	}
	mu.Lock()
	globalCleanup = NewCleanupWorker(GlobalStore, cfg)
	globalCleanup.Start()
	mu.Unlock()
}

// StartClusterWorker initializes the on-demand cluster worker (no cron).
// The worker only computes when explicitly triggered via ComputeOnce().
func StartClusterWorker(hub *ws.StreamHub) {
	mu.Lock()
	globalClusterWorker = NewClusterWorker(hub)
	GlobalClusterWorker = globalClusterWorker
	mu.Unlock()
	slog.Info("cluster worker initialized (on-demand, no cron)")
}

// StopClusterWorker cancels any running cluster computation and clears the worker.
func StopClusterWorker() {
	mu.Lock()
	if globalClusterWorker != nil {
		globalClusterWorker.Stop()
	}
	globalClusterWorker = nil
	GlobalClusterWorker = nil
	mu.Unlock()
}

// Shutdown closes the RAG store, indexer, cleanup worker, and cluster worker.
func Shutdown() {
	mu.Lock()

	if globalClusterWorker != nil {
		globalClusterWorker.Stop()
	}
	globalClusterWorker = nil
	GlobalClusterWorker = nil

	if globalIndexer != nil {
		globalIndexer.Stop()
		globalIndexer = nil
	}
	if globalCleanup != nil {
		globalCleanup.Stop()
		globalCleanup = nil
	}
	if GlobalStore != nil {
		_ = GlobalStore.Close()
		GlobalStore = nil
	}
	GlobalEmbedder = nil
	mu.Unlock()
}

// Reconfigure applies new RAG config at runtime (hot-reload).
// It recreates the embedding client (pointer swap, no field mutation)
// and restarts the indexer and cleanup worker with the new config.
// When cfg.VectorEnabled is false, vector embedding is disabled but FTS indexing continues.
func Reconfigure(cfg model.RAGConfig) {
	mu.Lock()
	defer mu.Unlock()

	// Stop indexer and cleanup worker regardless (will restart below)
	if globalIndexer != nil {
		globalIndexer.Stop()
		globalIndexer = nil
	}
	if globalCleanup != nil {
		globalCleanup.Stop()
		globalCleanup = nil
	}

	if !cfg.VectorEnabled {
		// Disable vector embedding only — FTS indexing continues
		GlobalEmbedder = nil
		embedderHealthyFlag.Store(false)

		// Restart indexer without embedder (FTS-only mode)
		if GlobalStore != nil {
			globalIndexer = NewIndexer(GlobalStore, nil, cfg)
			globalIndexer.Start()
			globalCleanup = NewCleanupWorker(GlobalStore, cfg)
			globalCleanup.Start()
		}
		slog.Info("hot-reload: RAG vector embedding disabled, FTS-only mode")
		return
	}

	// Create a new EmbeddingClient instead of mutating the existing one.
	// This eliminates data races: in-flight requests on the old client
	// complete on their own http.Client; the pointer swap is atomic.
	if cfg.BaseURL != "" && cfg.Model != "" {
		GlobalEmbedder = NewEmbeddingClient(cfg.BaseURL, cfg.Model, cfg.APIKey)
		embedderHealthyFlag.Store(false)
	} else {
		GlobalEmbedder = nil
		embedderHealthyFlag.Store(false)
	}

	// Restart indexer with new config
	if GlobalStore != nil {
		globalIndexer = NewIndexer(GlobalStore, GlobalEmbedder, cfg)
		globalIndexer.Start()
		globalCleanup = NewCleanupWorker(GlobalStore, cfg)
		globalCleanup.Start()
	}

	slog.Info("hot-reload: RAG reconfigured", slog.String("base_url", cfg.BaseURL), slog.String("model", cfg.Model), slog.Bool("has_api_key", cfg.APIKey != ""))
}
