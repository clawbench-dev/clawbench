package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/store"
)

// Relay configuration storage.
//
// The failures worth guarding are silent: a save that blanks the credential it
// never received, or a half-filled pair that is accepted and then fails at the
// first meeting with a 401 that names no field.

func withLiveSFUDB(t *testing.T) {
	t.Helper()
	db, err := InitInMemoryDB()
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
}

func TestLiveSFUConfig_EmptyByDefault(t *testing.T) {
	withLiveSFUDB(t)

	cfg, err := GetLiveSFUConfig()
	require.NoError(t, err)
	assert.False(t, cfg.Configured(), "a fresh install must not look configured")
	assert.Equal(t, "", cfg.URL)
}

func TestLiveSFUConfig_SaveAndRead(t *testing.T) {
	withLiveSFUDB(t)

	key, secret := "lk-key", "lk-secret"
	saved, err := SaveLiveSFUConfig(LiveSFUPatch{
		URL:       "wss://livekit.example.internal:7880",
		APIKey:    &key,
		APISecret: &secret,
	})
	require.NoError(t, err)
	assert.True(t, saved.Configured())

	got, err := GetLiveSFUConfig()
	require.NoError(t, err)
	assert.Equal(t, "wss://livekit.example.internal:7880", got.URL)
	assert.Equal(t, "lk-key", got.APIKey)
	assert.Equal(t, "lk-secret", got.APISecret)
}

// The UI never receives the secret, so editing only the URL sends apiKey and
// apiSecret as absent. Treating that as "set to empty" would silently wipe a
// working credential and the next meeting would fail with no explanation.
func TestLiveSFUConfig_PatchWithoutCredentialKeepsIt(t *testing.T) {
	withLiveSFUDB(t)

	key, secret := "lk-key", "lk-secret"
	_, err := SaveLiveSFUConfig(LiveSFUPatch{URL: "ws://10.0.0.5:7880", APIKey: &key, APISecret: &secret})
	require.NoError(t, err)

	saved, err := SaveLiveSFUConfig(LiveSFUPatch{URL: "wss://relay.example.com"})
	require.NoError(t, err)
	assert.Equal(t, "wss://relay.example.com", saved.URL)
	assert.Equal(t, "lk-key", saved.APIKey, "a URL-only edit must not clear the key")
	assert.Equal(t, "lk-secret", saved.APISecret)
}

func TestLiveSFUConfig_ClearRemovesEverything(t *testing.T) {
	withLiveSFUDB(t)

	key, secret := "lk-key", "lk-secret"
	_, err := SaveLiveSFUConfig(LiveSFUPatch{URL: "ws://10.0.0.5:7880", APIKey: &key, APISecret: &secret})
	require.NoError(t, err)

	saved, err := SaveLiveSFUConfig(LiveSFUPatch{Clear: true})
	require.NoError(t, err)
	assert.False(t, saved.Configured())
	assert.Equal(t, "", saved.URL)
	assert.Equal(t, "", saved.APIKey)
}

func TestLiveSFUConfig_RejectsHalfACredential(t *testing.T) {
	withLiveSFUDB(t)

	key := "only-key"
	_, err := SaveLiveSFUConfig(LiveSFUPatch{URL: "ws://10.0.0.5:7880", APIKey: &key})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLiveSFUInvalid)
}

func TestLiveSFUConfig_RejectsNonWebSocketScheme(t *testing.T) {
	withLiveSFUDB(t)

	key, secret := "k", "s"
	_, err := SaveLiveSFUConfig(LiveSFUPatch{
		URL:       "https://livekit.example.com",
		APIKey:    &key,
		APISecret: &secret,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLiveSFUInvalid, "the browser connects with ws/wss, not http")
}

func TestLiveSFUConfig_APIBaseURLFollowsScheme(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"wss://relay.example.com:7880", "https://relay.example.com:7880"},
		{"ws://10.0.0.5:7880", "http://10.0.0.5:7880"},
		{"wss://relay.example.com/rtc", "https://relay.example.com"},
		{"", ""},
	}
	for _, c := range cases {
		got := LiveSFUConfig{URL: c.url}.APIBaseURL()
		assert.Equal(t, c.want, got, c.url)
	}
}
