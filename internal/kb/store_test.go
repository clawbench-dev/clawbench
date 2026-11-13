package kb

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/rag"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Chinese FTS needs the gse segmenter, exactly as the chat index does.
	_ = rag.InitSegmenter()
	os.Exit(m.Run())
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := NewStoreForTest(filepath.Join(t.TempDir(), "kb.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestCollectionScoping(t *testing.T) {
	st := newTestStore(t)

	globalID, err := st.CreateCollection(Collection{Name: "全局库", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	projectID, err := st.CreateCollection(Collection{Name: "项目库", Scope: ScopeProject, ProjectID: 7, AutoInject: true})
	require.NoError(t, err)

	// No project: only global collections.
	cols, err := st.ListCollections(0)
	require.NoError(t, err)
	require.Len(t, cols, 1)
	require.Equal(t, globalID, cols[0].ID)
	require.Equal(t, ScopeGlobal, cols[0].Scope)

	// Project 7: global + its own.
	cols, err = st.ListCollections(7)
	require.NoError(t, err)
	require.Len(t, cols, 2)

	// Project 8 sees only the global one; the project collection is invisible.
	cols, err = st.ListCollections(8)
	require.NoError(t, err)
	require.Len(t, cols, 1)

	ok, err := st.CollectionAccessible(projectID, 8)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = st.CollectionAccessible(projectID, 7)
	require.NoError(t, err)
	require.True(t, ok)

	// Invalid scope is rejected.
	_, err = st.CreateCollection(Collection{Name: "bad", Scope: "team"})
	require.ErrorIs(t, err, ErrInvalidScope)
}

func TestDocumentLifecycleAndFTS(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "手册", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)

	docID, err := st.AddDocument(Document{
		CollectionID: colID,
		Title:        "员工手册.pdf",
		StoredPath:   "/tmp/does-not-matter.pdf",
		Ext:          ".pdf",
		SizeBytes:    1234,
		ContentHash:  "hash-1",
		Status:       StatusPending,
	})
	require.NoError(t, err)

	// Same content in the same collection is refused by the unique index.
	_, err = st.AddDocument(Document{
		CollectionID: colID,
		Title:        "副本.pdf",
		Ext:          ".pdf",
		ContentHash:  "hash-1",
		Status:       StatusPending,
	})
	require.ErrorIs(t, err, ErrDuplicateDocument)

	found, err := st.FindDocumentByHash(colID, "hash-1")
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, docID, found.ID)

	chunks := []Chunk{
		{
			DocumentID:         docID,
			CollectionID:       colID,
			ProjectID:          0,
			ChunkIndex:         0,
			ChunkText:          "公司的报销流程是先提交申请，再等财务审批。",
			ChunkTextSegmented: rag.SegmentText("公司的报销流程是先提交申请，再等财务审批。"),
			Location:           "第 12 页",
			TokenCount:         20,
		},
		{
			DocumentID:         docID,
			CollectionID:       colID,
			ProjectID:          0,
			ChunkIndex:         1,
			ChunkText:          "年假天数按工龄计算。",
			ChunkTextSegmented: rag.SegmentText("年假天数按工龄计算。"),
			Location:           "第 13 页",
			TokenCount:         10,
		},
	}
	require.NoError(t, st.ReplaceChunks(docID, chunks, ""))

	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, StatusReady, doc.Status)
	require.True(t, doc.FTSReady)
	require.False(t, doc.VectorReady)
	require.Equal(t, 2, doc.ChunkCount)

	ids, err := st.ListCollectionIDs(0)
	require.NoError(t, err)
	require.Equal(t, []int64{colID}, ids)

	hits, err := st.SearchFTS("报销流程", 10, ids)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.Equal(t, "员工手册.pdf", hits[0].DocumentTitle)
	require.Equal(t, "第 12 页", hits[0].Location)
	require.Contains(t, hits[0].ChunkText, "报销")

	// Deleting the document removes its chunks and FTS rows.
	stored, err := st.DeleteDocument(docID)
	require.NoError(t, err)
	require.Equal(t, "/tmp/does-not-matter.pdf", stored)

	hits, err = st.SearchFTS("报销流程", 10, ids)
	require.NoError(t, err)
	require.Empty(t, hits)

	_, err = st.GetDocument(docID)
	require.ErrorIs(t, err, ErrDocumentNotFound)
}

func TestDeleteCollectionCascades(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "临时", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID,
		Title:        "a.txt",
		ContentHash:  "h",
		Status:       StatusReady,
	})
	require.NoError(t, err)
	require.NoError(t, st.ReplaceChunks(docID, []Chunk{{
		DocumentID:         docID,
		CollectionID:       colID,
		ChunkText:          "数据库查询优化",
		ChunkTextSegmented: rag.SegmentText("数据库查询优化"),
		ChunkIndex:         0,
	}}, ""))

	require.NoError(t, st.DeleteCollection(colID))
	_, err = st.GetCollection(colID)
	require.ErrorIs(t, err, ErrCollectionNotFound)

	var docs, chunks int
	require.NoError(t, st.db.QueryRow(`SELECT COUNT(*) FROM kb_documents`).Scan(&docs))
	require.NoError(t, st.db.QueryRow(`SELECT COUNT(*) FROM kb_chunks`).Scan(&chunks))
	require.Zero(t, docs)
	require.Zero(t, chunks)
}

func TestResetDocumentForReindex(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{CollectionID: colID, Title: "a.txt", ContentHash: "h", Status: StatusReady})
	require.NoError(t, err)
	require.NoError(t, st.ReplaceChunks(docID, []Chunk{{
		DocumentID:         docID,
		CollectionID:       colID,
		ChunkText:          "旧内容",
		ChunkTextSegmented: rag.SegmentText("旧内容"),
		ChunkIndex:         0,
	}}, ""))

	require.NoError(t, st.ResetDocumentForReindex(docID))
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, StatusPending, doc.Status)
	require.Zero(t, doc.ChunkCount)

	// Reindexing does not delete the document itself.
	pending, err := st.PendingDocuments(10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, docID, pending[0].ID)
}

func TestStats(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "a.txt", ContentHash: "h", Status: StatusReady, SizeBytes: 2048,
	})
	require.NoError(t, err)
	require.NoError(t, st.ReplaceChunks(docID, []Chunk{{
		DocumentID:         docID,
		CollectionID:       colID,
		ChunkText:          "统计测试",
		ChunkTextSegmented: rag.SegmentText("统计测试"),
		ChunkIndex:         0,
	}}, ""))

	stt, err := st.Stats()
	require.NoError(t, err)
	require.Equal(t, 1, stt.Collections)
	require.Equal(t, 1, stt.Documents)
	require.Equal(t, 1, stt.ReadyDocs)
	require.Equal(t, 1, stt.Chunks)
	require.Equal(t, 0, stt.EmbeddedChunks)
	require.Equal(t, int64(2048), stt.StoredBytes)
}

func TestResetStuckDocuments(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{CollectionID: colID, Title: "a.txt", ContentHash: "h", Status: StatusExtracting})
	require.NoError(t, err)

	require.NoError(t, st.ResetStuckDocuments())
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, StatusPending, doc.Status)
}

func TestUpdateCollection(t *testing.T) {
	st := newTestStore(t)
	id, err := st.CreateCollection(Collection{Name: "旧名", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)

	name := "新名"
	desc := "说明"
	auto := false
	require.NoError(t, st.UpdateCollection(id, &name, &desc, &auto))
	col, err := st.GetCollection(id)
	require.NoError(t, err)
	require.Equal(t, "新名", col.Name)
	require.Equal(t, "说明", col.Description)
	require.False(t, col.AutoInject)

	require.NoError(t, st.UpdateCollection(id, nil, nil, nil))
}

func TestDocumentTimestampsAdvance(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{CollectionID: colID, Title: "a.txt", ContentHash: "h", Status: StatusPending})
	require.NoError(t, err)
	before, err := st.GetDocument(docID)
	require.NoError(t, err)

	time.Sleep(5 * time.Millisecond)
	require.NoError(t, st.SetDocumentStatus(docID, StatusReady, "", "warning"))
	after, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.False(t, after.UpdatedAt.Before(before.UpdatedAt))
	require.Equal(t, "warning", after.Warning)
}

func TestDeleteProjectData(t *testing.T) {
	st := newTestStore(t)
	projectCol, err := st.CreateCollection(Collection{Name: "项目库", Scope: ScopeProject, ProjectID: 7, AutoInject: true})
	require.NoError(t, err)
	globalCol, err := st.CreateCollection(Collection{Name: "全局库", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)

	ids, err := st.DeleteProjectData(7)
	require.NoError(t, err)
	require.Equal(t, []int64{projectCol}, ids)

	_, err = st.GetCollection(projectCol)
	require.ErrorIs(t, err, ErrCollectionNotFound)
	_, err = st.GetCollection(globalCol)
	require.NoError(t, err)

	// The global sentinel never matches a project collection.
	ids, err = st.DeleteProjectData(0)
	require.NoError(t, err)
	require.Empty(t, ids)
}

func TestMissingRowsAndInvalidEmbeddings(t *testing.T) {
	st := newTestStore(t)
	require.ErrorIs(t, st.DeleteCollection(999), ErrCollectionNotFound)
	_, err := st.DeleteDocument(999)
	require.ErrorIs(t, err, ErrDocumentNotFound)
	require.ErrorIs(t, st.ResetDocumentForReindex(999), ErrDocumentNotFound)
	require.Error(t, st.EnsureDimension(0))
	require.Error(t, st.SaveEmbeddings([]ChunkEmbedding{{ChunkID: 1}}))
	require.Error(t, st.SaveEmbeddings([]ChunkEmbedding{{
		ChunkID: 1, Vector: []float64{math.NaN()},
	}}))
}

func TestLoadEmbeddingDimOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kb.db")
	st, err := NewStoreForTest(path)
	require.NoError(t, err)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{CollectionID: colID, Title: "a", ContentHash: "h", Status: StatusReady})
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
	require.NoError(t, st.Close())

	st2, err := NewStoreForTest(path)
	require.NoError(t, err)
	defer func() { _ = st2.Close() }()
	require.True(t, st2.HasVecData())
	// The width was loaded from the stored chunks; asking for the same width
	// must not drop the table.
	require.NoError(t, st2.EnsureDimension(3))
	require.True(t, st2.HasVecData())
}

func TestClosedStoreErrors(t *testing.T) {
	st := newTestStore(t)
	require.NoError(t, st.Close())

	_, err := st.Stats()
	require.Error(t, err)
	_, err = st.PendingDocuments(1)
	require.Error(t, err)
	_, err = st.ListDocuments(1)
	require.Error(t, err)
	_, err = st.DeleteDocument(1)
	require.Error(t, err)
	require.Error(t, st.DeleteCollection(1))
	require.Error(t, st.ReplaceChunks(1, []Chunk{{DocumentID: 1, CollectionID: 1, ChunkIndex: 0, ChunkText: "x", ChunkTextSegmented: "x"}}, ""))
	require.Error(t, st.SaveEmbeddings([]ChunkEmbedding{{ChunkID: 1, Vector: []float64{1, 0, 0}}}))
}
