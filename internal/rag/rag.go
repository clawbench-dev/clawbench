package rag

import (
	"log/slog"
	"path/filepath"
	"sync/atomic"

	"clawbench/internal/model"

	// Blank import registers the sqlite-vec virtual table extension so vec0
	// vector search is available on every RAG store connection.
	_ "modernc.org/sqlite/vec"
)

// Global state
var (
	GlobalStore    *Store
	GlobalEmbedder *EmbeddingClient

	globalIndexer *Indexer
	globalCleanup *CleanupWorker
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
	dbPath := filepath.Join(model.BinDir, ".clawbench", "ClawBench.db")
	slog.Info("rag: opening SQLite store", slog.String("path", dbPath))

	// Open SQLite store (uses the same database file as the main app)
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		return err
	}
	GlobalStore = store

	// Initialize embedding client
	if cfg.BaseURL != "" && cfg.Model != "" {
		GlobalEmbedder = NewEmbeddingClient(cfg.BaseURL, cfg.Model, cfg.APIKey)
		slog.Info("rag: embedding client initialized", slog.String("model", cfg.Model), slog.String("url", cfg.BaseURL))
	}

	return nil
}

// StartIndexer starts the background indexing worker.
func StartIndexer(cfg model.RAGConfig) {
	if GlobalStore == nil {
		return
	}
	globalIndexer = NewIndexer(GlobalStore, GlobalEmbedder, cfg)
	globalIndexer.Start()
}

// StartCleanupWorker starts the background cleanup worker.
func StartCleanupWorker(cfg model.RAGConfig) {
	if GlobalStore == nil {
		return
	}
	globalCleanup = NewCleanupWorker(GlobalStore, cfg)
	globalCleanup.Start()
}

// Shutdown closes the RAG store, indexer, and cleanup worker.
func Shutdown() {
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
}
