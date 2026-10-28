package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	t.Cleanup(func() { liveRegistry = prev })
}

// liveReq builds a request carrying the project cookie the manage endpoints
// require.
func liveReq(t *testing.T, method, path, project string, body any) *http.Request {
	t.Helper()
	req := newRequest(t, method, path, body)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: project})
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
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", "/proj/a", map[string]string{"title": "我的屏幕"}))

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
	ServeLiveShareManage(w1, liveReq(t, http.MethodPost, "/api/live-share", "/proj/a", nil))
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &first))

	w2 := httptest.NewRecorder()
	ServeLiveShareManage(w2, liveReq(t, http.MethodPost, "/api/live-share", "/proj/a", nil))
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
	ServeLiveShareManage(w1, liveReq(t, http.MethodPost, "/api/live-share", "/proj/a", nil))
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &a))

	w2 := httptest.NewRecorder()
	ServeLiveShareManage(w2, liveReq(t, http.MethodPost, "/api/live-share", "/proj/b", nil))
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
		_, err := liveRegistry.Create("/proj/"+string(rune('a'+i)), "x")
		require.NoError(t, err)
	}

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", "/proj/one-more", nil))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestLiveShareCreate_AllowsReuseAtCeiling(t *testing.T) {
	resetLiveRegistry(t)
	for i := range maxLiveRooms {
		_, err := liveRegistry.Create("/proj/"+string(rune('a'+i)), "x")
		require.NoError(t, err)
	}

	// An existing project may still re-POST at the ceiling: that reuses its room
	// rather than adding one, so the limit must not block it.
	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPost, "/api/live-share", "/proj/a", nil))

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLiveShareStatus_NoRoom(t *testing.T) {
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodGet, "/api/live-share", "/proj/a", nil))

	require.Equal(t, http.StatusOK, w.Code)
	var resp liveShareStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Active)
}

func TestLiveShareStatus_ReportsViewerCount(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create("/proj/a", "A")
	require.NoError(t, err)
	_, err = room.AddViewer("", &liveSink{})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodGet, "/api/live-share", "/proj/a", nil))

	var resp liveShareStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Active)
	assert.Equal(t, 1, resp.Viewers)
}

func TestLiveShareEnd_RemovesRoom(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create("/proj/a", "A")
	require.NoError(t, err)
	token := room.Token()

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodDelete, "/api/live-share?token="+token, "/proj/a", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, liveRegistry.Get(token))
	assert.Equal(t, 0, liveShareRoomCountForTest())
}

func TestLiveShareEnd_UnknownTokenIsOk(t *testing.T) {
	resetLiveRegistry(t)

	// Ending twice must not surface an error: the publisher's UI may retry, and
	// the user-visible outcome (the share is gone) is identical.
	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodDelete, "/api/live-share?token=deadbeef", "/proj/a", nil))

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLiveShareEnd_RequiresToken(t *testing.T) {
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodDelete, "/api/live-share", "/proj/a", nil))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLiveShareManage_MethodNotAllowed(t *testing.T) {
	resetLiveRegistry(t)

	w := httptest.NewRecorder()
	ServeLiveShareManage(w, liveReq(t, http.MethodPut, "/api/live-share", "/proj/a", nil))

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestShareMeta_ReportsLiveKind(t *testing.T) {
	resetLiveRegistry(t)
	room, err := liveRegistry.Create("/proj/a", "直播标题")
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
	room, err := liveRegistry.Create("/proj/a", "live")
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
	room, err := liveRegistry.Create("/proj/a", "测试共享")
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/live-share/publish/ws", func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: "/proj/a"})
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

	pub := dialWS(t, srv, "/api/live-share/publish/ws?token="+token)
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

	pub := dialWS(t, srv, "/api/live-share/publish/ws?token="+token)
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

	pub := dialWS(t, srv, "/api/live-share/publish/ws?token="+token)
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

	pub := dialWS(t, srv, "/api/live-share/publish/ws?token="+token)
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

	pub := dialWS(t, srv, "/api/live-share/publish/ws?token="+token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

	joined := readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	// Turn the room switch on, then the viewer simply turns its mic on — no
	// request, no approval.
	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetTalkEnabled, Enabled: true})

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

	pub := dialWS(t, srv, "/api/live-share/publish/ws?token="+token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

	joined := readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetTalkEnabled, Enabled: true})
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

	pub := dialWS(t, srv, "/api/live-share/publish/ws?token="+token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

	joined := readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetTalkEnabled, Enabled: true})
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

	pub := dialWS(t, srv, "/api/live-share/publish/ws?token="+token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

	_ = readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlSetTalkEnabled, Enabled: true})
	writeJSONFrame(t, view, liveshare.ViewerControl{Type: liveshare.CtrlTalkStart})
	_ = readPublisherMsg(t, pub) // talk-state true

	writeJSONFrame(t, pub, liveshare.PublisherControl{Type: liveshare.CtrlTalkMuteAll})

	revoked := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgTalkRevoked, revoked.Type)
	assert.Equal(t, liveshare.ReasonMuted, revoked.Reason)
}

func TestLiveWS_PublisherGoneOnViewer(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialWS(t, srv, "/api/live-share/publish/ws?token="+token)
	view := dialWS(t, srv, "/api/share/"+token+"/signal")

	_ = readViewerMsg(t, view)
	_ = readPublisherMsg(t, pub)

	require.NoError(t, pub.Close(websocket.StatusNormalClosure, "bye"))

	msg := readViewerMsg(t, view)
	assert.Equal(t, liveshare.MsgPublisherGone, msg.Type)
}

func TestLiveWS_EndControlClosesRoom(t *testing.T) {
	srv, token := liveWSServer(t)

	pub := dialWS(t, srv, "/api/live-share/publish/ws?token="+token)
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
