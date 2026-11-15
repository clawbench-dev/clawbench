package kb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/rag"

	"github.com/stretchr/testify/require"
)

// TestFacades exercises the package-level API the HTTP layer uses. The handler
// tests live in another package, so without this the facades would show 0%
// coverage even though they are on the production path.
func TestFacades(t *testing.T) {
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
	require.True(t, Available())
	require.False(t, VectorEnabled())
	require.Greater(t, MaxFileBytes(), int64(0))

	col, err := CreateCollection("制度", "说明", ScopeGlobal, 0)
	require.NoError(t, err)
	require.NotZero(t, col.ID)

	cols, err := ListCollections(0)
	require.NoError(t, err)
	require.Len(t, cols, 1)

	got, err := GetCollection(col.ID)
	require.NoError(t, err)
	require.Equal(t, "制度", got.Name)

	name := "新名字"
	require.NoError(t, UpdateCollection(col.ID, &name, nil, nil, nil))
	got, err = GetCollection(col.ID)
	require.NoError(t, err)
	require.Equal(t, "新名字", got.Name)

	ok, err := CollectionAccessible(col.ID, 0)
	require.NoError(t, err)
	require.True(t, ok)

	ids, err := ListCollectionIDs(0)
	require.NoError(t, err)
	require.Equal(t, []int64{col.ID}, ids)

	// Add through the upload path (reader).
	doc, err := AddDocumentFromReader(col.ID, "a.txt", strings.NewReader("报销流程：先提交申请。"))
	require.NoError(t, err)
	require.Equal(t, StatusPending, doc.Status)

	docs, err := ListDocuments(col.ID)
	require.NoError(t, err)
	require.Len(t, docs, 1)

	// Index it by hand so the chunk/detail facade has something to return.
	st := currentStore()
	require.NotNil(t, st)
	require.NoError(t, st.ReplaceChunks(doc.ID, []Chunk{{
		DocumentID:         doc.ID,
		CollectionID:       col.ID,
		ChunkText:          "报销流程：先提交申请。",
		ChunkTextSegmented: rag.SegmentText("报销流程：先提交申请。"),
		ChunkIndex:         0,
	}}, ""))

	chunks, err := DocumentChunks(doc.ID, 10)
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	require.NoError(t, ReindexDocument(doc.ID))
	require.NoError(t, ReindexCollections([]int64{col.ID}))
	require.NoError(t, ReindexCollections(nil))
	require.NoError(t, ResetVectorIndex())

	stats, err := Stats()
	require.NoError(t, err)
	require.Equal(t, 1, stats.Documents)

	// Unsupported uploads are rejected before anything is written, and carry the
	// typed reason so the HTTP layer can localize it (a plain wrapped error
	// rendered an empty toast).
	_, err = AddDocumentFromReader(col.ID, "old.doc", strings.NewReader("x"))
	require.ErrorIs(t, err, ErrUnsupportedFile)
	var fileErr *FileError
	require.ErrorAs(t, err, &fileErr)
	require.Equal(t, ReasonLegacyDoc, fileErr.Reason)
	require.Equal(t, ".doc", fileErr.Detail)

	// Directories are rejected too.
	_, err = AddDocumentFromPath(col.ID, t.TempDir())
	require.ErrorIs(t, err, ErrUnsupportedFile)

	require.NoError(t, DeleteDocument(doc.ID))
	require.NoError(t, DeleteCollection(col.ID))
	_, err = GetCollection(col.ID)
	require.ErrorIs(t, err, ErrCollectionNotFound)

	// Project cleanup through the facade.
	projectCol, err := CreateCollection("项目库", "", ScopeProject, 7)
	require.NoError(t, err)
	DeleteProjectData(7)
	_, err = GetCollection(projectCol.ID)
	require.ErrorIs(t, err, ErrCollectionNotFound)
}

func TestFacadesUnavailableWhenDisabled(t *testing.T) {
	oldDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() {
		Shutdown()
		model.DataDir = oldDir
	})

	require.NoError(t, Init(model.KBConfig{Enabled: false}, model.RAGConfig{}))
	require.False(t, Available())
	_, err := ListCollections(0)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = Stats()
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestExtractPlainTextUTF16(t *testing.T) {
	dir := t.TempDir()
	// UTF-16 LE with BOM: "知识库".
	buf := make([]byte, 0, 2+len([]rune("知识库"))*2)
	buf = append(buf, 0xFF, 0xFE)
	for _, r := range "知识库" {
		u := uint16(r)
		buf = append(buf, byte(u), byte(u>>8))
	}
	path := filepath.Join(dir, "u16.txt")
	require.NoError(t, os.WriteFile(path, buf, 0o644))

	blocks, _, err := ExtractFile(path, ".txt", 1<<20)
	require.NoError(t, err)
	require.NotEmpty(t, blocks)
	require.Contains(t, blocks[0].Text, "知识库")
}

func TestReconfigure(t *testing.T) {
	oldDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() {
		Shutdown()
		model.DataDir = oldDir
	})

	cfg := model.KBConfig{
		Enabled: true, PollInterval: "20ms", EmbedBatchSize: 4,
		MaxFileMB: 50, MaxTextMB: 20,
	}
	ragCfg := model.RAGConfig{VectorEnabled: false, ChunkSize: 512, ChunkOverlap: 64}
	require.NoError(t, Init(cfg, ragCfg))

	Reconfigure(cfg, ragCfg) // enabled, store already open: restart the worker
	require.True(t, Available())

	Reconfigure(model.KBConfig{Enabled: false}, ragCfg)
	require.False(t, Available())

	Reconfigure(cfg, ragCfg) // re-enable from a closed state
	require.True(t, Available())
}
