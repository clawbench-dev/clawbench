package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServeConfigTest_STTCategory covers the "stt" branch of the
// ServeConfigTest switch, which dispatches to testSTT.
func TestServeConfigTest_STTCategory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/audio/transcriptions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"你好"}`))
	}))
	defer srv.Close()

	body := fmt.Sprintf(`{"category":"stt","values":{"stt.base_url":%q,"stt.model":"m"}}`, srv.URL)
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	ServeConfigTest(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var result ConnectivityTestResult
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.True(t, result.Success, result.Message)
	assert.Contains(t, result.Message, "你好")
}

// TestSTTConnectivity_EmptyBaseURL covers the missing base URL guard in testSTT.
func TestSTTConnectivity_EmptyBaseURL(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": "",
		"stt.model":    "m",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "required")
}

// TestSTTConnectivity_DefaultModel covers the default model fallback
// ("openai/whisper-large-v3") when no model is provided.
func TestSTTConnectivity_DefaultModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "openai/whisper-large-v3", r.FormValue("model"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"你好"}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": srv.URL,
	})
	assert.True(t, res.Success, res.Message)
}

// TestSTTConnectivity_WithAPIKey covers setting the Authorization header
// when an API key is configured.
func TestSTTConnectivity_WithAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"你好"}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": srv.URL,
		"stt.model":    "m",
		"stt.api_key":  "test-api-key",
	})
	assert.True(t, res.Success, res.Message)
}

// TestSTTConnectivity_Unreachable covers the client.Do network error path.
func TestSTTConnectivity_Unreachable(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": "http://127.0.0.1:19999",
		"stt.model":    "m",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "unreachable")
}

// TestSTTConnectivity_NewRequestError covers the invalid-URL branch: the probe
// must report it rather than panicking or reporting success.
func TestSTTConnectivity_NewRequestError(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": ":",
		"stt.model":    "m",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "create request")
}

// TestIsSTTModelNotFound covers both the true (keyword-matched) and false
// return paths of isSTTModelNotFound.
func TestIsSTTModelNotFound(t *testing.T) {
	trueCases := []string{
		`{"error":"model not found"}`,
		`{"error":"unknown model"}`,
		`{"error":"model does not exist"}`,
		`{"error":"model_not_found"}`,
		`{"error":"no such model"}`,
	}
	for _, tc := range trueCases {
		assert.True(t, isSTTModelNotFound([]byte(tc)), "expected true for %q", tc)
	}

	falseCases := []string{
		`{"error":"internal server error"}`,
		"hello world",
		`{"error":"invalid audio"}`,
		"",
	}
	for _, fc := range falseCases {
		assert.False(t, isSTTModelNotFound([]byte(fc)), "expected false for %q", fc)
	}
}

// ── Doubao (Volcano Engine Seed-ASR) branch ───────────────────

// withDoubaoTestServer points the doubao connectivity probe at srv and restores
// the production host afterwards.
func withDoubaoTestServer(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	prev := doubaoSTTBaseURL
	doubaoSTTBaseURL = srv.URL
	t.Cleanup(func() {
		doubaoSTTBaseURL = prev
		srv.Close()
	})
}

// TestSTTConnectivity_DoubaoSuccess asserts the probe really sends the embedded
// speech clip with the X-Api-Key / resource headers, and reports the recognized
// text. A silence-only probe would pass auth but prove nothing about ASR.
func TestSTTConnectivity_DoubaoSuccess(t *testing.T) {
	var gotKey, gotResource string
	var gotAudio []byte
	withDoubaoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Api-Key")
		gotResource = r.Header.Get("X-Api-Resource-Id")
		var body struct {
			Audio struct {
				Format string `json:"format"`
				Data   string `json:"data"`
			} `json:"audio"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "mp3", body.Audio.Format)
		decoded, err := base64.StdEncoding.DecodeString(body.Audio.Data)
		require.NoError(t, err)
		gotAudio = decoded
		_, _ = w.Write([]byte(`{"header":{"code":20000000,"message":"OK"},"result":{"text":"你好"}}`))
	})

	res := testSTT(context.Background(), map[string]any{
		"stt.provider":    "doubao",
		"stt.api_key":     "key-123",
		"stt.resource_id": "volc.seedasr.sauc.duration",
	})

	assert.True(t, res.Success, res.Message)
	assert.Contains(t, res.Message, "你好")
	assert.Equal(t, "key-123", gotKey)
	assert.Equal(t, "volc.seedasr.sauc.duration", gotResource)
	assert.Equal(t, sttProbeAudio, gotAudio, "probe must upload the real speech clip")
}

// TestSTTConnectivity_DoubaoRequiresAPIKey covers the missing-key guard.
func TestSTTConnectivity_DoubaoRequiresAPIKey(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.provider": "doubao",
		"stt.api_key":  "",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "API key")
}

// TestSTTConnectivity_DoubaoBodyErrorIsFailure covers the failure path: the API
// answers HTTP 200 with a non-success body code, which must not read as success.
func TestSTTConnectivity_DoubaoBodyErrorIsFailure(t *testing.T) {
	withDoubaoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"header":{"code":45000030,"message":"requested resource not granted"}}`))
	})

	res := testSTT(context.Background(), map[string]any{
		"stt.provider":    "doubao",
		"stt.api_key":     "key-123",
		"stt.resource_id": "volc.bigasr.auc",
	})

	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "45000030")
}

// TestSTTConnectivity_DoubaoEmptyTranscriptIsFailure covers a 200/OK response
// that carries no text — that is a broken probe, not a working engine.
func TestSTTConnectivity_DoubaoEmptyTranscriptIsFailure(t *testing.T) {
	withDoubaoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"header":{"code":20000000},"result":{"text":"  "}}`))
	})

	res := testSTT(context.Background(), map[string]any{
		"stt.provider": "doubao",
		"stt.api_key":  "key-123",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "empty")
}

// TestServeConfigTest_STTDoubaoDispatches covers provider dispatch through the
// HTTP entry point rather than calling testSTT directly.
func TestServeConfigTest_STTDoubaoDispatches(t *testing.T) {
	withDoubaoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"header":{"code":20000000},"result":{"text":"你好"}}`))
	})

	body := `{"category":"stt","values":{"stt.provider":"doubao","stt.api_key":"k"}}`
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	ServeConfigTest(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var result ConnectivityTestResult
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.True(t, result.Success, result.Message)
}
