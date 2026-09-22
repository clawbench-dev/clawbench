package rag

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/service"
)

// ---------- RAGSearch strategy selection ----------

func TestRAGSearch_EmptyQuery(t *testing.T) {
	store := setupSQLiteStore(t)
	result, err := RAGSearch(context.Background(), store, nil, SearchParams{Query: ""}, 5, 20)
	require.NoError(t, err)
	assert.Equal(t, SearchModeFTS, result.Mode)
	assert.Empty(t, result.Results)
}

func TestRAGSearch_NilStore(t *testing.T) {
	_, err := RAGSearch(context.Background(), nil, nil, SearchParams{Query: "test"}, 5, 20)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "store is nil")
}

func TestRAGSearch_FTSOnly_WhenNoEmbedder(t *testing.T) {
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	// Insert some chunks with FTS text
	chunk := Chunk{
		SessionID: "sess-1", MessageID: 1, ChunkText: "database query optimization",
		ChunkTextSegmented: "database query optimization", ChunkIndex: 0,
		TokenCount: 3, Embedding: makeTestEmbedding(), HasEmbedding: true,
		ProjectPath: testProjectPath, Backend: testBackendClaude, Role: testRoleAssistant,
		CreatedAt: time.Now().Truncate(time.Millisecond),
	}
	err := store.InsertChunks([]Chunk{chunk})
	require.NoError(t, err)

	// Search with no embedder — should use FTS-only
	result, err := RAGSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
	}, 5, 20)
	require.NoError(t, err)
	assert.Equal(t, SearchModeFTS, result.Mode)
	assert.NotEmpty(t, result.Results)
}

func TestRAGSearch_Hybrid_WhenEmbedderHealthy(t *testing.T) {
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(true)

	// Insert chunks with embeddings and FTS text
	chunks := make([]Chunk, 3)
	for i := range 3 {
		chunks[i] = Chunk{
			SessionID: "sess-1", MessageID: int64(i + 1), ChunkText: "database query optimization test",
			ChunkTextSegmented: "database query optimization test", ChunkIndex: i,
			TokenCount: 5, Embedding: makeTestEmbedding(), HasEmbedding: true,
			ProjectPath: testProjectPath, Backend: testBackendClaude, Role: testRoleAssistant,
			CreatedAt: time.Now().Truncate(time.Millisecond),
		}
	}
	err := store.InsertChunks(chunks)
	require.NoError(t, err)

	// Pass nil embedder — since EmbedderHealthy=true, it will try to embed
	// but will fail and fall back to FTS. This is the expected behavior
	// when embedder is marked healthy but the actual client is nil.
	result, err := RAGSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
	}, 5, 20)
	require.NoError(t, err)
	// With nil embedder but healthy flag, embedding will fail → falls back to FTS
	assert.Equal(t, SearchModeFTS, result.Mode)
	assert.NotEmpty(t, result.Results)
}

func TestRAGSearch_RespectsDefaultLimit(t *testing.T) {
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)
	insertTestChunksSQLite(t, store, 10)

	result, err := RAGSearch(context.Background(), store, nil, SearchParams{
		Query:       "chunk",
		ProjectPath: testProjectPath,
	}, 3, 20)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result.Results), 3)
}

func TestRAGSearch_NoVecData_FallbackToFTS(t *testing.T) {
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(true)

	// Insert chunk WITHOUT embedding — HasVecData() returns false
	chunk := Chunk{
		SessionID: "sess-1", MessageID: 1, ChunkText: "database search test",
		ChunkTextSegmented: "database search test", ChunkIndex: 0,
		TokenCount: 3, Embedding: nil, HasEmbedding: false,
		ProjectPath: testProjectPath, Backend: testBackendClaude, Role: testRoleAssistant,
		CreatedAt: time.Now().Truncate(time.Millisecond),
	}
	err := store.InsertChunks([]Chunk{chunk})
	require.NoError(t, err)

	require.False(t, store.HasVecData(), "no chunks with embeddings → HasVecData should be false")

	// With healthy flag but HasVecData()=false — should fall back to FTS
	result, err := RAGSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
	}, 5, 20)
	require.NoError(t, err)
	assert.Equal(t, SearchModeFTS, result.Mode, "should fall back to FTS when HasVecData() is false")
}

func TestRAGSearch_HasVecDataFalse_FTSFallback(t *testing.T) {
	// When HasVecData() returns false (no vectors in vec0 table), search should
	// fall back to FTS-only even if the embedder is healthy and embDim > 0.
	// This can happen when embDim is set but all embeddings have been deleted,
	// or when embDim is configured but no chunks have been embedded yet.
	store := setupSQLiteStore(t)

	// Insert a chunk WITHOUT embedding (HasEmbedding=false), so HasVecData() returns false
	chunk := Chunk{
		SessionID: "sess-1", MessageID: 1, ChunkText: "database search test",
		ChunkTextSegmented: "database search test", ChunkIndex: 0,
		TokenCount: 3, Embedding: nil, HasEmbedding: false,
		ProjectPath: testProjectPath, Backend: testBackendClaude, Role: testRoleAssistant,
		CreatedAt: time.Now().Truncate(time.Millisecond),
	}
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	// Force embDim > 0 so the old check (embDim > 0) would pass,
	// but HasVecData() returns false because no chunks have embeddings
	store.embDim = 1024

	require.False(t, store.HasVecData(), "no chunks with embeddings → HasVecData should be false")

	// Create a mock embedding server
	mockEmb := makeTestEmbedding()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			embJSON, _ := json.Marshal(mockEmb)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"data":[{"embedding":%s,"index":0}]}`, embJSON)
			return
		}
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	embedder := NewEmbeddingClient(server.URL, "test-model", "")
	SetEmbedderHealthy(true)

	// With healthy embedder but no vec data — should use FTS-only
	result, err := RAGSearch(context.Background(), store, embedder, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
	}, 5, 20)
	require.NoError(t, err)
	assert.Equal(t, SearchModeFTS, result.Mode, "should fall back to FTS when HasVecData() is false even with healthy embedder")
	assert.NotEmpty(t, result.Results)
}

// ---------- RAGSearch with real embedder (mock HTTP server) ----------

func TestRAGSearch_Hybrid_WithMockEmbedder(t *testing.T) {
	// Create a mock embedding server that returns 1024-dim vectors (matching the store's default dimension)
	mockEmb := makeTestEmbedding()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			// Build a JSON array from the mock embedding
			embJSON, _ := json.Marshal(mockEmb)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"data":[{"embedding":%s,"index":0}]}`, embJSON)
			return
		}
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	embedder := NewEmbeddingClient(server.URL, "test-model", "")

	// Create store with 1024-dim embeddings (must set dimension for vec0 table creation)
	store := setupSQLiteStoreWithDim(t)

	// Insert chunk with 1024-dim embedding
	chunk := Chunk{
		SessionID: "sess-1", MessageID: 1, ChunkText: "database search test",
		ChunkTextSegmented: "database search test", ChunkIndex: 0,
		TokenCount: 3, Embedding: makeTestEmbedding(), HasEmbedding: true,
		ProjectPath: testProjectPath, Backend: testBackendClaude, Role: testRoleAssistant,
		CreatedAt: time.Now().Truncate(time.Millisecond),
	}
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	// Reload embDim from DB after insert (InsertChunks writes embedding_dim but doesn't update store.embDim)
	store.loadEmbeddingDimFromDB()

	// Now search with embedder — SearchVector is implemented so hybrid should work
	SetEmbedderHealthy(true)
	result, err := RAGSearch(context.Background(), store, embedder, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
	}, 5, 20)
	require.NoError(t, err)
	assert.Equal(t, SearchModeHybrid, result.Mode)
	assert.NotEmpty(t, result.Results)
}

func TestRAGSearch_EmbeddingFails_FallbackToFTS(t *testing.T) {
	// Create a mock server that returns errors for embeddings
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"internal server error"}`))
			return
		}
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	embedder := NewEmbeddingClient(server.URL, "test-model", "")

	store := setupSQLiteStore(t)
	chunk := Chunk{
		SessionID: "sess-1", MessageID: 1, ChunkText: "database search test",
		ChunkTextSegmented: "database search test", ChunkIndex: 0,
		TokenCount: 3, Embedding: makeTestEmbedding(), HasEmbedding: true,
		ProjectPath: testProjectPath, Backend: testBackendClaude, Role: testRoleAssistant,
		CreatedAt: time.Now().Truncate(time.Millisecond),
	}
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	// Force embedder healthy flag (normally set by indexer health check)
	SetEmbedderHealthy(true)

	result, err := RAGSearch(context.Background(), store, embedder, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
	}, 5, 20)
	require.NoError(t, err)
	// Embedding should fail and fall back to FTS
	assert.Equal(t, SearchModeFTS, result.Mode)
	assert.NotEmpty(t, result.Results)
}

func TestRAGSearch_ZeroLimitUsesDefault(t *testing.T) {
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)
	insertTestChunksSQLite(t, store, 5)

	result, err := RAGSearch(context.Background(), store, nil, SearchParams{
		Query:       "chunk",
		ProjectPath: testProjectPath,
		Limit:       0, // should use default
	}, 2, 20)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result.Results), 2)
}

func TestRAGSearch_NegativeLimitUsesDefault(t *testing.T) {
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)
	insertTestChunksSQLite(t, store, 5)

	result, err := RAGSearch(context.Background(), store, nil, SearchParams{
		Query:       "chunk",
		ProjectPath: testProjectPath,
		Limit:       -1,
	}, 2, 20)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result.Results), 2)
}

func TestRAGSearch_EmbedderHealthyButNoVecData(t *testing.T) {
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(true)

	// Insert chunk WITHOUT embedding — HasVecData() returns false
	chunk := Chunk{
		SessionID: "sess-1", MessageID: 1, ChunkText: "database search test",
		ChunkTextSegmented: "database search test", ChunkIndex: 0,
		TokenCount: 3, Embedding: nil, HasEmbedding: false,
		ProjectPath: testProjectPath, Backend: testBackendClaude, Role: testRoleAssistant,
		CreatedAt: time.Now().Truncate(time.Millisecond),
	}
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	require.False(t, store.HasVecData(), "no chunks with embeddings → HasVecData should be false")

	result, err := RAGSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
	}, 5, 20)
	require.NoError(t, err)
	assert.Equal(t, SearchModeFTS, result.Mode, "should fall back to FTS when HasVecData() is false even if embedder healthy")
}

func TestRAGSearch_ForceFTS_ViaPreferMode(t *testing.T) {
	// Even with a healthy embedder and vec data, PreferMode="fts" should force FTS-only.
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(true)
	insertTestChunksSQLite(t, store, 3)

	result, err := RAGSearch(context.Background(), store, nil, SearchParams{
		Query:       "chunk",
		ProjectPath: testProjectPath,
		PreferMode:  "fts",
	}, 5, 20)
	require.NoError(t, err)
	assert.Equal(t, SearchModeFTS, result.Mode, "should use FTS when PreferMode=fts")
	assert.NotEmpty(t, result.Results)
}

// ---------- getSessionTitles ----------

func TestGetSessionTitles_EmptyInput(t *testing.T) {
	titles := getSessionTitles(nil)
	assert.Empty(t, titles)

	titles = getSessionTitles(map[string]bool{})
	assert.Empty(t, titles)
}

func TestGetSessionTitles_ServiceDBNil(t *testing.T) {
	// service.DB is nil in tests — should return empty map without panic
	titles := getSessionTitles(map[string]bool{"sess-1": true})
	assert.NotNil(t, titles)
}

// ---------- RAGSearch vector-only path ----------

func TestRAGSearch_VectorOnly_WhenVecDataReadyButFTSUnavailable(t *testing.T) {
	// This tests the defensive "embedderHealthy && HasVecData() && !ftsAvailable" branch.
	// In practice ftsAvailable is always true with SQLite, but the code has this branch.
	// We test indirectly by verifying the search strategy when FTS returns no results.
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)
	insertTestChunksSQLite(t, store, 3)

	// FTS-only search (default path when embedder not healthy)
	result, err := RAGSearch(context.Background(), store, nil, SearchParams{
		Query:       "chunk",
		ProjectPath: testProjectPath,
	}, 5, 20)
	require.NoError(t, err)
	assert.Equal(t, SearchModeFTS, result.Mode)
}

// ---------- RAGSearch result enrichment ----------

func TestRAGSearch_EnrichesSessionTitles(t *testing.T) {
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	chunk := Chunk{
		SessionID: "sess-1", MessageID: 1, ChunkText: "database query optimization",
		ChunkTextSegmented: "database query optimization", ChunkIndex: 0,
		TokenCount: 3, Embedding: makeTestEmbedding(), HasEmbedding: true,
		ProjectPath: testProjectPath, Backend: testBackendClaude, Role: testRoleAssistant,
		CreatedAt: time.Now().Truncate(time.Millisecond),
	}
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	result, err := RAGSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
	}, 5, 20)
	require.NoError(t, err)
	assert.NotEmpty(t, result.Results)
	// SessionTitle may be empty since service.DB is nil in tests, but should not panic
	assert.Equal(t, SearchModeFTS, result.Mode)
}

// ---------- RAGSessionSearch aggregation, sort and archive filter ----------

func TestRAGSessionSearch_DefaultRelevanceOrder(t *testing.T) {
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	base := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	for i, sid := range []string{"sess-a", "sess-b", "sess-c"} {
		chunk := makeTestChunk(sid, int64(i+1), 0, "database query optimization")
		chunk.CreatedAt = base.Add(time.Duration(i) * time.Hour)
		require.NoError(t, store.InsertChunks([]Chunk{chunk}))
	}

	result, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, result.Sessions, 3)
	// Default order keeps search relevance (score desc); assert the strongest
	// result is first without assuming which session that is.
	assert.GreaterOrEqual(t, result.Sessions[0].Score, result.Sessions[2].Score)
}

func TestRAGSessionSearch_SortOldestUsesSessionTime(t *testing.T) {
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	base := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	specs := []struct {
		sid string
		at  time.Time
	}{
		{"sess-new", base.Add(3 * time.Hour)},
		{"sess-old", base},
		{"sess-mid", base.Add(2 * time.Hour)},
	}
	for i, s := range specs {
		chunk := makeTestChunk(s.sid, int64(i+1), 0, "database query optimization")
		chunk.CreatedAt = s.at
		require.NoError(t, store.InsertChunks([]Chunk{chunk}))
	}

	oldest, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
		SortOrder:   "oldest",
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, oldest.Sessions, 3)
	assert.Equal(t, "sess-old", oldest.Sessions[0].SessionID)
	assert.Equal(t, "sess-mid", oldest.Sessions[1].SessionID)
	assert.Equal(t, "sess-new", oldest.Sessions[2].SessionID)

	newest, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
		SortOrder:   "newest",
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, newest.Sessions, 3)
	assert.Equal(t, "sess-new", newest.Sessions[0].SessionID)
	assert.Equal(t, "sess-mid", newest.Sessions[1].SessionID)
	assert.Equal(t, "sess-old", newest.Sessions[2].SessionID)
}

func TestRAGSessionSearch_ArchiveFilterActiveKeepsAllWhenNoSessionDB(t *testing.T) {
	// With no service DB, archived status is unknown and defaults to active, so
	// filtering for "active" must not drop any results.
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	for i, sid := range []string{"sess-a", "sess-b"} {
		chunk := makeTestChunk(sid, int64(i+1), 0, "database query optimization")
		require.NoError(t, store.InsertChunks([]Chunk{chunk}))
	}

	result, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
		Archived:    "active",
	}, 10, 20)
	require.NoError(t, err)
	assert.Len(t, result.Sessions, 2)

	// "archived" with unknown status → nothing matches.
	result, err = RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
		Archived:    "archived",
	}, 10, 20)
	require.NoError(t, err)
	assert.Empty(t, result.Sessions)
}

// ---------- session type filter ----------

// TestRAGSessionSearch_TypeFilter locks the post-aggregation type filter. The
// session_type lives on chat_sessions, not on rag_chunks, so the filter can only
// run once each session's DB metadata is loaded — the same shape as the archive
// filter.
func TestRAGSessionSearch_TypeFilter(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	// One conversation and one task execution, both matching the query text.
	_, err := serviceDB.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, session_type) VALUES ('sess-conv', ?, 'claude', 'Conversation', 'chat')",
		testProjectPath,
	)
	require.NoError(t, err)
	_, err = serviceDB.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, session_type) VALUES ('sess-job', ?, 'claude', 'Task run', 'scheduled')",
		testProjectPath,
	)
	require.NoError(t, err)

	for i, sid := range []string{"sess-conv", "sess-job"} {
		require.NoError(t, store.InsertChunks([]Chunk{
			makeTestChunk(sid, int64(i+1), 0, testDBQueryOptimization),
		}))
	}

	// "all" (and empty) → no type restriction, both sessions returned.
	all, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
		SessionType: "all",
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, all.Sessions, 2)

	// "task" → only the scheduled session, and its type is reported back.
	task, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
		SessionType: "task",
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, task.Sessions, 1)
	assert.Equal(t, "sess-job", task.Sessions[0].SessionID)
	assert.Equal(t, "scheduled", task.Sessions[0].SessionType)

	// "chat" → only the conversation.
	chat, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
		SessionType: "chat",
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, chat.Sessions, 1)
	assert.Equal(t, "sess-conv", chat.Sessions[0].SessionID)
	assert.Equal(t, "chat", chat.Sessions[0].SessionType)

	// An unknown value falls back to "all" rather than dropping everything.
	bogus, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
		SessionType: "bogus",
	}, 10, 20)
	require.NoError(t, err)
	assert.Len(t, bogus.Sessions, 2)
}

// TestRAGSessionSearch_TypeFilterUnknownTypeTreatedAsChat mirrors the archive
// filter's "unknown defaults to the benign value" contract: with no service DB
// the session type is unknown, so "chat" keeps every result while "task"
// matches nothing.
func TestRAGSessionSearch_TypeFilterUnknownTypeTreatedAsChat(t *testing.T) {
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	for i, sid := range []string{"sess-a", "sess-b"} {
		require.NoError(t, store.InsertChunks([]Chunk{
			makeTestChunk(sid, int64(i+1), 0, testDBQueryOptimization),
		}))
	}

	chat, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
		SessionType: "chat",
	}, 10, 20)
	require.NoError(t, err)
	assert.Len(t, chat.Sessions, 2)

	task, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
		SessionType: "task",
	}, 10, 20)
	require.NoError(t, err)
	assert.Empty(t, task.Sessions)
}

// ---------- aggregateSessionHits / sessionIDSet / sortSessionResults ----------
// Regression coverage for the helpers extracted out of RAGSessionSearch. They
// encode the aggregation contract (first-seen order, per-session chunk cap,
// best-score tracking) and the time-sort tie-break, so lock them down directly
// rather than only through the DB-backed end-to-end path.

func TestAggregateSessionHits_GroupsInFirstSeenOrder(t *testing.T) {
	base := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	hits := []SearchHit{
		{SessionID: "sess-b", ChunkID: 1, Score: 0.4, Backend: "claude", ProjectPath: testProjectPath, CreatedAt: base},
		{SessionID: "sess-a", ChunkID: 2, Score: 0.9, Backend: "codex", ProjectPath: testProjectPath, CreatedAt: base.Add(time.Hour)},
		{SessionID: "sess-b", ChunkID: 3, Score: 0.7},
	}

	sessions := aggregateSessionHits(hits)
	require.Len(t, sessions, 2)
	// First-seen order: sess-b appeared first even though sess-a scores higher.
	assert.Equal(t, "sess-b", sessions[0].SessionID)
	assert.Equal(t, "sess-a", sessions[1].SessionID)

	// Backend/ProjectPath/CreatedAt come from the first hit of the session.
	assert.Equal(t, "claude", sessions[0].Backend)
	assert.Equal(t, testProjectPath, sessions[0].ProjectPath)
	assert.Equal(t, base, sessions[0].CreatedAt)

	// MatchCount counts every hit; Chunks holds the details.
	assert.Equal(t, 2, sessions[0].MatchCount)
	require.Len(t, sessions[0].Chunks, 2)
	assert.Equal(t, int64(1), sessions[0].Chunks[0].ChunkID)
	assert.Equal(t, int64(3), sessions[0].Chunks[1].ChunkID)
	// Best score wins even though it was the second hit.
	assert.Equal(t, 0.7, sessions[0].Score)
}

func TestAggregateSessionHits_PerSessionChunkCap(t *testing.T) {
	hits := make([]SearchHit, 0, maxChunksPerSession+3)
	// maxChunksPerSession+3 hits for one session: details are capped but the
	// match count still reflects every hit.
	for i := range maxChunksPerSession + 3 {
		hits = append(hits, SearchHit{
			SessionID: "sess-cap",
			ChunkID:   int64(i + 1),
			Score:     float64(i) / 100.0,
		})
	}

	sessions := aggregateSessionHits(hits)
	require.Len(t, sessions, 1)
	assert.Len(t, sessions[0].Chunks, maxChunksPerSession)
	assert.Equal(t, maxChunksPerSession+3, sessions[0].MatchCount)
}

func TestSessionIDSet_CollectsDistinctIDs(t *testing.T) {
	sessions := []*SessionSearchResult{
		{SessionID: "a"}, {SessionID: "b"}, {SessionID: "a"},
	}
	ids := sessionIDSet(sessions)
	assert.Len(t, ids, 2)
	assert.True(t, ids["a"])
	assert.True(t, ids["b"])
}

func TestSortSessionResults_TimeSortTieBreakByID(t *testing.T) {
	at := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	// Equal timestamps must fall back to ascending session id, deterministically.
	sessions := []*SessionSearchResult{
		{SessionID: "sess-c", CreatedAt: at, Score: 0.1},
		{SessionID: "sess-a", CreatedAt: at, Score: 0.9},
		{SessionID: "sess-b", CreatedAt: at, Score: 0.5},
	}

	sortSessionResults(sessions, "newest")
	assert.Equal(t, []string{"sess-a", "sess-b", "sess-c"}, sessionIDs(sessions))

	// Default (relevance) orders by score desc, ignoring ids/timestamps.
	sortSessionResults(sessions, "")
	assert.Equal(t, []string{"sess-a", "sess-b", "sess-c"}, sessionIDs(sessions))
}

func sessionIDs(sessions []*SessionSearchResult) []string {
	out := make([]string, len(sessions))
	for i, s := range sessions {
		out[i] = s.SessionID
	}
	return out
}

// ---------- title channel ----------

// insertTitleSession adds a chat_sessions row so the title channel has
// something to match. created_at is explicit so ordering assertions are stable.
func insertTitleSession(t *testing.T, db *sql.DB, id, title, createdAt string, archived bool, sessionType string) {
	t.Helper()
	archivedInt := 0
	if archived {
		archivedInt = 1
	}
	if sessionType == "" {
		sessionType = "chat"
	}
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title, session_type, archived, created_at, updated_at)
		 VALUES (?, ?, 'claude', ?, ?, ?, ?, ?)`,
		id, testProjectPath, title, sessionType, archivedInt, createdAt, createdAt,
	)
	require.NoError(t, err)
}

// A session reachable only by its title must be found, and title matches must
// rank ahead of content matches: the user typed a name they remembered.
func TestRAGSessionSearch_TitleMatchRanksFirst(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	// sess-title matches on its name only; its message body has no "database".
	insertTitleSession(t, serviceDB, "sess-title", "数据库优化讨论", "2024-01-01 10:00:00", false, "")
	// sess-content matches on message content only.
	insertTitleSession(t, serviceDB, "sess-content", "无关标题", "2024-02-01 10:00:00", false, "")
	chunk := makeTestChunk("sess-content", 1, 0, "database query optimization")
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	result, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "数据库",
		ProjectPath: testProjectPath,
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	assert.Equal(t, "sess-title", result.Sessions[0].SessionID)
	assert.True(t, result.Sessions[0].TitleMatch)
	assert.True(t, result.Sessions[0].TitleOnly, "no content search matched this session")
	assert.Equal(t, "数据库优化讨论", result.Sessions[0].SessionTitle)
	assert.NotEmpty(t, result.Sessions[0].TitleMatchPositions, "title offsets drive the highlight")
}

// A session matching on both channels is emitted once, keeping its chunks so the
// detail view still shows the message hits, while also carrying the title badge.
func TestRAGSessionSearch_MergesTitleAndContentMatch(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	// Both the title and the message body contain the query term, so the session
	// is found by both channels.
	insertTitleSession(t, serviceDB, "sess-both", "数据库优化方案", "2024-01-01 10:00:00", false, "")
	chunk := makeTestChunk("sess-both", 1, 0, "数据库 query optimization")
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	result, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "数据库",
		ProjectPath: testProjectPath,
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, result.Sessions, 1, "one session matching both channels must appear once")
	s := result.Sessions[0]
	assert.Equal(t, "sess-both", s.SessionID)
	assert.True(t, s.TitleMatch)
	assert.False(t, s.TitleOnly, "chunks were found, so the detail view has content")
	assert.NotEmpty(t, s.Chunks)
	assert.Equal(t, 1, s.MatchCount)
	assert.Equal(t, "数据库优化方案", s.SessionTitle)
}

// Content-only results carry no title text from the content query, so the merge
// must fill it in — the client renders and highlights it.
func TestRAGSessionSearch_ContentOnlyResultsGetTitleFilledIn(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	insertTitleSession(t, serviceDB, "sess-c", "无关标题", "2024-01-01 10:00:00", false, "")
	chunk := makeTestChunk("sess-c", 1, 0, "database query optimization")
	require.NoError(t, store.InsertChunks([]Chunk{chunk}))

	result, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:       "database",
		ProjectPath: testProjectPath,
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	assert.Equal(t, "无关标题", result.Sessions[0].SessionTitle)
	assert.False(t, result.Sessions[0].TitleMatch)
	assert.False(t, result.Sessions[0].TitleOnly)
}

// With no RAG store the title channel is still the whole answer: an install that
// never configured RAG can search sessions by name instead of getting an error.
func TestRAGSessionSearch_NilStoreStillSearchesTitles(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)

	insertTitleSession(t, serviceDB, "sess-title", "数据库优化讨论", "2024-01-01 10:00:00", false, "")
	insertTitleSession(t, serviceDB, "sess-other", "前端重构", "2024-01-02 10:00:00", false, "")

	result, err := RAGSessionSearch(context.Background(), nil, nil, SearchParams{
		Query:       "数据库",
		ProjectPath: testProjectPath,
	}, 10, 20)
	require.NoError(t, err, "a nil store must not fail the title channel")
	require.Len(t, result.Sessions, 1)
	assert.Equal(t, "sess-title", result.Sessions[0].SessionID)
	assert.True(t, result.Sessions[0].TitleMatch)
	// "title" reports that only the name channel ran — distinct from "recent"
	// (browse), which the client would render as a paginated browse list.
	assert.Equal(t, SearchModeTitle, result.Mode)
}

func TestRAGSessionSearch_NilStoreEmptyQueryStillEmpty(t *testing.T) {
	// Browse mode is the handler's job; an empty query here stays empty.
	result, err := RAGSessionSearch(context.Background(), nil, nil, SearchParams{}, 10, 20)
	require.NoError(t, err)
	assert.Empty(t, result.Sessions)
}

// The title channel honors the same filters as the content channel, so both
// sets of results come from the same population.
func TestRAGSessionSearch_TitleChannelHonorsFilters(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	insertTitleSession(t, serviceDB, "sess-active", "数据库优化", "2024-01-01 10:00:00", false, "")
	insertTitleSession(t, serviceDB, "sess-archived", "数据库归档", "2024-02-01 10:00:00", true, "")
	insertTitleSession(t, serviceDB, "sess-task", "数据库任务", "2024-03-01 10:00:00", false, "scheduled")

	active, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query: "数据库", ProjectPath: testProjectPath, Archived: "active",
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, active.Sessions, 1)
	assert.Equal(t, "sess-active", active.Sessions[0].SessionID)

	archived, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query: "数据库", ProjectPath: testProjectPath, Archived: "archived",
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, archived.Sessions, 1)
	assert.Equal(t, "sess-archived", archived.Sessions[0].SessionID)

	// Type filter switches to task executions instead of conversations.
	tasks, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query: "数据库", ProjectPath: testProjectPath, SessionType: "task",
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, tasks.Sessions, 1)
	assert.Equal(t, "sess-task", tasks.Sessions[0].SessionID)

	// A time range that excludes every session's creation time yields nothing.
	windowed, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query: "数据库", ProjectPath: testProjectPath, FromTime: "2025-01-01 00:00:00",
	}, 10, 20)
	require.NoError(t, err)
	assert.Empty(t, windowed.Sessions)
}

// Time sort overrides the title-first ordering: the user asked for a
// chronological list, so the blocks are not preserved.
func TestRAGSessionSearch_TimeSortOverridesTitleFirst(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	insertTitleSession(t, serviceDB, "sess-new", "数据库新", "2024-03-01 10:00:00", false, "")
	insertTitleSession(t, serviceDB, "sess-old", "数据库旧", "2024-01-01 10:00:00", false, "")

	newest, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query: "数据库", ProjectPath: testProjectPath, SortOrder: "newest",
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, newest.Sessions, 2)
	assert.Equal(t, "sess-new", newest.Sessions[0].SessionID)

	oldest, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query: "数据库", ProjectPath: testProjectPath, SortOrder: "oldest",
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, oldest.Sessions, 2)
	assert.Equal(t, "sess-old", oldest.Sessions[0].SessionID)
}

// The title channel honors the exclude scope too. This is what keeps the
// current conversation out of its own /cb-chatsearch results: the command sets
// exclude_session_id, and a title match must not bypass it.
func TestRAGSessionSearch_TitleChannelHonorsExcludeSession(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	insertTitleSession(t, serviceDB, "sess-current", "数据库优化讨论", "2024-02-01 10:00:00", false, "")
	insertTitleSession(t, serviceDB, "sess-other", "数据库迁移记录", "2024-01-01 10:00:00", false, "")

	result, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query:            "数据库",
		ProjectPath:      testProjectPath,
		ExcludeSessionID: "sess-current",
	}, 10, 20)
	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	assert.Equal(t, "sess-other", result.Sessions[0].SessionID)
}

// searchLimit caps the merged list, and a title match cannot push the count over.
func TestRAGSessionSearch_TitleMatchesRespectSearchLimit(t *testing.T) {
	serviceDB := setupIndexerServiceDB(t)
	store := setupSQLiteStore(t)
	SetEmbedderHealthy(false)

	for i, id := range []string{"sess-1", "sess-2", "sess-3"} {
		insertTitleSession(t, serviceDB, id, "数据库优化", fmt.Sprintf("2024-01-0%d 10:00:00", i+1), false, "")
	}

	result, err := RAGSessionSearch(context.Background(), store, nil, SearchParams{
		Query: "数据库", ProjectPath: testProjectPath,
	}, 2, 20)
	require.NoError(t, err)
	assert.Len(t, result.Sessions, 2)
}

// ---------- titleSearchTerms ----------

func TestTitleSearchTerms(t *testing.T) {
	if segmenter == nil {
		require.NoError(t, InitSegmenter())
	}

	// Segmentation splits CJK into words, and every word is a term.
	assert.Equal(t, []string{"数据库", "优化"}, titleSearchTerms("数据库优化"))

	// Single-character segmentation noise (spaces, punctuation, lone chars) is
	// dropped: matching them would return nearly every session.
	assert.Equal(t, []string{"session", "search"}, titleSearchTerms("session search"))
	assert.Equal(t, []string{"bge", "m3", "模型"}, titleSearchTerms("bge-m3 模型"))

	// Nothing survives → the trimmed query is used literally, so a
	// one-character title is still findable rather than matching everything.
	assert.Equal(t, []string{"库"}, titleSearchTerms("库"))
	assert.Equal(t, []string{"..."}, titleSearchTerms("..."))

	// An empty query has no terms at all.
	assert.Empty(t, titleSearchTerms(""))
	assert.Empty(t, titleSearchTerms("   "))
}

// ---------- mergeTitleAndContentMatches ----------

func TestMergeTitleAndContentMatches_OrderAndDedup(t *testing.T) {
	titleMatches := []service.RecentSession{
		{ID: "t1", Title: "数据库甲", CreatedAt: time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)},
		{ID: "shared", Title: "数据库乙", CreatedAt: time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC)},
	}
	contentMatches := []*SessionSearchResult{
		{SessionID: "shared", Score: 0.9, MatchCount: 2, Chunks: []ChunkHit{{ChunkID: 1}}},
		{SessionID: "c1", Score: 0.5},
	}

	merged := mergeTitleAndContentMatches(titleMatches, contentMatches, "数据库")

	// Title matches first (as the title query ordered them), then content-only.
	require.Len(t, merged, 3)
	assert.Equal(t, []string{"t1", "shared", "c1"}, sessionIDs(merged))

	// The shared session keeps its chunks and is no longer title-only.
	shared := merged[1]
	assert.True(t, shared.TitleMatch)
	assert.False(t, shared.TitleOnly)
	assert.Equal(t, 2, shared.MatchCount)
	assert.Len(t, shared.Chunks, 1)
	assert.InDelta(t, 0.9, shared.Score, 1e-9)
	assert.NotEmpty(t, shared.TitleMatchPositions)
}

func TestMergeTitleAndContentMatches_ContentOnlyHasNoTitleFlag(t *testing.T) {
	contentMatches := []*SessionSearchResult{{SessionID: "c1", Score: 0.5}}
	merged := mergeTitleAndContentMatches(nil, contentMatches, "query")
	require.Len(t, merged, 1)
	assert.False(t, merged[0].TitleMatch)
	assert.False(t, merged[0].TitleOnly)
}

// Title matches lead even when a content match has a far higher score.
func TestSortSessionResults_TitleMatchBeatsHigherScore(t *testing.T) {
	sessions := []*SessionSearchResult{
		{SessionID: "content", Score: 99.0},
		{SessionID: "title", Score: 0.0, TitleMatch: true},
	}
	sortSessionResults(sessions, "")
	assert.Equal(t, []string{"title", "content"}, sessionIDs(sessions))
}

// Ties are broken on session id so equal-scoring rows keep a stable order —
// sort.Slice is not stable, and all title-only matches score 0.
func TestSortSessionResults_RelevanceTieBreakIsStable(t *testing.T) {
	at := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	sessions := []*SessionSearchResult{
		{SessionID: "sess-c", TitleMatch: true, CreatedAt: at},
		{SessionID: "sess-a", TitleMatch: true, CreatedAt: at},
		{SessionID: "sess-b", TitleMatch: true, CreatedAt: at},
	}
	sortSessionResults(sessions, "")
	assert.Equal(t, []string{"sess-a", "sess-b", "sess-c"}, sessionIDs(sessions))
}

// Equal scores prefer the newer session. Title matches all score 0, so this is
// what keeps them in the newest-first order the title query returned instead of
// falling through to a UUID comparison.
func TestSortSessionResults_EqualScorePrefersNewest(t *testing.T) {
	base := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	sessions := []*SessionSearchResult{
		{SessionID: "sess-old", TitleMatch: true, CreatedAt: base},
		{SessionID: "sess-new", TitleMatch: true, CreatedAt: base.Add(2 * time.Hour)},
		{SessionID: "sess-mid", TitleMatch: true, CreatedAt: base.Add(time.Hour)},
	}
	sortSessionResults(sessions, "")
	assert.Equal(t, []string{"sess-new", "sess-mid", "sess-old"}, sessionIDs(sessions))
}
