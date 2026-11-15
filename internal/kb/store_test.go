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

func TestStatsForProject(t *testing.T) {
	st := newTestStore(t)

	// One global collection, one owned by project 7, one by project 8. Each
	// holds a single ready document with one chunk.
	add := func(name string, scope string, projectID int64, hash string) {
		colID, err := st.CreateCollection(Collection{Name: name, Scope: scope, ProjectID: projectID, AutoInject: true})
		require.NoError(t, err)
		docID, err := st.AddDocument(Document{
			CollectionID: colID, Title: name + ".txt", ContentHash: hash, Status: StatusReady, SizeBytes: 100,
		})
		require.NoError(t, err)
		require.NoError(t, st.ReplaceChunks(docID, []Chunk{{
			DocumentID:         docID,
			CollectionID:       colID,
			ProjectID:          projectID,
			ChunkText:          name,
			ChunkTextSegmented: rag.SegmentText(name),
			ChunkIndex:         0,
		}}, ""))
	}
	add("global", ScopeGlobal, 0, "g")
	add("own", ScopeProject, 7, "o")
	add("other", ScopeProject, 8, "x")

	// Whole installation: everything.
	all, err := st.Stats()
	require.NoError(t, err)
	require.Equal(t, 3, all.Collections)
	require.Equal(t, 3, all.Documents)
	require.Equal(t, 3, all.Chunks)
	require.Equal(t, int64(300), all.StoredBytes)

	// Project 7: global + its own. The project-8 collection must not leak into
	// the counters the panel shows next to a list that cannot see it.
	own, err := st.StatsForProject(7)
	require.NoError(t, err)
	require.Equal(t, 2, own.Collections)
	require.Equal(t, 2, own.Documents)
	require.Equal(t, 2, own.Chunks)
	require.Equal(t, int64(200), own.StoredBytes)

	// A project with no collections of its own still sees the global one.
	other, err := st.StatsForProject(9)
	require.NoError(t, err)
	require.Equal(t, 1, other.Collections)
	require.Equal(t, 1, other.Documents)

	// 0 is the settings-page scope: installation-wide, same as Stats().
	fallback, err := st.StatsForProject(0)
	require.NoError(t, err)
	require.Equal(t, all, fallback)
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
	require.NoError(t, st.UpdateCollection(id, &name, &desc, &auto, nil))
	col, err := st.GetCollection(id)
	require.NoError(t, err)
	require.Equal(t, "新名", col.Name)
	require.Equal(t, "说明", col.Description)
	require.False(t, col.AutoInject)

	require.NoError(t, st.UpdateCollection(id, nil, nil, nil, nil))
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

func TestCollectionHealthCounts(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "手册", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)

	add := func(hash string) int64 {
		id, err := st.AddDocument(Document{
			CollectionID: colID,
			Title:        hash + ".txt",
			Ext:          ".txt",
			ContentHash:  hash,
			Status:       StatusPending,
		})
		require.NoError(t, err)
		return id
	}
	failed := add("h1")
	extracting := add("h2")
	queued := add("h3")
	ready := add("h4")
	require.NoError(t, st.SetDocumentStatus(failed, StatusFailed, "no_text", ""))
	require.NoError(t, st.SetDocumentStatus(extracting, StatusExtracting, "", ""))
	require.NoError(t, st.SetDocumentStatus(queued, StatusPending, "", ""))
	require.NoError(t, st.SetDocumentStatus(ready, StatusReady, "", ""))

	cols, err := st.ListCollections(0)
	require.NoError(t, err)
	require.Len(t, cols, 1)
	require.Equal(t, 4, cols[0].DocCount)
	require.Equal(t, 1, cols[0].FailedCount)
	// pending and extracting both read as "still working" to the user.
	require.Equal(t, 2, cols[0].PendingCount)

	col, err := st.GetCollection(colID)
	require.NoError(t, err)
	require.Equal(t, 1, col.FailedCount)
	require.Equal(t, 2, col.PendingCount)
}

func TestChunkWindow(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "手册", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID,
		Title:        "long.txt",
		Ext:          ".txt",
		ContentHash:  "hash",
		Status:       StatusPending,
	})
	require.NoError(t, err)

	chunks := make([]Chunk, 12)
	for i := range chunks {
		chunks[i] = Chunk{
			DocumentID:         docID,
			CollectionID:       colID,
			ChunkIndex:         i,
			ChunkText:          "段" + string(rune('A'+i)),
			ChunkTextSegmented: "段",
			Location:           "L" + string(rune('0'+i)),
		}
	}
	require.NoError(t, st.ReplaceChunks(docID, chunks, ""))
	all, err := st.ListChunks(docID, 20)
	require.NoError(t, err)
	require.Len(t, all, 12)

	window, target, err := st.ChunkWindow(docID, all[8].ID, 2, 2)
	require.NoError(t, err)
	require.Equal(t, 8, target)
	require.Len(t, window, 5)
	require.Equal(t, 6, window[0].ChunkIndex)
	require.Equal(t, 10, window[4].ChunkIndex)

	// Near the start the window is clamped, not padded.
	window, target, err = st.ChunkWindow(docID, all[1].ID, 2, 2)
	require.NoError(t, err)
	require.Equal(t, 1, target)
	require.Equal(t, 0, window[0].ChunkIndex)

	// A chunk that no longer exists falls back instead of erroring.
	window, target, err = st.ChunkWindow(docID, 999999, 2, 2)
	require.NoError(t, err)
	require.Equal(t, -1, target)
	require.Nil(t, window)
}

func TestDocumentProgress(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "手册", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID,
		Title:        "scan.pdf",
		Ext:          ".pdf",
		ContentHash:  "h1",
		Status:       StatusPending,
	})
	require.NoError(t, err)
	require.NoError(t, st.SetDocumentStatus(docID, StatusFailed, "pdf_no_text: scan", ""))

	progress, err := st.DocumentProgress(colID)
	require.NoError(t, err)
	require.Len(t, progress, 1)
	require.Equal(t, docID, progress[0].ID)
	require.Equal(t, StatusFailed, progress[0].Status)
	require.Equal(t, "pdf_no_text: scan", progress[0].Error)
	require.False(t, progress[0].FTSReady)
	require.Zero(t, progress[0].ChunkCount)

	// An unrelated collection's documents do not leak in.
	other, err := st.CreateCollection(Collection{Name: "别的", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	progress, err = st.DocumentProgress(other)
	require.NoError(t, err)
	require.Empty(t, progress)
}

func TestCollectionPinning(t *testing.T) {
	st := newTestStore(t)
	first, err := st.CreateCollection(Collection{Name: "乙", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	second, err := st.CreateCollection(Collection{Name: "甲", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)

	// Pinned first, regardless of creation order.
	pinned := true
	require.NoError(t, st.UpdateCollection(second, nil, nil, nil, &pinned))

	cols, err := st.ListCollections(0)
	require.NoError(t, err)
	require.Len(t, cols, 2)
	require.Equal(t, second, cols[0].ID)
	require.True(t, cols[0].Pinned)
	require.Equal(t, first, cols[1].ID)
	require.False(t, cols[1].Pinned)

	col, err := st.GetCollection(second)
	require.NoError(t, err)
	require.True(t, col.Pinned)

	// Unpinning restores creation order.
	unpinned := false
	require.NoError(t, st.UpdateCollection(second, nil, nil, nil, &unpinned))
	cols, err = st.ListCollections(0)
	require.NoError(t, err)
	require.Equal(t, first, cols[0].ID)
}

func TestSourceStateFlags(t *testing.T) {
	col := newCollectionForImport(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "policy.md")
	require.NoError(t, os.WriteFile(src, []byte("第一版"), 0o644))
	doc, err := AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)

	fresh, err := ListDocuments(col.ID)
	require.NoError(t, err)
	require.Len(t, fresh, 1)
	require.False(t, fresh[0].SourceStale)
	require.False(t, fresh[0].SourceMissing)

	// A source newer than the stored copy is stale.
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(src, future, future))
	stale, err := ListDocuments(col.ID)
	require.NoError(t, err)
	require.True(t, stale[0].SourceStale)

	// A deleted source is reported separately; the copy stays usable.
	require.NoError(t, os.Remove(src))
	missing, err := ListDocuments(col.ID)
	require.NoError(t, err)
	require.True(t, missing[0].SourceMissing)
	require.False(t, missing[0].SourceStale)
	_, err = os.Stat(doc.StoredPath)
	require.NoError(t, err)
}

func TestMoveDocument(t *testing.T) {
	source := newCollectionForImport(t)
	target, err := CreateCollection("目标库", "", ScopeGlobal, 0)
	require.NoError(t, err)

	dir := t.TempDir()
	file := filepath.Join(dir, "policy.md")
	require.NoError(t, os.WriteFile(file, []byte("制度正文"), 0o644))
	doc, err := AddDocumentFromPath(source.ID, file)
	require.NoError(t, err)
	oldPath := doc.StoredPath

	require.NoError(t, MoveDocument(doc.ID, target.ID))

	moved, err := GetDocument(doc.ID)
	require.NoError(t, err)
	require.Equal(t, target.ID, moved.CollectionID)
	require.Equal(t, StatusPending, moved.Status)
	require.NotEqual(t, oldPath, moved.StoredPath)
	_, err = os.Stat(moved.StoredPath)
	require.NoError(t, err)
	_, err = os.Stat(oldPath)
	require.True(t, os.IsNotExist(err))

	chunks, err := DocumentChunks(doc.ID, 10)
	require.NoError(t, err)
	require.Empty(t, chunks)

	// The same content cannot land twice in the target.
	again, err := AddDocumentFromPath(source.ID, file)
	require.NoError(t, err)
	err = MoveDocument(again.ID, target.ID)
	require.ErrorIs(t, err, ErrDuplicateDocument)

	// Moving to the same collection is a no-op.
	require.NoError(t, MoveDocument(again.ID, source.ID))
}

func TestDocumentTags(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "手册", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "a.md", Ext: ".md", ContentHash: "h", Status: StatusPending,
	})
	require.NoError(t, err)

	require.NoError(t, st.SetDocumentTags(docID, "制度, 报销"))
	got, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, "制度, 报销", got.Tags)

	docs, err := st.ListDocuments(colID)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, "制度, 报销", docs[0].Tags)

	require.NoError(t, st.SetDocumentTags(docID, ""))
	got, err = st.GetDocument(docID)
	require.NoError(t, err)
	require.Empty(t, got.Tags)
}

func TestDocumentUsage(t *testing.T) {
	col := newCollectionForImport(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.md")
	require.NoError(t, os.WriteFile(file, []byte("制度"), 0o644))
	doc, err := AddDocumentFromPath(col.ID, file)
	require.NoError(t, err)

	// One row per turn, deduped within the turn.
	RecordDocumentUsage([]Ref{{DocumentID: doc.ID}, {DocumentID: doc.ID}}, "s1")
	RecordDocumentUsage([]Ref{{DocumentID: doc.ID}}, "s2")
	// No session id → ignored.
	RecordDocumentUsage([]Ref{{DocumentID: doc.ID}}, "")

	usage, err := ListDocumentUsage(col.ID)
	require.NoError(t, err)
	require.Len(t, usage, 1)
	require.Equal(t, doc.ID, usage[0].DocumentID)
	require.Equal(t, 2, usage[0].Count)
	require.False(t, usage[0].LastUsed.IsZero())
}
