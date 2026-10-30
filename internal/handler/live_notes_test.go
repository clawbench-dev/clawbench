package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

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
	require.NoError(t, service.SetLiveNoteMinutes(room.Token(), pid, "", service.LiveNotesStatusReady, "## 概要", ""))

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
