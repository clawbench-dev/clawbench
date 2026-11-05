package handler

// Edge cases of the import path: the branches that decide *which* error the user
// sees, and the cleanup that keeps a rejected upload from leaving scratch files
// behind. The happy paths live in live_notes_import_test.go; these pin the
// failure modes, which is where an import silently misbehaves (a record left
// "running", a 500 where the panel expects a specific message, a temp file that
// outlives the request).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeLiveNotesImport_MethodNotAllowed(t *testing.T) {
	w := httptest.NewRecorder()
	ServeLiveNotesImport(w, liveReq(t, http.MethodGet, "/api/live-share/notes/import", projPath("/proj/a"), nil))
	assertErrorKey(t, w, http.StatusMethodNotAllowed, "MethodNotAllowed")
}

// A body that is not multipart must read as a bad request, not as an internal
// error: the panel shows the message it gets, and "500" would send the user
// looking for a server fault.
func TestServeLiveNotesImport_RejectsNonMultipartBody(t *testing.T) {
	setupLiveNotesDB(t)
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })

	w := httptest.NewRecorder()
	ServeLiveNotesImport(w, liveReq(t, http.MethodPost, "/api/live-share/notes/import", projPath("/proj/a"),
		map[string]string{"title": "x"}))
	assertErrorKey(t, w, http.StatusBadRequest, "BadRequest")
}

// The upload is written before the meeting row exists, so a failure to create
// the row must take the scratch file with it — otherwise every rejected import
// leaves a multi-gigabyte file in the temp dir.
func TestServeLiveNotesImport_FailsWhenMeetingRowCannotBeWritten(t *testing.T) {
	setupLiveNotesDB(t)
	project := t.TempDir()
	cacheProjectID(t, project)
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })
	dropTable(t, "live_notes")

	before := countImportTempFiles(t)

	w := httptest.NewRecorder()
	ServeLiveNotesImport(w, importRequest(t, project, "会.m4a", []byte("audio")))
	assertErrorKey(t, w, http.StatusInternalServerError, "")

	assert.Equal(t, before, countImportTempFiles(t), "the scratch upload must be removed")
}

// The artifact claim is what decides whether the conversion job runs. When it
// fails the request must not pretend the meeting will be transcribed.
func TestServeLiveNotesImport_FailsWhenJobCannotBeClaimed(t *testing.T) {
	setupLiveNotesDB(t)
	project := t.TempDir()
	cacheProjectID(t, project)
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })
	dropTable(t, "live_notes_artifacts")

	w := httptest.NewRecorder()
	ServeLiveNotesImport(w, importRequest(t, project, "会.m4a", []byte("audio")))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

// countImportTempFiles counts the scratch files an import could leave behind.
func countImportTempFiles(t *testing.T) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "clawbench-import-*"))
	require.NoError(t, err)
	return len(matches)
}

// A dropped file is written once: a second `file` part (some clients send one
// per field) must be ignored, and the first one is the one that becomes the
// meeting's source.
func TestReceiveLiveNoteImport_KeepsTheFirstFilePart(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("title", "  我的会  "))
	require.NoError(t, mw.WriteField("startedAt", "1700000000000"))
	first, err := mw.CreateFormFile("file", "first.mp3")
	require.NoError(t, err)
	_, err = first.Write([]byte("one"))
	require.NoError(t, err)
	second, err := mw.CreateFormFile("file", "second.mp3")
	require.NoError(t, err)
	_, err = second.Write([]byte("two"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/x", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	got, err := receiveLiveNoteImport(req)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupImportFile(got.filePath) })

	assert.Equal(t, "我的会", got.title, "plain fields are trimmed")
	assert.Equal(t, int64(1_700_000_000_000), got.startedAt)
	assert.Equal(t, "first.mp3", got.fileName)
	data, err := os.ReadFile(got.filePath)
	require.NoError(t, err)
	assert.Equal(t, "one", string(data))
}

// An empty file part is the user picking a zero-byte file; it must be refused
// rather than creating a meeting with nothing to transcribe.
func TestReceiveLiveNoteImport_RejectsEmptyFile(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "empty.mp3")
	require.NoError(t, err)
	_, err = part.Write(nil)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/x", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	_, err = receiveLiveNoteImport(req)
	assert.ErrorIs(t, err, errImportNoFile)
}

// A body that stops mid-part is the "connection dropped during upload" case.
// The bytes already written must not survive it.
func TestReceiveLiveNoteImport_CleansUpAfterTruncatedBody(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("title", "t"))
	part, err := mw.CreateFormFile("file", "a.mp3")
	require.NoError(t, err)
	_, err = part.Write([]byte("data"))
	require.NoError(t, err)
	// Deliberately do not close the writer: append the start of another part
	// and stop, so the reader hits an unexpected end of body.
	_, err = buf.WriteString("--" + mw.Boundary() + "\r\nContent-Disposition: form-data; name=\"x")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/x", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	_, err = receiveLiveNoteImport(req)
	require.Error(t, err)
}

// A suffix longer than a real extension is a name, not a type; keeping it would
// create a temp file whose suffix confuses later tooling.
func TestWriteImportFile_DropsAbsurdSuffix(t *testing.T) {
	path, err := writeImportFile(strings.NewReader("x"), "recording.verylongextension")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })

	assert.False(t, strings.HasSuffix(path, ".verylongextension"), path)
}

// A file named only by its extension still gives a usable title instead of an
// empty one (which would fall back to the date placeholder).
func TestLiveNoteImportTitle_ExtensionOnlyName(t *testing.T) {
	assert.Equal(t, ".m4a", liveNoteImportTitle("", ".m4a"))
}

// Without ffprobe the meeting collapses to an instant: wrong by one duration,
// but never in the future and never negative.
func TestLiveNoteImportWindow_WithoutProbe(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "", errors.New("not found") })

	start, end := liveNoteImportWindow(context.Background(), liveNoteImport{startedAt: 1_700_000_000_000})
	assert.Equal(t, int64(1_700_000_000_000), start)
	assert.Equal(t, start, end)
}

func TestFFmpegTranscode_ReportsMissingBinary(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "", errors.New("not found") })

	_, err := ffmpegTranscode(context.Background(), "src", "dst")
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrLiveNoteImport)
	assert.ErrorIs(t, err, ErrLiveNoteImportUnavailable,
		"the artifact row classifies by identity; a vanished ffmpeg is still an import failure")
}

// A decoder that fails is the normal case for a corrupt file: the error must
// carry ffmpeg's own last line, which is what tells "no audio stream" apart
// from "not a media file at all".
func TestFFmpegTranscode_WrapsRunnerFailure(t *testing.T) {
	stubLookPath(t, func(string) (string, error) {
		return filepath.Join(t.TempDir(), "ffmpeg-does-not-exist"), nil
	})

	_, err := ffmpegTranscode(context.Background(), "src", "dst")
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrLiveNoteImport)
}

func TestLastLogLine(t *testing.T) {
	cause := errors.New("exit status 1")
	assert.Equal(t, "exit status 1", lastLogLine("", cause), "empty stderr falls back to the exit error")
	assert.Equal(t, "exit status 1", lastLogLine("  \n\n", cause))
	assert.Equal(t, "the real problem", lastLogLine("warning\n\n  the real problem  \n", cause))
}

// The SPA points at the import UI for this code; "model" would send the user to
// the summary-provider settings instead.
func TestArtifactErrorCode_MapsImport(t *testing.T) {
	err := errors.Join(service.ErrLiveNoteImport, errors.New("Invalid data found"))
	assert.Equal(t, service.ArtifactErrImport, artifactErrorCode(service.ArtifactTranscript, err))
}

// ── Key frames ─────────────────────────────────────────────────────────────

func TestExtractImportFrames_NoLimit(t *testing.T) {
	frames, err := extractImportFrames(context.Background(), "anything", t.TempDir(), 0)
	require.NoError(t, err)
	assert.Nil(t, frames, "a zero budget means the caller wants no frames, not all of them")
}

func TestExtractImportFrames_WithoutFFmpeg(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "", errors.New("not found") })

	_, err := extractImportFrames(context.Background(), "anything", t.TempDir(), 10)
	assert.ErrorIs(t, err, ErrLiveNoteImportUnavailable)
}

// Without ffprobe the file cannot be classified; assuming a picture is the safe
// answer because ffmpeg then decides, and an audio-only file simply yields no
// frames.
func TestHasVideoStream_AssumesVideoWithoutProbe(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "", errors.New("not found") })
	assert.True(t, hasVideoStream(context.Background(), "anything"))
}

func TestParseSceneTimes_SkipsMalformedLines(t *testing.T) {
	raw := strings.Join([]string{
		"frame:0    pts:61440   pts_time:6",
		"frame:1    pts:92160",
		"frame:2    pts:1   pts_time:not-a-number",
		"frame:3    pts:1   pts_time:-1",
		"frame:4    pts:1   pts_time:inf",
		"lavfi.scene_score=0.4",
	}, "\n")
	assert.Equal(t, []int64{6000}, parseSceneTimes(raw))
}

func TestPickSceneFrames_SingleSlot(t *testing.T) {
	times := []int64{0, 20_000, 40_000}
	assert.Equal(t, []int{0}, pickSceneFrames(times, 1), "one slot keeps the opening frame")
	assert.Nil(t, pickSceneFrames(times, 0))
}

// One unreadable image must not cost the rest of the frames.
func TestSaveImportFrames_SkipsUnreadableFile(t *testing.T) {
	saveImportFrames(context.Background(), "token", 1,
		service.LiveNotesPaths{Dir: t.TempDir()}, 1_700_000_000_000,
		[]importFrame{{OffsetMS: 0, Path: filepath.Join(t.TempDir(), "missing.jpg")}})
}

// A shots directory that cannot be created is per-frame failure, not a panic.
func TestSaveImportFrames_ReportsWriteFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	frame := filepath.Join(t.TempDir(), "frame-0001.jpg")
	require.NoError(t, os.WriteFile(frame, realPNG(t, 2, 2), 0o644))

	saveImportFrames(context.Background(), "token", 1,
		service.LiveNotesPaths{Dir: blocker, Shots: filepath.Join(blocker, "shots")},
		1_700_000_000_000, []importFrame{{OffsetMS: 0, Path: frame}})
}

// The file is written first and recorded second; a failed row must delete the
// orphan file, or the meeting's resource list shows a picture it cannot open.
func TestSaveImportFrames_ReportsAppendFailure(t *testing.T) {
	setupLiveNotesDB(t)
	dropTable(t, "live_notes_shots")

	dir := t.TempDir()
	frame := filepath.Join(t.TempDir(), "frame-0001.jpg")
	require.NoError(t, os.WriteFile(frame, realPNG(t, 2, 2), 0o644))

	saveImportFrames(context.Background(), "token", 1,
		service.LiveNotesPaths{Dir: dir, Shots: filepath.Join(dir, "shots")},
		1_700_000_000_000, []importFrame{{OffsetMS: 0, Path: frame}})

	entries, err := os.ReadDir(filepath.Join(dir, "shots"))
	require.NoError(t, err)
	for _, entry := range entries {
		assert.NotEqual(t, ".jpg", filepath.Ext(entry.Name()),
			"the frame that could not be recorded must not stay on disk")
	}
}

// The happy path of the frame writer, driven without ffmpeg: CI runners do not
// ship it, and the row (offset, size, file on disk) is what the record page
// reads — the extraction that produced the file is a separate concern.
func TestSaveImportFrames_RecordsFrames(t *testing.T) {
	setupLiveNotesDB(t)
	project := t.TempDir()
	pid := cacheProjectID(t, project)
	startedAt := int64(1_700_000_000_000)
	token := "importtoken0000000000000000000003"
	paths, err := service.EnsureMeetingDir(token, pid, project, "帧", startedAt)
	require.NoError(t, err)

	dir := t.TempDir()
	first := filepath.Join(dir, "frame-0001.jpg")
	second := filepath.Join(dir, "frame-0002.jpg")
	require.NoError(t, os.WriteFile(first, realPNG(t, 4, 2), 0o644))
	require.NoError(t, os.WriteFile(second, realPNG(t, 8, 6), 0o644))

	saveImportFrames(context.Background(), token, pid, paths, startedAt, []importFrame{
		{OffsetMS: 0, Path: first},
		{OffsetMS: 5_000, Path: second},
	})

	shots, err := service.ListLiveNoteShots(token)
	require.NoError(t, err)
	require.Len(t, shots, 2)
	assert.Equal(t, startedAt, shots[0].At)
	assert.Equal(t, startedAt+5_000, shots[1].At)
	assert.Equal(t, 4, shots[0].Width)
	assert.Equal(t, 2, shots[0].Height)
	assert.Equal(t, service.LiveNoteShotReasonChange, shots[0].Reason)
	assert.FileExists(t, filepath.Join(paths.Dir, shots[0].FileName))
}

// failingReader hands out its bytes and then fails: an upload cut short by a
// dropped connection, at the level the writer sees it.
type failingReader struct {
	data []byte
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestWriteImportFile_ReportsCopyError(t *testing.T) {
	_, err := writeImportFile(&failingReader{data: []byte("partial"), err: errors.New("connection reset")}, "a.mp3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection reset")
}

// A read error in a small field must degrade to an empty value rather than
// failing the whole import: the file part is what matters.
func TestReadImportField_ReturnsEmptyOnError(t *testing.T) {
	got := readImportField(&failingReader{data: []byte("title"), err: errors.New("connection reset")})
	assert.Equal(t, "", got)
}

// A file part whose bytes stop early must be reported as a bad request and the
// partial scratch file removed.
func TestReceiveLiveNoteImport_ReportsFileWriteError(t *testing.T) {
	body := "--BOUND\r\n" +
		"Content-Disposition: form-data; name=\"file\"; filename=\"a.mp3\"\r\n\r\n" +
		"partial"
	req := httptest.NewRequest(http.MethodPost, "/x",
		&failingReader{data: []byte(body), err: errors.New("connection reset")})
	req.Header.Set("Content-Type", `multipart/form-data; boundary=BOUND`)

	_, err := receiveLiveNoteImport(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection reset")
}

// stubMediaCommand replaces the ffmpeg/ffprobe process for one test.
func stubMediaCommand(t *testing.T, fn func(ctx context.Context, name string, args ...string) ([]byte, []byte, error)) {
	t.Helper()
	prev := runMediaCommand
	runMediaCommand = fn
	t.Cleanup(func() { runMediaCommand = prev })
}

// isProbeCall tells the two commands apart by a flag only ffprobe gets.
func isProbeCall(args []string) bool {
	for _, arg := range args {
		if arg == "-select_streams" {
			return true
		}
	}
	return false
}

// isDurationCall matches the ffprobe invocation that reads format=duration.
func isDurationCall(args []string) bool {
	for _, arg := range args {
		if arg == "-show_entries" {
			return true
		}
	}
	return false
}

// writeFrames writes n jpgs next to the output pattern, the way ffmpeg does.
func writeFrames(t *testing.T, pattern string, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		path := filepath.Join(filepath.Dir(pattern), fmt.Sprintf("frame-%04d.jpg", i))
		require.NoError(t, os.WriteFile(path, []byte("jpg"), 0o644))
	}
}

// The pairing between ffmpeg's timestamps and the files it wrote is positional;
// getting it wrong attaches a picture to the wrong moment of the meeting.
func TestExtractImportFrames_PairsTimestampsWithFiles(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })
	stubMediaCommand(t, func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		if isProbeCall(args) {
			return []byte("video\n"), nil, nil
		}
		writeFrames(t, args[len(args)-1], 2)
		// 20s apart: the minimum gap keeps the second frame instead of
		// treating it as a flip-book transition.
		return []byte("frame:0 pts:0 pts_time:0\nlavfi.scene_score=0.5\n" +
			"frame:1 pts:1 pts_time:20\nlavfi.scene_score=0.6\n"), nil, nil
	})

	frames, err := extractImportFrames(context.Background(), "video.mp4", t.TempDir(), 10)
	require.NoError(t, err)
	require.Len(t, frames, 2)
	assert.Equal(t, int64(0), frames[0].OffsetMS)
	assert.Equal(t, int64(20_000), frames[1].OffsetMS)
	assert.Contains(t, frames[1].Path, "frame-0002.jpg")
}

// A mismatch means the filter chain changed shape; guessing would attach wrong
// timestamps, so it is an error.
func TestExtractImportFrames_RejectsTimestampMismatch(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })
	stubMediaCommand(t, func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		if isProbeCall(args) {
			return []byte("video\n"), nil, nil
		}
		writeFrames(t, args[len(args)-1], 2)
		return []byte("frame:0 pts:0 pts_time:0\n"), nil, nil
	})

	_, err := extractImportFrames(context.Background(), "video.mp4", t.TempDir(), 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 frames but 1 timestamps")
}

// A decoder that succeeds without writing anything (an empty video) is "no
// pictures", not an error.
func TestExtractImportFrames_ReturnsNothingWhenNoFrameWritten(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })
	stubMediaCommand(t, func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		if isProbeCall(args) {
			return []byte("video\n"), nil, nil
		}
		return nil, nil, nil
	})

	frames, err := extractImportFrames(context.Background(), "video.mp4", t.TempDir(), 10)
	require.NoError(t, err)
	assert.Nil(t, frames)
}

func TestExtractImportFrames_ReportsDecoderFailure(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })
	stubMediaCommand(t, func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		if isProbeCall(args) {
			return []byte("video\n"), nil, nil
		}
		return nil, []byte("Invalid data found when processing input\n"), errors.New("exit status 1")
	})

	_, err := extractImportFrames(context.Background(), "video.mp4", t.TempDir(), 10)
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrLiveNoteImport)
	assert.Contains(t, err.Error(), "Invalid data found")
}

func TestHasVideoStream_ReadsTheStreamType(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffprobe", nil })

	stubMediaCommand(t, func(context.Context, string, ...string) ([]byte, []byte, error) {
		return []byte("audio\n"), nil, nil
	})
	assert.False(t, hasVideoStream(context.Background(), "a.wav"), "an audio-only import skips frame extraction")

	stubMediaCommand(t, func(context.Context, string, ...string) ([]byte, []byte, error) {
		return []byte("video\n"), nil, nil
	})
	assert.True(t, hasVideoStream(context.Background(), "a.mp4"))
}

func TestProbeDurationMS_ParsesSeconds(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffprobe", nil })

	stubMediaCommand(t, func(context.Context, string, ...string) ([]byte, []byte, error) {
		return []byte("12.5\n"), nil, nil
	})
	assert.Equal(t, int64(12_500), probeDurationMS(context.Background(), "a.webm"))

	stubMediaCommand(t, func(context.Context, string, ...string) ([]byte, []byte, error) {
		return []byte("N/A\n"), nil, nil
	})
	assert.Equal(t, int64(0), probeDurationMS(context.Background(), "a.webm"), "an unparseable duration is 'unknown'")
}

// The converted file's own duration is what the meeting is measured by; the
// source is only the fallback.
func TestFFmpegTranscode_ReportsConvertedDuration(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })
	stubMediaCommand(t, func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		if isDurationCall(args) {
			return []byte("12.5\n"), nil, nil
		}
		return nil, nil, nil
	})

	duration, err := ffmpegTranscode(context.Background(), "src.mp4", "dst.webm")
	require.NoError(t, err)
	assert.Equal(t, int64(12_500), duration)
}

// A video the decoder reports as having no frames leaves the transcript as the
// only product, without failing the meeting.
func TestExtractAndSaveFrames_NoFrames(t *testing.T) {
	setupLiveNotesDB(t)
	stubLookPath(t, func(string) (string, error) { return "/usr/bin/ffmpeg", nil })
	stubMediaCommand(t, func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		if isProbeCall(args) {
			return []byte("video\n"), nil, nil
		}
		return nil, nil, nil
	})

	extractAndSaveFrames(context.Background(), "token", 1,
		service.LiveNotesPaths{Dir: t.TempDir()}, 1_700_000_000_000, "video.mp4")
}

func TestImageSize(t *testing.T) {
	width, height := imageSize([]byte("not an image"))
	assert.Equal(t, 0, width, "an unreadable image only loses the aspect hint")
	assert.Equal(t, 0, height)

	img := image.NewRGBA(image.Rect(0, 0, 7, 3))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	w, h := imageSize(buf.Bytes())
	assert.Equal(t, 7, w)
	assert.Equal(t, 3, h)
}

// A cross-device temp dir makes rename impossible; the copy is the fallback and
// a copy failure is the only case that loses the recording.
func TestStoreImportSource_ReportsCopyFailure(t *testing.T) {
	staged := filepath.Join(t.TempDir(), "staged.mp3")
	require.NoError(t, os.WriteFile(staged, []byte("audio"), 0o644))
	missingDir := filepath.Join(t.TempDir(), "missing", "meeting")

	_, err := storeImportSource(missingDir, staged, "a.mp3")
	require.Error(t, err)
}

// Control characters and the characters Windows refuses would make the name
// unusable (or, worse, allow a path trick); both are stripped or replaced.
func TestLiveNoteImportSourceName_Sanitizes(t *testing.T) {
	assert.Equal(t, "ab_.mp3", liveNoteImportSourceName("a\x01b?.mp3"))
	assert.Equal(t, "source", liveNoteImportSourceName("../.mp3"), "an extension with no name in front of it is dropped")
}

func TestTruncateFileName_KeepsExtension(t *testing.T) {
	assert.Equal(t, "short.mp3", truncateFileName("short.mp3", 20))
	long := truncateFileName(strings.Repeat("名", 200)+".mp3", 10)
	assert.LessOrEqual(t, len([]rune(long)), 10)
	assert.True(t, strings.HasSuffix(long, ".mp3"))

	// No room for the extension: the name is cut, never the extension alone.
	assert.Equal(t, "名", truncateFileName(strings.Repeat("名", 20)+".webm", 1))
}

// A best-effort pass over a file it cannot decode is a no-op, not a failed
// meeting: the transcript is the product.
func TestExtractAndSaveFrames_WithoutFFmpeg(t *testing.T) {
	stubLookPath(t, func(string) (string, error) { return "", errors.New("not found") })
	extractAndSaveFrames(context.Background(), "token", 1,
		service.LiveNotesPaths{Dir: t.TempDir()}, 1_700_000_000_000, "video.mp4")
}
