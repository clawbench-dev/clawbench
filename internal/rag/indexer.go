package rag

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// Indexer polls for unindexed chat messages and generates embeddings.
// When the embedding API is unavailable, it indexes text-only (for FTS search).
// When the embedding API becomes available, it backfills embeddings for pending chunks.
type Indexer struct {
	store           *Store
	embedder        *EmbeddingClient
	cfg             model.RAGConfig
	stopCh          chan struct{}
	doneCh          chan struct{}
	mu              sync.Mutex
	running         bool
	modelWarn       bool
	embedderHealthy bool
	dimensionSynced bool
	batchCancel     context.CancelFunc
}

// NewIndexer creates a new RAG indexer.
func NewIndexer(store *Store, embedder *EmbeddingClient, cfg model.RAGConfig) *Indexer {
	return &Indexer{
		store:    store,
		embedder: embedder,
		cfg:      cfg,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start begins the indexer loop in a goroutine.
func (idx *Indexer) Start() {
	idx.mu.Lock()
	if idx.running {
		idx.mu.Unlock()
		return
	}
	idx.running = true
	idx.mu.Unlock()

	go idx.run()
	slog.Info(
		"rag indexer started",
		slog.String("poll_interval", idx.cfg.PollInterval),
		slog.Int("batch_size", idx.cfg.BatchSize),
		slog.Int("chunk_size", idx.cfg.ChunkSize),
	)
}

// Stop signals the indexer to stop and waits for it to finish.
func (idx *Indexer) Stop() {
	idx.mu.Lock()
	if !idx.running {
		idx.mu.Unlock()
		return
	}
	idx.mu.Unlock()

	if idx.batchCancel != nil {
		idx.batchCancel()
	}

	close(idx.stopCh)

	select {
	case <-idx.doneCh:
	case <-time.After(5 * time.Second):
		slog.Warn("rag: indexer did not stop within timeout, continuing shutdown")
	}

	idx.mu.Lock()
	idx.running = false
	idx.mu.Unlock()

	slog.Info("rag indexer stopped")
}

// run is the main indexer loop.
// Uses continuous mode: after each batch, if more work remains, immediately
// processes the next batch instead of waiting for the poll interval.
func (idx *Indexer) run() {
	defer close(idx.doneCh)

	pollInterval, err := time.ParseDuration(idx.cfg.PollInterval)
	if err != nil {
		slog.Error("invalid rag poll_interval, using 10s", slog.String("value", idx.cfg.PollInterval), slog.String("err", err.Error()))
		pollInterval = 10 * time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Run first indexing immediately
	idx.indexBatch()

	for {
		select {
		case <-idx.stopCh:
			return
		case <-ticker.C:
			select {
			case <-idx.stopCh:
				return
			default:
			}
			// After each batch, check if more work remains.
			// If so, loop immediately instead of waiting for the next tick.
			for {
				hasMore := idx.indexBatch()
				if !hasMore {
					break
				}
				// Check stop signal between continuous batches
				select {
				case <-idx.stopCh:
					return
				default:
				}
			}
		}
	}
}

// embedSubBatchSize is the maximum number of texts per embedding API call.
// Local models (e.g. Ollama) can be slow; smaller sub-batches avoid timeouts
// and allow partial progress to be saved.
const embedSubBatchSize = 20

// embedInSubBatches calls EmbedBatch in smaller sub-batches to avoid timeouts.
// Returns all embeddings in the same order as input texts. If a sub-batch fails,
// its embeddings are nil (caller handles gracefully).
func (idx *Indexer) embedInSubBatches(ctx context.Context, texts []string) [][]float64 {
	results := make([][]float64, len(texts))
	for i := 0; i < len(texts); i += embedSubBatchSize {
		end := i + embedSubBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		subBatch := texts[i:end]
		embeddings, err := idx.embedder.EmbedBatch(ctx, subBatch)
		if err != nil {
			slog.Warn("rag: sub-batch embedding failed",
				slog.Int("from", i), slog.Int("to", end),
				slog.String("err", err.Error()))
			// nil entries left in results — caller treats as no embedding
			continue
		}
		for j, emb := range embeddings {
			results[i+j] = emb
		}
		// Check context cancellation between sub-batches
		if ctx.Err() != nil {
			break
		}
	}
	return results
}

// indexBatch processes one batch of unindexed messages and backfills embeddings.
// Returns true if more unindexed messages may remain (caller should loop).
func (idx *Indexer) indexBatch() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	idx.mu.Lock()
	idx.batchCancel = cancel
	idx.mu.Unlock()

	// Check embedding API health
	idx.checkEmbedderHealth(ctx)

	// Phase 1: Index new messages from SQLite
	hasMore := idx.indexNewMessages(ctx)

	if ctx.Err() != nil {
		return false
	}

	// Phase 2: Backfill embeddings for chunks that were indexed without them
	if idx.embedderHealthy {
		idx.backfillEmbeddings(ctx)
	}

	return hasMore
}

// checkEmbedderHealth checks embedding API availability and updates the healthy flag.
func (idx *Indexer) checkEmbedderHealth(ctx context.Context) {
	if idx.embedder == nil {
		idx.embedderHealthy = false
		SetEmbedderHealthy(false)
		return
	}

	reachable, modelAvailable, err := idx.embedder.IsHealthy(ctx)
	if err != nil {
		slog.Debug("rag: embedding API health check error", slog.String("err", err.Error()))
		idx.embedderHealthy = false
		SetEmbedderHealthy(false)
		return
	}
	if !reachable {
		if idx.embedderHealthy {
			slog.Info("rag: embedding API became unreachable")
		}
		idx.embedderHealthy = false
		SetEmbedderHealthy(false)
		return
	}
	if !modelAvailable {
		if !idx.modelWarn {
			slog.Warn(
				"rag: embedding API reachable but model not available",
				slog.String("model", idx.cfg.Model),
			)
			idx.modelWarn = true
		}
		idx.embedderHealthy = false
		SetEmbedderHealthy(false)
		return
	}

	if !idx.embedderHealthy {
		slog.Info("rag: embedding API became healthy, will backfill embeddings")
	}

	// Sync dimension from embedder to store (one-time)
	if !idx.dimensionSynced {
		if dim := idx.embedder.Dim(); dim > 0 {
			// Check for dimension mismatch against existing data
			existingDim, mismatch, _ := idx.store.CheckDimensionMismatch()
			if mismatch {
				slog.Warn("rag: embedding dimension mismatch, resetting store", slog.Int("existing", existingDim), slog.Int("new", dim))
				if err := idx.store.ResetForDimensionMismatch(dim); err != nil {
					slog.Error("rag: failed to reset store for dimension mismatch", slog.String("err", err.Error()))
				}
			} else if idx.store.SetEmbeddingDim(dim) {
				slog.Info("rag: synced embedding dimension from embedder", slog.Int("dim", dim))
			}
			idx.dimensionSynced = true
		}
	}

	idx.embedderHealthy = true
	idx.modelWarn = false
	SetEmbedderHealthy(true)
}

// indexNewMessages indexes new (unindexed) messages from SQLite.
// Optimization: all messages in the batch are chunked first, then embeddings are
// requested in a single EmbedBatch call, and all chunks are inserted together.
// Returns true if more unindexed messages may remain.
func (idx *Indexer) indexNewMessages(ctx context.Context) bool {
	messages, err := service.GetUnindexedMessages(idx.cfg.BatchSize)
	if err != nil {
		slog.Error("rag: failed to fetch unindexed messages", slog.String("err", err.Error()))
		return false
	}
	if len(messages) == 0 {
		return false
	}

	slog.Info(
		"rag: indexing batch",
		slog.Int("batch_size", len(messages)),
		slog.Bool("embedder_healthy", idx.embedderHealthy),
	)

	batchStart := time.Now()

	// Phase 1: Extract text and chunk all messages
	type msgChunks struct {
		msg    service.UnindexedMessage
		text   string
		chunks []Chunk
	}
	allMsgChunks := make([]msgChunks, 0, len(messages))
	var allTexts []string // flat list of all chunk texts for batch embedding

	for _, msg := range messages {
		text := ExtractTextFromContent(msg.Content, msg.Role)
		if text == "" {
			allMsgChunks = append(allMsgChunks, msgChunks{msg: msg, text: ""})
			continue
		}

		textChunks := ChunkText(text, idx.cfg.ChunkSize, idx.cfg.ChunkOverlap)
		if len(textChunks) == 0 {
			allMsgChunks = append(allMsgChunks, msgChunks{msg: msg, text: text})
			continue
		}

		if len(textChunks) > 50 {
			slog.Warn("rag: message produced too many chunks, truncating",
				slog.Int64("message_id", msg.ID),
				slog.Int("original", len(textChunks)),
			)
			textChunks = textChunks[:50]
		}

		chunks := make([]Chunk, len(textChunks))
		for i, tc := range textChunks {
			chunks[i] = Chunk{
				SessionID:          msg.SessionID,
				MessageID:          msg.ID,
				ChunkText:          tc.Text,
				ChunkTextSegmented: SegmentText(tc.Text),
				ChunkIndex:         tc.Index,
				TokenCount:         tc.TokenCount,
				ProjectPath:        msg.ProjectPath,
				Backend:            msg.Backend,
				Role:               msg.Role,
				CreatedAt:          msg.CreatedAt,
			}
			allTexts = append(allTexts, tc.Text)
		}

		allMsgChunks = append(allMsgChunks, msgChunks{msg: msg, text: text, chunks: chunks})
	}

	// Phase 2: Batch embedding in sub-batches to avoid timeouts
	var allEmbeddings [][]float64
	if idx.embedderHealthy && len(allTexts) > 0 {
		allEmbeddings = idx.embedInSubBatches(ctx, allTexts)
		// Check if any embeddings were actually returned
		hasAny := false
		for _, e := range allEmbeddings {
			if e != nil {
				hasAny = true
				break
			}
		}
		if !hasAny {
			allEmbeddings = nil
		}
	}

	// Phase 3: Assign embeddings to chunks
	textIdx := 0 // cursor into allTexts/allEmbeddings
	var allChunks []Chunk
	var indexedIDs []int64
	skipped := 0

	for i := range allMsgChunks {
		mc := &allMsgChunks[i]
		if mc.text == "" || len(mc.chunks) == 0 {
			skipped++
			indexedIDs = append(indexedIDs, mc.msg.ID)
			continue
		}

		// Assign embeddings from the flat batch result
		if allEmbeddings != nil {
			for j := range mc.chunks {
				if textIdx < len(allEmbeddings) && allEmbeddings[textIdx] != nil {
					mc.chunks[j].Embedding = allEmbeddings[textIdx]
					mc.chunks[j].HasEmbedding = true
				}
				textIdx++
			}
		} else {
			textIdx += len(mc.chunks)
		}

		allChunks = append(allChunks, mc.chunks...)
		indexedIDs = append(indexedIDs, mc.msg.ID)
	}

	// Phase 4: Insert all chunks in a single transaction
	if len(allChunks) > 0 {
		if err := idx.store.InsertChunks(allChunks); err != nil {
			slog.Error("rag: failed to insert chunks batch", slog.String("err", err.Error()))
		}
	}

	// Phase 5: Batch mark all indexed messages
	if len(indexedIDs) > 0 {
		if err := service.MarkMessagesIndexed(indexedIDs); err != nil {
			slog.Error("rag: failed to batch mark messages indexed", slog.String("err", err.Error()))
		}
	}

	slog.Info(
		"rag: batch complete",
		slog.Int("messages", len(indexedIDs)),
		slog.Int("chunks", len(allChunks)),
		slog.Int("skipped", skipped),
		slog.Duration("elapsed", time.Since(batchStart)),
	)

	// More work remains if we fetched a full batch
	return len(messages) >= idx.cfg.BatchSize
}

// backfillEmbeddings generates embeddings for chunks that were stored without them.
// Processes in sub-batches: each sub-batch is embedded and committed independently,
// so partial progress is preserved even if a later sub-batch times out.
func (idx *Indexer) backfillEmbeddings(ctx context.Context) {
	pending, err := idx.store.PendingEmbeddingCount()
	if err != nil {
		slog.Debug("rag: failed to check pending embeddings", slog.String("err", err.Error()))
		return
	}
	if pending == 0 {
		return
	}

	slog.Info("rag: backfilling embeddings", slog.Int("pending", pending))

	batchSize := idx.cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 50
	}

	maxBackfill := batchSize
	if maxBackfill > 200 {
		maxBackfill = 200
	}

	pendingChunks, err := idx.store.GetPendingEmbeddings(maxBackfill)
	if err != nil {
		slog.Error("rag: failed to fetch pending embeddings", slog.String("err", err.Error()))
		return
	}
	if len(pendingChunks) == 0 {
		return
	}

	totalBackfilled := 0
	// Process in embedSubBatchSize chunks, committing each to DB
	for i := 0; i < len(pendingChunks); i += embedSubBatchSize {
		if ctx.Err() != nil {
			break
		}
		end := i + embedSubBatchSize
		if end > len(pendingChunks) {
			end = len(pendingChunks)
		}
		subBatch := pendingChunks[i:end]

		texts := make([]string, len(subBatch))
		for j, p := range subBatch {
			texts[j] = p.ChunkText
		}

		embeddings, err := idx.embedder.EmbedBatch(ctx, texts)
		if err != nil {
			slog.Warn("rag: backfill sub-batch embedding failed",
				slog.Int("from", i), slog.Int("to", end),
				slog.String("err", err.Error()))
			continue
		}

		backfilled, err := idx.store.BatchUpdateEmbeddings(subBatch, embeddings)
		if err != nil {
			slog.Error("rag: batch update embeddings failed", slog.String("err", err.Error()))
			continue
		}
		totalBackfilled += backfilled
	}

	slog.Info(
		"rag: backfill pass complete",
		slog.Int("backfilled", totalBackfilled),
		slog.Int("total_pending", pending),
	)
}
