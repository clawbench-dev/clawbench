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
	"strings"
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
	room, err := liveRegistry.Create(project, "会议")
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

	generateLiveNotesMinutes("tok-fail", pid, t.TempDir(), "", []service.LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_000, Text: "内容"},
	})

	note, ok, err := service.GetLiveNote("tok-fail")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, service.LiveNotesStatusFailed, note.Status)
	assert.NotEmpty(t, note.Error)
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

	generateLiveNotesMinutes("tok-nofile", pid, blocker, "", []service.LiveNoteSegment{
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
