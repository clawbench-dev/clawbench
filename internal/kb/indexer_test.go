package kb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/rag"

	"github.com/stretchr/testify/require"
)

func TestIndexerEndToEnd(t *testing.T) {
	oldDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() {
		Shutdown()
		model.DataDir = oldDir
	})

	cfg := model.KBConfig{
		Enabled:        true,
		AutoInject:     false,
		InjectTopN:     4,
		InjectMaxChars: 2000,
		MaxFileMB:      50,
		MaxTextMB:      20,
		PollInterval:   "20ms",
		EmbedBatchSize: 4,
	}
	ragCfg := model.RAGConfig{VectorEnabled: false, ChunkSize: 512, ChunkOverlap: 64}
	require.NoError(t, Init(cfg, ragCfg))
	StartIndexer()

	col, err := CreateCollection("测试库", "端到端", ScopeGlobal, 0)
	require.NoError(t, err)
	require.NotNil(t, col)

	src := filepath.Join(t.TempDir(), "notes.md")
	require.NoError(t, os.WriteFile(src, []byte("# 标题\n\n这是关于数据库查询优化的说明。\n\n另一段：索引重建。"), 0o644))
	doc, err := AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)
	require.Equal(t, StatusPending, doc.Status)
	require.Greater(t, doc.SizeBytes, int64(0))

	deadline := time.Now().Add(5 * time.Second)
	var ready *Document
	for time.Now().Before(deadline) {
		d, err := GetDocument(doc.ID)
		require.NoError(t, err)
		if d.Status == StatusFailed {
			t.Fatalf("indexing failed: %s", d.Error)
		}
		if d.Status == StatusReady {
			ready = d
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NotNil(t, ready, "document never became ready")
	require.True(t, ready.FTSReady)
	require.False(t, ready.VectorReady)
	require.Greater(t, ready.ChunkCount, 0)

	res, err := Search(context.Background(), SearchParams{Query: "数据库查询优化", ProjectID: 0})
	require.NoError(t, err)
	require.NotEmpty(t, res.Results)
	require.Equal(t, "notes.md", res.Results[0].DocumentTitle)

	// The stored copy is deleted with the document.
	require.NoError(t, DeleteDocument(doc.ID))
	_, err = os.Stat(ready.StoredPath)
	require.True(t, os.IsNotExist(err))
}

func TestIndexerReportsUnsupportedContent(t *testing.T) {
	oldDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() {
		Shutdown()
		model.DataDir = oldDir
	})

	cfg := model.KBConfig{
		Enabled: true, MaxFileMB: 50, MaxTextMB: 20, PollInterval: "20ms", EmbedBatchSize: 4,
	}
	require.NoError(t, Init(cfg, model.RAGConfig{VectorEnabled: false, ChunkSize: 512, ChunkOverlap: 64}))
	StartIndexer()

	col, err := CreateCollection("测试库", "", ScopeGlobal, 0)
	require.NoError(t, err)

	// A .txt file whose bytes are binary passes the extension check but fails
	// during extraction; the document must land in "failed" with a reason.
	src := filepath.Join(t.TempDir(), "blob.txt")
	require.NoError(t, os.WriteFile(src, []byte{0x00, 0x01, 0x02, 0x03}, 0o644))
	doc, err := AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)

	deadline := time.Now().Add(5 * time.Second)
	var failed *Document
	for time.Now().Before(deadline) {
		d, err := GetDocument(doc.ID)
		require.NoError(t, err)
		if d.Status == StatusFailed {
			failed = d
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NotNil(t, failed, "binary document was not marked failed")
	require.NotEmpty(t, failed.Error)
}

func TestAddDocumentRejectsDuplicate(t *testing.T) {
	oldDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() {
		Shutdown()
		model.DataDir = oldDir
	})
	cfg := model.KBConfig{Enabled: true, MaxFileMB: 50, MaxTextMB: 20, PollInterval: "20ms", EmbedBatchSize: 4}
	require.NoError(t, Init(cfg, model.RAGConfig{VectorEnabled: false, ChunkSize: 512, ChunkOverlap: 64}))

	col, err := CreateCollection("测试库", "", ScopeGlobal, 0)
	require.NoError(t, err)
	src := filepath.Join(t.TempDir(), "same.txt")
	require.NoError(t, os.WriteFile(src, []byte("相同内容"), 0o644))

	_, err = AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)
	_, err = AddDocumentFromPath(col.ID, src)
	require.ErrorIs(t, err, ErrDuplicateDocument)
}

func TestIndexerEmbedsPendingChunks(t *testing.T) {
	srv := newFakeEmbedServer(t)
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "a.txt", ContentHash: "h", Status: StatusReady,
	})
	require.NoError(t, err)
	require.NoError(t, st.ReplaceChunks(docID, []Chunk{{
		DocumentID: docID, CollectionID: colID, ChunkIndex: 0,
		ChunkText: "报销流程", ChunkTextSegmented: "报销 流程",
	}}, ""))

	cfg := model.KBConfig{EmbedBatchSize: 4, MaxTextMB: 20}
	idx := NewIndexer(st, rag.NewEmbeddingClient(srv.URL, "test-model", ""), cfg,
		model.RAGConfig{ChunkSize: 512, ChunkOverlap: 64})

	require.True(t, idx.processEmbeddings())
	require.True(t, st.HasVecData())
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.True(t, doc.VectorReady)

	// Nothing left to embed.
	require.False(t, idx.processEmbeddings())
}

func TestIndexerEmbeddingFailureBacksOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	st := newTestStore(t)
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

	idx := NewIndexer(st, rag.NewEmbeddingClient(srv.URL, "m", ""),
		model.KBConfig{EmbedBatchSize: 4, MaxTextMB: 20},
		model.RAGConfig{ChunkSize: 512, ChunkOverlap: 64})

	require.False(t, idx.processEmbeddings())
	// The failure starts a backoff, so the next call does not hit the endpoint.
	require.False(t, idx.processEmbeddings())
}
