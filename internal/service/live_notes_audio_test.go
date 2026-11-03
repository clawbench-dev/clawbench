package service

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"runtime"
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

// ── Container parsing: malformed input ──────────────────────────────────────

// Every one of these is a file this package did not write. Refusing it by name
// is what turns "the recording is silent" into "this segment is corrupt".
func TestDecodeLiveNoteElement_RejectsMalformedHeaders(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"empty", nil, "empty element"},
		{"id wider than four bytes", []byte{0x08, 0x00, 0x00, 0x00, 0x00, 0x00}, "bad element id"},
		{"truncated id", []byte{0x1A, 0x45}, "bad element id"},
		{"missing size", []byte{0xE7}, "missing element size"},
		{"size wider than eight bytes", []byte{0xE7, 0x01}, "bad element size"},
		{"unknown size", []byte{0xE7, 0xFF}, "unknown size"},
		{"size past the end", []byte{0xE7, 0x85, 0x01}, "claims 5 bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeLiveNoteElement(tc.in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestEncodeEBMLSize_UsesTheShortestField(t *testing.T) {
	assert.Len(t, encodeEBMLSize(0), 1)
	assert.Len(t, encodeEBMLSize(126), 1)
	assert.Len(t, encodeEBMLSize(300), 2)
	assert.Len(t, encodeEBMLSize(1<<20), 3)
}

func TestDecodeLiveNoteUint_RejectsMoreThanEightBytes(t *testing.T) {
	assert.Zero(t, decodeLiveNoteUint(make([]byte, 9)))
	assert.Equal(t, uint64(0x0102), decodeLiveNoteUint([]byte{0x01, 0x02}))
}

func TestReadLiveNoteWebM_RejectsUnusableFiles(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		_, err := readLiveNoteWebM(filepath.Join(t.TempDir(), "gone.webm"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stat segment")
	})

	t.Run("larger than one segment can be", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "huge.webm")
		require.NoError(t, os.WriteFile(path, testSegment(t, testCluster{timecode: 0}), 0o644))
		require.NoError(t, os.Truncate(path, liveNoteAudioSegmentMaxBytes+1))

		_, err := readLiveNoteWebM(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "larger than one segment can be")
	})

	t.Run("unreadable", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("permissions are not enforced for this user")
		}
		path := writeTemp(t, testSegment(t, testCluster{timecode: 0}))
		require.NoError(t, os.Chmod(path, 0o000))
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

		_, err := readLiveNoteWebM(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read segment")
	})

	t.Run("no segment element", func(t *testing.T) {
		_, err := readLiveNoteWebM(writeTemp(t, elem(t, ebmlIDHeader, []byte{0x42, 0x86, 0x81, 0x01})))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no Segment element")
	})

	t.Run("malformed info payload", func(t *testing.T) {
		// The Info element is well-formed but its children are not.
		body := elem(t, ebmlIDInfo, []byte{0xE7, 0x85, 0x01})
		body = append(body, elem(t, ebmlIDTracks, []byte{0xAE, 0x80})...)
		broken := append(elem(t, ebmlIDHeader, []byte{0x42, 0x86, 0x81, 0x01}),
			elem(t, ebmlIDSegment, body)...)

		_, err := readLiveNoteWebM(writeTemp(t, broken))
		require.Error(t, err)
	})

	t.Run("malformed child", func(t *testing.T) {
		// A Segment whose payload is a Cluster claiming more bytes than exist.
		broken := append(elem(t, ebmlIDHeader, []byte{0x42, 0x86, 0x81, 0x01}),
			elem(t, ebmlIDSegment, []byte{0x1F, 0x43, 0xB6, 0x75, 0x85, 0x01})...)

		_, err := readLiveNoteWebM(writeTemp(t, broken))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "claims 5 bytes")
	})
}

func TestValidateLiveNoteWebMContainer_RejectsUnreadableAndTruncated(t *testing.T) {
	err := validateLiveNoteWebMContainer(filepath.Join(t.TempDir(), "gone.webm"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open segment")

	short := filepath.Join(t.TempDir(), "short.webm")
	require.NoError(t, os.WriteFile(short, []byte{0x1A, 0x45}, 0o644))
	err = validateLiveNoteWebMContainer(short)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no EBML header")
}

func TestLiveNoteInfoDuration_ReadsBothFloatWidths(t *testing.T) {
	info := func(payload []byte) []byte { return elem(t, ebmlIDInfo, payload) }
	duration := func(payload []byte) []byte { return elem(t, ebmlIDDuration, payload) }
	var f32 [4]byte
	binary.BigEndian.PutUint32(f32[:], math.Float32bits(6000))
	var f64 [8]byte
	binary.BigEndian.PutUint64(f64[:], math.Float64bits(8000))

	assert.Equal(t, 6000.0, liveNoteInfoDuration(info(duration(f32[:]))), "MediaRecorder writes a float32")
	assert.Equal(t, 8000.0, liveNoteInfoDuration(info(duration(f64[:]))))
	assert.Zero(t, liveNoteInfoDuration(info(duration([]byte{0x01, 0x02}))), "a 2-byte Duration is not a float")
	assert.Zero(t, liveNoteInfoDuration(info(elem(t, ebmlIDMuxingApp, []byte("Chrome")))), "no Duration at all")
	assert.Zero(t, liveNoteInfoDuration([]byte{0x15, 0x49, 0xA9, 0x66, 0x85, 0x01}), "malformed Info")
	assert.Zero(t, liveNoteInfoDuration(info([]byte{0xE7, 0x85, 0x01})), "malformed child")
}

func TestStripLiveNoteDuration_RejectsMalformedInfo(t *testing.T) {
	_, err := stripLiveNoteDuration(nil)
	require.Error(t, err)

	_, err = stripLiveNoteDuration([]byte{0x15, 0x49, 0xA9, 0x66, 0x85, 0x01})
	require.Error(t, err)

	// A well-formed outer element whose payload is not.
	_, err = stripLiveNoteDuration(elem(t, ebmlIDInfo, []byte{0xE7, 0x85, 0x01}))
	require.Error(t, err)
}

func TestLiveNoteInfoWithDuration_RejectsMalformedInfo(t *testing.T) {
	_, err := liveNoteInfoWithDuration([]byte{0x15, 0x49, 0xA9, 0x66, 0x85, 0x01})
	require.Error(t, err)
}

func TestShiftLiveNoteCluster_RejectsMalformedClusters(t *testing.T) {
	_, err := shiftLiveNoteCluster(nil, 0)
	require.Error(t, err)

	_, err = shiftLiveNoteCluster(elem(t, ebmlIDInfo, nil), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected a Cluster")

	// A Cluster without a Timecode has nothing to shift; silently writing it
	// would place the audio at the wrong minute.
	noTimecode := elem(t, ebmlIDCluster, elem(t, ebmlIDSimpleBlock, []byte{0x01}))
	_, err = shiftLiveNoteCluster(noTimecode, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Timecode element")

	// A 9-byte Timecode is not an unsigned integer this package writes.
	wide := elem(t, ebmlIDCluster, append(
		encodeEBMLID(ebmlIDTimecode), append([]byte{0x89}, make([]byte, 9)...)...,
	))
	_, err = shiftLiveNoteCluster(wide, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timecode of 9 bytes")

	_, err = shiftLiveNoteCluster(elem(t, ebmlIDCluster, []byte{0xE7, 0x85, 0x01}), 0)
	require.Error(t, err)
}

func TestLiveNoteClusterTimecode_ReadsAndToleratesMalformed(t *testing.T) {
	cluster := elem(t, ebmlIDCluster, elem(t, ebmlIDTimecode, encodeLiveNoteUint(4200)))
	assert.Equal(t, uint64(4200), liveNoteClusterTimecode(cluster))

	assert.Zero(t, liveNoteClusterTimecode(nil))
	assert.Zero(t, liveNoteClusterTimecode([]byte{0x1F, 0x43, 0xB6, 0x75, 0x85, 0x01}))
	assert.Zero(t, liveNoteClusterTimecode(elem(t, ebmlIDCluster, []byte{0xE7, 0x85, 0x01})),
		"a malformed child is estimated as zero, not guessed at")
	assert.Zero(t, liveNoteClusterTimecode(elem(t, ebmlIDCluster, elem(t, ebmlIDSimpleBlock, []byte{0x01}))))
}

// The Duration element is authoritative; the last cluster's timecode is the
// fallback for a file that lost it, and is slightly short.
func TestLiveNoteSegmentEndMS_PrefersDurationThenLastCluster(t *testing.T) {
	assert.Equal(t, 8000.0, liveNoteSegmentEndMS(&liveNoteWebM{
		durationMS: 8000,
		clusters:   [][]byte{elem(t, ebmlIDCluster, nil)},
	}))
	assert.Zero(t, liveNoteSegmentEndMS(&liveNoteWebM{}), "no duration and no clusters")

	cluster := elem(t, ebmlIDCluster, elem(t, ebmlIDTimecode, encodeLiveNoteUint(4200)))
	assert.Equal(t, 4200.0, liveNoteSegmentEndMS(&liveNoteWebM{clusters: [][]byte{cluster}}))
}

// ── Merge: failures and unusual layouts ─────────────────────────────────────

// POSIX-only: Go's os.ReadDir on Windows returns the regular file itself as a
// single entry instead of failing with ENOTDIR, so the same construction makes
// the merge succeed there (with nothing to merge, which is the right answer for
// a Windows layout — there is no such thing as a "file where a directory is").
func TestMergeLiveNoteAudio_AudioPathIsAFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.ReadDir on Windows lists a regular file instead of failing")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "audio"), []byte("x"), 0o644))

	_, err := MergeLiveNoteAudio(LiveNotesPathsIn(dir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list audio")
}

// A directory whose name looks like a segment is somebody else's data; only
// regular files are segments.
func TestMergeLiveNoteAudio_SkipsDirectoriesThatLookLikeSegments(t *testing.T) {
	paths := audioPaths(t)
	fake := filepath.Join(paths.Audio, "1700000000000-mic.webm")
	require.NoError(t, os.MkdirAll(fake, 0o755))
	writeSegment(t, paths, 1_700_000_005_000, "mic", testSegment(t, testCluster{timecode: 0}))

	merged, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(paths.Audio, "mic.webm")}, merged.Channels)
	assert.DirExists(t, fake, "a directory is never consumed by the merge")
}

func TestParseLiveNoteAudioSegmentName_RejectsZeroStart(t *testing.T) {
	_, ok := parseLiveNoteAudioSegmentName("0000000000000-mic.webm")
	assert.False(t, ok, "a zero timestamp is not a segment this package wrote")
}

// Two participants and no mix track: nobody duplicates anybody, so both
// channels are kept.
func TestMergeLiveNoteAudio_TwoParticipantsWithoutMixKeepBothChannels(t *testing.T) {
	paths := audioPaths(t)
	writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))
	writeSegment(t, paths, 1_700_000_000_000, "viewer:2", testSegment(t, testCluster{timecode: 0}))

	merged, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	assert.Empty(t, merged.Meeting, "no mix track means no whole-meeting file")
	assert.Len(t, merged.Channels, 2, "neither participant duplicates a mix that does not exist")
}

func TestMergeLiveNoteAudioGroup_ReportsUnpublishableTarget(t *testing.T) {
	dir := t.TempDir()
	segPath := filepath.Join(dir, "1700000000000-mic.webm")
	require.NoError(t, os.WriteFile(segPath, testSegment(t, testCluster{timecode: 0}), 0o644))
	target := filepath.Join(dir, "mic.webm")
	require.NoError(t, os.MkdirAll(target, 0o755))

	_, _, err := mergeLiveNoteAudioGroup(dir, target, []audioSegment{
		{path: segPath, channel: "mic", startedAt: 1_700_000_000_000},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename single segment")
}

// The first segment supplies the header; a corrupt one must fail before
// anything is written, not produce a file with no decoder-readable header.
func TestMergeLiveNoteAudioGroup_ReportsUnreadableFirstSegment(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "1700000000000-mic.webm")
	good := filepath.Join(dir, "1700000005000-mic.webm")
	require.NoError(t, os.WriteFile(bad, []byte("not a webm"), 0o644))
	require.NoError(t, os.WriteFile(good, testSegment(t, testCluster{timecode: 0}), 0o644))

	_, _, err := mergeLiveNoteAudioGroup(dir, filepath.Join(dir, "mic.webm"), []audioSegment{
		{path: bad, startedAt: 1_700_000_000_000},
		{path: good, startedAt: 1_700_000_005_000},
	})
	require.Error(t, err)
}

// The remuxed file is published with a rename; a directory sitting on the
// target name must fail the group rather than leave the temp file behind.
func TestMergeLiveNoteAudioGroup_ReportsUnpublishableRemux(t *testing.T) {
	dir := t.TempDir()
	segA := filepath.Join(dir, "1700000000000-mic.webm")
	segB := filepath.Join(dir, "1700000005000-mic.webm")
	require.NoError(t, os.WriteFile(segA, testSegment(t, testCluster{timecode: 0}), 0o644))
	require.NoError(t, os.WriteFile(segB, testSegment(t, testCluster{timecode: 0}), 0o644))
	target := filepath.Join(dir, "mic.webm")
	require.NoError(t, os.MkdirAll(target, 0o755))

	_, _, err := mergeLiveNoteAudioGroup(dir, target, []audioSegment{
		{path: segA, startedAt: 1_700_000_000_000},
		{path: segB, startedAt: 1_700_000_005_000},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish merged audio")
}

func TestMergeLiveNoteAudioGroup_ReportsUnwritableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for this user")
	}
	dir := t.TempDir()
	segA := filepath.Join(dir, "1700000000000-mic.webm")
	segB := filepath.Join(dir, "1700000005000-mic.webm")
	require.NoError(t, os.WriteFile(segA, testSegment(t, testCluster{timecode: 0}), 0o644))
	require.NoError(t, os.WriteFile(segB, testSegment(t, testCluster{timecode: 0}), 0o644))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, _, err := mergeLiveNoteAudioGroup(dir, filepath.Join(dir, "mic.webm"), []audioSegment{
		{path: segA, startedAt: 1_700_000_000_000},
		{path: segB, startedAt: 1_700_000_005_000},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create merge temp")
}

func TestDropLiveNoteAudioSegments_ToleratesMissingAndReportsUndeletable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, dropLiveNoteAudioSegments([]audioSegment{{path: filepath.Join(dir, "gone.webm")}}),
		"a segment that is already gone is not an error")

	nested := filepath.Join(dir, "segment.webm")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "child"), []byte("x"), 0o644))
	err := dropLiveNoteAudioSegments([]audioSegment{{path: nested}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove duplicate segment")
}

// A cluster the merger cannot shift aborts the whole group: writing the
// remaining clusters would publish a recording with a hole in it.
func TestMergeLiveNoteAudio_RejectsAClusterWithoutTimecode(t *testing.T) {
	paths := audioPaths(t)
	writeSegment(t, paths, 1_700_000_000_000, "mic", testSegment(t, testCluster{timecode: 0}))

	// Build the second segment by hand: header + Segment(Info, Tracks, Cluster
	// with no Timecode).
	segment := append(elem(t, ebmlIDInfo, elem(t, ebmlIDTimecodeScale, []byte{0x0F, 0x42, 0x40})),
		elem(t, ebmlIDTracks, []byte{0xAE, 0x80})...)
	segment = append(segment, elem(t, ebmlIDCluster, elem(t, ebmlIDSimpleBlock, []byte{0x01}))...)
	broken := append(elem(t, ebmlIDHeader, []byte{0x42, 0x86, 0x81, 0x01}), elem(t, ebmlIDSegment, segment)...)
	require.NoError(t, os.WriteFile(filepath.Join(paths.Audio, "1700000005000-mic.webm"), broken, 0o644))

	_, err := MergeLiveNoteAudio(paths)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Timecode element")
}

// A WebM without a Tracks element cannot be remuxed: the result would be a file
// no decoder can open, so the merge refuses instead of writing it.
func TestMergeLiveNoteAudio_RejectsASegmentWithoutTracks(t *testing.T) {
	paths := audioPaths(t)
	body := elem(t, ebmlIDInfo, elem(t, ebmlIDTimecodeScale, []byte{0x0F, 0x42, 0x40}))
	body = append(body, elem(t, ebmlIDCluster, elem(t, ebmlIDTimecode, encodeLiveNoteUint(0)))...)
	headerless := append(elem(t, ebmlIDHeader, []byte{0x42, 0x86, 0x81, 0x01}), elem(t, ebmlIDSegment, body)...)

	writeSegment(t, paths, 1_700_000_000_000, "mic", headerless)
	writeSegment(t, paths, 1_700_000_005_000, "mic", testSegment(t, testCluster{timecode: 0}))

	_, err := MergeLiveNoteAudio(paths)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a self-contained WebM segment")
}
