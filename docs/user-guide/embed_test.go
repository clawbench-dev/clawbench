package userguide

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtract_WritesFilesUnderVersion asserts the manual lands at
// <dataDir>/user-guide/<version>/ with the Markdown files intact. The version
// component is the whole point: a new build must not overwrite the directory a
// concurrently running older build points at.
func TestExtract_WritesFilesUnderVersion(t *testing.T) {
	dir := t.TempDir()
	got, err := Extract(dir, "v1.2.3")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "user-guide", "v1.2.3"), got)

	// At least the index and a couple of chapters must be present, and the
	// content must match what is embedded.
	entries, readErr := os.ReadDir(got)
	require.NoError(t, readErr)
	require.NotEmpty(t, entries)

	embeddedReadme, err := embedded.ReadFile("README.md")
	require.NoError(t, err)
	onDisk, err := os.ReadFile(filepath.Join(got, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, embeddedReadme, onDisk)

	// Dir() reflects the extraction so the command can find it.
	assert.Equal(t, got, Dir())
	t.Cleanup(func() { SetDir("") })
}

// TestExtract_EmptyDataDir guards the caller contract: an empty data dir must
// be rejected rather than joined into a relative path.
func TestExtract_EmptyDataDir(t *testing.T) {
	_, err := Extract("", "v1")
	require.Error(t, err)
}

// TestExtract_DataDirIsFile covers the MkdirAll failure branch: the data dir
// path exists but is a regular file, so the versioned parent cannot be created.
func TestExtract_DataDirIsFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	_, err := Extract(file, "v1")
	require.Error(t, err)
}

// errorFS is an fs.FS whose reads always fail, used to reach writeFiles' error
// branches without a broken disk.
type errorFS struct{ readDirErr, readFileErr bool }

func (e errorFS) Open(string) (fs.File, error) { return nil, os.ErrNotExist }

func (e errorFS) ReadDir(string) ([]fs.DirEntry, error) {
	if e.readDirErr {
		return nil, errors.New("readdir failed")
	}
	return []fs.DirEntry{fakeEntry{}}, nil
}

func (e errorFS) ReadFile(string) ([]byte, error) {
	if e.readFileErr {
		return nil, errors.New("readfile failed")
	}
	return []byte("ok"), nil
}

type fakeEntry struct{}

func (fakeEntry) Name() string               { return "a.md" }
func (fakeEntry) IsDir() bool                { return false }
func (fakeEntry) Type() fs.FileMode          { return 0 }
func (fakeEntry) Info() (fs.FileInfo, error) { return nil, errors.New("no info") }

// TestWriteFiles_ErrorBranches covers the read failures that would otherwise
// only surface as an opaque startup warning.
func TestWriteFiles_ErrorBranches(t *testing.T) {
	dst := t.TempDir()

	err := writeFiles(errorFS{readDirErr: true}, dst)
	require.Error(t, err)

	err = writeFiles(errorFS{readFileErr: true}, dst)
	require.Error(t, err)

	// The happy path still writes the entry through.
	require.NoError(t, writeFiles(errorFS{}, dst))
	_, statErr := os.Stat(filepath.Join(dst, "a.md"))
	assert.NoError(t, statErr)
}

// TestPruneOldVersions_ListError covers the branch where the parent cannot be
// listed at all; it must log and return rather than panic.
func TestPruneOldVersions_ListError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a-file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	// A file path makes os.ReadDir fail.
	assert.NotPanics(t, func() { pruneOldVersions(file, "v1") })
}

// TestPruneOldVersions_RemoveError covers the branch where an old version
// directory cannot be removed. Running as root bypasses the permission check,
// so the case is skipped there.
func TestPruneOldVersions_RemoveError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	parent := t.TempDir()
	old := filepath.Join(parent, "old")
	require.NoError(t, os.Mkdir(old, 0o755))
	// Make the directory non-removable: a non-empty dir whose parent is read-only.
	require.NoError(t, os.WriteFile(filepath.Join(old, "f"), []byte("x"), 0o644))
	require.NoError(t, os.Chmod(parent, 0o555))
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	// Must not panic; the failure is logged.
	assert.NotPanics(t, func() { pruneOldVersions(parent, "keep") })
}

// TestExtract_PrunesOldVersions asserts the previous version directory is
// removed. Leaving them behind would accumulate one full manual per upgrade.
func TestExtract_PrunesOldVersions(t *testing.T) {
	dir := t.TempDir()
	_, err := Extract(dir, "v1")
	require.NoError(t, err)
	oldDir := filepath.Join(dir, "user-guide", "v1")
	require.DirExists(t, oldDir)

	newDir, err := Extract(dir, "v2")
	require.NoError(t, err)

	assert.NoDirExists(t, oldDir, "the previous version must be pruned")
	assert.DirExists(t, newDir)

	// Only the new version remains.
	entries, err := os.ReadDir(filepath.Join(dir, "user-guide"))
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Equal(t, []string{"v2"}, names)
	t.Cleanup(func() { SetDir("") })
}

// TestExtract_ReplacesSameVersion covers a rebuilt dev binary whose version
// string is unchanged: the second extraction must replace the directory, not
// fail on a non-empty destination.
func TestExtract_ReplacesSameVersion(t *testing.T) {
	dir := t.TempDir()
	_, err := Extract(dir, "dev-1")
	require.NoError(t, err)

	// Plant a file that is not part of the embedded set; it must not survive.
	stale := filepath.Join(dir, "user-guide", "dev-1", "stale.md")
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o644))

	got, err := Extract(dir, "dev-1")
	require.NoError(t, err)
	assert.NoFileExists(t, stale, "re-extraction must replace the directory wholesale")
	assert.DirExists(t, got)
	t.Cleanup(func() { SetDir("") })
}

// TestSafeVersion_NoPathEscape guards the version component against a value
// that would climb out of the data directory. `git describe` on a branch name
// can legitimately contain "/", and a crafted version could contain "..".
func TestSafeVersion_NoPathEscape(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"v1.2.3", "v1.2.3"},
		{"v1.2.3-07291030", "v1.2.3-07291030"},
		{"feature/foo", "feature_foo"},
		{"../../etc", "_.._etc"},
		{"..", "unknown"},
		{"", "unknown"},
		{"a b:c", "a_b_c"},
	}
	for _, tc := range tests {
		got := safeVersion(tc.in)
		assert.Equalf(t, tc.want, got, "safeVersion(%q)", tc.in)
		// Whatever the input, the result must be a single path element.
		assert.NotContains(t, got, string(filepath.Separator))
		assert.NotEqual(t, ".", got)
		assert.NotEqual(t, "..", got)
	}
}

// TestFirstHeading pins the table-of-contents extraction: the first level-1
// heading wins, and a document with none falls back to its file name.
func TestFirstHeading(t *testing.T) {
	assert.Equal(t, "Hello", firstHeading("# Hello\n\ntext\n# Second", "fallback.md"))
	// A level-2 heading is not a chapter title.
	assert.Equal(t, "fallback.md", firstHeading("## Not a title\n", "fallback.md"))
	// Empty level-1 heading is skipped, in favor of a real one.
	assert.Equal(t, "Real", firstHeading("# \n# Real\n", "fallback.md"))
	assert.Equal(t, "fallback.md", firstHeading("no heading", "fallback.md"))
}

// TestChapters_ListsEmbeddedDocs asserts the table of contents is non-empty,
// sorted, and carries a title for every document.
func TestChapters_ListsEmbeddedDocs(t *testing.T) {
	chapters := Chapters()
	require.NotEmpty(t, chapters)

	var prev string
	for _, c := range chapters {
		assert.True(t, strings.HasSuffix(c.File, ".md"), "only Markdown is embedded: %s", c.File)
		assert.NotEmpty(t, c.Title, "%s must resolve a title", c.File)
		assert.LessOrEqual(t, prev, c.File, "chapters must be sorted by file name")
		prev = c.File
	}
}
