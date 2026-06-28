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

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestServeConfigPassword_Success(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Set up plaintext password with bcrypt hash
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
		"new_password":     "newpass1",
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["needs_restart"])

	// Verify config.yaml was written with sha256: prefix
	configData, err := os.ReadFile(filepath.Join(model.BinDir, "config", "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(configData), "sha256:")

	// Verify ConfigInstance.Password was updated
	assert.True(t, model.IsSHA256Password(model.ConfigInstance.Password))
}

func TestServeConfigPassword_WrongCurrentPassword(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	password := "correct-password"
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	model.SessionToken = hex.EncodeToString(hash[:])
	model.PasswordIsSHA256 = false
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	model.PasswordHash = bcryptHash
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "wrong-password",
		"new_password":     "newpass1",
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "wrong_password", resp["error"])
}

func TestServeConfigPassword_EmptyFields(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = "sometoken"
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "",
		"new_password":     "",
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "empty_password", resp["error"])
}

func TestServeConfigPassword_TooShort(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	password := "test-password"
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	model.SessionToken = hex.EncodeToString(hash[:])
	model.PasswordIsSHA256 = false
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	model.PasswordHash = bcryptHash
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": password,
		"new_password":     "abc1234", // 7 chars — below new min of 8
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "password_too_short", resp["error"])
}

func TestServeConfigPassword_SHA256StoredPassword(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Simulate password already stored as SHA-256 hash
	password := "stored-sha256-password"
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	hashStr := hex.EncodeToString(hash[:])
	model.SessionToken = hashStr
	model.PasswordIsSHA256 = true
	model.PasswordHash = nil // No bcrypt when stored as SHA-256
	model.ConfigInstance = model.Config{}
	model.BinDir = t.TempDir()
	_ = os.MkdirAll(filepath.Join(model.BinDir, "config"), 0o755)

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": password,
		"new_password":     "brand-new1",
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["needs_restart"])

	// Verify config.yaml was written with sha256: prefix
	configData, err := os.ReadFile(filepath.Join(model.BinDir, "config", "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(configData), "sha256:")
}

func TestServeConfigPassword_SHA256StoredPassword_WrongCurrent(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	password := "correct-sha256-password"
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	hashStr := hex.EncodeToString(hash[:])
	model.SessionToken = hashStr
	model.PasswordIsSHA256 = true
	model.PasswordHash = nil
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "wrong-password",
		"new_password":     "brand-new1",
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestServeConfigPassword_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/config/password", http.NoBody)
	withAuthCookie(req, "sometoken")
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeConfigPassword_InvalidJSON(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = "sometoken"
	req := httptest.NewRequest(http.MethodPost, "/api/config/password", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeConfigPassword_RateLimited(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	password := "test-password"
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	model.SessionToken = hex.EncodeToString(hash[:])
	model.PasswordIsSHA256 = false
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	model.PasswordHash = bcryptHash
	model.ConfigInstance = model.Config{}

	// Make failed requests to trigger rate limiting
	// The global login limiter persists across tests, so count from current state
	blocked := false
	for range 10 {
		req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
			"current_password": "wrong-password",
			"new_password":     "newpass1",
		})
		withAuthCookie(req, model.SessionToken)
		w := callHandler(ServeConfigPassword, req)
		if w.Code == http.StatusTooManyRequests {
			blocked = true
			break
		}
	}
	assert.True(t, blocked, "expected rate limiting to kick in after repeated failures")
}

func TestServeConfig_Get_HasPassword(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	model.ConfigInstance = cfg

	// With password set
	model.SessionToken = "sometoken"
	req := newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["has_password"])

	// Without password
	model.SessionToken = ""
	model.CookieToken = ""
	req = newRequest(t, http.MethodGet, "/api/config", nil)
	withAuthCookie(req, "")
	w = callHandler(ServeConfig, req)

	assert.Equal(t, http.StatusOK, w.Code)
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["has_password"])
}

func TestServeConfigPassword_TooLong(t *testing.T) {
	_, teardown := setupTestEnv(t)
	// Reset rate limiter to avoid interference from previous tests
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	password := "test-password"
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	model.SessionToken = hex.EncodeToString(hash[:])
	model.PasswordIsSHA256 = false
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	model.PasswordHash = bcryptHash
	model.ConfigInstance = model.Config{}

	// 33 chars with letter+digit — exceeds new max of 32
	longPassword := "a1" + strings.Repeat("a", 31)

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": password,
		"new_password":     longPassword,
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "password_too_long", resp["error"])
}

func TestServeConfigPassword_NoPasswordHash(t *testing.T) {
	_, teardown := setupTestEnv(t)
	// Reset rate limiter to avoid interference from previous tests
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	// Simulate no password set (no bcrypt hash, not SHA-256)
	model.SessionToken = ""
	model.CookieToken = ""
	model.PasswordIsSHA256 = false
	model.PasswordHash = nil
	model.ConfigInstance = model.Config{}

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": "any-password",
		"new_password":     "newpass1",
	})
	// No auth cookie needed when no password is set, but require one for auth middleware
	withAuthCookie(req, "")
	w := callHandler(ServeConfigPassword, req)

	// Should fail with wrong_password since neither SHA-256 nor bcrypt matches
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "wrong_password", resp["error"])
}

func TestServeConfigPassword_WriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows: directory permission model differs")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping as root: root bypasses filesystem permissions")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping as root: root bypasses filesystem permissions")
	}
	_, teardown := setupTestEnv(t)
	// Reset rate limiter to avoid interference from previous tests
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	defer teardown()

	password := "test-password"
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	model.SessionToken = hex.EncodeToString(hash[:])
	model.PasswordIsSHA256 = false
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	model.PasswordHash = bcryptHash
	model.ConfigInstance = model.Config{Password: password}

	// Create a temp dir and make its config subdirectory read-only to trigger write failure
	origBinDir := model.BinDir
	failDir := t.TempDir()
	configDir := filepath.Join(failDir, "config")
	_ = os.MkdirAll(configDir, 0o755)
	_ = os.Chmod(configDir, 0o555)
	model.BinDir = failDir
	defer func() {
		_ = os.Chmod(configDir, 0o755) // restore for cleanup
		model.BinDir = origBinDir
	}()

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": password,
		"new_password":     "newpass1",
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "write_failed", resp["error"])

	// Verify in-memory config was rolled back
	assert.Equal(t, password, model.ConfigInstance.Password)
}

func TestServeConfigPassword_NoLetter(t *testing.T) {
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

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": password,
		"new_password":     "12345678", // all digits, no letter
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "password_no_letter_digit", resp["error"])
}

func TestServeConfigPassword_NoDigit(t *testing.T) {
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

	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": password,
		"new_password":     "abcdefgh", // all letters, no digit
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "password_no_letter_digit", resp["error"])
}

func TestServeConfigPassword_Boundary32(t *testing.T) {
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

	// Exactly 32 chars with letter+digit — should succeed
	boundaryPassword := "a1" + strings.Repeat("a", 30) // 32 chars
	req := newRequest(t, http.MethodPost, "/api/config/password", map[string]string{
		"current_password": password,
		"new_password":     boundaryPassword,
	})
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfigPassword, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
