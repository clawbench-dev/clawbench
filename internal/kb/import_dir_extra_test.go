package kb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

func TestImportDirectory_RejectsBadInput(t *testing.T) {
	col := newCollectionForImport(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.md": "内容"})

	// A cancelled request must not start copying: the user closed the dialog,
	// and the counts would describe work nobody is waiting for.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ImportDirectory(cancelled, col.ID, root)
	require.ErrorIs(t, err, context.Canceled)

	// An unknown library is refused before the tree is walked.
	_, err = ImportDirectory(context.Background(), 9999, root)
	require.ErrorIs(t, err, ErrCollectionNotFound)

	// A path that does not exist reports the folder, not a walk failure: that
	// is the message the panel shows when the folder was moved away.
	_, err = ImportDirectory(context.Background(), col.ID, filepath.Join(root, "missing"))
	require.ErrorContains(t, err, "read folder")

	// Preview has the same guards, decided in one place.
	_, err = PreviewDirectory(context.Background(), 9999, root)
	require.ErrorIs(t, err, ErrCollectionNotFound)
}

func TestPreviewDirectory_CountsWithoutCopying(t *testing.T) {
	col := newCollectionForImport(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"handbook.md":       "报销流程：先提交申请。",
		"logo.png":          "\x89PNG\r\n",
		"sub/big-drawing":   "\x00binary",
		"sub/deep/plan.csv": "产品,价格\n基础版,199\n",
	})

	res, err := PreviewDirectory(context.Background(), col.ID, root)
	require.NoError(t, err)
	// Two supported files; the image and the binary blob are skipped, and the
	// extension-less binary is judged by content (ValidateFile sniffs it).
	require.Equal(t, 2, res.Added)
	require.Equal(t, 2, res.Skipped)
	require.Equal(t, 0, res.Duplicates)
	require.False(t, res.Truncated)

	// Nothing was copied: preview exists so a large folder can be inspected
	// before it costs disk space.
	docs, err := ListDocuments(col.ID)
	require.NoError(t, err)
	require.Empty(t, docs)

	// After a real import the same folder previews as duplicates, which is how
	// the panel tells "already imported" from "new files".
	_, err = ImportDirectory(context.Background(), col.ID, root)
	require.NoError(t, err)
	res, err = PreviewDirectory(context.Background(), col.ID, root)
	require.NoError(t, err)
	require.Equal(t, 0, res.Added)
	require.Equal(t, 2, res.Duplicates)
}

func TestCollectDirFiles_SkipsHiddenDeepAndUnreadableEntries(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"keep.md":            "a",
		".hidden.md":         "b",
		".config/state.yaml": "c",
		"node_modules/x.md":  "d",
		"sub/keep2.md":       "e",
		"vendor/y.md":        "f",
	})
	// A directory deeper than maxDirImportDepth is pruned instead of walked:
	// a repository can be arbitrarily deep and its build output is never
	// knowledge the user meant to import.
	deep := root
	for i := 0; i <= maxDirImportDepth; i++ {
		deep = filepath.Join(deep, fmt.Sprintf("d%d", i))
	}
	require.NoError(t, os.MkdirAll(deep, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(deep, "too-deep.md"), []byte("g"), 0o644))

	files, truncated, err := collectDirFiles(context.Background(), root)
	require.NoError(t, err)
	require.False(t, truncated)
	rel := make([]string, 0, len(files))
	for _, f := range files {
		r, relErr := filepath.Rel(root, f)
		require.NoError(t, relErr)
		rel = append(rel, filepath.ToSlash(r))
	}
	require.ElementsMatch(t, []string{"keep.md", "sub/keep2.md"}, rel)

	// A missing root yields "no files" rather than a walk error: filepath.WalkDir
	// reports the failure of the root itself through the callback, and that
	// callback treats every unreadable entry as "skip". The outer Stat in
	// importDirectory is what refuses a missing folder (see
	// TestImportDirectory_RejectsBadInput), so this is the contract the caller
	// must not rely on this function for.
	files, _, err = collectDirFiles(context.Background(), filepath.Join(root, "gone"))
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestImportFiles_CountsSkippedFailedAndDuplicates(t *testing.T) {
	col := newCollectionForImport(t)
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.md")
	require.NoError(t, os.WriteFile(keep, []byte("制度"), 0o644))
	// A file that no longer exists is a FAILED import, not a skipped one: the
	// panel offers a retry for those.
	vanished := filepath.Join(dir, "vanished.md")

	res, err := importFiles(context.Background(), col.ID, []string{vanished, keep}, false)
	require.NoError(t, err)
	require.Equal(t, 1, res.Failed)
	require.Equal(t, 1, res.Added)

	// Cancellation between files returns the counts gathered so far, so a
	// half-done import is reported honestly instead of as a total failure.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err = importFiles(ctx, col.ID, []string{keep}, false)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, res.Added)
}

// TestImportFiles_RecordsSkipReasons: a skipped file's count alone does not
// tell the user why. The reason breakdown is what lets the panel say "legacy
// .doc — save it as .docx" instead of a bare "1 skipped".
func TestImportFiles_RecordsSkipReasons(t *testing.T) {
	col := newCollectionForImport(t)
	dir := t.TempDir()
	doc := filepath.Join(dir, "old.doc")
	require.NoError(t, os.WriteFile(doc, []byte{0xD0, 0xCF, 0x11, 0xE0}, 0o644))
	img := filepath.Join(dir, "photo.png")
	require.NoError(t, os.WriteFile(img, []byte("\x89PNG"), 0o644))

	// dryRun: ValidateFile still classifies both, nothing is copied.
	res, err := importFiles(context.Background(), col.ID, []string{doc, img}, true)
	require.NoError(t, err)
	require.Equal(t, 2, res.Skipped)
	require.Equal(t, 1, res.SkipReasons[ReasonLegacyDoc])
	require.Equal(t, 1, res.SkipReasons[ReasonImage])
}

func TestImportDirectory_RequiresEnabledStore(t *testing.T) {
	oldDir := model.DataDir
	t.Cleanup(func() {
		Shutdown()
		model.DataDir = oldDir
	})
	require.NoError(t, Init(model.KBConfig{Enabled: false}, model.RAGConfig{}))
	_, err := ImportDirectory(context.Background(), 1, t.TempDir())
	require.ErrorIs(t, err, ErrUnavailable)
}
