package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidLiveNoteRecordingID(t *testing.T) {
	for _, id := range []string{"rec-1", "abc_DEF-123", "0123456789"} {
		assert.True(t, ValidLiveNoteRecordingID(id), id)
	}
	for _, id := range []string{"", "../x", "a/b", "a b", "rec.webm", "x/"} {
		assert.False(t, ValidLiveNoteRecordingID(id), id)
	}
	// 64 is the limit, 65 is one too many.
	assert.True(t, ValidLiveNoteRecordingID(strings.Repeat("a", 64)))
	assert.False(t, ValidLiveNoteRecordingID(strings.Repeat("a", 65)))
}

func TestEnsureLiveNoteRecordingDir_CreatesDirAndGitignore(t *testing.T) {
	dir := t.TempDir()
	paths := LiveNotesPathsIn(dir)

	require.NoError(t, EnsureLiveNoteRecordingDir(paths))
	recDir := filepath.Join(dir, LiveNoteRecordingDirName)
	info, err := os.Stat(recDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	ignore, err := os.ReadFile(filepath.Join(recDir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "*\n", string(ignore))

	// Idempotent, and an existing .gitignore is left alone.
	require.NoError(t, os.WriteFile(filepath.Join(recDir, ".gitignore"), []byte("# mine\n"), 0o644))
	require.NoError(t, EnsureLiveNoteRecordingDir(paths))
	ignore, err = os.ReadFile(filepath.Join(recDir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "# mine\n", string(ignore))
}

func TestLiveNoteRecordingFileName_SortsByStart(t *testing.T) {
	first := LiveNoteRecordingFileName(1_700_000_000_000, "a")
	second := LiveNoteRecordingFileName(1_700_000_060_000, "b")
	assert.Equal(t, "recording/1700000000000-a.webm", first)
	assert.Less(t, first, second)
	// A missing timestamp still produces a usable name.
	assert.Contains(t, LiveNoteRecordingFileName(0, "c"), "recording/")
}
