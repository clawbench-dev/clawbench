package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/service"
	"clawbench/internal/store"
)

// Relay configuration endpoints.
//
// The two things that must never regress: the secret never appears in a
// response, and "save only the URL" does not wipe the stored credential.

func withLiveSFUTestDB(t *testing.T) {
	t.Helper()
	db, err := service.InitInMemoryDB()
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
}

func doLiveSFU(t *testing.T, h http.HandlerFunc, method, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, "/api/live-share/sfu", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestServeLiveSFUConfig_EmptyThenSaved(t *testing.T) {
	withLiveSFUTestDB(t)

	rec, got := doLiveSFU(t, ServeLiveSFUConfig, http.MethodGet, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, false, got["configured"])
	assert.Equal(t, false, got["hasSecret"])

	rec, got = doLiveSFU(t, ServeLiveSFUConfig, http.MethodPatch,
		`{"url":"wss://relay.example.com:7880","apiKey":"lk-key","apiSecret":"lk-secret"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, true, got["configured"])
	assert.Equal(t, true, got["hasSecret"])
	assert.Equal(t, "wss://relay.example.com:7880", got["url"])

	// The secret is write-only: it must not appear under any key.
	_, raw := doLiveSFU(t, ServeLiveSFUConfig, http.MethodGet, "")
	for k, v := range raw {
		if s, ok := v.(string); ok {
			assert.NotEqual(t, "lk-secret", s, "secret leaked in field %q", k)
		}
	}
	assert.NotContains(t, rec.Body.String(), "lk-secret")
}

func TestServeLiveSFUConfig_URLOnlyEditKeepsSecret(t *testing.T) {
	withLiveSFUTestDB(t)

	_, _ = doLiveSFU(t, ServeLiveSFUConfig, http.MethodPatch,
		`{"url":"ws://10.0.0.5:7880","apiKey":"lk-key","apiSecret":"lk-secret"}`)

	rec, got := doLiveSFU(t, ServeLiveSFUConfig, http.MethodPatch, `{"url":"wss://relay.example.com"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, true, got["hasSecret"], "the UI never receives the secret, so it cannot resend it")
	assert.Equal(t, true, got["configured"])
}

func TestServeLiveSFUConfig_RejectsBadScheme(t *testing.T) {
	withLiveSFUTestDB(t)

	rec, _ := doLiveSFU(t, ServeLiveSFUConfig, http.MethodPatch,
		`{"url":"https://relay.example.com","apiKey":"k","apiSecret":"s"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServeLiveSFUTest_RequiresAllFields(t *testing.T) {
	withLiveSFUTestDB(t)

	rec, got := doLiveSFU(t, ServeLiveSFUTest, http.MethodPost, `{"url":"ws://127.0.0.1:7880"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, false, got["success"])
	assert.Contains(t, got["message"], "API Key")
}

func TestServeLiveSFUTest_ProbesWithCredentials(t *testing.T) {
	withLiveSFUTestDB(t)

	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"rooms":[]}`))
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	rec, got := doLiveSFU(t, ServeLiveSFUTest, http.MethodPost,
		`{"url":"`+wsURL+`","apiKey":"lk-key","apiSecret":"lk-secret"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, true, got["success"], got["message"])
	// A token was actually presented — the probe is not a bare reachability check.
	assert.True(t, strings.HasPrefix(auth, "Bearer eyJ"), auth)
}

func TestServeLiveSFUTest_ReportsRefusal(t *testing.T) {
	withLiveSFUTestDB(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthenticated","msg":"invalid api key"}`))
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	_, got := doLiveSFU(t, ServeLiveSFUTest, http.MethodPost,
		`{"url":"`+wsURL+`","apiKey":"bad","apiSecret":"bad"}`)
	assert.Equal(t, false, got["success"])
	assert.Contains(t, got["message"], "密钥")
}
