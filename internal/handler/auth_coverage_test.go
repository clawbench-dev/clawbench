package handler

import (
	"crypto/tls"
	"net/http"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
)

// --- loginLimiter internal tests ---

func TestLoginLimiter_IsBlocked_NotBlocked(t *testing.T) {
	l := &loginLimiter{records: make(map[string]*ipRecord)}
	assert.False(t, l.isBlocked("1.2.3.4"))
}

func TestLoginLimiter_IsBlocked_CurrentlyBlocked(t *testing.T) {
	l := &loginLimiter{records: make(map[string]*ipRecord)}
	l.records["1.2.3.4"] = &ipRecord{
		failCount:    maxLoginFails,
		blockedUntil: time.Now().Add(5 * time.Minute),
	}
	assert.True(t, l.isBlocked("1.2.3.4"))
}

func TestLoginLimiter_IsBlocked_ExpiredBlockResets(t *testing.T) {
	l := &loginLimiter{records: make(map[string]*ipRecord)}
	l.records["1.2.3.4"] = &ipRecord{
		failCount:    maxLoginFails,
		blockedUntil: time.Now().Add(-1 * time.Second), // expired
	}
	assert.False(t, l.isBlocked("1.2.3.4"))
	// After expiry, the record should be reset
	rec := l.records["1.2.3.4"]
	assert.Equal(t, 0, rec.failCount)
	assert.True(t, rec.blockedUntil.IsZero())
}

func TestLoginLimiter_IsBlocked_ZeroBlockNotBlocked(t *testing.T) {
	l := &loginLimiter{records: make(map[string]*ipRecord)}
	// blockedUntil is zero (not blocked) but record exists
	l.records["1.2.3.4"] = &ipRecord{failCount: 2}
	assert.False(t, l.isBlocked("1.2.3.4"))
}

func TestLoginLimiter_RecordFailure_ExponentialBackoff(t *testing.T) {
	l := &loginLimiter{records: make(map[string]*ipRecord)}

	// Record maxLoginFails failures
	for range maxLoginFails {
		l.recordFailure("1.2.3.4")
	}
	rec := l.records["1.2.3.4"]
	assert.Equal(t, maxLoginFails, rec.failCount)
	assert.False(t, rec.blockedUntil.IsZero())

	// The block duration should be initialLoginBlock (5 min)
	expectedDur := initialLoginBlock
	assert.WithinDuration(t, time.Now().Add(expectedDur), rec.blockedUntil, 2*time.Second)

	// Record more failures to increase the block
	for range maxLoginFails {
		l.recordFailure("1.2.3.4")
	}
	rec = l.records["1.2.3.4"]
	// Second infraction: 2x initial block = 10 min
	expectedDur = initialLoginBlock * 2
	assert.WithinDuration(t, time.Now().Add(expectedDur), rec.blockedUntil, 2*time.Second)
}

func TestLoginLimiter_RecordFailure_CappedAtMaxBlock(t *testing.T) {
	l := &loginLimiter{records: make(map[string]*ipRecord)}
	// Simulate many infractions to exceed maxLoginBlock
	for range maxLoginFails * 10 {
		l.recordFailure("1.2.3.4")
	}
	rec := l.records["1.2.3.4"]
	// blockedUntil should not exceed lastFail + maxLoginBlock
	maxExpiry := rec.lastFail.Add(maxLoginBlock)
	assert.True(t, !rec.blockedUntil.After(maxExpiry) || rec.blockedUntil.Equal(maxExpiry),
		"block duration should be capped at maxLoginBlock")
}

func TestLoginLimiter_Reset(t *testing.T) {
	l := &loginLimiter{records: make(map[string]*ipRecord)}
	l.records["1.2.3.4"] = &ipRecord{failCount: 5}
	l.reset("1.2.3.4")
	_, exists := l.records["1.2.3.4"]
	assert.False(t, exists)
}

func TestLoginLimiter_Cleanup(t *testing.T) {
	l := &loginLimiter{records: make(map[string]*ipRecord)}

	// Add an old expired record
	l.records["1.2.3.4"] = &ipRecord{
		failCount:    1,
		lastFail:     time.Now().Add(-loginRecordTTL - time.Minute),
		blockedUntil: time.Time{}, // not blocked
	}
	// Add a recent record that should survive
	l.records["5.6.7.8"] = &ipRecord{
		failCount:    1,
		lastFail:     time.Now(),
		blockedUntil: time.Time{},
	}

	l.cleanup()

	_, existsOld := l.records["1.2.3.4"]
	assert.False(t, existsOld, "expired record should be cleaned up")
	_, existsRecent := l.records["5.6.7.8"]
	assert.True(t, existsRecent, "recent record should survive")
}

// --- ServeAuthCheck: CookieToken fallback ---

func TestServeAuthCheck_CookieTokenFallback(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Set SessionToken but not CookieToken — should fall back to SessionToken
	model.SessionToken = hashPassword("testpass")
	model.CookieToken = ""

	req := newRequest(t, http.MethodGet, "/api/auth/check", nil)
	withAuthCookie(req, model.SessionToken)

	w := callHandler(ServeAuthCheck, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeAuthCheck_CookieTokenPreferred(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("testpass")
	model.CookieToken = "random-cookie-token"

	req := newRequest(t, http.MethodGet, "/api/auth/check", nil)
	withAuthCookie(req, model.CookieToken)

	w := callHandler(ServeAuthCheck, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeAuthCheck_CookieTokenMismatch(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("testpass")
	model.CookieToken = "random-cookie-token"

	req := newRequest(t, http.MethodGet, "/api/auth/check", nil)
	// Send SessionToken value instead of CookieToken — should fail
	withAuthCookie(req, model.SessionToken)

	w := callHandler(ServeAuthCheck, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- ServeLogin: CookieToken generation on successful login ---

func TestServeLogin_GeneratesCookieTokenIfEmpty(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("testpass")
	model.CookieToken = ""
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
	model.PasswordHash = bcryptHash

	req := newRequest(t, http.MethodPost, "/login", map[string]string{
		"password": "testpass",
	})
	w := callHandler(ServeLogin, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, model.CookieToken, "CookieToken should be generated on first successful login")
	assert.NotEqual(t, model.SessionToken, model.CookieToken, "CookieToken should be random, not the password hash")
}

func TestServeLogin_ExistingCookieTokenNotRegenerated(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("testpass")
	model.CookieToken = "existing-random-token"
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
	model.PasswordHash = bcryptHash

	req := newRequest(t, http.MethodPost, "/login", map[string]string{
		"password": "testpass",
	})
	w := callHandler(ServeLogin, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "existing-random-token", model.CookieToken, "existing CookieToken should not be regenerated")
}

// --- ServeLogin: SHA-256 login with empty SessionToken ---

func TestServeLogin_EmptySessionToken_Authenticated(t *testing.T) {
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
}

// --- ServeLogin: RemoteAddr without port ---

func TestServeLogin_RemoteAddrNoPort(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("testpass")
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
	model.PasswordHash = bcryptHash

	req := newRequest(t, http.MethodPost, "/login", map[string]string{
		"password": "testpass",
	})
	req.RemoteAddr = "192.0.2.1" // no port

	w := callHandler(ServeLogin, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// --- ServeLogin: rate limiter reset on global limiter ---

func TestServeLogin_RateLimiter_ResetOnSuccess(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("testpass")
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
	model.PasswordHash = bcryptHash

	globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
	globalLoginLimiterOnce = sync.Once{}

	// Add some failures
	for range maxLoginFails - 1 {
		req := newRequest(t, http.MethodPost, "/login", map[string]string{
			"password": "wrongpass",
		})
		w := callHandler(ServeLogin, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	}

	// Successful login should reset
	req := newRequest(t, http.MethodPost, "/login", map[string]string{
		"password": "testpass",
	})
	w := callHandler(ServeLogin, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Should be able to fail again without being blocked
	req = newRequest(t, http.MethodPost, "/login", map[string]string{
		"password": "wrongpass",
	})
	w = callHandler(ServeLogin, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- ServeLogin: SHA-256 wrong password rate limited ---
// (Covered by TestServeLogin_RateLimiting in auth_test.go)

// --- ServeLogin: DELETE method returns 405 ---

func TestServeLogin_DeleteMethod_Returns405(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/login", nil)
	w := callHandler(ServeLogin, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- ServeLogin: Cookie Secure flag over TLS ---

func TestServeLogin_CookieSecureOverTLS(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("testpass")
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
	model.PasswordHash = bcryptHash

	req := newRequest(t, http.MethodPost, "/login", map[string]string{
		"password": "testpass",
	})
	req.TLS = &tls.ConnectionState{} // simulate TLS

	w := callHandler(ServeLogin, req)
	assert.Equal(t, http.StatusOK, w.Code)

	for _, c := range w.Result().Cookies() {
		if c.Name == model.SessionCookie {
			assert.True(t, c.Secure, "cookie should be Secure over TLS")
		}
	}
}

// --- getLoginLimiter singleton ---

func TestGetLoginLimiter_Singleton(t *testing.T) {
	// Reset the singleton
	globalLoginLimiter = nil
	globalLoginLimiterOnce = sync.Once{}

	l1 := getLoginLimiter()
	l2 := getLoginLimiter()
	assert.Equal(t, l1, l2, "getLoginLimiter should return the same instance")
}
