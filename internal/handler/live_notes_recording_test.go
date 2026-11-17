package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// resetLiveRecordings drops the in-memory session table between tests: it is a
// package-level map, so one test's leftovers would make the next one's first
// chunk look like a retry.
func resetLiveRecordings(t *testing.T) {
	t.Helper()
	liveRecordingMu.Lock()
	liveRecordingSessions = map[string]*liveRecordingSession{}
	liveRecordingMu.Unlock()
	t.Cleanup(func() {
		liveRecordingMu.Lock()
		liveRecordingSessions = map[string]*liveRecordingSession{}
		liveRecordingMu.Unlock()
	})
}

func recordingUpload(t *testing.T, project, token, id string, seq int, body []byte) *http.Request {
	t.Helper()
	target := "/api/live-share/notes/recording?token=" + token +
		"&recordingId=" + id + "&seq=" + strconv.Itoa(seq) + "&at=1700000001000"
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/octet-stream")
	if project != "" {
		req.AddCookie(&http.Cookie{
			Name:  model.ScopedCookieName("clawbench_project"),
			Value: project,
		})
	}
	return req
}

func recordingFinish(t *testing.T, project, token, id string) *http.Request {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"token": token, "recordingId": id})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/live-share/notes/recording/finish", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	if project != "" {
		req.AddCookie(&http.Cookie{
			Name:  model.ScopedCookieName("clawbench_project"),
			Value: project,
		})
	}
	return req
}

func TestServeLiveShareNoteRecording_AppendsOrderedChunks(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	resetLiveRecordings(t)
	project := recordingProject(t)
	token := createShotMeeting(t, project)

	var last struct {
		FileName string `json:"fileName"`
		Bytes    int64  `json:"bytes"`
		NextSeq  int    `json:"nextSeq"`
	}
	for seq, chunk := range []string{"header", "-one", "-two"} {
		w := httptest.NewRecorder()
		ServeLiveShareNoteRecording(w, recordingUpload(t, project, token, "rec-1", seq, []byte(chunk)))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &last))
		assert.Equal(t, seq+1, last.NextSeq)
	}

	// A retry of an already-written chunk must not append it twice: the file
	// would then contain the bytes in the middle of a cluster.
	w := httptest.NewRecorder()
	ServeLiveShareNoteRecording(w, recordingUpload(t, project, token, "rec-1", 1, []byte("-one")))
	require.Equal(t, http.StatusOK, w.Code)
	var retried struct {
		Bytes   int64 `json:"bytes"`
		NextSeq int   `json:"nextSeq"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &retried))
	assert.Equal(t, int64(len("header-one-two")), retried.Bytes)
	assert.Equal(t, 3, retried.NextSeq)

	require.Contains(t, last.FileName, "recording/")
	body, err := os.ReadFile(filepath.Join(service.MeetingDirPath(project, mustMeetingDir(t, token)), last.FileName))
	require.NoError(t, err)
	assert.Equal(t, "header-one-two", string(body))

	// Finish reports the same file and forgets the session, so a second call is
	// a no-op rather than a second file.
	w = httptest.NewRecorder()
	ServeLiveShareNoteRecordingFinish(w, recordingFinish(t, project, token, "rec-1"))
	require.Equal(t, http.StatusOK, w.Code)
	var done struct {
		FileName string `json:"fileName"`
		Bytes    int64  `json:"bytes"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &done))
	assert.Equal(t, last.FileName, done.FileName)
	assert.Equal(t, int64(len("header-one-two")), done.Bytes)

	w = httptest.NewRecorder()
	ServeLiveShareNoteRecordingFinish(w, recordingFinish(t, project, token, "rec-1"))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &done))
	assert.Empty(t, done.FileName)
}

// A gap means the server lost the session (restart) or the client skipped a
// chunk; either way the file can never be played, so the client must be told to
// stop instead of silently producing a corrupt video.
func TestServeLiveShareNoteRecording_RejectsGap(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	resetLiveRecordings(t)
	project := recordingProject(t)
	token := createShotMeeting(t, project)

	w := httptest.NewRecorder()
	ServeLiveShareNoteRecording(w, recordingUpload(t, project, token, "rec-gap", 1, []byte("second")))
	assert.Equal(t, http.StatusConflict, w.Code)

	// Starting at zero after the refusal is still fine: nothing was written.
	w = httptest.NewRecorder()
	ServeLiveShareNoteRecording(w, recordingUpload(t, project, token, "rec-gap", 0, []byte("first")))
	assert.Equal(t, http.StatusOK, w.Code)
}

// A room can exist without a notes row (the record is created by the publisher
// socket). Without a row there is no meeting folder to write into, so the
// answer is 404 rather than an empty file in an arbitrary directory.
func TestServeLiveShareNoteRecording_RequiresAMeetingRecord(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	resetLiveRecordings(t)
	project := recordingProject(t)
	room, err := liveRegistry.Create(project, "无记录")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNoteRecording(w, recordingUpload(t, project, room.Token(), "rec-1", 0, []byte("x")))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// `at` is optional: a client without a clock still gets a correctly ordered
// file name (the server stamps it).
func TestServeLiveShareNoteRecording_DefaultsTheStartTime(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	resetLiveRecordings(t)
	project := recordingProject(t)
	token := createShotMeeting(t, project)

	target := "/api/live-share/notes/recording?token=" + token + "&recordingId=rec-at&seq=0"
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader("x"))
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: project})
	w := httptest.NewRecorder()
	ServeLiveShareNoteRecording(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		FileName string `json:"fileName"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.FileName, "recording/")
	assert.Contains(t, resp.FileName, "-rec-at.webm")
}

func TestServeLiveShareNoteRecording_RequiresOwnership(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	resetLiveRecordings(t)
	project := recordingProject(t)
	other := recordingProject(t)
	token := createShotMeeting(t, project)

	w := httptest.NewRecorder()
	ServeLiveShareNoteRecording(w, recordingUpload(t, other, token, "rec-x", 0, []byte("x")))
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	ServeLiveShareNoteRecording(w, recordingUpload(t, "", token, "rec-x", 0, []byte("x")))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// The id ends up in a file name; path separators and dots must never reach it.
func TestServeLiveShareNoteRecording_RejectsBadID(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	resetLiveRecordings(t)
	project := recordingProject(t)
	token := createShotMeeting(t, project)

	for _, id := range []string{"../escape", "a/b", "", strings.Repeat("a", 65)} {
		w := httptest.NewRecorder()
		ServeLiveShareNoteRecording(w, recordingUpload(t, project, token, id, 0, []byte("x")))
		assert.Equal(t, http.StatusBadRequest, w.Code, id)
	}
}

func mustMeetingDir(t *testing.T, token string) string {
	t.Helper()
	name, err := service.MeetingDirName(token)
	require.NoError(t, err)
	require.NotEmpty(t, name)
	return name
}

// A malformed chunk must be refused before any file is created: a zero-length
// append would leave an empty recording that can never be played.
func TestServeLiveShareNoteRecording_RejectsBadRequests(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	resetLiveRecordings(t)
	project := recordingProject(t)
	token := createShotMeeting(t, project)

	cases := []struct {
		name   string
		target string
		body   string
	}{
		{"missing token", "/api/live-share/notes/recording?recordingId=rec-1&seq=0", "x"},
		{"negative seq", "/api/live-share/notes/recording?token=" + token + "&recordingId=rec-1&seq=-1", "x"},
		{"empty body", "/api/live-share/notes/recording?token=" + token + "&recordingId=rec-1&seq=0", ""},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, tc.target, strings.NewReader(tc.body))
		req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: project})
		ServeLiveShareNoteRecording(w, req)
		assert.NotEqual(t, http.StatusOK, w.Code, tc.name)
	}

	// A GET is not an append.
	w := httptest.NewRecorder()
	ServeLiveShareNoteRecording(w, recordingUpload(t, project, token, "rec-1", 0, []byte("x")))
	// (the POST above is valid; the method check is covered by its own request)
	req := httptest.NewRequest(http.MethodGet, "/api/live-share/notes/recording?token="+token, http.NoBody)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: project})
	w = httptest.NewRecorder()
	ServeLiveShareNoteRecording(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// The body cap is enforced while reading, not after: an endless upload must not
// be buffered in memory first.
func TestServeLiveShareNoteRecording_RejectsOversizedChunk(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	resetLiveRecordings(t)
	project := recordingProject(t)
	token := createShotMeeting(t, project)

	huge := strings.Repeat("x", liveNoteRecordingChunkMaxBytes+1)
	w := httptest.NewRecorder()
	ServeLiveShareNoteRecording(w, recordingUpload(t, project, token, "rec-big", 0, []byte(huge)))
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

// A stale session table must refuse new work rather than growing forever.
func TestServeLiveShareNoteRecording_RefusesWhenTableIsFull(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	resetLiveRecordings(t)
	project := recordingProject(t)
	token := createShotMeeting(t, project)

	liveRecordingMu.Lock()
	for i := range liveNoteRecordingMaxSessions {
		liveRecordingSessions[liveRecordingKey("other", strconv.Itoa(i))] = &liveRecordingSession{
			fileName: "recording/x.webm",
			started:  time.Now(),
		}
	}
	liveRecordingMu.Unlock()

	w := httptest.NewRecorder()
	ServeLiveShareNoteRecording(w, recordingUpload(t, project, token, "rec-full", 0, []byte("x")))
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

// Two recordings must never share a file, even if the client reuses an id.
func TestServeLiveShareNoteRecording_RefusesToReuseAnExistingFile(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	resetLiveRecordings(t)
	project := recordingProject(t)
	token := createShotMeeting(t, project)

	_, err := service.EnsureMeetingDir(token, mustProjectID(t, project), project, "发版评审", 1_700_000_000_000)
	require.NoError(t, err)
	dirName := mustMeetingDir(t, token)
	recDir := filepath.Join(service.MeetingDirPath(project, dirName), service.LiveNoteRecordingDirName)
	require.NoError(t, os.MkdirAll(recDir, 0o755))
	// The name the handler will build for at=1700000001000.
	name := service.LiveNoteRecordingFileName(1_700_000_001_000, "rec-dup")
	require.NoError(t, os.WriteFile(filepath.Join(service.MeetingDirPath(project, dirName), name), []byte("old"), 0o644))

	w := httptest.NewRecorder()
	ServeLiveShareNoteRecording(w, recordingUpload(t, project, token, "rec-dup", 0, []byte("new")))
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestServeLiveShareNoteRecordingFinish_ValidatesInput(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)
	resetLiveRecordings(t)
	project := recordingProject(t)
	other := recordingProject(t)
	token := createShotMeeting(t, project)

	bad := httptest.NewRequest(http.MethodPost, "/api/live-share/notes/recording/finish", strings.NewReader("{"))
	bad.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: project})
	w := httptest.NewRecorder()
	ServeLiveShareNoteRecordingFinish(w, bad)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	ServeLiveShareNoteRecordingFinish(w, recordingFinish(t, project, token, "../x"))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	ServeLiveShareNoteRecordingFinish(w, recordingFinish(t, other, token, "rec-1"))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Ending a meeting forgets its recordings: a leftover session would make a
// later recording with the same id append into the old file.
func TestDropLiveNoteRecordingSessions_ForgetsOnlyThatMeeting(t *testing.T) {
	resetLiveRecordings(t)
	liveRecordingMu.Lock()
	liveRecordingSessions[liveRecordingKey("tok-a", "1")] = &liveRecordingSession{fileName: "recording/a.webm"}
	liveRecordingSessions[liveRecordingKey("tok-b", "1")] = &liveRecordingSession{fileName: "recording/b.webm"}
	liveRecordingMu.Unlock()

	dropLiveNoteRecordingSessions("tok-a")

	liveRecordingMu.Lock()
	_, keptA := liveRecordingSessions[liveRecordingKey("tok-a", "1")]
	_, keptB := liveRecordingSessions[liveRecordingKey("tok-b", "1")]
	liveRecordingMu.Unlock()
	assert.False(t, keptA)
	assert.True(t, keptB)
}

func TestPruneLiveRecordingSessions_DropsOnlyExpired(t *testing.T) {
	resetLiveRecordings(t)
	liveRecordingMu.Lock()
	liveRecordingSessions[liveRecordingKey("tok", "old")] = &liveRecordingSession{
		fileName: "recording/old.webm",
		started:  time.Now().Add(-liveNoteRecordingSessionTTL - time.Minute),
	}
	liveRecordingSessions[liveRecordingKey("tok", "new")] = &liveRecordingSession{
		fileName: "recording/new.webm",
		started:  time.Now(),
	}
	pruneLiveRecordingSessions(time.Now())
	_, oldKept := liveRecordingSessions[liveRecordingKey("tok", "old")]
	_, newKept := liveRecordingSessions[liveRecordingKey("tok", "new")]
	liveRecordingMu.Unlock()
	assert.False(t, oldKept)
	assert.True(t, newKept)
}
