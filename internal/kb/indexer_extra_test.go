package kb

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/rag"

	"github.com/stretchr/testify/require"
)

// TestIndexerStopIsIdempotentAndTerminates pins the worker's exit rule: Stop
// closes the channel and the loop must leave between two passes. An indexer
// that kept running after Shutdown would write into a store that is already
// closed (the "sql: database is closed" crashes seen in the logs).
func TestIndexerStopIsIdempotentAndTerminates(t *testing.T) {
	st := newTestStore(t)
	idx := NewIndexer(st, nil, model.KBConfig{PollInterval: "1h"}, model.RAGConfig{})
	idx.Start()
	// The ticker never fires within the test, so the only way out is the stop
	// channel; Stop blocks until the goroutine has actually left.
	idx.Stop()
	idx.Stop() // the once-guard makes a second Stop a no-op instead of a double close
	idx.Trigger()
}

// TestIndexerProcessDocument_PendingPath covers the two outcomes of a pass that
// do not depend on the extractor's format handling: an empty queue claims
// nothing, and a plain text file becomes ready chunks.
func TestIndexerProcessDocument_PendingPath(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	idx := NewIndexer(st, nil, model.KBConfig{MaxTextMB: 20}, model.RAGConfig{ChunkSize: 512, ChunkOverlap: 64})

	// Nothing queued: the pass reports "no progress" so drain() can stop.
	require.False(t, idx.processDocument())

	src := filepath.Join(t.TempDir(), "policy.txt")
	require.NoError(t, writeFileForTest(t, src, "报销流程：先提交申请。\n再等财务审批。"))
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "policy.txt", StoredPath: src, Ext: ".txt",
		ContentHash: "h1", Status: StatusPending,
	})
	require.NoError(t, err)

	require.True(t, idx.processDocument())
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, doc.Status)
	require.Positive(t, doc.ChunkCount)
	require.True(t, doc.FTSReady)

	require.False(t, idx.processDocument(), "the queue must be empty again")
}

// TestIndexerProcessDocument_ReportsNoText: a document whose extractor finds
// nothing is marked failed with the stable reason CODE, not the English
// sentinel text, because the panel translates the code.
func TestIndexerProcessDocument_ReportsNoText(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	idx := NewIndexer(st, nil, model.KBConfig{MaxTextMB: 20}, model.RAGConfig{ChunkSize: 512, ChunkOverlap: 64})

	blank := filepath.Join(t.TempDir(), "blank.txt")
	require.NoError(t, writeFileForTest(t, blank, "\n\n\n"))
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "blank.txt", StoredPath: blank, Ext: ".txt",
		ContentHash: "h2", Status: StatusPending,
	})
	require.NoError(t, err)

	require.True(t, idx.processDocument())
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, StatusFailed, doc.Status)
	require.Contains(t, doc.Error, ReasonNoText)
	require.NotContains(t, doc.Error, ErrNoText.Error(),
		"the stored reason is the code the panel translates, not the English sentence")
}

// TestIndexerProcessEmbeddings_RejectsMismatchedBatch: an endpoint that answers
// with fewer vectors than it was given must not be saved — RAG pairs vectors
// with chunks by index, so a short batch would attach one chunk's vector to
// another chunk.
func TestIndexerProcessEmbeddings_RejectsMismatchedBatch(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "a.txt", ContentHash: "mismatch", Status: StatusReady,
	})
	require.NoError(t, err)
	require.NoError(t, st.ReplaceChunks(docID, []Chunk{
		{DocumentID: docID, CollectionID: colID, ChunkIndex: 0, ChunkText: "一", ChunkTextSegmented: "一"},
		{DocumentID: docID, CollectionID: colID, ChunkIndex: 1, ChunkText: "二", ChunkTextSegmented: "二"},
	}, "", colID))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// More entries than the request has chunks, so the check under test is the
		// indexer's own count comparison and not the server's.
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":[{"index":0,"embedding":[1,0,0]},{"index":1,"embedding":[0,1,0]},{"index":2,"embedding":[0,0,0.0]},{"index":3,"embedding":[]}]}`)
	}))
	t.Cleanup(srv.Close)

	idx := NewIndexer(st, rag.NewEmbeddingClient(srv.URL, "m", ""),
		model.KBConfig{EmbedBatchSize: 4, MaxTextMB: 20}, model.RAGConfig{ChunkSize: 512, ChunkOverlap: 64})

	require.False(t, idx.processEmbeddings())
	require.False(t, st.HasVecData(), "a short batch must not be written")
	require.False(t, idx.processEmbeddings(), "the failure must start a backoff")
}

// TestIndexerEmbeddingBackoffRecovers: after the backoff window the same
// endpoint is used again. The window is the point of the backoff (a dead
// endpoint must not be retried on every poll tick), so it is simulated by
// moving the recorded failure into the past rather than sleeping.
func TestIndexerEmbeddingBackoffRecovers(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "a.txt", ContentHash: "backoff", Status: StatusReady,
	})
	require.NoError(t, err)
	require.NoError(t, st.ReplaceChunks(docID, []Chunk{{
		DocumentID: docID, CollectionID: colID, ChunkIndex: 0,
		ChunkText: "报销流程", ChunkTextSegmented: "报销 流程",
	}}, "", colID))

	srv := newEmbedServer(t, 3)
	idx := NewIndexer(st, rag.NewEmbeddingClient(srv.URL, "m", ""),
		model.KBConfig{EmbedBatchSize: 4, MaxTextMB: 20}, model.RAGConfig{ChunkSize: 512, ChunkOverlap: 64})

	srv.fail = true
	require.False(t, idx.processEmbeddings())
	require.False(t, st.HasVecData())
	require.False(t, idx.lastEmbedFail.IsZero(), "a failed batch must record when it failed")

	srv.fail = false
	idx.lastEmbedFail = time.Now().Add(-31 * time.Second)
	require.True(t, idx.processEmbeddings())
	require.True(t, st.HasVecData())
	require.True(t, idx.lastEmbedFail.IsZero(), "a successful batch must clear the backoff")
}
