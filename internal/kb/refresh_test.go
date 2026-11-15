package kb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRefreshDocumentFromSource(t *testing.T) {
	col := newCollectionForImport(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "handbook.md")
	require.NoError(t, os.WriteFile(src, []byte("第一版：报销流程先提交申请。"), 0o644))

	doc, err := AddDocumentFromPath(col.ID, src)
	require.NoError(t, err)
	require.Equal(t, StatusPending, doc.Status)

	// Unchanged bytes: no reindex, no copy churn.
	changed, err := RefreshDocumentFromSource(doc.ID)
	require.NoError(t, err)
	require.False(t, changed)
	same, err := GetDocument(doc.ID)
	require.NoError(t, err)
	require.Equal(t, doc.ContentHash, same.ContentHash)

	// Changed source: the stored copy is replaced and the document requeued.
	require.NoError(t, os.WriteFile(src, []byte("第二版：报销流程已更新。"), 0o644))
	changed, err = RefreshDocumentFromSource(doc.ID)
	require.NoError(t, err)
	require.True(t, changed)

	updated, err := GetDocument(doc.ID)
	require.NoError(t, err)
	require.Equal(t, StatusPending, updated.Status)
	require.NotEqual(t, doc.ContentHash, updated.ContentHash)
	require.Zero(t, updated.ChunkCount)
	stored, err := os.ReadFile(updated.StoredPath)
	require.NoError(t, err)
	require.Contains(t, string(stored), "第二版")
	// The temporary sibling is gone.
	_, statErr := os.Stat(updated.StoredPath + ".refresh")
	require.True(t, os.IsNotExist(statErr))

	// A missing original is reported as such; the stored copy stays usable.
	require.NoError(t, os.Remove(src))
	_, err = RefreshDocumentFromSource(doc.ID)
	require.ErrorIs(t, err, ErrSourceMissing)
	still, err := os.Stat(updated.StoredPath)
	require.NoError(t, err)
	require.Positive(t, still.Size())
}

func TestRefreshDocumentRejectsDuplicateContent(t *testing.T) {
	col := newCollectionForImport(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "a.md")
	second := filepath.Join(dir, "b.md")
	require.NoError(t, os.WriteFile(first, []byte("甲内容"), 0o644))
	require.NoError(t, os.WriteFile(second, []byte("乙内容"), 0o644))

	a, err := AddDocumentFromPath(col.ID, first)
	require.NoError(t, err)
	b, err := AddDocumentFromPath(col.ID, second)
	require.NoError(t, err)

	// Making b identical to a would collide with the unique content index.
	require.NoError(t, os.WriteFile(second, []byte("甲内容"), 0o644))
	_, err = RefreshDocumentFromSource(b.ID)
	require.ErrorIs(t, err, ErrDuplicateDocument)

	unchanged, err := GetDocument(b.ID)
	require.NoError(t, err)
	require.Equal(t, "乙内容", readStored(t, unchanged))
	_ = a
}

func readStored(t *testing.T, doc *Document) string {
	t.Helper()
	b, err := os.ReadFile(doc.StoredPath)
	require.NoError(t, err)
	return string(b)
}
