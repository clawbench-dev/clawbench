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

// --- maskAPIKey: additional edge cases (supplement settings_sentinel_test.go) ---

func TestMaskAPIKey_8Chars(t *testing.T) {
	assert.Equal(t, "abcd***xyz", maskAPIKey("abcdwxyz"))
}

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

	origBinDir := model.BinDir
	// Use a path that cannot be created on any OS:
	// - Linux: /proc is a procfs mount, mkdir inside it fails with EROFS
	// - Windows: CON is a reserved device name, mkdir fails
	if runtime.GOOS == "windows" {
		model.BinDir = `CON\cannot-create-here`
	} else {
		model.BinDir = "/proc/cannot-create-here"
	}
	defer func() { model.BinDir = origBinDir }()

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

// --- ServeConfig PATCH: rag.api_key with *** rejected ---

func TestServeConfigPatch_RAGMaskedKey(t *testing.T) {
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
