package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const screenTestToken = "0123456789abcdef0123456789abcdef"

func TestServeLiveShareNoteScreen_RequiresProject(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()

	w := httptest.NewRecorder()
	ServeLiveShareNoteScreen(w, newRequest(t, http.MethodPost, "/api/live-share/notes/screen", map[string]any{
		"token":  screenTestToken,
		"sharer": "host",
		"at":     1000,
	}))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeLiveShareNoteScreen_RejectsForeignToken(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	project := projPath("/proj/screen-foreign")
	other := projPath("/proj/screen-other")
	otherID, err := store.ProjectIDForPath(other)
	require.NoError(t, err)
	require.NoError(t, service.StartLiveNote(screenTestToken, otherID, "别人的会", 1_700_000_000_000))

	w := httptest.NewRecorder()
	ServeLiveShareNoteScreen(w, liveReq(t, http.MethodPost, "/api/live-share/notes/screen", project, map[string]any{
		"token":  screenTestToken,
		"sharer": "host",
		"at":     1000,
	}))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeLiveShareNoteScreen_RejectsUnknownSharer(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	project := projPath("/proj/screen-bad")
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	require.NoError(t, service.StartLiveNote(screenTestToken, pid, "周会", 1_700_000_000_000))

	w := httptest.NewRecorder()
	ServeLiveShareNoteScreen(w, liveReq(t, http.MethodPost, "/api/live-share/notes/screen", project, map[string]any{
		"token":  screenTestToken,
		"sharer": "someone-else",
		"at":     1000,
	}))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeLiveShareNoteScreen_RecordsTransitions(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	project := projPath("/proj/screen-record")
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	require.NoError(t, service.StartLiveNote(screenTestToken, pid, "周会", 1_700_000_000_000))

	for _, body := range []map[string]any{
		{"token": screenTestToken, "sharer": "host", "at": 1000},
		{"token": screenTestToken, "sharer": "viewer:v3", "sharerName": "张三", "at": 5000},
		{"token": screenTestToken, "sharer": "", "at": 9000},
	} {
		w := httptest.NewRecorder()
		ServeLiveShareNoteScreen(w, liveReq(t, http.MethodPost, "/api/live-share/notes/screen", project, body))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	rec := httptest.NewRecorder()
	ServeLiveShareNotes(rec, liveNotesProjectRequest(t, "/api/live-share/notes?token="+screenTestToken, project))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp liveNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Screens, 2)
	assert.Equal(t, "host", resp.Screens[0].Sharer)
	assert.Equal(t, int64(5000), resp.Screens[0].EndedAt)
	assert.Equal(t, "张三", resp.Screens[1].SharerName)
	assert.Equal(t, int64(9000), resp.Screens[1].EndedAt)
}

func TestServeLiveShareNoteScreen_RejectsWrongMethod(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	w := httptest.NewRecorder()
	ServeLiveShareNoteScreen(w, newRequest(t, http.MethodGet, "/api/live-share/notes/screen", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
