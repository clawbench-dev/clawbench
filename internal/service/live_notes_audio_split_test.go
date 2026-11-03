package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSplitFixture(t *testing.T, clusters ...testCluster) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mic.webm")
	require.NoError(t, os.WriteFile(path, testSegment(t, clusters...), 0o644))
	return path
}

func TestSplitLiveNoteAudio_CutsWindowsAndResetsTimecodes(t *testing.T) {
	path := writeSplitFixture(t,
		testCluster{timecode: 0, blocks: [][]byte{{0x81, 0x00}}},
		testCluster{timecode: 5_000, blocks: [][]byte{{0x81, 0x00}}},
		testCluster{timecode: 10_000, blocks: [][]byte{{0x81, 0x00}}},
		testCluster{timecode: 20_000, blocks: [][]byte{{0x81, 0x00}}},
		testCluster{timecode: 25_000, blocks: [][]byte{{0x81, 0x00}}},
		testCluster{timecode: 40_000, blocks: [][]byte{{0x81, 0x00}}},
	)

	chunks, err := SplitLiveNoteAudio(path, 20_000)
	require.NoError(t, err)
	require.Len(t, chunks, 3)
	assert.Equal(t, int64(0), chunks[0].OffsetMS)
	assert.Equal(t, int64(20_000), chunks[1].OffsetMS)
	assert.Equal(t, int64(40_000), chunks[2].OffsetMS)

	for i, chunk := range chunks {
		assert.NotEmpty(t, chunk.WebM, "chunk %d", i)
		assert.Greater(t, chunk.DurationMS, int64(0), "chunk %d", i)
	}
}

func TestSplitLiveNoteAudio_ChunksAreSelfContainedWebM(t *testing.T) {
	path := writeSplitFixture(t,
		testCluster{timecode: 0, blocks: [][]byte{{0x81, 0x00}}},
		testCluster{timecode: 5_000, blocks: [][]byte{{0x81, 0x00}}},
		testCluster{timecode: 25_000, blocks: [][]byte{{0x81, 0x00}}},
	)
	chunks, err := SplitLiveNoteAudio(path, 20_000)
	require.NoError(t, err)
	require.Len(t, chunks, 2)

	dir := t.TempDir()
	for i, chunk := range chunks {
		chunkPath := filepath.Join(dir, "chunk-"+string(rune('a'+i))+".webm")
		require.NoError(t, os.WriteFile(chunkPath, chunk.WebM, 0o644))
		parsed, err := readLiveNoteWebM(chunkPath)
		require.NoError(t, err, "chunk %d", i)
		assert.NotEmpty(t, parsed.header, "chunk %d", i)
		assert.NotEmpty(t, parsed.info, "chunk %d", i)
		assert.NotEmpty(t, parsed.tracks, "chunk %d", i)
		require.NotEmpty(t, parsed.clusters, "chunk %d", i)
		assert.Equal(t, uint64(0), liveNoteClusterTimecode(parsed.clusters[0]),
			"chunk %d must start at 0", i)
		assert.Greater(t, parsed.durationMS, float64(0), "chunk %d duration", i)
	}
}

func TestSplitLiveNoteAudio_SingleCluster(t *testing.T) {
	path := writeSplitFixture(t, testCluster{timecode: 0, blocks: [][]byte{{0x81, 0x00}}})
	chunks, err := SplitLiveNoteAudio(path, 0)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	assert.Equal(t, int64(0), chunks[0].OffsetMS)
	assert.Greater(t, chunks[0].DurationMS, int64(0))
}

func TestSplitLiveNoteAudio_RejectsNonWebM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not.webm")
	require.NoError(t, os.WriteFile(path, []byte("not a webm"), 0o644))
	_, err := SplitLiveNoteAudio(path, 20_000)
	assert.Error(t, err)
}

func TestMergeLiveNoteAudio_RecordsOrigins(t *testing.T) {
	paths := audioPaths(t)
	writeSegment(t, paths, 1_700_000_000_000, "mic",
		testSegment(t, testCluster{timecode: 0, blocks: [][]byte{{0x81, 0x00}}}))
	writeSegment(t, paths, 1_700_000_010_000, "mic",
		testSegment(t, testCluster{timecode: 0, blocks: [][]byte{{0x81, 0x00}}}))

	merged, err := MergeLiveNoteAudio(paths)
	require.NoError(t, err)
	assert.Equal(t, int64(1_700_000_000_000), merged.Origins["mic"])
	assert.Greater(t, merged.Durations["mic"], int64(0))
	assert.Equal(t, "mic.webm", merged.Files["mic"])
}
