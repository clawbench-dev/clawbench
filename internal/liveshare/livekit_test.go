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
