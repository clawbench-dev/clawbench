package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/liveshare"
	"clawbench/internal/model"

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

func TestLiveShareCreate_TitleFallsBackToProjectBaseName(t *testing.T) {
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", "/home/u/myproj", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var resp liveShareManageResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	room := liveRegistry.Get(resp.Token)
	require.NotNil(t, room)
	assert.Equal(t, "myproj", room.Title())
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
	resetLiveRegistry(t)

	// Ending twice must not surface an error: the publisher's UI may retry, and
	// the user-visible outcome (the share is gone) is identical.
	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodDelete, "/api/live-share?token=deadbeef", projPath("/proj/a"), nil))

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLiveShareEnd_RequiresToken(t *testing.T) {
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

func TestLastPathSegment(t *testing.T) {
	assert.Equal(t, "proj", lastPathSegment("/a/b/proj"))
	assert.Equal(t, "proj", lastPathSegment(`C:\a\b\proj`))
	assert.Equal(t, "proj", lastPathSegment("proj"))
	assert.Equal(t, "", lastPathSegment("/a/b/"))
}

// ── WebSocket end-to-end ─────────────────────────────────────────────────────

// liveWSServer wires the publisher and signal handlers onto one test server.
func liveWSServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	resetLiveRegistry(t)
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

func readViewerMsg(t *testing.T, conn *websocket.Conn) liveshare.ViewerMessage {
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
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

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

func TestLiveWS_OfferAnswerRoundTrip(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

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
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

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
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

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
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

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
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

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
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

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
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

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
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

	_ = readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	require.NoError(t, pub.Close(websocket.StatusNormalClosure, "bye"))

	msg := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgPublisherGone, msg.Type)
}

func TestLiveWS_EndControlClosesRoom(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialPublisher(t, srv, token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

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
	// "the room switch went off" only by this field.
	for _, s := range []*recordingSink{s1, s2} {
		msg, ok := s.last().(liveshare.ViewerMessage)
		require.True(t, ok, "viewers must receive a ViewerMessage")
		assert.Equal(t, liveshare.MsgTalkRevoked, msg.Type)
		assert.Equal(t, liveshare.ReasonDisabled, msg.Reason)
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
	view := dialWS(t, srv, "/api/share/"+token+"/signal")
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
	view1 := dialWS(t, srv, "/api/share/"+token+"/signal")
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
	view1 := dialWS(t, srv, "/api/share/"+token+"/signal")
	_ = readViewerMsg(t, view1)
	_ = readPublisherMsg(t, pub)
	view2 := dialWS(t, srv, "/api/share/"+token+"/signal")
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
	view := dialWS(t, srv, "/api/share/"+token+"/signal")
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
