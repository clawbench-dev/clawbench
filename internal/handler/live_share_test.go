package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/liveshare"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetLiveRegistry installs a fresh registry for a test and restores the
// process-wide one afterwards. The registry is global, so without this a room
// created by one test would be visible to the next.
func resetLiveRegistry(t *testing.T) {
	t.Helper()
	prev := liveRegistry
	liveRegistry = liveshare.NewRegistry()
	// init() applies maxLiveRooms to the ORIGINAL registry only, so a replaced
	// one would silently have no ceiling — tests that fill it up would then
	// pass for the wrong reason.
	liveRegistry.SetMaxRooms(maxLiveRooms)
	t.Cleanup(func() { liveRegistry = prev })
}

// projPath normalizes a test project path the SAME way the server does.
//
// The handlers read the project from a cookie and run it through
// model.NormalizeProjectPath (filepath.Abs + EvalSymlinks + Clean), while the
// tests register rooms under a literal "/proj/a". On POSIX those agree, but on
// Windows the cookie value becomes `C:\proj\a` — a different project key, so
// every manage request answered 404/503 and the whole file failed on that one
// platform. Normalizing here keeps the two sides comparable everywhere; the
// literal is only a name, never a real directory.
func projPath(p string) string {
	return model.NormalizeProjectPath(p)
}

// liveReq builds a request carrying the project cookie the manage endpoints
// require.
func liveReq(t *testing.T, method, path, project string, body any) *http.Request {
	t.Helper()
	req := newRequest(t, method, path, body)
	// The value is URL-escaped exactly as the browser sends it: a Windows path
	// contains backslashes, and net/http silently DROPS a cookie whose Value has
	// an invalid byte — the request then looks like "no project selected".
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: url.QueryEscape(project)})
	return req
}

func TestLiveShareCreate_RequiresProject(t *testing.T) {
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, newRequest(t, http.MethodPost, "/api/live-share", nil))

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, liveShareRoomCountForTest())
}

func TestLiveShareCreate_ReturnsTokenAndPath(t *testing.T) {
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/a"), map[string]string{"title": "我的屏幕"}))

	require.Equal(t, http.StatusOK, w.Code)
	var resp liveShareManageResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Token, 32)
	assert.Equal(t, "/share/"+resp.Token, resp.Path)
	assert.Equal(t, 1, liveShareRoomCountForTest())
}

func TestLiveShareCreate_IsIdempotentForSameProject(t *testing.T) {
	resetLiveRegistry(t)

	var first, second liveShareManageResponse
	w1 := httptest.NewRecorder()
	ServeLiveShareManage(w1, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/a"), nil))
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &first))

	w2 := httptest.NewRecorder()
	ServeLiveShareManage(w2, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/a"), nil))
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &second))

	// A second POST must not mint a new token: the user may already have copied
	// the first link.
	assert.Equal(t, first.Token, second.Token)
	assert.Equal(t, 1, liveShareRoomCountForTest())
}

func TestLiveShareCreate_SeparateProjectsGetSeparateRooms(t *testing.T) {
	resetLiveRegistry(t)

	var a, b liveShareManageResponse
	w1 := httptest.NewRecorder()
	ServeLiveShareManage(w1, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/a"), nil))
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &a))

	w2 := httptest.NewRecorder()
	ServeLiveShareManage(w2, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/b"), nil))
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &b))

	assert.NotEqual(t, a.Token, b.Token)
	assert.Equal(t, 2, liveShareRoomCountForTest())
}

// An unnamed meeting is dated, not named after the project. The project's name
// used to stand in here, which made every meeting in a project look identical
// in the record list; the summary model replaces the placeholder once the
// minutes exist.
func TestLiveShareCreate_TitleFallsBackToTheStartTime(t *testing.T) {
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", "/home/u/myproj", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var resp liveShareManageResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	room := liveRegistry.Get(resp.Token)
	require.NotNil(t, room)
	assert.True(t, service.IsPlaceholderMeetingTitle(room.Title()), "got %q", room.Title())
	assert.NotEqual(t, "myproj", room.Title())
}

// The create body has two shapes: legacy `video` ("is there a picture") and the
// video-meeting pair `camera`/`screen` ("what is the picture"). Getting this
// wrong is invisible in the UI — it shows up as a viewer waiting for a track
// that will never arrive, or as remote control offered on a camera image.
func TestLiveShareCreate_DerivesCameraScreenAndVideo(t *testing.T) {
	cases := []struct {
		name    string
		body    map[string]any
		video   bool
		camera  bool
		screen  bool
		explain string
	}{
		{
			name: "legacy client with no flags", body: nil,
			video: true, camera: false, screen: true,
			explain: "an old client gets the screen share it has always got",
		},
		{
			name: "legacy voice meeting", body: map[string]any{"video": false},
			video: false, camera: false, screen: false,
			explain: "voice is no picture at all, and it never was a screen",
		},
		{
			name: "camera only", body: map[string]any{"camera": true, "screen": false},
			video: true, camera: true, screen: false,
			explain: "a camera-only meeting has a picture but no screen to control",
		},
		{
			name: "screen only", body: map[string]any{"camera": false, "screen": true},
			video: true, camera: false, screen: true,
		},
		{
			name: "camera and screen", body: map[string]any{"camera": true, "screen": true},
			video: true, camera: true, screen: true,
			explain: "the picture-in-picture case: both sources are live",
		},
		{
			name: "video flag is derived, not trusted", body: map[string]any{"video": true, "camera": false, "screen": false},
			video: false, camera: false, screen: false,
			explain: "a client claiming video with no source would strand every viewer",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetLiveRegistry(t)
			w := httptest.NewRecorder()
			ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/a"), tc.body))
			require.Equal(t, http.StatusOK, w.Code)

			var resp liveShareManageResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			room := liveRegistry.Get(resp.Token)
			require.NotNil(t, room)

			assert.Equal(t, tc.video, room.VideoEnabled(), "video: %s", tc.explain)
			assert.Equal(t, tc.camera, room.CameraEnabled(), "camera: %s", tc.explain)
			assert.Equal(t, tc.screen, room.HostScreen(), "screen: %s", tc.explain)
		})
	}
}

// The create form carries the two audience permissions, so a host who ticks
// them there does not have to tick them again inside the meeting. The response
// echoes them because the publisher is the one client that never receives a
// `joined` frame — without the echo the stage would draw both switches as off
// while the room has them on.
func TestLiveShareCreate_AppliesAudiencePermissions(t *testing.T) {
	resetLiveRegistry(t)

	var resp liveShareManageResponse
	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/open"),
		map[string]any{"allowTalk": true, "allowMemberScreen": true}))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	room := liveRegistry.Get(resp.Token)
	require.NotNil(t, room)
	assert.True(t, room.TalkEnabled())
	assert.True(t, room.ScreenEnabled())
	assert.True(t, resp.TalkEnabled)
	assert.True(t, resp.ScreenEnabled)

	// A client that knows nothing about the switches still gets the closed room
	// it has always had.
	w = httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/legacy"), nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	room = liveRegistry.Get(resp.Token)
	require.NotNil(t, room)
	assert.False(t, room.TalkEnabled())
	assert.False(t, room.ScreenEnabled())
	assert.False(t, resp.TalkEnabled)
	assert.False(t, resp.ScreenEnabled)
}

func TestLiveShareCreate_RejectsWhenRoomCeilingReached(t *testing.T) {
	resetLiveRegistry(t)
	for i := range maxLiveRooms {
		_, err := liveRegistry.Create(projPath("/proj/"+string(rune('a'+i))), "x")
		require.NoError(t, err)
	}

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/one-more"), nil))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestLiveShareCreate_AllowsReuseAtCeiling(t *testing.T) {
	resetLiveRegistry(t)
	for i := range maxLiveRooms {
		_, err := liveRegistry.Create(projPath("/proj/"+string(rune('a'+i))), "x")
		require.NoError(t, err)
	}

	// An existing project may still re-POST at the ceiling: that reuses its room
	// rather than adding one, so the limit must not block it.
	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/a"), nil))

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLiveShareStatus_NoRoom(t *testing.T) {
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodGet, "/api/live-share", projPath("/proj/a"), nil))

	require.Equal(t, http.StatusOK, w.Code)
	var resp liveShareStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Active)
}

func TestLiveShareStatus_ReportsViewerCount(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "A")
	require.NoError(t, err)
	_, err = room.AddViewer("", &liveSink{})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodGet, "/api/live-share", projPath("/proj/a"), nil))

	var resp liveShareStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Active)
	assert.Equal(t, 1, resp.Viewers)
}

func TestLiveShareEnd_RemovesRoom(t *testing.T) {
	// Ending a share resolves the project id, so the record tables must exist:
	// without them the handler runs against whatever DB a previous test left
	// behind (a closed one, on a partial run) and panics.
	setupLiveNotesDB(t)
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "A")
	require.NoError(t, err)
	token := room.Token()

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodDelete, "/api/live-share?token="+token, projPath("/proj/a"), nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, liveRegistry.Get(token))
	assert.Equal(t, 0, liveShareRoomCountForTest())
}

func TestLiveShareEnd_RejectsForeignProject(t *testing.T) {
	// Ending a share resolves the project id, so the record tables must exist:
	// without them the handler runs against whatever DB a previous test left
	// behind (a closed one, on a partial run) and panics.
	setupLiveNotesDB(t)
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/owner"), "A")
	require.NoError(t, err)
	token := room.Token()

	// A signed-in user of a DIFFERENT project must not be able to end this
	// share. The token is handed to every viewer, so it is not a secret.
	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodDelete, "/api/live-share?token="+token, projPath("/proj/other"), nil))

	// Uniform 404: the response must not reveal that a foreign room exists.
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NotNil(t, liveRegistry.Get(token), "foreign project must not end the room")
}

func TestLiveShareEnd_UnknownTokenIsOk(t *testing.T) {
	// Ending a share resolves the project id, so the record tables must exist:
	// without them the handler runs against whatever DB a previous test left
	// behind (a closed one, on a partial run) and panics.
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	// Ending twice must not surface an error: the publisher's UI may retry, and
	// the user-visible outcome (the share is gone) is identical.
	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodDelete, "/api/live-share?token=deadbeef", projPath("/proj/a"), nil))

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLiveShareEnd_RequiresToken(t *testing.T) {
	// Ending a share resolves the project id, so the record tables must exist:
	// without them the handler runs against whatever DB a previous test left
	// behind (a closed one, on a partial run) and panics.
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodDelete, "/api/live-share", projPath("/proj/a"), nil))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLiveShareManage_MethodNotAllowed(t *testing.T) {
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPut, "/api/live-share", projPath("/proj/a"), nil))

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestShareMeta_ReportsLiveKind(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "直播标题")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	serveShareMeta(w, newRequest(t, http.MethodGet, "/api/share/"+room.Token()+"/meta", nil), room.Token())

	require.Equal(t, http.StatusOK, w.Code)
	var resp shareMetaResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, shareKindLive, resp.Kind)
	assert.Equal(t, "直播标题", resp.Title)
}

func TestShareMeta_LiveWinsOverDatabase(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "live")
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

func TestLiveTokenFromSignalPath(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/api/share/abc123/signal", "abc123"},
		{"/api/share/abc123/meta", ""},
		{"/api/share//signal", ""},
		{"/api/share/abc/signal", "abc"},
		{"/api/share/a/b/signal", ""},
		{"/api/share/abc/signal/", ""},
		{"/other/abc/signal", ""},
		{"/api/share/abc", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, liveTokenFromSignalPath(c.path), "path=%q", c.path)
	}
}

func TestTruncateRunes(t *testing.T) {
	assert.Equal(t, "abc", truncateRunes("abc", 5))
	assert.Equal(t, "abcde", truncateRunes("abcde", 5))
	assert.Equal(t, "abcde…", truncateRunes("abcdefgh", 5))
	// Rune-based: a multi-byte title must never be split mid-character.
	assert.Equal(t, "屏幕…", truncateRunes("屏幕共享测试", 2))
}

func TestLiveShareCreateBody_UnnamedMeetingGetsADate(t *testing.T) {
	rec := httptest.NewRequest(http.MethodPost, "/api/live-share/create", strings.NewReader(`{}`))
	opts := liveShareCreateBody(rec)

	// The project's own name used to stand in here, which made every meeting in
	// a project carry the identical title ("clawbench").
	assert.True(t, service.IsPlaceholderMeetingTitle(opts.title), "got %q", opts.title)
	assert.NotEqual(t, "ClawBench", opts.title)
}

func TestLiveShareCreateBody_KeepsTheTitleTheUserTyped(t *testing.T) {
	rec := httptest.NewRequest(http.MethodPost, "/api/live-share/create",
		strings.NewReader(`{"title":"  发版评审  "}`))
	opts := liveShareCreateBody(rec)

	assert.Equal(t, "发版评审", opts.title)
	assert.False(t, service.IsPlaceholderMeetingTitle(opts.title))
}

// The two audience switches start closed unless the client asks for them: an
// older client (or an older APK) must not silently open a room it never offered
// the host a switch for.
func TestLiveShareCreateBody_AudienceSwitchesDefaultToOff(t *testing.T) {
	rec := httptest.NewRequest(http.MethodPost, "/api/live-share/create",
		strings.NewReader(`{"title":"周会"}`))
	opts := liveShareCreateBody(rec)

	assert.False(t, opts.allowTalk)
	assert.False(t, opts.allowMemberScreen)

	rec = httptest.NewRequest(http.MethodPost, "/api/live-share/create",
		strings.NewReader(`{"allowTalk":true,"allowMemberScreen":true}`))
	opts = liveShareCreateBody(rec)

	assert.True(t, opts.allowTalk)
	assert.True(t, opts.allowMemberScreen)
}

// ── WebSocket end-to-end ─────────────────────────────────────────────────────

// liveWSServer wires the publisher and signal handlers onto one test server.
func liveWSServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	resetLiveRegistry(t)
	// Attaching a publisher now also creates the meeting record, which needs the
	// notes schema. A nil DB would panic inside the handler.
	setupLiveNotesDB(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "测试共享")
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/live-share/publish/ws", func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: url.QueryEscape(projPath("/proj/a"))})
		LiveSharePublishWS(w, r)
	})
	mux.HandleFunc("/api/share/", ServeSharePublic)
	mux.HandleFunc("/share/", ServeSharePage)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, room.Token()
}

func dialWS(t *testing.T, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+path, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

// dialPublisher dials the publisher socket AND waits until the room reports an
// attached publisher.
//
// The server reserves the slot before upgrading (so a second publisher gets a
// real 409) and only calls AttachPublisher AFTER the handshake returns. The
// client's Dial therefore completes while `Publisher()` is still nil, and a
// viewer that dials at that instant is refused with 404 by design. Waiting here
// is what makes the publisher/viewer pairs deterministic — without it the tests
// pass locally and fail on a slower runner.
func dialPublisher(t *testing.T, srv *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	conn := dialWS(t, srv, "/api/live-share/publish/ws?token="+token)
	room := liveRegistry.Get(token)
	require.NotNil(t, room, "the room must still exist right after the publisher attached")
	require.Eventually(t, func() bool { return room.Publisher() != nil }, 3*time.Second, 5*time.Millisecond,
		"publisher never attached to the room")
	return conn
}

// readViewerMsg returns the next viewer frame, skipping roster snapshots.
//
// The roster is a background state frame: it is re-sent on every join, leave and
// speech change, so a test that cares about `talk-revoked` must not be at the
// mercy of how many people happened to join first. Tests that DO care about the
// roster use readViewerRoster.
func readViewerMsg(t *testing.T, conn *websocket.Conn) liveshare.ViewerMessage {
	t.Helper()
	for {
		m := readViewerFrame(t, conn)
		if m.Type == liveshare.MsgRoster {
			continue
		}
		return m
	}
}

// readViewerRoster waits for the next roster snapshot.
func readViewerRoster(t *testing.T, conn *websocket.Conn) liveshare.ViewerMessage {
	t.Helper()
	for {
		m := readViewerFrame(t, conn)
		if m.Type == liveshare.MsgRoster {
			return m
		}
	}
}

func readViewerFrame(t *testing.T, conn *websocket.Conn) liveshare.ViewerMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	require.NoError(t, err)
	var m liveshare.ViewerMessage
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

func readPublisherMsg(t *testing.T, conn *websocket.Conn) liveshare.PublisherMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	require.NoError(t, err)
	var m liveshare.PublisherMessage
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

func writeJSONFrame(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, websocket.MessageText, data))
}

func TestLiveWS_ViewerJoinReachesPublisher(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")

	joined := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgJoined, joined.Type)
	assert.NotEmpty(t, joined.ViewerID)
	assert.Equal(t, "测试共享", joined.Title)
	assert.NotNil(t, joined.ICEServers, "iceServers must serialize as [] not null")
	assert.Empty(t, joined.ICEServers)

	notice := readPublisherMsg(t, pub)
	assert.Equal(t, liveshare.MsgViewerJoin, notice.Type)
	assert.Equal(t, joined.ViewerID, notice.ViewerID)
}

// A room with no picture must SAY so. The flag is a plain bool, so `omitempty`
// would drop it when false — the viewer would then default to "picture
// expected" and wait for a track that may never arrive. This asserts on the raw
// JSON, because a typed decode cannot tell `false` from absent.
func TestLiveWS_RoomWithoutPictureSaysSo(t *testing.T) {
	srv, token := liveWSServer(t)
	room := liveRegistry.Get(token)
	require.NotNil(t, room)
	room.SetVideoEnabled(false)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=voice-viewer")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := view.Read(ctx)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	video, present := raw["video"]
	require.True(t, present, "a room without a picture must serialize video:false, not omit it")
	assert.Equal(t, false, video)

	// The nickname the server accepted travels to the publisher.
	notice := readPublisherMsg(t, pub)
	assert.Equal(t, liveshare.MsgViewerJoin, notice.Type)
	assert.Equal(t, "voice-viewer", notice.Name)
}

// A video meeting must say BOTH what the picture is made of (camera? screen?)
// and that there is one. `camera` and `hostScreen` are plain bools for the same
// reason as `video`: absent must never be read as "off" by a viewer that would
// then hide a tile the publisher is actually sending.
func TestLiveWS_JoinedCarriesCameraAndHostScreen(t *testing.T) {
	srv, token := liveWSServer(t)
	room := liveRegistry.Get(token)
	require.NotNil(t, room)
	room.SetVideoEnabled(true)
	room.SetCameraEnabled(true)
	room.SetHostScreen(true)

	_ = dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=video-viewer")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := view.Read(ctx)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	camera, present := raw["camera"]
	require.True(t, present, "joined must serialize camera, not omit it")
	assert.Equal(t, true, camera)
	hostScreen, present := raw["hostScreen"]
	require.True(t, present, "joined must serialize hostScreen, not omit it")
	assert.Equal(t, true, hostScreen)
}

// A meeting has no type: the picture can start or stop at any moment, and the
// change has to reach viewers that are already connected AND the room itself —
// a viewer joining afterwards reads its `joined` frame, and a stale flag there
// renders a layout that ended minutes ago.
func TestLiveWS_PictureChangeReachesViewersAndSticks(t *testing.T) {
	srv, token := liveWSServer(t)
	room := liveRegistry.Get(token)
	require.NotNil(t, room)
	room.SetVideoEnabled(true)
	room.SetHostScreen(true)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=early")
	_ = readViewerMsg(t, view) // joined
	_ = readPublisherMsg(t, pub)

	// The host stopped sharing the screen and has no camera: nothing to show.
	writeJSONFrame(t, pub, liveshare.PublisherControl{
		Type:   liveshare.CtrlSetPicture,
		Video:  false,
		Camera: false,
		Screen: false,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := view.Read(ctx)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Equal(t, liveshare.MsgPictureState, raw["type"])
	// None of these may be omitted when false: they are exactly the values the
	// viewer needs in order to hide the picture.
	for _, key := range []string{"video", "camera", "hostScreen"} {
		value, present := raw[key]
		require.True(t, present, "picture-state must serialize %s:false, not omit it", key)
		assert.Equal(t, false, value, key)
	}

	assert.False(t, room.VideoEnabled(), "the room must remember the new state")
	assert.False(t, room.HostScreen(), "the picture is no longer a screen")

	late := dialWS(t, srv, "/api/share/"+token+"/signal?name=late")
	joined := readViewerMsg(t, late)
	assert.False(t, joined.VideoEnabled, "a late viewer must not see a stale picture")
	assert.False(t, joined.HostScreen, "a late viewer must not see a stale screen flag")
}

func TestLiveWS_OfferAnswerRoundTrip(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")

	joined := readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub) // viewer-join

	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlOffer, SDP: "v=0-offer"})

	offer := readPublisherMsg(t, pub)
	require.Equal(t, liveshare.MsgOffer, offer.Type)
	require.Equal(t, joined.ViewerID, offer.ViewerID)
	require.Equal(t, "v=0-offer", offer.SDP)

	writeJSONFrame(t, pub, liveshare.PublisherControl{
		Type:     liveshare.CtrlAnswer,
		ViewerID: joined.ViewerID,
		SDP:      "v=0-answer",
	})

	answer := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgAnswer, answer.Type)
	assert.Equal(t, "v=0-answer", answer.SDP)
}

func TestLiveWS_ICEIsForwardedBothWays(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")

	joined := readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	cand := map[string]any{"candidate": "candidate:1 1 udp 1 10.0.0.1 5000 typ host", "sdpMid": "0"}

	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlICE, Candidate: cand})
	toPub := readPublisherMsg(t, pub)
	require.Equal(t, liveshare.MsgICE, toPub.Type)
	assert.Equal(t, joined.ViewerID, toPub.ViewerID)
	assert.NotNil(t, toPub.Candidate, "candidate must survive the round trip")

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlICE, ViewerID: joined.ViewerID, Candidate: cand})
	toView := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgICE, toView.Type)
	assert.NotNil(t, toView.Candidate)
}

func TestLiveWS_TalkStartRefusedWhenRoomDisabled(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")

	_ = readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	// The room switch is off, so turning the microphone on must be refused
	// loudly rather than silently ignored (a silent drop is an undiagnosable
	// "the toggle does nothing").
	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlTalkStart})

	refused := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgTalkDisabled, refused.Type)
	assert.Equal(t, liveshare.ReasonDisabled, refused.Reason)
}

func TestLiveWS_TalkToggleFlow(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")

	joined := readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	// Turn the room switch on, then the viewer simply turns its mic on — no
	// request, no approval.
	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetTalkEnabled, Enabled: true})
	// Consume the broadcast that tells this viewer the toggle is now available.
	enabled := readViewerMsg(t, view)
	require.Equal(t, liveshare.MsgTalkEnabled, enabled.Type)

	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlTalkStart})
	st := readPublisherMsg(t, pub)
	require.Equal(t, liveshare.MsgTalkState, st.Type)
	require.Equal(t, joined.ViewerID, st.ViewerID)
	assert.True(t, st.Talking)

	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlTalkEnd})
	st = readPublisherMsg(t, pub)
	assert.Equal(t, liveshare.MsgTalkState, st.Type)
	assert.False(t, st.Talking)
}

func TestLiveWS_PublisherMuteReachesViewer(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")

	joined := readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetTalkEnabled, Enabled: true})
	_ = readViewerMsg(t, view) // talk-enabled

	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlTalkStart})
	_ = readPublisherMsg(t, pub) // talk-state true

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlTalkRevoke, ViewerID: joined.ViewerID})

	revoked := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgTalkRevoked, revoked.Type)
	// The publisher is told too, so its viewer list stops showing "speaking".
	st := readPublisherMsg(t, pub)
	assert.Equal(t, liveshare.MsgTalkState, st.Type)
	assert.False(t, st.Talking)
}

func TestLiveWS_DisablingRoomMutesEveryViewer(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")

	joined := readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetTalkEnabled, Enabled: true})
	_ = readViewerMsg(t, view) // talk-enabled

	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlTalkStart})
	_ = readPublisherMsg(t, pub) // talk-state true

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetTalkEnabled, Enabled: false})

	revoked := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgTalkRevoked, revoked.Type)
	assert.Equal(t, liveshare.ReasonDisabled, revoked.Reason)

	st := readPublisherMsg(t, pub)
	assert.Equal(t, liveshare.MsgTalkState, st.Type)
	assert.Equal(t, joined.ViewerID, st.ViewerID)
	assert.False(t, st.Talking)
}

func TestLiveWS_MuteAllReachesSpeakingViewer(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")

	_ = readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetTalkEnabled, Enabled: true})
	// The publisher's switch change is broadcast to every viewer, so an
	// already-connected viewer learns the toggle became available.
	enabled := readViewerMsg(t, view)
	require.Equal(t, liveshare.MsgTalkEnabled, enabled.Type)

	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlTalkStart})
	_ = readPublisherMsg(t, pub) // talk-state true

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlTalkMuteAll})

	revoked := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgTalkRevoked, revoked.Type)
	assert.Equal(t, liveshare.ReasonMuted, revoked.Reason)
}

func TestLiveWS_PublisherGoneOnViewer(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")

	_ = readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	require.NoError(t, pub.Close(websocket.StatusNormalClosure, "bye"))

	msg := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgPublisherGone, msg.Type)
}

func TestLiveWS_EndControlClosesRoom(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")

	_ = readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlEnd})

	msg := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgPublisherGone, msg.Type)
	assert.Nil(t, liveRegistry.Get(token))
}

func TestLiveWS_SignalForUnknownTokenIs404(t *testing.T) {
	srv, _ := liveWSServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/share/"+strings.Repeat("a", 32)+"/signal", nil)
	if err == nil {
		_ = conn.CloseNow()
	}
	require.Error(t, err, "dial to an unknown token must fail")
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestLiveWS_PublishForUnknownTokenIs404(t *testing.T) {
	srv, _ := liveWSServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live-share/publish/ws?token="+strings.Repeat("b", 32), nil)
	if err == nil {
		_ = conn.CloseNow()
	}
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestLiveWS_SecondPublisherConflicts(t *testing.T) {
	srv, token := liveWSServer(t)

	_ = dialWS(t, srv, "/api/live-share/publish/ws?token="+token)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live-share/publish/ws?token="+token, nil)
	if err == nil {
		_ = conn.CloseNow()
	}
	require.Error(t, err, "a second publisher must be refused")
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

// ── Control-frame edge cases ────────────────────────────────────────────────
//
// handlePublisherControl / handleViewerControl are pure switches over the wire
// enums, so they can be driven directly. The WS tests above cover the happy
// paths; these cover the frames that are easy to leave unhandled — an unknown
// type, a revoke for a viewer that is not speaking, and the informational join.

// wsPair is a liveSink wired to a real websocket whose peer records every frame.
type wsPair struct {
	sink *liveSink
	mu   sync.Mutex
	got  []json.RawMessage
}

func (p *wsPair) frames() []json.RawMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]json.RawMessage, len(p.got))
	copy(out, p.got)
	return out
}

// newWSPair returns a sink whose writes land in a peer-side buffer. It uses a
// real connection because liveSink writes through coder/websocket — a stub
// would not exercise the marshal+write path the handler relies on.
func newWSPair(t *testing.T) *wsPair {
	t.Helper()
	pair := &wsPair{}
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		close(ready)
		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			pair.mu.Lock()
			pair.got = append(pair.got, append(json.RawMessage(nil), data...))
			pair.mu.Unlock()
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	<-ready

	pair.sink = &liveSink{conn: conn, mu: &sync.Mutex{}}
	return pair
}

// waitFrames polls until at least n frames arrived (writes are asynchronous on
// the peer side), then returns them.
func (p *wsPair) waitFrames(t *testing.T, n int) []json.RawMessage {
	t.Helper()
	require.Eventually(t, func() bool { return len(p.frames()) >= n }, 3*time.Second, 10*time.Millisecond,
		"expected %d frame(s)", n)
	return p.frames()
}

func TestHandlePublisherControl_UnknownTypeReportsErrorAndKeepsSocket(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)
	pair := newWSPair(t)

	closeConn := handlePublisherControl(room, pair.sink, liveshare.PublisherControl{Type: "nonsense"})

	// Unknown frames must not tear the share down: a newer client may send
	// something this build does not know yet.
	assert.False(t, closeConn)
	frames := pair.waitFrames(t, 1)
	var msg liveshare.PublisherMessage
	require.NoError(t, json.Unmarshal(frames[0], &msg))
	assert.Equal(t, liveshare.MsgError, msg.Type)
	assert.Contains(t, msg.Name, "nonsense", "the offending type must be named")
}

func TestHandlePublisherControl_EndClosesSocket(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)

	assert.True(t, handlePublisherControl(room, newWSPair(t).sink, liveshare.PublisherControl{Type: liveshare.CtrlEnd}))
	// The informational publish frame is a no-op.
	assert.False(t, handlePublisherControl(room, newWSPair(t).sink, liveshare.PublisherControl{Type: liveshare.CtrlPublish}))
}

// A revoke for a viewer that never spoke is reported back, not swallowed: the
// publisher's UI would otherwise believe the mute landed.
func TestHandlePublisherControl_RevokeForSilentViewerReportsError(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)
	pair := newWSPair(t)

	closeConn := handlePublisherControl(room, pair.sink, liveshare.PublisherControl{
		Type:     liveshare.CtrlTalkRevoke,
		ViewerID: "nobody",
	})

	assert.False(t, closeConn)
	frames := pair.waitFrames(t, 1)
	var msg liveshare.PublisherMessage
	require.NoError(t, json.Unmarshal(frames[0], &msg))
	assert.Equal(t, liveshare.MsgError, msg.Type)
	assert.Equal(t, "nobody", msg.ViewerID)
}

// Turning the room switch off must both mute every viewer and tell each one why.
func TestHandlePublisherControl_DisableRoomNotifiesEveryViewer(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)
	room.SetTalkEnabled(true)

	s1, s2 := &recordingSink{}, &recordingSink{}
	v1, err := room.AddViewer("a", s1)
	require.NoError(t, err)
	_, err = room.AddViewer("b", s2)
	require.NoError(t, err)
	require.NoError(t, room.SetTalking(v1.ID, true))

	handlePublisherControl(room, newWSPair(t).sink, liveshare.PublisherControl{
		Type:    liveshare.CtrlSetTalkEnabled,
		Enabled: false,
	})

	// Reason matters: a viewer distinguishes "the publisher muted you" from
	// "the room switch went off" only by this field. Roster snapshots ride the
	// same socket, so scan for the revocation rather than assuming it is last.
	for _, s := range []*recordingSink{s1, s2} {
		var revoked *liveshare.ViewerMessage
		for _, frame := range s.all() {
			if msg, ok := frame.(liveshare.ViewerMessage); ok && msg.Type == liveshare.MsgTalkRevoked {
				m := msg
				revoked = &m
			}
		}
		require.NotNil(t, revoked, "viewers must be told the switch went off")
		assert.Equal(t, liveshare.ReasonDisabled, revoked.Reason)
	}
	assert.False(t, room.TalkEnabled())
}

func TestHandleViewerControl_JoinIsInformational(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)
	viewer, err := room.AddViewer("a", &recordingSink{})
	require.NoError(t, err)
	pub := &recordingSink{}
	room.SetPublisher(pub)

	handleViewerControl(room, viewer, liveshare.ViewerControl{Type: liveshare.CtrlJoin})

	// The join already happened when the socket attached; a second frame must
	// not produce another viewer-join for the publisher.
	assert.Empty(t, pub.all())
}

// Stopping a microphone that was never started must be harmless (the viewer may
// retry after a network blip). It still emits one "not talking" frame: the
// publisher keys its speaking indicator on these frames, and a duplicate `false`
// is idempotent, whereas suppressing it would leave the indicator stuck if an
// earlier `true` had been lost.
func TestHandleViewerControl_TalkEndWhenNotTalkingIsHarmless(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)
	viewer, err := room.AddViewer("a", &recordingSink{})
	require.NoError(t, err)
	pub := &recordingSink{}
	room.SetPublisher(pub)

	require.NotPanics(t, func() {
		handleViewerControl(room, viewer, liveshare.ViewerControl{Type: liveshare.CtrlTalkEnd})
	})

	require.Len(t, pub.all(), 1)
	msg, ok := pub.last().(liveshare.PublisherMessage)
	require.True(t, ok)
	assert.Equal(t, liveshare.MsgTalkState, msg.Type)
	assert.False(t, msg.Talking)
	assert.Equal(t, viewer.ID, msg.ViewerID)
}

func TestLiveSink_SendPropagatesMarshalErrors(t *testing.T) {
	// A channel cannot be marshaled; Send must report that instead of writing a
	// half frame.
	pair := newWSPair(t)
	require.Error(t, pair.sink.Send(make(chan int)))
	assert.Empty(t, pair.frames(), "a marshal failure must not put bytes on the wire")
}

// recordingSink is the in-memory Sink used where the payload is what matters.
type recordingSink struct {
	mu   sync.Mutex
	msgs []any
}

func (f *recordingSink) Send(msg any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msg)
	return nil
}

func (f *recordingSink) all() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]any, len(f.msgs))
	copy(out, f.msgs)
	return out
}

func (f *recordingSink) last() any {
	all := f.all()
	if len(all) == 0 {
		return nil
	}
	return all[len(all)-1]
}

// A publisher socket opened with a non-GET method must be refused before any
// upgrade attempt.
func TestLiveSharePublishWS_RejectsNonGet(t *testing.T) {
	resetLiveRegistry(t)
	w := httptest.NewRecorder()
	LiveSharePublishWS(w, liveReq(t, http.MethodPost, "/api/live-share/publish/ws", projPath("/proj/a"), nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// A viewer socket must be refused the same way.
func TestLiveShareSignalWS_RejectsNonGet(t *testing.T) {
	resetLiveRegistry(t)
	w := httptest.NewRecorder()
	LiveShareSignalWS(w, newRequest(t, http.MethodPost, "/api/share/tok/signal", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// A malformed token in the signal path is a 404, not a 400: the viewer must not
// be able to tell "wrong shape" from "no such share".
func TestLiveShareSignalWS_RejectsMalformedToken(t *testing.T) {
	resetLiveRegistry(t)
	for _, tok := range []string{"", "short", strings.Repeat("x", 200), "../../etc"} {
		w := httptest.NewRecorder()
		LiveShareSignalWS(w, newRequest(t, http.MethodGet, "/api/share/"+tok+"/signal", nil))
		assert.Equal(t, http.StatusNotFound, w.Code, "token %q", tok)
	}
}

// A room whose publisher detached is not joinable: attaching a viewer to it
// would leave that viewer waiting for an answer that can never come.
func TestLiveShareSignalWS_RefusesRoomWithoutPublisher(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)
	// No SetPublisher: the room exists but nobody is publishing.

	mux := http.NewServeMux()
	mux.HandleFunc("/api/share/", LiveShareSignalWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/share/"+room.Token()+"/signal", nil)
	require.Error(t, err, "the upgrade must be refused")
	if resp != nil {
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	}
}

// An idle room (no publisher, no viewers) must be dropped so it stops occupying
// one of the maxLiveRooms slots. The production ticker runs once a minute, so the
// scan is driven directly here — it is the same code path.
//
// The fan-out the ticker performs is asserted separately (a room with a viewer
// can never be idle, so the two cannot be observed in one room).
func TestLiveShareReap_DropsIdleRoom(t *testing.T) {
	resetLiveRegistry(t)
	liveRegistry.SetLimits(liveshare.DefaultMaxViewers, time.Minute)
	_, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)
	require.Equal(t, 1, liveShareRoomCountForTest())

	// `Reap` takes the reference time, so "now + 2 minutes" needs no sleep.
	dead := liveRegistry.Reap(time.Now().Add(2 * time.Minute))

	require.Len(t, dead, 1)
	assert.Equal(t, 0, liveShareRoomCountForTest())
}

// A room with a publisher must survive the scan: it is an ongoing share that
// simply has no audience yet. Reaping it would 404 a link whose UI still says
// "sharing".
func TestLiveShareReap_KeepsRoomWithPublisher(t *testing.T) {
	resetLiveRegistry(t)
	liveRegistry.SetLimits(liveshare.DefaultMaxViewers, time.Minute)
	room, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)
	room.SetPublisher(&recordingSink{})

	dead := liveRegistry.Reap(time.Now().Add(2 * time.Minute))

	assert.Empty(t, dead)
	assert.Equal(t, 1, liveShareRoomCountForTest())
}

// ── Audience screen sharing over the real sockets ───────────────────────────

func TestLiveWS_ScreenStartDeniedWhenSwitchIsOff(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")
	_ = readViewerMsg(t, view) // joined
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlScreenStart})

	denied := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgScreenDenied, denied.Type)
	assert.Equal(t, liveshare.ReasonDisabled, denied.Reason)
}

func TestLiveWS_ScreenStartBroadcastsWhoIsSharing(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	view1 := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")
	joined1 := readViewerMsg(t, view1)
	_ = readPublisherMsg(t, pub)

	// The publisher opens the switch; both sides learn about it.
	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetScreenEnabled, Enabled: true})
	enabled := readViewerMsg(t, view1)
	assert.Equal(t, liveshare.MsgScreenState, enabled.Type)
	assert.True(t, enabled.ScreenEnabled)
	assert.False(t, enabled.Sharing)
	assert.Equal(t, liveshare.MsgScreenState, readPublisherMsg(t, pub).Type)

	writeJSONFrame(t, view1, liveshare.ViewerControl{Type: liveshare.CtrlScreenStart})

	state := readViewerMsg(t, view1)
	assert.Equal(t, liveshare.MsgScreenState, state.Type)
	assert.True(t, state.Sharing)
	assert.Equal(t, joined1.ViewerID, state.ViewerID)

	// The publisher gets the same frame, with the id it needs to label the
	// preview and to revoke that one viewer.
	pubState := readPublisherMsg(t, pub)
	assert.Equal(t, liveshare.MsgScreenState, pubState.Type)
	assert.Equal(t, joined1.ViewerID, pubState.ViewerID)
	assert.True(t, pubState.Sharing)
}

func TestLiveWS_SecondSharerIsRefused(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	view1 := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")
	_ = readViewerMsg(t, view1)
	_ = readPublisherMsg(t, pub)
	view2 := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")
	_ = readViewerMsg(t, view2)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetScreenEnabled, Enabled: true})
	_ = readViewerMsg(t, view1)
	_ = readViewerMsg(t, view2)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, view1, liveshare.ViewerControl{Type: liveshare.CtrlScreenStart})
	_ = readViewerMsg(t, view1)
	_ = readViewerMsg(t, view2)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, view2, liveshare.ViewerControl{Type: liveshare.CtrlScreenStart})

	denied := readViewerMsg(t, view2)
	assert.Equal(t, liveshare.MsgScreenDenied, denied.Type)
	assert.Equal(t, liveshare.ReasonBusy, denied.Reason)
}

func TestLiveWS_RevokeStopsOneSharerAndDisablingStopsTheRoom(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")
	joined := readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetScreenEnabled, Enabled: true})
	_ = readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)
	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlScreenStart})
	_ = readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlScreenRevoke, ViewerID: joined.ViewerID})

	denied := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgScreenDenied, denied.Type)
	assert.Equal(t, liveshare.ReasonRevoked, denied.Reason)
	state := readViewerMsg(t, view)
	assert.False(t, state.Sharing)
}

// Switching audience screen sharing off must also stop the current sharer and
// tell it why — its browser is still capturing.
func TestApplySetScreenEnabled_StopsCurrentSharer(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)
	viewer, err := room.AddViewer("a", &recordingSink{})
	require.NoError(t, err)
	pub := &recordingSink{}
	room.SetPublisher(pub)
	room.SetScreenEnabled(true)
	require.NoError(t, room.ClaimScreen(viewer.ID))

	applySetScreenEnabled(room, false)

	assert.False(t, room.ScreenEnabled())
	assert.Empty(t, room.ScreenSharer())
	msg, ok := pub.last().(liveshare.PublisherMessage)
	require.True(t, ok, "the publisher must learn the share stopped")
	assert.Equal(t, liveshare.MsgScreenState, msg.Type)
}

// Revoking a viewer that is not sharing is a no-op: a stop frame racing a
// disconnect must not fail.
func TestApplyScreenRevoke_NotSharingIsNoop(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)
	viewer, err := room.AddViewer("a", &recordingSink{})
	require.NoError(t, err)
	room.SetPublisher(&recordingSink{})
	room.SetScreenEnabled(true)

	require.NotPanics(t, func() { applyScreenRevoke(room, viewer.ID) })
	assert.Empty(t, room.ScreenSharer())
}

func TestApplyScreenRevoke_TellsTheFormerSharer(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)
	sink := &recordingSink{}
	viewer, err := room.AddViewer("a", sink)
	require.NoError(t, err)
	room.SetPublisher(&recordingSink{})
	room.SetScreenEnabled(true)
	require.NoError(t, room.ClaimScreen(viewer.ID))

	applyScreenRevoke(room, viewer.ID)

	assert.Empty(t, room.ScreenSharer())
	var denied bool
	for _, m := range sink.all() {
		if msg, ok := m.(liveshare.ViewerMessage); ok &&
			msg.Type == liveshare.MsgScreenDenied && msg.Reason == liveshare.ReasonRevoked {
			denied = true
		}
	}
	assert.True(t, denied, "the former sharer must be told its share was revoked")
}

// A CtrlScreenStop frame from the sharer clears the state and broadcasts it.
func TestHandleViewerControl_ScreenStopClearsSharer(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/a"), "t")
	require.NoError(t, err)
	viewer, err := room.AddViewer("a", &recordingSink{})
	require.NoError(t, err)
	pub := &recordingSink{}
	room.SetPublisher(pub)
	room.SetScreenEnabled(true)
	require.NoError(t, room.ClaimScreen(viewer.ID))

	handleViewerControl(room, viewer, liveshare.ViewerControl{Type: liveshare.CtrlScreenStop})

	assert.Empty(t, room.ScreenSharer())
	msg, ok := pub.last().(liveshare.PublisherMessage)
	require.True(t, ok)
	assert.Equal(t, liveshare.MsgScreenState, msg.Type)
}

// Remote control is a PUBLISHER capability: the injector lives on the host
// machine and only a whole-screen picture has a desktop to map onto. The viewer
// learns it from the server instead of guessing from its own platform, so a
// late joiner must find it in `joined` — the publisher may have advertised it
// before anyone was in the room.
func TestLiveWS_JoinedCarriesControlCapability(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetControlEnabled, Enabled: true})
	// Wait until the room applied it: the frame is processed on the publisher's
	// read loop, so a viewer that dials immediately can be told "not available"
	// and then receive the advertisement as a separate `control-enabled` frame —
	// which is correct behaviour, but not what this test is about.
	room := liveRegistry.Get(token)
	require.NotNil(t, room)
	require.Eventually(t, func() bool { return room.ControlEnabled() }, 3*time.Second, 5*time.Millisecond,
		"the advertisement must be applied before a viewer joins")

	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")
	// `joined` is not guaranteed to be the FIRST frame: the publisher's
	// advertisement is fanned out from the room and can overtake the join on
	// this socket (the client dispatches by type, so the order is harmless).
	joined := readViewerMsgOfType(t, view, liveshare.MsgJoined)
	assert.True(t, joined.ControlEnabled, "a viewer joining after the advertisement must see it")
}

// readViewerMsgOfType reads frames until one of `want` arrives.
//
// Broadcast frames (roster, control-enabled, ...) are fanned out by the room and
// can be delivered before the frame a test is waiting for; the deadline is the
// per-read timeout, so a missing frame fails the test instead of hanging.
func readViewerMsgOfType(t *testing.T, conn *websocket.Conn, want string) liveshare.ViewerMessage {
	t.Helper()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, data, err := conn.Read(ctx)
		cancel()
		require.NoError(t, err, "never received a %q frame", want)
		var m liveshare.ViewerMessage
		require.NoError(t, json.Unmarshal(data, &m))
		if m.Type == want {
			return m
		}
	}
}

// A viewer that is already in the room learns about a change as it happens —
// its `joined` frame is long gone, and the host can start or stop a screen (and
// therefore the capability) at any moment.
func TestLiveWS_SetControlEnabledReachesEveryViewer(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	view1 := dialWS(t, srv, "/api/share/"+token+"/signal?name=one")
	_ = readViewerMsg(t, view1)
	_ = readPublisherMsg(t, pub)
	view2 := dialWS(t, srv, "/api/share/"+token+"/signal?name=two")
	_ = readViewerMsg(t, view2)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetControlEnabled, Enabled: true})
	for _, v := range []*websocket.Conn{view1, view2} {
		msg := readViewerMsg(t, v)
		assert.Equal(t, liveshare.MsgControlEnabled, msg.Type)
		assert.True(t, msg.ControlEnabled)
	}

	// And back off again: a window capture or a revoked share must take the
	// button away, not leave a stale enabled state.
	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetControlEnabled, Enabled: false})
	for _, v := range []*websocket.Conn{view1, view2} {
		msg := readViewerMsg(t, v)
		assert.Equal(t, liveshare.MsgControlEnabled, msg.Type)
		assert.False(t, msg.ControlEnabled)
	}
}

// A room whose publisher vanished (crash, app restart) is a leftover: reusing
// its token would make the next meeting inherit the previous transcript.
func TestCreateLiveShareRoom_RotatesStaleToken(t *testing.T) {
	resetLiveRegistry(t)
	prev := liveShareReuseGrace
	// Negative, not zero: time.Since can be exactly 0 on a coarse clock
	// (Windows ~15 ms), and "0 <= 0" would keep the stale token.
	liveShareReuseGrace = -time.Second
	t.Cleanup(func() { liveShareReuseGrace = prev })

	project := projPath("/proj/a")
	first, err := createLiveShareRoom(project, "会议")
	require.NoError(t, err)

	second, err := createLiveShareRoom(project, "会议")
	require.NoError(t, err)
	assert.NotEqual(t, first.Token(), second.Token(), "a leftover room must not hand its token to the next meeting")
	assert.Nil(t, liveRegistry.Get(first.Token()), "the stale room is gone")
}

// While a publisher is attached (or about to attach) the room is live and the
// token must stay stable — a second device or a retry must join the same share.
func TestCreateLiveShareRoom_KeepsTokenWhileLive(t *testing.T) {
	resetLiveRegistry(t)
	prev := liveShareReuseGrace
	liveShareReuseGrace = 0
	t.Cleanup(func() { liveShareReuseGrace = prev })

	project := projPath("/proj/a")
	first, err := createLiveShareRoom(project, "会议")
	require.NoError(t, err)
	first.SetPublisher(&recordingSink{})

	second, err := createLiveShareRoom(project, "会议")
	require.NoError(t, err)
	assert.Equal(t, first.Token(), second.Token())
}

// The normal sequence is "create, then attach the socket a moment later": the
// grace period must keep that from rotating the token out from under itself.
func TestCreateLiveShareRoom_KeepsTokenWithinGrace(t *testing.T) {
	resetLiveRegistry(t)

	project := projPath("/proj/a")
	first, err := createLiveShareRoom(project, "会议")
	require.NoError(t, err)

	second, err := createLiveShareRoom(project, "会议")
	require.NoError(t, err)
	assert.Equal(t, first.Token(), second.Token())
}

// The restart path: a record the previous process left open cannot belong to a
// running meeting (rooms are in-memory), so starting the reaper must close it
// right away instead of waiting for the hourly sweep's 12h staleness cutoff.
func TestStartLiveShareReaper_ClosesMeetingsLeftOpenByThePreviousRun(t *testing.T) {
	setupLiveNotesDB(t)
	pid, err := store.ProjectIDForPath("/proj/a")
	require.NoError(t, err)
	require.NoError(t, service.StartLiveNote("orphan", pid, "崩溃的会", time.Now().Add(-time.Hour).UnixMilli()))

	stop := StartLiveShareReaper()
	defer stop()

	note, exists, err := service.GetLiveNote("orphan")
	require.NoError(t, err)
	require.True(t, exists)
	assert.NotZero(t, note.EndedAt, "a record orphaned by a crash must be closed at startup")
}

func TestLiveWS_HostMuteBlocksMicUntilUnmuted(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")

	joined := readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetTalkEnabled, Enabled: true})
	_ = readViewerMsg(t, view) // talk-enabled

	// The host silences this one viewer. Unlike talk-revoke, this must survive
	// the viewer's own attempt to switch the microphone back on.
	writeJSONFrame(t, pub, liveshare.PublisherControl{
		Type: liveshare.CtrlTalkMute, ViewerID: joined.ViewerID, Enabled: true,
	})
	revoked := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgTalkRevoked, revoked.Type)
	assert.Equal(t, liveshare.ReasonHostMuted, revoked.Reason, "a host mute must not read as 'room has no speech'")

	st := readPublisherMsg(t, pub)
	require.Equal(t, liveshare.MsgTalkState, st.Type)
	assert.False(t, st.Talking)
	require.NotNil(t, st.Muted, "the false/true pair must reach the publisher")
	assert.True(t, *st.Muted)

	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlTalkStart})
	refused := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgTalkRevoked, refused.Type)
	assert.Equal(t, liveshare.ReasonHostMuted, refused.Reason)

	// Lifting the block only removes the refusal; the microphone stays off
	// until the viewer turns it back on itself.
	writeJSONFrame(t, pub, liveshare.PublisherControl{
		Type: liveshare.CtrlTalkMute, ViewerID: joined.ViewerID, Enabled: false,
	})
	unmuted := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgTalkUnmuted, unmuted.Type)
	st = readPublisherMsg(t, pub)
	require.NotNil(t, st.Muted)
	assert.False(t, *st.Muted)

	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlTalkStart})
	st = readPublisherMsg(t, pub)
	assert.Equal(t, liveshare.MsgTalkState, st.Type)
	assert.True(t, st.Talking)
}

func TestLiveWS_KickRemovesViewerAndClosesSocket(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal?name=viewer")

	joined := readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlKick, ViewerID: joined.ViewerID})

	// The page learns why it is going away before the socket dies.
	kicked := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgKicked, kicked.Type)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := view.Read(ctx)
	assert.Error(t, err, "the socket must be closed, not merely told about the kick")

	// The publisher list has to lose the row too.
	left := readPublisherMsg(t, pub)
	assert.Equal(t, liveshare.MsgViewerLeave, left.Type)
	assert.Equal(t, joined.ViewerID, left.ViewerID)
}

func TestLiveWS_RosterReachesEveryViewer(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	// The host's nickname travels on the publish frame; it is what the
	// audience's member list shows on the host row.
	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlPublish, Title: "t", Name: "泷泽"})
	// That frame also broadcasts a roster (the host row appeared). Wait until the
	// room has it before anybody joins, otherwise the broadcast can land AFTER
	// the first viewer attached and be read as the join-triggered roster — which
	// has one row fewer and looks like a lost participant.
	room := liveRegistry.Get(token)
	require.NotNil(t, room)
	require.Eventually(t, func() bool { return room.PublisherName() == "泷泽" }, 3*time.Second, 5*time.Millisecond,
		"the publish frame must be applied before a viewer joins")

	first := dialWS(t, srv, "/api/share/"+token+"/signal?name="+url.QueryEscape("小明"))
	joinedFirst := readViewerMsg(t, first)
	require.Equal(t, liveshare.MsgJoined, joinedFirst.Type)
	require.Len(t, joinedFirst.Roster, 2, "the joining viewer gets the list with itself already in it")
	assert.True(t, joinedFirst.Roster[0].Host)
	assert.Equal(t, "泷泽", joinedFirst.Roster[0].Name)
	assert.Equal(t, "小明", joinedFirst.Roster[1].Name)

	second := dialWS(t, srv, "/api/share/"+token+"/signal?name="+url.QueryEscape("小红"))
	joinedSecond := readViewerMsg(t, second)
	require.Len(t, joinedSecond.Roster, 3)

	// The first viewer is told somebody arrived, without asking.
	roster := readViewerRoster(t, first)
	require.Len(t, roster.Roster, 3)
	assert.Equal(t, joinedSecond.ViewerID, roster.Roster[2].ID)

	// And speaking shows up in the same list.
	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetTalkEnabled, Enabled: true})
	_ = readViewerMsg(t, first)
	_ = readViewerMsg(t, second)
	writeJSONFrame(t, second, liveshare.ViewerControl{Type: liveshare.CtrlTalkStart})
	_ = readPublisherMsg(t, pub) // talk-state

	roster = readViewerRoster(t, first)
	require.Len(t, roster.Roster, 3)
	assert.True(t, roster.Roster[2].Talking, "the audience must see who is speaking")
}

func TestLiveWS_RosterDropsALeavingViewer(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	_ = pub
	first := dialWS(t, srv, "/api/share/"+token+"/signal?name=a")
	_ = readViewerMsg(t, first)
	second := dialWS(t, srv, "/api/share/"+token+"/signal?name=b")
	_ = readViewerMsg(t, second)
	_ = readViewerRoster(t, first) // first learns about second

	_ = second.CloseNow()

	roster := readViewerRoster(t, first)
	assert.Len(t, roster.Roster, 2, "the departed viewer must leave the list")
	for _, e := range roster.Roster {
		assert.NotEqual(t, "b", e.Name)
	}
}

// closePublisherAndWait tears the publisher socket down and waits until its
// deferred finalize has closed the meeting record.
//
// Without this the test returns first and the handler's post-close writes race
// the DB teardown — the record's finalize then hits a nil handle and panics in
// a goroutine, which the race detector surfaces as a crash rather than a
// failure. The record's ended_at is the observable proof the write landed.
func closePublisherAndWait(t *testing.T, pub *websocket.Conn, token string) {
	t.Helper()
	_ = pub.CloseNow()
	require.Eventually(t, func() bool {
		note, ok, err := service.GetLiveNote(token)
		return err == nil && ok && note.EndedAt > 0
	}, 3*time.Second, 10*time.Millisecond, "publisher teardown never closed the meeting record")
}

// waitMeetingNotifications polls the log until at least `want` rows of
// `eventType` for `token` exist. Recording happens on the viewer's read-loop
// goroutine, so it is not synchronous with the frame we wrote.
func waitMeetingNotifications(t *testing.T, token, eventType string, want int) []service.Notification {
	t.Helper()
	var matched []service.Notification
	require.Eventually(t, func() bool {
		all, err := service.ListNotifications(service.NotificationFilter{Kind: service.NotificationKindMeeting})
		if err != nil {
			return false
		}
		matched = matched[:0]
		for _, n := range all {
			if n.MeetingToken == token && n.EventType == eventType {
				matched = append(matched, n)
			}
		}
		return len(matched) >= want
	}, 3*time.Second, 10*time.Millisecond, "meeting notification was never recorded")
	return matched
}

// TestLiveWS_ViewerJoinIsRecorded: a join is the only signal a host gets when
// the meeting page is not the thing on screen.
func TestLiveWS_ViewerJoinIsRecorded(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	viewer := dialWS(t, srv, "/api/share/"+token+"/signal?name=张三")
	_ = readViewerMsg(t, viewer) // joined
	defer closePublisherAndWait(t, pub, token)

	rows := waitMeetingNotifications(t, token, service.MeetingEventViewerJoined, 1)
	assert.Equal(t, "测试共享", rows[0].Title)
	assert.Contains(t, rows[0].Body, "张三", "the body names who joined")
}

// TestLiveWS_ControlRequestIsRecorded pins the reason the viewer sends a second
// copy of its request over the WS: the P2P frame never reaches the server, so
// without this one the host could not be notified at all.
func TestLiveWS_ControlRequestIsRecorded(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	viewer := dialWS(t, srv, "/api/share/"+token+"/signal?name=张三")
	_ = readViewerMsg(t, viewer) // joined
	defer closePublisherAndWait(t, pub, token)

	writeJSONFrame(t, viewer, map[string]string{"type": liveshare.CtrlControlRequest})

	rows := waitMeetingNotifications(t, token, service.MeetingEventControlRequest, 1)
	assert.Equal(t, "测试共享", rows[0].Title)
	assert.Contains(t, rows[0].Body, "张三")
	assert.NotZero(t, rows[0].ProjectID)
}

// TestLiveWS_ScreenShareNotifiesOnlyWhenGranted: a refused share changes
// nothing in the room, so announcing it would be a false alarm.
func TestLiveWS_ScreenShareNotifiesOnlyWhenGranted(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	room := liveRegistry.Get(token)
	require.NotNil(t, room)
	viewer := dialWS(t, srv, "/api/share/"+token+"/signal?name=张三")
	_ = readViewerMsg(t, viewer) // joined
	defer closePublisherAndWait(t, pub, token)

	// The switch is off by default: the request is refused.
	writeJSONFrame(t, viewer, map[string]string{"type": liveshare.CtrlScreenStart})
	denied := readViewerMsg(t, viewer)
	assert.Equal(t, liveshare.MsgScreenDenied, denied.Type)

	room.SetScreenEnabled(true)
	writeJSONFrame(t, viewer, map[string]string{"type": liveshare.CtrlScreenStart})

	rows := waitMeetingNotifications(t, token, service.MeetingEventScreenShare, 1)
	assert.Contains(t, rows[0].Body, "张三")
}

// ── Reusing a live room must not rewrite its switches ────────────────────────

// The create endpoint is idempotent per project, so the same POST that starts a
// meeting is also what a retry, a second device or a script sends. Every switch
// in the body defaults to false when omitted, so applying them to a room that is
// already running revokes the audience's microphone and cuts off whoever is
// sharing a screen — the host's own retry would end the meeting's features.
func TestLiveShareCreate_ReuseDoesNotResetALiveMeeting(t *testing.T) {
	resetLiveRegistry(t)

	var first liveShareManageResponse
	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/live"),
		map[string]any{"allowTalk": true, "allowMemberScreen": true, "camera": true, "screen": false, "notes": true}))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))

	room := liveRegistry.Get(first.Token)
	require.NotNil(t, room)
	// The title the audience is looking at (the create body had none, so this is
	// the date placeholder).
	titleBefore := room.Title()
	require.NotEmpty(t, titleBefore)

	// The meeting is running: a publisher is attached, a member is speaking and
	// sharing a screen, and the publisher's picture is a camera (not a screen).
	pubSink := &recordingSink{}
	room.SetPublisher(pubSink)
	viewerSink := &recordingSink{}
	viewer, err := room.AddViewer("成员", viewerSink)
	require.NoError(t, err)
	require.NoError(t, room.SetTalking(viewer.ID, true))
	require.NoError(t, room.ClaimScreen(viewer.ID))
	require.False(t, room.HostScreen())

	// A second device (or the same client retrying after a flaky response)
	// POSTs with only a title — every switch absent.
	w = httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/live"),
		map[string]any{"title": "第二次"}))
	require.Equal(t, http.StatusOK, w.Code)

	assert.True(t, room.TalkEnabled(), "a reuse must not take the audience's microphone away")
	assert.True(t, room.ScreenEnabled(), "a reuse must not disable member screen sharing")
	assert.Equal(t, viewer.ID, room.ScreenSharer(), "the running screen share must not be cut off")
	assert.True(t, viewer.Talking(), "the speaking member must keep the floor")
	assert.True(t, room.CameraEnabled(), "the picture flags are not initial state any more")
	assert.False(t, room.HostScreen(), "the picture flags are not initial state any more")
	// 听记 too: the second POST's body has no `notes`, so it means false. A live
	// meeting learns about a toggle from `set-notes`, and rewriting the flag here
	// (without a broadcast) would tell the NEXT viewer "听记 stopped" while the
	// publisher is still recording — two viewers of one meeting disagreeing.
	assert.True(t, room.NotesEnabled(), "a reuse must not stop 听记 behind the publisher's back")
	// The title is a starting state too. Without this the second device's
	// "第二次" (or, when its form is empty, a fresh date placeholder) would
	// rename the meeting for everyone watching, and for every notification
	// raised afterwards.
	assert.Equal(t, titleBefore, room.Title(), "a reuse must not rename a running meeting")
	// And nothing was pushed at the viewer: a reset would have sent
	// talk-revoked / screen-denied, which is what the audience actually sees.
	for _, msg := range viewerSink.all() {
		vm, ok := msg.(liveshare.ViewerMessage)
		if !ok {
			continue
		}
		assert.NotEqual(t, liveshare.MsgTalkRevoked, vm.Type, "no revocation on reuse")
		assert.NotEqual(t, liveshare.MsgScreenDenied, vm.Type, "no denial on reuse")
	}
}

// The counter-case: a room with nobody on the other side is still initial state,
// so a second create (the normal "POST, then attach" sequence, or a retry inside
// the grace window) must apply its switches. Blanket-skipping would make the
// create form's checkboxes silently ineffective.
func TestLiveShareCreate_ReuseBeforeThePublisherAttachesStillApplies(t *testing.T) {
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/pre"), nil))
	require.Equal(t, http.StatusOK, w.Code)

	var resp liveShareManageResponse
	w = httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/pre"),
		map[string]any{"allowTalk": true, "allowMemberScreen": true}))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	room := liveRegistry.Get(resp.Token)
	require.NotNil(t, room)
	assert.True(t, room.TalkEnabled())
	assert.True(t, room.ScreenEnabled())
	assert.True(t, resp.TalkEnabled)
	assert.True(t, resp.ScreenEnabled)

	// The title follows the same rule as the switches: applied while nobody is
	// publishing, ignored once the meeting is live. This is the "still applies"
	// side, so the guard cannot be a blanket skip.
	w = httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", projPath("/proj/pre"),
		map[string]any{"title": "改过的名字"}))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "改过的名字", room.Title())
}

// ── Meeting notification de-duplication ─────────────────────────────────────

// freezeMeetingNotifyClock replaces the de-duplication clock and returns a
// function that advances it.
//
// The viewer's read loop runs in its own goroutine, so a test cannot sleep its
// way past the 30s window deterministically (and a 30s sleep is not a test). The
// replacement reads an atomic offset, so advancing it is race-free even while
// the handler is inside allow().
func freezeMeetingNotifyClock(t *testing.T) func(time.Duration) {
	t.Helper()
	base := time.Now()
	var offset atomic.Int64
	meetingNotifyDedupe.mu.Lock()
	prev := meetingNotifyDedupe.now
	meetingNotifyDedupe.now = func() time.Time { return base.Add(time.Duration(offset.Load())) }
	meetingNotifyDedupe.mu.Unlock()
	t.Cleanup(func() {
		meetingNotifyDedupe.mu.Lock()
		meetingNotifyDedupe.now = prev
		meetingNotifyDedupe.mu.Unlock()
	})
	return func(d time.Duration) { offset.Add(int64(d)) }
}

// probeMeetingNotify waits until the viewer's read loop has processed everything
// written before the probe.
//
// The probe is a talk-start in a room whose speech switch is off: the server
// answers talk-disabled on the same connection, in order. Frames are handled
// sequentially, so receiving the answer proves the earlier frames have already
// reached the notifier — without it "count is 1" could just mean "the other four
// frames have not been read yet", and the assertion would pass on the buggy code.
func probeMeetingNotify(t *testing.T, viewer *websocket.Conn) {
	t.Helper()
	writeJSONFrame(t, viewer, map[string]string{"type": liveshare.CtrlTalkStart})
	msg := readViewerMsg(t, viewer)
	require.Equal(t, liveshare.MsgTalkDisabled, msg.Type, "the probe must reach the room")
}

// listMeetingNotifications counts the rows of one event type for a token.
func listMeetingNotifications(t *testing.T, token, eventType string) []service.Notification {
	t.Helper()
	all, err := service.ListNotifications(service.NotificationFilter{Kind: service.NotificationKindMeeting})
	require.NoError(t, err)
	var matched []service.Notification
	for _, n := range all {
		if n.MeetingToken == token && n.EventType == eventType {
			matched = append(matched, n)
		}
	}
	return matched
}

// An anonymous viewer can send `control-request` in a loop: each occurrence used
// to write a notification row (whose 500-row cap then evicts real events) and
// broadcast a frame that pops a toast on the host. The window keeps the FIRST
// ask of a burst — that is the one the host must see.
func TestLiveWS_ControlRequestIsDeduplicated(t *testing.T) {
	advance := freezeMeetingNotifyClock(t)
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	defer closePublisherAndWait(t, pub, token)

	viewer := dialWS(t, srv, "/api/share/"+token+"/signal?name=张三")
	_ = readViewerMsg(t, viewer) // joined

	for range 5 {
		writeJSONFrame(t, viewer, map[string]string{"type": liveshare.CtrlControlRequest})
	}
	probeMeetingNotify(t, viewer)
	require.Len(t, listMeetingNotifications(t, token, service.MeetingEventControlRequest), 1,
		"five frames within the window are one ask")

	// A DIFFERENT viewer asking for the same thing is a different fact: the key
	// carries the viewer id, so two people must never be merged.
	other := dialWS(t, srv, "/api/share/"+token+"/signal?name=李四")
	_ = readViewerMsg(t, other) // joined
	writeJSONFrame(t, other, map[string]string{"type": liveshare.CtrlControlRequest})
	probeMeetingNotify(t, other)
	require.Len(t, listMeetingNotifications(t, token, service.MeetingEventControlRequest), 2)

	// ...and the same viewer again after the window is a NEW ask, not a
	// duplicate: the host may have missed the first one entirely.
	advance(31 * time.Second)
	writeJSONFrame(t, viewer, map[string]string{"type": liveshare.CtrlControlRequest})
	probeMeetingNotify(t, viewer)
	require.Len(t, listMeetingNotifications(t, token, service.MeetingEventControlRequest), 3,
		"a repeat after the window must be recorded again")
}

// A reconnect loop arrives as `viewer_joined` with a fresh viewer id each time,
// so that event is keyed by nickname instead: the same person coming back is one
// arrival, two different people are two.
func TestLiveWS_ViewerJoinIsDeduplicatedByNickname(t *testing.T) {
	freezeMeetingNotifyClock(t)
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	defer closePublisherAndWait(t, pub, token)
	room := liveRegistry.Get(token)
	require.NotNil(t, room)

	first := dialWS(t, srv, "/api/share/"+token+"/signal?name=张三")
	_ = readViewerMsg(t, first) // joined
	require.Eventually(t, func() bool { return room.ViewerCount() == 1 }, 3*time.Second, 10*time.Millisecond)

	// Drop and rejoin under the same nickname — a network hiccup, not a person.
	_ = first.CloseNow()
	require.Eventually(t, func() bool { return room.ViewerCount() == 0 }, 3*time.Second, 10*time.Millisecond)

	second := dialWS(t, srv, "/api/share/"+token+"/signal?name=张三")
	_ = readViewerMsg(t, second) // joined
	probeMeetingNotify(t, second)
	require.Len(t, listMeetingNotifications(t, token, service.MeetingEventViewerJoined), 1,
		"the same person reconnecting is not a second arrival")

	// A different person is.
	third := dialWS(t, srv, "/api/share/"+token+"/signal?name=李四")
	_ = readViewerMsg(t, third) // joined
	probeMeetingNotify(t, third)
	require.Len(t, listMeetingNotifications(t, token, service.MeetingEventViewerJoined), 2)
}

// The entry cap is a HARD bound: a burst of one-off keys (many tokens, many
// viewer ids) inside one window must not grow the table without limit. Dropping
// the oldest key only risks a duplicate notification, never a lost one.
func TestMeetingNotifyLimiter_HardCapsTheTable(t *testing.T) {
	frozen := time.Unix(1_700_000_000, 0)
	l := &meetingNotifyLimiter{
		last:   make(map[meetingNotifyKey]time.Time),
		window: time.Minute,
		now:    func() time.Time { return frozen },
	}

	for i := range meetingNotifyDedupeMaxEntries + 50 {
		require.True(t, l.allow(meetingNotifyKey{token: "t", eventType: "e", actor: strconv.Itoa(i)}))
	}

	assert.LessOrEqual(t, len(l.last), meetingNotifyDedupeMaxEntries,
		"a burst of distinct keys must not grow the table past the cap")
}

// ── AI 听记 toggled mid-meeting ─────────────────────────────────────────────

// The room's 听记 flag used to be write-once at create time, so a viewer that
// joined while it was off never learned it came on (the transcript pane never
// appeared) and one watching a stopped transcript kept polling. `set-notes` is
// the publisher's frame; every viewer must hear `notes-enabled`.
func TestLiveWS_SetNotesReachesEveryViewer(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	defer closePublisherAndWait(t, pub, token)
	room := liveRegistry.Get(token)
	require.NotNil(t, room)

	first := dialWS(t, srv, "/api/share/"+token+"/signal?name=甲")
	joined := readViewerMsg(t, first)
	require.Equal(t, liveshare.MsgJoined, joined.Type)
	assert.False(t, joined.NotesEnabled, "the room was created without 听记")

	second := dialWS(t, srv, "/api/share/"+token+"/signal?name=乙")
	_ = readViewerMsg(t, second) // joined

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetNotes, Enabled: true})
	for _, conn := range []*websocket.Conn{first, second} {
		msg := readViewerMsg(t, conn)
		assert.Equal(t, liveshare.MsgNotesEnabled, msg.Type)
		assert.True(t, msg.NotesEnabled)
	}
	assert.True(t, room.NotesEnabled(), "the room must remember it for later joiners")

	// A viewer that arrives AFTER the toggle reads the new value from `joined`
	// — this is the path that made the old write-once flag wrong: it would have
	// said "no 听记" for the rest of the meeting.
	late := dialWS(t, srv, "/api/share/"+token+"/signal?name=丙")
	joinedLate := readViewerMsgOfType(t, late, liveshare.MsgJoined)
	assert.True(t, joinedLate.NotesEnabled, "a late joiner must see 听记 as on")

	// Turning it off has to reach viewers as an explicit `false`: an omitted
	// field would leave the pane polling a transcript that will never grow.
	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetNotes, Enabled: false})
	raw := readViewerRaw(t, first)
	assert.Equal(t, liveshare.MsgNotesEnabled, raw["type"])
	value, present := raw["notesEnabled"]
	require.True(t, present, "notesEnabled must be on the wire even when false")
	assert.Equal(t, false, value)
	assert.False(t, room.NotesEnabled())
}

// readViewerRaw returns the next non-roster viewer frame as a raw map, so a test
// can tell "the field is absent" from "the field is false".
func readViewerRaw(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, data, err := conn.Read(ctx)
		cancel()
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))
		if m["type"] == liveshare.MsgRoster {
			continue
		}
		return m
	}
}

// ── Ending a meeting from another device ────────────────────────────────────

// The publisher page is driven by its socket: if ending the meeting elsewhere
// merely forgot the sink, that page kept showing "sharing" forever.
func TestLiveShareEnd_ClosesThePublisherSocket(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodDelete, "/api/live-share?token="+token, projPath("/proj/a"), nil))
	require.Equal(t, http.StatusOK, w.Code)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := pub.Read(ctx)
	require.Error(t, err, "the publisher socket must be closed when the meeting ends elsewhere")
	// Not just "quiet": a socket left open would also end this read with a
	// deadline, and that is precisely the bug (the page keeps showing
	// "sharing" because nothing ever closed it).
	require.False(t, errors.Is(err, context.DeadlineExceeded),
		"the socket must be CLOSED, not merely idle: %v", err)
}

// ── Viewer nickname ─────────────────────────────────────────────────────────

// A missing name is a real 400 the share page can explain, with a localized key
// (the previous "BadRequest" did not exist, so the body carried the raw key).
func TestLiveShareSignalWS_RequiresAViewerName(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	defer closePublisherAndWait(t, pub, token)

	base := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/share/" + token + "/signal"
	for _, query := range []string{
		"",
		"?name=",
		"?name=%20%20",
		"?name=" + strings.Repeat("x", liveViewerNameMaxRunes+1),
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		// Pin the language: the assertion below compares the TRANSLATED text,
		// which is the only thing that distinguishes "the key exists" from
		// "i18n echoed the key back".
		_, resp, err := websocket.Dial(ctx, base+query, &websocket.DialOptions{
			HTTPHeader: http.Header{"Accept-Language": []string{"en"}},
		})
		cancel()
		require.Error(t, err, "query %q must not upgrade", query)
		require.NotNil(t, resp, "query %q must answer with a status", query)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "query %q", query)
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.NoError(t, readErr)
		assertLocalizedError(t, body, "ViewerNameRequired", "a display name is required to join")
	}
}

// assertLocalizedError asserts the body names msgKey AND carries its
// translation.
//
// `assert.Contains(body, "SomeKey")` is a tautology: i18n.T returns the message
// id verbatim when the key is missing from the locale files, so deleting the key
// leaves that assertion green. The rendered sentence is what tells the two
// apart, and pinning the wording also catches a copy/paste of another message.
// The request must pin a language (Accept-Language), otherwise the expected
// sentence depends on the bundle default.
func assertLocalizedError(t *testing.T, body []byte, wantKey, wantText string) {
	t.Helper()
	var resp model.ErrorResponse
	require.NoError(t, json.Unmarshal(body, &resp), "body: %s", string(body))
	assert.Equal(t, wantKey, resp.MsgKey)
	assert.Equal(t, wantText, resp.Error,
		"the body must carry the translated message, not the raw key")
}

// The nickname is written verbatim as the speaker of every archived chat line
// and into notification bodies, so a newline in it forges a whole extra line in
// the record of the meeting. Control characters are stripped, not rejected: the
// share page forwards the account nickname, and refusing to join over an
// invisible byte is a dead end the reader cannot fix.
func TestLiveShareSignalWS_SanitizesTheViewerName(t *testing.T) {
	srv, token := liveWSServer(t)
	pub := dialPublisher(t, srv, token)
	defer closePublisherAndWait(t, pub, token)
	room := liveRegistry.Get(token)
	require.NotNil(t, room)

	forged := "主持人\n\n[10:03] 主持人：会议已结束"
	viewer := dialWS(t, srv, "/api/share/"+token+"/signal?name="+url.QueryEscape(forged))
	_ = readViewerMsg(t, viewer) // joined

	viewers := room.Viewers()
	require.Len(t, viewers, 1)
	assert.Equal(t, "主持人 [10:03] 主持人：会议已结束", viewers[0].Name)
	assert.NotContains(t, viewers[0].Name, "\n")

	rows := waitMeetingNotifications(t, token, service.MeetingEventViewerJoined, 1)
	assert.NotContains(t, rows[0].Body, "\n", "a newline must not survive into the notification body")
	assert.Contains(t, rows[0].Body, "主持人")
}
