package kb

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// vecEntries builds one embedding for a fresh chunk of docID.
func vecEntry(t *testing.T, st *Store, docID int64, vector ...float64) ChunkEmbedding {
	t.Helper()
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	pending, err := st.PendingEmbeddingChunks(1)
	require.NoError(t, err)
	require.NotEmpty(t, pending, "precondition: a chunk must still need a vector")
	return ChunkEmbedding{
		ChunkID: pending[0].ID, DocumentID: docID,
		CollectionID: doc.CollectionID, Vector: vector,
	}
}

// TestHasEmbeddings_TableAloneDoesNotCount: the startup reconcile asks
// HasEmbeddings to decide whether a kb_vec table is expected. Once kb_vec
// exists the answer is trivially true; before that it must reflect the chunks,
// or a library whose vectors were cleared would skip the rebuild.
func TestHasEmbeddings_TableAloneDoesNotCount(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)

	require.False(t, st.HasEmbeddings(), "a fresh store embeds nothing")

	docID := seedDoc(t, st, colID, "a.txt", "报销流程", "")
	entry := vecEntry(t, st, docID, 1, 0, 0)
	require.False(t, st.HasEmbeddings(), "a chunk without a vector is not an embedding")
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{entry}))
	require.True(t, st.HasEmbeddings(), "kb_vec now exists, so the reconcile must compare widths")

	// After a model switch the table is dropped; the answer must fall back to
	// the chunk flag (which ResetVector cleared) rather than staying true.
	require.NoError(t, st.ResetVector(0))
	require.False(t, st.HasEmbeddings(), "cleared vectors must make the reconcile a no-op again")
}

// TestSaveEmbeddings_RejectsInvalidVectors pins the input validation: an empty
// vector, a NaN and a batch whose members disagree about the width would all be
// written as garbage rows that only fail at query time.
func TestSaveEmbeddings_RejectsInvalidVectors(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "a.txt", "报销流程", "")

	require.NoError(t, st.SaveEmbeddings(nil), "nothing to write is not an error")

	entry := vecEntry(t, st, docID, 1, 0, 0)

	empty := entry
	empty.Vector = nil
	require.ErrorContains(t, st.SaveEmbeddings([]ChunkEmbedding{empty}), "empty embedding")

	nan := entry
	nan.Vector = []float64{1, 0, 0}
	short := entry
	short.Vector = []float64{1, 0}
	require.ErrorContains(t, st.SaveEmbeddings([]ChunkEmbedding{nan, short}), "mismatch")
	require.False(t, st.HasVecData())
}

// TestStore_VectorSchemaEdges: the width parser reads its number out of the DDL
// of the virtual table. A table whose declaration cannot be understood must
// read as "no table" (so it is recreated) rather than as width 0 (which would
// reject every batch with a mismatch).
func TestStore_VectorSchemaEdges(t *testing.T) {
	st := newTestStore(t)
	// No table yet.
	_, ok := st.vecDimFromSchema()
	require.False(t, ok)
	require.False(t, st.vecTableExists())

	// An unparsable declaration is treated as absent.
	_, err := st.db.Exec(`CREATE TABLE kb_vec (not_a_vector TEXT)`)
	require.NoError(t, err)
	_, ok = st.vecDimFromSchema()
	require.False(t, ok)
	require.False(t, st.vecTableExists())
	_, err = st.db.Exec(`DROP TABLE kb_vec`)
	require.NoError(t, err)

	require.Error(t, st.ensureVecTable(0), "a width is required to create the table")
	require.NoError(t, st.EnsureDimension(2))
	dim, ok := st.vecDimFromSchema()
	require.True(t, ok)
	require.Equal(t, 2, dim)
	// Re-asserting the same width is a no-op that keeps the table.
	require.NoError(t, st.EnsureDimension(2))
	dim, _ = st.vecDimFromSchema()
	require.Equal(t, 2, dim)
}

// TestStore_ResetVectorRecreatesImmediately: ResetVector(dim) is used by
// EnsureDimension when the model changes; the caller expects the new table to
// be there before the next batch arrives, otherwise the first save would have
// to rebuild again.
func TestStore_ResetVectorRecreatesImmediately(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "a.txt", "报销流程", "")
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{vecEntry(t, st, docID, 1, 0, 0)}))

	require.NoError(t, st.ResetVector(5))
	dim, ok := st.vecDimFromSchema()
	require.True(t, ok)
	require.Equal(t, 5, dim)

	// Without a width the table is dropped and the next batch creates it.
	require.NoError(t, st.ResetVector(0))
	_, ok = st.vecDimFromSchema()
	require.False(t, ok)
}

// TestStore_ColumnMigrationIsIdempotent: addColumnIfMissing runs on every start
// against databases created by older versions. Running it twice must not fail
// (SQLite rejects a duplicate column), and a missing table must be reported.
func TestStore_ColumnMigrationIsIdempotent(t *testing.T) {
	st := newTestStore(t)
	require.NoError(t, st.addColumnIfMissing("kb_documents", "tags", "TEXT NOT NULL DEFAULT ''"))
	require.NoError(t, st.addColumnIfMissing("kb_documents", "tags", "TEXT NOT NULL DEFAULT ''"))
	require.Error(t, st.addColumnIfMissing("no_such_table", "x", "INTEGER"))
}

// TestStore_CollectionGuards pins the write-side validation the HTTP layer
// relies on instead of duplicating it: an unknown scope is refused (the CHECK
// constraint would otherwise surface as a raw SQL error), and updates/deletes
// of a missing row report not-found rather than silently doing nothing.
func TestStore_CollectionGuards(t *testing.T) {
	st := newTestStore(t)
	_, err := st.CreateCollection(Collection{Name: "x", Scope: "invalid"})
	require.ErrorIs(t, err, ErrInvalidScope)

	require.ErrorIs(t, st.UpdateCollection(9999, nil, nil, nil), ErrCollectionNotFound)
	require.ErrorIs(t, st.DeleteCollection(9999), ErrCollectionNotFound)

	id, err := st.CreateCollection(Collection{Name: "项目库", Scope: ScopeProject, ProjectID: 7, AutoInject: true})
	require.NoError(t, err)
	// A global collection is flattened to project 0 so it is visible everywhere.
	globalID, err := st.CreateCollection(Collection{Name: "全局", Scope: ScopeGlobal, ProjectID: 9})
	require.NoError(t, err)
	got, err := st.GetCollection(globalID)
	require.NoError(t, err)
	require.Zero(t, got.ProjectID)

	// CollectionAccessible answers false (not an error) for another project's
	// collection, and reports a missing one separately.
	ok, err := st.CollectionAccessible(id, 8)
	require.NoError(t, err)
	require.False(t, ok)
	_, err = st.CollectionAccessible(9999, 0)
	require.ErrorIs(t, err, ErrCollectionNotFound)
}

// TestFindDocumentByHash_EmptyAndMissing: the hash lookup backs both the
// duplicate check and the commit-time "is this content already here" guard, so
// a miss must be (nil, nil) and a hit must carry the full row.
func TestFindDocumentByHash_EmptyAndMissing(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "a.txt", "报销流程", "")

	found, err := st.FindDocumentByHash(colID, "a.txt")
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, docID, found.ID)

	missing, err := st.FindDocumentByHash(colID, "nope")
	require.NoError(t, err)
	require.Nil(t, missing)
}

// TestListChunksAndWindow_Defaults pins the limits callers get for free: a
// non-positive limit falls back instead of returning nothing, negative windows
// clamp to the target, and a chunk id that no longer exists reports -1 so the
// panel opens the document at the top.
func TestListChunksAndWindow_Defaults(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{
		CollectionID: colID, Title: "a.txt", ContentHash: "w", Status: StatusReady,
	})
	require.NoError(t, err)
	chunks := make([]Chunk, 0, 30)
	for i := range 30 {
		chunks = append(chunks, Chunk{
			DocumentID: docID, CollectionID: colID, ChunkIndex: i,
			ChunkText: "块" + string(rune('A'+i)), ChunkTextSegmented: "块",
		})
	}
	require.NoError(t, st.ReplaceChunks(docID, chunks, "truncated", colID))

	listed, err := st.ListChunks(docID, 0)
	require.NoError(t, err)
	require.Len(t, listed, 20, "the default page size")

	all, err := st.ListChunks(docID, 100)
	require.NoError(t, err)
	require.Len(t, all, 30)
	target := all[5]

	window, index, err := st.ChunkWindow(docID, target.ID, -3, -2)
	require.NoError(t, err)
	require.Equal(t, 5, index)
	require.Len(t, window, 1, "negative neighbors clamp to the target itself")

	window, index, err = st.ChunkWindow(docID, target.ID, 2, 1)
	require.NoError(t, err)
	require.Equal(t, 5, index)
	require.Len(t, window, 4)
	require.Equal(t, 3, window[0].ChunkIndex)

	// A chunk that was re-indexed away is not an error.
	none, index, err := st.ChunkWindow(docID, 999999, 1, 1)
	require.NoError(t, err)
	require.Nil(t, none)
	require.Equal(t, -1, index)

	// The document is searchable and the warning survived the write.
	doc, err := st.GetDocument(docID)
	require.NoError(t, err)
	require.Equal(t, WarningTruncated, doc.Warning)

	// Pending chunk batches default to 16.
	pending, err := st.PendingEmbeddingChunks(0)
	require.NoError(t, err)
	require.Len(t, pending, 16)
}

// TestDocumentProgressAndUsage covers the polling projection: the panel merges
// only the volatile fields, so a status change must be visible without reading
// the whole document, and usage counts are per collection.
func TestDocumentProgressAndUsage(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "a.txt", "报销流程", "")
	otherID := seedDoc(t, st, colID, "b.txt", "差旅标准", "")
	otherCol, err := st.CreateCollection(Collection{Name: "d", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	foreignID := seedDoc(t, st, otherCol, "c.txt", "无关", "")

	require.NoError(t, st.SetDocumentStatus(docID, StatusFailed, "binary", "truncated"))
	progress, err := st.DocumentProgress(colID)
	require.NoError(t, err)
	require.Len(t, progress, 2)
	var found bool
	for _, p := range progress {
		if p.ID == docID {
			found = true
			require.Equal(t, StatusFailed, p.Status)
			require.Equal(t, "binary", p.Error)
			require.Equal(t, WarningTruncated, p.Warning)
		}
	}
	require.True(t, found)

	// Usage is counted per collection and ignores blank session ids.
	require.NoError(t, st.RecordDocumentUsage([]Ref{{DocumentID: otherID}, {DocumentID: foreignID}}, "s1"))
	require.NoError(t, st.RecordDocumentUsage([]Ref{{DocumentID: otherID}}, "  "))
	usage, err := st.DocumentUsage(colID)
	require.NoError(t, err)
	require.Len(t, usage, 1)
	require.Equal(t, otherID, usage[0].DocumentID)
	require.Equal(t, 1, usage[0].Count)
}

// TestStore_ReopenKeepsEmbeddingWidth: the width is read back from the chunks
// on open, so a restart does not have to probe the endpoint before the first
// batch can be written.
func TestStore_ReopenKeepsEmbeddingWidth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kb.db")
	st, err := NewStoreForTest(path)
	require.NoError(t, err)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	docID := seedDoc(t, st, colID, "a.txt", "报销流程", "")
	require.NoError(t, st.SaveEmbeddings([]ChunkEmbedding{vecEntry(t, st, docID, 1, 0, 0, 0)}))
	require.NoError(t, st.Close())

	reopened, err := NewStoreForTest(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	reopened.mu.RLock()
	dim := reopened.embDim
	reopened.mu.RUnlock()
	require.Equal(t, 4, dim)
	require.True(t, reopened.HasEmbeddings())
}

// TestStore_SearchReportsBadEmbeddings: both search legs validate their input,
// so a NaN query vector is an error instead of a query that silently matches
// nothing.
func TestStore_SearchReportsBadEmbeddings(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)

	_, err = st.SearchVector(nil, 10, []int64{colID})
	require.ErrorContains(t, err, "query embedding")

	hits, err := st.SearchFTS("   ", 10, []int64{colID})
	require.NoError(t, err)
	require.Nil(t, hits)

	hits, err = st.SearchFTS("报销", 10, nil)
	require.NoError(t, err)
	require.Nil(t, hits, "no collections means no scope, not every collection")

	// Nothing was embedded, so the vector leg is empty rather than an error.
	hits, err = st.SearchVector([]float64{1, 0}, 10, []int64{colID})
	require.NoError(t, err)
	require.Nil(t, hits)
}

// TestStatsForProject_FallsBackToGlobal keeps the two stats entry points in
// agreement: projectID <= 0 is documented as the installation-wide count.
func TestStatsForProject_FallsBackToGlobal(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal, AutoInject: true})
	require.NoError(t, err)
	seedDoc(t, st, colID, "a.txt", "报销流程", "")

	global, err := st.Stats()
	require.NoError(t, err)
	for _, projectID := range []int64{0, -1} {
		scoped, err := st.StatsForProject(projectID)
		require.NoError(t, err)
		require.Equal(t, global, scoped)
	}
}

// TestStore_DropColumnIfExists: the pinned column is removed on every start.
// It must be a no-op on installs that never had it (fresh DBs), drop it when
// present, and report a missing table rather than pretending success.
func TestStore_DropColumnIfExists(t *testing.T) {
	st := newTestStore(t)

	// Fresh schema: the column is already gone, so this is a no-op.
	require.NoError(t, st.dropColumnIfExists("kb_collections", "pinned"))

	// Put it back, then confirm the migration removes it.
	_, err := st.db.Exec("ALTER TABLE kb_collections ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0")
	require.NoError(t, err)
	var before int
	require.NoError(t, st.db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('kb_collections') WHERE name = 'pinned'").Scan(&before))
	require.Equal(t, 1, before)

	require.NoError(t, st.dropColumnIfExists("kb_collections", "pinned"))
	var after int
	require.NoError(t, st.db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('kb_collections') WHERE name = 'pinned'").Scan(&after))
	require.Equal(t, 0, after, "the dead column must be dropped")

	// A table that does not exist reports "no such column" (zero rows), which is
	// the same state as a fresh install: nothing to drop.
	require.NoError(t, st.dropColumnIfExists("no_such_table", "x"))

	// SQLite refuses to drop a column an index depends on: the error must be
	// reported, not swallowed (the caller aborts startup on it).
	_, err = st.db.Exec("ALTER TABLE kb_collections ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0")
	require.NoError(t, err)
	_, err = st.db.Exec("CREATE INDEX idx_kb_collections_pinned ON kb_collections(pinned)")
	require.NoError(t, err)
	require.Error(t, st.dropColumnIfExists("kb_collections", "pinned"))

	// An unreadable handle must surface the inspection failure.
	closed, err := NewStoreForTest(filepath.Join(t.TempDir(), "closed.db"))
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	require.Error(t, closed.dropColumnIfExists("kb_collections", "pinned"))
}
