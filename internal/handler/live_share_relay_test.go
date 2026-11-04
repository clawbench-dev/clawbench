package handler

// Relay (LiveKit) enforcement tests.
//
// Two things must never regress: a p2p room never talks to a relay, and every
// relayed permission change actually reaches LiveKit with the identity the join
// token signed — a wrong identity is a 404 nobody sees, and the host's "muted"
// would be a lie.
//
// The fake relay below is LiveKit's Twirp API: it records what was asked and
// answers 200, or whatever status the test set.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/liveshare"
	"clawbench/internal/service"
)

// relayCall is one control-plane call the fake relay received.
type relayCall struct {
	method     string
	room       string
	identity   string
	canPublish bool
}

type fakeRelay struct {
	mu     sync.Mutex
	calls  []relayCall
	status int
}

func (f *fakeRelay) snapshot() []relayCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]relayCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeRelay) setStatus(code int) {
	f.mu.Lock()
	f.status = code
	f.mu.Unlock()
}

// newFakeRelay starts the fake and returns its base URL (http://127.0.0.1:port).
func newFakeRelay(t *testing.T) (*fakeRelay, string) {
	t.Helper()
	f := &fakeRelay{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var body struct {
			Room       string `json:"room"`
			Identity   string `json:"identity"`
			Permission *struct {
				CanPublish bool `json:"canPublish"`
			} `json:"permission"`
		}
		_ = json.Unmarshal(raw, &body)

		f.mu.Lock()
		status := f.status
		f.calls = append(f.calls, relayCall{
			method:     strings.TrimPrefix(r.URL.Path, "/twirp/livekit.RoomService/"),
			room:       body.Room,
			identity:   body.Identity,
			canPublish: body.Permission != nil && body.Permission.CanPublish,
		})
		f.mu.Unlock()

		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"code":"internal","msg":"boom"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return f, srv.URL
}

// newRelayRoom creates a room whose snapshot points at the fake relay.
func newRelayRoom(t *testing.T, apiURL string) *liveshare.Room {
	t.Helper()
	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/relay"), "Relay test")
	require.NoError(t, err)
	cfg := liveshare.LiveKitConfig{
		URL:       "ws://127.0.0.1:7880",
		APIURL:    apiURL,
		APIKey:    "lk-key",
		APISecret: "lk-secret",
	}
	require.True(t, room.SetMode(liveshare.ModeSFU, cfg, liveshare.DefaultMaxViewers))
	require.True(t, room.UseRelay(), "the room must really be in relay mode")
	return room
}

func saveRelayConfig(t *testing.T, relayURL string) {
	t.Helper()
	key, secret := "lk-key", "lk-secret"
	_, err := service.SaveLiveSFUConfig(service.LiveSFUPatch{URL: relayURL, APIKey: &key, APISecret: &secret})
	require.NoError(t, err)
}

// A p2p room must not make a single relay call. The installation config is
// pointed at a REACHABLE relay on purpose: if any of these paths ever consults
// the global config instead of the room's snapshot, the fake records it.
func TestLiveShareRelay_P2PRoomNeverTouchesTheRelay(t *testing.T) {
	withLiveSFUTestDB(t)
	f, apiURL := newFakeRelay(t)
	saveRelayConfig(t, "ws"+strings.TrimPrefix(apiURL, "http"))

	resetLiveRegistry(t)
	room, err := liveRegistry.Create(projPath("/proj/p2p"), "P2P")
	require.NoError(t, err)
	require.False(t, room.UseRelay())
	bob, err := room.AddViewer("Bob", &recordingSink{})
	require.NoError(t, err)

	pair := newWSPair(t)
	applySetTalkEnabled(room, true)
	applyTalkMute(room, pair.sink, liveshare.PublisherControl{
		Type: liveshare.CtrlTalkMute, ViewerID: bob.ID, Enabled: true,
	})
	applyTalkMuteAll(room)
	handlePublisherControl(room, pair.sink, liveshare.PublisherControl{
		Type: liveshare.CtrlKick, ViewerID: bob.ID,
	})

	assert.Empty(t, f.snapshot(), "p2p mode must not reach a relay")
}

// Muting one viewer revokes exactly that participant, and unmuting restores it.
func TestLiveShareRelay_MuteRevokesPublishForThatViewer(t *testing.T) {
	f, apiURL := newFakeRelay(t)
	room := newRelayRoom(t, apiURL)
	room.SetTalkEnabled(true)

	bob, err := room.AddViewer("Bob", &recordingSink{})
	require.NoError(t, err)
	alice, err := room.AddViewer("Alice", &recordingSink{})
	require.NoError(t, err)
	pair := newWSPair(t)

	handlePublisherControl(room, pair.sink, liveshare.PublisherControl{
		Type: liveshare.CtrlTalkMute, ViewerID: bob.ID, Enabled: true,
	})

	calls := f.snapshot()
	require.Len(t, calls, 1, "muting one viewer must touch exactly that participant")
	assert.Equal(t, "UpdateParticipant", calls[0].method)
	assert.Equal(t, room.Token(), calls[0].room, "the relay room is the share token")
	assert.Equal(t, "viewer:"+bob.ID, calls[0].identity)
	assert.NotEqual(t, "viewer:"+alice.ID, calls[0].identity, "the wrong viewer must not be silenced")
	assert.False(t, calls[0].canPublish)

	handlePublisherControl(room, pair.sink, liveshare.PublisherControl{
		Type: liveshare.CtrlTalkMute, ViewerID: bob.ID, Enabled: false,
	})
	calls = f.snapshot()
	require.Len(t, calls, 2)
	assert.True(t, calls[1].canPublish,
		"unmuting restores the permission; the viewer's own microphone switch is still off")
}

// Turning the room switch off revokes everybody; turning it back on must not
// resurrect a viewer the host muted individually.
func TestLiveShareRelay_RoomSwitchRevokesEveryViewer(t *testing.T) {
	f, apiURL := newFakeRelay(t)
	room := newRelayRoom(t, apiURL)
	room.SetTalkEnabled(true)

	bob, err := room.AddViewer("Bob", &recordingSink{})
	require.NoError(t, err)
	alice, err := room.AddViewer("Alice", &recordingSink{})
	require.NoError(t, err)
	_, err = room.SetMuted(bob.ID, true)
	require.NoError(t, err)

	applySetTalkEnabled(room, false)
	byIdentity := map[string]bool{}
	calls := f.snapshot()
	require.Len(t, calls, 2, "every viewer must be revoked, not only the ones with a microphone on")
	for _, c := range calls {
		byIdentity[c.identity] = c.canPublish
	}
	assert.Equal(t, map[string]bool{
		"viewer:" + bob.ID:   false,
		"viewer:" + alice.ID: false,
	}, byIdentity)

	applySetTalkEnabled(room, true)
	calls = f.snapshot()
	require.Len(t, calls, 4)
	byIdentity = map[string]bool{}
	for _, c := range calls[2:] {
		byIdentity[c.identity] = c.canPublish
	}
	assert.Equal(t, map[string]bool{
		"viewer:" + bob.ID:   false, // still blocked by the per-viewer mute
		"viewer:" + alice.ID: true,
	}, byIdentity)
}

// A kick hangs up the relay connection too, or the browser keeps publishing to
// a meeting it was removed from.
func TestLiveShareRelay_KickRemovesParticipant(t *testing.T) {
	f, apiURL := newFakeRelay(t)
	room := newRelayRoom(t, apiURL)
	bob, err := room.AddViewer("Bob", &recordingSink{})
	require.NoError(t, err)
	pair := newWSPair(t)

	closed := handlePublisherControl(room, pair.sink, liveshare.PublisherControl{
		Type: liveshare.CtrlKick, ViewerID: bob.ID,
	})
	assert.False(t, closed, "a kick removes one viewer, it does not end the host's socket")

	calls := f.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "RemoveParticipant", calls[0].method)
	assert.Equal(t, "viewer:"+bob.ID, calls[0].identity)
	assert.Equal(t, room.Token(), calls[0].room)
}

// A relay that answers 500 must not take the control path down with it: the
// room state and the frames are what the UI renders, and they are unaffected.
func TestLiveShareRelay_FailureLeavesTheControlPathWorking(t *testing.T) {
	f, apiURL := newFakeRelay(t)
	f.setStatus(http.StatusInternalServerError)
	room := newRelayRoom(t, apiURL)
	room.SetTalkEnabled(true)
	bob, err := room.AddViewer("Bob", &recordingSink{})
	require.NoError(t, err)
	pair := newWSPair(t)
	// The publisher socket is what notifyTalkState writes to; without it the
	// assertion below would pass on a room that simply has nobody to tell.
	room.AttachPublisher(pair.sink)

	closed := handlePublisherControl(room, pair.sink, liveshare.PublisherControl{
		Type: liveshare.CtrlTalkMute, ViewerID: bob.ID, Enabled: true,
	})
	assert.False(t, closed)
	require.Len(t, f.snapshot(), 1, "the call must have been attempted")

	assert.True(t, bob.Muted(), "the room is still authoritative")
	assert.False(t, bob.Talking())

	frames := pair.waitFrames(t, 1)
	var state liveshare.PublisherMessage
	require.NoError(t, json.Unmarshal(frames[0], &state))
	assert.Equal(t, liveshare.MsgTalkState, state.Type)
	require.NotNil(t, state.Muted)
	assert.True(t, *state.Muted)
}

// The permission call addresses the participant by the identity the join token
// signed. If the two ever drift apart every call is a 404 the host never sees.
func TestLiveShareRelay_IdentityMatchesJoinToken(t *testing.T) {
	_, apiURL := newFakeRelay(t)
	room := newRelayRoom(t, apiURL)
	bob, err := room.AddViewer("Bob", &recordingSink{})
	require.NoError(t, err)

	msg, err := joinedViewerMessage(room, bob)
	require.NoError(t, err)
	require.NotNil(t, msg.Relay)

	parts := strings.Split(msg.Relay.Token, ".")
	require.Len(t, parts, 3, "an HS256 JWT has three segments")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims struct {
		Sub string `json:"sub"`
	}
	require.NoError(t, json.Unmarshal(payload, &claims))
	assert.Equal(t, relayViewerIdentity(bob.ID), claims.Sub)
}

// --- signaling proxy ---

func relayPort(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	require.NotEmpty(t, u.Port())
	return u.Port()
}

// /livekit/rtc must arrive at the relay as /rtc: LiveKit's signaling endpoint
// lives at the root, so a forwarded prefix would 404 and the page would sit on
// "connecting" forever.
func TestServeLiveKitProxy_RewritesPrefixToRelay(t *testing.T) {
	withLiveSFUTestDB(t)

	var gotPath, gotQuery, gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotHost = r.URL.Path, r.URL.RawQuery, r.Host
		_, _ = w.Write([]byte("relay"))
	}))
	t.Cleanup(upstream.Close)
	port := relayPort(t, upstream.URL)
	saveRelayConfig(t, "ws://127.0.0.1:"+port)

	front := httptest.NewServer(http.HandlerFunc(ServeLiveKitProxy))
	t.Cleanup(front.Close)

	resp, err := http.Get(front.URL + "/livekit/rtc?access_token=abc")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "relay", string(body))
	assert.Equal(t, "/rtc", gotPath)
	assert.Equal(t, "access_token=abc", gotQuery)
	assert.Equal(t, "127.0.0.1:"+port, gotHost, "the request must be addressed to the relay, not to the page's origin")
}

// The proxy dials exactly the configured relay — a remote one is the normal
// deployment now. Loopback names are normalized to 127.0.0.1 so a config edit
// cannot point this endpoint at another local service.
func TestLiveKitRelayTarget_UsesConfiguredHost(t *testing.T) {
	withLiveSFUTestDB(t)

	for _, c := range []struct{ url, want string }{
		{"ws://relay.example.com:7880", "http://relay.example.com:7880"},
		{"ws://192.168.1.10:7880", "http://192.168.1.10:7880"},
		{"ws://localhost:7880", "http://127.0.0.1:7880"},
		{"ws://127.0.0.1:7880", "http://127.0.0.1:7880"},
		{"wss://relay.example.com", "https://relay.example.com:443"},
	} {
		saveRelayConfig(t, c.url)
		target, ok := liveKitRelayTarget()
		require.True(t, ok, c.url)
		assert.Equal(t, c.want, target.String(), c.url)
	}
}

// A non-loopback relay is no longer rejected. The address here refuses the
// connection, which must read as 502 (tried and failed) rather than 404 (no
// proxy on this deployment).
func TestServeLiveKitProxy_ForwardsNonLoopbackRelay(t *testing.T) {
	withLiveSFUTestDB(t)
	saveRelayConfig(t, "ws://127.0.0.2:1")

	front := httptest.NewServer(http.HandlerFunc(ServeLiveKitProxy))
	t.Cleanup(front.Close)

	resp, err := http.Get(front.URL + "/livekit/rtc")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}

func TestServeLiveKitProxy_NotConfiguredIs404(t *testing.T) {
	withLiveSFUTestDB(t)

	front := httptest.NewServer(http.HandlerFunc(ServeLiveKitProxy))
	t.Cleanup(front.Close)

	resp, err := http.Get(front.URL + "/livekit/rtc")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// The proxy's whole reason to exist is the signaling socket, so an upgrade
// must survive it: a proxy that answers the handshake but drops the stream
// would look connected and then carry nothing.
func TestServeLiveKitProxy_ForwardsWebSocketUpgrade(t *testing.T) {
	withLiveSFUTestDB(t)

	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			_, data, rerr := conn.Read(r.Context())
			if rerr != nil {
				return
			}
			if werr := conn.Write(r.Context(), websocket.MessageText, data); werr != nil {
				return
			}
		}
	}))
	t.Cleanup(upstream.Close)
	saveRelayConfig(t, "ws://127.0.0.1:"+relayPort(t, upstream.URL))

	front := httptest.NewServer(http.HandlerFunc(ServeLiveKitProxy))
	t.Cleanup(front.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(front.URL, "http")+"/livekit/rtc", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })

	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte("hello")))
	_, data, err := conn.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
	assert.Equal(t, "/rtc", gotPath)
}

func TestLiveKitUpstreamPath(t *testing.T) {
	for in, want := range map[string]string{
		"/livekit/rtc":          "/rtc",
		"/livekit/rtc/validate": "/rtc/validate",
		"/livekit/":             "/",
		"/livekit":              "/",
	} {
		assert.Equal(t, want, liveKitUpstreamPath(in), "path %q", in)
	}
}
