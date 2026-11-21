package kb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// ── disabled-store behavior ──────────────────────────────────────────────
//
// The whole scenario is one test function on purpose. The package keeps a single
// global store (one knowledge base per server) and these assertions deliberately
// switch it OFF, so they must all run in one Init/Shutdown window: split across
// tests, an assertion would depend on which one ran first.
func TestKBFacade_DisabledStoreReportsUnavailable(t *testing.T) {
	// The knowledge base is switched off: every facade answers ErrUnavailable
	// instead of touching a store that was never opened.
	oldDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() {
		Shutdown()
		model.DataDir = oldDir
	})
	require.NoError(t, Init(model.KBConfig{Enabled: false}, model.RAGConfig{}))

	ctx := context.Background()
	_, err := Search(ctx, SearchParams{Query: "报销流程"})
	require.ErrorIs(t, err, ErrUnavailable, "search on a disabled knowledge base")

	_, err = ListCollections(0)
	require.ErrorIs(t, err, ErrUnavailable)

	_, err = ImportDirectory(ctx, 1, t.TempDir())
	require.ErrorIs(t, err, ErrUnavailable, "directory preview on a disabled knowledge base")

	_, err = PreviewDirectory(ctx, 1, t.TempDir())
	require.ErrorIs(t, err, ErrUnavailable)

	_, err = AddDocumentFromReader(1, "a.txt", strings.NewReader("报销流程"))
	require.ErrorIs(t, err, ErrUnavailable, "an upload needs a store even before anything is read")

	_, err = AddDocumentFromPath(1, filepath.Join(t.TempDir(), "a.txt"))
	require.ErrorIs(t, err, ErrUnavailable)

	require.ErrorIs(t, DeleteDocument(1), ErrUnavailable)
	require.ErrorIs(t, DeleteCollection(1), ErrUnavailable)
	require.ErrorIs(t, ReindexCollections([]int64{}), ErrUnavailable,
		"an empty list is still a call into a store that does not exist")
	require.ErrorIs(t, MoveDocument(1, 2), ErrUnavailable)
	require.ErrorIs(t, ReindexDocument(1), ErrUnavailable)
	require.ErrorIs(t, ReindexCollections(nil), ErrUnavailable)
	require.ErrorIs(t, ResetVectorIndex(), ErrUnavailable)

	_, err = Stats()
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = StatsForProject(7)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = ListDocumentProgress(1)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = ListDocuments(1)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = GetDocument(1)
	require.ErrorIs(t, err, ErrUnavailable)
	_, _, err = DocumentChunkWindow(1, 2, 0, 0)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = DocumentChunks(1, 5)
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, SetDocumentTags(1, "a"), ErrUnavailable)
	_, err = ListDocumentUsage(1)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = GetCollection(1)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = CreateCollection("c", "", ScopeGlobal, 0)
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, UpdateCollection(1, nil, nil, nil), ErrUnavailable)
	ok, err := CollectionAccessible(1, 0)
	require.ErrorIs(t, err, ErrUnavailable)
	require.False(t, ok)
	_, err = AddDocumentFromURL(ctx, 1, "https://example.com/a.html")
	require.ErrorIs(t, err, ErrUnavailable)

	DeleteProjectData(7) // best effort: must not panic and must do nothing
}

// ── stored-copy helpers ───────────────────────────────────────────────────

// moveStoredFile falls back to copy+delete when a rename cannot work (the copy is
// in another filesystem). A destination directory that does not exist is the one
// failure both paths share: the error is reported and nothing is left behind.
func TestMoveStoredFile_ReportsAnUnusableDestination(t *testing.T) {
	src := filepath.Join(t.TempDir(), "policy.txt")
	require.NoError(t, os.WriteFile(src, []byte("报销流程"), 0o644))

	dest := filepath.Join(t.TempDir(), "nope", "deep", "policy.txt")
	err := moveStoredFile(src, dest)
	require.ErrorContains(t, err, "copy stored file")
	require.FileExists(t, src, "a failed move must leave the original in place")
}

// Across the same filesystem the move is a rename: the source is gone and the
// destination holds the bytes.
func TestMoveStoredFile_RenamesWithinTheFilesystem(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "from.txt")
	dest := filepath.Join(dir, "to.txt")
	require.NoError(t, os.WriteFile(src, []byte("报销流程"), 0o644))

	require.NoError(t, moveStoredFile(src, dest))
	_, statErr := os.Stat(src)
	require.True(t, os.IsNotExist(statErr))
	body, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "报销流程", string(body))
}

// refreshableSource refuses an original whose extension the knowledge base no
// longer indexes: "update from original" must not import a file the add path
// would have rejected.
func TestRefreshableSource_ValidatesTheOriginal(t *testing.T) {
	dir := t.TempDir()
	image := filepath.Join(dir, "scan.png")
	require.NoError(t, os.WriteFile(image, []byte("\x89PNG"), 0o644))

	_, err := refreshableSource(&Document{SourcePath: image})
	var fe *FileError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonImage, fe.Reason)

	// An empty source path means the document has no original at all.
	_, err = refreshableSource(&Document{SourcePath: "  "})
	require.ErrorIs(t, err, ErrSourceMissing)

	// A directory is not a document.
	folder := filepath.Join(dir, "folder.txt")
	require.NoError(t, os.MkdirAll(folder, 0o755))
	_, err = refreshableSource(&Document{SourcePath: folder})
	require.ErrorAs(t, err, &fe)
	require.Equal(t, ReasonNotRegular, fe.Reason)

	// A readable text file passes and comes back as given.
	text := filepath.Join(dir, "policy.txt")
	require.NoError(t, os.WriteFile(text, []byte("报销流程"), 0o644))
	got, err := refreshableSource(&Document{SourcePath: text})
	require.NoError(t, err)
	require.Equal(t, text, got)
}

// replaceFile overwrites an existing destination, which on Windows a plain
// rename cannot do: the fallback removes it first.
func TestReplaceFile_OverwritesAnExistingTarget(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "stored.txt")
	tmp := filepath.Join(dir, "stored.txt.new")
	require.NoError(t, os.WriteFile(old, []byte("旧"), 0o644))
	require.NoError(t, os.WriteFile(tmp, []byte("新"), 0o644))

	require.NoError(t, replaceFile(tmp, old))
	body, err := os.ReadFile(old)
	require.NoError(t, err)
	require.Equal(t, "新", string(body))
	_, statErr := os.Stat(tmp)
	require.True(t, os.IsNotExist(statErr))
}

// hashFile is the duplicate check's only input: two identical files must hash the
// same, and a different one must differ (so a content change is detected).
func TestHashFile_IsContentOnly(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	require.NoError(t, os.WriteFile(a, []byte("报销流程"), 0o644))
	require.NoError(t, os.WriteFile(b, []byte("报销流程"), 0o600))
	require.NoError(t, os.WriteFile(c, []byte("报销流程 "), 0o644))

	ha, err := hashFile(a)
	require.NoError(t, err)
	hb, err := hashFile(b)
	require.NoError(t, err)
	hc, err := hashFile(c)
	require.NoError(t, err)
	require.Equal(t, ha, hb, "name and mode are not content")
	require.NotEqual(t, ha, hc)
	require.Len(t, ha, 64, "sha256 hex")
}
