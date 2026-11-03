package service

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func shotFixture(at int64) LiveNoteShot {
	return LiveNoteShot{At: at, Reason: LiveNoteShotReasonFile, Ref: "docs/a.md", FileName: "shots/a.webp", Width: 1280, Height: 720, Bytes: 42}
}

// A retry after a flaky response must not create a second row: the client
// generates the id, so the server can recognize the same frame.
func TestAppendLiveNoteShot_RetryIsIdempotent(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()
	base := int64(1_700_000_000_000)

	first, err := AppendLiveNoteShot(ctx, "tok", 7, shotFixture(base), "client-1", "deadbeef")
	require.NoError(t, err)
	second, err := AppendLiveNoteShot(ctx, "tok", 7, shotFixture(base), "client-1", "deadbeef")
	require.NoError(t, err)
	assert.Equal(t, first, second, "same client id returns the existing row")

	shots, err := ListLiveNoteShots("tok")
	require.NoError(t, err)
	require.Len(t, shots, 1)
	assert.Equal(t, base, shots[0].At)
	assert.Equal(t, LiveNoteShotReasonFile, shots[0].Reason)
}

// Frames are captured from several sources and uploaded in whatever order the
// queue drains; the timeline has to be in capture order.
func TestListLiveNoteShots_OrdersByTimeThenID(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()
	base := int64(1_700_000_000_000)

	for _, at := range []int64{base + 2000, base, base + 1000} {
		_, err := AppendLiveNoteShot(ctx, "tok", 7, shotFixture(at), "c-"+strconv.FormatInt(at, 10), "h")
		require.NoError(t, err)
	}
	shots, err := ListLiveNoteShots("tok")
	require.NoError(t, err)
	require.Len(t, shots, 3)
	assert.Equal(t, []int64{base, base + 1000, base + 2000}, []int64{shots[0].At, shots[1].At, shots[2].At})
}

// The name is generated server-side: it sorts in capture order, carries the
// reason, and never reuses a name within the same millisecond.
func TestSaveLiveNoteShot_NamesAndWritesUnderShots(t *testing.T) {
	dir := t.TempDir()
	paths := LiveNotesPathsIn(dir)

	name, err := SaveLiveNoteShot(paths, 1_700_000_000_000, LiveNoteShotReasonManual, "webp", []byte("frame-one"))
	require.NoError(t, err)
	assert.Equal(t, "shots/1700000000000-manual-", name[:len("shots/1700000000000-manual-")])
	assert.True(t, filepath.Ext(name) == ".webp")

	body, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	assert.Equal(t, "frame-one", string(body))

	// Same millisecond, different bytes: a second file, not an overwrite.
	other, err := SaveLiveNoteShot(paths, 1_700_000_000_000, LiveNoteShotReasonManual, "webp", []byte("frame-two"))
	require.NoError(t, err)
	assert.NotEqual(t, name, other)

	ignore := filepath.Join(dir, LiveNoteShotDirName, ".gitignore")
	content, err := os.ReadFile(ignore)
	require.NoError(t, err)
	assert.Equal(t, "*\n", string(content), "binary frames stay out of git")
}

// Deleting a frame removes the row and its file; an unknown id is the same
// "not found" the record endpoints use.
func TestDeleteLiveNoteShot_RemovesRowAndFile(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ctx := context.Background()
	dir := t.TempDir()
	paths := LiveNotesPathsIn(dir)
	name, err := SaveLiveNoteShot(paths, 1_700_000_000_000, LiveNoteShotReasonManual, "webp", []byte("frame"))
	require.NoError(t, err)

	shot := shotFixture(1_700_000_000_000)
	shot.FileName = name
	id, err := AppendLiveNoteShot(ctx, "tok", 7, shot, "c1", "h")
	require.NoError(t, err)

	gotName, err := DeleteLiveNoteShot("tok", 7, id)
	require.NoError(t, err)
	assert.Equal(t, name, gotName)
	require.NoError(t, DeleteLiveNoteShotFile(paths, gotName))
	assert.NoFileExists(t, filepath.Join(dir, name))

	shots, err := ListLiveNoteShots("tok")
	require.NoError(t, err)
	assert.Empty(t, shots)

	_, err = DeleteLiveNoteShot("tok", 7, id)
	assert.ErrorIs(t, err, ErrMeetingNotFound)
}

// A corrupted row must not be able to point outside the shots folder.
func TestDeleteLiveNoteShotFile_ClampsToShotsFolder(t *testing.T) {
	dir := t.TempDir()
	paths := LiveNotesPathsIn(dir)
	require.NoError(t, os.MkdirAll(paths.Shots, 0o755))
	outside := filepath.Join(dir, "outside.webp")
	require.NoError(t, os.WriteFile(outside, []byte("keep"), 0o644))

	// filepath.Base turns this into "outside.webp" inside shots/, which does not
	// exist — the real file one level up is untouched.
	require.NoError(t, DeleteLiveNoteShotFile(paths, "../../outside.webp"))
	content, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, "keep", string(content))
}

// The record's shot count is derived from the folder, like the other artifact
// flags: a frame deleted from disk stops being advertised.
func TestLiveNoteShotCountIn_CountsOnlyFrames(t *testing.T) {
	dir := t.TempDir()
	paths := LiveNotesPathsIn(dir)
	require.NoError(t, os.MkdirAll(paths.Shots, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(paths.Shots, "a.webp"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(paths.Shots, "b.png"), []byte("b"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(paths.Shots, ".gitignore"), []byte("*\n"), 0o644))

	assert.Equal(t, 2, liveNoteShotCountIn(paths.Shots))
	assert.Equal(t, 0, liveNoteShotCountIn(filepath.Join(dir, "missing")))
}
