package share

import (
	"clawbench/internal/plugin/live"
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

// 这四个用例验证的是「分享页怎么解析一个会议/纪要 token」——属于分享功能，
// 不是会议纪要本身。会议纪要搬进 internal/plugin/live 之后，这里通过
// pluglive 引用它的房间表与类型。

func TestShareMeta_LiveWinsOverDatabase(t *testing.T) {
	resetLiveRegistry(t)
	room, err := live.LiveRegistry.Create(projPath("/proj/a"), "live")
	require.NoError(t, err)

	// A live room must resolve even when no DB row exists for the token — and,
	// by ordering, even if one did. This pins the precedence documented in
	// serveShareMeta.
	w := httptest.NewRecorder()
	serveShareMeta(w, newRequest(t, http.MethodGet, "/api/share/"+room.Token()+"/meta", nil), room.Token())

	var resp shareMetaResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, shareKindLive, resp.Kind, "live must be checked before the DB lookups")
}

func TestShareMeta_ReportsLiveKind(t *testing.T) {
	resetLiveRegistry(t)
	room, err := live.LiveRegistry.Create(projPath("/proj/a"), "直播标题")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	serveShareMeta(w, newRequest(t, http.MethodGet, "/api/share/"+room.Token()+"/meta", nil), room.Token())

	require.Equal(t, http.StatusOK, w.Code)
	var resp shareMetaResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, shareKindLive, resp.Kind)
	assert.Equal(t, "直播标题", resp.Title)
}

// The switch defaults to off, so a meeting that just ended is NOT readable by
// whoever holds the link. This is the whole point of the opt-in: the token is
// handed out so people can join, not so the minutes leak afterwards.
//
// Uses the full handler environment rather than the notes-only fixture: the
// meta endpoint falls through to the session and file share lookups, which need
// their tables to exist.
func TestServeShareLiveNotes_RecordIs404UntilShared(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	resetLiveRegistry(t)

	pid, err := store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)
	const token = "11223344556677889900aabbccddeeff"
	require.NoError(t, service.StartLiveNote(token, pid, "内部复盘", 1_700_000_000_000))
	require.NoError(t, service.EndLiveNote(token, 1_700_000_060_000))

	// Room gone, switch untouched (off).
	w := httptest.NewRecorder()
	live.ServeShareLiveNotes(w, httptest.NewRequest(http.MethodGet, "/api/share/x/notes", http.NoBody), token)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Same for the meta endpoint: the share page must report "link gone" rather
	// than mounting a record view that 404s on its first fetch.
	w = httptest.NewRecorder()
	serveShareMeta(w, newRequest(t, http.MethodGet, "/api/share/"+token+"/meta", nil), token)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// After the owner publishes it, both resolve.
	require.NoError(t, service.SetMeetingShareEnabled(pid, token, true))
	w = httptest.NewRecorder()
	live.ServeShareLiveNotes(w, httptest.NewRequest(http.MethodGet, "/api/share/x/notes", http.NoBody), token)
	assert.Equal(t, http.StatusOK, w.Code)
	w = httptest.NewRecorder()
	serveShareMeta(w, newRequest(t, http.MethodGet, "/api/share/"+token+"/meta", nil), token)
	assert.Equal(t, http.StatusOK, w.Code)
}

// The dispatcher must resolve a finished meeting to `live` + ended, so the
// share page opens the record instead of asking for a nickname to join a room
// that no longer exists.
func TestShareMeta_RecordOutlivesRoom(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	// serveShareMeta falls through to the session and file share lookups, which
	// need their tables to exist.
	_, err := db.Exec(service.SessionSharesDDL)
	require.NoError(t, err)
	_, err = db.Exec(service.FileSharesDDL)
	require.NoError(t, err)
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	const token = "aaaabbbbccccddddeeeeffff00001111"
	require.NoError(t, service.StartLiveNote(token, pid, "发版评审", 1_700_000_000_000))
	require.NoError(t, service.EndLiveNote(token, 1_700_000_060_000))
	require.NoError(t, service.SetMeetingShareEnabled(pid, token, true))

	w := httptest.NewRecorder()
	serveShareMeta(w, newRequest(t, http.MethodGet, "/api/share/"+token+"/meta", nil), token)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp shareMetaResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, shareKindLive, resp.Kind)
	assert.True(t, resp.Ended)
	assert.Equal(t, "发版评审", resp.Title)
	assert.Equal(t, int64(1_700_000_000_000), resp.StartedAt)
	assert.Equal(t, int64(1_700_000_060_000), resp.EndedAt)
}

// The public notes route lives under the share subtree because a live token has
// no file_shares row — the file-share lookup would 404 it before dispatch.
func TestServeSharePublic_RoutesNotesSubpath(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	room, err := live.LiveRegistry.Create(project, "会议")
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
	var resp struct {
		Segments []struct {
			Text string `json:"text"`
		} `json:"segments"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Segments, 1)
	assert.Equal(t, "你好", resp.Segments[0].Text)
}
