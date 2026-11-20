package kb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/rag"

	"github.com/stretchr/testify/require"
)

// withTempKB initializes the package against a fresh data directory and leaves
// the store reachable through the facades. Several cases below assert on what
// happens WITHOUT an indexer running (a document stays pending, the refresh
// path does not silently re-index), so the worker is never started here.
func withTempKB(t *testing.T) {
	t.Helper()
	oldDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() {
		Shutdown()
		model.DataDir = oldDir
	})
	cfg := model.KBConfig{
		Enabled: true, AutoInject: true, InjectTopN: 4, InjectMaxChars: 2000,
		MaxFileMB: 50, MaxTextMB: 20, PollInterval: "20ms", EmbedBatchSize: 4,
	}
	require.NoError(t, Init(cfg, model.RAGConfig{VectorEnabled: false, ChunkSize: 512, ChunkOverlap: 64}))
}

// setMaxFileMB shrinks the configured per-file cap so the size paths can be
// reached without writing tens of megabytes.
func setMaxFileMB(t *testing.T, mb int) {
	t.Helper()
	mu.Lock()
	globalCfg.MaxFileMB = mb
	mu.Unlock()
}

// TestAddDocumentFromPath_ReportsMissingFileAndMissingCollection pins the
// preconditions of the add path: a vanished source, an unknown library and a
// directory are three different failures the panel words differently, and all
// three must be decided before a row or a copy exists.
func TestAddDocumentFromPath_ReportsMissingFileAndMissingCollection(t *testing.T) {
	withTempKB(t)
	col, err := CreateCollection("库", "", ScopeGlobal, 0)
	require.NoError(t, err)

	_, err = AddDocumentFromPath(col.ID, filepath.Join(t.TempDir(), "gone.txt"))
	require.ErrorContains(t, err, "read file")

	src := filepath.Join(t.TempDir(), "ok.txt")
	require.NoError(t, os.WriteFile(src, []byte("内容"), 0o644))
	_, err = AddDocumentFromPath(9999, src)
	require.ErrorIs(t, err, ErrCollectionNotFound)

	_, err = AddDocumentFromPath(col.ID, t.TempDir())
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonNotRegular, fe.Reason)

	// Nothing was written for any of the three.
	docs, err := ListDocuments(col.ID)
	require.NoError(t, err)
	require.Empty(t, docs)
}

func TestAddDocumentFromReader_RejectsOversizedUpload(t *testing.T) {
	withTempKB(t)
	col, err := CreateCollection("库", "", ScopeGlobal, 0)
	require.NoError(t, err)
	setMaxFileMB(t, 1)

	big := strings.Repeat("a", (1<<20)+1024)
	_, err = AddDocumentFromReader(col.ID, "big.txt", strings.NewReader(big))
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonFileTooLarge, fe.Reason)
	require.ErrorIs(t, err, ErrFileTooLarge)

	// The partial copy must not stay behind: it would be re-indexed on the next
	// start as a document nobody asked for.
	entries, readErr := os.ReadDir(filepath.Join(KnowledgeDir(), fmt.Sprint(col.ID)))
	if readErr == nil {
		require.Empty(t, entries)
	}
}

// failingReader hands out its bytes and then fails, so the "read upload" branch
// (as opposed to a clean EOF) is exercised: a broken client connection must be
// an error, not an empty document.
type failingReader struct{ done bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, fmt.Errorf("connection reset")
	}
	r.done = true
	return copy(p, "partial"), nil
}

func TestAddDocumentFromReader_ReportsBrokenStream(t *testing.T) {
	withTempKB(t)
	col, err := CreateCollection("库", "", ScopeGlobal, 0)
	require.NoError(t, err)

	_, err = AddDocumentFromReader(col.ID, "broken.txt", &failingReader{})
	require.ErrorContains(t, err, "read upload")
}

// TestMoveDocument_RejectsUnknownTargets covers the guards ahead of the move.
// The happy path and the duplicate case live in store_test.go; what is added
// here is that an unknown document or library is refused BEFORE the stored
// copy is touched, and that the copy lands under the target's directory (a row
// pointing at the old directory would index a file the new library cannot find).
func TestMoveDocument_RejectsUnknownTargets(t *testing.T) {
	withTempKB(t)
	source, err := CreateCollection("旧库", "", ScopeGlobal, 0)
	require.NoError(t, err)
	target, err := CreateCollection("新库", "", ScopeGlobal, 0)
	require.NoError(t, err)

	src := filepath.Join(t.TempDir(), "policy.txt")
	require.NoError(t, os.WriteFile(src, []byte("报销流程"), 0o644))
	doc, err := AddDocumentFromPath(source.ID, src)
	require.NoError(t, err)
	original := doc.StoredPath

	require.ErrorIs(t, MoveDocument(9999, target.ID), ErrDocumentNotFound)
	require.ErrorIs(t, MoveDocument(doc.ID, 9999), ErrCollectionNotFound)

	still, err := GetDocument(doc.ID)
	require.NoError(t, err)
	require.Equal(t, source.ID, still.CollectionID)
	require.Equal(t, original, still.StoredPath)

	require.NoError(t, MoveDocument(doc.ID, target.ID))
	moved, err := GetDocument(doc.ID)
	require.NoError(t, err)
	require.Equal(t, target.ID, moved.CollectionID)
	require.Contains(t, moved.StoredPath, filepath.Join("knowledge", fmt.Sprint(target.ID)))
	_, statErr := os.Stat(original)
	require.True(t, os.IsNotExist(statErr), "the old copy must not be left behind")
}

// Refresh's unchanged / changed / duplicate paths live in refresh_test.go. What
// is added here are the preconditions that decide whether a refresh can run at
// all: the panel distinguishes "uploaded, no original", "original deleted" and
// "original is no longer a file".
func TestRefreshDocumentFromSource_SourcePreconditions(t *testing.T) {
	withTempKB(t)
	col, err := CreateCollection("库", "", ScopeGlobal, 0)
	require.NoError(t, err)
	srcDir := t.TempDir()

	upload, err := AddDocumentFromReader(col.ID, "note.txt", strings.NewReader("来自上传"))
	require.NoError(t, err)
	_, err = RefreshDocumentFromSource(upload.ID)
	require.ErrorIs(t, err, ErrSourceMissing, "an upload has no original to re-read")

	src := filepath.Join(srcDir, "policy.txt")
	require.NoError(t, os.WriteFile(src, []byte("第一版"), 0o644))
	doc, err := AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)
	require.NoError(t, os.Remove(src))
	_, err = RefreshDocumentFromSource(doc.ID)
	require.ErrorIs(t, err, ErrSourceMissing)

	// A row whose source is a directory (possible after the user repoints it)
	// hits the same regular-file rule the add path uses.
	dirDocID, err := currentStore().AddDocument(Document{
		CollectionID: col.ID, Title: "folder", SourcePath: srcDir, StoredPath: srcDir,
		Ext: ".txt", Status: StatusPending,
	})
	require.NoError(t, err)
	_, err = RefreshDocumentFromSource(dirDocID)
	require.ErrorIs(t, err, ErrUnsupportedFile)
}

// TestListDocuments_DecoratesSourceState covers the two read-time flags the
// panel uses to offer or hide "update from original". Both are derived at read
// time, so they must follow the file system without any stored column.
func TestListDocuments_DecoratesSourceState(t *testing.T) {
	withTempKB(t)
	col, err := CreateCollection("库", "", ScopeGlobal, 0)
	require.NoError(t, err)

	src := filepath.Join(t.TempDir(), "policy.txt")
	require.NoError(t, os.WriteFile(src, []byte("v1"), 0o644))
	doc, err := AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)

	docs, err := ListDocuments(col.ID)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.False(t, docs[0].SourceMissing)
	require.False(t, docs[0].SourceStale)

	// Edited after the copy was taken: the panel offers "update from original".
	older := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(doc.StoredPath, older, older))
	docs, err = ListDocuments(col.ID)
	require.NoError(t, err)
	require.True(t, docs[0].SourceStale)

	require.NoError(t, os.Remove(src))
	docs, err = ListDocuments(col.ID)
	require.NoError(t, err)
	require.True(t, docs[0].SourceMissing)
	require.False(t, docs[0].SourceStale)

	// An upload has no source path, so the stat is skipped and the row keeps
	// reporting the document as complete.
	upload, err := AddDocumentFromReader(col.ID, "upload.txt", strings.NewReader("x"))
	require.NoError(t, err)
	require.Empty(t, upload.SourcePath)
	fetched, err := GetDocument(upload.ID)
	require.NoError(t, err)
	require.False(t, fetched.SourceMissing)
	require.False(t, fetched.SourceStale)
}

func TestDeleteDocument_AndUnavailableFacades(t *testing.T) {
	withTempKB(t)
	col, err := CreateCollection("库", "", ScopeGlobal, 0)
	require.NoError(t, err)
	src := filepath.Join(t.TempDir(), "a.txt")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0o644))
	doc, err := AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)
	require.NoError(t, DeleteDocument(doc.ID))
	require.ErrorIs(t, DeleteDocument(doc.ID), ErrDocumentNotFound)

	Shutdown()

	// Every facade reports the same typed error once the feature is off; the
	// HTTP layer maps it to 503 and the panel hides the tab. A facade that
	// forgot this check would panic on a nil store instead.
	_, err = AddDocumentFromPath(col.ID, src)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = AddDocumentFromReader(col.ID, "a.txt", strings.NewReader("x"))
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = AddDocumentFromURL(t.Context(), col.ID, "https://example.com")
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, MoveDocument(1, 2), ErrUnavailable)
	_, err = RefreshDocumentFromSource(1)
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, DeleteDocument(1), ErrUnavailable)
	require.ErrorIs(t, DeleteCollection(1), ErrUnavailable)
	require.ErrorIs(t, ReindexDocument(1), ErrUnavailable)
	require.ErrorIs(t, ResetVectorIndex(), ErrUnavailable)
	require.ErrorIs(t, ReindexCollections(nil), ErrUnavailable)
	_, err = ListDocuments(1)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = GetDocument(1)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = GetCollection(1)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = CreateCollection("x", "", ScopeGlobal, 0)
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, UpdateCollection(1, nil, nil, nil), ErrUnavailable)
	_, err = CollectionAccessible(1, 0)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = ListCollections(0)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = ListCollectionIDs(0)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = DocumentChunks(1, 10)
	require.ErrorIs(t, err, ErrUnavailable)
	_, _, err = DocumentChunkWindow(1, 1, 1, 1)
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, SetDocumentTags(1, ""), ErrUnavailable)
	_, err = ListDocumentUsage(1)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = StatsForProject(0)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = ListDocumentProgress(1)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = Search(t.Context(), SearchParams{Query: "x"})
	require.ErrorIs(t, err, ErrUnavailable)
	// Best-effort helpers must not panic when the feature is off.
	RecordDocumentUsage([]Ref{{DocumentID: 1}}, "session")
	DeleteProjectData(1)
	StartIndexer()
}

func TestReindexCollections_EmptyListIsNotEverything(t *testing.T) {
	withTempKB(t)
	col, err := CreateCollection("库", "", ScopeGlobal, 0)
	require.NoError(t, err)
	src := filepath.Join(t.TempDir(), "a.txt")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0o644))
	doc, err := AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)

	// A project with no visible library resolves to an empty (non-nil) list;
	// treating that as "all" would wipe the whole installation's index.
	require.NoError(t, ReindexCollections([]int64{}))

	// Store-level errors travel through the facade instead of being swallowed.
	require.ErrorIs(t, ReindexDocument(9999), ErrDocumentNotFound)
	require.ErrorIs(t, SetDocumentTags(9999, "a"), ErrDocumentNotFound)
	require.NoError(t, DeleteCollection(col.ID))
	_, err = GetDocument(doc.ID)
	require.ErrorIs(t, err, ErrDocumentNotFound)
}

// embedServer answers /v1/embeddings with a fixed-width vector and can be told
// to fail.
type embedServer struct {
	*httptest.Server
	fail bool
}

func newEmbedServer(t *testing.T, dim int) *embedServer {
	t.Helper()
	s := &embedServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		vec := make([]float64, dim)
		vec[0] = 1
		data := make([]map[string]any, 0, len(body.Input))
		for i := range body.Input {
			data = append(data, map[string]any{"index": i, "embedding": vec})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(s.Close)
	return s
}

// TestReconcileEmbeddingDimensionThroughTheFacade pins the wiring the store
// tests cannot see: a nil store/embedder pair (the disabled case) is a no-op,
// a library that was never embedded is left alone, and errText distinguishes a
// missing error from a real one.
func TestReconcileEmbeddingDimensionThroughTheFacade(t *testing.T) {
	st := newTestStore(t)
	reconcileWG.Add(1)
	reconcileEmbeddingDimension(nil, nil)
	require.Equal(t, "", errText(nil))
	require.Equal(t, "boom", errText(fmt.Errorf("boom")))

	// Nothing was ever embedded: the table is created by the first batch, so the
	// reconcile must neither create one nor probe the endpoint.
	srv := newEmbedServer(t, 3)
	reconcileWG.Add(1)
	reconcileEmbeddingDimension(st, rag.NewEmbeddingClient(srv.URL, "m", ""))
	_, have := st.vecDimFromSchema()
	require.False(t, have)
}

// TestReconfigureWithEmbedderStartsAndStops drives Reconfigure along the path a
// settings save takes when the user turns vector search on: the embedder is
// rebuilt from the live config, the old worker is stopped, and Shutdown waits
// for the background reconcile before it closes the store.
func TestReconfigureWithEmbedderStartsAndStops(t *testing.T) {
	withTempKB(t)
	srv := newEmbedServer(t, 5)
	cfg := model.KBConfig{Enabled: true, MaxFileMB: 50, MaxTextMB: 20, PollInterval: "20ms", EmbedBatchSize: 4}
	ragCfg := model.RAGConfig{VectorEnabled: true, BaseURL: srv.URL, Model: "m", ChunkSize: 512, ChunkOverlap: 64}
	Reconfigure(cfg, ragCfg)
	require.True(t, VectorEnabled(), "an embedder must be built from the live config")
	StartIndexer()
	Shutdown()
	require.False(t, Available())

	// Re-enabling from a closed state re-runs Init (the store-nil branch).
	Reconfigure(cfg, ragCfg)
	require.True(t, Available())
	require.True(t, VectorEnabled())
}

func TestMaxFileBytesDefaultsWhenUnset(t *testing.T) {
	withTempKB(t)
	mu.Lock()
	globalCfg.MaxFileMB = 0
	mu.Unlock()
	require.Equal(t, int64(50)<<20, MaxFileBytes(), "an unset cap must fall back, not become 0 (which rejects everything)")
	setMaxFileMB(t, 7)
	require.Equal(t, int64(7)<<20, MaxFileBytes())
}
