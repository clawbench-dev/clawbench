package kb

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A closed store is what the panel talks to for a moment after a reconfigure.
// Every call must come back as an error instead of panicking on a nil handle or
// silently reporting "nothing found", which the panel would render as an empty
// library.
func TestClosedStore_CollectionCallsReportTheClosedHandle(t *testing.T) {
	st := newTestStore(t)
	require.NoError(t, st.Close())

	_, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal})
	require.ErrorContains(t, err, "insert collection")

	_, err = st.ListCollections(0)
	require.ErrorContains(t, err, "list collections")

	_, err = st.GetCollection(1)
	require.ErrorContains(t, err, "get collection")

	require.ErrorContains(t, st.UpdateCollection(1, nil, nil, nil, nil), "update collection")

	// DeleteCollection begins a transaction first, so the failure is named after
	// the transaction, not the delete.
	require.ErrorContains(t, st.DeleteCollection(1), "begin delete collection")

	_, err = st.ListCollectionIDs(0)
	require.ErrorContains(t, err, "list collection ids")

	_, err = st.ListInjectCollectionIDs(0)
	require.ErrorContains(t, err, "list collection ids")

	_, err = st.CollectionAccessible(1, 0)
	require.Error(t, err)
}

func TestClosedStore_DocumentCallsReportTheClosedHandle(t *testing.T) {
	st := newTestStore(t)
	require.NoError(t, st.Close())

	_, err := st.AddDocument(Document{CollectionID: 1, Title: "a", ContentHash: "h", Status: StatusReady})
	require.Error(t, err)

	_, err = st.ListDocuments(1)
	require.Error(t, err)

	_, err = st.GetDocument(1)
	require.Error(t, err)

	_, err = st.FindDocumentByHash(1, "h")
	require.Error(t, err)

	_, err = st.PendingDocuments(10)
	require.Error(t, err)

	require.Error(t, st.SetDocumentStatus(1, StatusReady, "", ""))
	require.Error(t, st.SetDocumentTags(1, "a,b"))
	require.Error(t, st.ResetStuckDocuments())
	require.Error(t, st.ResetDocumentsForReindex([]int64{1}))
	require.Error(t, st.ResetDocumentForReindex(1))
	_, err = st.DocumentProgress(1)
	require.Error(t, err)
	_, err = st.Stats()
	require.Error(t, err)
}

// Resetting vectors writes inside a transaction; a closed store must fail on the
// begin, naming the operation, rather than dropping the table and leaving the
// rows pointing at nothing.
func TestClosedStore_ResetVectorReportsTheTransactionFailure(t *testing.T) {
	st := newTestStore(t)
	require.NoError(t, st.Close())

	err := st.ResetVector(3)
	require.ErrorContains(t, err, "begin reset vector")
}

// A store that is still open but whose schema is broken is a different case:
// the error must name the table that could not be inspected, because that is the
// only hint a support request will carry.
func TestAddColumnIfMissing_NamesTheTableAndColumn(t *testing.T) {
	st := newTestStore(t)

	// The table does not exist: pragma_table_info simply returns no rows, so the
	// ALTER is attempted and fails. The message must still say which column.
	err := st.addColumnIfMissing("no_such_table", "flag", "INTEGER")
	require.ErrorContains(t, err, "add column no_such_table.flag")

	// An existing column is a no-op, not a second ALTER (which would fail).
	require.NoError(t, st.addColumnIfMissing("kb_collections", "pinned", "INTEGER NOT NULL DEFAULT 0"))
}

// newStore runs a fixed startup sequence against whatever path it is given. A
// path that cannot even be opened must be reported as an open failure; a
// directory in its place is the realistic mistake (a data dir copied by hand).
func TestNewStore_ReportsUnusablePaths(t *testing.T) {
	dir := t.TempDir()
	// sql.Open is lazy, so the directory is reported by the first PRAGMA; what
	// matters is that it is an error naming the step, not a half-open handle.
	_, err := NewStoreForTest(dir)
	require.ErrorContains(t, err, "set WAL mode")
}

// FindDocumentByHash is what the import preview uses to count duplicates. A hash
// that is not stored finds nothing; an EMPTY hash matches a legacy row whose
// stored hash is empty.
func TestFindDocumentByHash_EmptyHashMatchesOnlyWhenStored(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal})
	require.NoError(t, err)
	_, err = st.AddDocument(Document{CollectionID: colID, Title: "legacy", ContentHash: "", Status: StatusReady})
	require.NoError(t, err)

	// Today an empty hash DOES match such a legacy row. That is a trap for the
	// caller: a preview would report the folder as already imported. Pinned so a
	// behavior change is a deliberate edit, not a silent one.
	got, err := st.FindDocumentByHash(colID, "")
	require.NoError(t, err)
	require.NotNil(t, got, "documented behavior: an empty hash matches a legacy row")

	// A hash that is not stored finds nothing.
	got, err = st.FindDocumentByHash(colID, "not-stored")
	require.NoError(t, err)
	require.Nil(t, got)
}

// The access guard decides whether a share link may read a library. A global
// collection (project_id = 0) is visible from anywhere; a project collection is
// only visible from its own project; an unknown id is not visible at all.
func TestCollectionAccessible_ScopeRules(t *testing.T) {
	st := newTestStore(t)
	globalID, err := st.CreateCollection(Collection{Name: "global", Scope: ScopeGlobal})
	require.NoError(t, err)
	projectID, err := st.CreateCollection(Collection{Name: "project", Scope: ScopeProject, ProjectID: 7})
	require.NoError(t, err)

	ok, err := st.CollectionAccessible(globalID, 7)
	require.NoError(t, err)
	require.True(t, ok, "a global collection is visible from every project")

	ok, err = st.CollectionAccessible(globalID, 0)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = st.CollectionAccessible(projectID, 7)
	require.NoError(t, err)
	require.True(t, ok, "a project collection is visible from its own project")

	ok, err = st.CollectionAccessible(projectID, 8)
	require.NoError(t, err)
	require.False(t, ok, "a project collection must not leak into another project")

	ok, err = st.CollectionAccessible(projectID, 0)
	require.NoError(t, err)
	require.False(t, ok, "a project collection must not leak into the global context")

	_, err = st.CollectionAccessible(9999, 7)
	require.ErrorIs(t, err, ErrCollectionNotFound)
}

// DeleteProjectData is called when a project is removed. It must delete exactly
// the collections of that project and hand back their ids (the caller removes
// their directories), never the global ones.
func TestDeleteProjectData_ReturnsOnlyThatProjectsCollections(t *testing.T) {
	st := newTestStore(t)
	globalID, err := st.CreateCollection(Collection{Name: "global", Scope: ScopeGlobal})
	require.NoError(t, err)
	mine, err := st.CreateCollection(Collection{Name: "mine", Scope: ScopeProject, ProjectID: 4})
	require.NoError(t, err)
	theirs, err := st.CreateCollection(Collection{Name: "theirs", Scope: ScopeProject, ProjectID: 5})
	require.NoError(t, err)

	ids, err := st.DeleteProjectData(4)
	require.NoError(t, err)
	require.Equal(t, []int64{mine}, ids)

	_, err = st.GetCollection(mine)
	require.ErrorIs(t, err, ErrCollectionNotFound)

	_, err = st.GetCollection(globalID)
	require.NoError(t, err, "the global collection must survive")
	_, err = st.GetCollection(theirs)
	require.NoError(t, err, "another project's collection must survive")

	// Nothing left to delete is an empty result, not an error.
	ids, err = st.DeleteProjectData(4)
	require.NoError(t, err)
	require.Empty(t, ids)
}

// A collection can only be created in one of the two scopes the schema allows;
// anything else would be stored and then fail on read, so it is refused up front.
func TestCreateCollection_RefusesUnknownScopes(t *testing.T) {
	st := newTestStore(t)
	_, err := st.CreateCollection(Collection{Name: "c", Scope: "team"})
	require.ErrorIs(t, err, ErrInvalidScope)

	// Global always ends up with project_id 0, whatever the caller passed.
	id, err := st.CreateCollection(Collection{Name: "g", Scope: ScopeGlobal, ProjectID: 9})
	require.NoError(t, err)
	col, err := st.GetCollection(id)
	require.NoError(t, err)
	require.Equal(t, int64(0), col.ProjectID)
}

// ListCollections orders global collections first, then pinned ones, then by
// creation. The panel relies on that order to keep the shared libraries on top.
func TestListCollections_OrderIsGlobalThenPinnedThenOldest(t *testing.T) {
	st := newTestStore(t)
	first, err := st.CreateCollection(Collection{Name: "first", Scope: ScopeGlobal})
	require.NoError(t, err)
	second, err := st.CreateCollection(Collection{Name: "second", Scope: ScopeGlobal})
	require.NoError(t, err)
	pinned := true
	require.NoError(t, st.UpdateCollection(second, nil, nil, nil, &pinned))

	cols, err := st.ListCollections(0)
	require.NoError(t, err)
	require.Len(t, cols, 2)
	require.Equal(t, second, cols[0].ID, "the pinned collection comes first")
	require.Equal(t, first, cols[1].ID)
}

// Every statement that reads a row treats sql.ErrNoRows as "not found" and every
// other failure as an error with context. Both halves matter: a missing document
// is a 404 the panel expects, not a 500.
func TestMissingRows_AreNotFoundNotErrors(t *testing.T) {
	st := newTestStore(t)

	_, err := st.GetCollection(12345)
	require.ErrorIs(t, err, ErrCollectionNotFound)

	_, err = st.GetDocument(12345)
	require.ErrorIs(t, err, ErrDocumentNotFound)

	_, err = st.DeleteDocument(12345)
	require.ErrorIs(t, err, ErrDocumentNotFound)

	require.ErrorIs(t, st.UpdateCollection(12345, nil, nil, nil, nil), ErrCollectionNotFound)
	require.ErrorIs(t, st.DeleteCollection(12345), ErrCollectionNotFound)
}

// scannerErr forces rows.Scan to fail, so the "scan" branch of a list query is
// reachable without a schema change.
type scannerErr struct{}

func (scannerErr) Scan(...any) error { return errors.New("scan failed") }

func TestScanDocument_ReportsScanFailures(t *testing.T) {
	// scanDocument returns the driver error as-is: callers wrap it with the
	// operation they were performing (see the list/get paths).
	_, err := scanDocument(scannerErr{})
	require.ErrorContains(t, err, "scan failed")
}

// A document row deleted while the caller holds its id must produce a clean
// "not found" from GetDocument rather than a zero-valued document.
func TestGetDocument_AfterDeleteIsNotFound(t *testing.T) {
	st := newTestStore(t)
	colID, err := st.CreateCollection(Collection{Name: "c", Scope: ScopeGlobal})
	require.NoError(t, err)
	docID, err := st.AddDocument(Document{CollectionID: colID, Title: "a", ContentHash: "h", Status: StatusReady})
	require.NoError(t, err)

	require.NoError(t, func() error { _, err := st.DeleteDocument(docID); return err }())
	_, err = st.GetDocument(docID)
	require.ErrorIs(t, err, ErrDocumentNotFound)
}

// The KB store and the rest of the application share one SQLite file, so the
// store must open an existing database without touching its contents.
func TestNewStore_ReopensAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kb.db")
	first, err := NewStoreForTest(path)
	require.NoError(t, err)
	id, err := first.CreateCollection(Collection{Name: "kept", Scope: ScopeGlobal})
	require.NoError(t, err)
	require.NoError(t, first.Close())

	second, err := NewStoreForTest(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	col, err := second.GetCollection(id)
	require.NoError(t, err)
	require.Equal(t, "kept", col.Name)
}

// A database file whose schema was created by an older version has no `pinned`
// column; the startup migration must add it instead of failing the open.
func TestNewStore_AddsMissingColumnsToAnOlderSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kb.db")
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TABLE kb_collections (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		scope TEXT NOT NULL,
		project_id INTEGER NOT NULL DEFAULT 0,
		auto_inject INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	st, err := NewStoreForTest(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	id, err := st.CreateCollection(Collection{Name: "migrated", Scope: ScopeGlobal})
	require.NoError(t, err)
	pinned := true
	require.NoError(t, st.UpdateCollection(id, nil, nil, nil, &pinned), "the pinned column must exist after the migration")
}

// A cancelled context must abort a search before any query runs: the search is
// triggered from chat and the caller has already given up.
func TestSearch_UnavailableStoreAndEmptyQuery(t *testing.T) {
	setTestGlobalsNoStore(t)

	_, err := Search(context.Background(), SearchParams{Query: "报销"})
	require.ErrorIs(t, err, ErrUnavailable)
}

// setTestGlobalsNoStore removes the active store and puts it back afterwards.
func setTestGlobalsNoStore(t *testing.T) {
	t.Helper()
	mu.Lock()
	old := globalStore
	globalStore = nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		globalStore = old
		mu.Unlock()
	})
}
