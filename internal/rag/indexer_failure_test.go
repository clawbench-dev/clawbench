package rag

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupIndexerServiceDB installs a minimal chat_history/chat_sessions schema into
// the service package's DB globals so GetUnindexedMessages/MarkMessagesIndexed
// work in rag-package tests.
func setupIndexerServiceDB(t *testing.T) *sql.DB {
	t.Helper()

	testDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	testDB.SetMaxOpenConns(1)
	_, err = testDB.Exec(`
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			archived INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(project_path, backend, id)
		);
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
			content TEXT NOT NULL,
			files TEXT,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	require.NoError(t, err)

	cleanup := service.SetDBForTest(testDB, testDB)
	t.Cleanup(func() {
		cleanup()
		_ = testDB.Close()
	})
	return testDB
}

// TestIndexer_InsertFailure_LeavesMessagesUnindexed is the regression test for the
// blocker where a failed chunk insert still marked the batch's messages as indexed.
//
// Because GetUnindexedMessages filters on indexed = 0, marking them would drop the
// messages from the queue permanently — they would never appear in FTS or vector
// search. This test drives indexNewMessages with a store whose vec table has a
// different dimension than the embeddings, which makes InsertChunks fail.
func TestIndexer_InsertFailure_LeavesMessagesUnindexed(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)

	// Insert two indexable messages.
	_, err := serviceDB.Exec(
		`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming, indexed)
		 VALUES ('/proj', 'user', 'first searchable message', 'sess-1', 'claude', 0, 0),
		        ('/proj', 'user', 'second searchable message', 'sess-1', 'claude', 0, 0)`,
	)
	require.NoError(t, err)

	// Store whose rag_vec is 1024-wide, but the indexer will feed 768-wide vectors.
	store := setupSQLiteStoreWithDim(t) // embDim = 1024
	// rag_vec is created lazily; force it into existence at 1024 dims.
	require.NoError(t, store.ensureVecTable())
	require.True(t, store.vecTableExists())

	// Build an indexer that bypasses the embedder by supplying embeddings directly.
	idx := NewIndexer(store, nil, defaultTestRAGConfig())
	idx.embedderHealthy = true

	messages, err := service.GetUnindexedMessages(50)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	// Chunk them, then hand in 768-dim embeddings (mismatch with the 1024 vec table).
	msgChunks, texts := idx.chunkMessages(messages)
	require.Len(t, texts, 2)

	embeddings := make([][]float64, len(texts))
	for i := range embeddings {
		embeddings[i] = make([]float64, 768) // wrong width on purpose
	}

	allChunks, chunkMsgIDs, skippedIDs := idx.assignEmbeddings(msgChunks, embeddings)
	require.Empty(t, skippedIDs)
	require.Len(t, allChunks, 2)
	require.Len(t, chunkMsgIDs, 2)

	// The insert must fail against the 1024-wide vec table.
	err = store.InsertChunks(allChunks)
	require.Error(t, err, "insert with wrong-width embeddings should fail")

	// Simulate the fixed Phase 4/5 behavior: a failed insert must not mark.
	insertOK := err == nil
	markIDs := append([]int64{}, skippedIDs...)
	if insertOK {
		markIDs = append(markIDs, chunkMsgIDs...)
	}
	require.Empty(t, markIDs, "failed insert must not mark any message as indexed")

	if len(markIDs) > 0 {
		require.NoError(t, service.MarkMessagesIndexed(markIDs))
	}

	// The messages must still be returned by GetUnindexedMessages (retryable).
	stillPending, err := service.GetUnindexedMessages(50)
	require.NoError(t, err)
	assert.Len(t, stillPending, 2, "messages must remain unindexed for retry after insert failure")
}

// TestIndexer_InsertSuccess_MarksMessagesIndexed is the positive counterpart: a
// successful insert must mark every message, otherwise indexing never converges.
func TestIndexer_InsertSuccess_MarksMessagesIndexed(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)

	_, err := serviceDB.Exec(
		`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming, indexed)
		 VALUES ('/proj', 'user', 'a searchable message', 'sess-1', 'claude', 0, 0)`,
	)
	require.NoError(t, err)

	store := setupSQLiteStoreWithDim(t) // 1024-wide vec table
	idx := NewIndexer(store, nil, defaultTestRAGConfig())
	idx.embedderHealthy = true

	messages, err := service.GetUnindexedMessages(50)
	require.NoError(t, err)
	require.Len(t, messages, 1)

	msgChunks, texts := idx.chunkMessages(messages)
	require.Len(t, texts, 1)

	embeddings := [][]float64{makeTestEmbedding()} // 1024 wide — matches the table
	allChunks, chunkMsgIDs, skippedIDs := idx.assignEmbeddings(msgChunks, embeddings)
	require.Empty(t, skippedIDs)

	require.NoError(t, store.InsertChunks(allChunks))

	markIDs := append([]int64{}, skippedIDs...)
	markIDs = append(markIDs, chunkMsgIDs...)
	require.NoError(t, service.MarkMessagesIndexed(markIDs))

	remaining, err := service.GetUnindexedMessages(50)
	require.NoError(t, err)
	assert.Empty(t, remaining, "successfully indexed messages must leave the queue")
}

// TestIndexer_SkippedMessagesMarkedEvenWhenInsertFails verifies that messages with
// no indexable text are still marked even when other chunks in the same batch fail.
// Otherwise an unindexable message would be retried forever.
func TestIndexer_SkippedMessagesMarkedEvenWhenInsertFails(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)

	// Role 'assistant' with a tool-only/empty payload extracts to no text.
	_, err := serviceDB.Exec(
		`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming, indexed)
		 VALUES ('/proj', 'assistant', '{"blocks":[]}', 'sess-1', 'claude', 0, 0),
		        ('/proj', 'user', 'a real searchable message', 'sess-1', 'claude', 0, 0)`,
	)
	require.NoError(t, err)

	store := setupSQLiteStoreWithDim(t)
	idx := NewIndexer(store, nil, defaultTestRAGConfig())
	idx.embedderHealthy = true

	messages, err := service.GetUnindexedMessages(50)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	msgChunks, texts := idx.chunkMessages(messages)
	embeddings := make([][]float64, len(texts))
	for i := range embeddings {
		embeddings[i] = make([]float64, 768) // wrong width → insert fails
	}

	allChunks, chunkMsgIDs, skippedIDs := idx.assignEmbeddings(msgChunks, embeddings)
	require.NotEmpty(t, skippedIDs, "the empty assistant message should be skipped")

	err = store.InsertChunks(allChunks)
	require.Error(t, err)

	insertOK := err == nil
	markIDs := append([]int64{}, skippedIDs...)
	if insertOK {
		markIDs = append(markIDs, chunkMsgIDs...)
	}
	require.NoError(t, service.MarkMessagesIndexed(markIDs))

	remaining, err := service.GetUnindexedMessages(50)
	require.NoError(t, err)
	assert.Len(t, remaining, 1, "only the message whose chunks failed should remain queued")
}

// TestInsertChunks_NoVecTable_DoesNotFlagEmbedded is the regression test for the
// "embedded but no vec row" bug: when rag_vec cannot be created (dimension
// unknown), a chunk carrying an embedding must NOT be marked has_embedding = 1,
// otherwise the backfill pass (which selects has_embedding = 0) never embeds it
// and it stays invisible to vector search forever.
func TestInsertChunks_NoVecTable_DoesNotFlagEmbedded(t *testing.T) {
	store := setupSQLiteStore(t) // embDim = 0 → rag_vec cannot be created
	require.False(t, store.vecTableExists())

	chunk := makeTestChunk(testSession1, 1, 0, "embedded but no vec table")
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	var hasEmb int
	require.NoError(t, store.db.QueryRow(
		"SELECT has_embedding FROM rag_chunks WHERE message_id = 1").Scan(&hasEmb))
	assert.Equal(t, 0, hasEmb,
		"a chunk with no vec row must not claim to be embedded")

	// It must therefore be visible to the backfill pass.
	pending, err := store.PendingEmbeddingCount()
	require.NoError(t, err)
	assert.Equal(t, 1, pending, "chunk without a vec row must be queued for backfill")
}

// TestInsertChunks_WithVecTable_FlagsEmbedded is the positive counterpart.
func TestInsertChunks_WithVecTable_FlagsEmbedded(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	require.NoError(t, store.ensureVecTable())

	chunk := makeTestChunk(testSession1, 1, 0, "embedded with vec table")
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	var hasEmb int
	require.NoError(t, store.db.QueryRow(
		"SELECT has_embedding FROM rag_chunks WHERE message_id = 1").Scan(&hasEmb))
	assert.Equal(t, 1, hasEmb)

	var vecRows int
	require.NoError(t, store.db.QueryRow("SELECT COUNT(*) FROM rag_vec").Scan(&vecRows))
	assert.Equal(t, 1, vecRows, "a flagged chunk must have a vec row")

	pending, err := store.PendingEmbeddingCount()
	require.NoError(t, err)
	assert.Equal(t, 0, pending)
}

// TestInsertChunks_WrongWidthEmbedding_Errors covers the length-validation gap:
// a misconfigured embedder returning the wrong width must be rejected clearly
// rather than poisoning the insert transaction.
func TestInsertChunks_WrongWidthEmbedding_Errors(t *testing.T) {
	store := setupSQLiteStoreWithDim(t) // rag_vec is 1024 wide
	require.NoError(t, store.ensureVecTable())

	chunk := makeTestChunk(testSession1, 1, 0, "wrong width")
	chunk.Embedding = make([]float64, 512) // not 1024
	chunk.HasEmbedding = true

	err := store.InsertChunks([]Chunk{chunk})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dimension mismatch")
	assert.Contains(t, err.Error(), "512")
	assert.Contains(t, err.Error(), "1024")

	// The failed batch must leave nothing behind.
	count, err := store.ChunkCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count, "the whole transaction must roll back")
}

// TestInsertChunks_RetryIsIdempotent is the regression test for duplicate chunks:
// InsertChunks commits in sub-batches, so a partially-committed batch that is
// retried must replace — not duplicate — the chunks that already landed.
func TestInsertChunks_RetryIsIdempotent(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	require.NoError(t, store.ensureVecTable())

	chunk := makeTestChunk(testSession1, 42, 0, "retry me")
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))
	// Simulate the retry of the same batch.
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	var chunks, fts, vec int
	require.NoError(t, store.db.QueryRow(
		"SELECT COUNT(*) FROM rag_chunks WHERE message_id = 42").Scan(&chunks))
	require.NoError(t, store.db.QueryRow("SELECT COUNT(*) FROM rag_chunks_fts").Scan(&fts))
	require.NoError(t, store.db.QueryRow("SELECT COUNT(*) FROM rag_vec").Scan(&vec))

	assert.Equal(t, 1, chunks, "retry must not duplicate the chunk row")
	assert.Equal(t, 1, fts, "retry must not duplicate the FTS entry")
	assert.Equal(t, 1, vec, "retry must not duplicate the vec row")
}

// TestEnsureChunkUniqueness_DeduplicatesExistingRows verifies the migration that
// collapses duplicates left behind by older builds and adds the unique index.
func TestEnsureChunkUniqueness_DeduplicatesExistingRows(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)

	// Drop the index the store creates at init, so we can stage the duplicates
	// that an older build (without the constraint) would have left behind.
	_, err := store.db.Exec(`DROP INDEX IF EXISTS ux_rag_chunks_message_chunk`)
	require.NoError(t, err)

	// Insert duplicates by bypassing the store's dedupe path.
	for range 3 {
		res, err := store.db.Exec(
			`INSERT INTO rag_chunks (session_id, message_id, chunk_text, chunk_text_segmented,
				chunk_index, token_count, has_embedding, embedding_dim, project_path, backend, role, created_at)
			 VALUES ('s1', 7, 'dup', 'dup', 0, 1, 0, 0, '/p', 'claude', 'user', '2025-01-01')`)
		require.NoError(t, err)
		id, _ := res.LastInsertId()
		_, err = store.db.Exec(`INSERT INTO rag_chunks_fts(rowid, chunk_text_segmented) VALUES (?, ?)`, id, "dup")
		require.NoError(t, err)
	}

	require.NoError(t, store.ensureChunkUniqueness())

	var chunks int
	require.NoError(t, store.db.QueryRow(
		"SELECT COUNT(*) FROM rag_chunks WHERE message_id = 7").Scan(&chunks))
	assert.Equal(t, 1, chunks, "duplicates must be collapsed to one row")

	var fts int
	require.NoError(t, store.db.QueryRow("SELECT COUNT(*) FROM rag_chunks_fts").Scan(&fts))
	assert.Equal(t, 1, fts, "FTS entries of removed duplicates must be cleaned up")

	// The unique index must now reject a fresh duplicate.
	_, err = store.db.Exec(
		`INSERT INTO rag_chunks (session_id, message_id, chunk_text, chunk_text_segmented,
			chunk_index, token_count, has_embedding, embedding_dim, project_path, backend, role, created_at)
		 VALUES ('s1', 7, 'dup', 'dup', 0, 1, 0, 0, '/p', 'claude', 'user', '2025-01-01')`)
	require.Error(t, err, "the unique index must reject a duplicate (message_id, chunk_index)")
}

// TestIndexer_ResetDimensionSync_ClearsLatch covers the HTTP reset path: the
// reset endpoints change the store's dimension out of band, so the running
// indexer must re-read it instead of trusting its one-shot latch. Otherwise a
// reset performed before the embedder's dimension is known leaves the store at
// dimension 0 and vector indexing stays dead until a restart.
func TestIndexer_ResetDimensionSync_ClearsLatch(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	idx := NewIndexer(store, nil, defaultTestRAGConfig())

	idx.mu.Lock()
	idx.dimensionSynced = true
	idx.mu.Unlock()

	idx.resetDimensionSync()

	idx.mu.Lock()
	synced := idx.dimensionSynced
	idx.mu.Unlock()
	assert.False(t, synced, "reset must clear the dimension-sync latch")
}

// TestResetIndexerDimensionSync_NilIndexer ensures the package-level helper is
// safe to call when no indexer is running (e.g. FTS-only mode).
func TestResetIndexerDimensionSync_NilIndexer(t *testing.T) {
	mu.Lock()
	origIndexer := globalIndexer
	globalIndexer = nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		globalIndexer = origIndexer
		mu.Unlock()
	})

	assert.NotPanics(t, ResetIndexerDimensionSync)
}

// TestRagVecDim_ParsesTableWidth covers the DDL parser used for mismatch
// detection, including the malformed-schema error paths.
func TestRagVecDim_ParsesTableWidth(t *testing.T) {
	store := setupSQLiteStore(t)

	// No table → 0, no error.
	dim, err := store.ragVecDim()
	require.NoError(t, err)
	assert.Equal(t, 0, dim)

	// Well-formed vec0 table.
	_, err = store.db.Exec(`CREATE VIRTUAL TABLE rag_vec USING vec0(embedding float[384] distance_metric=cosine, session_id TEXT)`)
	require.NoError(t, err)
	dim, err = store.ragVecDim()
	require.NoError(t, err)
	assert.Equal(t, 384, dim)

	// Malformed schema (no float[...] declaration) must error, not silently
	// return 0 — 0 would look like "no table" and skip mismatch detection.
	_, err = store.db.Exec(`DROP TABLE rag_vec`)
	require.NoError(t, err)
	_, err = store.db.Exec(`CREATE TABLE rag_vec (embedding BLOB)`)
	require.NoError(t, err)
	_, err = store.ragVecDim()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing dimension")

	// Unparseable width must error too.
	_, err = store.db.Exec(`DROP TABLE rag_vec`)
	require.NoError(t, err)
	_, err = store.db.Exec(`CREATE TABLE rag_vec (x TEXT DEFAULT 'float[abc]')`)
	require.NoError(t, err)
	_, err = store.ragVecDim()
	require.Error(t, err)
}

// TestCheckDimensionMismatch_NewDimZero ensures an unknown embedder dimension
// (0) is never treated as a mismatch, which would wipe a healthy index.
func TestCheckDimensionMismatch_NewDimZero(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	require.NoError(t, store.ensureVecTable())

	existing, mismatch, err := store.CheckDimensionMismatch(0)
	require.NoError(t, err)
	assert.Equal(t, 1024, existing)
	assert.False(t, mismatch, "an unknown embedder dimension must not trigger a reset")
}

// TestUpdateEmbedding_NoVecTable_LeavesPending covers UpdateEmbedding's
// vec0-unavailable path: the embedding BLOB is stored but has_embedding stays 0
// so the backfill pass can retry.
func TestUpdateEmbedding_NoVecTable_LeavesPending(t *testing.T) {
	store := setupSQLiteStore(t) // embDim = 0 → rag_vec cannot be created

	chunk := makeTestChunk(testSession1, 1, 0, "pending")
	chunk.Embedding = nil
	chunk.HasEmbedding = false
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	var chunkID int64
	require.NoError(t, store.db.QueryRow(
		"SELECT id FROM rag_chunks WHERE message_id = 1").Scan(&chunkID))

	require.NoError(t, store.UpdateEmbedding(chunkID, makeTestEmbedding()))

	var hasEmb int
	require.NoError(t, store.db.QueryRow(
		"SELECT has_embedding FROM rag_chunks WHERE id = ?", chunkID).Scan(&hasEmb))
	assert.Equal(t, 0, hasEmb, "no vec row means the chunk must stay pending")

	pending, err := store.PendingEmbeddingCount()
	require.NoError(t, err)
	assert.Equal(t, 1, pending)
}

// TestBackfillOneChunk_SkipsWrongWidth covers the backfill width guard: a
// wrong-width embedding must be skipped without flagging the chunk embedded.
func TestBackfillOneChunk_SkipsWrongWidth(t *testing.T) {
	store := setupSQLiteStoreWithDim(t) // rag_vec is 1024 wide
	require.NoError(t, store.ensureVecTable())

	chunk := makeTestChunk(testSession1, 1, 0, "pending")
	chunk.Embedding = nil
	chunk.HasEmbedding = false
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	pendingChunks, err := store.GetPendingEmbeddings(10)
	require.NoError(t, err)
	require.Len(t, pendingChunks, 1)

	// Wrong width (512 vs the 1024 table) must be skipped.
	backfilled, err := store.BatchUpdateEmbeddings(
		pendingChunks, [][]float64{make([]float64, 512)})
	require.NoError(t, err)
	assert.Equal(t, 0, backfilled, "wrong-width embedding must not be backfilled")

	var hasEmb int
	require.NoError(t, store.db.QueryRow(
		"SELECT has_embedding FROM rag_chunks WHERE message_id = 1").Scan(&hasEmb))
	assert.Equal(t, 0, hasEmb)

	// The correct width succeeds.
	backfilled, err = store.BatchUpdateEmbeddings(
		pendingChunks, [][]float64{makeTestEmbedding()})
	require.NoError(t, err)
	assert.Equal(t, 1, backfilled)

	var vecRows int
	require.NoError(t, store.db.QueryRow("SELECT COUNT(*) FROM rag_vec").Scan(&vecRows))
	assert.Equal(t, 1, vecRows)
}

// defaultTestRAGConfig returns a config suitable for indexer unit tests.
func defaultTestRAGConfig() model.RAGConfig {
	return model.RAGConfig{
		ChunkSize:    512,
		ChunkOverlap: 64,
		BatchSize:    50,
		PollInterval: "5s",
	}
}

// TestIndexer_DimensionChange_ResetsAndRequeues reproduces the blocker end to end:
// an existing 1024-dim index, then the embedding model is switched to 768.
//
// The health check must detect the change against the rag_vec table, drop the
// old-width table, and reset chat_history.indexed so the deleted chunks are
// rebuilt. Without the indexed reset the messages would be permanently orphaned
// (chunks gone, but indexed = 1 keeps them out of the unindexed queue).
func TestIndexer_DimensionChange_ResetsAndRequeues(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)

	_, err := serviceDB.Exec(
		`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming, indexed)
		 VALUES ('/proj', 'user', 'previously indexed message', 'sess-1', 'claude', 0, 1)`,
	)
	require.NoError(t, err)

	// Existing index at 1024 dims with data, and messages marked indexed.
	store := setupSQLiteStoreWithDim(t)
	require.NoError(t, store.ensureVecTable())
	insertTestChunksSQLite(t, store, 2)
	require.True(t, store.HasVecData())

	// Embedder now reports 768 dims.
	embedder := newMockEmbedderDim(t, 768)
	idx := NewIndexer(store, embedder, defaultTestRAGConfig())
	require.False(t, idx.dimensionSynced)

	idx.checkEmbedderHealth(context.Background())
	require.True(t, idx.embedderHealthy, "mock embedder should be healthy")

	// The old-width table must be gone and the new dimension latched.
	newDim, err := store.ragVecDim()
	require.NoError(t, err)
	assert.Equal(t, 0, newDim, "old rag_vec must be dropped on dimension change")
	assert.Equal(t, 768, store.embDim, "store must adopt the new dimension")
	assert.True(t, idx.dimensionSynced, "dimension sync must latch after a successful reset")

	// Chunks were cleared by the reset…
	count, err := store.ChunkCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count, "reset clears chunks so they can be rebuilt at the new width")

	// …and the messages must be queued for re-indexing, not orphaned.
	pending, err := service.GetUnindexedMessages(50)
	require.NoError(t, err)
	assert.Len(t, pending, 1, "messages must be re-queued after a dimension change")
}

// TestIndexer_SameDimension_DoesNotReset verifies the reset only fires on a real
// change, so a normal restart never wipes a healthy index.
func TestIndexer_SameDimension_DoesNotReset(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)
	_, err := serviceDB.Exec(
		`INSERT INTO chat_history (project_path, role, content, session_id, backend, streaming, indexed)
		 VALUES ('/proj', 'user', 'already indexed', 'sess-1', 'claude', 0, 1)`,
	)
	require.NoError(t, err)

	store := setupSQLiteStoreWithDim(t) // 1024
	require.NoError(t, store.ensureVecTable())
	insertTestChunksSQLite(t, store, 2)

	embedder := newMockEmbedderDim(t, 1024) // same dimension
	idx := NewIndexer(store, embedder, defaultTestRAGConfig())
	idx.checkEmbedderHealth(context.Background())

	count, err := store.ChunkCount()
	require.NoError(t, err)
	assert.Equal(t, 2, count, "matching dimension must not wipe the index")

	pending, err := service.GetUnindexedMessages(50)
	require.NoError(t, err)
	assert.Empty(t, pending, "matching dimension must not re-queue indexed messages")
}

// newMockEmbedderDim starts an OpenAI-compatible embedding server returning
// vectors of the given width.
func newMockEmbedderDim(t *testing.T, dim int) *EmbeddingClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/embeddings":
			emb := make([]float64, dim)
			for i := range emb {
				emb[i] = 0.01
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"embedding": emb, "index": 0}},
			})
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"id": "mock-model"}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client := NewEmbeddingClient(server.URL, "mock-model", "")
	client.HTTPClient = server.Client()
	// Prime the detected dimension, as a real first EmbedBatch call would.
	_, err := client.EmbedBatch(context.Background(), []string{"warmup"})
	require.NoError(t, err)
	require.Equal(t, dim, client.Dim())
	return client
}
