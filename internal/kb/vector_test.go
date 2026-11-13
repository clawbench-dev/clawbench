package kb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/rag"

	"github.com/stretchr/testify/require"
)

func TestVectorIndexLifecycle(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})

	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "a.txt", ContentHash: "h", Status: StatusReady,
	})
	require.NoError(t, err)
	require.NoError(t, st.ReplaceChunks(docID, []Chunk{{
		DocumentID:         docID,
		CollectionID:       colID,
		ChunkText:          "报销流程：先提交申请。",
		ChunkTextSegmented: rag.SegmentText("报销流程：先提交申请。"),
		ChunkIndex:         0,
	}}, ""))

	pending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.Len(t, pending, 1)

	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID:      pending[0].ID,
		DocumentID:   docID,
		CollectionID: colID,
		ProjectID:    0,
		Vector:       []float64{1, 0, 0},
	}}))

	require.True(t, st.HasVecData())
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.True(t, doc.VectorReady)

	hits, err := st.SearchVector([]float64{1, 0, 0}, 10, []int64{colID})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.InDelta(t, 1.0, hits[0].VectorScore, 1e-6)
	require.Equal(t, "a.txt", hits[0].DocumentTitle)

	// Reset drops the vector table and clears the flags; keyword data stays.
	require.NoError(t, st.ResetVector(3))
	require.False(t, st.HasVecData())
	doc, err = st.GetDocument(docID)
	require.NoError(t, err)
	require.False(t, doc.VectorReady)
	require.True(t, doc.FTSReady)
}

// newFakeEmbedServer answers /v1/embeddings with a fixed 3-dim vector for every
// input text. Used by both the search and indexer vector tests.
func newFakeEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		data := make([]map[string]any, 0, len(body.Input))
		for i := range body.Input {
			data = append(data, map[string]any{"index": i, "embedding": []float64{1, 0, 0}})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHybridSearchWithEmbedder(t *testing.T) {
	srv := newFakeEmbedServer(t)

	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})
	mu.Lock()
	globalEmbedder = rag.NewEmbeddingClient(srv.URL, "test-model", "")
	mu.Unlock()

	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "handbook.md", ContentHash: "h", Status: StatusReady,
	})
	require.NoError(t, err)
	require.NoError(t, st.ReplaceChunks(docID, []Chunk{{
		DocumentID:         docID,
		CollectionID:       colID,
		ChunkText:          "报销流程：先提交申请，再等财务审批。",
		ChunkTextSegmented: rag.SegmentText("报销流程：先提交申请，再等财务审批。"),
		ChunkIndex:         0,
	}}, ""))

	pending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID:      pending[0].ID,
		DocumentID:   docID,
		CollectionID: colID,
		Vector:       []float64{1, 0, 0},
	}}))

	res, err := Search(context.Background(), SearchParams{Query: "报销流程", ProjectID: 0})
	require.NoError(t, err)
	require.Equal(t, SearchModeHybrid, res.Mode)
	require.NotEmpty(t, res.Results)
	require.Equal(t, "handbook.md", res.Results[0].DocumentTitle)
}

func TestEnsureDimensionMismatchResetsVectors(t *testing.T) {
	st := newTestStore(t)
	setTestGlobals(t, st, model.KBConfig{})

	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "a.txt", ContentHash: "h", Status: StatusReady,
	})
	require.NoError(t, err)
	require.NoError(t, st.ReplaceChunks(docID, []Chunk{{
		DocumentID: docID, CollectionID: colID, ChunkIndex: 0,
		ChunkText: "x", ChunkTextSegmented: "x",
	}}, ""))
	pending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID: pending[0].ID, DocumentID: docID, CollectionID: colID,
		Vector: []float64{1, 0, 0},
	}}))

	// A different model produces a different width: the vector layer is rebuilt
	// and every chunk becomes pending again.
	require.NoError(t, st.EnsureDimension(4))
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.False(t, doc.VectorReady)
	again, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.Len(t, again, 1)
}
