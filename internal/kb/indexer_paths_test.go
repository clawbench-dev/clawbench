package kb

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/rag"

	"github.com/stretchr/testify/require"
)

// embedServer answers embedding requests with a fixed vector per input and
// records how many requests it saw. countingHandler may be nil.
func startEmbedServer(t *testing.T, vec []float64, onRequest func()) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if onRequest != nil {
			onRequest()
		}
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

// A document whose collection disappeared between being queued and being read
// must be marked failed with that reason: the row cannot be indexed anywhere,
// and leaving it pending would retry forever.
func TestIndexerProcessDocument_CollectionGoneMarksFailed(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal})
	require.NoError(t, err)
	src := filepath.Join(t.TempDir(), "policy.txt")
	require.NoError(t, os.WriteFile(src, []byte("报销流程：先提交申请。"), 0o644))
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "policy.txt", StoredPath: src, Ext: ".txt",
		ContentHash: "h", Status: StatusPending,
	})
	require.NoError(t, err)

	// The collection row is gone but the document row still points at it.
	foreignKeys, err := st.db.Query(`PRAGMA foreign_keys`)
	require.NoError(t, err)
	defer func() { _ = foreignKeys.Close() }()
	require.True(t, foreignKeys.Next())
	var fkOn int
	require.NoError(t, foreignKeys.Scan(&fkOn))
	require.NoError(t, foreignKeys.Err())
	require.Zero(t, fkOn, "precondition: deleting a collection must leave its documents behind")

	_, err = st.db.Exec(`DELETE FROM kb_collections WHERE id = ?`, colID)
	require.NoError(t, err)

	idx := NewIndexer(st, nil, model.KBConfig{MaxTextMB: 20}, model.RAGConfig{ChunkSize: 512, ChunkOverlap: 64})
	require.True(t, idx.processDocument(), "the document is claimed, then failed")

	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, StatusFailed, doc.Status)
	require.Contains(t, doc.Error, "not found", "the reason names the missing collection")
}

// An extraction failure stamps the document failed with the extractor's reason,
// so the panel can translate it and offer a retry.
func TestIndexerProcessDocument_ExtractFailureMarksFailed(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal})
	require.NoError(t, err)
	src := filepath.Join(t.TempDir(), "broken.docx")
	require.NoError(t, os.WriteFile(src, []byte("not a zip"), 0o644))
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "broken.docx", StoredPath: src, Ext: ".docx",
		ContentHash: "h", Status: StatusPending,
	})
	require.NoError(t, err)

	idx := NewIndexer(st, nil, model.KBConfig{MaxTextMB: 20}, model.RAGConfig{ChunkSize: 512, ChunkOverlap: 64})
	require.True(t, idx.processDocument())

	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, StatusFailed, doc.Status)
	require.NotEmpty(t, doc.Error, "the failure reason must be stored for the panel")
}

// drain keeps going until nothing is left: one call must index every queued
// document and embed every pending chunk, not one item per tick.
func TestIndexerDrain_ProcessesEverythingInOnePass(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal})
	require.NoError(t, err)

	var mu sync.Mutex
	requests := 0
	srv := startEmbedServer(t, []float64{1, 0, 0}, func() {
		mu.Lock()
		requests++
		mu.Unlock()
	})
	emb := rag.NewEmbeddingClient(srv.URL, "m", "")
	idx := NewIndexer(st, emb, model.KBConfig{MaxTextMB: 20, EmbedBatchSize: 2}, model.RAGConfig{ChunkSize: 512, ChunkOverlap: 64})

	dir := t.TempDir()
	for i := range 5 {
		src := filepath.Join(dir, "doc"+itoa(i)+".txt")
		require.NoError(t, os.WriteFile(src, []byte("报销流程：先提交申请。第"+itoa(i)+"份。"), 0o644))
		_, err := st.AddDocument(Document{
			CollectionID: colID, Title: "doc" + itoa(i) + ".txt", StoredPath: src, Ext: ".txt",
			ContentHash: "h" + itoa(i), Status: StatusPending,
		})
		require.NoError(t, err)
	}

	idx.drain()

	pending, err := st.PendingDocuments(10)
	require.NoError(t, err)
	require.Empty(t, pending, "every queued document must be indexed")

	chunks, err := st.PendingEmbeddingChunks(100)
	require.NoError(t, err)
	require.Empty(t, chunks, "every chunk must be embedded")
	require.True(t, st.HasVecData())
	require.Greater(t, requests, 1, "batches of 2 mean five documents take several requests")

	// A drain with nothing left does nothing and returns immediately.
	idx.drain()
}

// A batch whose vectors are all empty is not progress: the chunks stay pending
// and the worker must not spin on them. The failure also starts the backoff.
func TestIndexerProcessEmbeddings_EmptyVectorsAreNotProgress(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal})
	require.NoError(t, err)
	seedDoc(t, st, colID, "a.md", "报销流程", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// One result with an empty embedding: the endpoint answered, but with
		// nothing usable.
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[]}]}`))
	}))
	defer srv.Close()
	idx := NewIndexer(st, rag.NewEmbeddingClient(srv.URL, "m", ""), model.KBConfig{EmbedBatchSize: 4}, model.RAGConfig{})

	require.False(t, idx.processEmbeddings(), "an all-empty batch is not progress")
	pending, err := st.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.NotEmpty(t, pending, "the chunks stay pending for the next attempt")
}

// A failed save backs off for 30 seconds: a database that rejects the write must
// not be hammered once per drain pass, and the endpoint must not be called again
// while the worker is backing off. Closing the store after a successful read is
// the way to make ONLY the write fail.
func TestIndexerProcessEmbeddings_FailureStartsTheBackoff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kb.db")
	st, err := NewStoreForTest(path)
	require.NoError(t, err)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal})
	require.NoError(t, err)
	seedDoc(t, st, colID, "a.md", "报销流程", "")

	var mu sync.Mutex
	requests := 0
	srv := startEmbedServer(t, []float64{1, 0, 0}, func() {
		mu.Lock()
		requests++
		mu.Unlock()
	})
	idx := NewIndexer(st, rag.NewEmbeddingClient(srv.URL, "m", ""), model.KBConfig{EmbedBatchSize: 4}, model.RAGConfig{})

	// Precondition only: the chunk the worker is about to save exists. The
	// store's own handle is then closed so the worker's read fails and the
	// failure path (backoff + no further endpoint calls) is entered.
	sidecar, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer func() { _ = sidecar.Close() }()
	var n int
	require.NoError(t, sidecar.QueryRow(`SELECT COUNT(*) FROM kb_chunks WHERE has_embedding = 0`).Scan(&n))
	require.Positive(t, n)
	require.NoError(t, st.Close())

	require.False(t, idx.processEmbeddings(), "a write against a closed store is not progress")

	mu.Lock()
	before := requests
	mu.Unlock()
	require.False(t, idx.processEmbeddings())
	mu.Lock()
	after := requests
	mu.Unlock()
	require.Equal(t, before, after, "the worker must not call the endpoint again while backing off")
}

// loadEmbeddingDimFromDB keeps a reopened store's width, so a restart does not
// rebuild the vector table from scratch.
func TestLoadEmbeddingDimFromDB_ReadsTheStoredWidth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kb.db")
	first, err := NewStoreForTest(path)
	require.NoError(t, err)
	colID, err := first.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal})
	require.NoError(t, err)
	docID := seedDoc(t, first, colID, "a.md", "报销流程", "")
	pending, err := first.PendingEmbeddingChunks(10)
	require.NoError(t, err)
	require.NoError(t, first.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID: pending[0].ID, DocumentID: docID, CollectionID: colID,
		Vector: []float64{1, 0, 0, 0},
	}}))
	require.NoError(t, first.Close())

	second, err := NewStoreForTest(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	require.Equal(t, 4, second.embeddingDim(), "the width is restored from the chunks")
}

// embeddingDim reads the store's cached width under its lock.
func (s *Store) embeddingDim() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.embDim
}

// A store whose chunks table is missing entirely (a database created by a much
// older build) must still open: the width is simply unknown until the schema is
// migrated. Nothing here may panic on the failed scan.
func TestLoadEmbeddingDimFromDB_BrokenQueryLeavesTheWidthUnset(t *testing.T) {
	raw, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "empty.db"))
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()
	st := &Store{db: raw, writeMu: noOpLocker{}}
	st.loadEmbeddingDimFromDB()
	require.Zero(t, st.embeddingDim())
}

// The poll interval from the config drives the ticker, and the WORKER (not a
// panic on a zero-duration ticker) must come up for every value: "1ms" polls,
// while an empty, unparsable or negative value falls back to the default.
func TestIndexerRun_UsesTheConfiguredPollInterval(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal})
	require.NoError(t, err)
	src := filepath.Join(t.TempDir(), "poll.txt")
	require.NoError(t, os.WriteFile(src, []byte("报销流程：轮询"), 0o644))

	for i, interval := range []string{"1ms", "", "not-a-duration", "-5s"} {
		docID, err := st.AddDocument(Document{
			CollectionID: colID, Title: "poll" + itoa(i) + ".txt", StoredPath: src, Ext: ".txt",
			ContentHash: "poll-" + itoa(i), Status: StatusPending,
		})
		require.NoError(t, err)

		idx := NewIndexer(st, nil, model.KBConfig{MaxTextMB: 20, PollInterval: interval}, model.RAGConfig{})
		idx.Start()
		if interval == "1ms" {
			// Only a real tick can pick this one up: the wake channel is never
			// used here, so this is what proves the ticker interval was applied.
			require.Eventually(t, func() bool {
				doc, getErr := st.GetDocument(docID)
				return getErr == nil && doc.Status == StatusReady
			}, 5*time.Second, 10*time.Millisecond, "a 1ms poll interval must index the document")
		}
		idx.Stop()
	}
}

// Stop must return promptly even when the poll interval is an hour and nobody
// triggered: the loop has to watch its stop channel, not only the ticker.
func TestIndexerRun_StopsPromptlyOnStop(t *testing.T) {
	st := newTestStore(t)
	idx := NewIndexer(st, nil, model.KBConfig{PollInterval: "1h"}, model.RAGConfig{})
	idx.Start()

	done := make(chan struct{})
	go func() {
		idx.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return: the loop is not watching its stop channel")
	}
}
