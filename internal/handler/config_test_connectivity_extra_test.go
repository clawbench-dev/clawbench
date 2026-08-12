package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestServeConfigTest_STTCategory covers the "stt" branch of the
// ServeConfigTest switch, which dispatches to testSTT.
func TestServeConfigTest_STTCategory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/audio/transcriptions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":""}`))
	}))
	defer srv.Close()

	body := fmt.Sprintf(`{"category":"stt","values":{"stt.base_url":%q,"stt.model":"m"}}`, srv.URL)
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	ServeConfigTest(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var result ConnectivityTestResult
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.True(t, result.Success)
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
		_, _ = w.Write([]byte(`{"text":""}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": srv.URL,
	})
	assert.True(t, res.Success)
}

// TestSTTConnectivity_WithAPIKey covers setting the Authorization header
// when an API key is configured.
func TestSTTConnectivity_WithAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":""}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": srv.URL,
		"stt.model":    "m",
		"stt.api_key":  "test-api-key",
	})
	assert.True(t, res.Success)
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

// TestSTTConnectivity_NewRequestError covers the http.NewRequest error branch
// by providing a base URL that produces an invalid URL.
func TestSTTConnectivity_NewRequestError(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": ":",
		"stt.model":    "m",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "Failed to create request")
}

// TestSTTConnectivity_ProbeOtherStatus covers the fall-through success result
// when the server returns a non-2xx status that is neither auth, 404, nor
// model-not-found (i.e. the request reached the transcription handler).
func TestSTTConnectivity_ProbeOtherStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal server error"}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": srv.URL,
		"stt.model":    "m",
	})
	assert.True(t, res.Success)
	assert.Contains(t, res.Message, "probe returned HTTP 500")
}

// TestBuildSTTProbe verifies the multipart probe body contains the expected
// fields and a valid Content-Type, and returns no error.
func TestBuildSTTProbe(t *testing.T) {
	body, contentType, err := buildSTTProbe("Qwen3-ASR-1.7B", "zh")
	assert.NoError(t, err)
	assert.NotEmpty(t, contentType)
	assert.True(t, strings.HasPrefix(contentType, "multipart/form-data"))

	// Parse the multipart body to verify fields.
	mr := multipart.NewReader(body, strings.TrimPrefix(contentType, "multipart/form-data; boundary="))
	form, err := mr.ReadForm(1 << 20)
	assert.NoError(t, err)
	defer func() { _ = form.RemoveAll() }()

	assert.Len(t, form.File["file"], 1, "probe should include one audio file part")
	assert.Equal(t, []string{"Qwen3-ASR-1.7B"}, form.Value["model"])
	assert.Equal(t, []string{"zh"}, form.Value["language"])
}

// TestBuildSTTProbe_NoLanguage verifies that omitting language skips the
// language field entirely.
func TestBuildSTTProbe_NoLanguage(t *testing.T) {
	body, contentType, err := buildSTTProbe("m", "")
	assert.NoError(t, err)

	mr := multipart.NewReader(body, strings.TrimPrefix(contentType, "multipart/form-data; boundary="))
	form, err := mr.ReadForm(1 << 20)
	assert.NoError(t, err)
	defer func() { _ = form.RemoveAll() }()

	_, ok := form.Value["language"]
	assert.False(t, ok, "language field should be absent when empty")
	assert.Equal(t, []string{"m"}, form.Value["model"])
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
