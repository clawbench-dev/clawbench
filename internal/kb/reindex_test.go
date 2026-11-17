package kb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/rag"

	"github.com/stretchr/testify/require"
)

// TestResetDocumentsForReindex_EmptySliceTouchesNothing pins the difference
// between nil ("every collection") and an empty slice ("no collections").
// A project with no visible library resolves to an empty slice, and reading
// that as "everything" wiped every other project's index.
func TestResetDocumentsForReindex_EmptySliceTouchesNothing(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "a.txt", "报销流程", "")

	// Empty, non-nil: nothing happens.
	require.NoError(t, st.ResetDocumentsForReindex([]int64{}))
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, doc.Status)
	chunks, err := st.ListChunks(docID, 10)
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	// The facade is equally strict (it is the path the HTTP layer uses).
	setTestGlobals(t, st, model.KBConfig{})
	require.NoError(t, ReindexCollections([]int64{}))
	chunks, err = st.ListChunks(docID, 10)
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	// nil still means "rebuild everything": that path must keep working.
	require.NoError(t, st.ResetDocumentsForReindex(nil))
	chunks, err = st.ListChunks(docID, 10)
	require.NoError(t, err)
	require.Empty(t, chunks)
	doc, err = st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, StatusPending, doc.Status)
}

// TestReconcileEmbeddingDimension_RebuildsStaleTable covers the "switched the
// embedding model" case: the library was fully embedded with a 3-dim model, no
// chunk is pending, and the new model answers with 4 dims. Without the startup
// reconciliation kb_vec keeps its old width forever, hybrid search silently
// returns keywords only and mode:"vector" fails on every request.
func TestReconcileEmbeddingDimension_RebuildsStaleTable(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "a.txt", "报销流程：先提交申请", "")
	pending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID: pending[0].ID, DocumentID: docID, CollectionID: colID,
		Vector: []float64{1, 0, 0},
	}}))
	require.True(t, st.HasVecData())
	nothingPending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.Empty(t, nothingPending, "precondition: no work left, that is the trap")

	// The new model answers with a different width.
	srv := newFakeEmbedServerDim(t, 4)
	reconcileWG.Add(1)
	reconcileEmbeddingDimension(st, rag.NewEmbeddingClient(srv.URL, "new-model", ""))

	dim, ok := st.vecDimFromSchema()
	require.True(t, ok)
	require.Equal(t, 4, dim, "kb_vec must be recreated at the new width")
	require.False(t, st.HasVecData(), "old vectors must be gone")
	again, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.Len(t, again, 1, "the chunk must be queued for re-embedding")
}

// TestReconcileEmbeddingDimension_SameWidthKeepsData is the negative control:
// the probe must not drop a perfectly good index.
func TestReconcileEmbeddingDimension_SameWidthKeepsData(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "a.txt", "报销流程", "")
	pending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID: pending[0].ID, DocumentID: docID, CollectionID: colID,
		Vector: []float64{1, 0, 0},
	}}))

	srv := newFakeEmbedServerDim(t, 3)
	reconcileWG.Add(1)
	reconcileEmbeddingDimension(st, rag.NewEmbeddingClient(srv.URL, "same-model", ""))

	require.True(t, st.HasVecData())
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.True(t, doc.VectorReady)
}

// TestReconcileEmbeddingDimension_ProbeFailureKeepsIndex: when the endpoint is
// down nothing can be compared, so nothing may be guessed.
func TestReconcileEmbeddingDimension_ProbeFailureKeepsIndex(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "a.txt", "报销流程", "")
	pending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID: pending[0].ID, DocumentID: docID, CollectionID: colID,
		Vector: []float64{1, 0, 0},
	}}))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	reconcileWG.Add(1)
	reconcileEmbeddingDimension(st, rag.NewEmbeddingClient(srv.URL, "m", ""))
	require.True(t, st.HasVecData())
}

// newFakeEmbedServerDim answers /v1/embeddings with a fixed-width vector.
func newFakeEmbedServerDim(t *testing.T, dim int) *httptest.Server {
	t.Helper()
	vec := make([]float64, dim)
	vec[0] = 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		data := make([]map[string]any, 0, len(body.Input))
		for i := range body.Input {
			data = append(data, map[string]any{"index": i, "embedding": vec})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSaveEmbeddings_RebuildsOnWidthChange: a batch produced by a newly
// configured model must not be written into the old table (sqlite-vec only
// notices at query time).
func TestSaveEmbeddings_RebuildsOnWidthChange(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "a.txt", "报销流程", "")
	pending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID: pending[0].ID, DocumentID: docID, CollectionID: colID,
		Vector: []float64{1, 0, 0},
	}}))

	// Second document, new model with a wider vector.
	second := seedDoc(t, st, colID, "b.txt", "年假天数", "")
	next, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.Len(t, next, 1)
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID: next[0].ID, DocumentID: second, CollectionID: colID,
		Vector: []float64{1, 0, 0, 0},
	}}))

	dim, ok := st.vecDimFromSchema()
	require.True(t, ok)
	require.Equal(t, 4, dim)
	hits, err := st.SearchVector([]float64{1, 0, 0, 0}, 10, []int64{colID})
	require.NoError(t, err, "the query vector must be accepted by the rebuilt table")
	require.Len(t, hits, 1)
	require.Equal(t, second, hits[0].DocumentID)
}

// TestReplaceChunks_RejectsMovedDocument: the indexer reads the collection
// before a long extraction. If the document was moved meanwhile, writing the
// chunks would make the text searchable in the OLD library.
func TestReplaceChunks_RejectsMovedDocument(t *testing.T) {
	st := newTestStore(t)
	source, err := st.CreateCollection(Collection{Name: "old", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	target, err := st.CreateCollection(Collection{Name: "new", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)

	docID, err := st.AddDocument(Document{
		CollectionID: source, Title: "a.txt", ContentHash: "h", StoredPath: filepath.Join(t.TempDir(), "a.txt"),
		Status: StatusPending,
	})
	require.NoError(t, err)
	// The indexer marked it extracting and read the collection.
	require.NoError(t, st.SetDocumentStatus(docID, StatusExtracting, "", ""))
	expected := source

	// The user moves it (which drops chunks and re-queues it).
	require.NoError(t, st.MoveDocumentRecord(docID, target, "/tmp/moved.txt"))

	err = st.ReplaceChunks(docID, []Chunk{{
		DocumentID: docID, CollectionID: expected, ChunkIndex: 0,
		ChunkText: "报销流程", ChunkTextSegmented: rag.SegmentText("报销流程"),
	}}, "", expected)
	require.ErrorIs(t, err, ErrDocumentChanged)

	// Nothing was written anywhere, and the row is still queued.
	oldHits, err := st.SearchFTS("报销流程", 10, []int64{source})
	require.NoError(t, err)
	require.Empty(t, oldHits, "the old library must not gain the document's text")
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, StatusPending, doc.Status)
	require.Equal(t, target, doc.CollectionID)
}

// TestReplaceChunks_RejectsDeletedDocument: a delete during extraction must
// not leave orphaned chunks pointing at a row that no longer exists.
func TestReplaceChunks_RejectsDeletedDocument(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "a.txt", ContentHash: "h", Status: StatusExtracting,
	})
	require.NoError(t, err)

	_, err = st.DeleteDocument(docID)
	require.NoError(t, err)

	err = st.ReplaceChunks(docID, []Chunk{{
		DocumentID: docID, CollectionID: colID, ChunkIndex: 0, ChunkText: "x", ChunkTextSegmented: "x",
	}}, "", colID)
	require.ErrorIs(t, err, ErrDocumentChanged)

	var chunks int
	require.NoError(t, st.db.QueryRow(`SELECT COUNT(*) FROM kb_chunks WHERE document_id = ?`, docID).Scan(&chunks))
	require.Zero(t, chunks)
}

// TestProcessDocumentDropsResultAfterMove drives the indexer itself: the
// document is moved between the pending read and the write, which the test
// simulates through the store guard it must honor.
func TestIndexerDiscardsResultForMovedDocument(t *testing.T) {
	st := newTestStore(t)
	colA, err := st.CreateCollection(Collection{Name: "A", Scope: ScopeProject, ProjectID: 1, AutoInject: true})
	require.NoError(t, err)
	colB, err := st.CreateCollection(Collection{Name: "B", Scope: ScopeProject, ProjectID: 2, AutoInject: true})
	require.NoError(t, err)

	dir := t.TempDir()
	src := filepath.Join(dir, "policy.md")
	require.NoError(t, writeFileForTest(t, src, "# 制度\n\n报销流程：先提交申请。"))

	docID, err := st.AddDocument(Document{
		CollectionID: colA, Title: "policy.md", StoredPath: src, SourcePath: src,
		Ext: ".md", ContentHash: "h", Status: StatusPending,
	})
	require.NoError(t, err)

	idx := NewIndexer(st, nil, model.KBConfig{MaxTextMB: 20}, model.RAGConfig{ChunkSize: 512, ChunkOverlap: 64})
	// Simulate "moved while extracting": the row is extracting under colA,
	// then MoveDocumentRecord re-points it to colB before the chunks land.
	require.NoError(t, st.SetDocumentStatus(docID, StatusExtracting, "", ""))
	expected := colA
	require.NoError(t, st.MoveDocumentRecord(docID, colB, src))

	err = st.ReplaceChunks(docID, []Chunk{{
		DocumentID: docID, CollectionID: expected, ChunkIndex: 0,
		ChunkText: "报销流程", ChunkTextSegmented: rag.SegmentText("报销流程"),
	}}, "", expected)
	require.ErrorIs(t, err, ErrDocumentChanged)

	// The indexer's next pass indexes it in the right place.
	require.True(t, idx.processDocument())
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, doc.Status)
	require.Equal(t, colB, doc.CollectionID)

	hits, err := st.SearchFTS("报销流程", 10, []int64{colB})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	hits, err = st.SearchFTS("报销流程", 10, []int64{colA})
	require.NoError(t, err)
	require.Empty(t, hits)
}

func writeFileForTest(t *testing.T, path, body string) error {
	t.Helper()
	return os.WriteFile(path, []byte(body), 0o644)
}

func TestImportDirectoryStopsOnCancelledContext(t *testing.T) {
	col := newCollectionForImport(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a.md": "甲",
		"b.md": "乙",
		"c.md": "丙",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ImportDirectory(ctx, col.ID, root)
	require.ErrorIs(t, err, context.Canceled)

	docs, err := ListDocuments(col.ID)
	require.NoError(t, err)
	require.Empty(t, docs, "a cancelled import must not copy anything")

	// A live context still imports.
	res, err := ImportDirectory(context.Background(), col.ID, root)
	require.NoError(t, err)
	require.Equal(t, 3, res.Added)
}
