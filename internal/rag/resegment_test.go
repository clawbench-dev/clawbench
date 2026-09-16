package rag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarkAllChunksForResegment_QueuesWithoutTouchingData asserts the trigger is
// a pure invalidation: it flags chunks for re-segmentation but must not rewrite
// text, drop vectors, or reset message indexed flags.
//
// This is what makes the trigger cheap and safe to call from an HTTP handler —
// the expensive segmentation happens later, in the indexer.
func TestMarkAllChunksForResegment_QueuesWithoutTouchingData(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	require.NoError(t, store.InsertChunks([]Chunk{
		makeTestChunk(testSession1, 1, 0, "hello world"),
		makeTestChunk(testSession1, 2, 0, "second chunk"),
	}))

	var segBefore []string
	rows, err := store.db.Query("SELECT chunk_text_segmented FROM rag_chunks ORDER BY id")
	require.NoError(t, err)
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		segBefore = append(segBefore, s)
	}
	require.NoError(t, rows.Err())
	rows.Close()

	queued, err := store.MarkAllChunksForResegment()
	require.NoError(t, err)
	assert.Equal(t, int64(2), queued)

	pending, err := store.PendingResegmentCount()
	require.NoError(t, err)
	assert.Equal(t, 2, pending, "both chunks must be queued")

	// Nothing may have changed yet: the trigger only marks work.
	var segAfter []string
	rows, err = store.db.Query("SELECT chunk_text_segmented FROM rag_chunks ORDER BY id")
	require.NoError(t, err)
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		segAfter = append(segAfter, s)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	assert.Equal(t, segBefore, segAfter, "trigger must not rewrite segmentation")

	// Vectors survive.
	assert.True(t, store.HasVecData(), "vectors must not be dropped by a resegment trigger")

	// FTS still searchable before the indexer runs.
	hits, err := store.SearchFTS("hello", 5, "", "", "", "", "", "", "")
	require.NoError(t, err)
	assert.NotEmpty(t, hits, "the existing index must stay usable until the indexer catches up")
}

// TestMarkAllChunksForResegment_IsIdempotent asserts re-triggering does not
// double-count: already-queued chunks are not flagged again, so the reported
// count reflects newly-queued work.
func TestMarkAllChunksForResegment_IsIdempotent(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	require.NoError(t, store.InsertChunks([]Chunk{
		makeTestChunk(testSession1, 1, 0, "only chunk"),
	}))

	first, err := store.MarkAllChunksForResegment()
	require.NoError(t, err)
	assert.Equal(t, int64(1), first)

	second, err := store.MarkAllChunksForResegment()
	require.NoError(t, err)
	assert.Equal(t, int64(0), second, "already-queued chunks must not be re-flagged")

	pending, err := store.PendingResegmentCount()
	require.NoError(t, err)
	assert.Equal(t, 1, pending)
}

// TestBatchResegment_RewritesTextAndFts asserts the indexer's work unit: it
// recomputes segmentation, rewrites the FTS entry, and clears the queue flag.
//
// The stored segmented value is deliberately corrupted first to simulate the
// historical bug this feature exists to repair (chunks indexed while the
// segmenter was unavailable, leaving unsplittable CJK).
func TestBatchResegment_RewritesTextAndFts(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)

	const cjk = "中文分词测试内容"
	require.NoError(t, store.InsertChunks([]Chunk{{
		SessionID: testSession1, MessageID: 1, ChunkIndex: 0,
		ChunkText: cjk,
		// Simulate the degraded state: raw text, unsplit.
		ChunkTextSegmented: cjk,
		TokenCount:         4,
		Embedding:          makeTestEmbedding(), HasEmbedding: true,
		ProjectPath: testProjectPath, Backend: testBackendClaude, Role: testRoleAssistant,
	}}))

	_, err := store.MarkAllChunksForResegment()
	require.NoError(t, err)

	chunks, err := store.GetPendingResegmentChunks(10)
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	changed, err := store.BatchResegment(chunks)
	require.NoError(t, err)
	assert.Equal(t, 1, changed, "the corrupted segmentation must be detected as changed")

	// Queue drained.
	pending, err := store.PendingResegmentCount()
	require.NoError(t, err)
	assert.Equal(t, 0, pending)

	// Stored value is now the properly segmented form.
	var stored string
	require.NoError(t, store.db.QueryRow(
		"SELECT chunk_text_segmented FROM rag_chunks WHERE message_id = 1").Scan(&stored))
	assert.Equal(t, SegmentText(cjk), stored)
	assert.NotEqual(t, cjk, stored, "raw CJK must have been split by the segmenter")

	// The FTS entry must reflect the new segmentation, not the old one.
	hits, err := store.SearchFTS("分词", 5, "", "", "", "", "", "", "")
	require.NoError(t, err)
	assert.NotEmpty(t, hits, "a partial CJK query must match after re-segmentation")
}

// TestBatchResegment_ReportsNoChangeWhenCurrent asserts a no-op pass reports 0
// changed, which is what the UI surfaces so the user can tell the data was
// already up to date.
func TestBatchResegment_ReportsNoChangeWhenCurrent(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	require.NoError(t, store.InsertChunks([]Chunk{
		makeTestChunk(testSession1, 1, 0, "already segmented"),
	}))

	_, err := store.MarkAllChunksForResegment()
	require.NoError(t, err)

	chunks, err := store.GetPendingResegmentChunks(10)
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	changed, err := store.BatchResegment(chunks)
	require.NoError(t, err)
	assert.Equal(t, 0, changed, "unchanged segmentation must not be reported as a change")

	// The flag must still be cleared, or the indexer would loop forever.
	pending, err := store.PendingResegmentCount()
	require.NoError(t, err)
	assert.Equal(t, 0, pending, "the queue flag must clear even when nothing changed")
}

// TestGetPendingResegmentChunks_RespectsLimit asserts batching works, so the
// indexer processes the queue in bounded units rather than loading 44k rows.
func TestGetPendingResegmentChunks_RespectsLimit(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	chunks := make([]Chunk, 0, 5)
	for i := range 5 {
		chunks = append(chunks, makeTestChunk(testSession1, int64(i+1), 0, "chunk text"))
	}
	require.NoError(t, store.InsertChunks(chunks))

	_, err := store.MarkAllChunksForResegment()
	require.NoError(t, err)

	got, err := store.GetPendingResegmentChunks(2)
	require.NoError(t, err)
	assert.Len(t, got, 2, "must respect the batch limit")
}

// TestPendingResegmentCount_EmptyByDefault asserts a freshly built store has an
// empty queue, so the indexer's new pass costs one cheap query in the common case.
func TestPendingResegmentCount_EmptyByDefault(t *testing.T) {
	store := setupSQLiteStoreWithDim(t)
	require.NoError(t, store.InsertChunks([]Chunk{
		makeTestChunk(testSession1, 1, 0, "text"),
	}))

	n, err := store.PendingResegmentCount()
	require.NoError(t, err)
	assert.Equal(t, 0, n, "newly indexed chunks must not be queued for re-segmentation")
}
