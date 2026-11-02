package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCluster is one cluster of a synthetic segment file.
type testCluster struct {
	timecode uint64
	blocks   [][]byte
}

// testSegment builds a complete WebM file the way MediaRecorder writes one:
// an EBML header, then a Segment holding Info, Tracks and the clusters.
func testSegment(t *testing.T, clusters ...testCluster) []byte {
	t.Helper()
	segment := make([]byte, 0, 256)
	info := append(elem(t, ebmlIDTimecodeScale, []byte{0x0F, 0x42, 0x40}), elem(t, ebmlIDDuration, []byte{0x40, 0x59, 0, 0, 0, 0, 0, 0})...)
	info = append(info, elem(t, ebmlIDMuxingApp, []byte("Chrome"))...)
	segment = append(segment, elem(t, ebmlIDInfo, info)...)
	segment = append(segment, elem(t, ebmlIDTracks, []byte{0xAE, 0x80})...)
	for _, c := range clusters {
		var body []byte
		body = append(body, elem(t, ebmlIDTimecode, encodeLiveNoteUint(c.timecode))...)
		for _, b := range c.blocks {
			body = append(body, elem(t, ebmlIDSimpleBlock, b)...)
		}
		segment = append(segment, elem(t, ebmlIDCluster, body)...)
	}
	out := elem(t, ebmlIDHeader, []byte{0x42, 0x86, 0x81, 0x01})
	return append(out, elem(t, ebmlIDSegment, segment)...)
}

// elem encodes one element with its minimal size field.
func elem(t *testing.T, id uint32, payload []byte) []byte {
	t.Helper()
	out := append([]byte{}, encodeEBMLID(id)...)
	out = append(out, encodeEBMLSize(len(payload))...)
	return append(out, payload...)
}

// clusterTimecodes returns the timecode of every cluster in a WebM file.
func clusterTimecodes(t *testing.T, path string) []uint64 {
	t.Helper()
	file, err := readLiveNoteWebM(path)
	require.NoError(t, err)
	out := make([]uint64, 0, len(file.clusters))
	for _, c := range file.clusters {
		cluster, err := decodeLiveNoteElement(c)
		require.NoError(t, err)
		payload := c[cluster.header : cluster.header+cluster.size]
		timecode, err := decodeLiveNoteElement(payload)
		require.NoError(t, err)
		out = append(out, decodeLiveNoteUint(payload[timecode.header:timecode.header+timecode.size]))
	}
	return out
}

// writeSegment drops one uploaded segment into a meeting folder.
func writeSegment(t *testing.T, paths LiveNotesPaths, startedAt int64, channel string, data []byte) string {
	t.Helper()
	path, err := SaveLiveNoteAudio(paths, channel, startedAt, data)
	require.NoError(t, err)
	return path
}

func audioPaths(t *testing.T) LiveNotesPaths {
	t.Helper()
	paths, err := ensureLiveNotesDirs(t.TempDir())
	require.NoError(t, err)
	return paths
}

func TestMergeLiveNoteAudio_ShiftsClustersOntoOneTimeline(t *testing.T) {
	paths := audioPaths(t)
	writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0, blocks: [][]byte{{0x01}}}, testCluster{timecode: 5000}))
	writeSegment(t, paths, 1_700_000_008_000, "mic", testSegment(t, testCluster{timecode: 0, blocks: [][]byte{{0x02}}}))

	merged, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	require.Len(t, merged.Channels, 1)
	assert.Equal(t, filepath.Join(paths.Audio, "mic.webm"), merged.Channels[0])
	assert.Equal(t, 2, merged.Segments)
	assert.Empty(t, merged.Meeting)

	// 0, 5000 from the first segment; the second restarts at 0 and must land on
	// 8000 — the silence between the two is the gap, not padded audio.
	assert.Equal(t, []uint64{0, 5000, 8000}, clusterTimecodes(t, merged.Channels[0]))
}

// One participant means the mix and the mic are the same audio: keeping both
// wrote an identical file twice and doubled the merge work.
func TestMergeLiveNoteAudio_SoloMeetingKeepsOnlyTheMix(t *testing.T) {
	paths := audioPaths(t)
	writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	writeSegment(t, paths, 1_700_000_000_000, LiveNoteMixChannel, testSegment(t, testCluster{timecode: 0}))
	writeSegment(t, paths, 1_700_000_002_000, LiveNoteMixChannel, testSegment(t, testCluster{timecode: 0}))

	merged, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(paths.Audio, LiveNoteMeetingAudioFile), merged.Meeting)
	assert.Empty(t, merged.Channels, "the mix already is that participant's audio")
	assert.Equal(t, 3, merged.Segments)

	// The mix is remuxed even though one of its segments could have been
	// renamed: two segments mean one file.
	assert.Equal(t, []uint64{0, 2000}, clusterTimecodes(t, merged.Meeting))

	_, err = os.Stat(filepath.Join(paths.Audio, "mic.webm"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	entries, err := os.ReadDir(paths.Audio)
	require.NoError(t, err)
	webm := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".webm") {
			webm++
		}
	}
	require.Equal(t, 1, webm, "only the meeting file is left behind")
}

func TestMergeLiveNoteAudio_TwoParticipantsKeepTheirChannelFiles(t *testing.T) {
	paths := audioPaths(t)
	writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	writeSegment(t, paths, 1_700_000_000_000, "viewer:2", testSegment(t, testCluster{timecode: 0}))
	writeSegment(t, paths, 1_700_000_000_000, LiveNoteMixChannel, testSegment(t, testCluster{timecode: 0}))

	merged, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(paths.Audio, LiveNoteMeetingAudioFile), merged.Meeting)
	require.Len(t, merged.Channels, 2)
	assert.Equal(t, filepath.Join(paths.Audio, "mic.webm"), merged.Channels[0])
	assert.Equal(t, filepath.Join(paths.Audio, "viewer-2.webm"), merged.Channels[1])
}

// A retry must never delete a channel file an earlier pass finished: the mix is
// there, but so is the work.
func TestMergeLiveNoteAudio_SoloRetryKeepsTheExistingChannelFile(t *testing.T) {
	paths := audioPaths(t)
	writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	writeSegment(t, paths, 1_700_000_000_000, LiveNoteMixChannel, testSegment(t, testCluster{timecode: 0}))

	first, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	require.Empty(t, first.Channels)

	// Simulate the old layout (both files present) and re-run the merge.
	require.NoError(t, os.WriteFile(filepath.Join(paths.Audio, "mic.webm"),
		testSegment(t, testCluster{timecode: 0}), 0o644))

	second, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(paths.Audio, "mic.webm"), second.Channels[0])
	_, statErr := os.Stat(filepath.Join(paths.Audio, "mic.webm"))
	assert.NoError(t, statErr)
}

func TestMergeLiveNoteAudio_SingleSegmentIsRenamedNotRewritten(t *testing.T) {
	paths := audioPaths(t)
	raw := testSegment(t, testCluster{timecode: 0})
	writeSegment(t, paths, 1_700_000_000_000, "viewer:2", raw)

	merged, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	require.Len(t, merged.Channels, 1)
	assert.Equal(t, 0, merged.Segments, "a renamed file was not folded in")
	assert.Equal(t, filepath.Join(paths.Audio, "viewer-2.webm"), merged.Channels[0])

	got, err := os.ReadFile(merged.Channels[0])
	require.NoError(t, err)
	assert.Equal(t, raw, got, "the single segment is already a complete WebM")
}

// A single segment is renamed, not remuxed, so nothing else in that path ever
// looks at its contents. MediaRecorder silently falls back to audio/mp4 on
// Safari, and publishing that as `mic.webm` would only fail later, when somebody
// plays the recording.
func TestMergeLiveNoteAudio_RejectsASingleSegmentThatIsNotWebM(t *testing.T) {
	paths := audioPaths(t)
	// ISO-BMFF (MP4) header: a box size, then `ftyp`.
	mp4 := append([]byte{0, 0, 0, 0x20}, []byte("ftypisom")...)
	writeSegment(t, paths, 1_700_000_000_000, "mic", mp4)

	_, err := MergeLiveNoteAudio(paths)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a WebM recording")

	// Nothing may be published under a name that claims to be a WebM recording,
	// and the uploaded segment must still be there (the merge failed, it did not
	// consume the file).
	_, statErr := os.Stat(filepath.Join(paths.Audio, "mic.webm"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
	entries, err := os.ReadDir(paths.Audio)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "the .gitignore and the rejected segment: %v", entries)
}

func TestMergeLiveNoteAudio_SecondPassChangesNothing(t *testing.T) {
	paths := audioPaths(t)
	writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	writeSegment(t, paths, 1_700_000_005_000, "mic", testSegment(t, testCluster{timecode: 0}))

	first, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	before, err := os.ReadFile(first.Channels[0])
	require.NoError(t, err)

	second, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	assert.Equal(t, first.Channels, second.Channels)
	assert.Equal(t, 0, second.Segments)
	after, err := os.ReadFile(second.Channels[0])
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestMergeLiveNoteAudio_MissingAudioDirIsNotAnError(t *testing.T) {
	merged, err := MergeLiveNoteAudio(LiveNotesPathsIn(filepath.Join(t.TempDir(), "nope")))
	require.NoError(t, err)
	assert.Empty(t, merged.Channels)
	assert.Empty(t, merged.Meeting)
}

func TestMergeLiveNoteAudio_LeavesUnrelatedFilesAlone(t *testing.T) {
	paths := audioPaths(t)
	ignore := filepath.Join(paths.Audio, ".gitignore")
	require.NoError(t, os.WriteFile(ignore, []byte("*\n"), 0o644))
	writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	writeSegment(t, paths, 1_700_000_005_000, "mic", testSegment(t, testCluster{timecode: 0}))

	_, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	assert.FileExists(t, ignore)
}

func TestMergeLiveNoteAudio_RejectsNonWebMInput(t *testing.T) {
	paths := audioPaths(t)
	writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	writeSegment(t, paths, 1_700_000_005_000, "mic", []byte("not a webm"))

	_, err := MergeLiveNoteAudio(paths)
	require.Error(t, err)
	// The segments stay: a failed merge must not cost the recording.
	entries, readErr := os.ReadDir(paths.Audio)
	require.NoError(t, readErr)
	assert.Len(t, entries, 3, ".gitignore + the two segments")
}

func TestStripLiveNoteDuration_DropsOnlyTheDuration(t *testing.T) {
	info := testSegment(t, testCluster{timecode: 0})
	file, err := readLiveNoteWebM(writeTemp(t, info))
	require.NoError(t, err)
	require.NotEmpty(t, file.info)

	el, err := decodeLiveNoteElement(file.info)
	require.NoError(t, err)
	payload := file.info[el.header : el.header+el.size]
	assert.NotContains(t, string(payload), string(encodeEBMLID(ebmlIDDuration)),
		"a per-segment Duration would make the merged file report seconds")
	assert.Contains(t, string(payload), "Chrome")
}

func TestParseLiveNoteAudioSegmentName(t *testing.T) {
	seg, ok := parseLiveNoteAudioSegmentName("1700000000000-viewer-2.webm")
	require.True(t, ok)
	assert.Equal(t, "viewer-2", seg.channel)
	assert.Equal(t, int64(1700000000000), seg.startedAt)

	for _, name := range []string{"mic.webm", LiveNoteMeetingAudioFile, ".gitignore", "1700000000000-mic.tmp"} {
		_, ok := parseLiveNoteAudioSegmentName(name)
		assert.False(t, ok, name)
	}
}

func TestLiveNoteArtifactFlags_CountsChannelsNotTheMix(t *testing.T) {
	paths := audioPaths(t)
	require.NoError(t, os.WriteFile(filepath.Join(paths.Audio, "mic.webm"), []byte("m"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(paths.Audio, LiveNoteMeetingAudioFile), []byte("x"), 0o644))

	st := liveNoteArtifactFlags(paths.Dir)
	assert.True(t, st.hasMeetingAudio)
	// One channel: the merged file is not another recording, it is the same
	// audio mixed down.
	assert.Equal(t, 1, st.audioFiles)
}

func TestLiveNoteAudioFileName(t *testing.T) {
	assert.Equal(t, LiveNoteMeetingAudioFile, liveNoteAudioFileName(LiveNoteMixChannel))
	assert.Equal(t, "mic.webm", liveNoteAudioFileName("mic"))
	assert.Equal(t, "viewer-2.webm", liveNoteAudioFileName("viewer-2"))
}

// writeTemp writes data to a file in a fresh temp dir and returns its path.
func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "segment.webm")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}
