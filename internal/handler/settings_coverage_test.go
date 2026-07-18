package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/speech"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// --- maskAPIKey: removed (supplement settings_sentinel_test.go) ---
// maskAPIKey was removed — config API now returns full values for password fields.

// --- joinArgs: additional case with quote ---

func TestJoinArgs_WithQuote(t *testing.T) {
	assert.Equal(t, `'it'\''s' 'world'`, joinArgs([]string{"it's", "world"}))
}

// --- shellQuote: additional cases ---

func TestShellQuote_SingleQuoteEscaped(t *testing.T) {
	assert.Equal(t, `'it'\''s'`, shellQuote("it's"))
}

func TestShellQuote_EmptyVal(t *testing.T) {
	assert.Equal(t, `''`, shellQuote(""))
}

// --- IsRunningUnderSupervisor: container env var ---

func TestIsRunningUnderSupervisor_ContainerEnvVar(t *testing.T) {
	t.Setenv("CLAWBENCH_NO_SUPERVISOR", "")
	t.Setenv("container", "docker")
	assert.True(t, IsRunningUnderSupervisor())
}

// --- ServeConfigPassword: additional coverage ---

func TestServeConfigPassword_EmptyPw(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "",
		"new_password":     "",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, "sometoken")
	w := callHandler(ServeConfigPassword, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "empty_password")
}

func TestServeConfigPassword_PwTooShort(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "current1",
		"new_password":     "short1",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, "sometoken")
	w := callHandler(ServeConfigPassword, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "password_too_short")
}

func TestServeConfigPassword_PwTooLong(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "current1",
		"new_password":     strings.Repeat("a", 33) + "1",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, "sometoken")
	w := callHandler(ServeConfigPassword, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "password_too_long")
}

func TestServeConfigPassword_PwNoLetterOrDigit(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "current1",
		"new_password":     "!!!!!!!!",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, "sometoken")
	w := callHandler(ServeConfigPassword, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "password_no_letter_digit")
}

func TestServeConfigPassword_PwNoLetter(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "current1",
		"new_password":     "123456789",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, "sometoken")
	w := callHandler(ServeConfigPassword, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "password_no_letter_digit")
}

func TestServeConfigPassword_WrongPw(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	password := "correct-password1"
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	model.SessionToken = "sometoken"
	model.PasswordHash = bcryptHash
	model.PasswordIsSHA256 = false
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "wrong-password1",
		"new_password":     "brand-new1",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, "sometoken")
	w := callHandler(ServeConfigPassword, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "wrong_password")
}

func TestServeConfigPassword_SHA256WrongPw(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	password := "correct-password1"
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	model.SessionToken = hex.EncodeToString(hash[:])
	model.PasswordIsSHA256 = true
	model.PasswordHash = nil
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "wrong-password1",
		"new_password":     "brand-new1",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, "sometoken")
	w := callHandler(ServeConfigPassword, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestServeConfigPassword_NilPwHash(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	model.SessionToken = "sometoken"
	model.PasswordIsSHA256 = false
	model.PasswordHash = nil
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "current1",
		"new_password":     "brand-new1",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, "sometoken")
	w := callHandler(ServeConfigPassword, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestServeConfigPassword_BlockedByRateLimit(t *testing.T) {
	_, teardown := setupTestEnv(t)
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	globalLoginLimiter.records["192.0.2.1"] = &ipRecord{
		failCount:    maxLoginFails,
		blockedUntil: time.Now().Add(5 * time.Minute),
	}

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "test",
		"new_password":     "test1",
	})
	req.RemoteAddr = "192.0.2.1:1234"
	withAuthCookie(req, "sometoken")
	w := callHandler(ServeConfigPassword, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

// --- ServeConfig: DELETE method ---

func TestServeConfig_DeleteReturns405(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- applyHotReloadGlobals: Piper with explicit length_scale ---

func TestApplyHotReloadGlobals_PiperExplicitLength(t *testing.T) {
	origProvider := GetSpeechProvider()
	defer SetSpeechProvider(origProvider)

	SetSpeechProvider(&speech.PiperProvider{LengthScale: 1.0})
	model.ConfigInstance.TTS.Voice = ""
	model.ConfigInstance.TTS.Speed = 2.0
	model.ConfigInstance.TTS.Piper.LengthScale = 1.5

	applyHotReloadGlobals()

	p := GetSpeechProvider().(*speech.PiperProvider)
	// When explicit length_scale > 0 is set, speed should NOT override it
	// The provider's LengthScale stays at its original value (not overridden by speed)
	assert.Equal(t, 1.0, p.LengthScale, "explicit length_scale should prevent speed override")
}

// --- applyHotReloadGlobals: EdgeTTS speed exactly 1.0 ---

func TestApplyHotReloadGlobals_EdgeTTS_RateZero(t *testing.T) {
	origProvider := GetSpeechProvider()
	defer SetSpeechProvider(origProvider)

	SetSpeechProvider(&speech.EdgeTTSProvider{Rate: "+0%"})
	model.ConfigInstance.TTS.Voice = ""
	model.ConfigInstance.TTS.Speed = 1.0

	applyHotReloadGlobals()

	p := GetSpeechProvider().(*speech.EdgeTTSProvider)
	assert.Equal(t, "+0%", p.Rate)
}

// --- writeConfigYAML: mkdir failure ---

func TestWriteConfigYAML_MkdirFail(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping as root")
	}

	origDataDir := model.DataDir
	// Use a path that cannot be created on any OS:
	// - Linux: /proc is a procfs mount, mkdir inside it fails with EROFS
	// - Windows: CON is a reserved device name, mkdir fails
	if runtime.GOOS == "windows" {
		model.DataDir = `CON\cannot-create-here`
	} else {
		model.DataDir = "/proc/cannot-create-here"
	}
	defer func() { model.DataDir = origDataDir }()

	err := writeConfigYAML(map[string]any{"test": "value"})
	assert.Error(t, err)
}

// --- copyFile: error path ---

func TestCopyFile_SourceMissing(t *testing.T) {
	tmpDir := t.TempDir()
	err := copyFile(filepath.Join(tmpDir, "nonexistent"), filepath.Join(tmpDir, "dest"))
	assert.Error(t, err)
}

// --- ServeConfigPassword: body read error ---

func TestServeConfigPassword_ReadErr(t *testing.T) {
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

// --- ServeConfigPassword: method not allowed ---

func TestServeConfigPassword_GetReturns405(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/config/password", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- ServeConfig GET: has_password field when no password ---

func TestServeConfig_Get_NoPassword(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = ""
	model.CookieToken = ""
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, false, resp["has_password"])
}

// --- validatePatchValues: default_agent empty string ---

func TestServeConfig_Patch_DefaultAgentEmpty(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"default_agent":""}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// --- ServeConfig PATCH: rag.api_key with *** (maskAPIKey removed, now accepted) ---

func TestServeConfigPatch_RAGKeyWithStars(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"rag":{"api_key":"sk-1***xyz"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- ServeConfig PATCH: tts.tts_model ---

func TestServeConfigPatch_TTSModelName(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"tts":{"tts_model":"test-tts-model"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "test-tts-model", model.ConfigInstance.TTS.TTSModel)
}

// --- ServeConfig PATCH: localhost_auth_exempt false ---

func TestServeConfigPatch_LocalhostAuthExemptFalse(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.LocalhostAuthExempt = true

	body := `{"localhost_auth_exempt":false}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, model.ConfigInstance.LocalhostAuthExempt)
	assert.False(t, model.LocalhostAuthExempt)
}

// --- hotReloadWarnings ---

func TestAddHotReloadWarning_AndApply(t *testing.T) {
	// Clear any existing warnings
	_ = applyHotReloadWarnings()

	AddHotReloadWarning("test warning 1")
	AddHotReloadWarning("test warning 2")

	warnings := applyHotReloadWarnings()
	assert.Equal(t, []string{"test warning 1", "test warning 2"}, warnings)

	// Second call should return empty (cleared)
	warnings2 := applyHotReloadWarnings()
	assert.Nil(t, warnings2)
}

func TestApplyHotReloadWarnings_Empty(t *testing.T) {
	// Clear any existing warnings
	_ = applyHotReloadWarnings()

	warnings := applyHotReloadWarnings()
	assert.Nil(t, warnings)
}

// --- DingTalk config patch ---

func TestServeConfigPatch_DingTalkConfig(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"dingtalk":{"enabled":true,"app_key":"test-key","app_secret":"test-secret","agent_id":123,"users":["user1"]}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, model.ConfigInstance.DingTalk.Enabled)
	assert.Equal(t, "test-key", model.ConfigInstance.DingTalk.AppKey)
}

func TestServeConfigPatch_DingTalkSecretWithStars(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	body := `{"dingtalk":{"app_secret":"***masked***"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- push_mode validation and application ---

func TestServeConfigPatch_PushModeValid(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}

	body := `{"push_mode":"dingtalk"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "dingtalk", model.ConfigInstance.PushMode)
	assert.True(t, model.ConfigInstance.DingTalk.Enabled)
}

func TestServeConfigPatch_PushModeInvalid(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}

	body := `{"push_mode":"invalid"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "push_mode")
}

// --- summarize.tts_backend validation ---

func TestServeConfigPatch_SummarizeTTSBackendValid(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}

	body := `{"summarize":{"tts_backend":"simple"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "simple", model.ConfigInstance.Summarize.TTSBackend)
}

func TestServeConfigPatch_SummarizeTTSBackendInvalid(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}

	body := `{"summarize":{"tts_backend":"claude"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "summarize.tts_backend")
}

func TestServeConfigPatch_SummarizeTTSBackendAPIWithoutBaseURL(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Summarize.TTSBackend = "api"
	model.ConfigInstance.Summarize.TTSAPI.BaseURL = ""

	body := `{"summarize":{"tts_model":"test"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tts_api.base_url")
}

func TestServeConfigPatch_SummarizeTTSBackendSwitchedToAPI(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Summarize.TTSBackend = "simple"

	// Switching tts_backend to "api" should not require base_url yet
	body := `{"summarize":{"tts_backend":"api"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeConfigPatch_SummarizeTTSAPISubConfig(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Summarize.TTSBackend = "api"
	model.ConfigInstance.Summarize.TTSAPI.BaseURL = "https://example.com"

	body := `{"summarize":{"tts_api":{"base_url":"https://updated.com","key":"test-key"}}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "https://updated.com", model.ConfigInstance.Summarize.TTSAPI.BaseURL)
	assert.Equal(t, "test-key", model.ConfigInstance.Summarize.TTSAPI.Key)
}

// --- summarize.tts_model patch ---

func TestServeConfigPatch_SummarizeTTSModel(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}

	body := `{"summarize":{"tts_model":"gpt-4"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "gpt-4", model.ConfigInstance.Summarize.TTSModel)
}

// --- ServeConfig GET: TTSAPI conditional sub-config ---

func TestServeConfig_Get_TTSAPISubConfig(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Summarize.TTSBackend = "api"
	model.ConfigInstance.Summarize.TTSAPI.BaseURL = "https://tts.example.com"
	model.ConfigInstance.Summarize.TTSAPI.Key = "tts-key"

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp configResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotNil(t, resp.Summarize.TTSAPI)
	assert.Equal(t, "https://tts.example.com", resp.Summarize.TTSAPI.BaseURL)
}

// --- ServeConfig GET: PushMode field ---

func TestServeConfig_Get_PushMode(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.PushMode = "native"

	req := newRequest(t, http.MethodGet, "/api/config", nil)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp configResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "native", resp.PushMode)
}

// --- FRP token with *** (mask removed, now accepted) ---

func TestServeConfigPatch_FRPTokenWithStars(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}

	body := `{"frp":{"token":"abc***xyz"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "abc***xyz", model.ConfigInstance.FRP.Token)
}
