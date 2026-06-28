package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
)

func TestServeAuthCheck(t *testing.T) {
	t.Run("NoPasswordSet_Returns200", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// No password set — both tokens must be empty
		model.SessionToken = ""
		model.CookieToken = ""

		req := newRequest(t, http.MethodGet, "/api/auth/check", nil)
		w := callHandler(ServeAuthCheck, req)

		assert.Equal(t, http.StatusOK, w.Code)
		_ = env
	})

	t.Run("PasswordSet_ValidCookie_Returns200", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		model.SessionToken = hashPassword("testpass")

		req := newRequest(t, http.MethodGet, "/api/auth/check", nil)
		withAuthCookie(req, model.SessionToken)

		w := callHandler(ServeAuthCheck, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("PasswordSet_NoCookie_Returns401", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		model.SessionToken = hashPassword("testpass")

		req := newRequest(t, http.MethodGet, "/api/auth/check", nil)
		// No cookie added

		w := callHandler(ServeAuthCheck, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("PasswordSet_WrongCookie_Returns401", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		model.SessionToken = hashPassword("testpass")

		req := newRequest(t, http.MethodGet, "/api/auth/check", nil)
		withAuthCookie(req, "wrong-token-value")

		w := callHandler(ServeAuthCheck, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("PasswordSet_LocalhostBypass_Returns200", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		model.SessionToken = hashPassword("testpass")

		req := newRequest(t, http.MethodGet, "/api/auth/check", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		// No cookie — should still pass because localhost

		w := callHandler(ServeAuthCheck, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("PasswordSet_LocalhostIPv6Bypass_Returns200", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		model.SessionToken = hashPassword("testpass")

		req := newRequest(t, http.MethodGet, "/api/auth/check", nil)
		req.RemoteAddr = "[::1]:54321"
		// No cookie — should still pass because localhost

		w := callHandler(ServeAuthCheck, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("LocalhostAuthExempt_LocalhostNoCookie_Returns401", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		model.SessionToken = hashPassword("testpass")
		model.LocalhostAuthExempt = false
		defer func() { model.LocalhostAuthExempt = true }()

		req := newRequest(t, http.MethodGet, "/api/auth/check", nil)
		req.RemoteAddr = "127.0.0.1:54321"

		w := callHandler(ServeAuthCheck, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("LocalhostAuthExempt_LocalhostWithCookie_Returns200", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		model.SessionToken = hashPassword("testpass")
		model.LocalhostAuthExempt = false
		defer func() { model.LocalhostAuthExempt = true }()

		req := newRequest(t, http.MethodGet, "/api/auth/check", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		withAuthCookie(req, model.SessionToken)

		w := callHandler(ServeAuthCheck, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestServeLogin(t *testing.T) {
	t.Run("GET_DoesNotCrash", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/login", nil)
		w := callHandler(ServeLogin, req)

		// May be 200 (if public/index.html exists) or 404 — just verify no panic
		assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, w.Code)
	})

	t.Run("POST_BcryptCorrectPassword_Returns200WithCookie", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		model.SessionToken = hashPassword("testpass")
		bcryptHash, err := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
		assert.NoError(t, err)
		model.PasswordHash = bcryptHash

		req := newRequest(t, http.MethodPost, "/login", map[string]string{
			"password": "testpass",
		})
		w := callHandler(ServeLogin, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assertJSONField(t, w, "ok", true)

		// Verify cookie is set with the cryptographically random CookieToken
		// (ISS-117, ISS-131, ISS-183: cookie value must NOT equal the password hash)
		var foundCookie bool
		for _, c := range w.Result().Cookies() {
			if c.Name == model.SessionCookie {
				foundCookie = true
				// Cookie value should be the random CookieToken, not the password-derived SessionToken
				assert.Equal(t, model.CookieToken, c.Value)
				assert.Equal(t, "/", c.Path)
				assert.True(t, c.HttpOnly)
				// Cookie must differ from the password hash (security: decoupled tokens)
				assert.NotEqual(t, model.SessionToken, c.Value, "cookie must not equal password hash")
			}
		}
		assert.True(t, foundCookie, "expected session cookie to be set")
	})

	t.Run("POST_BcryptWrongPassword_Returns401", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		model.SessionToken = hashPassword("testpass")
		bcryptHash, err := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
		assert.NoError(t, err)
		model.PasswordHash = bcryptHash

		req := newRequest(t, http.MethodPost, "/login", map[string]string{
			"password": "wrongpass",
		})
		w := callHandler(ServeLogin, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assertJSONField(t, w, "ok", false)
	})

	t.Run("POST_NilPasswordHash_Returns500", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		// PasswordHash is nil — should reject login (no insecure SHA-256 fallback)
		model.SessionToken = hashPassword("testpass")
		model.PasswordHash = nil

		req := newRequest(t, http.MethodPost, "/login", map[string]string{
			"password": "testpass",
		})
		w := callHandler(ServeLogin, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("POST_EmptyBody_Returns401", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		model.SessionToken = hashPassword("testpass")
		bcryptHash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
		model.PasswordHash = bcryptHash

		req := newRequest(t, http.MethodPost, "/login", map[string]string{
			"password": "",
		})
		w := callHandler(ServeLogin, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assertJSONField(t, w, "ok", false)
	})

	// ISS-146: Malformed JSON must return 400, not silently fall through
	// to bcrypt comparison with an empty password.
	t.Run("POST_MalformedJSON_Returns400", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		model.SessionToken = hashPassword("testpass")
		bcryptHash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
		model.PasswordHash = bcryptHash

		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{invalid json"))
		req.Header.Set("Content-Type", "application/json")
		w := callHandler(ServeLogin, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("POST_NoPasswordRequired_Returns200", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		model.SessionToken = ""
		model.CookieToken = ""
		model.PasswordHash = nil

		req := newRequest(t, http.MethodPost, "/login", map[string]string{
			"password": "anything",
		})
		w := callHandler(ServeLogin, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assertJSONField(t, w, "ok", true)
	})

	t.Run("OtherMethod_Returns405", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodPut, "/login", nil)
		w := callHandler(ServeLogin, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

// --- ISS-SHA256: SHA-256 password login test ---

func TestServeLogin_SHA256StoredPassword(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	password := "sha256-password"
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	model.SessionToken = hex.EncodeToString(hash[:])
	model.PasswordIsSHA256 = true
	model.PasswordHash = nil

	req := newRequest(t, http.MethodPost, "/login", map[string]string{
		"password": password,
	})
	w := callHandler(ServeLogin, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assertJSONField(t, w, "ok", true)
}

func TestServeLogin_SHA256StoredPassword_WrongPassword(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	password := "correct-sha256-password"
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	model.SessionToken = hex.EncodeToString(hash[:])
	model.PasswordIsSHA256 = true
	model.PasswordHash = nil

	req := newRequest(t, http.MethodPost, "/login", map[string]string{
		"password": "wrong-password",
	})
	w := callHandler(ServeLogin, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- ISS-003c: Login rate limiting tests ---

func TestServeLogin_RateLimiting(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("testpass")
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
	model.PasswordHash = bcryptHash

	// Reset the global limiter for this test
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	globalLoginLimiterOnce = sync.Once{}

	// Send maxLoginFails wrong password attempts
	for range maxLoginFails {
		req := newRequest(t, http.MethodPost, "/login", map[string]string{
			"password": "wrongpass",
		})
		w := callHandler(ServeLogin, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	}

	// Next attempt should be blocked
	req := newRequest(t, http.MethodPost, "/login", map[string]string{
		"password": "testpass",
	})
	w := callHandler(ServeLogin, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestServeLogin_RateLimiting_SuccessUnblocks(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("testpass")
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
	model.PasswordHash = bcryptHash

	// Reset the global limiter
	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	globalLoginLimiterOnce = sync.Once{}

	// Send (maxLoginFails - 1) wrong password attempts (just under the limit)
	for range maxLoginFails - 1 {
		req := newRequest(t, http.MethodPost, "/login", map[string]string{
			"password": "wrongpass",
		})
		w := callHandler(ServeLogin, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	}

	// Correct password should still work and reset the counter
	req := newRequest(t, http.MethodPost, "/login", map[string]string{
		"password": "testpass",
	})
	w := callHandler(ServeLogin, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Should be able to fail more times now (counter was reset)
	req = newRequest(t, http.MethodPost, "/login", map[string]string{
		"password": "wrongpass",
	})
	w = callHandler(ServeLogin, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code) // not 429
}

func TestAuth_WatchDir_RequiresAuth(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("secret")

	req := newRequest(t, http.MethodGet, "/api/watch-dir", nil)
	w := callHandlerWithAuth(ServeRoots, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuth_WatchDir_PassWithValidCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("secret")

	req := newRequest(t, http.MethodGet, "/api/watch-dir", nil)
	withAuthCookie(req, model.SessionToken)

	w := callHandlerWithAuth(ServeRoots, req)

	// Should not be 401 — may be 200 or 403 depending on project cookie, but NOT 401
	assert.NotEqual(t, http.StatusUnauthorized, w.Code)
}

func TestAuth_Project_RequiresAuth(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("secret")

	req := newRequest(t, http.MethodGet, "/api/project", nil)
	w := callHandlerWithAuth(ServeProjectSet, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuth_Project_PassWithValidCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("secret")

	req := newRequest(t, http.MethodGet, "/api/project", nil)
	withAuthCookie(req, model.SessionToken)

	w := callHandlerWithAuth(ServeProjectSet, req)

	// Should not be 401
	assert.NotEqual(t, http.StatusUnauthorized, w.Code)
}
