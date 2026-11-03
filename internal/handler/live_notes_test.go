package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// stubSTT records the call and returns a canned transcription.
type stubSTT struct {
	text  string
	err   error
	calls int
}

func (s *stubSTT) Transcribe(context.Context, io.Reader, string) (string, error) {
	s.calls++
	return s.text, s.err
}

func setupLiveNotesDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(store.ProjectsDDL)
	require.NoError(t, err)
	_, err = db.Exec(service.LiveNotesDDL)
	require.NoError(t, err)
	// Meeting events are recorded in the notification log (the durable half of
	// a meeting notification), so the schema has to be there for any test that
	// drives a meeting socket.
	_, err = db.Exec(service.NotificationsDDL)
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

// notesUpload builds the multipart request the publisher sends for one segment.
func notesUpload(t *testing.T, project string, fields map[string]string, audio []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		require.NoError(t, mw.WriteField(k, v))
	}
	fw, err := mw.CreateFormFile("file", "segment.webm")
	require.NoError(t, err)
	_, err = fw.Write(audio)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/live-share/notes/segment", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if project != "" {
		req.AddCookie(&http.Cookie{
			Name:  model.ScopedCookieName("clawbench_project"),
			Value: url.QueryEscape(project),
		})
	}
	return req
}

func TestServeLiveShareNoteSegment_RequiresProject(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, "", map[string]string{"token": "abc"}, []byte("x")))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeLiveShareNoteSegment_StoresTranscribedSegment(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)
	room.SetNotesEnabled(true)

	stub := &stubSTT{text: "  大家好  "}
	prev := GetSTTProvider()
	SetSTTProvider(stub)
	t.Cleanup(func() { SetSTTProvider(prev) })

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, project, map[string]string{
		"token":      room.Token(),
		"channel":    "mic",
		"speaker":    "我",
		"startedAt":  "1700000000000",
		"durationMs": "10000",
	}, []byte("audio-bytes")))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "大家好", resp["text"])
	assert.Equal(t, "mic", resp["channel"])
	assert.Greater(t, resp["id"], float64(0))
	assert.Equal(t, 1, stub.calls)

	segments, err := service.ListLiveNoteSegments(room.Token(), 0)
	require.NoError(t, err)
	require.Len(t, segments, 1)
	assert.Equal(t, "大家好", segments[0].Text)
	assert.Equal(t, int64(1700000000000), segments[0].StartedAt)
}

func TestServeLiveShareNoteSegment_SilenceIsNotStored(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	prev := GetSTTProvider()
	SetSTTProvider(&stubSTT{text: "   "})
	t.Cleanup(func() { SetSTTProvider(prev) })

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, project, map[string]string{
		"token":   room.Token(),
		"channel": "system",
	}, []byte("audio")))

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(0), resp["id"])
}

func TestServeLiveShareNoteSegment_ArchiveSkipsTranscription(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := projPath(t.TempDir())
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	stub := &stubSTT{text: "不该被调用"}
	prev := GetSTTProvider()
	SetSTTProvider(stub)
	t.Cleanup(func() { SetSTTProvider(prev) })

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, project, map[string]string{
		"token":     room.Token(),
		"channel":   service.LiveNoteMixChannel,
		"startedAt": "1700000000000",
		"archive":   "1",
	}, []byte("audio")))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	// The mix track has no single speaker: transcribing it would only produce a
	// duplicate of the per-channel lines with the labels lost.
	assert.Equal(t, 0, stub.calls)

	dirName, err := service.MeetingDirName(room.Token())
	require.NoError(t, err)
	audioDir := service.LiveNotesPathsIn(service.MeetingDirPath(project, dirName)).Audio
	entries, err := os.ReadDir(audioDir)
	require.NoError(t, err)
	// .gitignore keeps the binary audio out of the repository; the segment is
	// the only recording so far.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Contains(t, names, "1700000000000-mix.webm")
}

func TestServeLiveNotesAudioMerge_PublishesMergedFiles(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := projPath(t.TempDir())
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)
	room.SetNotesEnabled(true)

	prev := GetSTTProvider()
	SetSTTProvider(&stubSTT{text: "大家好"})
	t.Cleanup(func() { SetSTTProvider(prev) })

	upload := func(channel, startedAt string, archive bool) {
		t.Helper()
		fields := map[string]string{
			"token":     room.Token(),
			"channel":   channel,
			"startedAt": startedAt,
		}
		if archive {
			fields["archive"] = "1"
		}
		w := httptest.NewRecorder()
		// A real MediaRecorder segment starts with the EBML magic, and the merge
		// validates it even on the single-segment (rename) path.
		body := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, []byte("audio-"+channel)...)
		ServeLiveShareNoteSegment(w, notesUpload(t, project, fields, body))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	upload("mic", "1700000000000", false)
	// A second participant, so this is not a solo meeting: with only the mix and
	// one channel the merge deliberately publishes just meeting.webm (the two
	// would be the same audio).
	upload("viewer:2", "1700000000000", false)
	upload(service.LiveNoteMixChannel, "1700000000000", true)

	w := httptest.NewRecorder()
	ServeLiveNotesAudioMerge(w, liveReq(t, http.MethodPost, "/api/live-share/notes/audio/merge", project,
		map[string]string{"token": room.Token()}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	dirName, err := service.MeetingDirName(room.Token())
	require.NoError(t, err)
	dir := service.MeetingDirPath(project, dirName)
	assert.FileExists(t, filepath.Join(dir, "audio", "mic.webm"))
	assert.FileExists(t, filepath.Join(dir, "audio", "viewer-2.webm"))
	assert.FileExists(t, filepath.Join(dir, "audio", service.LiveNoteMeetingAudioFile))

	var resp struct {
		OK        bool               `json:"ok"`
		Artifacts liveNotesArtifacts `json:"artifacts"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.OK)
	assert.Equal(t, filepath.Join(dir, "audio", service.LiveNoteMeetingAudioFile), resp.Artifacts.AudioMeeting)
	// The segments are gone: the user opens one recording, not a folder of them.
	// The count is the channel recordings — the merged file is not a channel.
	assert.Equal(t, 2, resp.Artifacts.AudioFiles)
}

func TestServeLiveNotesAudioMerge_OtherProjectIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := projPath(t.TempDir())
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveNotesAudioMerge(w, liveReq(t, http.MethodPost, "/api/live-share/notes/audio/merge", projPath(t.TempDir()),
		map[string]string{"token": room.Token()}))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeLiveShareNoteSegment_RejectsUnknownChannel(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, project, map[string]string{
		"token":   room.Token(),
		"channel": "speaker/1",
	}, []byte("audio")))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeLiveShareNoteSegment_UnknownTokenIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, projPath("/proj/a"), map[string]string{
		"token":   "0123456789abcdef0123456789abcdef",
		"channel": "mic",
	}, []byte("audio")))

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Another project must not be able to write into a room it does not own, even
// though it knows the token (every viewer knows it).
func TestServeLiveShareNoteSegment_OtherProjectIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	room, err := liveRegistry.Create(projPath("/proj/a"), "会议")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, projPath("/proj/b"), map[string]string{
		"token":   room.Token(),
		"channel": "mic",
	}, []byte("audio")))

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeShareLiveNotes_RequiresLiveRoom(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/share/tok/notes", http.NoBody)
	ServeShareLiveNotes(w, req, "0123456789abcdef0123456789abcdef")

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeShareLiveNotes_ReturnsSegmentsForLiveRoom(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	_, err = service.AppendLiveNoteSegment(context.Background(), room.Token(), pid, service.LiveNoteSegment{
		Channel: "system", Speaker: "系统声音", StartedAt: 1000, DurationMS: 9000, Text: "你好",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/share/tok/notes?since=0", http.NoBody)
	ServeShareLiveNotes(w, req, room.Token())

	require.Equal(t, http.StatusOK, w.Code)
	var resp liveNotesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Segments, 1)
	assert.Equal(t, "你好", resp.Segments[0].Text)
	assert.Equal(t, service.LiveNotesStatusIdle, resp.Minutes.Status)
}

func TestServeLiveShareNotesMinutes_EmptyTranscriptStaysIdle(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	// The row exists (a segment was stored) but the transcript was cleaned out.
	_, err = service.AppendLiveNoteSegment(context.Background(), room.Token(), pid, service.LiveNoteSegment{
		Channel: "mic", StartedAt: 1, Text: "x",
	})
	require.NoError(t, err)
	_, err = store.WriteExec(`DELETE FROM live_notes_segments WHERE token = ?`, room.Token())
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"token": room.Token()})
	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", project, json.RawMessage(body)))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp liveNotesMinutesPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, service.LiveNotesStatusIdle, resp.Status)
}

func TestServeLiveShareNotesMinutes_ReadyIsIdempotent(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	_, err = service.AppendLiveNoteSegment(context.Background(), room.Token(), pid, service.LiveNoteSegment{
		Channel: "mic", StartedAt: 1, Text: "内容",
	})
	require.NoError(t, err)
	require.NoError(t, service.SetLiveNoteMinutes(room.Token(), pid, "", service.LiveNotesStatusReady, "## 概要", "", ""))

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", project, map[string]any{"token": room.Token()}))

	require.Equal(t, http.StatusOK, w.Code)
	var resp liveNotesMinutesPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, service.LiveNotesStatusReady, resp.Status)
	assert.Equal(t, "## 概要", resp.Markdown)
}

func TestServeLiveShareNotes_OtherProjectIs404(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	room, err := liveRegistry.Create(projPath("/proj/a"), "会议")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNotes(w, liveReq(t, http.MethodGet, "/api/live-share/notes?token="+room.Token(), projPath("/proj/b"), nil))

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── Read endpoint ──────────────────────────────────────────────────────────

func TestServeLiveShareNotes_ReturnsSegmentsAndMinutes(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	_, err = service.AppendLiveNoteSegment(context.Background(), room.Token(), pid, service.LiveNoteSegment{
		Channel: "mic", Speaker: "我", StartedAt: 1_000, Text: "第一句",
	})
	require.NoError(t, err)
	require.NoError(t, service.SetLiveNoteMinutes(room.Token(), pid, "周会", service.LiveNotesStatusReady, "## 概要", "", "/p/x.md"))

	w := httptest.NewRecorder()
	ServeLiveShareNotes(w, liveReq(t, http.MethodGet, "/api/live-share/notes?token="+room.Token()+"&since=0", project, nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp liveNotesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "周会", resp.Title)
	require.Len(t, resp.Segments, 1)
	assert.Equal(t, "第一句", resp.Segments[0].Text)
	assert.Equal(t, service.LiveNotesStatusReady, resp.Minutes.Status)
	assert.Equal(t, "## 概要", resp.Minutes.Markdown)
	assert.Equal(t, "/p/x.md", resp.Minutes.FilePath, "the publisher may see where the file went")
}

func TestServeLiveShareNotes_RejectsWrongMethod(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNotes(w, liveReq(t, http.MethodPost, "/api/live-share/notes", projPath("/proj/a"), nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeLiveShareNotes_RequiresProject(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNotes(w, newRequest(t, http.MethodGet, "/api/live-share/notes?token=abc", nil))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeLiveShareNotes_InvalidTokenIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNotes(w, liveReq(t, http.MethodGet, "/api/live-share/notes?token=bad%20token", projPath("/proj/a"), nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── Segment upload validation ──────────────────────────────────────────────

func TestServeLiveShareNoteSegment_RejectsWrongMethod(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, liveReq(t, http.MethodGet, "/api/live-share/notes/segment", projPath("/proj/a"), nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeLiveShareNoteSegment_InvalidTokenIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, projPath("/proj/a"), map[string]string{
		"token": "has spaces", "channel": "mic",
	}, []byte("audio")))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeLiveShareNoteSegment_RequiresAudioFile(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("token", room.Token()))
	require.NoError(t, mw.WriteField("channel", "mic"))
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/live-share/notes/segment", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: url.QueryEscape(project)})

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeLiveShareNoteSegment_RejectsEmptyAudio(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, project, map[string]string{
		"token": room.Token(), "channel": "mic",
	}, nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// A provider outage must not abort the publisher's upload queue: the segment is
// rejected with 502 and the share keeps working.
func TestServeLiveShareNoteSegment_TranscriptionFailureIsBadGateway(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	prev := GetSTTProvider()
	SetSTTProvider(&stubSTT{err: errors.New("stt down")})
	t.Cleanup(func() { SetSTTProvider(prev) })

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, project, map[string]string{
		"token": room.Token(), "channel": "mic",
	}, []byte("audio")))

	assert.Equal(t, http.StatusBadGateway, w.Code)
}

// ── Ownership by meeting record ───────────────────────────────────────────

// The meeting record exists from the moment the publisher attaches, so an
// upload is authorized by the row rather than by a live room.
func TestServeLiveShareNoteSegment_RecordOwnershipAuthorizesUpload(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "口述", time.Now().UnixMilli()))

	prev := GetSTTProvider()
	SetSTTProvider(&stubSTT{text: "你好"})
	t.Cleanup(func() { SetSTTProvider(prev) })

	rec := httptest.NewRecorder()
	ServeLiveShareNoteSegment(rec, notesUpload(t, project, map[string]string{
		"token":     token,
		"channel":   "mic",
		"title":     "口述",
		"startedAt": "1700000000000",
	}, []byte("audio")))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	segments, err := service.ListLiveNoteSegments(token, 0)
	require.NoError(t, err)
	require.Len(t, segments, 1)
	assert.Equal(t, "你好", segments[0].Text)
}

func TestServeLiveShareNoteSegment_UnknownRecordIs404(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	rec := httptest.NewRecorder()
	ServeLiveShareNoteSegment(rec, notesUpload(t, project, map[string]string{
		"token":   "ffffffffffffffffffffffffffffffff",
		"channel": "mic",
	}, []byte("audio")))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ── Minutes endpoint ───────────────────────────────────────────────────────

func withSummaryModel(t *testing.T, baseURL string) {
	t.Helper()
	prev := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = prev })
	model.ConfigInstance = model.Config{}
	model.ConfigInstance.AISummary.API.BaseURL = baseURL
	model.ConfigInstance.AISummary.Format = "openai"
}

func TestServeLiveShareNotesMinutes_RejectsWrongMethod(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodGet, "/api/live-share/notes/minutes", projPath("/proj/a"), nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeLiveShareNotesMinutes_RequiresProject(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, newRequest(t, http.MethodPost, "/api/live-share/notes/minutes", map[string]any{"token": "abc"}))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeLiveShareNotesMinutes_RejectsMalformedBody(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	req := liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", projPath("/proj/a"), nil)
	req.Body = io.NopCloser(strings.NewReader("{not json"))
	req.ContentLength = int64(len("{not json"))
	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeLiveShareNotesMinutes_InvalidTokenIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", projPath("/proj/a"),
		map[string]any{"token": "bad token"}))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// The room exists but no notes row does: there is nothing to summarize.
func TestServeLiveShareNotesMinutes_UnknownShareIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", project,
		map[string]any{"token": room.Token()}))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// End to end: the endpoint answers "generating" immediately, the detached job
// produces the minutes, writes the Markdown file and flips the status to ready.
func TestServeLiveShareNotesMinutes_GeneratesInBackground(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要\n\n确认发布计划。"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	project := t.TempDir()
	// The room is keyed by the NORMALIZED project path (the handler normalizes
	// the cookie value); on macOS t.TempDir() is a symlink under /var, so the
	// raw value would never match and every request would 404.
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	_, err = service.AppendLiveNoteSegment(context.Background(), room.Token(), pid, service.LiveNoteSegment{
		Channel: "mic", Speaker: "我", StartedAt: 1_700_000_000_000, Text: "确认发布计划",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", project,
		map[string]any{"token": room.Token(), "title": "周会"}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp liveNotesMinutesPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, service.LiveNotesStatusGenerating, resp.Status)

	require.Eventually(t, func() bool {
		note, ok, err := service.GetLiveNote(room.Token())
		return err == nil && ok && note.Status == service.LiveNotesStatusReady
	}, 10*time.Second, 20*time.Millisecond, "background job must finish")

	note, _, err := service.GetLiveNote(room.Token())
	require.NoError(t, err)
	assert.Contains(t, note.Minutes, "确认发布计划")
	assert.Equal(t, "周会", note.Title)
	require.NotEmpty(t, note.FilePath, "minutes must be written into the project")
	body, err := os.ReadFile(note.FilePath)
	require.NoError(t, err)
	assert.Contains(t, string(body), "## 概要")
}

// A failing provider must be recorded as "failed" with the reason, so the
// publisher sees why instead of an endless "generating".
func TestGenerateLiveNotesMinutes_BackgroundFailureMarksFailed(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	require.NoError(t, service.SetLiveNoteMinutes("tok-fail", pid, "", service.LiveNotesStatusGenerating, "", "", ""))

	generateLiveNotesArtifacts("tok-fail", pid, t.TempDir(), "", []service.LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_000, Text: "内容"},
	})

	note, ok, err := service.GetLiveNote("tok-fail")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, service.LiveNotesStatusFailed, note.Status)
	assert.NotEmpty(t, note.Error)
}

// The account nickname is read at generation time and handed to the summariser
// together with the transcript's own speaker label: the minutes must name the
// user instead of repeating the transcript's first-person "我".
func TestGenerateLiveNotesArtifacts_NamesTheUser(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()

	orig := model.UserNickname
	model.UserNickname = "泷泽"
	t.Cleanup(func() { model.UserNickname = orig })

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要\n\n确认。"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	pid, err := store.ProjectIDForPath("/proj/nick")
	require.NoError(t, err)
	require.NoError(t, service.SetLiveNoteMinutes("tok-nick", pid, "", service.LiveNotesStatusGenerating, "", "", ""))

	generateLiveNotesArtifacts("tok-nick", pid, t.TempDir(), "", []service.LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_000, Text: "我来负责发版"},
		{Channel: "viewer:v1", Speaker: "观众 1", StartedAt: 2_000, Text: "好的"},
	})

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, bodies)
	assert.Contains(t, bodies[len(bodies)-1], "泷泽",
		"the minutes pass must know who the transcript's 「我」 is")
	assert.Contains(t, bodies[len(bodies)-1], "「我」",
		"the label comes from the segment data, not a hard-coded string")
}

// ── Public viewer endpoint ─────────────────────────────────────────────────

func TestServeShareLiveNotes_RejectsWrongMethod(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeShareLiveNotes(w, httptest.NewRequest(http.MethodPost, "/api/share/tok/notes", http.NoBody), "abc")
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeShareLiveNotes_InvalidTokenIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeShareLiveNotes(w, httptest.NewRequest(http.MethodGet, "/api/share/tok/notes", http.NoBody), "bad token")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// A path is only reported when the file is actually there: the folder exists
// from the first audio segment, the transcripts and minutes much later.
func TestLiveNoteArtifactsOf_OnlyListsFilesThatExist(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "audio"), 0o755))

	empty := liveNoteArtifactsOf(dir)
	assert.Equal(t, dir, empty.Dir)
	assert.Equal(t, filepath.Join(dir, "audio"), empty.Audio)
	assert.Empty(t, empty.Minutes)
	assert.Empty(t, empty.Transcript)
	assert.Empty(t, empty.Revised)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "transcript.vtt"), []byte("WEBVTT\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "audio", "1-mic.webm"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "audio", ".gitignore"), []byte("*\n"), 0o644))

	got := liveNoteArtifactsOf(dir)
	assert.Equal(t, filepath.Join(dir, "transcript.vtt"), got.Transcript)
	assert.Empty(t, got.Minutes, "minutes.md is only listed once it exists")
	assert.Equal(t, 1, got.AudioFiles, "the .gitignore is not a recording")
}

// The recording is the one artifact that cannot be regenerated, so it is
// written BEFORE the STT call: a provider outage must cost the text, never the
// audio.
func TestServeLiveShareNoteSegment_PersistsAudioEvenWhenTranscriptionFails(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)

	prev := GetSTTProvider()
	SetSTTProvider(&stubSTT{err: errors.New("stt down")})
	t.Cleanup(func() { SetSTTProvider(prev) })

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, project, map[string]string{
		"token":     room.Token(),
		"channel":   "viewer:2",
		"title":     "季度会",
		"startedAt": "1700000000123",
	}, []byte("audio-bytes")))
	assert.Equal(t, http.StatusBadGateway, w.Code)

	notesDir := filepath.Join(project, ".clawbench", "notes")
	entries, err := os.ReadDir(notesDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "one meeting, one folder")

	audio := filepath.Join(notesDir, entries[0].Name(), "audio", "1700000000123-viewer-2.webm")
	body, err := os.ReadFile(audio)
	require.NoError(t, err)
	assert.Equal(t, "audio-bytes", string(body))
}

// Full post-meeting job: raw transcript, proofread transcript, minutes — all
// three on disk, and the minutes generated from the PROOFREAD text.
func TestServeLiveShareNotesMinutes_WritesThreeArtifactsAndSummarisesTheProofreadText(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// The proofreading pass is the one that carries the raw transcript; the
		// summarisation pass carries whatever it returned.
		if strings.Contains(string(body), "待校对转写") {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[00:00] 我：确认 ClawBench 发布计划"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要\n\n确认 ClawBench 发布计划。"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	project := t.TempDir()
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	_, err = service.AppendLiveNoteSegment(context.Background(), room.Token(), pid, service.LiveNoteSegment{
		Channel: "mic", Speaker: "我", StartedAt: 1_700_000_000_000, Text: "确认科劳本奇发布计划",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", project,
		map[string]any{"token": room.Token(), "title": "周会"}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Eventually(t, func() bool {
		note, ok, err := service.GetLiveNote(room.Token())
		return err == nil && ok && note.Status == service.LiveNotesStatusReady
	}, 10*time.Second, 20*time.Millisecond, "background job must finish")

	note, _, err := service.GetLiveNote(room.Token())
	require.NoError(t, err)
	assert.Equal(t, service.LiveNotesStatusReady, note.ReviseStatus)
	assert.Equal(t, "[00:00] 我：确认 ClawBench 发布计划", note.Revised)
	require.NotEmpty(t, note.DirName)

	raw, err := os.ReadFile(filepath.Join(service.MeetingDirPath(project, note.DirName), "transcript.vtt"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "确认科劳本奇发布计划", "the raw transcript keeps what the ASR heard")
	assert.True(t, strings.HasPrefix(string(raw), "WEBVTT\n"), "the raw transcript is WebVTT, not Markdown")
	assert.Contains(t, string(raw), "<v 我>", "the voice span carries the speaker")

	revised, err := os.ReadFile(filepath.Join(service.MeetingDirPath(project, note.DirName), "transcript-revised.md"))
	require.NoError(t, err)
	assert.Contains(t, string(revised), "ClawBench")

	minutes, err := os.ReadFile(filepath.Join(service.MeetingDirPath(project, note.DirName), "minutes.md"))
	require.NoError(t, err)
	assert.Contains(t, string(minutes), "## 概要")
	assert.Contains(t, string(minutes), "[transcript-revised.md](transcript-revised.md)")

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 2, "one proofread pass, one summarisation pass")
	assert.Contains(t, bodies[1], "ClawBench 发布计划", "the minutes see the corrected spelling")
	assert.NotContains(t, bodies[1], "科劳本奇", "and never the raw mis-recognition")
}

// A failed proofread must not cost the summary: the minutes are generated from
// the raw transcript instead, and the failure is recorded where the UI can show
// it.
func TestServeLiveShareNotesMinutes_ProofreadFailureFallsBackToRaw(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "待校对转写") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	project := t.TempDir()
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	_, err = service.AppendLiveNoteSegment(context.Background(), room.Token(), pid, service.LiveNoteSegment{
		Channel: "mic", Speaker: "我", StartedAt: 1_700_000_000_000, Text: "内容",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", project,
		map[string]any{"token": room.Token()}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Eventually(t, func() bool {
		note, ok, err := service.GetLiveNote(room.Token())
		return err == nil && ok && note.Status == service.LiveNotesStatusReady
	}, 10*time.Second, 20*time.Millisecond, "minutes must still be produced")

	note, _, err := service.GetLiveNote(room.Token())
	require.NoError(t, err)
	assert.Equal(t, service.LiveNotesStatusFailed, note.ReviseStatus)
	assert.NotEmpty(t, note.ReviseError)
	assert.Contains(t, note.Minutes, "## 概要")

	_, err = os.Stat(filepath.Join(service.MeetingDirPath(project, note.DirName), "transcript-revised.md"))
	assert.True(t, os.IsNotExist(err), "a failed proofread writes no revised file")
}

// ── Helpers ────────────────────────────────────────────────────────────────

func TestValidLiveNoteChannel(t *testing.T) {
	for _, ok := range []string{"system", "mic", "viewer:a1", "viewer:" + strings.Repeat("a", 32)} {
		assert.True(t, validLiveNoteChannel(ok), ok)
	}
	for _, bad := range []string{"", "System", "viewer:", "viewer:A", "viewer:a b", "viewer:" + strings.Repeat("a", 33), "speaker/1"} {
		assert.False(t, validLiveNoteChannel(bad), bad)
	}
}

func TestParseLiveNoteMillis(t *testing.T) {
	assert.Equal(t, int64(1_700_000_000_000), parseLiveNoteMillis(" 1700000000000 "))
	assert.Zero(t, parseLiveNoteMillis(""))
	assert.Zero(t, parseLiveNoteMillis("garbage"))
	assert.Zero(t, parseLiveNoteMillis("-5"))
}

func TestLiveNotesStartTime_UsesEarliestTimestamp(t *testing.T) {
	assert.Equal(t, int64(2_000), liveNotesStartTime([]service.LiveNoteSegment{
		{StartedAt: 5_000}, {StartedAt: 0}, {StartedAt: 2_000},
	}).UnixMilli())

	// No usable timestamp at all: fall back to "now" rather than 1970.
	now := time.Now()
	got := liveNotesStartTime([]service.LiveNoteSegment{{StartedAt: 0}, {StartedAt: -1}})
	assert.WithinDuration(t, now, got, time.Minute)
}

// The Markdown file is the durable copy, but it is written inside the project:
// an unavailable project directory must not lose the minutes themselves.
func TestGenerateLiveNotesMinutes_FileWriteFailureStillStoresMinutes(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	require.NoError(t, service.SetLiveNoteMinutes("tok-nofile", pid, "", service.LiveNotesStatusGenerating, "", "", ""))

	// A regular file as the "project path" makes MkdirAll fail.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	generateLiveNotesArtifacts("tok-nofile", pid, blocker, "", []service.LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_000, Text: "内容"},
	})

	note, ok, err := service.GetLiveNote("tok-nofile")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, service.LiveNotesStatusReady, note.Status)
	assert.Empty(t, note.FilePath, "no file means no path, not a failed generation")
	assert.Contains(t, note.Minutes, "## 概要")
}

func TestTranscribeLiveNoteSegment_NoProvider(t *testing.T) {
	prev := GetSTTProvider()
	SetSTTProvider(nil)
	t.Cleanup(func() { SetSTTProvider(prev) })

	_, err := transcribeLiveNoteSegment(context.Background(), []byte("audio"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stt provider not configured")
}

// A cancelled request must not wait for a free transcription slot.
func TestTranscribeLiveNoteSegment_CancelledContextDoesNotQueue(t *testing.T) {
	// Occupy every slot so the select can only proceed via ctx.Done.
	for range liveNoteSTTConcurrency {
		liveNoteSTTSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for range liveNoteSTTConcurrency {
			<-liveNoteSTTSlots
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := transcribeLiveNoteSegment(ctx, []byte("audio"))
	require.ErrorIs(t, err, context.Canceled)
}

func TestCapInt64(t *testing.T) {
	assert.Equal(t, int64(10), capInt64(50, 10))
	assert.Equal(t, int64(7), capInt64(7, 10))
	assert.Equal(t, int64(0), capInt64(0, 10))
}

// The public notes route lives under the share subtree because a live token has
// no file_shares row — the file-share lookup would 404 it before dispatch.
func TestServeSharePublic_RoutesNotesSubpath(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	_, err = service.AppendLiveNoteSegment(context.Background(), room.Token(), pid, service.LiveNoteSegment{
		Channel: "mic", Speaker: "我", StartedAt: 1_000, Text: "你好",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeSharePublic(w, httptest.NewRequest(http.MethodGet, "/api/share/"+room.Token()+"/notes", http.NoBody))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp liveNotesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Segments, 1)
	assert.Equal(t, "你好", resp.Segments[0].Text)
}

// A database outage must be a 500, not an empty transcript that looks like a
// quiet meeting.
func TestServeLiveShareNotes_DatabaseFailureIsServerError(t *testing.T) {
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	_, err = service.AppendLiveNoteSegment(context.Background(), room.Token(), pid, service.LiveNoteSegment{
		Channel: "mic", StartedAt: 1_000, Text: "内容",
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	w := httptest.NewRecorder()
	ServeLiveShareNotes(w, liveReq(t, http.MethodGet, "/api/live-share/notes?token="+room.Token(), project, nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestServeShareLiveNotes_DatabaseFailureIsServerError(t *testing.T) {
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	_, err = service.AppendLiveNoteSegment(context.Background(), room.Token(), pid, service.LiveNoteSegment{
		Channel: "mic", StartedAt: 1_000, Text: "内容",
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())

	w := httptest.NewRecorder()
	ServeShareLiveNotes(w, httptest.NewRequest(http.MethodGet, "/api/share/x/notes", http.NoBody), room.Token())
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// An oversized upload must say "too big" (413), not "this share is gone" (404):
// the publisher drops its whole upload queue on 404.
func TestServeLiveShareNoteSegment_OversizedUploadIs413(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	big := bytes.Repeat([]byte("x"), liveNoteSegmentMaxBytes+4096)
	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, project, map[string]string{
		"token": room.Token(), "channel": "mic",
	}, big))

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

// A body that is not valid multipart at all is a client bug (400), not an
// oversized upload (413) and certainly not "share not found" (404).
func TestServeLiveShareNoteSegment_MalformedMultipartIs400(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	req := httptest.NewRequest(http.MethodPost, "/api/live-share/notes/segment", strings.NewReader("not multipart at all"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=zzz")
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: url.QueryEscape(projPath("/proj/a"))})

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── History endpoints ───────────────────────────────────────────────────────

func liveNotesProjectRequest(t *testing.T, target, project string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, http.NoBody)
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: url.QueryEscape(project),
	})
	return req
}

func TestServeLiveNotesSessions_ListsTheProjectsRecords(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)

	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	paths, err := service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(paths.Minutes, []byte("MINUTES"), 0o644))

	rec := httptest.NewRecorder()
	ServeLiveNotesSessions(rec, liveNotesProjectRequest(t, "/api/live-share/notes/sessions", project))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Sessions []service.MeetingRecord `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Sessions, 1)
	assert.Equal(t, token, body.Sessions[0].Token)
	assert.Equal(t, "周会", body.Sessions[0].Title)
	assert.True(t, body.Sessions[0].HasMinutes)
}

// A meeting that recorded nothing is still a record: no folder, no artifacts,
// but it must be listed (and deletable) like any other.
func TestServeLiveNotesSessions_ListsMeetingsWithoutArtifacts(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	require.NoError(t, service.StartLiveNote("0123456789abcdef0123456789abcdef", pid, "空会", 1_700_000_000_000))

	rec := httptest.NewRecorder()
	ServeLiveNotesSessions(rec, liveNotesProjectRequest(t, "/api/live-share/notes/sessions", project))
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Sessions []service.MeetingRecord `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Sessions, 1)
	assert.Empty(t, body.Sessions[0].Dir)
	assert.False(t, body.Sessions[0].HasMinutes)
}

func TestServeLiveNotesArtifact_ServesByTokenAndRejectsUnknown(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	paths, err := service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(paths.Minutes, []byte("MINUTES"), 0o644))

	rec := httptest.NewRecorder()
	ServeLiveNotesArtifact(rec, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions/artifact?token="+token, project))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "minutes", body["kind"], "an omitted kind defaults to the minutes")
	assert.Equal(t, "MINUTES", body["content"])

	// No client-supplied path any more: an unknown token is the whole attack
	// surface, and it is answered as "not found".
	rec = httptest.NewRecorder()
	ServeLiveNotesArtifact(rec, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions/artifact?token=ffffffffffffffffffffffffffffffff", project))
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = httptest.NewRecorder()
	ServeLiveNotesArtifact(rec, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions/artifact?token="+token+"&kind=audio", project))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── Delete + chat ──────────────────────────────────────────────────────────

func TestServeLiveNotesDelete_RemovesRecordAndFolder(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	paths, err := service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(paths.Minutes, []byte("MINUTES"), 0o644))

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions?token="+token, project)
	req.Method = http.MethodDelete
	rec := httptest.NewRecorder()
	ServeLiveNotesSessions(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	_, err = os.Stat(paths.Dir)
	assert.True(t, os.IsNotExist(err), "the folder goes with the record")
	_, exists, err := service.GetLiveNote(token)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestServeLiveNotesDelete_RefusesWhilePublishingOrGenerating(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))

	// A live room wins over everything: the meeting is still happening.
	room, err := liveRegistry.Create(projPath(project), "周会")
	require.NoError(t, err)
	t.Cleanup(func() { liveRegistry.Delete(room.Token()) })
	_ = room
	require.NoError(t, service.StartLiveNote(room.Token(), pid, "周会", 1_700_000_000_000))

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions?token="+room.Token(), project)
	req.Method = http.MethodDelete
	rec := httptest.NewRecorder()
	ServeLiveNotesSessions(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)

	// A running minutes job would recreate the folder seconds later.
	require.NoError(t, service.SetLiveNoteMinutes(token, pid, "周会", service.LiveNotesStatusGenerating, "", "", ""))
	req = liveNotesProjectRequest(t, "/api/live-share/notes/sessions?token="+token, project)
	req.Method = http.MethodDelete
	// Pin the language so the assertion below can compare the translated text:
	// `Contains(body, "LiveNotesGenerating")` would also pass when the key is
	// missing from the locale files, because i18n.T echoes the id back.
	req.Header.Set("Accept-Language", "en")
	rec = httptest.NewRecorder()
	ServeLiveNotesSessions(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assertLocalizedError(t, rec.Body.Bytes(), "LiveNotesGenerating", "the minutes are still being generated")
}

// A crash during generation leaves status='generating' behind with no job to
// finish it. Refusing forever would make that meeting — folder, transcript,
// chat — undeletable, so the guard judges by the job's last write instead.
func TestServeLiveNotesDelete_AllowsDeletingAnInterruptedGeneration(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	require.NoError(t, service.SetLiveNoteMinutes(token, pid, "周会", service.LiveNotesStatusGenerating, "", "", ""))
	_, err = store.WriteExec(`UPDATE live_notes SET updated_at = ? WHERE token = ?`,
		time.Now().Add(-11*time.Minute).UTC().Format("2006-01-02 15:04:05"), token)
	require.NoError(t, err)

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions?token="+token, project)
	req.Method = http.MethodDelete
	rec := httptest.NewRecorder()
	ServeLiveNotesSessions(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	_, exists, err := service.GetLiveNote(token)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestServeLiveShareNoteChat_IsIdempotentAndReadable(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))

	post := func(id, text string) int64 {
		t.Helper()
		rec := httptest.NewRecorder()
		ServeLiveShareNoteChat(rec, liveReq(t, http.MethodPost, "/api/live-share/notes/chat", project,
			map[string]any{"token": token, "id": id, "speaker": "小明", "text": text}))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var resp struct {
			ID int64 `json:"id"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.NotZero(t, resp.ID)
		return resp.ID
	}

	first := post("m1", "收到")
	// Another message in between: without it a stale last_insert_rowid() would
	// happen to equal the right answer and the retry check below could not tell
	// the two implementations apart.
	second := post("m2", "好的")
	require.NotEqual(t, first, second)
	again := post("m1", "收到")
	assert.Equal(t, first, again, "a retry must return the id of ITS row")

	rec := httptest.NewRecorder()
	ServeLiveNotesChat(rec, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions/chat?token="+token, project))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Messages []service.LiveNoteChatMessage `json:"messages"`
		Count    int                           `json:"count"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Messages, 2, "a retried message must not duplicate")
	assert.Equal(t, 2, body.Count)
	assert.Equal(t, first, body.Messages[0].ID)
	assert.Equal(t, "小明", body.Messages[0].Speaker)
	assert.Equal(t, "收到", body.Messages[0].Text)
	assert.Equal(t, second, body.Messages[1].ID)
}

// ── Failure paths: method / project / token / database ──────────────────────
//
// Each guard answers a different status code, and the publisher's uploader
// branches on them: 404 means "this share is gone, drop the queue", 400 means
// "retry with a different payload", 500 means "the server is broken, keep the
// queue and retry". Collapsing them into one "some error" assertion would let a
// refactor swap 404 for 400 and strand or drop a whole recording unnoticed.

// assertErrorKey pins the status AND the message key. An empty key means the
// response came from model.WriteError (the un-localized 500), which is a
// different contract from writeLocalizedErrorf's "InternalError".
func assertErrorKey(t *testing.T, w *httptest.ResponseRecorder, status int, key string) {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	var resp model.ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	assert.Equal(t, key, resp.MsgKey, w.Body.String())
	if key == "" {
		assert.Equal(t, "internal server error", resp.Error,
			"a bare 500 must come from model.WriteError, not a localized key")
	}
}

// cacheProjectID registers the project row while the database is still up, so a
// later failure isolates the call under test: without it, closing the DB fails
// in liveNotesProject (which every endpoint shares) and the branch below it can
// never be reached.
func cacheProjectID(t *testing.T, project string) int64 {
	t.Helper()
	id, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	return id
}

// dropTable removes one table so only the query under test fails. Closing the
// whole database would trip the earlier, shared lookups instead.
func dropTable(t *testing.T, name string) {
	t.Helper()
	_, err := store.WriteExec(`DROP TABLE ` + name)
	require.NoError(t, err)
}

func TestServeLiveNotesSessions_MethodNotAllowed(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveNotesSessions(w, liveReq(t, http.MethodPut, "/api/live-share/notes/sessions", projPath("/proj/a"), nil))
	assertErrorKey(t, w, http.StatusMethodNotAllowed, "MethodNotAllowed")
}

func TestServeLiveNotesSessions_RequiresProject(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveNotesSessions(w, newRequest(t, http.MethodGet, "/api/live-share/notes/sessions", nil))
	assertErrorKey(t, w, http.StatusForbidden, "NoProjectSelected")
}

func TestServeLiveNotesList_InvalidTokenIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveNotesSessions(w, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions?token=not%20a%20token", projPath("/proj/a")))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeLiveNotesList_UnknownTokenIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveNotesSessions(w, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions?token=ffffffffffffffffffffffffffffffff", projPath("/proj/a")))
	assert.Equal(t, http.StatusNotFound, w.Code,
		"a well-formed token with no row is 'not found', not an empty list")
}

// A single record addressed by token is a different payload shape from the
// list: the SPA's detail dialog reads `session`, the list reads `sessions`.
func TestServeLiveNotesList_ServesOneRecordByToken(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	paths, err := service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(paths.Minutes, []byte("MINUTES"), 0o644))

	w := httptest.NewRecorder()
	ServeLiveNotesSessions(w, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions?token="+token, project))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Session  service.MeetingRecord   `json:"session"`
		Sessions []service.MeetingRecord `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, token, body.Session.Token)
	assert.Equal(t, "周会", body.Session.Title)
	assert.True(t, body.Session.HasMinutes)
	assert.Empty(t, body.Sessions, "the token form must not also return the list")
}

func TestServeLiveNotesList_FailureIs500(t *testing.T) {
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	require.NoError(t, db.Close())

	w := httptest.NewRecorder()
	ServeLiveNotesSessions(w, liveNotesProjectRequest(t, "/api/live-share/notes/sessions", project))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeLiveNotesList_RecordFailureIs500(t *testing.T) {
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	require.NoError(t, db.Close())

	w := httptest.NewRecorder()
	ServeLiveNotesSessions(w, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions?token=0123456789abcdef0123456789abcdef", project))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeLiveNotesArtifact_MethodNotAllowed(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveNotesArtifact(w, liveReq(t, http.MethodPost, "/api/live-share/notes/sessions/artifact", projPath("/proj/a"), nil))
	assertErrorKey(t, w, http.StatusMethodNotAllowed, "MethodNotAllowed")
}

func TestServeLiveNotesArtifact_RequiresProject(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveNotesArtifact(w, newRequest(t, http.MethodGet, "/api/live-share/notes/sessions/artifact?token=x", nil))
	assertErrorKey(t, w, http.StatusForbidden, "NoProjectSelected")
}

// A meeting that recorded nothing has no folder, so there is no artifact to
// read — but the token is valid and owned, so this is a 404 rather than a
// permission error.
func TestServeLiveNotesArtifact_NoFolderIs404(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "空会", 1_700_000_000_000))

	w := httptest.NewRecorder()
	ServeLiveNotesArtifact(w, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions/artifact?token="+token, project))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeLiveNotesArtifact_MissingFileIs404(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	// The folder exists (audio was recorded) but the minutes were never written.
	_, err := service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveNotesArtifact(w, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions/artifact?token="+token+"&kind=minutes", project))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeLiveNotesArtifact_DirLookupFailureIs500(t *testing.T) {
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	// The room authorizes the request without touching the database, so the
	// only failing lookup is MeetingDirName.
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	w := httptest.NewRecorder()
	ServeLiveNotesArtifact(w, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions/artifact?token="+room.Token(), project))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

// Stat succeeds but the read does not (permissions): that is an outage, not a
// missing artifact, so it must not masquerade as 404.
//
// POSIX-only: Windows has no file read bit, so a 0o000 file there is still
// readable and the artifact is served normally.
func TestServeLiveNotesArtifact_ReadFailureIs500(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no file read bit; a read cannot be made to fail this way")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not apply to root")
	}
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	paths, err := service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(paths.Minutes, []byte("MINUTES"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(paths.Minutes, 0o644) })

	w := httptest.NewRecorder()
	ServeLiveNotesArtifact(w, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions/artifact?token="+token, project))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeLiveShareNotes_FilesLookupFailureIs500(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	dropTable(t, "live_notes_files")

	w := httptest.NewRecorder()
	ServeLiveShareNotes(w, liveReq(t, http.MethodGet, "/api/live-share/notes?token="+room.Token(), project, nil))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeLiveShareNotes_ScreensLookupFailureIs500(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	dropTable(t, "live_notes_screen")

	w := httptest.NewRecorder()
	ServeLiveShareNotes(w, liveReq(t, http.MethodGet, "/api/live-share/notes?token="+room.Token(), project, nil))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeLiveShareNotes_NoteLookupFailureIs500(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	dropTable(t, "live_notes")

	w := httptest.NewRecorder()
	ServeLiveShareNotes(w, liveReq(t, http.MethodGet, "/api/live-share/notes?token="+room.Token(), project, nil))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeLiveNotesDelete_InvalidTokenIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	req := liveReq(t, http.MethodDelete, "/api/live-share/notes/sessions?token=bad%20token", projPath("/proj/a"), nil)
	w := httptest.NewRecorder()
	ServeLiveNotesSessions(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// A corrupted row whose folder name points outside the notes root must be
// refused rather than deleting a sibling directory.
func TestServeLiveNotesDelete_RefusesEscapingFolderIs500(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	_, err := store.WriteExec(`UPDATE live_notes SET dir_name = ? WHERE token = ?`, "../escape", token)
	require.NoError(t, err)

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions?token="+token, project)
	req.Method = http.MethodDelete
	w := httptest.NewRecorder()
	ServeLiveNotesSessions(w, req)
	assertErrorKey(t, w, http.StatusInternalServerError, "")

	_, exists, err := service.GetLiveNote(token)
	require.NoError(t, err)
	assert.True(t, exists, "a refused delete must not remove the record")
}

func TestServeLiveNotesChat_MethodNotAllowed(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveNotesChat(w, liveReq(t, http.MethodPost, "/api/live-share/notes/sessions/chat", projPath("/proj/a"), nil))
	assertErrorKey(t, w, http.StatusMethodNotAllowed, "MethodNotAllowed")
}

func TestServeLiveNotesChat_RequiresProject(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveNotesChat(w, newRequest(t, http.MethodGet, "/api/live-share/notes/sessions/chat?token=x", nil))
	assertErrorKey(t, w, http.StatusForbidden, "NoProjectSelected")
}

func TestServeLiveNotesChat_InvalidTokenIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveNotesChat(w, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions/chat?token=bad%20token", projPath("/proj/a")))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeLiveNotesChat_ListFailureIs500(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	dropTable(t, "live_notes_chat")

	w := httptest.NewRecorder()
	ServeLiveNotesChat(w, liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions/chat?token="+room.Token(), project))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeLiveShareNoteChat_MethodNotAllowed(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNoteChat(w, liveReq(t, http.MethodGet, "/api/live-share/notes/chat", projPath("/proj/a"), nil))
	assertErrorKey(t, w, http.StatusMethodNotAllowed, "MethodNotAllowed")
}

func TestServeLiveShareNoteChat_RequiresProject(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNoteChat(w, newRequest(t, http.MethodPost, "/api/live-share/notes/chat", map[string]any{"token": "x"}))
	assertErrorKey(t, w, http.StatusForbidden, "NoProjectSelected")
}

func TestServeLiveShareNoteChat_MalformedBodyIs400(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	req := liveReq(t, http.MethodPost, "/api/live-share/notes/chat", projPath("/proj/a"), nil)
	req.Body = io.NopCloser(strings.NewReader("{not json"))
	req.ContentLength = int64(len("{not json"))
	w := httptest.NewRecorder()
	ServeLiveShareNoteChat(w, req)
	assertErrorKey(t, w, http.StatusBadRequest, "InvalidRequestBody")
}

func TestServeLiveShareNoteChat_ForeignTokenIs404(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	room, err := liveRegistry.Create(projPath("/proj/a"), "会议")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNoteChat(w, liveReq(t, http.MethodPost, "/api/live-share/notes/chat", projPath("/proj/b"),
		map[string]any{"token": room.Token(), "id": "m1", "text": "hi"}))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeLiveShareNoteChat_EmptyTextIs400(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNoteChat(w, liveReq(t, http.MethodPost, "/api/live-share/notes/chat", project,
		map[string]any{"token": room.Token(), "id": "m1", "text": "   "}))
	assertErrorKey(t, w, http.StatusBadRequest, "BadRequest")
}

func TestServeLiveShareNoteChat_AppendFailureIs500(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	dropTable(t, "live_notes_chat")

	w := httptest.NewRecorder()
	ServeLiveShareNoteChat(w, liveReq(t, http.MethodPost, "/api/live-share/notes/chat", project,
		map[string]any{"token": room.Token(), "id": "m1", "text": "hi"}))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeLiveShareNoteFiles_MalformedBodyIs400(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	req := liveReq(t, http.MethodPost, "/api/live-share/notes/files", projPath("/proj/a"), nil)
	req.Body = io.NopCloser(strings.NewReader("not json"))
	req.ContentLength = int64(len("not json"))
	w := httptest.NewRecorder()
	ServeLiveShareNoteFiles(w, req)
	assertErrorKey(t, w, http.StatusBadRequest, "InvalidRequestBody")
}

func TestServeLiveShareNoteFiles_RecordFailureIs500(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	dropTable(t, "live_notes_files")

	w := httptest.NewRecorder()
	ServeLiveShareNoteFiles(w, liveReq(t, http.MethodPost, "/api/live-share/notes/files", project,
		map[string]any{"token": room.Token(), "files": []map[string]any{
			{"path": "main.go", "openedAt": 1000},
		}}))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

// A client that reports a close BEFORE the open (clock skew between two
// machines, a retry that reordered) must not produce a negative interval: the
// timeline would render "10:11–10:03".
func TestServeLiveShareNoteFiles_ClampsClosedAtToOpenedAt(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNoteFiles(w, liveReq(t, http.MethodPost, "/api/live-share/notes/files", project,
		map[string]any{"token": room.Token(), "files": []map[string]any{
			{"path": "main.go", "openedAt": 5000, "closedAt": 1000},
		}}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	files, err := service.ListLiveNoteFiles(room.Token(), 0)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, int64(5000), files[0].OpenedAt)
	assert.Equal(t, int64(5000), files[0].ClosedAt, "a close before the open collapses onto the open")
	_ = pid
}

func TestServeLiveShareNoteScreen_MalformedBodyIs400(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	req := liveReq(t, http.MethodPost, "/api/live-share/notes/screen", projPath("/proj/a"), nil)
	req.Body = io.NopCloser(strings.NewReader("{"))
	req.ContentLength = 1
	w := httptest.NewRecorder()
	ServeLiveShareNoteScreen(w, req)
	assertErrorKey(t, w, http.StatusBadRequest, "InvalidRequestBody")
}

func TestServeLiveShareNoteScreen_RecordFailureIs500(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	dropTable(t, "live_notes_screen")

	w := httptest.NewRecorder()
	ServeLiveShareNoteScreen(w, liveReq(t, http.MethodPost, "/api/live-share/notes/screen", project,
		map[string]any{"token": room.Token(), "sharer": "host", "at": 1000}))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeLiveNotesAudioMerge_MethodNotAllowed(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveNotesAudioMerge(w, liveReq(t, http.MethodGet, "/api/live-share/notes/audio/merge", projPath("/proj/a"), nil))
	assertErrorKey(t, w, http.StatusMethodNotAllowed, "MethodNotAllowed")
}

func TestServeLiveNotesAudioMerge_RequiresProject(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveNotesAudioMerge(w, newRequest(t, http.MethodPost, "/api/live-share/notes/audio/merge", map[string]any{"token": "x"}))
	assertErrorKey(t, w, http.StatusForbidden, "NoProjectSelected")
}

func TestServeLiveNotesAudioMerge_MalformedBodyIs400(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	req := liveReq(t, http.MethodPost, "/api/live-share/notes/audio/merge", projPath("/proj/a"), nil)
	req.Body = io.NopCloser(strings.NewReader("{"))
	req.ContentLength = 1
	w := httptest.NewRecorder()
	ServeLiveNotesAudioMerge(w, req)
	assertErrorKey(t, w, http.StatusBadRequest, "InvalidRequestBody")
}

func TestServeLiveNotesAudioMerge_DirLookupFailureIs500(t *testing.T) {
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	w := httptest.NewRecorder()
	ServeLiveNotesAudioMerge(w, liveReq(t, http.MethodPost, "/api/live-share/notes/audio/merge", project,
		map[string]string{"token": room.Token()}))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

// A meeting whose notes were off has no folder: merging nothing is success, not
// an error, and the payload still carries the (empty) artifact block so the
// caller can render the dialog without a second request.
func TestServeLiveNotesAudioMerge_NothingRecordedIsOK(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "空会", 1_700_000_000_000))

	w := httptest.NewRecorder()
	ServeLiveNotesAudioMerge(w, liveReq(t, http.MethodPost, "/api/live-share/notes/audio/merge", project,
		map[string]string{"token": token}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		OK        bool               `json:"ok"`
		Artifacts liveNotesArtifacts `json:"artifacts"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.OK)
	assert.Empty(t, resp.Artifacts.Dir)
}

// A segment file that is not the WebM the merge expects must fail loudly: the
// alternative is publishing a corrupt recording as the meeting's audio.
func TestServeLiveNotesAudioMerge_CorruptSegmentIs500(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	paths, err := service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(paths.Audio, "1700000000000-mic.webm"), []byte("not webm"), 0o644))

	w := httptest.NewRecorder()
	ServeLiveNotesAudioMerge(w, liveReq(t, http.MethodPost, "/api/live-share/notes/audio/merge", project,
		map[string]string{"token": token}))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeLiveShareNotesMinutes_NoteLookupFailureIs500(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	dropTable(t, "live_notes")

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", project,
		map[string]any{"token": room.Token()}))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeLiveShareNotesMinutes_SegmentLookupFailureIs500(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	dropTable(t, "live_notes_segments")

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", project,
		map[string]any{"token": token}))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeShareLiveNotes_FilesLookupFailureIs500(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	dropTable(t, "live_notes_files")

	w := httptest.NewRecorder()
	ServeShareLiveNotes(w, httptest.NewRequest(http.MethodGet, "/api/share/x/notes", http.NoBody), room.Token())
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeShareLiveNotes_ScreensLookupFailureIs500(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	dropTable(t, "live_notes_screen")

	w := httptest.NewRecorder()
	ServeShareLiveNotes(w, httptest.NewRequest(http.MethodGet, "/api/share/x/notes", http.NoBody), room.Token())
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeShareLiveNotes_NoteLookupFailureIs500(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	cacheProjectID(t, project)
	room, err := liveRegistry.Create(projPath(project), "会议")
	require.NoError(t, err)
	dropTable(t, "live_notes")

	w := httptest.NewRecorder()
	ServeShareLiveNotes(w, httptest.NewRequest(http.MethodGet, "/api/share/x/notes", http.NoBody), room.Token())
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

// The notes row is the second ownership source. When the lookup itself fails we
// cannot prove ownership, so the answer is "not yours" (404) — never an
// accidental grant.
func TestLiveNotesOwnedByProject_LookupFailureIsNotOwned(t *testing.T) {
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	require.NoError(t, db.Close())

	assert.False(t, liveNotesOwnedByProject("0123456789abcdef0123456789abcdef", project, pid))
}

// The recording is written before transcription, and a failed write is only a
// warning: losing the transcript because the disk filled up would be the worse
// trade. The handler must still answer (the STT stub is never reached here).
func TestPersistLiveNoteAudio_WriteFailureIsSwallowed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not apply to root")
	}
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	paths, err := service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)
	// MkdirAll on an existing directory succeeds, so EnsureMeetingDir still
	// passes; the write inside it is what fails.
	require.NoError(t, os.Chmod(paths.Audio, 0o500))
	t.Cleanup(func() { _ = os.Chmod(paths.Audio, 0o755) })

	persistLiveNoteAudio(token, pid, project, "周会", "mic", 1_700_000_000_000, []byte("audio"))
}

func TestLiveNotesTimeline_ScreenSectionIsAppended(t *testing.T) {
	started := time.UnixMilli(1_700_000_000_000)
	screens := []service.LiveNoteScreenShare{{Sharer: "host", SharerName: "主持人", StartedAt: 1_700_000_100_000}}

	// No screens: the file timeline alone, with no trailing separator.
	files := []service.LiveNoteFile{{Path: "/proj/a/main.go", OpenedAt: 1_700_000_050_000}}
	onlyFiles := liveNotesTimeline(files, nil, started, "/proj/a")
	require.NotEmpty(t, onlyFiles)
	assert.NotContains(t, onlyFiles, "屏幕共享")

	// Both: one block for each, a blank line apart.
	both := liveNotesTimeline(files, screens, started, "/proj/a")
	assert.Contains(t, both, "屏幕共享")
	assert.Contains(t, both, "\n\n", "the two timelines are separate blocks")
	assert.Greater(t, len(both), len(onlyFiles))

	// Screens only (no files opened): still a usable block.
	onlyScreens := liveNotesTimeline(nil, screens, started, "/proj/a")
	assert.Contains(t, onlyScreens, "屏幕共享")
}

// The minutes file is the human-readable index of the folder: it must count the
// channel recordings and link the merged track, otherwise the reader has no way
// to find the audio from the summary.
func TestWriteLiveNotesMinutes_CountsChannelRecordings(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "audio"), 0o755))
	for _, name := range []string{"1-mic.webm", "1-viewer-2.webm", service.LiveNoteMeetingAudioFile} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "audio", name), []byte("a"), 0o644))
	}
	// Not a recording: the .gitignore keeps the binary audio out of the repo.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "audio", ".gitignore"), []byte("*\n"), 0o644))

	paths := service.LiveNotesPathsIn(dir)
	written := writeLiveNotesMinutes(paths, "周会", time.UnixMilli(1_700_000_000_000), "## 概要")
	require.Equal(t, paths.Minutes, written)

	body, err := os.ReadFile(written)
	require.NoError(t, err)
	assert.Contains(t, string(body), "2", "two channel recordings, not three and not one")
	assert.Contains(t, string(body), service.LiveNoteMeetingAudioFile, "the merged track is linked")
}

// breakNotesFolder makes the meeting folder unusable on every platform.
//
// POSIX revokes the directory's write bit. Windows has no such bit — os.Chmod
// there only sets FILE_ATTRIBUTE_READONLY, which the filesystem ignores when
// deciding whether a file may be created inside the directory — so the folder
// is replaced by a regular file instead: every create/write below it then
// fails, which is the condition these tests are about.
func breakNotesFolder(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		return
	}
	require.NoError(t, os.RemoveAll(dir))
	require.NoError(t, os.WriteFile(dir, []byte("not a folder"), 0o644))
}

// A folder that cannot be written must not fail the job: the caller stores the
// minutes in the database either way and simply reports no path.
func TestWriteLiveNotesMinutes_UnwritableFolderReturnsEmptyPath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not apply to root")
	}
	dir := t.TempDir()
	paths := service.LiveNotesPathsIn(dir)
	breakNotesFolder(t, dir)

	assert.Empty(t, writeLiveNotesMinutes(paths, "周会", time.UnixMilli(1_700_000_000_000), "## 概要"))
}

// breakWrites points the WRITE pool at a closed handle while leaving reads
// working, so a handler can be driven all the way to its persist step. Closing
// the whole database would fail the earlier read lookups instead and the write
// branch would never be reached.
func breakWrites(t *testing.T, project string) {
	t.Helper()
	broken, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	require.NoError(t, broken.Close())
	t.Cleanup(store.SetDBForTest(broken, store.UnsafeReadDBForTest()))
	// SetDBForTest clears the id cache, and ProjectIDForPath upserts before it
	// reads — so the project would 500 on the way in. Warm the cache through
	// the READ path instead, which is the only lookup left working.
	_, ok, err := store.ProjectIDByPath(project)
	require.NoError(t, err)
	require.True(t, ok, "the project row must exist before writes are broken")
}

// A read-only project registry is an outage: the handler must say 500 rather
// than fall through with a zero project id, which would file the meeting under
// "no project" and hide it from the owner.
func TestLiveNotesProject_ProjectLookupFailureIs500(t *testing.T) {
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)
	require.NoError(t, db.Close())

	w := httptest.NewRecorder()
	ServeLiveNotesSessions(w, liveNotesProjectRequest(t, "/api/live-share/notes/sessions", t.TempDir()))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

func TestServeLiveShareNotesMinutes_MarkGeneratingFailureIs500(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	_, err := service.AppendLiveNoteSegment(context.Background(), token, pid, service.LiveNoteSegment{
		Channel: "mic", StartedAt: 1_000, Text: "内容",
	})
	require.NoError(t, err)
	breakWrites(t, project)

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", project,
		map[string]any{"token": token}))
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

// A failed segment write is a server error, but only AFTER the audio is safely
// on disk: the recording cannot be regenerated, the row can.
func TestServeLiveShareNoteSegment_AppendFailureIs500(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	// The folder is created first: EnsureMeetingDir reuses the recorded name
	// without writing, so only the transcription row's write can fail below.
	_, err := service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)

	prev := GetSTTProvider()
	SetSTTProvider(&stubSTT{text: "你好"})
	t.Cleanup(func() { SetSTTProvider(prev) })
	breakWrites(t, project)

	w := httptest.NewRecorder()
	ServeLiveShareNoteSegment(w, notesUpload(t, project, map[string]string{
		"token": token, "channel": "mic", "startedAt": "1700000000000",
	}, []byte("audio-bytes")))
	assertErrorKey(t, w, http.StatusInternalServerError, "")

	// The audio still landed: only the transcription row failed.
	notesDir := filepath.Join(project, ".clawbench", "notes")
	entries, err := os.ReadDir(notesDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.FileExists(t, filepath.Join(notesDir, entries[0].Name(), "audio", "1700000000000-mic.webm"))
}

// The durable copies are a convenience, not the source of truth: when the
// meeting folder cannot be written the job must still produce and store the
// minutes, and report no path rather than a failed meeting.
func TestGenerateLiveNotesArtifacts_UnwritableFolderStillStoresMinutes(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not apply to root")
	}
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	paths, err := service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)
	breakNotesFolder(t, paths.Dir)

	generateLiveNotesArtifacts(token, pid, project, "周会", []service.LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_000, Text: "内容"},
	})

	note, ok, err := service.GetLiveNote(token)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, service.LiveNotesStatusReady, note.Status)
	assert.Contains(t, note.Minutes, "## 概要")
	assert.Empty(t, note.FilePath, "no file means no path, not a failed generation")
	_, statErr := os.Stat(paths.Transcript)
	assert.True(t, os.IsNotExist(statErr), "the transcript write failed and was only a warning")
}

// Every durable write failing is still a completed meeting: the status has to
// move off "generating", or the reader waits forever and the delete guard
// refuses to remove the row.
func TestGenerateLiveNotesArtifacts_SurvivesNotesWriteFailure(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	breakWrites(t, project)

	// The job must return, not panic or block, on a database that cannot write.
	generateLiveNotesArtifacts(token, pid, project, "周会", []service.LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_000, Text: "内容"},
	})
}

// A failing summariser plus a failing write: the failure still has to be
// recorded (or at least attempted) rather than silently swallowed.
func TestGenerateLiveNotesArtifacts_FailedMinutesWithBrokenWrite(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	breakWrites(t, project)

	generateLiveNotesArtifacts(token, pid, project, "周会", []service.LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_000, Text: "内容"},
	})
}

func TestServeLiveNotesDelete_ProjectLookupFailureIs500(t *testing.T) {
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)
	require.NoError(t, db.Close())

	req := liveNotesProjectRequest(t,
		"/api/live-share/notes/sessions?token=0123456789abcdef0123456789abcdef", t.TempDir())
	req.Method = http.MethodDelete
	w := httptest.NewRecorder()
	ServeLiveNotesSessions(w, req)
	assertErrorKey(t, w, http.StatusInternalServerError, "")
}

// The segments on disk are folded into one recording per channel before the
// artifacts are written, so the meeting is opened as one file rather than forty.
func TestGenerateLiveNotesArtifacts_MergesRecordedSegments(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	paths, err := service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)
	// A real MediaRecorder segment starts with the EBML magic; the merge
	// validates it even on the single-segment (rename) path.
	segment := filepath.Join(paths.Audio, "1700000000000-mic.webm")
	require.NoError(t, os.WriteFile(segment, append([]byte{0x1A, 0x45, 0xDF, 0xA3}, []byte("audio")...), 0o644))

	generateLiveNotesArtifacts(token, pid, project, "周会", []service.LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_700_000_000_000, Text: "内容"},
	})

	assert.FileExists(t, filepath.Join(paths.Audio, "mic.webm"))
	assert.NoFileExists(t, segment, "the segment is folded into the channel file")
}

// A corrupt segment must not abort the meeting: the minutes still get produced,
// the recording is simply left as it was.
func TestGenerateLiveNotesArtifacts_SurvivesCorruptAudio(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	paths, err := service.EnsureMeetingDir(token, pid, project, "周会", 1_700_000_000_000)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(paths.Audio, "1700000000000-mic.webm"), []byte("not webm"), 0o644))

	generateLiveNotesArtifacts(token, pid, project, "周会", []service.LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_700_000_000_000, Text: "内容"},
	})

	note, ok, err := service.GetLiveNote(token)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, service.LiveNotesStatusReady, note.Status)
}

// The file and screen timelines are context, not content: losing either is a
// gap in the minutes, never a reason to fail the meeting.
func TestGenerateLiveNotesArtifacts_SurvivesTimelineLookupFailures(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	project := t.TempDir()
	pid := cacheProjectID(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	dropTable(t, "live_notes_files")
	dropTable(t, "live_notes_screen")

	generateLiveNotesArtifacts(token, pid, project, "周会", []service.LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_700_000_000_000, Text: "内容"},
	})

	note, ok, err := service.GetLiveNote(token)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, service.LiveNotesStatusReady, note.Status)
	assert.Contains(t, note.Minutes, "## 概要")
}
