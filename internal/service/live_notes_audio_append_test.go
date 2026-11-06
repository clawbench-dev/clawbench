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

func TestAppendLiveNoteAudio_RebuildsWhenTheSidecarIsUnreadable(t *testing.T) {
	// A corrupt sidecar must not wedge the channel: the next segment rebuilds
	// the file instead of every upload failing forever.
	paths := audioPaths(t)
	seg := writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	_, err := AppendLiveNoteAudio(paths, "mic", 1_700_000_000_000, seg)
	require.NoError(t, err)

	sidecar := filepath.Join(paths.Audio, "mic.append.json")
	require.NoError(t, os.WriteFile(sidecar, []byte("not json"), 0o644))

	next := writeSegment(t, paths, 1_700_000_009_000, "mic", testSegment(t, testCluster{timecode: 0}))
	consumed, err := AppendLiveNoteAudio(paths, "mic", 1_700_000_009_000, next)
	require.NoError(t, err)
	assert.True(t, consumed)

	// Rebuilt from the new segment: one cluster, no half-written leftovers.
	assert.Equal(t, []uint64{0}, clusterTimecodes(t, filepath.Join(paths.Audio, "mic.webm")))
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
