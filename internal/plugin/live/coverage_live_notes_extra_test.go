package live

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Clear-all must not take the rows it just reported as skipped with it. The
// regular "skips a live meeting" test checks the live branch of the loop; these
// two go one step further and pin the counters against a MIXED list, so a
// regression that counts one row twice (or forgets to delete the deletable ones
// after a skip) shows up as a wrong number rather than a passing test.
func TestServeLiveNotesClear_MixedListReportsExactCounts(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)

	deletable := []string{
		"0123456789abcdef0123456789abcde1",
		"0123456789abcdef0123456789abcde2",
		"0123456789abcdef0123456789abcde3",
	}
	for i, token := range deletable {
		require.NoError(t, service.StartLiveNote(token, pid, "已结束", 1_700_000_000_000+int64(i)))
	}

	// One still being published (the room exists), one whose minutes job is
	// running (the artifact row says so). Both are skipped for different reasons.
	liveRoom, err := LiveRegistry.Create(projPath(project), "进行中")
	require.NoError(t, err)
	t.Cleanup(func() { LiveRegistry.Delete(liveRoom.Token()) })
	require.NoError(t, service.StartLiveNote(liveRoom.Token(), pid, "进行中", 1_700_000_100_000))

	generating := "0123456789abcdef0123456789abcde4"
	require.NoError(t, service.StartLiveNote(generating, pid, "写纪要中", 1_700_000_200_000))
	// A FRESH running job: job_at is recent, so it is not yet stale.
	now := time.Now().UnixMilli()
	_, err = db.Exec(
		`INSERT INTO live_notes_artifacts (token, kind, job_status, job_at, started_at)
		 VALUES (?, ?, ?, ?, ?)`,
		generating, service.ArtifactMinutes, service.ArtifactJobRunning, now, now)
	require.NoError(t, err)

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
	assert.Equal(t, len(deletable), body.Deleted, "every finished meeting goes")
	assert.Equal(t, 2, body.Skipped, "the live one and the generating one are both reported")

	for _, token := range deletable {
		_, exists, getErr := service.GetLiveNote(token)
		require.NoError(t, getErr)
		assert.False(t, exists, "a deletable meeting must be gone")
	}
	for _, token := range []string{liveRoom.Token(), generating} {
		_, exists, getErr := service.GetLiveNote(token)
		require.NoError(t, getErr)
		assert.True(t, exists, "%s was reported skipped, so it must still exist", token)
	}
}

// The delete of ONE meeting goes through the same two guards. A meeting whose
// artifact job is running answers 409 with its own key (not 500): the request is
// well-formed, the meeting simply cannot be deleted yet.
func TestServeLiveNotesDelete_RefusesWhileArtifactsAreGenerating(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)

	token := "0123456789abcdef0123456789abcde5"
	require.NoError(t, service.StartLiveNote(token, pid, "写纪要中", 1_700_000_200_000))
	now := time.Now().UnixMilli()
	_, err = db.Exec(
		`INSERT INTO live_notes_artifacts (token, kind, job_status, job_at, started_at)
		 VALUES (?, ?, ?, ?, ?)`,
		token, service.ArtifactMinutes, service.ArtifactJobQueued, now, now)
	require.NoError(t, err)

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions?token="+token, project)
	req.Method = http.MethodDelete
	rec := httptest.NewRecorder()
	ServeLiveNotesSessions(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "LiveNotesGenerating")

	_, exists, err := service.GetLiveNote(token)
	require.NoError(t, err)
	assert.True(t, exists, "the refused delete must leave the record in place")

	// Once the job is no longer active (stale), the same delete succeeds — the
	// guard must never make a row permanently undeletable.
	_, err = db.Exec(`UPDATE live_notes_artifacts SET job_status = ? WHERE token = ?`, service.ArtifactJobFailed, token)
	require.NoError(t, err)
	req = liveNotesProjectRequest(t, "/api/live-share/notes/sessions?token="+token, project)
	req.Method = http.MethodDelete
	rec = httptest.NewRecorder()
	ServeLiveNotesSessions(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	_, exists, err = service.GetLiveNote(token)
	require.NoError(t, err)
	assert.False(t, exists)
}

// A record that is currently being published is refused with "meeting is live":
// deleting it would rip the notes out from under the publishers still uploading.
func TestServeLiveNotesDelete_RefusesAStillLiveMeeting(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)

	room, err := LiveRegistry.Create(projPath(project), "进行中")
	require.NoError(t, err)
	t.Cleanup(func() { LiveRegistry.Delete(room.Token()) })
	require.NoError(t, service.StartLiveNote(room.Token(), pid, "进行中", 1_700_000_000_000))

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions?token="+room.Token(), project)
	req.Method = http.MethodDelete
	rec := httptest.NewRecorder()
	ServeLiveNotesSessions(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "LiveNotesMeetingLive")

	_, exists, err := service.GetLiveNote(room.Token())
	require.NoError(t, err)
	assert.True(t, exists)
}

// Renaming to a blank or over-long title is refused (the create form's own limit),
// and a valid name is applied trimmed. The response carries the refreshed record
// so the header can redraw without a second fetch.
func TestServeLiveNotesRename_ValidatesAndReturnsTheRecord(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)

	token := "0123456789abcdef0123456789abcde6"
	require.NoError(t, service.StartLiveNote(token, pid, "占位名", 1_700_000_000_000))

	for _, title := range []string{"", "   ", strings.Repeat("长", liveNoteTitleMaxRunes+1)} {
		req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions?token="+token, project)
		req.Method = http.MethodPatch
		req.Header.Set("Content-Type", "application/json")
		req.Body = jsonBody(t, map[string]any{"title": title})
		rec := httptest.NewRecorder()
		ServeLiveNotesSessions(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "title %q: %s", title, rec.Body.String())
	}

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions?token="+token, project)
	req.Method = http.MethodPatch
	req.Header.Set("Content-Type", "application/json")
	req.Body = jsonBody(t, map[string]any{"title": "  产品评审  "})
	rec := httptest.NewRecorder()
	ServeLiveNotesSessions(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Session struct {
			Title string `json:"title"`
		} `json:"session"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "产品评审", body.Session.Title)
}

// A meeting that never produced artifacts has no folder, so the artifact endpoint
// answers 404 instead of inventing a path to read.
func TestServeLiveNotesArtifact_NoFolderIsNotFound(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)

	token := "0123456789abcdef0123456789abcde7"
	require.NoError(t, service.StartLiveNote(token, pid, "没录到东西", 1_700_000_000_000))

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions/artifact?token="+token, project)
	rec := httptest.NewRecorder()
	ServeLiveNotesArtifact(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// The shots endpoint must not pretend a frame's offset is meaningful when there
// is no transcript and no start time: every offset clamps to 0, which is what the
// detail view renders as "at the beginning".
func TestServeLiveNotesShots_OffsetsClampWithoutATranscript(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)

	token := "0123456789abcdef0123456789abcde8"
	require.NoError(t, service.StartLiveNote(token, pid, "看图", 1_700_000_000_000))

	req := liveNotesProjectRequest(t, "/api/live-share/notes/shots?token="+token, project)
	rec := httptest.NewRecorder()
	ServeLiveNotesShots(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Shots []struct {
			OffsetMS int64 `json:"offsetMs"`
		} `json:"shots"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	for _, shot := range body.Shots {
		assert.GreaterOrEqual(t, shot.OffsetMS, int64(0))
	}
}

// ── small helpers ─────────────────────────────────────────────────────────

// jsonBody marshals a request body into a ReadCloser, so a PATCH fixture reads
// like the client's payload instead of a hand-escaped string literal.
func jsonBody(t *testing.T, v any) io.ReadCloser {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return io.NopCloser(bytes.NewReader(raw))
}
