package rag

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- Indexer embedding guard tests (ISS-285/ISS-305) ----------

func TestIndexer_PartialEmbedBatch_NoPanic(t *testing.T) {
	// ISS-285/ISS-305: When EmbedBatch returns fewer results than input texts,
	// or returns nil entries for some indices, the code must not panic
	// and must set HasEmbedding=false for missing entries.
	store := setupSQLiteStore(t)

	// Simulate what indexMessage does after EmbedBatch returns partial results.
	// The fix is in the assignment loop: check i < len(embeddings) && embeddings[i] != nil
	texts := []string{"hello", "world", "test"}
	textChunks := ChunkText(texts[0], 100, 0) // Simple single chunk
	_ = textChunks

	// Create chunks as indexMessage would
	chunks := make([]Chunk, len(texts))
	for i, tc := range texts {
		chunks[i] = Chunk{
			SessionID:          testSession1,
			MessageID:          int64(i + 1),
			ChunkText:          tc,
			ChunkTextSegmented: SegmentText(tc),
			ChunkIndex:         i,
			TokenCount:         len(tc) / 4,
			ProjectPath:        testProjectPath,
			Backend:            testBackendClaude,
			Role:               testRoleAssistant,
			CreatedAt:          time.Now().Truncate(time.Millisecond),
		}
	}

	// Simulate EmbedBatch returning partial results:
	// - Index 0: valid embedding
	// - Index 1: nil embedding (API returned no result for this input)
	// - Index 2: missing entirely (fewer results than inputs)
	embeddings := make([][]float64, 2)
	embeddings[0] = makeTestEmbedding()
	embeddings[1] = nil // nil entry

	// This is the same logic as the fix in indexMessage
	for i := range chunks {
		if i < len(embeddings) && embeddings[i] != nil {
			chunks[i].Embedding = embeddings[i]
			chunks[i].HasEmbedding = true
		} else {
			chunks[i].Embedding = nil
			chunks[i].HasEmbedding = false
		}
	}

	// Verify assignments
	assert.True(t, chunks[0].HasEmbedding, "chunk 0 should have embedding")
	assert.NotNil(t, chunks[0].Embedding, "chunk 0 embedding should not be nil")
	assert.False(t, chunks[1].HasEmbedding, "chunk 1 should not have embedding (nil entry)")
	assert.Nil(t, chunks[1].Embedding, "chunk 1 embedding should be nil")
	assert.False(t, chunks[2].HasEmbedding, "chunk 2 should not have embedding (missing entry)")
	assert.Nil(t, chunks[2].Embedding, "chunk 2 embedding should be nil")

	// InsertChunks should succeed with the mixed embedding state
	err := store.InsertChunks(chunks)
	require.NoError(t, err, "InsertChunks should succeed with partial embeddings")

	// Verify the chunks were stored correctly
	pending, err := store.PendingEmbeddingCount()
	require.NoError(t, err)
	assert.Equal(t, 2, pending, "2 chunks should need backfill (index 1 and 2)")
}

func TestIndexer_EmptyEmbedBatch_NoPanic(t *testing.T) {
	// When EmbedBatch returns an empty slice, all chunks should be stored without embeddings.
	store := setupSQLiteStore(t)

	chunks := []Chunk{
		{
			SessionID:          testSession1,
			MessageID:          1,
			ChunkText:          "hello",
			ChunkTextSegmented: SegmentText("hello"),
			ChunkIndex:         0,
			TokenCount:         1,
			ProjectPath:        testProjectPath,
			Backend:            testBackendClaude,
			Role:               testRoleAssistant,
			CreatedAt:          time.Now().Truncate(time.Millisecond),
		},
	}

	// Simulate EmbedBatch returning empty slice
	embeddings := [][]float64{}

	for i := range chunks {
		if i < len(embeddings) && embeddings[i] != nil {
			chunks[i].Embedding = embeddings[i]
			chunks[i].HasEmbedding = true
		} else {
			chunks[i].Embedding = nil
			chunks[i].HasEmbedding = false
		}
	}

	assert.False(t, chunks[0].HasEmbedding, "chunk should not have embedding when batch is empty")

	err := store.InsertChunks(chunks)
	require.NoError(t, err, "InsertChunks should succeed with no embeddings")
}
