package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestServeConfig_Get(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Upload.MaxSizeMB = 50
	cfg.Upload.MaxFiles = 10
	cfg.Chat.InitialMessages = 15
	cfg.Chat.PageSize = 25
	cfg.Chat.CollapsedHeight = 200
	cfg.Chat.SystemPromptInterval = 5
	cfg.Session.MaxCount = 5
	cfg.Terminal.Enabled = true
	cfg.Terminal.IdleTimeout = "10m"
	cfg.Terminal.MaxSessions = 8
	cfg.Terminal.BufferLines = 3000
	cfg.TTS.Engine = "edge"
	cfg.TTS.Speed = 1.5
	cfg.TTS.Voice = "zh-CN-XiaoxiaoNeural"
	cfg.TTS.MaxCacheFiles = 50
	cfg.RAG.BaseURL = "http://localhost:11434"
	cfg.RAG.Model = "bge-m3"
	cfg.RAG.ChunkSize = 512
	cfg.RAG.SearchLimit = 5
	cfg.RAG.RetentionDays = 30
	cfg.PortForward.Enabled = true
	cfg.PortForward.Port = 20001
	cfg.Push.JPush.Enabled = true
	cfg.Push.JPush.AppKey = "test-app-key"
	cfg.Summarize.Backend = "simple"
	cfg.Summarize.Model = ""
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)

	// Verify version is present
	assert.Contains(t, resp, "version")
	assert.NotEmpty(t, resp["version"])

	// Verify allowed sections ARE present
	assert.Contains(t, resp, "chat")
	assert.Contains(t, resp, "session")
	assert.Contains(t, resp, "upload")
	assert.Contains(t, resp, "terminal")
	assert.Contains(t, resp, "tts")
	assert.Contains(t, resp, "rag")
	assert.Contains(t, resp, "port_forward")
	assert.Contains(t, resp, "push")
	assert.Contains(t, resp, "summarize")

	// Verify specific values
	chat, _ := resp["chat"].(map[string]any)
	assert.Equal(t, float64(200), chat["collapsed_height"])
	assert.Equal(t, float64(15), chat["initial_messages"])

	upload, _ := resp["upload"].(map[string]any)
	assert.Equal(t, float64(50), upload["max_size_mb"])

	terminal, _ := resp["terminal"].(map[string]any)
	assert.Equal(t, true, terminal["enabled"])
	assert.Equal(t, "10m", terminal["idle_timeout"])

	// Verify summarize section
	summarize, _ := resp["summarize"].(map[string]any)
	assert.Equal(t, "simple", summarize["backend"])

	// When engine=edge, engine-specific sub-configs should NOT be present
	tts, _ := resp["tts"].(map[string]any)
	assert.NotContains(t, tts, "piper")
	assert.NotContains(t, tts, "kokoro")
	assert.NotContains(t, tts, "moss_nano")
	// TTS response no longer contains summarize_backend, summarize_model, or api
	assert.NotContains(t, tts, "summarize_backend")
	assert.NotContains(t, tts, "summarize_model")
	assert.NotContains(t, tts, "api")
	// Internal fields should never be present
	assert.NotContains(t, tts, "inline_code_max_len")
	assert.NotContains(t, tts, "max_summarize_runes")

	// Verify sensitive fields are NOT present
	assert.NotContains(t, resp, "password")
	assert.NotContains(t, resp, "tls")
	assert.NotContains(t, resp, "host")
	assert.NotContains(t, resp, "port")
	assert.NotContains(t, resp, "log_level")
	assert.NotContains(t, resp, "log_dir")
	assert.NotContains(t, resp, "watch_dir")
	assert.NotContains(t, resp, "dev_port")

	// Verify port_forward doesn't expose host_key
	pf, _ := resp["port_forward"].(map[string]any)
	assert.NotContains(t, pf, "host_key")

	// Verify Push exposes master_secret (masked)
	push, _ := resp["push"].(map[string]any)
	jpush, _ := push["jpush"].(map[string]any)
	assert.Contains(t, jpush, "master_secret")
}

func TestServeConfig_Get_ConditionalPiperSubConfig(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "piper"
	cfg.TTS.Piper.ModelPath = "/path/to/model.onnx"
	cfg.TTS.Piper.NoiseScale = 0.667
	cfg.TTS.Piper.LengthScale = 1.0
	cfg.TTS.Piper.SentenceSilence = 0.2
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	tts, _ := resp["tts"].(map[string]any)
	assert.Contains(t, tts, "piper")
	// Kokoro/MossNano should not be present
	assert.NotContains(t, tts, "kokoro")
	assert.NotContains(t, tts, "moss_nano")

	piper, _ := tts["piper"].(map[string]any)
	assert.Equal(t, "/path/to/model.onnx", piper["model_path"])
	assert.Equal(t, 0.667, piper["noise_scale"])
}

func TestServeConfig_Get_ConditionalKokoroSubConfig(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "kokoro"
	cfg.TTS.Kokoro.ModelPath = "/path/to/kokoro.onnx"
	cfg.TTS.Kokoro.VoicesPath = "/path/to/voices.bin"
	cfg.TTS.Kokoro.Lang = "cmn"
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	tts, _ := resp["tts"].(map[string]any)
	assert.Contains(t, tts, "kokoro")
	assert.NotContains(t, tts, "piper")
	assert.NotContains(t, tts, "moss_nano")

	kokoro, _ := tts["kokoro"].(map[string]any)
	assert.Equal(t, "cmn", kokoro["lang"])
}

func TestServeConfig_Get_ConditionalMossNanoSubConfig(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "moss-nano"
	cfg.TTS.MossNano.ModelDir = "/path/to/models"
	cfg.TTS.MossNano.Voice = "Junhao"
	cfg.TTS.MossNano.Backend = "onnx"
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	tts, _ := resp["tts"].(map[string]any)
	assert.Contains(t, tts, "moss_nano")
	assert.NotContains(t, tts, "piper")
	assert.NotContains(t, tts, "kokoro")

	mossNano, _ := tts["moss_nano"].(map[string]any)
	assert.Equal(t, "onnx", mossNano["backend"])
	assert.Equal(t, "Junhao", mossNano["voice"])
}

func TestServeConfig_Get_ConditionalAPISubConfig(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "edge"
	cfg.Summarize.Backend = "api"
	cfg.Summarize.API.BaseURL = "https://api.openai.com/v1/chat/completions"
	cfg.Summarize.API.Key = "sk-1234567890abcdefghijklmnopqrstuvwxyz"
	cfg.Summarize.API.Format = "openai"
	cfg.Summarize.Model = "gpt-4o-mini"
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	summarize, _ := resp["summarize"].(map[string]any)
	assert.Contains(t, summarize, "api")

	api, _ := summarize["api"].(map[string]any)
	assert.Equal(t, "https://api.openai.com/v1/chat/completions", api["base_url"])
	// API key must be masked
	assert.Contains(t, api["key"], "***")
	assert.NotEqual(t, "sk-1234567890abcdefghijklmnopqrstuvwxyz", api["key"])
	// Verify mask format: first 4 + *** + last 3
	assert.Equal(t, "sk-1***xyz", api["key"])
	assert.Equal(t, "openai", api["format"])
}

func TestServeConfig_Get_APIMaskShortKey(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Summarize.Backend = "api"
	cfg.Summarize.API.Key = "short"
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	summarize, _ := resp["summarize"].(map[string]any)
	api, _ := summarize["api"].(map[string]any)
	assert.Equal(t, "****", api["key"])
}

func TestServeConfig_Get_APIMaskEmptyKey(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Summarize.Backend = "api"
	cfg.Summarize.API.Key = ""
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	summarize, _ := resp["summarize"].(map[string]any)
	api, _ := summarize["api"].(map[string]any)
	assert.Equal(t, "", api["key"])
}

func TestServeConfig_Get_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeConfig_Get_Unauthorized(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = "test-token"

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	w := callHandler(middleware.Auth(ServeConfig), req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- PATCH /api/config tests ---

func TestServeConfig_Patch_Success(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Chat.CollapsedHeight = 150
	cfg.Upload.MaxSizeMB = 100
	model.ConfigInstance = cfg

	body := `{"chat":{"collapsed_height":200},"upload":{"max_size_mb":50}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	// Both chat.collapsed_height and upload.max_size_mb are hot-reload fields
	assert.False(t, resp["needs_restart"].(bool), "hot-reload fields should not need restart")
	changed, ok := resp["changed_cold_fields"].([]any)
	assert.True(t, ok)
	assert.Empty(t, changed, "no cold fields should be reported for hot-reload changes")

	assert.Equal(t, 200, model.ConfigInstance.Chat.CollapsedHeight)
	assert.Equal(t, 50, model.ConfigInstance.Upload.MaxSizeMB)
}

func TestServeConfig_Patch_PiperSubConfig(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "piper"
	cfg.TTS.Piper.ModelPath = "/path/to/model.onnx" // required when engine=piper
	model.ConfigInstance = cfg

	body := `{"tts":{"piper":{"noise_scale":0.5,"length_scale":1.2,"sentence_silence":0.3}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 0.5, model.ConfigInstance.TTS.Piper.NoiseScale)
	assert.Equal(t, 1.2, model.ConfigInstance.TTS.Piper.LengthScale)
	assert.Equal(t, 0.3, model.ConfigInstance.TTS.Piper.SentenceSilence)
}

func TestServeConfig_Patch_APISubConfig(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"summarize":{"model":"gpt-4o-mini","api":{"base_url":"https://api.example.com/v1/chat","format":"openai"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "https://api.example.com/v1/chat", model.ConfigInstance.Summarize.API.BaseURL)
	assert.Equal(t, "openai", model.ConfigInstance.Summarize.API.Format)
	assert.Equal(t, "gpt-4o-mini", model.ConfigInstance.Summarize.Model)
}

func TestServeConfig_Patch_APIKeyMasked(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	// PATCH with masked key containing *** should be rejected
	body := `{"summarize":{"api":{"key":"sk-1***xyz"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfig_Patch_APIKeyFull(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"summarize":{"api":{"key":"sk-1234567890abcdef"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "sk-1234567890abcdef", model.ConfigInstance.Summarize.API.Key)
}

func TestServeConfig_Patch_SummarizeSection(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"summarize":{"backend":"codebuddy","model":"codebuddy-latest"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "codebuddy", model.ConfigInstance.Summarize.Backend)
	assert.Equal(t, "codebuddy-latest", model.ConfigInstance.Summarize.Model)
}

func TestServeConfig_Patch_MossNanoInvalidBackend(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"tts":{"moss_nano":{"backend":"invalid"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfig_Patch_APIInvalidFormat(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"tts":{"api":{"format":"invalid"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfig_Patch_ForbiddenField_Password(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := `{"password":"hacked"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfig_Patch_ForbiddenField_TLS(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := `{"tls":{"enabled":false}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfig_Patch_MasterSecret(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// master_secret is now patchable and should be accepted
	body := `{"push":{"jpush":{"master_secret":"newsecret1234567890"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "newsecret1234567890", model.ConfigInstance.Push.JPush.MasterSecret)
}

func TestServeConfig_Patch_InvalidEngine(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := `{"tts":{"engine":"invalid_engine"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfig_Patch_InvalidSummarizeBackend(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := `{"summarize":{"backend":"nonexistent"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfig_Patch_NegativeNumber(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := `{"chat":{"collapsed_height":-1}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfig_Patch_InvalidJSON(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := `{invalid json`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfig_Patch_EmptyBody(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := `{}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- POST /api/config/restart tests ---

func TestServeConfigRestart_Success(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	restartCh := make(chan struct{}, 1)
	SetRestartFunc(func() {
		restartCh <- struct{}{}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/config/restart", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigRestart, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "restarting", resp["status"])

	select {
	case <-restartCh:
	case <-time.After(5 * time.Second):
		t.Fatal("restart function was not called within timeout")
	}
}

func TestServeConfigRestart_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/config/restart", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigRestart, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeConfig_Get_DefaultAgent(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.DefaultAgent = "codebuddy"
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.Contains(t, resp, "default_agent")
	assert.Equal(t, "codebuddy", resp["default_agent"])
}

func TestServeConfig_Patch_DefaultAgent(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.DefaultAgent = "codebuddy"
	model.ConfigInstance = cfg

	body := `{"default_agent":"claude"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "claude", model.ConfigInstance.DefaultAgent)
	assert.Equal(t, "claude", model.DefaultAgentID)
}

func TestServeConfig_Patch_SummarizeAPIWithoutBaseURL(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Set up config with empty api.base_url
	cfg := model.Config{}
	cfg.Summarize.Backend = "simple" // current value is not "api"
	cfg.Summarize.API.BaseURL = ""   // no base URL configured
	model.ConfigInstance = cfg

	// Switch backend to "api" without providing base_url — should succeed
	// because the user hasn't had a chance to fill in the API sub-config yet
	// (frontend auto-saves one field at a time, same as tts.engine switch).
	body := `{"summarize":{"backend":"api"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeConfig_Patch_SummarizeAPIAlreadySetWithoutBaseURL(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// backend is already "api" but base_url is missing — should reject
	cfg := model.Config{}
	cfg.Summarize.Backend = "api"
	cfg.Summarize.API.BaseURL = ""
	model.ConfigInstance = cfg

	// Patch another field while backend is already "api" — base_url should be required
	body := `{"summarize":{"model":"gpt-4o"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "base_url is required")
}

func TestServeConfig_Patch_SummarizeAPIWithBaseURL(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Set up config with base_url already configured
	cfg := model.Config{}
	cfg.Summarize.Backend = "simple"
	cfg.Summarize.API.BaseURL = "https://api.openai.com/v1"
	model.ConfigInstance = cfg

	// Patch backend to "api" — should succeed because base_url exists
	body := `{"summarize":{"backend":"api"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeConfig_Patch_PiperEngineWithoutModelPath(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "edge"
	cfg.TTS.Piper.ModelPath = ""
	model.ConfigInstance = cfg

	// Switching engine without sub-config should succeed — user fills sub-config later
	body := `{"tts":{"engine":"piper"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "piper", model.ConfigInstance.TTS.Engine)
}

func TestServeConfig_Patch_PiperSubConfigWithoutModelPath(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "piper"
	cfg.TTS.Piper.ModelPath = ""
	model.ConfigInstance = cfg

	// Saving sub-config when engine is already piper but model_path is empty should fail
	body := `{"tts":{"piper":{"noise_scale":0.5}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "piper.model_path is required")
}

func TestServeConfig_Patch_KokoroEngineWithoutPaths(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "edge"
	model.ConfigInstance = cfg

	// Switching engine without sub-config should succeed — user fills sub-config later
	body := `{"tts":{"engine":"kokoro"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "kokoro", model.ConfigInstance.TTS.Engine)
}

func TestServeConfig_Patch_KokoroSubConfigWithoutPaths(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "kokoro"
	cfg.TTS.Kokoro.ModelPath = ""
	cfg.TTS.Kokoro.VoicesPath = ""
	model.ConfigInstance = cfg

	// Saving sub-config when engine is already kokoro but paths are empty should fail
	body := `{"tts":{"kokoro":{"lang":"en"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "kokoro.model_path is required")
}

func TestServeConfig_Patch_MossNanoEngineWithoutModelDir(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "edge"
	model.ConfigInstance = cfg

	// Switching engine without sub-config should succeed — user fills sub-config later
	body := `{"tts":{"engine":"moss-nano"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "moss-nano", model.ConfigInstance.TTS.Engine)
}

func TestServeConfig_Patch_MossNanoSubConfigWithoutModelDir(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "moss-nano"
	cfg.TTS.MossNano.ModelDir = ""
	model.ConfigInstance = cfg

	// Saving sub-config when engine is already moss-nano and model_dir is empty should succeed
	// (model_dir is optional — empty value is valid, ResolveMossNanoModelDir handles it)
	body := `{"tts":{"moss_nano":{"voice":"Junhao"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Junhao", model.ConfigInstance.TTS.MossNano.Voice)
}

func TestServeConfig_Patch_InvalidDefaultAgent(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Set up agents so we can validate
	model.Agents = map[string]*model.Agent{"test": {ID: "test", Name: "Test", Backend: "test"}}
	model.AgentList = []*model.Agent{{ID: "test", Name: "Test", Backend: "test"}}
	defer func() { model.Agents = nil; model.AgentList = nil }()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"default_agent":"nonexistent"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "not found")
}

// --- PATCH needs_restart / cold-vs-hot field classification ---

func TestServeConfig_Patch_HotReloadFields_NoRestartNeeded(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Chat.CollapsedHeight = 150
	cfg.Upload.MaxSizeMB = 100
	model.ConfigInstance = cfg

	// Only hot-reload fields — no restart should be needed
	body := `{"chat":{"collapsed_height":200},"upload":{"max_size_mb":50}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.False(t, resp["needs_restart"].(bool), "needs_restart should be false when only hot-reload fields are changed")
	changed, ok := resp["changed_cold_fields"].([]any)
	assert.True(t, ok)
	assert.Empty(t, changed, "changed_cold_fields should be empty when only hot-reload fields are changed")

	assert.Equal(t, 200, model.ConfigInstance.Chat.CollapsedHeight)
	assert.Equal(t, 50, model.ConfigInstance.Upload.MaxSizeMB)
}

func TestServeConfig_Patch_ColdFields_NeedRestart(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Terminal.Enabled = true
	cfg.TTS.Engine = "edge"
	model.ConfigInstance = cfg

	// terminal.enabled is a cold field — restart should be needed
	body := `{"terminal":{"enabled":false},"tts":{"engine":"piper","piper":{"model_path":"/tmp/test.onnx"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.True(t, resp["needs_restart"].(bool), "needs_restart should be true when cold fields are changed")
	changed, ok := resp["changed_cold_fields"].([]any)
	assert.True(t, ok)
	assert.GreaterOrEqual(t, len(changed), 2)
	// Should contain the cold field paths
	changedStr := make([]string, len(changed))
	for i, v := range changed {
		changedStr[i] = fmt.Sprint(v)
	}
	assert.Contains(t, changedStr, "terminal.enabled")
	assert.Contains(t, changedStr, "tts.engine")
}

func TestServeConfig_Patch_MixedHotAndColdFields(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Chat.CollapsedHeight = 150
	cfg.Terminal.Enabled = true
	model.ConfigInstance = cfg

	// Mix of hot (chat.collapsed_height) and cold (terminal.enabled) fields
	body := `{"chat":{"collapsed_height":200},"terminal":{"enabled":false}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.True(t, resp["needs_restart"].(bool), "needs_restart should be true when any cold field is changed")
	changed, ok := resp["changed_cold_fields"].([]any)
	assert.True(t, ok)
	assert.Equal(t, 1, len(changed), "only cold fields should appear in changed_cold_fields")
	assert.Equal(t, "terminal.enabled", fmt.Sprint(changed[0]))
}

func TestServeConfig_Patch_SessionMaxCount_IsHotField(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Session.MaxCount = 10
	model.ConfigInstance = cfg

	// session.max_count is a hot-reload field — no restart should be needed
	body := `{"session":{"max_count":20}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.False(t, resp["needs_restart"].(bool), "session.max_count is hot-reloadable, should not need restart")
	changed, ok := resp["changed_cold_fields"].([]any)
	assert.True(t, ok)
	assert.Empty(t, changed)
}

// --- validatePatchValues additional coverage ---

func TestServeConfig_Patch_TTSFormatInvalid(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"tts":{"format":"ogg"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tts.format must be one of")
}

func TestServeConfig_Patch_TTSFormatEmptyAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	// Empty format string is allowed (means "use default")
	body := `{"tts":{"format":""}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeConfig_Patch_TTSSpeedTooLow(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"tts":{"speed":0.1}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tts.speed must be between 0.5 and 3.0")
}

func TestServeConfig_Patch_TTSSpeedTooHigh(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"tts":{"speed":5.0}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tts.speed must be between 0.5 and 3.0")
}

func TestServeConfig_Patch_PiperNoiseScaleInvalid(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"tts":{"piper":{"noise_scale":1.5}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tts.piper.noise_scale must be between 0 and 1")
}

func TestServeConfig_Patch_PiperNoiseScaleNegative(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"tts":{"piper":{"noise_scale":-0.1}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tts.piper.noise_scale must be between 0 and 1")
}

func TestServeConfig_Patch_PiperLengthScaleZero(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"tts":{"piper":{"length_scale":0}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tts.piper.length_scale must be positive")
}

func TestServeConfig_Patch_PiperLengthScaleNegative(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"tts":{"piper":{"length_scale":-1}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tts.piper.length_scale must be positive")
}

func TestServeConfig_Patch_PiperSentenceSilenceNegative(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"tts":{"piper":{"sentence_silence":-0.5}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tts.piper.sentence_silence must be non-negative")
}

func TestServeConfig_Patch_KokoroEmptyLang(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"tts":{"kokoro":{"lang":""}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tts.kokoro.lang must not be empty")
}

func TestServeConfig_Patch_TasksInvalidSummarizeBackend(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"summarize":{"backend":"nonexistent"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "summarize.backend must be one of")
}

func TestServeConfig_Patch_SessionNegativeMaxCount(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"session":{"max_count":-1}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "session.max_count must be non-negative")
}

func TestServeConfig_Patch_UploadNegativeMaxSizeMB(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"upload":{"max_size_mb":-1}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "upload.max_size_mb must be non-negative")
}

func TestServeConfig_Patch_UploadNegativeMaxFiles(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"upload":{"max_files":-1}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "upload.max_files must be non-negative")
}

func TestServeConfig_Patch_ChatNegativeSystemPromptInterval(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"chat":{"system_prompt_interval":-1}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "chat.system_prompt_interval must be non-negative")
}

// --- Cross-field consistency: summarize.backend api with base_url in patch ---

func TestServeConfig_Patch_TasksAPIWithBaseURLInPatch(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Summarize.API.BaseURL = ""
	cfg.Summarize.Backend = "simple"
	model.ConfigInstance = cfg

	// summarize.backend=api with summarize.api.base_url provided in same patch
	body := `{"summarize":{"backend":"api","api":{"base_url":"https://api.openai.com/v1"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- Cross-field: piper engine with model_path in same patch ---

func TestServeConfig_Patch_PiperEngineWithModelPathInPatch(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "edge"
	cfg.TTS.Piper.ModelPath = ""
	model.ConfigInstance = cfg

	// Engine=piper with model_path provided in same patch
	body := `{"tts":{"engine":"piper","piper":{"model_path":"/path/to/model.onnx"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "piper", model.ConfigInstance.TTS.Engine)
	assert.Equal(t, "/path/to/model.onnx", model.ConfigInstance.TTS.Piper.ModelPath)
}

// --- Cross-field: kokoro engine with both paths in same patch ---

func TestServeConfig_Patch_KokoroEngineWithPathsInPatch(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "edge"
	model.ConfigInstance = cfg

	body := `{"tts":{"engine":"kokoro","kokoro":{"model_path":"/path/to/kokoro.onnx","voices_path":"/path/to/voices.bin"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "kokoro", model.ConfigInstance.TTS.Engine)
}

func TestServeConfig_Patch_KokoroEngineWithoutVoicesPath(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "edge"
	cfg.TTS.Kokoro.ModelPath = "/path/to/kokoro.onnx"
	model.ConfigInstance = cfg

	// Engine switch should succeed even without voices_path — user fills sub-config later
	body := `{"tts":{"engine":"kokoro"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "kokoro", model.ConfigInstance.TTS.Engine)
}

// --- Cross-field: moss-nano engine with model_dir in same patch ---

func TestServeConfig_Patch_MossNanoEngineWithModelDirInPatch(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "edge"
	model.ConfigInstance = cfg

	body := `{"tts":{"engine":"moss-nano","moss_nano":{"model_dir":"/path/to/models"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "moss-nano", model.ConfigInstance.TTS.Engine)
}

// --- ServeConfigRestart with nil restartFunc ---

func TestServeConfigRestart_NilRestartFunc(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Set restartFunc to a function that signals when it's called.
	// This avoids DATA RACE: the goroutine inside ServeConfigRestart reads
	// restartFunc, and we must not concurrently write it back.
	origRestartFunc := restartFunc
	restartCalled := make(chan struct{})
	restartFunc = func() {
		close(restartCalled)
	}
	defer func() { restartFunc = origRestartFunc }()

	req := httptest.NewRequest(http.MethodPost, "/api/config/restart", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigRestart, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "restarting", resp["status"])

	// Wait for the goroutine to actually execute restartFunc
	// (restartGracePeriod = 200ms delay, then calls restartFunc)
	select {
	case <-restartCalled:
		// Success — goroutine finished reading restartFunc and called it
	case <-time.After(2 * time.Second):
		t.Fatal("restartFunc was not called within expected time")
	}
}

// --- validatePatchFields nested forbidden field ---

func TestServeConfig_Patch_ForbiddenNestedField(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Nested field that isn't in the patchable paths — e.g. ssh.host_key
	body := `{"ssh":{"host_key":"secret"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "forbidden_field")
}

func TestServeConfig_Patch_SummarizeAPIFormatInvalid(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"summarize":{"api":{"format":"invalid"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "summarize.api.format must be one of")
}

// --- recent_projects.max_count tests ---

func TestServeConfig_Get_RecentProjects(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.RecentProjects.MaxCount = 15
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)

	// recent_projects section should be present
	assert.Contains(t, resp, "recent_projects")
	rp, ok := resp["recent_projects"].(map[string]any)
	assert.True(t, ok, "recent_projects should be a map")
	assert.Equal(t, float64(15), rp["max_count"])
}

func TestServeConfig_Patch_RecentProjectsMaxCount(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.RecentProjects.MaxCount = 10
	model.ConfigInstance = cfg

	body := `{"recent_projects":{"max_count":20}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 20, model.ConfigInstance.RecentProjects.MaxCount)
	assert.Equal(t, 20, model.RecentProjectsMaxCount, "global variable should be updated via hot-reload")
}

func TestServeConfig_Patch_RecentProjectsMaxCount_IsHotField(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.RecentProjects.MaxCount = 10
	model.ConfigInstance = cfg

	// recent_projects.max_count is a hot-reload field — no restart should be needed
	body := `{"recent_projects":{"max_count":25}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.False(t, resp["needs_restart"].(bool), "recent_projects.max_count is hot-reloadable, should not need restart")
	changed, ok := resp["changed_cold_fields"].([]any)
	assert.True(t, ok)
	assert.Empty(t, changed)
}

func TestServeConfig_Patch_RecentProjectsMaxCount_ZeroRejected(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.RecentProjects.MaxCount = 10
	model.ConfigInstance = cfg

	// 0 should be rejected (min is 1)
	body := `{"recent_projects":{"max_count":0}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "recent_projects.max_count must be at least 1")
}

func TestServeConfig_Patch_RecentProjectsMaxCount_NegativeRejected(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"recent_projects":{"max_count":-5}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "recent_projects.max_count must be at least 1")
}

// --- Additional coverage: Kokoro with model_path in patch ---

func TestServeConfig_Patch_KokoroModelPathInPatch(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "kokoro"
	cfg.TTS.Kokoro.ModelPath = ""
	cfg.TTS.Kokoro.VoicesPath = ""
	model.ConfigInstance = cfg

	// Patch model_path and voices_path together when engine is already kokoro
	body := `{"tts":{"kokoro":{"model_path":"/path/to/kokoro.onnx","voices_path":"/path/to/voices.bin","lang":"en"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "/path/to/kokoro.onnx", model.ConfigInstance.TTS.Kokoro.ModelPath)
	assert.Equal(t, "/path/to/voices.bin", model.ConfigInstance.TTS.Kokoro.VoicesPath)
	assert.Equal(t, "en", model.ConfigInstance.TTS.Kokoro.Lang)
}

// --- MossNano with model_dir in patch (already set engine) ---

func TestServeConfig_Patch_MossNanoModelDirInPatch(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "moss-nano"
	cfg.TTS.MossNano.ModelDir = ""
	model.ConfigInstance = cfg

	// Patch model_dir when engine is already moss-nano
	body := `{"tts":{"moss_nano":{"model_dir":"/path/to/models","voice":"Test","backend":"onnx"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "/path/to/models", model.ConfigInstance.TTS.MossNano.ModelDir)
	assert.Equal(t, "Test", model.ConfigInstance.TTS.MossNano.Voice)
	assert.Equal(t, "onnx", model.ConfigInstance.TTS.MossNano.Backend)
}

// --- mergePatchIntoRaw: new nested map creation ---

func TestMergePatchIntoRaw_NewNestedKey(t *testing.T) {
	raw := map[string]any{
		"existing": "value",
	}

	patch := map[string]any{
		"new_key": map[string]any{
			"sub_key": "sub_value",
		},
	}

	mergePatchIntoRaw(raw, patch)

	assert.Equal(t, "value", raw["existing"])
	newKey, ok := raw["new_key"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "sub_value", newKey["sub_key"])
}

// --- getBuildVersion tests ---

func TestGetBuildVersion_FallbackVCS(t *testing.T) {
	origVersion := version.Version
	version.Version = ""
	defer func() { version.Version = origVersion }()

	v := getBuildVersion()
	assert.NotEmpty(t, v)
}

func TestGetBuildVersion_SetVersion(t *testing.T) {
	origVersion := version.Version
	version.Version = "v1.2.3"
	defer func() { version.Version = origVersion }()

	v := getBuildVersion()
	assert.Equal(t, "v1.2.3", v)
}

// --- serveConfigPatch error paths ---

func TestServeConfigPatch_BodyReadError(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPatch, "/api/config", errorReader{})
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// errorReader is an io.Reader that always returns an error.
type errorReader struct{}

func (errorReader) Read(_ []byte) (n int, err error) {
	return 0, fmt.Errorf("simulated read error")
}

func TestServeConfigPatch_ApplyConfigPatchError(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"rag":{"api_key":"sk-1***xyz"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "apply_failed")
}

func TestServeConfigPatch_WriteConfigYAMLError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("invalid BinDir path behavior differs on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping as root: root can create directories in non-existent paths")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping as root: root can create directories in non-existent paths")
	}
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	origBinDir := model.BinDir
	model.BinDir = "/nonexistent/path/that/cannot/be/created"
	defer func() { model.BinDir = origBinDir }()

	body := `{"chat":{"collapsed_height":200}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "write_failed")
}

// --- kokoro without voices_path (empty existing) ---

func TestServeConfig_Patch_KokoroWithoutVoicesPath(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "kokoro"
	cfg.TTS.Kokoro.ModelPath = "/path/to/model.onnx"
	cfg.TTS.Kokoro.VoicesPath = ""
	model.ConfigInstance = cfg

	body := `{"tts":{"kokoro":{"lang":"en"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "kokoro.voices_path is required")
}

func TestServeConfig_Patch_KokoroWithVoicesPathInPatch(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "kokoro"
	cfg.TTS.Kokoro.ModelPath = "/path/to/model.onnx"
	cfg.TTS.Kokoro.VoicesPath = ""
	model.ConfigInstance = cfg

	body := `{"tts":{"kokoro":{"voices_path":"/path/to/voices.bin","lang":"en"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "/path/to/voices.bin", model.ConfigInstance.TTS.Kokoro.VoicesPath)
}

// --- writeConfigYAML: no existing file ---

func TestServeConfigPatch_NoExistingConfig(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Chat.CollapsedHeight = 150
	model.ConfigInstance = cfg
	model.BinDir = t.TempDir()

	body := `{"chat":{"collapsed_height":200}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 200, model.ConfigInstance.Chat.CollapsedHeight)
}

// --- IsRunningUnderSupervisor ---

func TestIsRunningUnderSupervisor_EnvOverride(t *testing.T) {
	t.Setenv("CLAWBENCH_NO_SUPERVISOR", "1")

	assert.False(t, IsRunningUnderSupervisor())
}

func TestIsRunningUnderSupervisor_InvocationID(t *testing.T) {
	t.Setenv("CLAWBENCH_NO_SUPERVISOR", "")
	t.Setenv("INVOCATION_ID", "test-invocation-id")

	assert.True(t, IsRunningUnderSupervisor())
}

// --- ServeConfigPassword: auto-password file ---

func TestServeConfigPassword_WithAutoPasswordFile(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	password := "test-password"
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	model.SessionToken = hex.EncodeToString(hash[:])
	model.PasswordIsSHA256 = false
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	model.PasswordHash = bcryptHash
	model.ConfigInstance = model.Config{}

	binDir := t.TempDir()
	clawbenchDir := filepath.Join(binDir, ".clawbench")
	require.NoError(t, os.MkdirAll(clawbenchDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(clawbenchDir, "auto-password"), []byte("old-auto-password"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(binDir, "config"), 0o755))

	origBinDir := model.BinDir
	model.BinDir = binDir
	defer func() { model.BinDir = origBinDir }()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": password,
		"new_password":     "brand-new-password",
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusOK, w.Code)

	_, err := os.Stat(filepath.Join(clawbenchDir, "auto-password"))
	assert.True(t, os.IsNotExist(err), "auto-password file should be removed")
}

// --- ServeConfigRestart: nil restartFunc ---

func TestServeConfigRestart_NilRestartFuncWarn(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	origRestartFunc := restartFunc
	restartFunc = nil
	defer func() { restartFunc = origRestartFunc }()

	req := httptest.NewRequest(http.MethodPost, "/api/config/restart", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigRestart, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "restarting", resp["status"])

	time.Sleep(restartGracePeriod + 100*time.Millisecond)
}

// --- ServeConfigPassword: RemoteAddr without port ---

func TestServeConfigPassword_RemoteAddrNoPort(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	password := "test-password"
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	model.SessionToken = hex.EncodeToString(hash[:])
	model.PasswordIsSHA256 = false
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	model.PasswordHash = bcryptHash
	model.ConfigInstance = model.Config{}
	model.BinDir = t.TempDir()
	_ = os.MkdirAll(filepath.Join(model.BinDir, "config"), 0o755)

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": password,
		"new_password":     "brand-new-password",
	})
	req.RemoteAddr = "192.0.2.1"
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- writeConfigYAML: malformed existing config.yaml ---

func TestServeConfigPatch_MalformedExistingConfig(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Chat.CollapsedHeight = 150
	model.ConfigInstance = cfg

	binDir := t.TempDir()
	configDir := filepath.Join(binDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("{{invalid yaml:::"), 0o644))

	origBinDir := model.BinDir
	model.BinDir = binDir
	defer func() { model.BinDir = origBinDir }()

	body := `{"chat":{"collapsed_height":200}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 200, model.ConfigInstance.Chat.CollapsedHeight)
}

// --- applyConfigPatch: TTS model, voice, format, speed, max_cache_files ---

func TestServeConfigPatch_TTSModelAndVoice(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.TTS.Engine = "edge"
	model.ConfigInstance = cfg

	body := `{"tts":{"tts_model":"test-model","voice":"test-voice","format":"mp3","speed":1.5,"max_cache_files":200}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "test-model", model.ConfigInstance.TTS.TTSModel)
	assert.Equal(t, "test-voice", model.ConfigInstance.TTS.Voice)
	assert.Equal(t, "mp3", model.ConfigInstance.TTS.Format)
	assert.Equal(t, 1.5, model.ConfigInstance.TTS.Speed)
	assert.Equal(t, 200, model.ConfigInstance.TTS.MaxCacheFiles)
}

// --- applyConfigPatch: port forward, push, terminal, rag ---

func TestServeConfigPatch_PortForward(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"port_forward":{"enabled":true,"port":2222}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, model.ConfigInstance.PortForward.Enabled)
	assert.Equal(t, 2222, model.ConfigInstance.PortForward.Port)
}

func TestServeConfigPatch_PushJPush(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"push":{"jpush":{"enabled":true,"app_key":"test-key","master_secret":"test-secret-1234567890"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, model.ConfigInstance.Push.JPush.Enabled)
	assert.Equal(t, "test-key", model.ConfigInstance.Push.JPush.AppKey)
}

func TestServeConfigPatch_TerminalFields(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"terminal":{"enabled":true,"idle_timeout":"15m","max_sessions":5,"buffer_lines":5000}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, model.ConfigInstance.Terminal.Enabled)
	assert.Equal(t, "15m", model.ConfigInstance.Terminal.IdleTimeout)
	assert.Equal(t, 5, model.ConfigInstance.Terminal.MaxSessions)
	assert.Equal(t, 5000, model.ConfigInstance.Terminal.BufferLines)
}

func TestServeConfigPatch_RAGFields(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"rag":{"base_url":"http://localhost:11434","model":"bge-m3","api_key":"valid-full-key","chunk_size":256,"search_limit":10,"search_pool_size":100,"retention_days":60}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "http://localhost:11434", model.ConfigInstance.RAG.BaseURL)
	assert.Equal(t, "bge-m3", model.ConfigInstance.RAG.Model)
	assert.Equal(t, "valid-full-key", model.ConfigInstance.RAG.APIKey)
	assert.Equal(t, 256, model.ConfigInstance.RAG.ChunkSize)
	assert.Equal(t, 10, model.ConfigInstance.RAG.SearchLimit)
	assert.Equal(t, 100, model.ConfigInstance.RAG.SearchPoolSize)
	assert.Equal(t, 60, model.ConfigInstance.RAG.RetentionDays)
}

// --- serveConfigGet: RAG API key masking ---

func TestServeConfig_Get_RAGAPIKeyMasked(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.RAG.APIKey = "sk-1234567890abcdefghijklmnopqrstuvwxyz"
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	rag, _ := resp["rag"].(map[string]any)
	assert.Equal(t, "sk-1***xyz", rag["api_key"])
}

// --- validatePatchValues: default_agent with nil Agents ---

func TestServeConfig_Patch_DefaultAgentEmptyAgents(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	origAgents := model.Agents
	model.Agents = nil
	defer func() { model.Agents = origAgents }()

	body := `{"default_agent":"anything"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- validatePatchValues: summarize.api.base_url in patch while backend is api ---

func TestServeConfig_Patch_SummarizeAPIBaseURLInPatchWhileAPI(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Summarize.Backend = "api"
	cfg.Summarize.API.BaseURL = ""
	model.ConfigInstance = cfg

	body := `{"summarize":{"api":{"base_url":"https://api.openai.com/v1"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "https://api.openai.com/v1", model.ConfigInstance.Summarize.API.BaseURL)
}

// --- validatePatchValues: summarize.api.key with *** ---

func TestServeConfig_Patch_SummarizeAPIKeyMaskedRejected(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"summarize":{"api":{"key":"sk-1***xyz"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "must not contain '***'")
}

// --- validatePatchValues: summarize.api.format anthropic ---

func TestServeConfig_Patch_SummarizeAPIFormatAnthropic(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"summarize":{"api":{"base_url":"https://api.anthropic.com","format":"anthropic"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "anthropic", model.ConfigInstance.Summarize.API.Format)
}

// --- ServeConfigPassword: body read error ---

func TestServeConfigPassword_BodyReadError(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/config/password", errorReader{})
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, "sometoken")
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- writeConfigYAML: backup path ---

func TestServeConfigPatch_WithExistingConfigBackup(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Chat.CollapsedHeight = 150
	model.ConfigInstance = cfg

	binDir := t.TempDir()
	configDir := filepath.Join(binDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("chat:\n  collapsed_height: 100\n"), 0o644))

	origBinDir := model.BinDir
	model.BinDir = binDir
	defer func() { model.BinDir = origBinDir }()

	body := `{"chat":{"collapsed_height":200}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 200, model.ConfigInstance.Chat.CollapsedHeight)

	_, err := os.Stat(filepath.Join(configDir, "config.yaml.bak"))
	assert.NoError(t, err, "backup file should exist")
}
