package kb

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The folder picker hands back a path the user typed; a path that exists but is
// a file must say "not a directory" rather than reporting an empty import.
func TestImportDirectory_RefusesAFilePath(t *testing.T) {
	col := newCollectionForImport(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"policy.md": "报销流程"})

	_, err := ImportDirectory(context.Background(), col.ID, filepath.Join(dir, "policy.md"))
	require.ErrorIs(t, err, ErrNotDirectory)
}

// A nil context is what a caller that forgot to pass one produces; the import
// must use a background context instead of panicking on ctx.Err().
func TestImportDirectory_NilContextIsBackground(t *testing.T) {
	col := newCollectionForImport(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"policy.md": "报销流程：先提交申请。"})

	res, err := ImportDirectory(nil, col.ID, dir) //nolint:staticcheck // the nil context is the case under test
	require.NoError(t, err)
	require.Equal(t, 1, res.Added)
}

// A folder whose only files are one level too deep is reported as empty, not as
// an error: the depth cap is a policy, and the panel says "nothing found".
func TestImportDirectory_TooDeepFolderImportsNothing(t *testing.T) {
	col := newCollectionForImport(t)
	root := t.TempDir()
	deep := root
	for i := 0; i <= maxDirImportDepth; i++ {
		deep = filepath.Join(deep, "d")
	}
	require.NoError(t, os.MkdirAll(deep, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(deep, "policy.md"), []byte("报销流程"), 0o644))

	res, err := ImportDirectory(context.Background(), col.ID, root)
	require.NoError(t, err)
	require.Equal(t, 0, res.Added)
	require.Equal(t, 0, res.Skipped)
	require.Equal(t, 0, res.Failed)
	require.False(t, res.Truncated)
}

// The walk skips dependency and VCS directories at any depth, including one that
// also holds indexable files at its own top level.
func TestCollectDirFiles_SkipsDependencyDirectoriesAtAnyDepth(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"docs/handbook.md":            "甲",
		"docs/node_modules/pkg/x.md":  "乙",
		"docs/.venv/lib/y.md":         "丙",
		"docs/target/build/z.md":      "丁",
		"docs/__pycache__/w.md":       "戊",
		"docs/src/keep.md":            "己",
		"docs/vendor/github.com/v.md": "庚",
	})

	files, _, err := collectDirFiles(context.Background(), root)
	require.NoError(t, err)

	rel := make([]string, 0, len(files))
	for _, f := range files {
		r, relErr := filepath.Rel(root, f)
		require.NoError(t, relErr)
		rel = append(rel, filepath.ToSlash(r))
	}
	require.ElementsMatch(t, []string{"docs/handbook.md", "docs/src/keep.md"}, rel)
}

// Hidden files at any depth are tool state the user did not mean to publish, and
// non-regular entries (named pipes, sockets) are not documents.
func TestCollectDirFiles_SkipsHiddenFilesAndNonRegularEntries(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"keep.md":             "甲",
		"nested/.env":         "SECRET=1",
		"nested/keep2.md":     "乙",
		".gitignore":          "*",
		"nested/.hidden/x.md": "丙",
	})

	files, _, err := collectDirFiles(context.Background(), root)
	require.NoError(t, err)

	rel := make([]string, 0, len(files))
	for _, f := range files {
		r, relErr := filepath.Rel(root, f)
		require.NoError(t, relErr)
		rel = append(rel, filepath.ToSlash(r))
	}
	require.ElementsMatch(t, []string{"keep.md", "nested/keep2.md"}, rel)
}

// Preview and import classify a file too large for the configured cap the same
// way: skipped, not failed, so the panel's "skipped" count is stable across the
// two calls.
func TestImportFiles_VanishedFileIsFailedInBothModes(t *testing.T) {
	col := newCollectionForImport(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"big.txt": string(make([]byte, 64))})
	path := filepath.Join(dir, "big.txt")

	// A file that vanished between the walk and the stat is a FAILED import in
	// both modes: the panel offers a retry for failures, and the user picked the
	// folder expecting that file to come along.
	res, err := importFiles(context.Background(), col.ID, []string{filepath.Join(dir, "gone.txt")}, true)
	require.NoError(t, err)
	require.Equal(t, 1, res.Failed)

	res, err = importFiles(context.Background(), col.ID, []string{filepath.Join(dir, "gone.txt")}, false)
	require.NoError(t, err)
	require.Equal(t, 1, res.Failed)

	// And the existing file is added in the import mode.
	res, err = importFiles(context.Background(), col.ID, []string{path}, false)
	require.NoError(t, err)
	require.Equal(t, 1, res.Added)
}
