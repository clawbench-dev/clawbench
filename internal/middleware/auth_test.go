package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

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
	defer func() {
		model.SessionToken = origSession
		model.CookieToken = origCookie
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

// --- Auth: localhost WITHOUT a token must not bypass ---

// The core of the change: a loopback address alone no longer grants access.
// This is what closes the FRP hole, since frpc also dials in from 127.0.0.1.
func TestAuth_LocalhostWithoutToken_Rejected(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"
		model.CookieToken = "instance-key"

		for _, remote := range []string{"127.0.0.1:12345", "[::1]:12345"} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			req.RemoteAddr = remote

			middleware.Auth(okHandler).ServeHTTP(rec, req)

			assert.Equalf(t, http.StatusUnauthorized, rec.Code,
				"loopback %s without a token must be rejected", remote)
		}
	})
}

// --- Auth: localhost WITH a valid AI token bypasses ---

func TestAuth_LocalhostWithAIToken_PassThrough(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"
		model.CookieToken = "instance-key"

		for _, remote := range []string{"127.0.0.1:12345", "[::1]:12345"} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			req.RemoteAddr = remote
			req.Header.Set(model.AITokenHeader, model.SignAIToken(time.Now()))

			middleware.Auth(okHandler).ServeHTTP(rec, req)

			assert.Equalf(t, http.StatusOK, rec.Code,
				"loopback %s with a valid token must pass", remote)
		}
	})
}

// A leaked token must not be replayable from off-machine: the address half of
// IsAITokenRequest is what makes the token non-transferable.
func TestAuth_RemoteWithValidAIToken_Rejected(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"
		model.CookieToken = "instance-key"

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set(model.AITokenHeader, model.SignAIToken(time.Now()))

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestAuth_LocalhostWithExpiredAIToken_Rejected(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"
		model.CookieToken = "instance-key"

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set(model.AITokenHeader, model.SignAIToken(time.Now().Add(-2*model.AITokenTTL)))

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestAuth_LocalhostWithGarbageAIToken_Rejected(t *testing.T) {
	withSavedToken(func() {
		model.SessionToken = "valid-token"
		model.CookieToken = "instance-key"

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set(model.AITokenHeader, "not-a-real-token")

		middleware.Auth(okHandler).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

// --- IsAITokenRequest: direct unit coverage ---

func TestIsAITokenRequest(t *testing.T) {
	withSavedToken(func() {
		model.CookieToken = "instance-key"
		valid := model.SignAIToken(time.Now())

		cases := []struct {
			name       string
			remote     string
			token      string
			wantBypass bool
		}{
			{"loopback with valid token", "127.0.0.1:12345", valid, true},
			{"loopback no token", "127.0.0.1:12345", "", false},
			{"loopback garbage token", "127.0.0.1:12345", "garbage", false},
			{"remote with valid token", "192.168.1.100:12345", valid, false},
			{"remote no token", "192.168.1.100:12345", "", false},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
				req.RemoteAddr = tc.remote
				if tc.token != "" {
					req.Header.Set(model.AITokenHeader, tc.token)
				}
				assert.Equal(t, tc.wantBypass, middleware.IsAITokenRequest(req))
			})
		}
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
