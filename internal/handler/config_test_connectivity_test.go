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
	assert.Contains(t, result.Message, "Unknown category")
}

func TestServeConfigTest_MethodNotAllowed(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/config/test", http.NoBody)
	ServeConfigTest(w, r)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ── FRP tests ────────────────────────────────────────────────

func TestTestFRP_EmptyAddr(t *testing.T) {
	result := testFRP(context.Background(), map[string]any{
		"frp.server_addr": "",
		"frp.server_port": float64(7000),
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "required")
}

func TestTestFRP_Success(t *testing.T) {
	// Start a TCP listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	result := testFRP(context.Background(), map[string]any{
		"frp.server_addr": "127.0.0.1",
		"frp.server_port": float64(port),
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "Successfully connected")
}

func TestTestFRP_ConnectionRefused(t *testing.T) {
	// Use a port that's not listening
	result := testFRP(context.Background(), map[string]any{
		"frp.server_addr": "127.0.0.1",
		"frp.server_port": float64(19999),
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Failed to connect")
}

// ── Summarize tests ──────────────────────────────────────────

func TestTestSummarizeText_NotAPI(t *testing.T) {
	result := testSummarizeText(context.Background(), map[string]any{
		"summarize.backend": "simple",
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "not API mode")
}

func TestTestSummarizeText_EmptyURL(t *testing.T) {
	result := testSummarizeText(context.Background(), map[string]any{
		"summarize.backend":      "api",
		"summarize.api.base_url": "",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "required")
}

func TestTestSummarizeText_OpenAISuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	result := testSummarizeText(context.Background(), map[string]any{
		"summarize.backend":      "api",
		"summarize.api.base_url": srv.URL,
		"summarize.api.key":      "test-key",
		"summarize.model":        "test-model",
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "successful")
}

func TestTestSummarizeText_OpenAIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintln(w, `{"error":{"message":"Invalid API key"}}`)
	}))
	defer srv.Close()

	result := testSummarizeText(context.Background(), map[string]any{
		"summarize.backend":      "api",
		"summarize.api.base_url": srv.URL,
		"summarize.api.key":      "bad-key",
		"summarize.model":        "test-model",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Invalid API key")
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
		"summarize.tts_backend":      "api",
		"summarize.tts_api.base_url": srv.URL + "/v1/messages",
		"summarize.tts_api.key":      "test-key",
		"summarize.tts_model":        "claude-3-haiku",
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "anthropic")
}

func TestTestSummarizeVoice_NotAPI(t *testing.T) {
	result := testSummarizeVoice(context.Background(), map[string]any{
		"summarize.tts_backend": "simple",
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

func TestTestRAG_ReachableWithModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/v1/models")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"data":[{"id":"bge-m3"},{"id":"nomic-embed"}]}`)
	}))
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
		"rag.model":    "bge-m3",
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "available")
}

func TestTestRAG_ReachableModelNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"data":[{"id":"nomic-embed"}]}`)
	}))
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
		"rag.model":    "bge-m3",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "not found")
}

func TestTestRAG_ModelsNotSupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
		"rag.model":    "bge-m3",
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "not supported by server")
}

func TestTestRAG_Unreachable(t *testing.T) {
	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": "http://127.0.0.1:19999",
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "unreachable")
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
	sshServerRef = nil
	defer func() { sshServerRef = origSSH }()

	result := testPortForward(context.Background(), map[string]any{})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "not running")
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

// ── buildEndpointURL tests ────────────────────────────────────

func TestBuildEndpointURL(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		defaultPath string
		expected    string
	}{
		{"full URL no path", "https://api.openai.com", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"URL with /v1", "https://api.openai.com/v1", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"URL already complete", "https://api.openai.com/v1/chat/completions", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"URL with trailing slash", "https://api.openai.com/v1/", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"Anthropic no path", "https://api.anthropic.com", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"Anthropic with /v1", "https://api.anthropic.com/v1", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"Anthropic already complete", "https://api.anthropic.com/v1/messages", "/v1/messages", "https://api.anthropic.com/v1/messages"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildEndpointURL(tt.baseURL, tt.defaultPath)
			assert.Equal(t, tt.expected, result)
		})
	}
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

func TestServeConfigTest_FRPCategory(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	body := fmt.Sprintf(`{"category":"frp","values":{"frp.server_addr":"127.0.0.1","frp.server_port":%d}}`, port)
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	ServeConfigTest(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var result ConnectivityTestResult
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.True(t, result.Success)
}

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

// ── Port Forward tests ────────────────────────────────────────

func TestTestPortForward_ServerNotListening(t *testing.T) {
	origSSH := sshServerRef
	// Create server with port 0 (not listening)
	srv := ssh.NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 20000, "test-password", nil)
	sshServerRef = srv
	defer func() { sshServerRef = origSSH }()

	result := testPortForward(context.Background(), map[string]any{})
	assert.False(t, result.Success)
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
		"summarize.tts_backend": strAPI,
		"summarize.tts_api.base_url": "",
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
		"summarize.tts_backend":      strAPI,
		"summarize.tts_api.base_url": srv.URL,
		"summarize.tts_api.key":      "test-key",
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"data":[{"id":"bge-m3"}]}`)
	}))
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
		// No model — should default to "bge-m3"
	})
	assert.True(t, result.Success)
}

func TestTestRAG_PrefixMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"data":[{"id":"bge-m3:latest"}]}`)
	}))
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
		"rag.model":    "bge-m3",
	})
	assert.True(t, result.Success)
}

func TestTestRAG_ParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `not valid json for models list`)
	}))
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
	})
	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "could not parse")
}

func TestTestRAG_OtherHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
	})
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "HTTP 403")
}

func TestTestRAG_WithAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"data":[{"id":"bge-m3"}]}`)
	}))
	defer srv.Close()

	result := testRAG(context.Background(), map[string]any{
		"rag.base_url": srv.URL,
		"rag.model":    "bge-m3",
		"rag.api_key":  "test-api-key",
	})
	assert.True(t, result.Success)
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

// ── FRP additional tests ──────────────────────────────────────

func TestTestFRP_DefaultPort(t *testing.T) {
	// Start a TCP listener on port 7000 if available; otherwise skip
	ln, err := net.Listen("tcp", "127.0.0.1:7000")
	if err != nil {
		t.Skip("port 7000 not available for default port test")
	}
	defer ln.Close()

	result := testFRP(context.Background(), map[string]any{
		"frp.server_addr": "127.0.0.1",
		// No port — should default to 7000
	})
	assert.True(t, result.Success)
}

// ── buildEndpointURL additional tests ─────────────────────────

func TestBuildEndpointURL_NoSlashInPath(t *testing.T) {
	result := buildEndpointURL("https://api.example.com", "chat")
	assert.Equal(t, "https://api.example.com/chat", result)
}

// ── Summarize text default model test ─────────────────────────

func TestTestSummarizeText_DefaultModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	result := testSummarizeText(context.Background(), map[string]any{
		"summarize.backend":      strAPI,
		"summarize.api.base_url": srv.URL,
		"summarize.api.key":      "test-key",
		// No model — should default to "gpt-4o-mini"
	})
	assert.True(t, result.Success)
}
