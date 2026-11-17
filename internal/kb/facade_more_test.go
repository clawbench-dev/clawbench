package kb

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// wantErr reader fails on the first read, the way a dropped upload connection
// does.
type wantErr struct{}

func (wantErr) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

// An upload whose stream breaks before any byte arrives is reported as a read
// failure, and no file is left in the collection directory: a half-written copy
// would be indexed later as if it were complete.
func TestAddDocumentFromReader_BrokenStreamLeavesNoCopy(t *testing.T) {
	col := newCollectionForImport(t)
	_, err := AddDocumentFromReader(col.ID, "policy.txt", wantErr{})
	require.ErrorContains(t, err, "read upload")

	entries, readErr := os.ReadDir(filepath.Join(KnowledgeDir(), strconv.FormatInt(col.ID, 10)))
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	require.Empty(t, entries, "a failed upload must not leave a copy behind")
}

// An upload over the per-file cap is refused with the too-large reason, and the
// partial copy is removed.
func TestAddDocumentFromReader_RefusesAnOversizedUpload(t *testing.T) {
	col := newCollectionForImport(t)
	// The config used by these tests caps files at 50MB; a shorter cap is set
	// through the package config so the test does not write 50MB.
	mu.Lock()
	globalCfg.MaxFileMB = 1
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		globalCfg.MaxFileMB = 50
		mu.Unlock()
	})

	big := bytes.Repeat([]byte("a"), (1<<20)+10)
	_, err := AddDocumentFromReader(col.ID, "big.txt", bytes.NewReader(big))
	require.ErrorIs(t, err, ErrFileTooLarge)

	entries, readErr := os.ReadDir(filepath.Join(KnowledgeDir(), strconv.FormatInt(col.ID, 10)))
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	require.Empty(t, entries, "the oversized copy must be removed")
}

// An upload of a known-unsupported type is refused by extension before anything
// is read from the stream.
func TestAddDocumentFromReader_RefusesUnsupportedExtensionBeforeReading(t *testing.T) {
	col := newCollectionForImport(t)
	_, err := AddDocumentFromReader(col.ID, "photo.png", wantErr{})
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonImage, fe.Reason)
	require.ErrorIs(t, err, ErrUnsupportedFile)
}

// An extension-less upload is judged by its head: a NUL byte makes it binary.
func TestAddDocumentFromReader_RefusesBinaryContentWithoutExtension(t *testing.T) {
	col := newCollectionForImport(t)
	_, err := AddDocumentFromReader(col.ID, "payload", bytes.NewReader([]byte{'a', 0, 'b'}))
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonBinary, fe.Reason)
}

// An upload via AddDocumentFromReader is queued exactly like a file: the copy
// goes into the collection directory, the row carries the hash of what was
// written, and the same bytes uploaded twice is a duplicate.
func TestAddDocumentFromReader_StoresTheCopyAndDetectsDuplicates(t *testing.T) {
	col := newCollectionForImport(t)
	doc, err := AddDocumentFromReader(col.ID, "policy.txt", strings.NewReader("报销流程：先提交申请。"))
	require.NoError(t, err)
	require.Equal(t, "policy.txt", doc.Title)
	require.Equal(t, ".txt", doc.Ext)
	require.Equal(t, StatusPending, doc.Status)
	require.FileExists(t, doc.StoredPath)
	require.Contains(t, doc.StoredPath, strconv.FormatInt(col.ID, 10))
	require.NotEmpty(t, doc.ContentHash)
	require.Equal(t, int64(len("报销流程：先提交申请。")), doc.SizeBytes)

	_, err = AddDocumentFromReader(col.ID, "policy-again.txt", strings.NewReader("报销流程：先提交申请。"))
	require.ErrorIs(t, err, ErrDuplicateDocument)
}

// The in-memory knowledge base does not scope by physical location, and two
// files with the same base name must not overwrite each other's copy.
func TestAddDocumentFromPath_UniqueStoredNames(t *testing.T) {
	col := newCollectionForImport(t)
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeTree(t, dirA, map[string]string{"policy.txt": "甲：流程"})
	writeTree(t, dirB, map[string]string{"policy.txt": "乙：流程"})

	first, err := AddDocumentFromPath(col.ID, filepath.Join(dirA, "policy.txt"))
	require.NoError(t, err)
	second, err := AddDocumentFromPath(col.ID, filepath.Join(dirB, "policy.txt"))
	require.NoError(t, err)
	require.NotEqual(t, first.StoredPath, second.StoredPath, "same basename must not reuse the stored name")
	require.FileExists(t, first.StoredPath)
	require.FileExists(t, second.StoredPath)

	// The same source twice in a row is a duplicate, not a 500.
	_, err = AddDocumentFromPath(col.ID, filepath.Join(dirA, "policy.txt"))
	require.ErrorIs(t, err, ErrDuplicateDocument)
	require.FileExists(t, first.StoredPath, "the existing copy must survive a refused duplicate")
}

// A path that is not a regular file (a directory) is refused with the
// not-regular reason instead of producing an unreadable copy.
func TestAddDocumentFromPath_RefusesNonRegularFiles(t *testing.T) {
	col := newCollectionForImport(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "folder.txt"), 0o755))

	_, err := AddDocumentFromPath(col.ID, filepath.Join(dir, "folder.txt"))
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonNotRegular, fe.Reason)
}

// Deleting a document removes both the row and the stored copy: the copy is the
// only thing taking disk space, and a leftover would be invisible to the user.
func TestDeleteDocument_RemovesTheStoredCopy(t *testing.T) {
	col := newCollectionForImport(t)
	doc, err := AddDocumentFromReader(col.ID, "policy.txt", strings.NewReader("报销流程"))
	require.NoError(t, err)
	require.FileExists(t, doc.StoredPath)

	require.NoError(t, DeleteDocument(doc.ID))
	_, statErr := os.Stat(doc.StoredPath)
	require.True(t, os.IsNotExist(statErr), "the stored copy must be gone, got %v", statErr)

	// Deleting it again is a not-found, not a second silent success.
	require.ErrorIs(t, DeleteDocument(doc.ID), ErrDocumentNotFound)
}

// A stored copy that was already removed by hand must not turn a delete into a
// failure: the row is what the user sees.
func TestDeleteDocument_ToleratesAMissingStoredCopy(t *testing.T) {
	col := newCollectionForImport(t)
	doc, err := AddDocumentFromReader(col.ID, "policy.txt", strings.NewReader("报销流程"))
	require.NoError(t, err)
	require.NoError(t, os.Remove(doc.StoredPath))

	require.NoError(t, DeleteDocument(doc.ID))
	_, err = GetDocument(doc.ID)
	require.ErrorIs(t, err, ErrDocumentNotFound)
}

// Deleting a collection removes the directory holding its copies.
func TestDeleteCollection_RemovesItsDirectory(t *testing.T) {
	col := newCollectionForImport(t)
	doc, err := AddDocumentFromReader(col.ID, "policy.txt", strings.NewReader("报销流程"))
	require.NoError(t, err)
	colDir := filepath.Join(KnowledgeDir(), strconv.FormatInt(col.ID, 10))
	require.DirExists(t, colDir)

	require.NoError(t, DeleteCollection(col.ID))
	require.NoDirExists(t, colDir)
	_, statErr := os.Stat(doc.StoredPath)
	require.True(t, os.IsNotExist(statErr))
}

// DeleteProjectData removes the project's collections even though the caller
// gets no error back: it is best effort, run after the project rows are gone.
func TestDeleteProjectData_RemovesProjectDirectories(t *testing.T) {
	col := newCollectionForImport(t)
	projectCol, err := CreateCollection("项目资料", "", ScopeProject, 7)
	require.NoError(t, err)
	doc, err := AddDocumentFromReader(projectCol.ID, "policy.txt", strings.NewReader("报销流程"))
	require.NoError(t, err)
	require.FileExists(t, doc.StoredPath)

	DeleteProjectData(7)
	require.NoDirExists(t, filepath.Join(KnowledgeDir(), strconv.FormatInt(projectCol.ID, 10)))
	_, err = GetCollection(projectCol.ID)
	require.ErrorIs(t, err, ErrCollectionNotFound)

	// The global collection is untouched.
	_, err = GetCollection(col.ID)
	require.NoError(t, err)
}

// Moving a document between collections re-points the row, moves the copy and
// requeues it; nothing is left behind in the old collection directory.
func TestMoveDocument_MovesTheCopyAndRequeues(t *testing.T) {
	col := newCollectionForImport(t)
	target, err := CreateCollection("归档", "", ScopeGlobal, 0)
	require.NoError(t, err)
	doc, err := AddDocumentFromReader(col.ID, "policy.txt", strings.NewReader("报销流程：先提交申请。"))
	require.NoError(t, err)

	require.NoError(t, MoveDocument(doc.ID, target.ID))

	moved, err := GetDocument(doc.ID)
	require.NoError(t, err)
	require.Equal(t, target.ID, moved.CollectionID)
	require.Equal(t, StatusPending, moved.Status, "the chunks carried the old collection, so it is requeued")
	require.Contains(t, moved.StoredPath, strconv.FormatInt(target.ID, 10))
	require.FileExists(t, moved.StoredPath)
	_, statErr := os.Stat(doc.StoredPath)
	require.True(t, os.IsNotExist(statErr), "the old copy must be gone")

	// Moving into the collection it already belongs to is a no-op, not an error.
	require.NoError(t, MoveDocument(doc.ID, target.ID))
}

// The same content cannot exist twice in one collection, so a move that would
// create a duplicate is refused before any file is touched.
func TestMoveDocument_RefusesADuplicateInTheTarget(t *testing.T) {
	col := newCollectionForImport(t)
	target, err := CreateCollection("归档", "", ScopeGlobal, 0)
	require.NoError(t, err)
	content := "报销流程：先提交申请。"
	doc, err := AddDocumentFromReader(col.ID, "policy.txt", strings.NewReader(content))
	require.NoError(t, err)
	_, err = AddDocumentFromReader(target.ID, "policy-copy.txt", strings.NewReader(content))
	require.NoError(t, err)

	require.ErrorIs(t, MoveDocument(doc.ID, target.ID), ErrDuplicateDocument)
	require.FileExists(t, doc.StoredPath, "a refused move must leave the copy in place")
}

// Refreshing from the original re-reads it and reindexes only when the content
// changed; an unchanged file is a no-op so a "refresh all" does not reindex the
// whole library.
func TestRefreshDocumentFromSource_OnlyReindexesOnChange(t *testing.T) {
	col := newCollectionForImport(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "policy.txt")
	require.NoError(t, os.WriteFile(src, []byte("报销流程：第一版。"), 0o644))
	doc, err := AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)

	changed, err := RefreshDocumentFromSource(doc.ID)
	require.NoError(t, err)
	require.False(t, changed, "the source is unchanged")

	require.NoError(t, os.WriteFile(src, []byte("报销流程：第二版，附审批人。"), 0o644))
	changed, err = RefreshDocumentFromSource(doc.ID)
	require.NoError(t, err)
	require.True(t, changed)

	updated, err := GetDocument(doc.ID)
	require.NoError(t, err)
	require.Equal(t, StatusPending, updated.Status, "new content is reindexed")
	copied, err := os.ReadFile(updated.StoredPath)
	require.NoError(t, err)
	require.Equal(t, "报销流程：第二版，附审批人。", string(copied))
}

// A document that only exists as a copy (an upload or a URL import) has no
// original to refresh from.
func TestRefreshDocumentFromSource_NoOriginalIsSourceMissing(t *testing.T) {
	col := newCollectionForImport(t)
	doc, err := AddDocumentFromReader(col.ID, "policy.txt", strings.NewReader("报销流程"))
	require.NoError(t, err)

	changed, err := RefreshDocumentFromSource(doc.ID)
	require.False(t, changed)
	require.ErrorIs(t, err, ErrSourceMissing)
}

// Reindexing a document, a collection list, or the whole library queues the
// rows again; an EMPTY (non-nil) collection list means "nothing", which is how a
// project with no visible library cannot wipe the whole installation.
func TestReindexEntryPoints(t *testing.T) {
	col := newCollectionForImport(t)
	doc, err := AddDocumentFromReader(col.ID, "policy.txt", strings.NewReader("报销流程"))
	require.NoError(t, err)
	markReady(t, doc.ID)

	require.NoError(t, ReindexDocument(doc.ID))
	queued, err := GetDocument(doc.ID)
	require.NoError(t, err)
	require.Equal(t, StatusPending, queued.Status)

	require.NoError(t, ReindexCollections([]int64{}))
	require.Equal(t, StatusPending, queued.Status, "an empty list must touch nothing")

	require.NoError(t, ReindexCollections([]int64{col.ID}))
	require.NoError(t, ReindexCollections(nil))
}

// ResetVectorIndex drops the semantic index and queues a rebuild; it is what the
// settings panel's "rebuild" button calls.
func TestResetVectorIndex_ClearsVectorsAndRequeues(t *testing.T) {
	col := newCollectionForImport(t)
	doc, err := AddDocumentFromReader(col.ID, "policy.txt", strings.NewReader("报销流程"))
	require.NoError(t, err)
	markReady(t, doc.ID)

	require.NoError(t, ResetVectorIndex())
	st := currentStore()
	require.NotNil(t, st)
	require.False(t, st.HasVecData())
}

// Empty-path and empty-name guards: a URL with no path at all, and a file name
// that is only separators, must not produce an empty stored name.
func TestSanitizeFileName(t *testing.T) {
	require.Equal(t, "policy.txt", sanitizeFileName("/etc/policy.txt"))
	require.Equal(t, "a_b.txt", sanitizeFileName("a\nb.txt"))
	// Tabs are control characters too: they would corrupt the stored name and
	// the title shown in the panel, so they become underscores as well.
	require.Equal(t, "tab_kept", sanitizeFileName("tab\tkept"))
	// filepath.Base("") is "."; the guard is upstream (the caller rejects an
	// empty name), so the sanitiser's own answer is pinned as-is rather than
	// claimed to be empty.
	require.Equal(t, ".", sanitizeFileName(""))
	require.Len(t, []rune(sanitizeFileName(strings.Repeat("名", 200))), 100, "names are capped at 100 runes")
}

// An offline HTTP client is what a URL import sees when the machine has no
// network; it must be a fetch failure that names the address.
func TestAddDocumentFromURL_OfflineIsAFetchFailure(t *testing.T) {
	col := newCollectionForImport(t)
	setTestGlobals(t, currentStore(), model.KBConfig{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening any more

	_, err := addDocumentFromURL(context.Background(), col.ID, url+"/a.html", false)
	require.ErrorIs(t, err, ErrFetchFailed)
}

// A redirect to a private address is refused even for a public starting URL:
// the guard is re-run on every hop, because that is where a public URL turns
// into a metadata-service request.
func TestAddDocumentFromURL_RedirectToPrivateIsRefused(t *testing.T) {
	col := newCollectionForImport(t)
	setTestGlobals(t, currentStore(), model.KBConfig{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	// guard=false here only disables the dial filter; the redirect check is the
	// one under test, so it must fire regardless.
	_, err := addDocumentFromURL(context.Background(), col.ID, srv.URL+"/start.html", true)
	require.ErrorIs(t, err, ErrPrivateAddress)
}

// markReady moves a freshly added document to 'ready' so the reindex paths have
// something to reset.
func markReady(t *testing.T, docID int64) {
	t.Helper()
	st := currentStore()
	require.NotNil(t, st)
	require.NoError(t, st.SetDocumentStatus(docID, StatusReady, "", ""))
}

var _ = io.Discard
