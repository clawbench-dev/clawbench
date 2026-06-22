package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// setupPersistTestEnv sets up a test environment with BinDir configured
// so that writeConfigYAML actually writes to disk.
func setupPersistTestEnv(t *testing.T) (*testEnv, func()) { //nolint:unparam // test helper: testEnv used implicitly via global state
	t.Helper()
	env, teardown := setupTestEnv(t)

	// Set BinDir to a temp directory so config.yaml gets written there
	origBinDir := model.BinDir
	tmpDir := t.TempDir()
	model.BinDir = tmpDir

	// Also need config dir
	_ = os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755)

	origConfig := model.ConfigInstance

	cleanup := func() {
		model.BinDir = origBinDir
		model.ConfigInstance = origConfig
		teardown()
	}

	return env, cleanup
}

// patchAndReadConfig sends a PATCH request and reads back config.yaml to verify persistence.
func patchAndReadConfig(t *testing.T, body string) map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code, "PATCH should succeed: %s", w.Body.String())

	// Read config.yaml from disk
	configPath := filepath.Join(model.BinDir, "config", "config.yaml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err, "config.yaml should exist after PATCH")

	var cfg map[string]any
	err = yaml.Unmarshal(data, &cfg)
	require.NoError(t, err, "config.yaml should be valid YAML")

	return cfg
}

// getNestedValue reads a dot-path value from a nested map.
func getNestedValue(m map[string]any, path string) any {
	parts := strings.Split(path, ".")
	var current any = m
	for _, p := range parts {
		cm, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = cm[p]
		if !ok {
			return nil
		}
	}
	return current
}

// ─── Top-level fields ──────────────────────────────────────

func TestPersist_DefaultAgent(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"default_agent":"claude"}`)
	assert.Equal(t, "claude", getNestedValue(cfg, "default_agent"))
}

// ─── Chat section ──────────────────────────────────────

func TestPersist_ChatInitialMessages(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"chat":{"initial_messages":30}}`)
	assert.Equal(t, 30, getNestedValue(cfg, "chat.initial_messages"))
}

func TestPersist_ChatPageSize(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"chat":{"page_size":50}}`)
	assert.Equal(t, 50, getNestedValue(cfg, "chat.page_size"))
}

func TestPersist_ChatSystemPromptInterval(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"chat":{"system_prompt_interval":5}}`)
	assert.Equal(t, 5, getNestedValue(cfg, "chat.system_prompt_interval"))
}

// ─── Session section ──────────────────────────────────────

func TestPersist_SessionMaxCount(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"session":{"max_count":20}}`)
	assert.Equal(t, 20, getNestedValue(cfg, "session.max_count"))
}

// ─── Upload section ──────────────────────────────────────

func TestPersist_UploadMaxSizeMB(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"upload":{"max_size_mb":200}}`)
	assert.Equal(t, 200, getNestedValue(cfg, "upload.max_size_mb"))
}

func TestPersist_UploadMaxFiles(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"upload":{"max_files":50}}`)
	assert.Equal(t, 50, getNestedValue(cfg, "upload.max_files"))
}

// ─── Terminal section ──────────────────────────────────────

func TestPersist_TerminalEnabled(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"terminal":{"enabled":false}}`)
	assert.Equal(t, false, getNestedValue(cfg, "terminal.enabled"))
}

func TestPersist_TerminalIdleTimeout(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"terminal":{"idle_timeout":"30m"}}`)
	assert.Equal(t, "30m", getNestedValue(cfg, "terminal.idle_timeout"))
}

func TestPersist_TerminalMaxSessions(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"terminal":{"max_sessions":5}}`)
	assert.Equal(t, 5, getNestedValue(cfg, "terminal.max_sessions"))
}

func TestPersist_TerminalBufferLines(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"terminal":{"buffer_lines":5000}}`)
	assert.Equal(t, 5000, getNestedValue(cfg, "terminal.buffer_lines"))
}

// ─── TTS core ──────────────────────────────────────

func TestPersist_TTSEngine(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"engine":"piper","piper":{"model_path":"/tmp/test.onnx"}}}`)
	assert.Equal(t, "piper", getNestedValue(cfg, "tts.engine"))
}

func TestPersist_TTSVoice(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"voice":"zh-CN-XiaoxiaoNeural"}}`)
	assert.Equal(t, "zh-CN-XiaoxiaoNeural", getNestedValue(cfg, "tts.voice"))
}

func TestPersist_TTSSpeed(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"speed":1.5}}`)
	assert.Equal(t, 1.5, getNestedValue(cfg, "tts.speed"))
}

func TestPersist_TTSModel(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"tts_model":"Speech-2.8-Turbo"}}`)
	assert.Equal(t, "Speech-2.8-Turbo", getNestedValue(cfg, "tts.tts_model"))
}

func TestPersist_TTSFormat(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"format":"mp3"}}`)
	assert.Equal(t, "mp3", getNestedValue(cfg, "tts.format"))
}

func TestPersist_SummarizeBackend(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	// Set up initial config with base_url so the cross-field check passes
	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Summarize.API.BaseURL = "https://api.openai.com/v1"

	cfg := patchAndReadConfig(t, `{"summarize":{"backend":"api"}}`)
	assert.Equal(t, "api", getNestedValue(cfg, "summarize.backend"))
}

func TestPersist_SummarizeModel(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"summarize":{"model":"gpt-4o-mini"}}`)
	assert.Equal(t, "gpt-4o-mini", getNestedValue(cfg, "summarize.model"))
}

func TestPersist_TTSMaxCacheFiles(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"max_cache_files":200}}`)
	assert.Equal(t, 200, getNestedValue(cfg, "tts.max_cache_files"))
}

// ─── TTS Piper sub-config ──────────────────────────────────────

func TestPersist_PiperModelPath(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"piper":{"model_path":"/path/to/model.onnx"}}}`)
	assert.Equal(t, "/path/to/model.onnx", getNestedValue(cfg, "tts.piper.model_path"))
}

func TestPersist_PiperNoiseScale(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"piper":{"noise_scale":0.5}}}`)
	assert.Equal(t, 0.5, getNestedValue(cfg, "tts.piper.noise_scale"))
}

func TestPersist_PiperLengthScale(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"piper":{"length_scale":1.2}}}`)
	assert.Equal(t, 1.2, getNestedValue(cfg, "tts.piper.length_scale"))
}

func TestPersist_PiperSentenceSilence(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"piper":{"sentence_silence":0.3}}}`)
	assert.Equal(t, 0.3, getNestedValue(cfg, "tts.piper.sentence_silence"))
}

// ─── TTS Kokoro sub-config ──────────────────────────────────────

func TestPersist_KokoroModelPath(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"kokoro":{"model_path":"/path/to/kokoro.onnx"}}}`)
	assert.Equal(t, "/path/to/kokoro.onnx", getNestedValue(cfg, "tts.kokoro.model_path"))
}

func TestPersist_KokoroVoicesPath(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"kokoro":{"voices_path":"/path/to/voices.bin"}}}`)
	assert.Equal(t, "/path/to/voices.bin", getNestedValue(cfg, "tts.kokoro.voices_path"))
}

func TestPersist_KokoroLang(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"kokoro":{"lang":"en"}}}`)
	assert.Equal(t, "en", getNestedValue(cfg, "tts.kokoro.lang"))
}

// ─── TTS MossNano sub-config ──────────────────────────────────────

func TestPersist_MossNanoModelDir(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"moss_nano":{"model_dir":"/path/to/models"}}}`)
	assert.Equal(t, "/path/to/models", getNestedValue(cfg, "tts.moss_nano.model_dir"))
}

func TestPersist_MossNanoPromptSpeech(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"moss_nano":{"prompt_speech":"/path/to/ref.wav"}}}`)
	assert.Equal(t, "/path/to/ref.wav", getNestedValue(cfg, "tts.moss_nano.prompt_speech"))
}

func TestPersist_MossNanoVoice(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"moss_nano":{"voice":"Xiaoxiao"}}}`)
	assert.Equal(t, "Xiaoxiao", getNestedValue(cfg, "tts.moss_nano.voice"))
}

func TestPersist_MossNanoBackend(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"tts":{"moss_nano":{"backend":"pytorch"}}}`)
	assert.Equal(t, "pytorch", getNestedValue(cfg, "tts.moss_nano.backend"))
}

// ─── TTS API sub-config ──────────────────────────────────────

func TestPersist_APIBaseURL(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"summarize":{"api":{"base_url":"https://api.openai.com/v1/chat"}}}`)
	assert.Equal(t, "https://api.openai.com/v1/chat", getNestedValue(cfg, "summarize.api.base_url"))
}

func TestPersist_APIKey(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"summarize":{"api":{"key":"sk-1234567890abcdef"}}}`)
	assert.Equal(t, "sk-1234567890abcdef", getNestedValue(cfg, "summarize.api.key"))
}

func TestPersist_APIFormat(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"summarize":{"api":{"format":"anthropic"}}}`)
	assert.Equal(t, "anthropic", getNestedValue(cfg, "summarize.api.format"))
}

// ─── RAG section ──────────────────────────────────────

func TestPersist_RAGSearchPoolSize(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"rag":{"search_pool_size":30}}`)
	assert.Equal(t, 30, getNestedValue(cfg, "rag.search_pool_size"))
}

func TestPersist_RAGBaseURL(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"rag":{"base_url":"http://ollama:11434"}}`)
	assert.Equal(t, "http://ollama:11434", getNestedValue(cfg, "rag.base_url"))
}

func TestPersist_RAGModel(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"rag":{"model":"nomic-embed"}}`)
	assert.Equal(t, "nomic-embed", getNestedValue(cfg, "rag.model"))
}

func TestPersist_RAGChunkSize(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"rag":{"chunk_size":1024}}`)
	assert.Equal(t, 1024, getNestedValue(cfg, "rag.chunk_size"))
}

func TestPersist_RAGSearchLimit(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"rag":{"search_limit":10}}`)
	assert.Equal(t, 10, getNestedValue(cfg, "rag.search_limit"))
}

func TestPersist_RAGRetentionDays(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"rag":{"retention_days":30}}`)
	assert.Equal(t, 30, getNestedValue(cfg, "rag.retention_days"))
}

// ─── Port Forward section ──────────────────────────────────────

func TestPersist_PortForwardEnabled(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"port_forward":{"enabled":false}}`)
	assert.Equal(t, false, getNestedValue(cfg, "port_forward.enabled"))
}

func TestPersist_PortForwardPort(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"port_forward":{"port":2222}}`)
	assert.Equal(t, 2222, getNestedValue(cfg, "port_forward.port"))
}

// ─── Push section ──────────────────────────────────────

func TestPersist_PushJPushEnabled(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"push":{"jpush":{"enabled":true}}}`)
	push, ok := cfg["push"].(map[string]any)
	require.True(t, ok, "push should be a map")
	jpush, ok := push["jpush"].(map[string]any)
	require.True(t, ok, "jpush should be a map")
	assert.Equal(t, true, jpush["enabled"])
}

func TestPersist_PushJPushAppKey(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	cfg := patchAndReadConfig(t, `{"push":{"jpush":{"app_key":"new-app-key"}}}`)
	push, ok := cfg["push"].(map[string]any)
	require.True(t, ok, "push should be a map")
	jpush, ok := push["jpush"].(map[string]any)
	require.True(t, ok, "jpush should be a map")
	assert.Equal(t, "new-app-key", jpush["app_key"])
}

// ─── Multi-field PATCH ──────────────────────────────────────

func TestPersist_MultipleFieldsInOnePatch(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	body := `{
		"default_agent": "claude",
		"chat": {"initial_messages": 30, "page_size": 50},
		"tts": {"engine": "edge", "speed": 2.0},
		"rag": {"chunk_size": 1024, "search_pool_size": 30},
		"terminal": {"enabled": false, "idle_timeout": "5m"}
	}`

	cfg := patchAndReadConfig(t, body)

	assert.Equal(t, "claude", getNestedValue(cfg, "default_agent"))
	assert.Equal(t, 30, getNestedValue(cfg, "chat.initial_messages"))
	assert.Equal(t, 50, getNestedValue(cfg, "chat.page_size"))
	assert.Equal(t, "edge", getNestedValue(cfg, "tts.engine"))
	assert.InDelta(t, 2.0, getNestedValue(cfg, "tts.speed"), 0.01)
	assert.Equal(t, 1024, getNestedValue(cfg, "rag.chunk_size"))
	assert.Equal(t, 30, getNestedValue(cfg, "rag.search_pool_size"))
	assert.Equal(t, false, getNestedValue(cfg, "terminal.enabled"))
	assert.Equal(t, "5m", getNestedValue(cfg, "terminal.idle_timeout"))
}

// ─── In-memory + on-disk consistency ──────────────────────────────

func TestPersist_InMemoryAndDiskMatch(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	body := `{"chat":{"system_prompt_interval":20},"upload":{"max_size_mb":50},"default_agent":"claude"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify in-memory
	assert.Equal(t, 20, model.ConfigInstance.Chat.SystemPromptInterval)
	assert.Equal(t, 50, model.ConfigInstance.Upload.MaxSizeMB)
	assert.Equal(t, "claude", model.ConfigInstance.DefaultAgent)

	// Verify on-disk
	configPath := filepath.Join(model.BinDir, "config", "config.yaml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var cfg map[string]any
	require.NoError(t, yaml.Unmarshal(data, &cfg))

	assert.Equal(t, 20, getNestedValue(cfg, "chat.system_prompt_interval"))
	assert.Equal(t, 50, getNestedValue(cfg, "upload.max_size_mb"))
	assert.Equal(t, "claude", getNestedValue(cfg, "default_agent"))
}

// ─── Config backup created ──────────────────────────────────────

func TestPersist_CreatesBackup(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Chat.SystemPromptInterval = 10

	// First PATCH creates config.yaml
	patchAndReadConfig(t, `{"chat":{"system_prompt_interval":15}}`)

	// Second PATCH should create .bak
	patchAndReadConfig(t, `{"chat":{"system_prompt_interval":20}}`)

	bakPath := filepath.Join(model.BinDir, "config", "config.yaml.bak")
	_, err := os.Stat(bakPath)
	assert.NoError(t, err, "config.yaml.bak should exist after second PATCH")
}

// ─── Verify GET matches persisted config ──────────────────────────────

func TestPersist_GetMatchesDiskAfterPatch(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	// PATCH
	patchAndReadConfig(t, `{"rag":{"chunk_size":768,"search_pool_size":15},"terminal":{"max_sessions":3}}`)

	// GET
	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	rag, _ := resp["rag"].(map[string]any)
	assert.Equal(t, float64(768), rag["chunk_size"])
	assert.Equal(t, float64(15), rag["search_pool_size"])

	terminal, _ := resp["terminal"].(map[string]any)
	assert.Equal(t, float64(3), terminal["max_sessions"])
}

// --- writeConfigYAML fresh file (no existing config.yaml) ---

func TestPersist_FreshFileCreation(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	// Don't create config.yaml manually — let writeConfigYAML create it from ConfigInstance
	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Chat.SystemPromptInterval = 10

	cfg := patchAndReadConfig(t, `{"chat":{"system_prompt_interval":20}}`)
	assert.Equal(t, 20, getNestedValue(cfg, "chat.system_prompt_interval"))
}

// --- writeConfigYAML with corrupt existing file ---

func TestPersist_CorruptYAMLRecovery(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	configDir := filepath.Join(model.BinDir, "config")
	configPath := filepath.Join(configDir, "config.yaml")

	// Write corrupt YAML
	require.NoError(t, os.WriteFile(configPath, []byte("::invalid yaml::\n  [bad"), 0o644))

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Chat.SystemPromptInterval = 10

	// Should still succeed — corrupt file is overwritten from ConfigInstance
	cfg := patchAndReadConfig(t, `{"chat":{"system_prompt_interval":20}}`)
	assert.Equal(t, 20, getNestedValue(cfg, "chat.system_prompt_interval"))
}

// --- writeConfigYAML with empty existing file ---

func TestPersist_EmptyYAMLRecovery(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	configDir := filepath.Join(model.BinDir, "config")
	configPath := filepath.Join(configDir, "config.yaml")

	// Write empty file
	require.NoError(t, os.WriteFile(configPath, []byte(""), 0o644))

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Chat.SystemPromptInterval = 10

	cfg := patchAndReadConfig(t, `{"chat":{"system_prompt_interval":20}}`)
	assert.Equal(t, 20, getNestedValue(cfg, "chat.system_prompt_interval"))
}

// --- mergePatchIntoRaw creates nested map if missing ---

func TestPersist_MergeCreatesNestedMap(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	// Create minimal config.yaml with no nested sections
	configDir := filepath.Join(model.BinDir, "config")
	configPath := filepath.Join(configDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("default_agent: claude\n"), 0o644))

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Chat.SystemPromptInterval = 10

	// This should create the "chat" nested map and merge into it
	cfg := patchAndReadConfig(t, `{"chat":{"system_prompt_interval":20}}`)
	assert.Equal(t, 20, getNestedValue(cfg, "chat.system_prompt_interval"))
	// Original top-level field should be preserved
	assert.Equal(t, "claude", getNestedValue(cfg, "default_agent"))
}

// --- Config directory auto-creation ---

func TestPersist_CreatesConfigDir(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	// Remove the config directory that setupPersistTestEnv created
	configDir := filepath.Join(model.BinDir, "config")
	_ = os.RemoveAll(configDir)

	model.ConfigInstance = model.Config{}

	// PATCH should re-create the config directory
	cfg := patchAndReadConfig(t, `{"chat":{"system_prompt_interval":20}}`)
	assert.Equal(t, 20, getNestedValue(cfg, "chat.system_prompt_interval"))

	// Verify the directory and file exist
	_, err := os.Stat(filepath.Join(configDir, "config.yaml"))
	assert.NoError(t, err)
}

// --- Incremental patch preserves existing fields ---

func TestPersist_IncrementalPatchPreservesFields(t *testing.T) {
	_, cleanup := setupPersistTestEnv(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}

	// First PATCH
	patchAndReadConfig(t, `{"chat":{"system_prompt_interval":20}}`)

	// Second PATCH — should preserve the first change
	patchAndReadConfig(t, `{"upload":{"max_size_mb":50}}`)

	// Verify both values persisted
	configPath := filepath.Join(model.BinDir, "config", "config.yaml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var cfg map[string]any
	require.NoError(t, yaml.Unmarshal(data, &cfg))

	assert.Equal(t, 20, getNestedValue(cfg, "chat.system_prompt_interval"))
	assert.Equal(t, 50, getNestedValue(cfg, "upload.max_size_mb"))
}
