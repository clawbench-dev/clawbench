package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The recording used to be assembled once, at the end of the meeting. These
// tests pin the incremental version: every segment is folded into the channel
// file as it arrives, so the recording exists and is playable throughout, and a
// crash only costs the tail rather than everything.

func TestAppendLiveNoteAudio_BuildsTheChannelFileAsSegmentsArrive(t *testing.T) {
	paths := audioPaths(t)
	first := writeSegment(t, paths, 1_700_000_000_000, "mic",
		testSegment(t, testCluster{timecode: 0, blocks: [][]byte{{0x01}}}, testCluster{timecode: 5000}))
	second := writeSegment(t, paths, 1_700_000_008_000, "mic",
		testSegment(t, testCluster{timecode: 0, blocks: [][]byte{{0x02}}}))

	consumed, err := AppendLiveNoteAudio(paths, "mic", 1_700_000_000_000, first)
	require.NoError(t, err)
	assert.True(t, consumed)

	// The file is already usable before the meeting ends.
	channel := filepath.Join(paths.Audio, "mic.webm")
	require.FileExists(t, channel)
	assert.Equal(t, []uint64{0, 5000}, clusterTimecodes(t, channel))

	consumed, err = AppendLiveNoteAudio(paths, "mic", 1_700_000_008_000, second)
	require.NoError(t, err)
	assert.True(t, consumed)

	// The 8 s gap between the segments is preserved as a gap, exactly as the
	// end-of-meeting merge did: the second segment restarts at 0 and lands on
	// 8000.
	assert.Equal(t, []uint64{0, 5000, 8000}, clusterTimecodes(t, channel))

	// Folded-in segments are removed, so the end merge has nothing left to do.
	_, err = os.Stat(first)
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(second)
	assert.ErrorIs(t, err, os.ErrNotExist)
	require.FileExists(t, filepath.Join(paths.Audio, "mic.append.json"))
}

func TestAppendLiveNoteAudio_DropsADuplicateDelivery(t *testing.T) {
	paths := audioPaths(t)
	seg := writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	_, err := AppendLiveNoteAudio(paths, "mic", 1_700_000_000_000, seg)
	require.NoError(t, err)

	channel := filepath.Join(paths.Audio, "mic.webm")
	before, err := os.Stat(channel)
	require.NoError(t, err)

	// The same segment again (a retry that lost its response) must not append a
	// second copy: the timeline would gain a duplicate of the same audio.
	dup := writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	consumed, err := AppendLiveNoteAudio(paths, "mic", 1_700_000_000_000, dup)
	require.NoError(t, err)
	assert.True(t, consumed, "a duplicate is still consumed, not left behind")

	after, err := os.Stat(channel)
	require.NoError(t, err)
	assert.Equal(t, before.Size(), after.Size())
	assert.Equal(t, []uint64{0}, clusterTimecodes(t, channel))
}

func TestAppendLiveNoteAudio_NeverTruncatesAnExistingRecording(t *testing.T) {
	// A sidecar that did not survive a crash must not cost the recording. The
	// earlier version treated "no sidecar" as "start a new file" and truncated,
	// which silently threw away every minute recorded so far — and the segments
	// it came from were already deleted.
	paths := audioPaths(t)
	seg := writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	_, err := AppendLiveNoteAudio(paths, "mic", 1_700_000_000_000, seg)
	require.NoError(t, err)

	channel := filepath.Join(paths.Audio, "mic.webm")
	before, err := os.Stat(channel)
	require.NoError(t, err)

	// The bookkeeping is gone (crash between the header patch and the sidecar
	// write), and another segment arrives.
	require.NoError(t, os.Remove(filepath.Join(paths.Audio, "mic.append.json")))
	next := writeSegment(t, paths, 1_700_000_009_000, "mic", testSegment(t, testCluster{timecode: 0}))
	consumed, err := AppendLiveNoteAudio(paths, "mic", 1_700_000_009_000, next)
	require.NoError(t, err)

	// The segment is NOT consumed: it stays on disk for the end-of-meeting merge.
	assert.False(t, consumed)
	require.FileExists(t, next)

	after, err := os.Stat(channel)
	require.NoError(t, err)
	assert.Equal(t, before.Size(), after.Size(), "the existing recording must not be touched")
	assert.Equal(t, []uint64{0}, clusterTimecodes(t, channel))
}

// A member's channel is `viewer:<id>` on the wire but `<slug>.webm` on disk.
// Using the raw channel here produced `viewer:2.webm`, which the merge never
// looks for and which Windows cannot even create — so every segment of every
// member's microphone stayed on disk as its own fragment.
func TestAppendLiveNoteAudio_NamesAViewerChannelByItsSlug(t *testing.T) {
	paths := audioPaths(t)
	seg := writeSegment(t, paths, 1_700_000_000_000, "viewer:2",
		testSegment(t, testCluster{timecode: 0}))

	consumed, err := AppendLiveNoteAudio(paths, "viewer:2", 1_700_000_000_000, seg)
	require.NoError(t, err)
	assert.True(t, consumed)

	require.FileExists(t, filepath.Join(paths.Audio, "viewer-2.webm"))
	require.FileExists(t, filepath.Join(paths.Audio, "viewer-2.append.json"))
	_, err = os.Stat(filepath.Join(paths.Audio, "viewer:2.webm"))
	assert.ErrorIs(t, err, os.ErrNotExist, "a colon is not a legal file name on Windows")

	// The name the merge derives for that channel must be the one on disk, or a
	// second pass would report a channel it cannot find.
	assert.Equal(t, "viewer-2.webm", liveNoteAudioFileName("viewer-2"))
}

// A segment can be left next to a channel that is already assembled — the
// process died between the clusters and the bookkeeping, or the delete failed.
// The merge used to skip such a channel entirely, so the fragment lived on
// forever. It now belongs in that file.
func TestMergeLiveNoteAudio_FoldsLeftoversIntoAnAssembledChannel(t *testing.T) {
	paths := audioPaths(t)
	first := writeSegment(t, paths, 1_700_000_000_000, "mic",
		testSegment(t, testCluster{timecode: 0, blocks: [][]byte{{0x01}}}))
	_, err := AppendLiveNoteAudio(paths, "mic", 1_700_000_000_000, first)
	require.NoError(t, err)

	// A segment that never made it into the file (its append was lost).
	leftover := writeSegment(t, paths, 1_700_000_009_000, "mic",
		testSegment(t, testCluster{timecode: 0, blocks: [][]byte{{0x02}}}))

	merged, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	assert.Equal(t, 1, merged.Segments)
	assert.Equal(t, []uint64{0, 9000}, clusterTimecodes(t, filepath.Join(paths.Audio, "mic.webm")))
	_, err = os.Stat(leftover)
	assert.ErrorIs(t, err, os.ErrNotExist, "the fragment must not survive the merge")
}

// Firefox's MediaRecorder leaves the Segment and every Cluster unbounded. The
// parser used to reject the file outright, so every segment of such a meeting
// stayed on disk as its own fragment and no recording was ever assembled.
func TestAppendLiveNoteAudio_FoldsALiveSizedFirefoxSegment(t *testing.T) {
	paths := audioPaths(t)
	seg := writeSegment(t, paths, 1_700_000_000_000, "mic",
		testLiveSizedSegment(t,
			testCluster{timecode: 0, blocks: [][]byte{{0x01}}},
			testCluster{timecode: 1000, blocks: [][]byte{{0x02}}}))
	next := writeSegment(t, paths, 1_700_000_004_000, "mic",
		testLiveSizedSegment(t, testCluster{timecode: 0, blocks: [][]byte{{0x03}}}))

	consumed, err := AppendLiveNoteAudio(paths, "mic", 1_700_000_000_000, seg)
	require.NoError(t, err)
	assert.True(t, consumed, "an unbounded Segment is a normal MediaRecorder file, not a corrupt one")

	consumed, err = AppendLiveNoteAudio(paths, "mic", 1_700_000_004_000, next)
	require.NoError(t, err)
	assert.True(t, consumed)

	channel := filepath.Join(paths.Audio, "mic.webm")
	// Both clusters of the first segment survive (the second one must not be
	// swallowed into the first), and the next segment lands 4 s in.
	assert.Equal(t, []uint64{0, 1000, 4000}, clusterTimecodes(t, channel))
}

func TestMergeLiveNoteAudio_FoldsLiveSizedSegments(t *testing.T) {
	paths := audioPaths(t)
	for i := range 2 {
		at := int64(1_700_000_000_000) + int64(i)*3000
		writeSegment(t, paths, at, "mic",
			testLiveSizedSegment(t, testCluster{timecode: 0, blocks: [][]byte{{0x01}}}))
	}

	merged, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	require.Len(t, merged.Channels, 1)
	assert.Equal(t, 2, merged.Segments)
	assert.Equal(t, []uint64{0, 3000}, clusterTimecodes(t, merged.Channels[0]))
}

func TestMergeLiveNoteAudio_DropsTheDuplicateSoloFileAssembledLive(t *testing.T) {
	// One participant: the mix and their channel are the same audio. With the
	// recording assembled as the meeting went, the duplicate is a whole FILE —
	// there are no segments left for the old cleanup to delete.
	paths := audioPaths(t)
	for _, ch := range []string{"mic", LiveNoteMixChannel} {
		seg := writeSegment(t, paths, 1_700_000_000_000, ch, testSegment(t, testCluster{timecode: 0}))
		_, err := AppendLiveNoteAudio(paths, ch, 1_700_000_000_000, seg)
		require.NoError(t, err)
	}

	merged, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(paths.Audio, LiveNoteMeetingAudioFile), merged.Meeting)
	assert.Empty(t, merged.Channels, "the mix already is that participant's audio")

	_, err = os.Stat(filepath.Join(paths.Audio, "mic.webm"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	require.FileExists(t, filepath.Join(paths.Audio, LiveNoteMeetingAudioFile))
}

// An empty segment path is not folded: the append path is only for segments the
// splitter actually wrote to disk, and the caller passes "" when there was none.
func TestAppendLiveNoteAudio_EmptyPathIsNoOp(t *testing.T) {
	consumed, err := AppendLiveNoteAudio(audioPaths(t), "mic", 1_700_000_000_000, "")
	require.NoError(t, err)
	assert.False(t, consumed)
}

// A corrupt sidecar must not wedge the channel: it is treated as "start over"
// so the next segment still gets folded, rather than failing every upload for
// the rest of the meeting.
func TestAppendLiveNoteAudio_CorruptSidecarStartsOver(t *testing.T) {
	paths := audioPaths(t)
	seg := writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	sidecar := filepath.Join(paths.Audio, "mic.append.json")
	require.NoError(t, os.WriteFile(sidecar, []byte("{not json"), 0o644))

	consumed, err := AppendLiveNoteAudio(paths, "mic", 1_700_000_000_000, seg)
	require.NoError(t, err)
	assert.True(t, consumed, "a bad sidecar means start over, not fail the upload")
	require.FileExists(t, filepath.Join(paths.Audio, "mic.webm"))
}

// A recording left by an earlier run whose bookkeeping did not survive (crash
// between the header patch and the sidecar write) must not be truncated: the
// segments are already gone, so the file is the only copy of those minutes.
func TestAppendLiveNoteAudio_KeepsAnOrphanedRecording(t *testing.T) {
	paths := audioPaths(t)
	channel := filepath.Join(paths.Audio, "mic.webm")
	require.NoError(t, os.MkdirAll(paths.Audio, 0o755))
	require.NoError(t, os.WriteFile(channel, []byte("previous run"), 0o644))

	seg := writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	consumed, err := AppendLiveNoteAudio(paths, "mic", 1_700_000_000_000, seg)
	require.NoError(t, err)
	assert.False(t, consumed, "the segment is left for the end-of-meeting merge")

	data, err := os.ReadFile(channel)
	require.NoError(t, err)
	assert.Equal(t, "previous run", string(data), "the earlier recording must be untouched")
	require.FileExists(t, seg, "the segment must survive so the merge can still use it")
}
