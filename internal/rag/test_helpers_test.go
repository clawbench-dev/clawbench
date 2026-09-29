package rag

import (
	"time"
)

// Shared test constants to avoid goconst duplicates across test files.
const (
	testModelBgeM3Latest = "bge-m3:latest"
	testModelBgeM3       = "bge-m3"
	testV1Models         = "/v1/models"
	testV1Embeddings     = "/v1/embeddings"
	testRoleAssistant    = "assistant"
	testRoleUser         = "user"
	testSession1         = "sess-1"
	testSession2         = "sess-2"
	testProjectPath      = "/test"
	// testProjectID is the projects.id that testProjectPath maps to in the
	// fixtures that create a projects row (see ensureTestProject).
	testProjectID           = 1
	testBackendClaude       = "claude"
	testBackendCodebuddy    = "codebuddy"
	testNeedsBackfill       = "needs backfill"
	testOllamaURL           = "http://localhost:11434"
	testPollInterval10s     = "10s"
	testPollInterval24h     = "24h"
	testDBQueryOptimization = "database query optimization"
	testDBSearch            = "database search"
	testDBQuery             = "database query"
	testOtherModelLatest    = "other-model:latest"
	testSearchQueryTest     = "test"
	testSearchQueryChunk    = "chunk"
	testEmbeddingTextHello  = "hello"
)

// makeTestEmbedding creates a 1024-dim float64 slice
// with simple sequential values for testing.
func makeTestEmbedding() []float64 {
	emb := make([]float64, 1024)
	for i := range emb {
		emb[i] = float64(i%100) * 0.01
	}
	return emb
}

// makeTestChunk creates a Chunk with the given text and a default 1024-dim embedding.
func makeTestChunk(sessionID string, messageID int64, chunkIndex int, text string) Chunk {
	return Chunk{
		SessionID:          sessionID,
		MessageID:          messageID,
		ChunkText:          text,
		ChunkTextSegmented: SegmentText(text),
		ChunkIndex:         chunkIndex,
		TokenCount:         len(text) / 4,
		Embedding:          makeTestEmbedding(),
		HasEmbedding:       true,
		ProjectPath:        testProjectPath,
		Backend:            testBackendClaude,
		Role:               testRoleAssistant,
		CreatedAt:          time.Now().Truncate(time.Millisecond),
	}
}
