package liveshare

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The LiveKit control plane.
//
// Everything here is a silent failure if it is wrong: a token with the wrong
// grant connects but cannot speak, a permission update that drops canSubscribe
// makes a participant deaf, and a probe that passes with a bad key only fails
// at the first meeting.

func testConfig(apiURL string) LiveKitConfig {
	return LiveKitConfig{
		URL:       "ws://127.0.0.1:7880",
		APIURL:    apiURL,
		APIKey:    "lk-key",
		APISecret: "lk-secret",
	}
}

// decodeSegment base64url-decodes one JWT segment.
func decodeSegment(t *testing.T, seg string) map[string]any {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestJoinToken_CarriesRoomAndPublishGrant(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:7880")

	token, err := cfg.JoinToken("room-1", "viewer:3", "张三", true, time.Hour)
	require.NoError(t, err)

	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)

	header := decodeSegment(t, parts[0])
	assert.Equal(t, "HS256", header["alg"])

	claims := decodeSegment(t, parts[1])
	assert.Equal(t, "lk-key", claims["iss"])
	assert.Equal(t, "viewer:3", claims["sub"])
	assert.Equal(t, "张三", claims["name"])

	video, ok := claims["video"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "room-1", video["room"])
	assert.Equal(t, true, video["roomJoin"])
	assert.Equal(t, true, video["canSubscribe"])
	assert.Equal(t, true, video["canPublish"])

	// The signature must verify with the configured secret — otherwise the token
	// looks right and LiveKit rejects it.
	mac := hmac.New(sha256.New, []byte("lk-secret"))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	assert.Equal(t, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), parts[2])
}

// A token is only accepted if the relay considers it valid *now*, and the two
// clocks are never exactly equal. nbf has to sit far enough in the past that a
// local clock running a little fast still produces an already-valid token;
// exp still bounds when a leaked link stops working.
func TestJoinToken_BackdatesNotBeforeToAbsorbClockSkew(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:7880")
	const ttl = time.Hour

	before := time.Now()
	token, err := cfg.JoinToken("room-1", "viewer:5", "", false, ttl)
	require.NoError(t, err)
	after := time.Now()

	claims := decodeSegment(t, strings.Split(token, ".")[1])
	nbf := int64(claims["nbf"].(float64))
	exp := int64(claims["exp"].(float64))

	// At least five minutes of slack, regardless of how long signing took.
	assert.LessOrEqual(t, nbf, before.Add(-5*time.Minute).Unix(),
		"nbf must be backdated by at least five minutes")
	assert.GreaterOrEqual(t, nbf, after.Add(-6*time.Minute).Unix(),
		"nbf must not be backdated further than the skew budget")

	assert.GreaterOrEqual(t, exp, before.Add(ttl).Unix())
	assert.LessOrEqual(t, exp, after.Add(ttl).Unix())
	assert.Greater(t, exp, nbf)
}

func TestJoinToken_RefusesWithoutConfiguration(t *testing.T) {
	_, err := LiveKitConfig{}.JoinToken("r", "i", "n", false, time.Hour)
	require.Error(t, err)
}

func TestJoinToken_AudienceCannotPublish(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:7880")
	token, err := cfg.JoinToken("room-1", "viewer:4", "", false, time.Hour)
	require.NoError(t, err)

	video := decodeSegment(t, strings.Split(token, ".")[1])["video"].(map[string]any)
	assert.Nil(t, video["canPublish"], "false is omitted, and that must mean 'cannot publish'")
}

func TestListRooms_CountsRooms(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/twirp/livekit.RoomService/ListRooms", r.URL.Path)
		assert.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "))
		_, _ = io.WriteString(w, `{"rooms":[{"name":"a"},{"name":"b"}]}`)
	}))
	defer srv.Close()

	n, err := testConfig(srv.URL).ListRooms(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

// bearerVideoGrant returns the `video` claim of the token on the request.
func bearerVideoGrant(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	video, ok := decodeSegment(t, parts[1])["video"].(map[string]any)
	require.True(t, ok, "server calls must carry a video grant")
	return video
}

// ListRooms is an ACCOUNT-wide call. LiveKit scopes roomAdmin to the room named
// in the grant, so roomAdmin alone (no room) authorizes nothing and the call
// comes back 401 "permissions denied" — indistinguishable from a token that can
// only join, which is exactly what the 测试连接 button used to report while the
// key and secret were both correct.
func TestListRooms_TokenCarriesRoomListGrant(t *testing.T) {
	var video map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		video = bearerVideoGrant(t, r)
		_, _ = io.WriteString(w, `{"rooms":[]}`)
	}))
	defer srv.Close()

	_, err := testConfig(srv.URL).ListRooms(context.Background())
	require.NoError(t, err)

	assert.Equal(t, true, video["roomList"], "ListRooms is rejected without this grant")
	assert.Nil(t, video["roomAdmin"], "roomAdmin without a room grants nothing, so it must not be sent alone")
}

// The room-scoped calls are the mirror image: roomAdmin + the room name, and no
// account-wide grant.
func TestRoomScopedCalls_TokenCarriesRoomAdminGrant(t *testing.T) {
	var video map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		video = bearerVideoGrant(t, r)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	require.NoError(t, testConfig(srv.URL).RemoveParticipant(context.Background(), "room-1", "viewer:9"))

	assert.Equal(t, "room-1", video["room"], "roomAdmin is scoped to a room, so the room must travel with it")
	assert.Equal(t, true, video["roomAdmin"])
	assert.Nil(t, video["roomList"], "a per-room call must not be handed account-wide rights")
}

// A reachable server that refuses is a DIFFERENT failure from an unreachable
// one, and the user's fix is different too (credentials vs address/firewall).
func TestListRooms_DistinguishesRefusalFromUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"code":"unauthenticated","msg":"invalid api key"}`)
	}))
	defer srv.Close()

	_, err := testConfig(srv.URL).ListRooms(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRelayRejected)
	assert.Contains(t, err.Error(), "invalid api key")
}

func TestListRooms_Unreachable(t *testing.T) {
	_, err := testConfig("http://127.0.0.1:1").ListRooms(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRelayUnreachable)
}

// UpdateParticipant REPLACES the permission. Sending only canPublish would make
// the participant deaf, so canSubscribe has to be restated on every call.
func TestUpdateParticipantPermission_RestatesCanSubscribe(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/twirp/livekit.RoomService/UpdateParticipant", r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &body))
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	err := testConfig(srv.URL).UpdateParticipantPermission(context.Background(), "room-1", "viewer:3", true)
	require.NoError(t, err)

	assert.Equal(t, "room-1", body["room"])
	assert.Equal(t, "viewer:3", body["identity"])
	perm, ok := body["permission"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, perm["canSubscribe"], "dropping this makes the participant deaf")
	assert.Equal(t, true, perm["canPublish"])
}

func TestRemoveParticipant_SendsIdentity(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/twirp/livekit.RoomService/RemoveParticipant", r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &body))
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	require.NoError(t, testConfig(srv.URL).RemoveParticipant(context.Background(), "room-1", "viewer:9"))
	assert.Equal(t, "room-1", body["room"])
	assert.Equal(t, "viewer:9", body["identity"])
}
