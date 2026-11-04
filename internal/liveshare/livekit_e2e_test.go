package liveshare

// Contract tests against a REAL LiveKit relay.
//
// The unit tests in livekit_test.go assert which grant we put on the wire; these
// assert which grants LiveKit actually accepts. That distinction is the whole
// bug this file exists for: `roomAdmin` without a room looks right, signs
// correctly, and is answered with 401 "permissions denied" — the same reply a
// token that can only join gets, which is why the 测试连接 button blamed the key
// and secret for something that was never wrong with them.
//
// Opt-in, because they need a relay:
//
//	docker run -d --name lk-test -p 7880:7880 \
//	  -v /tmp/lk.yaml:/lk.yaml:ro livekit/livekit-server --config /lk.yaml
//	# /tmp/lk.yaml: port: 7880 / keys: {devkey: <32+ chars>}
//	LIVEKIT_TEST_URL=ws://127.0.0.1:7880 LIVEKIT_TEST_KEY=devkey \
//	LIVEKIT_TEST_SECRET=<same secret> go test ./internal/liveshare/ -run E2E -v

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func e2eConfig(t *testing.T) LiveKitConfig {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("LIVEKIT_TEST_URL"))
	if url == "" {
		t.Skip("set LIVEKIT_TEST_URL / LIVEKIT_TEST_KEY / LIVEKIT_TEST_SECRET to run against a real relay")
	}
	return LiveKitConfig{
		URL:       url,
		APIURL:    apiURLForTest(url),
		APIKey:    strings.TrimSpace(os.Getenv("LIVEKIT_TEST_KEY")),
		APISecret: strings.TrimSpace(os.Getenv("LIVEKIT_TEST_SECRET")),
	}
}

// apiURLForTest mirrors service.LiveSFUConfig.APIBaseURL, which this package
// cannot import (it would invert the dependency).
func apiURLForTest(wsURL string) string {
	switch {
	case strings.HasPrefix(wsURL, "wss://"):
		return "https://" + strings.TrimPrefix(wsURL, "wss://")
	case strings.HasPrefix(wsURL, "ws://"):
		return "http://" + strings.TrimPrefix(wsURL, "ws://")
	default:
		return wsURL
	}
}

// The fixed probe must pass on a real relay.
func TestE2E_ListRoomsSucceeds(t *testing.T) {
	n, err := e2eConfig(t).ListRooms(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, 0)
}

// The old token, replayed verbatim, must still be refused. If LiveKit ever
// starts accepting roomAdmin for ListRooms this test goes red — which is fine,
// it means the comment above the grant is stale and should be revisited.
func TestE2E_ListRoomsRejectsRoomAdminWithoutRoom(t *testing.T) {
	cfg := e2eConfig(t)
	token, err := cfg.sign(map[string]any{
		"sub":   cfg.APIKey,
		"video": LiveKitGrant{RoomAdmin: true},
	}, time.Minute)
	require.NoError(t, err)

	err = cfg.callWithToken(context.Background(), "ListRooms", token, struct{}{}, nil)
	require.Error(t, err)
	t.Logf("relay said: %v", err)
	assert.ErrorIs(t, err, ErrRelayRejected)
	assert.Contains(t, err.Error(), "permissions denied",
		"this is the exact reply the 测试连接 button used to surface")
}

// The room-scoped calls must keep working: roomAdmin + the room name is the
// right grant for them. A missing room is answered as "not found", not as a
// permission problem — that difference is the proof the grant was accepted.
func TestE2E_RoomScopedGrantIsAccepted(t *testing.T) {
	cfg := e2eConfig(t)

	for name, call := range map[string]func() error{
		"UpdateParticipant": func() error {
			return cfg.UpdateParticipantPermission(context.Background(), "e2e-no-such-room", "nobody", true)
		},
		"RemoveParticipant": func() error {
			return cfg.RemoveParticipant(context.Background(), "e2e-no-such-room", "nobody")
		},
	} {
		err := call()
		require.Error(t, err, name)
		t.Logf("%s relay said: %v", name, err)
		assert.NotContains(t, err.Error(), "permissions denied",
			"%s: roomAdmin + room must authorize room-scoped calls", name)
	}
}
