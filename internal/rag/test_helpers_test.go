package rag

import (
	"time"

	"clawbench/internal/model"
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

// testProjectPath is the canonical form of the fixture project path. The store
// keys chunks by project id and resolves the path back out through the registry,
// so both the input and the asserted output must use the canonical spelling —
// on Windows filepath.Abs("/test") is a drive-rooted path, not "/test".
var testProjectPath = model.NormalizeProjectPath("/test")

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
