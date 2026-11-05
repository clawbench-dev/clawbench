package handler

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// importRequest builds the multipart body the panel sends for one dropped file.
func importRequest(t *testing.T, project, fileName string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("startedAt", "1700000000000"))
	fw, err := mw.CreateFormFile("file", fileName)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/live-share/notes/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: url.QueryEscape(project),
	})
	return req
}

// stubTranscode replaces the ffmpeg call for one test.
func stubTranscode(t *testing.T, fn func(ctx context.Context, src, dst string) (int64, error)) {
	t.Helper()
	prev := transcodeToWebM
	transcodeToWebM = fn
	t.Cleanup(func() { transcodeToWebM = prev })
}

// stubLookPath makes ffmpeg look installed (or not) for one test.
func stubLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	prev := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = prev })
}

func TestServeLiveNotesImport_RequiresProject(t *testing.T) {
	setupLiveNotesDB(t)

	w := httptest.NewRecorder()
	ServeLiveNotesImport(w, importRequest(t, "", "a.mp3", []byte("x")))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeLiveNotesImport_UnavailableWithoutFFmpeg(t *testing.T) {
	setupLiveNotesDB(t)
	stubLookPath(t, func(string) (string, error) { return "", errors.New("not found") })

	w := httptest.NewRecorder()
	ServeLiveNotesImport(w, importRequest(t, projPath("/proj/a"), "a.mp3", []byte("x")))
	assertErrorKey(t, w, http.StatusServiceUnavailable, "MeetingImportUnavailable")
}

// The list carries the capability so the panel knows BEFORE it accepts a
// multi-gigabyte upload that the server cannot decode anything.
func TestServeLiveNotesList_ReportsImportAvailability(t *testing.T) {
	setupLiveNotesDB(t)
	project := projPath("/proj/a")

	stubLookPath(t, func(string) (string, error) { return "", errors.New("not found") })
	w := httptest.NewRecorder()
	ServeLiveNotesSessions(w, liveReq(t, http.MethodGet, "/api/live-share/notes/sessions", project, nil))
	require.Equal(t, http.StatusOK, w.Code)
	var blocked struct {
		Import struct {
			Available bool   `json:"available"`
			Reason    string `json:"reason"`
		} `json:"import"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &blocked))
	assert.False(t, blocked.Import.Available)
	assert.Equal(t, "ffmpeg_missing", blocked.Import.Reason)

	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })
	w = httptest.NewRecorder()
	ServeLiveNotesSessions(w, liveReq(t, http.MethodGet, "/api/live-share/notes/sessions", project, nil))
	require.Equal(t, http.StatusOK, w.Code)
	var allowed struct {
		Import struct {
			Available bool `json:"available"`
		} `json:"import"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &allowed))
	assert.True(t, allowed.Import.Available)
}

func TestServeLiveNotesImport_RejectsMissingFile(t *testing.T) {
	setupLiveNotesDB(t)
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("title", "只有标题"))
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/live-share/notes/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: url.QueryEscape(projPath("/proj/a")),
	})

	w := httptest.NewRecorder()
	ServeLiveNotesImport(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// A dropped file becomes a record named after the file, dated by its own
// timestamp, and the conversion failure is visible on the record instead of
// being swallowed.
func TestServeLiveNotesImport_CreatesRecordAndReportsFailure(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	project := t.TempDir()
	pid := cacheProjectID(t, project)
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })
	stubTranscode(t, func(context.Context, string, string) (int64, error) {
		return 0, errors.New("Invalid data found when processing input")
	})

	w := httptest.NewRecorder()
	ServeLiveNotesImport(w, importRequest(t, project, "产品评审会.m4a", []byte("not really audio")))
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Token   string                `json:"token"`
		Session service.MeetingRecord `json:"session"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Token, 32)
	// The file name is a better name than the date placeholder a recorded
	// meeting gets, and it is not a placeholder, so the model will not rename it.
	assert.Equal(t, "产品评审会", resp.Session.Title)
	assert.Equal(t, int64(1_700_000_000_000), resp.Session.StartedAt)
	assert.NotEmpty(t, resp.Session.Dir, "the meeting folder is created before the upload is accepted")

	// The conversion runs detached; wait for it to settle on the record.
	require.Eventually(t, func() bool {
		states, err := service.ArtifactStatesForToken(resp.Token)
		return err == nil && states[service.ArtifactTranscript].JobStatus == service.ArtifactJobFailed
	}, 5*time.Second, 20*time.Millisecond)
	states, err := service.ArtifactStatesForToken(resp.Token)
	require.NoError(t, err)
	assert.Equal(t, service.ArtifactErrImport, states[service.ArtifactTranscript].ErrorCode)

	// A failed import must still be closed: an open record reads as 进行中 and
	// the footer would offer to join it.
	record, ok, err := service.GetMeetingRecord(project, pid, resp.Token)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Greater(t, record.EndedAt, int64(0))
}

// The full path: a file the transcoder produced becomes a transcript, exactly
// the way a recorded meeting does.
func TestRunLiveNoteImport_TranscribesConvertedRecording(t *testing.T) {
	setupLiveNotesDB(t)
	project := t.TempDir()
	pid := cacheProjectID(t, project)
	startedAt := int64(1_700_000_000_000)
	paths, err := service.EnsureMeetingDir("importtoken0000000000000000000001", pid, project, "导入", startedAt)
	require.NoError(t, err)

	stubTranscode(t, func(_ context.Context, _ string, dst string) (int64, error) {
		require.NoError(t, os.WriteFile(dst, testWebM(0, 25_000), 0o644))
		return 30_000, nil
	})
	stub := &countingSTT{text: "导入后的转写"}
	withCountingSTT(t, stub)

	src := filepath.Join(t.TempDir(), "source.mp4")
	require.NoError(t, os.WriteFile(src, []byte("video"), 0o644))
	runLiveNoteImport(
		"importtoken0000000000000000000001", pid, project, "导入", startedAt, paths,
		src, "",
		[]service.RegenStep{{
			Kind: service.ArtifactTranscript, Mode: service.RegenModeASR, Reason: service.RegenReasonTarget,
		}},
	)

	assert.Equal(t, 2, stub.Calls(), "a 30s recording is two 20s windows")
	segments, err := service.ListLiveNoteSegments("importtoken0000000000000000000001", 0)
	require.NoError(t, err)
	require.Len(t, segments, 2)
	assert.Equal(t, service.LiveNoteMixChannel, segments[0].Channel)
	assert.Equal(t, startedAt, segments[0].StartedAt)
	assert.Equal(t, startedAt+25_000, segments[1].StartedAt)
	assert.Equal(t, "导入后的转写", segments[0].Text)
	assert.NotEmpty(t, segments[0].Speaker, "a mix-only recording still needs a speaker label")

	_, statErr := os.Stat(paths.Transcript)
	require.NoError(t, statErr, "the VTT is written like any other meeting")

	_, statErr = os.Stat(src)
	assert.NoError(t, statErr, "the recording itself stays with the meeting")

	// The finish line is stamped by the handler (a dropped file is never a live
	// meeting); this pipeline only owns the artifacts.
	_, _, err = service.GetLiveNote("importtoken0000000000000000000001")
	require.NoError(t, err)
}

func TestLiveNoteImportPlan_FollowsTheProofreadSetting(t *testing.T) {
	prev := model.ConfigInstance.Meeting.Proofread
	t.Cleanup(func() { model.ConfigInstance.Meeting.Proofread = prev })

	model.ConfigInstance.Meeting.Proofread = true
	plan := liveNoteImportPlan()
	require.Len(t, plan, 3)
	assert.Equal(t, service.ArtifactTranscript, plan[0].Kind)
	assert.Equal(t, service.RegenModeASR, plan[0].Mode, "an import has no segments to render")
	assert.Equal(t, service.ArtifactRevised, plan[1].Kind)
	assert.Equal(t, service.ArtifactMinutes, plan[2].Kind)

	model.ConfigInstance.Meeting.Proofread = false
	plan = liveNoteImportPlan()
	require.Len(t, plan, 2)
	assert.Equal(t, service.ArtifactMinutes, plan[1].Kind)
}

func TestLiveNoteImportTitle(t *testing.T) {
	cases := []struct{ given, file, want string }{
		{"", "周会录音.m4a", "周会录音"},
		{"  用户填的  ", "周会录音.m4a", "用户填的"},
		{"", "no-extension", "no-extension"},
		{"", "", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, liveNoteImportTitle(c.given, c.file))
	}
}

func TestLiveNoteImportStart_ClampsFutureTimestamps(t *testing.T) {
	assert.Equal(t, int64(1_700_000_000_000), liveNoteImportStart(1_700_000_000_000))

	now := time.Now().UnixMilli()
	assert.InDelta(t, now, liveNoteImportStart(now+86_400_000), 2_000)
	assert.InDelta(t, now, liveNoteImportStart(0), 2_000)
	assert.InDelta(t, now, liveNoteImportStart(-5), 2_000)
}

// The conversion flags are the contract with the rest of the pipeline: the
// output must be a WebM the cluster splitter can cut, or an imported meeting
// could never be re-recognized. Skipped where ffmpeg is not installed — that is
// exactly the state the endpoint reports as unavailable.
func TestFFmpegTranscode_ProducesSplittableWebM(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	src := filepath.Join(t.TempDir(), "tone.wav")
	require.NoError(t, writeTestWAV(src, 6*time.Second))
	dst := filepath.Join(t.TempDir(), "meeting.webm")

	duration, err := ffmpegTranscode(context.Background(), src, dst)
	require.NoError(t, err)
	assert.InDelta(t, 6000, duration, 500)

	chunks, err := service.SplitLiveNoteAudio(dst, 2_000)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(chunks), 2)
	assert.NotEmpty(t, chunks[0].WebM)
}

// writeTestWAV writes a silent 16 kHz mono PCM file: enough for a decoder to
// chew on without shipping a binary fixture.
func writeTestWAV(path string, d time.Duration) error {
	const rate = 16_000
	samples := int(d.Seconds() * rate)
	data := make([]byte, samples*2)
	header := make([]byte, 0, 44+len(data))
	header = header[:44]
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+len(data)))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1) // PCM
	binary.LittleEndian.PutUint16(header[22:], 1) // mono
	binary.LittleEndian.PutUint32(header[24:], rate)
	binary.LittleEndian.PutUint32(header[28:], rate*2)
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(len(data)))
	return os.WriteFile(path, append(header, data...), 0o644)
}

func TestLiveNoteImportSpan_BacksTheStartOutOfTheDuration(t *testing.T) {
	// A recorder writes as it records: the file's timestamp is the END.
	start, end := liveNoteImportSpan(1_700_003_600_000, 3_600_000)
	assert.Equal(t, int64(1_700_000_000_000), start)
	assert.Equal(t, int64(1_700_003_600_000), end)

	// No duration to subtract: the meeting collapses to an instant rather than
	// inventing a span.
	start, end = liveNoteImportSpan(1_700_003_600_000, 0)
	assert.Equal(t, int64(1_700_003_600_000), start)
	assert.Equal(t, start, end)

	// A bogus duration longer than the epoch itself must not produce a negative
	// start.
	start, end = liveNoteImportSpan(1_000, 5_000)
	assert.Equal(t, int64(1_000), start)
	assert.Equal(t, start, end)
}

// ── Key frames from an imported video ──────────────────────────────────────

func TestParseSceneTimes(t *testing.T) {
	raw := "frame:0    pts:61440   pts_time:6\nlavfi.scene_score=0.4\n" +
		"frame:1    pts:92160   pts_time:9.033333\nlavfi.scene_score=0.51\n" +
		"garbage line\n"
	assert.Equal(t, []int64{6000, 9033}, parseSceneTimes(raw))
	assert.Empty(t, parseSceneTimes(""))
}

func TestPickSceneFrames_KeepsTheFirstAndSpacesTheRest(t *testing.T) {
	times := []int64{0, 3_000, 16_000, 17_000, 40_000}
	// 15s apart: 0 kept, 3s too close, 16s kept, 17s too close, 40s kept.
	assert.Equal(t, []int{0, 2, 4}, pickSceneFrames(times, 10))
	assert.Nil(t, pickSceneFrames(times, 0))
}

// The cap must cover the WHOLE recording, not its beginning: a two-hour meeting
// whose first sixty changes all happen in the first twenty minutes would
// otherwise show pictures of nothing but the opening.
func TestPickSceneFrames_SpreadsWhenTheCapBites(t *testing.T) {
	times := make([]int64, 0, 100)
	for i := range 100 {
		times = append(times, int64(i)*20_000)
	}
	picked := pickSceneFrames(times, 10)
	require.Len(t, picked, 10)
	assert.Equal(t, 0, picked[0], "the opening frame survives")
	assert.Equal(t, 99, picked[len(picked)-1], "and so does the last change")
	// Evenly spaced, so consecutive picks are far apart.
	assert.Greater(t, times[picked[1]]-times[picked[0]], int64(60_000))
}

func TestLiveNoteImportSourceName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"产品评审会.mp4", "产品评审会.mp4"},
		{"../../etc/passwd", "passwd"},
		{"minutes.md", "source-minutes.md"},
		{"", "source"},
		{".hidden.mp4", "source.mp4"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, liveNoteImportSourceName(c.in), "input %q", c.in)
	}

	// A colon is a drive separator on Windows, so `filepath.Base` keeps only
	// what follows it there. Both outcomes are safe names; what must hold on
	// every platform is that nothing the filesystem refuses survives and the
	// extension is preserved.
	colon := liveNoteImportSourceName("a:b?.mkv")
	assert.NotContains(t, colon, ":")
	assert.NotContains(t, colon, "?")
	assert.True(t, strings.HasSuffix(colon, ".mkv"), colon)

	// Long names keep their extension so the file still opens as what it is.
	long := liveNoteImportSourceName(strings.Repeat("会", 200) + ".mp4")
	assert.LessOrEqual(t, len([]rune(long)), 120)
	assert.True(t, strings.HasSuffix(long, ".mp4"))
}

// A real decode: two solid colors joined by a hard cut is exactly what scene
// detection is for. Skipped where ffmpeg is not installed (the same condition
// the endpoint reports as "unavailable").
func TestExtractImportFrames_FindsTheCut(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	video := filepath.Join(t.TempDir(), "cuts.mp4")
	build := exec.Command("ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=red:size=320x240:rate=5:duration=16",
		"-f", "lavfi", "-i", "color=c=blue:size=320x240:rate=5:duration=2",
		"-filter_complex", "[0][1]concat=n=2:v=1:a=0",
		"-pix_fmt", "yuv420p", video)
	require.NoError(t, build.Run())

	frames, err := extractImportFrames(context.Background(), video, t.TempDir(), 10)
	require.NoError(t, err)
	require.Len(t, frames, 2, "the opening frame plus the cut")
	assert.Equal(t, int64(0), frames[0].OffsetMS)
	assert.InDelta(t, 16_000, frames[1].OffsetMS, 300)

	cfg, _, err := image.DecodeConfig(bytes.NewReader(mustReadFile(t, frames[0].Path)))
	require.NoError(t, err)
	assert.LessOrEqual(t, cfg.Width, liveNoteImportFrameMaxWidth)
}

func TestExtractImportFrames_AudioOnlyHasNoFrames(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	audio := filepath.Join(t.TempDir(), "voice.wav")
	require.NoError(t, writeTestWAV(audio, time.Second))

	frames, err := extractImportFrames(context.Background(), audio, t.TempDir(), 10)
	require.NoError(t, err)
	assert.Empty(t, frames, "an audio-only import has nothing to photograph")
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

// The recording is a resource of the meeting, not a scratch file: the folder
// keeps what the user dropped so it can be opened later.
func TestServeLiveNotesImport_KeepsTheRecording(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	project := t.TempDir()
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })
	stubTranscode(t, func(context.Context, string, string) (int64, error) {
		return 0, errors.New("stop here")
	})

	payload := []byte("the original bytes")
	w := httptest.NewRecorder()
	ServeLiveNotesImport(w, importRequest(t, project, "访谈 原片.mp4", payload))
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Token   string                `json:"token"`
		Session service.MeetingRecord `json:"session"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Session.Dir)

	dir := service.MeetingDirPath(project, resp.Session.Dir)
	kept, err := os.ReadFile(filepath.Join(dir, "访谈 原片.mp4"))
	require.NoError(t, err, "the dropped file must be inside the meeting folder")
	assert.Equal(t, payload, kept)

	// It is a root resource, not an audio channel: a `.webm` in audio/ would be
	// mistaken for a recording channel by the merge and by re-recognition.
	entries, err := os.ReadDir(filepath.Join(dir, "audio"))
	require.NoError(t, err)
	for _, entry := range entries {
		// The folder's own .gitignore is not a recording.
		assert.Equal(t, ".gitignore", entry.Name())
	}
}

// End to end over a real video: conversion, transcript and the automatically
// captured key frames.
func TestRunLiveNoteImport_CapturesKeyFrames(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	setupLiveNotesDB(t)
	project := t.TempDir()
	pid := cacheProjectID(t, project)
	startedAt := int64(1_700_000_000_000)
	token := "importtoken0000000000000000000002"
	paths, err := service.EnsureMeetingDir(token, pid, project, "录屏", startedAt)
	require.NoError(t, err)

	video := filepath.Join(paths.Dir, "screen.mp4")
	// The cut sits past the minimum gap: a change 2s after the opening frame is
	// deliberately NOT kept (that would make a flip book of every transition).
	build := exec.Command("ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=red:size=320x240:rate=10:duration=16",
		"-f", "lavfi", "-i", "color=c=blue:size=320x240:rate=10:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=18",
		"-filter_complex", "[0][1]concat=n=2:v=1:a=0[v]",
		"-map", "[v]", "-map", "2:a", "-shortest", "-pix_fmt", "yuv420p", video)
	require.NoError(t, build.Run())

	stub := &countingSTT{text: "录屏里的讲解"}
	withCountingSTT(t, stub)

	runLiveNoteImport(token, pid, project, "录屏", startedAt, paths, video, "",
		[]service.RegenStep{{
			Kind: service.ArtifactTranscript, Mode: service.RegenModeASR, Reason: service.RegenReasonTarget,
		}})

	shots, err := service.ListLiveNoteShots(token)
	require.NoError(t, err)
	require.Len(t, shots, 2, "the opening frame plus the hard cut")
	assert.Equal(t, service.LiveNoteShotReasonChange, shots[0].Reason)
	assert.Equal(t, startedAt, shots[0].At)
	assert.InDelta(t, startedAt+16_000, shots[1].At, 500)
	assert.Greater(t, shots[0].Width, 0)
	assert.FileExists(t, filepath.Join(paths.Dir, shots[0].FileName))
}
