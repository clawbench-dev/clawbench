package handler

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/service"
)

// realPNG encodes a decodable image: the upload path only sniffs magic bytes,
// but the image endpoint actually decodes, so the fixture has to be valid.
func realPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))))
	return buf.Bytes()
}

// uploadShot stores one frame through the real handler and returns the row the
// server created (its id is what every read path addresses).
func uploadShot(t *testing.T, project, token string, body []byte) service.LiveNoteShot {
	t.Helper()
	rec := httptest.NewRecorder()
	ServeLiveShareNoteShot(rec, shotUpload(t, project, map[string]string{
		"token": token, "clientShotId": "c1", "reason": "manual", "at": "1700000001000",
	}, body))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var shot service.LiveNoteShot
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &shot))
	return shot
}

// The record a member opens must carry the frames, not just the text: the
// transcript is supposed to read the same on both sides.
func TestServeShareLiveNotes_RecordCarriesShots(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	token := createShotMeeting(t, project)
	uploadShot(t, project, token, realPNG(t, 4, 3))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/share/"+token+"/notes?since=0&full=1", nil)
	ServeShareLiveNotes(rec, req, token)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var payload liveNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Len(t, payload.Shots, 1)
	assert.Positive(t, payload.Shots[0].ID)
	assert.Equal(t, "manual", payload.Shots[0].Reason)
	// The stored name is a path inside the publisher's project: viewers address
	// a frame by id, so the name never travels.
	assert.Empty(t, payload.Shots[0].FileName)
}

// The live poll runs every few seconds and the viewer already watches the
// picture: frames belong to the record only.
func TestServeShareLiveNotes_LivePollHasNoShots(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	token := createShotMeeting(t, project)
	uploadShot(t, project, token, realPNG(t, 4, 3))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/share/"+token+"/notes?since=0", nil)
	ServeShareLiveNotes(rec, req, token)

	require.Equal(t, http.StatusOK, rec.Code)
	var payload liveNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	assert.Empty(t, payload.Shots)
}

// A frame ref may be an absolute path on the publisher's machine. The viewer
// gets the base name — enough to follow the transcript, not enough to learn
// where the project lives.
func TestServeShareLiveNotes_RedactsShotRefPath(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	token := createShotMeeting(t, project)

	rec := httptest.NewRecorder()
	ServeLiveShareNoteShot(rec, shotUpload(t, project, map[string]string{
		"token": token, "clientShotId": "c-file", "reason": "file",
		"ref": `C:\work\secret\docs\arch.md`,
	}, realPNG(t, 4, 3)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = httptest.NewRecorder()
	ServeShareLiveNotes(rec, httptest.NewRequest(http.MethodGet, "/api/share/"+token+"/notes?full=1", nil), token)

	var payload liveNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Len(t, payload.Shots, 1)
	assert.Equal(t, "arch.md", payload.Shots[0].Ref)
}

func TestServeShareLiveNoteShotImage_ServesThumbnail(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	token := createShotMeeting(t, project)
	shot := uploadShot(t, project, token, realPNG(t, 40, 30))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/share/"+token+"/notes/shot?id="+strconv.FormatInt(shot.ID, 10)+"&w=100",
		nil,
	)
	ServeShareLiveNoteShotImage(rec, req, token)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "image/jpeg", rec.Header().Get("Content-Type"))
	assert.NotEmpty(t, rec.Body.Bytes())
}

// The id is scoped to the meeting: knowing another meeting's frame id must not
// read its picture.
func TestServeShareLiveNoteShotImage_RejectsForeignMeeting(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	token := createShotMeeting(t, project)
	shot := uploadShot(t, project, token, realPNG(t, 4, 3))

	// A second meeting in a DIFFERENT project: the registry hands back the
	// existing room for the same project, so reusing it would test nothing.
	other := createShotMeeting(t, t.TempDir())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/share/"+other+"/notes/shot?id="+strconv.FormatInt(shot.ID, 10),
		nil,
	)
	ServeShareLiveNoteShotImage(rec, req, other)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestServeShareLiveNoteShotImage_RejectsUnknownID(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	token := createShotMeeting(t, project)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/share/"+token+"/notes/shot?id=999999", nil)
	ServeShareLiveNoteShotImage(rec, req, token)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
