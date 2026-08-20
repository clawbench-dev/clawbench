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
	"clawbench/internal/speech"
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
	cfg.RAG.SearchLimit = 20
	cfg.RAG.RetentionDays = 30
	cfg.PortForward.Enabled = true
	cfg.PortForward.Port = 20001
	cfg.Summarize.TTSBackend = "simple"
	cfg.AISummary.Model = ""
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
	assert.Contains(t, resp, "summarize")

	// Verify specific values
	chat, _ := resp["chat"].(map[string]any)
	assert.Equal(t, float64(5), chat["system_prompt_interval"])
	assert.Equal(t, float64(15), chat["initial_messages"])

	upload, _ := resp["upload"].(map[string]any)
	assert.Equal(t, float64(50), upload["max_size_mb"])

	terminal, _ := resp["terminal"].(map[string]any)
	assert.Equal(t, true, terminal["enabled"])
	assert.Equal(t, "10m", terminal["idle_timeout"])

	// Verify summarize section
	summarize, _ := resp["summarize"].(map[string]any)
	assert.Equal(t, "simple", summarize["tts_backend"])

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
	assert.NotContains(t, resp, "host")
	assert.NotContains(t, resp, "port")
	assert.NotContains(t, resp, "log_level")
	assert.NotContains(t, resp, "log_dir")
	assert.NotContains(t, resp, "watch_dir")
	assert.NotContains(t, resp, "dev_port")

	// TLS exposes only the cert directory path and active state — never the
	// legacy private key path or PEM contents.
	tls, _ := resp["tls"].(map[string]any)
	assert.Contains(t, tls, "cert_dir")
	assert.Contains(t, tls, "active")
	assert.NotContains(t, tls, "key_file")
	assert.NotContains(t, tls, "cert_file")
	assert.NotContains(t, tls, "enabled")

	// Verify port_forward doesn't expose host_key
	pf, _ := resp["port_forward"].(map[string]any)
	assert.NotContains(t, pf, "host_key")
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
	cfg.TTS.Voice = "Junhao"
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
	// voice is now the shared tts.voice field, not moss_nano.voice
	assert.Equal(t, "Junhao", tts["voice"])
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
	cfg.Chat.SystemPromptInterval = 10
	cfg.Upload.MaxSizeMB = 100
	model.ConfigInstance = cfg

	body := `{"chat":{"system_prompt_interval":20},"upload":{"max_size_mb":50}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	// Both chat.system_prompt_interval and upload.max_size_mb are hot-reload fields
	assert.False(t, resp["needs_restart"].(bool), "hot-reload fields should not need restart")
	changed, ok := resp["changed_cold_fields"].([]any)
	assert.True(t, ok)
	assert.Empty(t, changed, "no cold fields should be reported for hot-reload changes")

	assert.Equal(t, 20, model.ConfigInstance.Chat.SystemPromptInterval)
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

func TestServeConfig_Patch_ForbiddenField_TLSLegacyEnabled(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Legacy tls.enabled is deprecated and no longer patchable.
	body := `{"tls":{"enabled":false}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfig_Patch_TLSCertDir(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := `{"tls":{"cert_dir":"/custom/certs"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "/custom/certs", model.ConfigInstance.TLS.CertDir)
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

func TestServeConfig_Patch_InvalidSummarizeTtsBackend(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := `{"summarize":{"tts_backend":"nonexistent"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "summarize.tts_backend must be one of")
}

func TestServeConfig_Patch_NegativeNumber(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := `{"chat":{"initial_messages":-1}}`
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

	// Saving voice when engine is already moss-nano should succeed
	// (voice is now the shared tts.voice field)
	body := `{"tts":{"voice":"Junhao"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Junhao", model.ConfigInstance.TTS.Voice)
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
	cfg.Chat.SystemPromptInterval = 10
	cfg.Upload.MaxSizeMB = 100
	model.ConfigInstance = cfg

	// Only hot-reload fields — no restart should be needed
	body := `{"chat":{"system_prompt_interval":20},"upload":{"max_size_mb":50}}`
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

	assert.Equal(t, 20, model.ConfigInstance.Chat.SystemPromptInterval)
	assert.Equal(t, 50, model.ConfigInstance.Upload.MaxSizeMB)
}

func TestServeConfig_Patch_ColdFields_NeedRestart(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.PortForward.Enabled = false
	cfg.RAG.BaseURL = "http://localhost:11434"
	model.ConfigInstance = cfg

	// All patchable fields are now hot-reload — no restart should be needed
	body := `{"port_forward":{"enabled":true},"rag":{"base_url":"http://other:11434"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.False(t, resp["needs_restart"].(bool), "needs_restart should be false when all changed fields are hot-reload")
	changed, ok := resp["changed_cold_fields"].([]any)
	assert.True(t, ok)
	assert.Equal(t, 0, len(changed), "no cold fields should remain")
}

func TestServeConfig_Patch_MixedHotAndColdFields(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Chat.SystemPromptInterval = 10
	cfg.PortForward.Enabled = false
	model.ConfigInstance = cfg

	// Mix of hot (chat.system_prompt_interval, port_forward.enabled) fields
	// port_forward.enabled is now a hot-reload field (SSH tunnel can be toggled at runtime)
	body := `{"chat":{"system_prompt_interval":20},"port_forward":{"enabled":true}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.False(t, resp["needs_restart"].(bool), "needs_restart should be false when all changed fields are hot-reload")
	changed, ok := resp["changed_cold_fields"].([]any)
	assert.True(t, ok)
	assert.Equal(t, 0, len(changed), "no cold fields should appear when all are hot-reload")
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

func TestServeConfig_Patch_ACPMaxLiveConns_IsHotField(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.ACP.MaxLiveConns = 10
	model.ConfigInstance = cfg

	// acp.max_live_conns is a hot-reload field — no restart should be needed
	body := `{"acp":{"max_live_conns":5}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.False(t, resp["needs_restart"].(bool), "acp.max_live_conns is hot-reloadable, should not need restart")
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
	body := `{"tts":{"moss_nano":{"model_dir":"/path/to/models","backend":"onnx"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "/path/to/models", model.ConfigInstance.TTS.MossNano.ModelDir)
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

	// API key containing *** is now accepted (maskAPIKey removed)
	body := `{"rag":{"api_key":"sk-1***xyz"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
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

	origDataDir := model.DataDir
	model.DataDir = "/nonexistent/path/that/cannot/be/created"
	defer func() { model.DataDir = origDataDir }()

	body := `{"chat":{"system_prompt_interval":20}}`
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
	cfg.Chat.SystemPromptInterval = 10
	model.ConfigInstance = cfg
	model.BinDir = t.TempDir()

	body := `{"chat":{"system_prompt_interval":20}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 20, model.ConfigInstance.Chat.SystemPromptInterval)
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
	origDataDir := model.DataDir
	model.BinDir = binDir
	model.DataDir = clawbenchDir
	defer func() { model.BinDir = origBinDir; model.DataDir = origDataDir }()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": password,
		"new_password":     "brand-new1",
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
	model.DataDir = model.BinDir
	_ = os.MkdirAll(filepath.Join(model.DataDir, "config"), 0o755)

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": password,
		"new_password":     "brand-new1",
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
	cfg.Chat.SystemPromptInterval = 10
	model.ConfigInstance = cfg

	binDir := t.TempDir()
	configDir := filepath.Join(binDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("{{invalid yaml:::"), 0o644))

	origBinDir := model.BinDir
	model.BinDir = binDir
	defer func() { model.BinDir = origBinDir }()

	body := `{"chat":{"system_prompt_interval":20}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 20, model.ConfigInstance.Chat.SystemPromptInterval)
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

	body := `{"rag":{"vector_enabled":false,"base_url":"http://localhost:11434","model":"bge-m3","api_key":"valid-full-key","chunk_size":256,"search_limit":10,"search_pool_size":100,"retention_days":60}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, model.ConfigInstance.RAG.VectorEnabled)
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
	assert.Equal(t, "sk-1234567890abcdefghijklmnopqrstuvwxyz", rag["api_key"])
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
	cfg.Chat.SystemPromptInterval = 10
	model.ConfigInstance = cfg

	binDir := t.TempDir()
	configDir := filepath.Join(binDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("chat:\n  system_prompt_interval: 5\n"), 0o644))

	origDataDir := model.DataDir
	model.DataDir = binDir
	defer func() { model.DataDir = origDataDir }()

	body := `{"chat":{"system_prompt_interval":20}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 20, model.ConfigInstance.Chat.SystemPromptInterval)

	_, err := os.Stat(filepath.Join(configDir, "config.yaml.bak"))
	assert.NoError(t, err, "backup file should exist")
}

func TestApplyHotReloadGlobals_TTSVoice_EdgeTTS(t *testing.T) {
	origProvider := GetSpeechProvider()
	defer SetSpeechProvider(origProvider)

	SetSpeechProvider(&speech.EdgeTTSProvider{Voice: "original-voice", Rate: "+0%"})
	model.ConfigInstance.TTS.Voice = "new-voice"
	model.ConfigInstance.TTS.Speed = 0 // don't trigger speed logic

	applyHotReloadGlobals()

	p := GetSpeechProvider().(*speech.EdgeTTSProvider)
	assert.Equal(t, "new-voice", p.Voice)
}

func TestApplyHotReloadGlobals_TTSVoice_Kokoro(t *testing.T) {
	origProvider := GetSpeechProvider()
	defer SetSpeechProvider(origProvider)

	SetSpeechProvider(&speech.KokoroProvider{Voice: "original-voice"})
	model.ConfigInstance.TTS.Voice = "new-kokoro-voice"
	model.ConfigInstance.TTS.Speed = 0

	applyHotReloadGlobals()

	p := GetSpeechProvider().(*speech.KokoroProvider)
	assert.Equal(t, "new-kokoro-voice", p.Voice)
}

func TestApplyHotReloadGlobals_TTSSpeed_EdgeTTS(t *testing.T) {
	origProvider := GetSpeechProvider()
	defer SetSpeechProvider(origProvider)

	SetSpeechProvider(&speech.EdgeTTSProvider{Rate: "+0%"})
	model.ConfigInstance.TTS.Voice = ""
	model.ConfigInstance.TTS.Speed = 1.5 // 50% faster

	applyHotReloadGlobals()

	p := GetSpeechProvider().(*speech.EdgeTTSProvider)
	assert.Equal(t, "+50%", p.Rate)
}

func TestApplyHotReloadGlobals_TTSSlow_EdgeTTS(t *testing.T) {
	origProvider := GetSpeechProvider()
	defer SetSpeechProvider(origProvider)

	SetSpeechProvider(&speech.EdgeTTSProvider{Rate: "+0%"})
	model.ConfigInstance.TTS.Voice = ""
	model.ConfigInstance.TTS.Speed = 0.5 // 50% slower => -50%

	applyHotReloadGlobals()

	p := GetSpeechProvider().(*speech.EdgeTTSProvider)
	assert.Equal(t, "-50%", p.Rate)
}

func TestApplyHotReloadGlobals_TTSSpeed_Kokoro(t *testing.T) {
	origProvider := GetSpeechProvider()
	defer SetSpeechProvider(origProvider)

	SetSpeechProvider(&speech.KokoroProvider{Speed: 1.0})
	model.ConfigInstance.TTS.Voice = ""
	model.ConfigInstance.TTS.Speed = 1.2

	applyHotReloadGlobals()

	p := GetSpeechProvider().(*speech.KokoroProvider)
	assert.Equal(t, 1.2, p.Speed)
}

func TestApplyHotReloadGlobals_TTSSpeed_Piper(t *testing.T) {
	origProvider := GetSpeechProvider()
	defer SetSpeechProvider(origProvider)

	SetSpeechProvider(&speech.PiperProvider{LengthScale: 1.0})
	model.ConfigInstance.TTS.Voice = ""
	model.ConfigInstance.TTS.Speed = 2.0
	model.ConfigInstance.TTS.Piper.LengthScale = 0 // not explicitly set

	applyHotReloadGlobals()

	p := GetSpeechProvider().(*speech.PiperProvider)
	assert.InDelta(t, 0.5, p.LengthScale, 0.01)
}

func TestServeConfig_Get_LocalhostAuthExempt(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.LocalhostAuthExempt = true

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, true, resp["localhost_auth_exempt"])
}

func TestServeConfig_Patch_LocalhostAuthExempt(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.LocalhostAuthExempt = false

	body := `{"localhost_auth_exempt":true}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, model.ConfigInstance.LocalhostAuthExempt)
	assert.True(t, model.LocalhostAuthExempt)
}

// --- SetReconfigureFunc and reconfigureOnHotReload ---

func TestSetReconfigureFunc(t *testing.T) {
	called := false
	SetReconfigureFunc(func() {
		called = true
	})
	defer func() { reconfigureOnHotReload = nil }()

	// Invoke the set function directly
	reconfigureOnHotReload()
	assert.True(t, called, "reconfigureOnHotReload should call the function set by SetReconfigureFunc")
}

func TestReconfigureOnHotReload_CalledDuringPatch(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	reconfigureCalled := false
	SetReconfigureFunc(func() {
		reconfigureCalled = true
	})
	defer func() { reconfigureOnHotReload = nil }()

	cfg := model.Config{}
	cfg.Chat.SystemPromptInterval = 10
	model.ConfigInstance = cfg

	// Patch a hot-reload field — should trigger reconfigureOnHotReload
	body := `{"chat":{"system_prompt_interval":20}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, reconfigureCalled, "reconfigureOnHotReload should be called when hot-reload fields are patched")
}

func TestReconfigureOnHotReload_NilDoesNotPanic(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Ensure reconfigureOnHotReload is nil (default in tests)
	reconfigureOnHotReload = nil

	cfg := model.Config{}
	cfg.Chat.SystemPromptInterval = 10
	model.ConfigInstance = cfg

	// Patch should succeed without panicking even when reconfigureOnHotReload is nil
	body := `{"chat":{"system_prompt_interval":20}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeConfig_Patch_LocalhostAuthExempt_IsHotField(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.LocalhostAuthExempt = false

	// localhost_auth_exempt is a hot-reload field — no restart should be needed
	body := `{"localhost_auth_exempt":true}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.False(t, resp["needs_restart"].(bool), "localhost_auth_exempt is hot-reloadable, should not need restart")
}

// --- FRP config validation tests ---

func TestServeConfig_Patch_FRPEnabledWithoutServerAddr_SwitchOn(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.FRP.Enabled = false
	cfg.FRP.ServerAddr = ""
	model.ConfigInstance = cfg

	// Switching FRP on without server_addr should succeed — user fills sub-config later
	body := `{"frp":{"enabled":true}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeConfig_Patch_FRPEnabledWithServerAddr(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.FRP.Enabled = true
	cfg.FRP.ServerAddr = "frp.example.com"
	model.ConfigInstance = cfg

	// FRP already enabled with server_addr — changing server_addr should work
	body := `{"frp":{"server_addr":"new-frp.example.com"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "new-frp.example.com", model.ConfigInstance.FRP.ServerAddr)
}

func TestServeConfig_Patch_FRPEnabledNoAddr_NotSwitchingOn(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.FRP.Enabled = true
	cfg.FRP.ServerAddr = ""
	model.ConfigInstance = cfg

	// FRP is already enabled but server_addr is empty — changing other field should fail
	body := `{"frp":{"auto_port":true}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "frp.server_addr is required")
}

func TestServeConfig_Patch_FRPServerPortOutOfRange(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.FRP.Enabled = true
	cfg.FRP.ServerAddr = "frp.example.com"
	model.ConfigInstance = cfg

	body := `{"frp":{"server_port":70000}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "frp.server_port must be between 0 and 65535")
}

func TestServeConfig_Patch_FRPRemotePortOutOfRange(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.FRP.Enabled = true
	cfg.FRP.ServerAddr = "frp.example.com"
	model.ConfigInstance = cfg

	body := `{"frp":{"remote_port":-1}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "frp.remote_port must be between 0 and 65535")
}

func TestServeConfig_Patch_FRPSSHRemotePortOutOfRange(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.FRP.Enabled = true
	cfg.FRP.ServerAddr = "frp.example.com"
	model.ConfigInstance = cfg

	body := `{"frp":{"ssh_remote_port":99999}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "frp.ssh_remote_port must be between 0 and 65535")
}

func TestServeConfig_Patch_FRPFields(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.FRP.Enabled = true
	cfg.FRP.ServerAddr = "frp.example.com"
	model.ConfigInstance = cfg

	body := `{"frp":{"server_addr":"frp2.example.com","server_port":7001,"token":"new-token","auto_port":true,"remote_port":0,"ssh_remote_port":0}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "frp2.example.com", model.ConfigInstance.FRP.ServerAddr)
	assert.Equal(t, 7001, model.ConfigInstance.FRP.ServerPort)
	assert.Equal(t, "new-token", model.ConfigInstance.FRP.Token)
	assert.True(t, model.ConfigInstance.FRP.AutoPort)
}

func TestServeConfig_Patch_FileSearchDisplayLimit(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}

	body := `{"file_search":{"display_limit":50}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 50, model.ConfigInstance.FileSearch.DisplayLimit)
}

func TestServeConfig_Patch_FileSearchDisplayLimitOutOfRange(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}

	body := `{"file_search":{"display_limit":5}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "file_search.display_limit must be between 10 and 500")
}

// --- ServeConfigPassword: validation tests ---

func TestServeConfigPassword_Validation_EmptyPassword(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "",
		"new_password":     "brand-new1",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "empty_password")
}

func TestServeConfigPassword_Validation_TooShort(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "test",
		"new_password":     "short1",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "password_too_short")
}

func TestServeConfigPassword_Validation_TooLong(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "test",
		"new_password":     strings.Repeat("a", 33) + "1",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "password_too_long")
}

func TestServeConfigPassword_Validation_NoDigit(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "test",
		"new_password":     "onlylettershere",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "password_no_letter_digit")
}

func TestServeConfigPassword_Validation_NoLetter(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "test",
		"new_password":     "12345678",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "password_no_letter_digit")
}

func TestServeConfigPassword_Validation_InvalidJSON(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/config/password", strings.NewReader(`{invalid json`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfigPassword_Validation_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/config/password", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- maskAPIKey tests (removed) ---
// maskAPIKey was removed — config API now returns full values for password fields.

// --- shellQuote tests ---

func TestShellQuote_NoSpecial(t *testing.T) {
	assert.Equal(t, "'hello'", shellQuote("hello"))
}

func TestShellQuote_WithSingleQuote(t *testing.T) {
	assert.Equal(t, "'it'\\''s'", shellQuote("it's"))
}

func TestShellQuote_Empty(t *testing.T) {
	assert.Equal(t, "''", shellQuote(""))
}

// --- joinArgs tests ---

func TestJoinArgs_Settings(t *testing.T) {
	assert.Equal(t, "'a' 'b' 'c'", joinArgs([]string{"a", "b", "c"}))
}

func TestJoinArgs_SettingsEmpty(t *testing.T) {
	assert.Equal(t, "", joinArgs([]string{}))
}

func TestJoinArgs_SettingsSingleArg(t *testing.T) {
	assert.Equal(t, "'hello'", joinArgs([]string{"hello"}))
}

// --- copyFile tests (indirectly via writeConfigYAML backup) ---

func TestServeConfig_Get_FRPFields(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.FRP.Enabled = true
	cfg.FRP.ServerAddr = "frp.example.com"
	cfg.FRP.ServerPort = 7000
	cfg.FRP.Token = "long-secret-token-value-here"
	cfg.FRP.AutoPort = true
	cfg.FRP.RemotePort = 20050
	cfg.FRP.SSHRemotePort = 20051
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	frp, ok := resp["frp"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, true, frp["enabled"])
	assert.Equal(t, "frp.example.com", frp["server_addr"])
	assert.Equal(t, float64(7000), frp["server_port"])
	// Token is returned in full (frontend uses type="password" for secure display)
	assert.Equal(t, "long-secret-token-value-here", frp["token"])
	assert.Equal(t, true, frp["auto_port"])
}

func TestTriggerRestart(t *testing.T) {
	called := false
	origRestartFunc := restartFunc
	restartFunc = func() { called = true }
	defer func() { restartFunc = origRestartFunc }()

	TriggerRestart()
	assert.True(t, called, "TriggerRestart should call the configured restartFunc")
}

func TestTriggerRestart_NilFunc(t *testing.T) {
	origRestartFunc := restartFunc
	restartFunc = nil
	defer func() { restartFunc = origRestartFunc }()

	// Should not panic when restartFunc is nil
	TriggerRestart()
}

// --- STT config: applyConfigPatch, validation, GET response ---

func TestApplyConfigPatch_STT(t *testing.T) {
	origConfig := model.ConfigInstance
	defer func() { model.ConfigInstance = origConfig }()

	applyConfigPatch(map[string]any{
		"stt": map[string]any{
			"base_url":     "http://localhost:9000/v1",
			"api_key":      "k",
			"model":        "whisper-small",
			"language":     "en",
			"streaming":    true,
			"chunk_ms":     float64(800),
			"shortcut_key": "Ctrl+M",
		},
	})
	cfg2 := model.ConfigInstance
	if cfg2.STT.BaseURL != "http://localhost:9000/v1" {
		t.Errorf("STT.BaseURL = %q, want http://localhost:9000/v1", cfg2.STT.BaseURL)
	}
	if cfg2.STT.APIKey != "k" {
		t.Errorf("STT.APIKey = %q, want k", cfg2.STT.APIKey)
	}
	if cfg2.STT.Model != "whisper-small" {
		t.Errorf("STT.Model = %q, want whisper-small", cfg2.STT.Model)
	}
	if cfg2.STT.Language != "en" {
		t.Errorf("STT.Language = %q, want en", cfg2.STT.Language)
	}
	if !cfg2.STT.Streaming {
		t.Error("STT.Streaming = false, want true")
	}
	if cfg2.STT.ChunkMs != 800 {
		t.Errorf("STT.ChunkMs = %d, want 800", cfg2.STT.ChunkMs)
	}
	if cfg2.STT.ShortcutKey != "Ctrl+M" {
		t.Errorf("STT.ShortcutKey = %q, want Ctrl+M", cfg2.STT.ShortcutKey)
	}
}

func TestValidatePatchValues_STTBadURL(t *testing.T) {
	err := validatePatchValues(map[string]any{
		"stt": map[string]any{"base_url": "://bad"},
	})
	if err == nil {
		t.Fatal("expected error for invalid stt.base_url, got nil")
	}
}

func TestServeConfig_Get_STT(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.STT.BaseURL = "http://localhost:9000/v1"
	cfg.STT.APIKey = "k"
	cfg.STT.Model = "whisper-small"
	cfg.STT.Language = "en"
	cfg.STT.Streaming = true
	cfg.STT.ChunkMs = 800
	cfg.STT.ShortcutKey = "Ctrl+M"
	model.ConfigInstance = cfg

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	stt, ok := resp["stt"].(map[string]any)
	require.True(t, ok, "response should contain stt section")
	assert.Equal(t, "http://localhost:9000/v1", stt["base_url"])
	assert.Equal(t, "k", stt["api_key"])
	assert.Equal(t, "whisper-small", stt["model"])
	assert.Equal(t, "en", stt["language"])
	assert.Equal(t, true, stt["streaming"])
	assert.Equal(t, float64(800), stt["chunk_ms"])
	assert.Equal(t, "Ctrl+M", stt["shortcut_key"])
}
