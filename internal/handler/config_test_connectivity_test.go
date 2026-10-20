package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/ssh"
	"clawbench/internal/summarize"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeConfigTest_InvalidBody(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader("not json"))
	ServeConfigTest(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfigTest_MissingFields(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(`{}`))
	ServeConfigTest(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfigTest_UnknownCategory(t *testing.T) {
	w := httptest.NewRecorder()
	body := `{"category":"unknown","values":{}}`
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	ServeConfigTest(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var result ConnectivityTestResult
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.False(t, result.Success)
}

func TestSTTConnectivity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("path = %q, want /v1/audio/transcriptions", r.URL.Path)
		}
		if got := r.FormValue("model"); got != "Qwen3-ASR-1.7B" {
			t.Fatalf("model = %q, want Qwen3-ASR-1.7B", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":""}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": srv.URL,
		"stt.model":    "Qwen3-ASR-1.7B",
	})
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
}

func TestSTTConnectivity_AuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": srv.URL,
		"stt.model":    "m",
	})
	if res.Success {
		t.Fatalf("expected failure for auth, got %+v", res)
	}
}

func TestSTTConnectivity_ModelNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"model Qwen3-ASR-1.7B does not exist"}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": srv.URL,
		"stt.model":    "Qwen3-ASR-1.7B",
	})
	if res.Success {
		t.Fatalf("expected failure for unknown model, got %+v", res)
	}
}

func TestSTTConnectivity_Endpoint404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": srv.URL,
		"stt.model":    "m",
	})
	if res.Success {
		t.Fatalf("expected failure for 404, got %+v", res)
	}
}

// TestSTTProbeEmbeddedAudio verifies the STT connectivity probe uses the
// embedded "你好" mp3 asset (not the old silence WAV).
func TestSTTProbeEmbeddedAudio(t *testing.T) {
	if len(sttProbeAudio) == 0 {
		t.Fatal("sttProbeAudio must embed the mp3 probe asset")
	}
	// MP3 payloads produced by MiniMax start with an ID3 tag.
	if len(sttProbeAudio) < 3 || string(sttProbeAudio[0:3]) != "ID3" {
		t.Fatalf("embedded probe does not look like an mp3 (header: %q)", sttProbeAudio[:3])
	}
}

func TestServeConfigTest_MethodNotAllowed(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/config/test", http.NoBody)
	ServeConfigTest(w, r)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestTestSummarizeVoice_AnthropicSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}))
	defer srv.Close()

	// Use /v1/messages suffix so auto-detection identifies this as Anthropic format
	result := testSummarizeVoice(context.Background(), map[string]any{
		"ai_summary.api.base_url": srv.URL + "/v1/messages",
		"ai_summary.api.key":      "test-key",
		"ai_summary.model":        "claude-3-haiku",
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "anthropic")
}

func TestTestSummarizeVoice_NotTTSBackend(t *testing.T) {
	// The test is decoupled from the voice-summary TTS backend: even with
	// summarize.tts_backend set to a non-API value, the shared AI summary API
	// must still be probed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	result := testSummarizeVoice(context.Background(), map[string]any{
		"summarize.tts_backend":   "simple",
		"ai_summary.api.base_url": srv.URL,
		"ai_summary.api.key":      "test-key",
		"ai_summary.model":        "gpt-4o-mini",
	})
	assert.True(t, result.Success)
}

// ── RAG tests ────────────────────────────────────────────────

func TestIsAnthropicURL(t *testing.T) {
	assert.True(t, summarize.IsAnthropicURL("https://api.anthropic.com"))
	assert.True(t, summarize.IsAnthropicURL("https://api.anthropic.com/v1/messages"))
	assert.True(t, summarize.IsAnthropicURL("http://localhost:8080/v1/messages"))
	assert.False(t, summarize.IsAnthropicURL("https://api.openai.com"))
	assert.False(t, summarize.IsAnthropicURL("https://api.openai.com/v1/chat/completions"))
	assert.False(t, summarize.IsAnthropicURL("http://localhost:11434"))
}

func TestTestRAG_EmptyURL(t *testing.T) {
	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": "",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "required")
}

func TestTestRAG_InvalidScheme(t *testing.T) {
	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": "ftp://host:21",
		"rag.model":    "bge-m3",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "scheme")
}

func TestTestRAG_MissingSchemeNormalized(t *testing.T) {
	srv := ragTestServer(t, nil)
	defer srv.Close()

	// Strip the scheme to exercise missing-scheme auto-completion.
	host := strings.TrimPrefix(srv.URL, "http://")
	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": host,
		"rag.model":    "bge-m3",
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "succeeded")
}

// The connectivity check probes /v1/embeddings directly and must never
// precheck /v1/models: a gateway that doesn't implement the models list (or
// doesn't enumerate every model) still serves embeddings, and the old precheck
// reported it as broken. ragTestServer fails the test on any non-embeddings
// request, so this also proves no models probe is issued.
func TestTestRAG_ProbesEmbeddingsNotModels(t *testing.T) {
	srv := ragTestServer(t, nil)
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
		"rag.model":    "bge-m3",
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "succeeded")
}

// A gateway that answers /v1/models with 404 (e.g. some Ollama versions) or
// 403 (auth-scoped model listing) must not be reported as broken when
// embeddings work. This is the exact scenario the precheck got wrong.
func TestTestRAG_ModelsEndpoint404StillSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/models") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"data":[{"embedding":[0.1,0.2,0.3],"index":0}]}`)
	}))
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
		"rag.model":    "bge-m3",
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "succeeded")
}

func TestTestRAG_Unreachable(t *testing.T) {
	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": "http://127.0.0.1:19999",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "embedding failed")
}

// ragTestServer answers POST /v1/embeddings with a valid vector. embedHandler,
// when non-nil, runs for the embedding request so a test can assert its shape
// or return a custom response.
//
// A request to any other path fails the test: the RAG connectivity check must
// probe the real embeddings endpoint, never precheck /v1/models. Gateways
// (e.g. OneAPI) often don't list every model — or implement /v1/models at all —
// while serving embeddings fine, so a models precheck reports a working service
// as broken.
func ragTestServer(t *testing.T, embedHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" || strings.HasSuffix(r.URL.Path, "/v1/embeddings") {
			if embedHandler != nil {
				embedHandler(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, `{"data":[{"embedding":[0.1,0.2,0.3],"index":0}]}`)
			return
		}
		t.Errorf("unexpected request to %s: the RAG test must not precheck /v1/models", r.URL.Path)
		http.NotFound(w, r)
	}))
}

// ── DingTalk tests ───────────────────────────────────────────

func TestTestDingTalk_MissingFields(t *testing.T) {
	result := testDingTalk(context.Background(), map[string]any{
		"dingtalk.app_key":    "",
		"dingtalk.app_secret": "",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "required")
}

func TestTestDingTalk_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RawQuery, "appkey=test-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"errcode":0,"errmsg":"ok","access_token":"token123"}`)
	}))
	defer srv.Close()

	// Override the DingTalk URL for testing
	origURL := dingtalkTokenURL
	dingtalkTokenURL = srv.URL + "/gettoken"
	defer func() { dingtalkTokenURL = origURL }()

	result := testDingTalk(context.Background(), map[string]any{
		"dingtalk.app_key":    "test-key",
		"dingtalk.app_secret": "test-secret",
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "successful")
}

func TestTestDingTalk_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"errcode":40001,"errmsg":"invalid appkey"}`)
	}))
	defer srv.Close()

	origURL := dingtalkTokenURL
	dingtalkTokenURL = srv.URL + "/gettoken"
	defer func() { dingtalkTokenURL = origURL }()

	result := testDingTalk(context.Background(), map[string]any{
		"dingtalk.app_key":    "bad-key",
		"dingtalk.app_secret": "bad-secret",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "invalid appkey")
}

// ── Port Forward tests ───────────────────────────────────────

func TestTestPortForward_NoServer(t *testing.T) {
	origSSH := sshServerRef
	origRegistry := service.ProxyService
	sshServerRef = nil
	// Both wires must be gone for "not available": an h2-only install has no
	// SSH listener but still forwards ports, so clearing the SSH ref alone is
	// no longer enough to reach the failure branch.
	service.ProxyService = nil
	defer func() { sshServerRef = origSSH; service.ProxyService = origRegistry }()

	result := testPortForward(context.Background(), map[string]any{})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "not available")
}

// ── TTS tests ────────────────────────────────────────────────

func TestTestTTS_UnknownEngine(t *testing.T) {
	result := testTTS(context.Background(), map[string]any{
		"tts.engine": "unknown",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Unknown TTS engine")
}

func TestTestTTS_EdgeReachable(t *testing.T) {
	// This test makes a real network call — skip in short mode
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	result := testTTSEdge(context.Background(), map[string]any{})
	// We can't assert true/false since it depends on network, but it shouldn't panic
	_ = result
}

// ── resolveStringValue tests ─────────────────────────────────

func TestResolveStringValue(t *testing.T) {
	tests := []struct {
		name     string
		values   map[string]any
		key      string
		fallback string
		expected string
	}{
		{"value present", map[string]any{"key": "hello"}, "key", "fallback", "hello"},
		{"key missing", map[string]any{}, "key", "fallback", "fallback"},
		{"empty string falls back to config", map[string]any{"key": ""}, "key", "anthropic", "anthropic"},
		{"non-string value", map[string]any{"key": float64(42)}, "key", "fallback", "42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveStringValue(tt.values, tt.key, tt.fallback)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveIntValue(t *testing.T) {
	tests := []struct {
		name     string
		values   map[string]any
		key      string
		def      int
		expected int
	}{
		{"float64", map[string]any{"port": float64(8080)}, "port", 0, 8080},
		{"int", map[string]any{"port": 443}, "port", 0, 443},
		{"string", map[string]any{"port": "9090"}, "port", 0, 9090},
		{"missing", map[string]any{}, "port", 8080, 8080},
		{"invalid string", map[string]any{"port": "abc"}, "port", 8080, 8080},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveIntValue(tt.values, tt.key, tt.def)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ── ServeConfigTest integration tests ──────────────────────────

func TestServeConfigTest_TTSCategory(t *testing.T) {
	body := `{"category":"tts","values":{"tts.engine":"unknown"}}`
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	ServeConfigTest(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var result ConnectivityTestResult
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Unknown TTS engine")
}

func TestServeConfigTest_RAGCategory(t *testing.T) {
	body := `{"category":"rag","values":{"rag.base_url":""}}`
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	ServeConfigTest(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var result ConnectivityTestResult
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "required")
}

func TestServeConfigTest_DingTalkCategory(t *testing.T) {
	body := `{"category":"dingtalk","values":{"dingtalk.app_key":"","dingtalk.app_secret":""}}`
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	ServeConfigTest(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var result ConnectivityTestResult
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.False(t, result.Success)
}

// ── TTS engine tests ──────────────────────────────────────────

func TestTestTTS_DefaultEdge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	// Empty engine should default to "edge"
	result := testTTS(context.Background(), map[string]any{
		"tts.engine": "",
	})
	// With empty engine, it defaults to "edge" — just verify it doesn't panic
	_ = result
}

func TestTestTTS_PiperNoBinary(t *testing.T) {
	result := testTTSPiper(map[string]any{})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Piper binary not found")
}

func TestTestTTS_PiperModelPathNotConfigured(t *testing.T) {
	// When piper binary is not found, it fails before checking model path.
	// This test verifies the function handles missing model path correctly.
	result := testTTSPiper(map[string]any{
		"tts.voice":            "",
		"tts.piper.model_path": "",
	})
	assert.False(t, result.Success)
}

func TestTestTTS_PiperModelNotFound(t *testing.T) {
	// When piper binary is not found, it fails before checking model file.
	result := testTTSPiper(map[string]any{
		"tts.voice":            "test-voice",
		"tts.piper.model_path": "/nonexistent/model.onnx",
	})
	assert.False(t, result.Success)
}

func TestTestTTS_PiperSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	modelFile := filepath.Join(tmpDir, "model.onnx")
	require.NoError(t, os.WriteFile(modelFile, []byte("fake model"), 0o644))

	result := testTTSPiper(map[string]any{
		"tts.voice":            "test-voice",
		"tts.piper.model_path": modelFile,
	})
	// May still fail if piper binary not found, but model check passes
	if result.Success {
		assert.Contains(t, result.Message, "Piper ready")
	}
}

func TestTestTTS_KokoroMultipleErrors(t *testing.T) {
	result := testTTSKokoro(map[string]any{
		"tts.kokoro.model_path":  "/nonexistent/model.onnx",
		"tts.kokoro.voices_path": "/nonexistent/voices.bin",
	})
	assert.False(t, result.Success)
	// Should report model and voices file errors
	assert.Contains(t, result.Message, "Kokoro model file not found")
	assert.Contains(t, result.Message, "Kokoro voices file not found")
}

func TestTestTTS_KokoroModelNotFound(t *testing.T) {
	result := testTTSKokoro(map[string]any{
		"tts.kokoro.model_path":  "/nonexistent/model.onnx",
		"tts.kokoro.voices_path": "/nonexistent/voices.bin",
	})
	assert.False(t, result.Success)
}

func TestTestTTS_KokoroVoicesNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	modelFile := filepath.Join(tmpDir, "kokoro.onnx")
	require.NoError(t, os.WriteFile(modelFile, []byte("fake"), 0o644))

	result := testTTSKokoro(map[string]any{
		"tts.kokoro.model_path":  modelFile,
		"tts.kokoro.voices_path": "/nonexistent/voices.bin",
	})
	assert.False(t, result.Success)
}

func TestTestTTS_NanoNoBinary(t *testing.T) {
	result := testTTSNano(map[string]any{
		"tts.moss_nano.model_dir": "",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "MOSS-Nano binary not found")
}

func TestTestTTS_NanoNoModelDir(t *testing.T) {
	// Without model dir, binary may be found — just check it doesn't panic
	result := testTTSNano(map[string]any{
		"tts.moss_nano.model_dir": "/nonexistent/models",
	})
	// Binary probably not found in CI
	_ = result
}

func TestTestTTS_NanoInvalidModelDir(t *testing.T) {
	// Point to a file instead of directory
	tmpDir := t.TempDir()
	notDir := filepath.Join(tmpDir, "not-a-dir")
	require.NoError(t, os.WriteFile(notDir, []byte("file"), 0o644))

	result := testTTSNano(map[string]any{
		"tts.moss_nano.model_dir": notDir,
	})
	// May fail on binary not found first, or model dir invalid
	_ = result
}

// ── OpenAI TTS connectivity tests ─────────────────────────────

func TestTestTTS_OpenAIRequiresFields(t *testing.T) {
	cases := []struct {
		name    string
		values  map[string]any
		wantSub string
	}{
		{
			name:    "missing base_url",
			values:  map[string]any{"tts.openai.model": "m", "tts.openai.voice": "alloy"},
			wantSub: "base URL is required",
		},
		{
			name:    "missing model",
			values:  map[string]any{"tts.openai.base_url": "https://api.openai.com/v1", "tts.openai.voice": "alloy"},
			wantSub: "model is required",
		},
		{
			name:    "missing voice",
			values:  map[string]any{"tts.openai.base_url": "https://api.openai.com/v1", "tts.openai.model": "m"},
			wantSub: "voice is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := testTTSOpenAI(context.Background(), tc.values)
			assert.False(t, result.Success)
			assert.Contains(t, result.Message, tc.wantSub)
		})
	}
}

// The probe must issue a real synthesis request (not a models lookup), because
// gateways commonly do not enumerate every model — and it must ask for the raw
// audio body, which every model supports, rather than SSE.
func TestTestTTS_OpenAIProbeRequestShape(t *testing.T) {
	var gotBody map[string]any
	var gotAuth, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("fake-mp3"))
	}))
	defer srv.Close()

	result := testTTSOpenAI(context.Background(), map[string]any{
		"tts.openai.base_url": srv.URL + "/v1",
		"tts.openai.api_key":  "sk-test",
		"tts.openai.model":    "gpt-4o-mini-tts",
		"tts.openai.voice":    "alloy",
	})

	assert.True(t, result.Success, result.Message)
	assert.Contains(t, result.Message, "gpt-4o-mini-tts")
	assert.Contains(t, result.Message, "alloy")

	assert.Equal(t, "/v1/audio/speech", gotPath, "base_url ending in /v1 must complete to the speech endpoint")
	assert.Equal(t, "Bearer sk-test", gotAuth)
	assert.Equal(t, "gpt-4o-mini-tts", gotBody["model"])
	assert.Equal(t, "alloy", gotBody["voice"])
	assert.Equal(t, "audio", gotBody["stream_format"], "probe must use the shape every model supports")
	assert.NotEmpty(t, gotBody["input"])
}

func TestTestTTS_OpenAIErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	result := testTTSOpenAI(context.Background(), map[string]any{
		"tts.openai.base_url": srv.URL,
		"tts.openai.model":    "m",
		"tts.openai.voice":    "alloy",
	})

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "401")
	assert.Contains(t, result.Message, "bad key")
}

// A 200 with an empty body would otherwise read as success while playback
// fails with no audio.
func TestTestTTS_OpenAIEmptyBodyFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
	}))
	defer srv.Close()

	result := testTTSOpenAI(context.Background(), map[string]any{
		"tts.openai.base_url": srv.URL,
		"tts.openai.model":    "m",
		"tts.openai.voice":    "alloy",
	})

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "empty audio body")
}

func TestTestTTS_OpenAIUnreachable(t *testing.T) {
	result := testTTSOpenAI(context.Background(), map[string]any{
		"tts.openai.base_url": "http://127.0.0.1:1/v1",
		"tts.openai.model":    "m",
		"tts.openai.voice":    "alloy",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "unreachable")
}

// The engine dispatcher must route "openai" to the OpenAI probe rather than
// falling into the unknown-engine branch.
func TestTestTTS_RoutesToOpenAI(t *testing.T) {
	result := testTTS(context.Background(), map[string]any{
		"tts.engine": "openai",
	})
	assert.False(t, result.Success)
	assert.NotContains(t, result.Message, "Unknown TTS engine")
	assert.Contains(t, result.Message, "base URL is required")
}

// ── Port Forward tests ────────────────────────────────────────

func TestTestPortForward_ServerNotListening(t *testing.T) {
	origSSH := sshServerRef
	origRegistry := service.ProxyService
	// Set sshServerRef to nil (no server running). The registry must go too —
	// with it present the h2 branch reports success (see the h2-only case).
	sshServerRef = nil
	service.ProxyService = nil
	defer func() { sshServerRef = origSSH; service.ProxyService = origRegistry }()

	result := testPortForward(context.Background(), map[string]any{})
	assert.False(t, result.Success)
}

func TestTestPortForward_H2Only(t *testing.T) {
	// The regression this guards: an h2-only install (port_forward.enabled:
	// false, so no SSH listener) forwards ports perfectly well, and the test
	// endpoint used to report it as broken because it only ever looked at SSH.
	origSSH := sshServerRef
	origRegistry := service.ProxyService
	sshServerRef = nil
	service.ProxyService = &service.ProxyRegistry{}
	defer func() { sshServerRef = origSSH; service.ProxyService = origRegistry }()

	result := testPortForward(context.Background(), map[string]any{})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "HTTP/2")
}

func TestTestPortForward_SshWinsWhenBothAvailable(t *testing.T) {
	// With an SSH listener up, the message stays the SSH one: it is the more
	// specific answer, and the pre-h2 behavior for SSH installs is unchanged.
	origSSH := sshServerRef
	origRegistry := service.ProxyService
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	sshServerRef = ssh.NewServer(model.PortForwardConfig{Enabled: true, Port: port}, 20000, "test-password", nil)
	service.ProxyService = &service.ProxyRegistry{}
	defer func() { sshServerRef = origSSH; service.ProxyService = origRegistry }()

	result := testPortForward(context.Background(), map[string]any{})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "SSH tunnel server is listening")
}

func TestTestPortForward_ServerListening(t *testing.T) {
	origSSH := sshServerRef
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	srv := ssh.NewServer(model.PortForwardConfig{Enabled: true, Port: port}, 20000, "test-password", nil)
	sshServerRef = srv
	defer func() { sshServerRef = origSSH }()

	result := testPortForward(context.Background(), map[string]any{})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "listening on port")
}

// ── Summarize Voice additional tests ─────────────────────────

func TestTestSummarizeVoice_EmptyURL(t *testing.T) {
	result := testSummarizeVoice(context.Background(), map[string]any{
		"ai_summary.api.base_url": "",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "required")
}

func TestTestSummarizeVoice_DefaultModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	result := testSummarizeVoice(context.Background(), map[string]any{
		"ai_summary.api.base_url": srv.URL,
		"ai_summary.api.key":      "test-key",
		// No model provided — should default to "gpt-4o-mini"
	})
	assert.True(t, result.Success)
}

// ── Anthropic API additional tests ────────────────────────────

func TestTestAnthropicAPI_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	result := testAnthropicAPI(context.Background(), client, srv.URL, "test-key", "claude-3-haiku")
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "anthropic")
}

func TestTestAnthropicAPI_ErrorWithMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintln(w, `{"error":{"message":"Invalid API key"}}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	result := testAnthropicAPI(context.Background(), client, srv.URL, "bad-key", "claude-3-haiku")
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Invalid API key")
}

func TestTestAnthropicAPI_ErrorWithoutMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, `{"error":{"type":"server_error"}}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	result := testAnthropicAPI(context.Background(), client, srv.URL, "test-key", "test-model")
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "HTTP 500")
}

func TestTestAnthropicAPI_ConnectionError(t *testing.T) {
	client := &http.Client{Timeout: 1 * time.Second}
	// Use a port that's not listening
	result := testAnthropicAPI(context.Background(), client, "http://127.0.0.1:19999", "key", "model")
	assert.False(t, result.Success)
}

func TestTestOpenAIAPI_ConnectionError(t *testing.T) {
	client := &http.Client{Timeout: 1 * time.Second}
	result := testOpenAIAPI(context.Background(), client, "http://127.0.0.1:19999", "key", "model")
	assert.False(t, result.Success)
}

func TestTestOpenAIAPI_ErrorWithoutMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, `{"error":{"type":"server_error"}}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	result := testOpenAIAPI(context.Background(), client, srv.URL, "test-key", "test-model")
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "HTTP 500")
}

func TestTestOpenAIAPI_NoAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify no Authorization header is sent when key is empty
		assert.Empty(t, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	result := testOpenAIAPI(context.Background(), client, srv.URL, "", "test-model")
	assert.True(t, result.Success)
}

// ── RAG additional tests ──────────────────────────────────────

func TestTestRAG_DefaultModel(t *testing.T) {
	srv := ragTestServer(t, nil)
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
		// No model — should default to "bge-m3"
	})
	assert.True(t, result.Success)
}

func TestTestRAG_WithAPIKey(t *testing.T) {
	srv := ragTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"data":[{"embedding":[0.1,0.2,0.3],"index":0}]}`)
	})
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
		"rag.model":    "bge-m3",
		"rag.api_key":  "test-api-key",
	})
	assert.True(t, result.Success)
}

func TestTestRAG_EmbeddingFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintln(w, `model not found`)
	}))
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
		"rag.model":    "bge-m3",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "embedding failed")
}

func TestTestRAG_EmbeddingEmptyVector(t *testing.T) {
	srv := ragTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"data":[{"embedding":[],"index":0}]}`)
	})
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
		"rag.model":    "bge-m3",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "empty embedding")
}

// ── DingTalk additional tests ─────────────────────────────────

func TestTestDingTalk_ParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `not valid json`)
	}))
	defer srv.Close()

	origURL := dingtalkTokenURL
	dingtalkTokenURL = srv.URL + "/gettoken"
	defer func() { dingtalkTokenURL = origURL }()

	result := testDingTalk(context.Background(), map[string]any{
		"dingtalk.app_key":    "key",
		"dingtalk.app_secret": "secret",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "parse DingTalk response")
}

func TestTestDingTalk_ConnectionError(t *testing.T) {
	origURL := dingtalkTokenURL
	dingtalkTokenURL = "http://127.0.0.1:19999/gettoken"
	defer func() { dingtalkTokenURL = origURL }()

	result := testDingTalk(context.Background(), map[string]any{
		"dingtalk.app_key":    "key",
		"dingtalk.app_secret": "secret",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Failed to connect")
}

func TestTestFeishu_MissingFields(t *testing.T) {
	result := testFeishu(context.Background(), map[string]any{
		"feishu.app_id":     "",
		"feishu.app_secret": "",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "required")
}

func TestTestFeishu_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok","tenant_access_token":"t-test","expire":7200}`)
	}))
	defer srv.Close()

	origURL := feishuTokenURL
	feishuTokenURL = srv.URL
	defer func() { feishuTokenURL = origURL }()

	result := testFeishu(context.Background(), map[string]any{
		"feishu.app_id":     "cli_test",
		"feishu.app_secret": "test_secret",
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "successful")
}

func TestTestFeishu_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":40014,"msg":"invalid app_id"}`)
	}))
	defer srv.Close()

	origURL := feishuTokenURL
	feishuTokenURL = srv.URL
	defer func() { feishuTokenURL = origURL }()

	result := testFeishu(context.Background(), map[string]any{
		"feishu.app_id":     "bad_id",
		"feishu.app_secret": "bad_secret",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "authentication failed")
}

func TestTestFeishu_ConnectionError(t *testing.T) {
	origURL := feishuTokenURL
	feishuTokenURL = "http://127.0.0.1:19999"
	defer func() { feishuTokenURL = origURL }()

	result := testFeishu(context.Background(), map[string]any{
		"feishu.app_id":     "test",
		"feishu.app_secret": "test",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Failed to connect")
}

func TestTestFeishu_ParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "not json")
	}))
	defer srv.Close()

	origURL := feishuTokenURL
	feishuTokenURL = srv.URL
	defer func() { feishuTokenURL = origURL }()

	result := testFeishu(context.Background(), map[string]any{
		"feishu.app_id":     "test",
		"feishu.app_secret": "test",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "parse Feishu response")
}

func TestServeConfigTest_FeishuCategory(t *testing.T) {
	body := `{"category":"feishu","values":{"feishu.app_id":"","feishu.app_secret":""}}`
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	ServeConfigTest(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var result ConnectivityTestResult
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.False(t, result.Success)
}

// ── MCP ──────────────────────────────────────────────────────────────────

// TestServeConfigTest_MCPProbesFormValues pins that the probe uses the SERVER
// DEFINITION IN THE FORM, not the saved one.
//
// The user clicks "test" to decide whether to save. Probing the saved
// definition would report the OLD server's health — a green result for a
// configuration that was never saved, or a red one for an edit that already
// fixed the problem.
func TestServeConfigTest_MCPProbesFormValues(t *testing.T) {
	script := writeMCPScript(t, `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"initialize"'*)
      printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"form-srv","version":"1.2"}}}'
      ;;
    *'"tools/list"'*)
      printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"tool_a"},{"name":"tool_b"}]}}'
      ;;
  esac
done
`)

	body, err := json.Marshal(map[string]any{
		"category": "mcp",
		"values": map[string]any{
			"mcp.name": "form-srv",
			"mcp.servers": []any{
				map[string]any{"name": "form-srv", "transport": "stdio", "command": script},
			},
		},
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(string(body)))
	ServeConfigTest(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var result ConnectivityTestResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result.Success, result.Message)
	assert.Contains(t, result.Message, "2 tool(s)")
	assert.Contains(t, result.Message, "form-srv")
	assert.Contains(t, result.Message, "tool_a")
}

// TestServeConfigTest_MCPUnknownNameListsAlternatives asserts a wrong name is
// reported with the names that DO exist: "not found" alone leaves the user
// guessing at a typo.
func TestServeConfigTest_MCPUnknownNameListsAlternatives(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"category": "mcp",
		"values": map[string]any{
			"mcp.name": "typo",
			"mcp.servers": []any{
				map[string]any{"name": "alpha", "transport": "stdio", "command": "sh"},
				map[string]any{"name": "beta", "transport": "stdio", "command": "sh"},
			},
		},
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(string(body)))
	ServeConfigTest(w, r)

	var result ConnectivityTestResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "alpha")
	assert.Contains(t, result.Message, "beta")
}

// TestServeConfigTest_MCPRequiresName asserts the target server must be named:
// probing "whichever server" would report one server's health for another.
func TestServeConfigTest_MCPRequiresName(t *testing.T) {
	body := `{"category":"mcp","values":{"mcp.servers":[]}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	ServeConfigTest(w, r)

	var result ConnectivityTestResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "mcp.name")
}

// TestServeConfigTest_MCPNoServers asserts an empty configuration says so
// rather than reporting a generic failure.
func TestServeConfigTest_MCPNoServers(t *testing.T) {
	body := `{"category":"mcp","values":{"mcp.name":"x","mcp.servers":[]}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	ServeConfigTest(w, r)

	var result ConnectivityTestResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "no MCP servers")
}

// TestServeConfigTest_MCPBrokenCommandFails asserts a real failure is reported
// as a failure — the whole point of the button.
func TestServeConfigTest_MCPBrokenCommandFails(t *testing.T) {
	body := `{"category":"mcp","values":{"mcp.name":"broken","mcp.servers":[{"name":"broken","transport":"stdio","command":"clawbench-no-such-binary"}]}}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	ServeConfigTest(w, r)

	var result ConnectivityTestResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "not found in PATH")
}

// writeMCPScript drops an executable test server into a temp dir.
func writeMCPScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mcp-server.sh")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o755))
	return p
}

// ── MCP connectivity test ────────────────────────────────────────────────────

// testMCP names every available server when the requested one is missing: "not
// found" alone does not tell the user what they can pick instead.
func TestTestMCP_UnknownNameListsAvailable(t *testing.T) {
	values := map[string]any{
		"mcp.name": "ghost",
		"mcp.servers": []any{
			map[string]any{"name": "alpha", "transport": "stdio", "command": "node"},
			map[string]any{"name": "beta", "transport": "http", "url": "https://b"},
		},
	}

	res := testMCP(context.Background(), values)
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "ghost")
	assert.Contains(t, res.Message, "alpha")
	assert.Contains(t, res.Message, "beta")
}

// With no servers at all the message must say so rather than printing an empty
// "available:" list.
func TestTestMCP_NoServersConfigured(t *testing.T) {
	prev := model.ConfigInstance.MCP
	model.ConfigInstance.MCP = model.MCPConfig{}
	t.Cleanup(func() { model.ConfigInstance.MCP = prev })

	res := testMCP(context.Background(), map[string]any{"mcp.name": "ghost"})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "no MCP servers are configured")
}

// A missing name is rejected before any probe runs: there is nothing to test.
func TestTestMCP_RequiresName(t *testing.T) {
	res := testMCP(context.Background(), map[string]any{})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "mcp.name is required")
}

// A malformed servers payload is reported as such instead of being silently
// ignored (which would probe the saved config and test the wrong thing).
func TestTestMCP_MalformedServersPayload(t *testing.T) {
	values := map[string]any{
		"mcp.name":    "alpha",
		"mcp.servers": []any{"not-an-object"},
	}
	res := testMCP(context.Background(), values)
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "must be an object")
}

// The form's definition is what gets probed, not the saved one: the user clicks
// test precisely to decide whether to save the edit.
func TestMCPServersFromValues_PrefersFormOverSaved(t *testing.T) {
	prev := model.ConfigInstance.MCP
	model.ConfigInstance.MCP = model.MCPConfig{Servers: []model.MCPServer{{Name: "saved"}}}
	t.Cleanup(func() { model.ConfigInstance.MCP = prev })

	got, err := mcpServersFromValues(map[string]any{
		"mcp.servers": []any{map[string]any{"name": "from-form", "transport": "stdio", "command": "x"}},
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "from-form", got[0].Name)

	// No list in the form → fall back to the saved config.
	got, err = mcpServersFromValues(map[string]any{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "saved", got[0].Name)
}
