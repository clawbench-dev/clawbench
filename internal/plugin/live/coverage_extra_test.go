package live

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A row whose artifact-job lookup fails is skipped, not fatal: refusing the whole
// clear because one row's query broke would leave the other ninety-nine behind.
func TestServeLiveNotesClear_JobLookupFailureSkipsEveryRow(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()
	pid := mustProjectID(t, project)

	first := "0123456789abcdef0123456789abcde1"
	second := "0123456789abcdef0123456789abcde2"
	require.NoError(t, service.StartLiveNote(first, pid, "第一条", 1_700_000_000_000))
	require.NoError(t, service.StartLiveNote(second, pid, "第二条", 1_700_000_001_000))

	// ArtifactJobActive wraps its scan error, and a missing table is the one fault
	// that hits EVERY row: nothing can be judged, so nothing may be deleted and
	// the whole clear must still answer 200 reporting the skips — refusing the
	// call outright would leave the user with no way to clear anything.
	_, err := db.Exec(`DROP TABLE live_notes_artifacts`)
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
	assert.Zero(t, body.Deleted, "a row whose lookup failed must not be deleted")
	assert.Equal(t, 2, body.Skipped, "both rows are reported as left behind")

	for _, token := range []string{first, second} {
		_, exists, getErr := service.GetLiveNote(token)
		require.NoError(t, getErr)
		assert.True(t, exists, "the clear must not delete a row it could not check")
	}
}

// The list itself failing is a 500 with no body beyond the standard error: the
// caller must not be told "deleted: 0" when nothing was even read.

// The list itself failing is a 500 with no body beyond the standard error: the
// caller must not be told "deleted: 0" when nothing was even read.
func TestServeLiveNotesClear_ListFailureIsInternalError(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	project := t.TempDir()

	_, err := db.Exec(`DROP TABLE live_notes`)
	require.NoError(t, err)

	req := liveNotesProjectRequest(t, "/api/live-share/notes/sessions/all", project)
	req.Method = http.MethodDelete
	rec := httptest.NewRecorder()
	ServeLiveNotesClear(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "internal server error",
		"the failure is reported as a server error, not as \"deleted: 0\"")
}
