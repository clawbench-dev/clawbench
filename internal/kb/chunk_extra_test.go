package kb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBuildChunks_DefaultsAndEmptyBlocks pins the two normalisations callers
// rely on: a non-positive chunk size falls back to the default (a zero from an
// unset config would produce one chunk per rune), a negative overlap becomes
// zero, and a whitespace-only block produces no chunk at all (an empty chunk
// would be embedded and then match every query with a zero vector).
func TestBuildChunks_DefaultsAndEmptyBlocks(t *testing.T) {
	blocks := []Block{
		{Text: "   ", Location: Location{Kind: "page", Start: 1}},
		{Text: "  报销流程：先提交申请。  ", Location: Location{Kind: "page", Start: 2}},
	}
	chunks := buildChunks(blocks, 0, -5)
	require.Len(t, chunks, 1, "the blank block must contribute nothing")
	require.Equal(t, "报销流程：先提交申请。", chunks[0].Text)
	require.Equal(t, "p.2", chunks[0].Location)
	require.Equal(t, 0, chunks[0].Index, "indices are dense: they are the position in the chunk list")
	require.NotEmpty(t, chunks[0].Segmented, "the FTS column must be filled, or keyword search never matches")
}

// TestCollectDirFiles_StopsExactlyAtTheCap: hitting the cap with more files
// behind it reports truncation, but a folder with exactly the cap reports none —
// otherwise the panel would warn about files the user never had.
func TestCollectDirFiles_StopsExactlyAtTheCap(t *testing.T) {
	root := t.TempDir()
	for i := range maxDirImportFiles {
		name := filepath.Join(root, fmt.Sprintf("f%04d.md", i))
		require.NoError(t, os.WriteFile(name, []byte("x"), 0o644))
	}
	files, truncated, err := collectDirFiles(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, files, maxDirImportFiles)
	require.False(t, truncated, "a full cap alone does not mean something was cut off")

	// One more file: now the cap really dropped something.
	extra := filepath.Join(root, "zzz-last.md")
	require.NoError(t, os.WriteFile(extra, []byte("x"), 0o644))
	_, truncated, err = collectDirFiles(context.Background(), root)
	require.NoError(t, err)
	require.True(t, truncated)
}
