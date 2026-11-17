package kb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// newCollectionForImport initializes the package against a temp data dir and
// returns an empty global collection.
func newCollectionForImport(t *testing.T) *Collection {
	t.Helper()
	oldDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() {
		Shutdown()
		model.DataDir = oldDir
	})
	cfg := model.KBConfig{
		Enabled: true, AutoInject: true, InjectTopN: 4, InjectMaxChars: 2000,
		MaxFileMB: 50, MaxTextMB: 20, PollInterval: "20ms", EmbedBatchSize: 4,
	}
	require.NoError(t, Init(cfg, model.RAGConfig{VectorEnabled: false, ChunkSize: 512, ChunkOverlap: 64}))
	col, err := CreateCollection("资料", "", ScopeGlobal, 0)
	require.NoError(t, err)
	return col
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
}

func TestImportDirectory_AddsSupportedFilesRecursively(t *testing.T) {
	col := newCollectionForImport(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"handbook.md":         "报销流程：先提交申请。",
		"sub/travel.txt":      "差旅标准：市内交通每日上限 100 元。",
		"sub/deep/prices.csv": "产品,价格\n基础版,199\n",
		// Skipped: unsupported type, hidden entry, dependency directory.
		"logo.png":          "\x89PNG\r\n",
		".secret.md":        "hidden",
		"node_modules/x.md": "dependency",
	})

	res, err := ImportDirectory(context.Background(), col.ID, root)
	require.NoError(t, err)
	require.Equal(t, 3, res.Added)
	require.Equal(t, 1, res.Skipped)
	require.Equal(t, 0, res.Duplicates)
	require.Equal(t, 0, res.Failed)
	require.False(t, res.Truncated)

	docs, err := ListDocuments(col.ID)
	require.NoError(t, err)
	require.Len(t, docs, 3)

	// A second run re-copies nothing: every file is already in the collection.
	res, err = ImportDirectory(context.Background(), col.ID, root)
	require.NoError(t, err)
	require.Equal(t, 0, res.Added)
	require.Equal(t, 3, res.Duplicates)
	require.Equal(t, 1, res.Skipped)
}

func TestImportDirectory_RejectsAFilePath(t *testing.T) {
	col := newCollectionForImport(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.md": "text"})

	_, err := ImportDirectory(context.Background(), col.ID, filepath.Join(root, "a.md"))
	require.ErrorIs(t, err, ErrNotDirectory)
}

func TestImportDirectory_ReportsTruncation(t *testing.T) {
	col := newCollectionForImport(t)
	root := t.TempDir()
	files := make(map[string]string, maxDirImportFiles+5)
	for i := range maxDirImportFiles + 5 {
		files[filepath.Join("many", "f"+strconv.Itoa(i)+".md")] = fmt.Sprintf("内容 %d", i)
	}
	writeTree(t, root, files)

	res, err := ImportDirectory(context.Background(), col.ID, root)
	require.NoError(t, err)
	require.Equal(t, maxDirImportFiles, res.Added)
	require.True(t, res.Truncated)
}
