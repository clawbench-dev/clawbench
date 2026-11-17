package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Publishing a meeting record while the share feature is off would hand out a
// link the server has just promised to revoke, so the toggle is refused — and
// the refusal is a 409 the panel can translate, not a silent no-op.
func TestServeLiveNotesShare_RefusesToPublishWhileSharingIsOff(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	saveFeatureSwitchState(t)
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	require.NoError(t, service.SetMeetingShareEnabled(pid, token, true))

	model.ShareEnabled = false
	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions/share?token="+token, project)
	req.Method = http.MethodPatch
	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(strings.NewReader(`{"enabled":true}`))
	rec := httptest.NewRecorder()
	ServeLiveNotesShare(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)

	enabled, err := service.MeetingShareEnabled(token)
	require.NoError(t, err)
	assert.True(t, enabled, "a refused request must not change the flag")

	// Un-publishing stays allowed: it is the cleanup direction, and refusing it
	// would leave a shared record with no way to turn sharing off.
	model.ShareEnabled = true
	req = liveNotesProjectRequest(t, "/api/live-share/notes/sessions/share?token="+token, project)
	req.Method = http.MethodPatch
	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(strings.NewReader(`{"enabled":false}`))
	rec = httptest.NewRecorder()
	ServeLiveNotesShare(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	enabled, err = service.MeetingShareEnabled(token)
	require.NoError(t, err)
	assert.False(t, enabled)
}

// Sharing is on, but meetings are excluded from it: the second refusal is the
// one that fires, and the record is still published to nobody.
func TestServeLiveNotesShare_RefusesWhenMeetingsAreExcluded(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	saveFeatureSwitchState(t)
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))

	model.ShareEnabled = true
	model.FeaturePolicy.ShareAllowMeetings = false

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions/share?token="+token, project)
	req.Method = http.MethodPatch
	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(strings.NewReader(`{"enabled":true}`))
	rec := httptest.NewRecorder()
	ServeLiveNotesShare(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)

	enabled, err := service.MeetingShareEnabled(token)
	require.NoError(t, err)
	assert.False(t, enabled)

	// A body that says "off" is not subject to either refusal.
	req = liveNotesProjectRequest(t, "/api/live-share/notes/sessions/share?token="+token, project)
	req.Method = http.MethodPatch
	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(strings.NewReader(`{"enabled":false}`))
	rec = httptest.NewRecorder()
	ServeLiveNotesShare(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// A clear-all reports what it left behind, and a record whose token is not in
// the list any more (deleted concurrently) is not counted as either: the reply
// describes this project's visible outcome.
func TestServeLiveNotesClear_ReportsZeroWhenThereIsNothing(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions/all", project)
	req.Method = http.MethodDelete
	rec := httptest.NewRecorder()
	ServeLiveNotesClear(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Deleted int `json:"deleted"`
		Skipped int `json:"skipped"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Zero(t, body.Deleted)
	assert.Zero(t, body.Skipped)
}

// The record is scoped to the project: a clear-all run from another project
// must not delete it (and therefore report zero deletions).
func TestServeLiveNotesClear_IsScopedToTheProject(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	owner := t.TempDir()
	other := t.TempDir()
	pid, err := store.ProjectIDForPath(owner)
	require.NoError(t, err)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions/all", other)
	req.Method = http.MethodDelete
	rec := httptest.NewRecorder()
	ServeLiveNotesClear(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	_, exists, err := service.GetLiveNote(token)
	require.NoError(t, err)
	assert.True(t, exists, "another project's clear-all must not touch this record")
}

// Wrong verb is refused before any work: a POST to a delete-everything endpoint
// must not be read as a delete.
func TestServeLiveNotesClear_RejectsWrongMethod(t *testing.T) {
	rec := httptest.NewRecorder()
	ServeLiveNotesClear(rec, httptest.NewRequest(http.MethodGet, "/api/live-share/notes/sessions/all", http.NoBody))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
