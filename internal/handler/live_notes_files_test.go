package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fileTestToken = "0123456789abcdef0123456789abcdef"

func TestServeLiveShareNoteFiles_RequiresProject(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()

	w := httptest.NewRecorder()
	ServeLiveShareNoteFiles(w, newRequest(t, http.MethodPost, "/api/live-share/notes/files", map[string]any{
		"token": fileTestToken,
		"files": []map[string]any{{"path": "a.md", "openedAt": 1000}},
	}))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeLiveShareNoteFiles_RejectsForeignToken(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	project := projPath("/proj/files-foreign")
	other := projPath("/proj/files-other")
	otherID, err := store.ProjectIDForPath(other)
	require.NoError(t, err)
	require.NoError(t, service.StartLiveNote(fileTestToken, otherID, "别人的会", 1_700_000_000_000))

	w := httptest.NewRecorder()
	ServeLiveShareNoteFiles(w, liveReq(t, http.MethodPost, "/api/live-share/notes/files", project, map[string]any{
		"token": fileTestToken,
		"files": []map[string]any{{"path": "a.md", "openedAt": 1000}},
	}))
	assert.Equal(t, http.StatusNotFound, w.Code, "a token from another project must look nonexistent")
}

func TestServeLiveShareNoteFiles_StoresIntervalsAndSkipsBadEntries(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	project := projPath("/proj/files-store")
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	require.NoError(t, service.StartLiveNote(fileTestToken, pid, "周会", 1_700_000_000_000))

	w := httptest.NewRecorder()
	ServeLiveShareNoteFiles(w, liveReq(t, http.MethodPost, "/api/live-share/notes/files", project, map[string]any{
		"token": fileTestToken,
		"files": []map[string]any{
			{"path": "docs/a.md", "openedAt": 1000, "closedAt": 5000, "source": "preview"},
			{"path": "", "openedAt": 2000},
			{"path": "docs/b.go"},
		},
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	rec := httptest.NewRecorder()
	ServeLiveShareNotes(rec, liveNotesProjectRequest(t, "/api/live-share/notes?token="+fileTestToken, project))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp liveNotesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Files, 1, "unusable entries are skipped, not fatal")
	assert.Equal(t, "docs/a.md", resp.Files[0].Path)
	assert.Equal(t, "a.md", resp.Files[0].Name)
	assert.Equal(t, int64(5000), resp.Files[0].ClosedAt)
}

// The publisher may see the project's tree; a viewer may not. Same rows, one
// field redacted — which is why this is asserted through the public endpoint.
func TestServeShareLiveNotes_RedactsFilePathsForViewers(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := projPath("/proj/files-public")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	require.NoError(t, service.RecordLiveNoteFiles(context.Background(), room.Token(), pid, []service.LiveNoteFile{
		{Owner: "host", Path: "/home/someone/secret/dir/notes.md", OpenedAt: 1000, ClosedAt: 9000},
	}))

	w := httptest.NewRecorder()
	ServeShareLiveNotes(w, httptest.NewRequest(http.MethodGet, "/api/share/tok/notes?since=0", http.NoBody), room.Token())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp liveNotesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Files, 1)
	assert.Empty(t, resp.Files[0].Path, "the viewer must not learn the directory layout")
	assert.Equal(t, "notes.md", resp.Files[0].Name)
}

func TestServeLiveShareNoteFiles_RejectsOversizedBatch(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	project := projPath("/proj/files-batch")
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	require.NoError(t, service.StartLiveNote(fileTestToken, pid, "周会", 1_700_000_000_000))

	files := make([]map[string]any, liveNoteFilesMaxBatch+1)
	for i := range files {
		files[i] = map[string]any{"path": "a.md", "openedAt": 1000 + i}
	}
	w := httptest.NewRecorder()
	ServeLiveShareNoteFiles(w, liveReq(t, http.MethodPost, "/api/live-share/notes/files", project, map[string]any{
		"token": fileTestToken,
		"files": files,
	}))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeLiveShareNoteFiles_RejectsWrongMethod(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	w := httptest.NewRecorder()
	ServeLiveShareNoteFiles(w, newRequest(t, http.MethodGet, "/api/live-share/notes/files", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
