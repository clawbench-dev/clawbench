package kb

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/rag"
)

// Indexer extracts pending documents and backfills their vectors. One worker is
// enough: extraction is local I/O and embedding calls are already batched, and
// a single worker keeps the document ordering deterministic.
type Indexer struct {
	store    *Store
	embedder *rag.EmbeddingClient
	cfg      model.KBConfig
	ragCfg   model.RAGConfig

	wakeCh   chan struct{}
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once

	lastEmbedFail time.Time
}

// NewIndexer creates a worker bound to one store and embedder.
func NewIndexer(store *Store, embedder *rag.EmbeddingClient, cfg model.KBConfig, ragCfg model.RAGConfig) *Indexer {
	return &Indexer{
		store:    store,
		embedder: embedder,
		cfg:      cfg,
		ragCfg:   ragCfg,
		wakeCh:   make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

func (idx *Indexer) Start() {
	go idx.run()
}

func (idx *Indexer) Stop() {
	idx.stopOnce.Do(func() { close(idx.stopCh) })
	<-idx.doneCh
}

// Trigger wakes the worker without blocking. Multiple triggers collapse into one.
func (idx *Indexer) Trigger() {
	select {
	case idx.wakeCh <- struct{}{}:
	default:
	}
}

func (idx *Indexer) run() {
	defer close(idx.doneCh)

	interval := 2 * time.Second
	if d, err := time.ParseDuration(idx.cfg.PollInterval); err == nil && d > 0 {
		interval = d
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-idx.stopCh:
			return
		case <-ticker.C:
		case <-idx.wakeCh:
		}
		idx.drain()
	}
}

// drain keeps working until neither extraction nor embedding has anything left.
func (idx *Indexer) drain() {
	for {
		progressed := false
		if idx.processDocument() {
			progressed = true
		}
		if idx.processEmbeddings() {
			progressed = true
		}
		if !progressed {
			return
		}
		select {
		case <-idx.stopCh:
			return
		default:
		}
	}
}

// processDocument handles one queued document. It returns false when the queue
// is empty.
func (idx *Indexer) processDocument() bool {
	docs, err := idx.store.PendingDocuments(1)
	if err != nil {
		slog.Warn("kb: list pending documents failed", slog.String("err", err.Error()))
		return false
	}
	if len(docs) == 0 {
		return false
	}
	doc := docs[0]

	if statusErr := idx.store.SetDocumentStatus(doc.ID, StatusExtracting, "", ""); statusErr != nil {
		slog.Warn("kb: mark extracting failed", slog.Int64("doc", doc.ID), slog.String("err", statusErr.Error()))
		return false
	}

	maxTextBytes := int64(idx.cfg.MaxTextMB) << 20
	blocks, warning, err := ExtractFile(doc.StoredPath, doc.Ext, maxTextBytes)
	if err != nil {
		slog.Warn("kb: extract failed",
			slog.Int64("doc", doc.ID), slog.String("title", doc.Title), slog.String("err", err.Error()))
		_ = idx.store.SetDocumentStatus(doc.ID, StatusFailed, err.Error(), "")
		return true
	}

	col, err := idx.store.GetCollection(doc.CollectionID)
	if err != nil {
		_ = idx.store.SetDocumentStatus(doc.ID, StatusFailed, err.Error(), warning)
		return true
	}

	chunks := buildChunks(blocks, idx.ragCfg.ChunkSize, idx.ragCfg.ChunkOverlap)
	if len(chunks) == 0 {
		_ = idx.store.SetDocumentStatus(doc.ID, StatusFailed, ErrNoText.Error(), warning)
		return true
	}

	stored := make([]Chunk, 0, len(chunks))
	for _, c := range chunks {
		stored = append(stored, Chunk{
			DocumentID:         doc.ID,
			CollectionID:       doc.CollectionID,
			ProjectID:          col.ProjectID,
			ChunkIndex:         c.Index,
			ChunkText:          c.Text,
			ChunkTextSegmented: c.Segmented,
			Location:           c.Location,
			TokenCount:         c.TokenCount,
		})
	}
	if err := idx.store.ReplaceChunks(doc.ID, stored, warning); err != nil {
		slog.Error("kb: store chunks failed",
			slog.Int64("doc", doc.ID), slog.String("title", doc.Title), slog.String("err", err.Error()))
		_ = idx.store.SetDocumentStatus(doc.ID, StatusFailed, err.Error(), warning)
		return true
	}
	slog.Info("kb: document indexed",
		slog.Int64("doc", doc.ID), slog.String("title", doc.Title), slog.Int("chunks", len(stored)))
	return true
}

// processEmbeddings embeds one batch of pending chunks. It returns false when
// there is nothing to do or the embedder is currently backing off.
func (idx *Indexer) processEmbeddings() bool {
	if idx.embedder == nil {
		return false
	}
	if !idx.lastEmbedFail.IsZero() && time.Since(idx.lastEmbedFail) < 30*time.Second {
		return false
	}
	chunks, err := idx.store.PendingEmbeddingChunks(idx.cfg.EmbedBatchSize)
	if err != nil {
		slog.Warn("kb: list pending chunks failed", slog.String("err", err.Error()))
		return false
	}
	if len(chunks) == 0 {
		return false
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.ChunkText
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	vectors, err := idx.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		slog.Warn("kb: embedding batch failed", slog.Int("count", len(texts)), slog.String("err", err.Error()))
		idx.lastEmbedFail = time.Now()
		return false
	}
	if len(vectors) != len(chunks) {
		slog.Warn("kb: embedding count mismatch",
			slog.Int("chunks", len(chunks)), slog.Int("vectors", len(vectors)))
		idx.lastEmbedFail = time.Now()
		return false
	}

	entries := make([]ChunkEmbedding, 0, len(chunks))
	for i, c := range chunks {
		if len(vectors[i]) == 0 {
			continue
		}
		entries = append(entries, ChunkEmbedding{
			ChunkID:      c.ID,
			DocumentID:   c.DocumentID,
			CollectionID: c.CollectionID,
			ProjectID:    c.ProjectID,
			Vector:       vectors[i],
		})
	}
	if len(entries) == 0 {
		return false
	}
	if err := idx.store.SaveEmbeddings(entries); err != nil {
		slog.Warn("kb: save embeddings failed", slog.String("err", err.Error()))
		idx.lastEmbedFail = time.Now()
		return false
	}
	idx.lastEmbedFail = time.Time{}
	return true
}
