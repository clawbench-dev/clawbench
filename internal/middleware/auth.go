package middleware

import (
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"net/url"

	"clawbench/internal/model"
)

// IsLocalhost returns true if the request originates from the local machine.
// CLI subcommands (clawbench task, clawbench rag) always connect from localhost.
func IsLocalhost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// ShouldBypassAuth returns true if the request should skip authentication.
// Localhost requests are trusted when localhost_auth_exempt is enabled
// (the default).
func ShouldBypassAuth(r *http.Request) bool {
	return IsLocalhost(r) && model.LocalhostAuthExempt
}

// Auth wraps a handler with password auth if configured.
// Localhost requests (CLI subcommands) bypass auth when localhost_auth_exempt
// is enabled (the default).
// Remote requests require a valid "clawbench_session" cookie.
func Auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// No password configured — open access
		if model.SessionToken == "" && model.CookieToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Localhost bypass (respects localhost_auth_exempt setting)
		if ShouldBypassAuth(r) {
			next.ServeHTTP(w, r)
			return
		}
		// Remote — cookie-based auth
		// Use CookieToken (cryptographically random) if available; fall back
		// to SessionToken for backward compatibility during migration.
		// (ISS-117, ISS-131, ISS-183)
		validateToken := model.CookieToken
		if validateToken == "" {
			validateToken = model.SessionToken
		}
		token, err := r.Cookie(model.ScopedCookieName(model.SessionCookie))
		if err == nil && token != nil && subtle.ConstantTimeCompare([]byte(token.Value), []byte(validateToken)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		slog.Warn("auth: rejecting request", "path", r.URL.Path, "remote", r.RemoteAddr, "has_cookie", err == nil)
		model.WriteError(w, model.Unauthorized(nil))
	}
}

// GetProjectFromCookie extracts the current project path from cookie.
func GetProjectFromCookie(r *http.Request) string {
	cookie, err := r.Cookie(model.ScopedCookieName("clawbench_project"))
	if err != nil || cookie == nil || cookie.Value == "" {
		return ""
	}
	decoded, decErr := url.QueryUnescape(cookie.Value)
	if decErr != nil {
		return cookie.Value
	}
	return decoded
}
