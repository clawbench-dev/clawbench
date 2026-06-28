package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"clawbench/internal/middleware"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// okHandler is an always-200 handler used as the "next" in middleware chains.
func okHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// withSavedToken saves model.SessionToken and model.CookieToken, runs f, then restores them.
func withSavedToken(f func()) {
	origSession := model.SessionToken
	origCookie := model.CookieToken
	origRequireAuth := model.LocalhostAuthExempt
	defer func() {
		model.SessionToken = origSession
		model.CookieToken = origCookie
		model.LocalhostAuthExempt = origRequireAuth
	}()
	f()
}

// --- Auth: no password configured ---

func TestAuth_NoPassword_PassThrough(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = ""

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

// --- Auth: localhost bypass ---

func TestAuth_Localhost_IPv4_BypassesAuth(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"
		model.LocalhostAuthExempt = true

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "127.0.0.1:12345"

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestAuth_Localhost_IPv6_BypassesAuth(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"
		model.LocalhostAuthExempt = true

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "[::1]:12345"

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

// --- Auth: remote with valid cookie ---

func TestAuth_ValidCookie_PassThrough(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "192.168.1.100:12345"
		req.AddCookie(&http.Cookie{
			Name:  model.SessionCookie,
			Value: "valid-token",
		})

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

// --- Auth: remote with invalid/missing cookie ---

func TestAuth_InvalidCookieValue_Returns401(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "192.168.1.100:12345"
		req.AddCookie(&http.Cookie{
			Name:  model.SessionCookie,
			Value: "wrong-token",
		})

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestAuth_MissingCookie_Returns401(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "192.168.1.100:12345"

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

// --- Auth: localhost + bad cookie still passes (localhost wins) ---

func TestAuth_LocalhostWithBadCookie_StillPasses(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"
		model.LocalhostAuthExempt = true

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "127.0.0.1:12345"
		req.AddCookie(&http.Cookie{
			Name:  model.SessionCookie,
			Value: "wrong-token",
		})

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

// --- Auth: LocalhostAuthExempt ---

func TestAuth_LocalhostAuthExempt_LocalhostIPv4_Returns401(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"
		model.LocalhostAuthExempt = false

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "127.0.0.1:12345"

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestAuth_LocalhostAuthExempt_LocalhostIPv6_Returns401(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"
		model.LocalhostAuthExempt = false

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "[::1]:12345"

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestAuth_LocalhostAuthExempt_WithValidCookie_PassThrough(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"
		model.LocalhostAuthExempt = false

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "127.0.0.1:12345"
		req.AddCookie(&http.Cookie{
			Name:  model.SessionCookie,
			Value: "valid-token",
		})

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestAuth_LocalhostAuthExempt_True_LocalhostStillBypasses(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"
		model.LocalhostAuthExempt = true

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "127.0.0.1:12345"

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

// --- GetProjectFromCookie ---

func TestGetProjectFromCookie_NormalExtraction(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: "/home/user/myproject",
	})

	result := middleware.GetProjectFromCookie(req)
	assert.Equal(t, "/home/user/myproject", result)
}

func TestGetProjectFromCookie_URLEncodedValueDecoded(t *testing.T) {
	encoded := url.QueryEscape("/home/user/my project")
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: encoded,
	})

	result := middleware.GetProjectFromCookie(req)
	assert.Equal(t, "/home/user/my project", result)
}

func TestGetProjectFromCookie_NoCookie_ReturnsEmpty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	result := middleware.GetProjectFromCookie(req)
	assert.Equal(t, "", result)
}

func TestGetProjectFromCookie_EmptyValue_ReturnsEmpty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: "",
	})

	result := middleware.GetProjectFromCookie(req)
	assert.Equal(t, "", result)
}
