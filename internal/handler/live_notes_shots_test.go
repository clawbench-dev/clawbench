package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
)

// A minimal 1x1 PNG: enough for the magic-byte check, which is the only thing
// the upload path looks at before writing the bytes.
var tinyPNG = []byte("\x89PNG\r\n\x1a\n" + "not-really-a-png")

func shotProjectCookie(project string) *http.Cookie {
	return &http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: url.QueryEscape(project)}
}

func mustProjectID(t *testing.T, project string) int64 {
	t.Helper()
	id, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	return id
}

func shotUpload(t *testing.T, project string, fields map[string]string, body []byte) *http.Request {
	t.Helper()
	req := notesUpload(t, project, fields, body)
	req.URL.Path = "/api/live-share/notes/shot"
	return req
}

func createShotMeeting(t *testing.T, project string) string {
	t.Helper()
	room, err := liveRegistry.Create(project, "发版评审")
	require.NoError(t, err)
	projectID, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	require.NoError(t, service.StartLiveNote(room.Token(), projectID, "发版评审", 1_700_000_000_000))
	return room.Token()
}

func TestServeLiveShareNoteShot_RequiresProject(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareNoteShot(w, shotUpload(t, "", map[string]string{
		"token": "abc", "clientShotId": "c1", "reason": "manual",
	}, tinyPNG))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// The file is sniffed, not trusted: a text body labeled as an image would only
// break the thumbnailer later, where the cause is impossible to trace.
func TestServeLiveShareNoteShot_RejectsNonImage(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	token := createShotMeeting(t, project)

	w := httptest.NewRecorder()
	ServeLiveShareNoteShot(w, shotUpload(t, project, map[string]string{
		"token": token, "clientShotId": "c1", "reason": "manual",
	}, []byte("hello, not an image")))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	shots, err := service.ListLiveNoteShots(token)
	require.NoError(t, err)
	assert.Empty(t, shots)
}

func TestServeLiveShareNoteShot_StoresRowAndFile(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	started := int64(1_700_000_000_000)
	token := createShotMeeting(t, project)

	w := httptest.NewRecorder()
	ServeLiveShareNoteShot(w, shotUpload(t, project, map[string]string{
		"token":        token,
		"clientShotId": "c1",
		"at":           strconv.FormatInt(started+5000, 10),
		"reason":       "file",
		"ref":          "docs/a.md",
		"width":        "1280",
		"height":       "720",
	}, tinyPNG))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp service.LiveNoteShot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Greater(t, resp.ID, int64(0))
	assert.Equal(t, started+5000, resp.At)
	assert.Equal(t, "file", resp.Reason)
	assert.Contains(t, resp.FileName, "shots/")
	assert.Equal(t, int64(len(tinyPNG)), resp.Bytes)

	dirName, err := service.MeetingDirName(token)
	require.NoError(t, err)
	require.NotEmpty(t, dirName)
	body, err := os.ReadFile(filepath.Join(service.MeetingDirPath(project, dirName), resp.FileName))
	require.NoError(t, err)
	assert.Equal(t, tinyPNG, body)

	// The record list reports the frame without a second query.
	record, ok, err := service.GetMeetingRecord(project, mustProjectID(t, project), token)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 1, record.ShotCount)
}

// A client clock that is ahead (or a frame from before the meeting started) is
// clamped rather than rejected: the picture itself is real.
func TestServeLiveShareNoteShot_ClampsAtIntoMeeting(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	started := int64(1_700_000_000_000)
	token := createShotMeeting(t, project)

	w := httptest.NewRecorder()
	ServeLiveShareNoteShot(w, shotUpload(t, project, map[string]string{
		"token": token, "clientShotId": "c1", "at": "1000", "reason": "manual",
	}, tinyPNG))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	shots, err := service.ListLiveNoteShots(token)
	require.NoError(t, err)
	require.Len(t, shots, 1)
	assert.Equal(t, started, shots[0].At)
}

// Offsets are relative to the transcript origin, so the detail view can merge
// frames into the cue list without a second notion of "meeting start".
func TestServeLiveNotesShots_ComputesOffsets(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	base := int64(1_700_000_000_000)
	token := createShotMeeting(t, project)

	_, err := service.AppendLiveNoteSegment(t.Context(), token, mustProjectID(t, project), service.LiveNoteSegment{
		Channel: "mic", Speaker: "我", StartedAt: base + 2000, DurationMS: 1000, Text: "开始",
	})
	require.NoError(t, err)
	_, err = service.AppendLiveNoteShot(t.Context(), token, mustProjectID(t, project), service.LiveNoteShot{
		At: base + 7000, Reason: "manual", FileName: "shots/x.webp",
	}, "c1", "h")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/live-share/notes/shots?token="+token, http.NoBody)
	req.AddCookie(shotProjectCookie(project))
	ServeLiveNotesShots(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Shots []service.LiveNoteShot `json:"shots"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Shots, 1)
	assert.Equal(t, int64(5000), resp.Shots[0].OffsetMS, "origin is the first utterance, not the meeting start")
}

// Without a transcript the origin falls back to the meeting start, otherwise
// every offset would be "milliseconds since 1970".
func TestServeLiveNotesShots_FallsBackToMeetingStart(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	base := int64(1_700_000_000_000)
	token := createShotMeeting(t, project)

	_, err := service.AppendLiveNoteShot(t.Context(), token, mustProjectID(t, project), service.LiveNoteShot{
		At: base + 3000, Reason: "manual", FileName: "shots/x.webp",
	}, "c1", "h")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/live-share/notes/shots?token="+token, http.NoBody)
	req.AddCookie(shotProjectCookie(project))
	ServeLiveNotesShots(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Shots []service.LiveNoteShot `json:"shots"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Shots, 1)
	assert.Equal(t, int64(3000), resp.Shots[0].OffsetMS)
}

func TestServeLiveNoteShotDelete_RemovesRowAndFile(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	base := int64(1_700_000_000_000)
	token := createShotMeeting(t, project)

	upload := httptest.NewRecorder()
	ServeLiveShareNoteShot(upload, shotUpload(t, project, map[string]string{
		"token": token, "clientShotId": "c1", "at": strconv.FormatInt(base, 10), "reason": "manual",
	}, tinyPNG))
	require.Equal(t, http.StatusOK, upload.Code, upload.Body.String())
	var created service.LiveNoteShot
	require.NoError(t, json.Unmarshal(upload.Body.Bytes(), &created))

	del := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete,
		"/api/live-share/notes/shot?token="+token+"&id="+strconv.FormatInt(created.ID, 10), http.NoBody)
	req.AddCookie(shotProjectCookie(project))
	ServeLiveShareNoteShot(del, req)
	require.Equal(t, http.StatusOK, del.Code, del.Body.String())

	shots, err := service.ListLiveNoteShots(token)
	require.NoError(t, err)
	assert.Empty(t, shots)

	dirName, err := service.MeetingDirName(token)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(service.MeetingDirPath(project, dirName), created.FileName))

	// Second delete: the row is gone, so the answer is the record endpoints'
	// "not found" rather than a silent success.
	again := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete,
		"/api/live-share/notes/shot?token="+token+"&id="+strconv.FormatInt(created.ID, 10), http.NoBody)
	req.AddCookie(shotProjectCookie(project))
	ServeLiveShareNoteShot(again, req)
	assert.Equal(t, http.StatusNotFound, again.Code)
}
