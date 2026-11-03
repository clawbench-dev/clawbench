package handler

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Minimal WebM fixture ───────────────────────────────────────────────────
//
// The service package builds its fixtures with unexported EBML helpers; the
// handler tests need a real container too, so this is the smallest valid
// Header + Segment(Info, Tracks, Clusters) a decoder can read.

var (
	ebmlHeaderID  = []byte{0x1A, 0x45, 0xDF, 0xA3}
	ebmlSegmentID = []byte{0x18, 0x53, 0x80, 0x67}
	ebmlInfoID    = []byte{0x15, 0x49, 0xA9, 0x66}
	ebmlTracksID  = []byte{0x16, 0x54, 0xAE, 0x6B}
	ebmlClusterID = []byte{0x1F, 0x43, 0xB6, 0x75}
	ebmlTimecode  = []byte{0xE7}
	ebmlBlock     = []byte{0xA3}
)

func ebmlSize(n int) []byte {
	switch {
	case n < 0x7F:
		return []byte{byte(0x80 | n)}
	case n < 0x3FFF:
		return []byte{byte(0x40 | (n >> 8)), byte(n)}
	case n < 0x1FFFFF:
		return []byte{byte(0x20 | (n >> 16)), byte(n >> 8), byte(n)}
	default:
		return []byte{0x10, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	}
}

func ebmlElem(id, payload []byte) []byte {
	out := append([]byte{}, id...)
	out = append(out, ebmlSize(len(payload))...)
	return append(out, payload...)
}

func ebmlUint(v uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	i := 0
	for i < 7 && buf[i] == 0 {
		i++
	}
	return buf[i:]
}

func testWebM(timecodes ...uint64) []byte {
	info := ebmlElem([]byte{0x2A, 0xD7, 0xB1}, []byte{0x0F, 0x42, 0x40})
	info = append(info, ebmlElem([]byte{0x44, 0x89}, make([]byte, 8))...)
	segment := ebmlElem(ebmlInfoID, info)
	segment = append(segment, ebmlElem(ebmlTracksID, []byte{0xAE, 0x80})...)
	for _, tc := range timecodes {
		body := ebmlElem(ebmlTimecode, ebmlUint(tc))
		body = append(body, ebmlElem(ebmlBlock, []byte{0x81, 0x00})...)
		segment = append(segment, ebmlElem(ebmlClusterID, body)...)
	}
	out := ebmlElem(ebmlHeaderID, []byte{0x42, 0x86, 0x81, 0x01})
	return append(out, ebmlElem(ebmlSegmentID, segment)...)
}

// ── Fixtures ───────────────────────────────────────────────────────────────

type countingSTT struct {
	mu    sync.Mutex
	text  string
	err   error
	calls int
}

func (s *countingSTT) Transcribe(context.Context, io.Reader, string) (string, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return s.text, s.err
}

func (s *countingSTT) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func withCountingSTT(t *testing.T, stub *countingSTT) {
	t.Helper()
	previous := GetSTTProvider()
	SetSTTProvider(stub)
	t.Cleanup(func() { SetSTTProvider(previous) })
}

func reasrFixture(t *testing.T) (token string, pid int64, project string, paths service.LiveNotesPaths) {
	t.Helper()
	setupLiveNotesDB(t)
	resetLiveRegistry(t)
	project = t.TempDir()
	pid = cacheProjectID(t, project)
	token = "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	require.NoError(t, service.EndLiveNote(token, 1_700_000_600_000))
	var err error
	paths, err = service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)
	return token, pid, project, paths
}

func writeTestAudio(t *testing.T, paths service.LiveNotesPaths, name string, timecodes ...uint64) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(paths.Audio, name), testWebM(timecodes...), 0o644))
}

func seedOldSegment(t *testing.T, token string, pid int64, channel, speaker string, at int64, text string) {
	t.Helper()
	_, err := service.AppendLiveNoteSegment(context.Background(), token, pid, service.LiveNoteSegment{
		Channel: channel, Speaker: speaker, StartedAt: at, DurationMS: 1_000, Text: text,
	})
	require.NoError(t, err)
}

// ── Tests ──────────────────────────────────────────────────────────────────

func TestBuildTranscriptASR_ReplacesSegmentsAndRenders(t *testing.T) {
	token, pid, project, paths := reasrFixture(t)
	base := int64(1_700_000_000_000)
	seedOldSegment(t, token, pid, "mic", "我", base, "旧内容")
	writeTestAudio(t, paths, "mic.webm", 0, 25_000)
	require.NoError(t, service.RecordLiveNoteAudioChannels(token, []service.LiveNoteAudioChannel{
		{Channel: "mic", File: "mic.webm", BaseAt: base, DurationMS: 30_000},
	}))
	stub := &countingSTT{text: "重新识别"}
	withCountingSTT(t, stub)

	runArtifactChain(token, pid, project, "周会", []service.RegenStep{
		{Kind: service.ArtifactTranscript, Mode: service.RegenModeASR, Reason: service.RegenReasonTarget},
	}, true)

	assert.Equal(t, 2, stub.Calls(), "two 20s windows must be recognized separately")
	segments, err := service.ListLiveNoteSegments(token, 0)
	require.NoError(t, err)
	require.Len(t, segments, 2)
	assert.Equal(t, "mic", segments[0].Channel)
	assert.Equal(t, "我", segments[0].Speaker)
	assert.Equal(t, base, segments[0].StartedAt)
	assert.Equal(t, base+25_000, segments[1].StartedAt)
	assert.Equal(t, "重新识别", segments[0].Text)

	states, err := service.ArtifactStatesForToken(token)
	require.NoError(t, err)
	assert.Equal(t, service.ArtifactJobIdle, states[service.ArtifactTranscript].JobStatus)
	assert.Greater(t, states[service.ArtifactTranscript].ContentAt, int64(0))
	assert.Zero(t, states[service.ArtifactTranscript].PendingASRAt)

	body, err := os.ReadFile(paths.Transcript)
	require.NoError(t, err)
	assert.Contains(t, string(body), "重新识别")
}

func TestBuildTranscriptASR_FailureKeepsOldSegments(t *testing.T) {
	token, pid, project, paths := reasrFixture(t)
	base := int64(1_700_000_000_000)
	seedOldSegment(t, token, pid, "mic", "我", base, "旧内容")
	writeTestAudio(t, paths, "mic.webm", 0, 25_000)
	require.NoError(t, service.RecordLiveNoteAudioChannels(token, []service.LiveNoteAudioChannel{
		{Channel: "mic", File: "mic.webm", BaseAt: base, DurationMS: 30_000},
	}))
	stub := &countingSTT{err: assert.AnError}
	withCountingSTT(t, stub)

	runArtifactChain(token, pid, project, "周会", []service.RegenStep{
		{Kind: service.ArtifactTranscript, Mode: service.RegenModeASR, Reason: service.RegenReasonTarget},
	}, true)

	segments, err := service.ListLiveNoteSegments(token, 0)
	require.NoError(t, err)
	require.Len(t, segments, 1)
	assert.Equal(t, "旧内容", segments[0].Text, "a failed recognition must not touch the old rows")

	states, err := service.ArtifactStatesForToken(token)
	require.NoError(t, err)
	assert.Equal(t, service.ArtifactJobFailed, states[service.ArtifactTranscript].JobStatus)
	assert.Equal(t, service.ArtifactErrSTT, states[service.ArtifactTranscript].ErrorCode)

	_, statErr := os.Stat(paths.Transcript)
	assert.True(t, os.IsNotExist(statErr), "no VTT is written when recognition fails")
}

func TestBuildTranscriptASR_MixOnlyUsesTheMix(t *testing.T) {
	token, pid, project, paths := reasrFixture(t)
	base := int64(1_700_000_000_000)
	seedOldSegment(t, token, pid, "mic", "我", base, "旧内容")
	writeTestAudio(t, paths, service.LiveNoteMeetingAudioFile, 0, 25_000)
	stub := &countingSTT{text: "混音识别"}
	withCountingSTT(t, stub)

	runArtifactChain(token, pid, project, "周会", []service.RegenStep{
		{Kind: service.ArtifactTranscript, Mode: service.RegenModeASR, Reason: service.RegenReasonTarget},
	}, true)

	segments, err := service.ListLiveNoteSegments(token, 0)
	require.NoError(t, err)
	require.Len(t, segments, 2)
	assert.Equal(t, service.LiveNoteMixChannel, segments[0].Channel)
	assert.Equal(t, "我", segments[0].Speaker, "the solo speaker label is copied from the old transcript")
	assert.Equal(t, base, segments[0].StartedAt)
}

func TestRegenAudioState_MixedOnlyIsNotIncomplete(t *testing.T) {
	token, pid, _, paths := reasrFixture(t)
	base := int64(1_700_000_000_000)
	seedOldSegment(t, token, pid, "mic", "我", base, "旧内容")
	writeTestAudio(t, paths, service.LiveNoteMeetingAudioFile, 0)
	segments, err := service.ListLiveNoteSegments(token, 0)
	require.NoError(t, err)

	incomplete, mixedOnly, baseKnown, err := regenAudioState(token, paths.Dir, true, segments)
	require.NoError(t, err)
	assert.False(t, incomplete, "the mix covers the solo channel")
	assert.True(t, mixedOnly)
	assert.False(t, baseKnown, "a merged file without metadata has no origin")
}

func TestRegenAudioState_PerChannelHoleIsIncomplete(t *testing.T) {
	token, pid, _, paths := reasrFixture(t)
	base := int64(1_700_000_000_000)
	seedOldSegment(t, token, pid, "mic", "我", base, "一")
	seedOldSegment(t, token, pid, "viewer:v1", "观众 1", base+1_000, "二")
	writeTestAudio(t, paths, "mic.webm", 0)
	segments, err := service.ListLiveNoteSegments(token, 0)
	require.NoError(t, err)

	incomplete, mixedOnly, _, err := regenAudioState(token, paths.Dir, true, segments)
	require.NoError(t, err)
	assert.True(t, incomplete, "viewer:v1 produced text but has no recording")
	assert.False(t, mixedOnly)
}

func TestRegenAudioState_SegmentFilesStillCarryTheirOrigin(t *testing.T) {
	token, pid, _, paths := reasrFixture(t)
	base := int64(1_700_000_000_000)
	seedOldSegment(t, token, pid, "mic", "我", base, "一")
	require.NoError(t, os.WriteFile(
		filepath.Join(paths.Audio, "1700000000000-mic.webm"), testWebM(0), 0o644))
	segments, err := service.ListLiveNoteSegments(token, 0)
	require.NoError(t, err)

	incomplete, mixedOnly, baseKnown, err := regenAudioState(token, paths.Dir, true, segments)
	require.NoError(t, err)
	assert.False(t, incomplete)
	assert.False(t, mixedOnly)
	assert.True(t, baseKnown, "an unmerged segment still has its timestamp in the file name")
}
