package middleware

import (
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"clawbench/internal/model"
)

// IsLocalhost returns true if the request originates from the local machine.
// The AI subprocesses that serve built-in slash commands always connect from
// localhost.
//
// This is an address check only and is not sufficient on its own to establish
// trust: the FRP tunnel's frpc process also dials in from 127.0.0.1. Callers
// must pair it with a credential (see IsAITokenRequest).
func IsLocalhost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// IsAITokenRequest returns true if the request should skip authentication
// because it carries a valid AI token.
//
// Two conditions must hold, and the address check is the weaker of the two:
//   - IsLocalhost(r) rejects a token replayed from off-machine. It cannot
//     distinguish the FRP tunnel's frpc process, which dials in from
//     127.0.0.1 (internal/frp), so the signature is what actually gates FRP.
//   - A valid, unexpired signature over the header value. The token is signed
//     with model.CookieToken, so rotating it (as a password change does)
//     invalidates every in-flight token.
//
// The AI subprocess is the only intended caller: the built-in slash commands
// inject a freshly signed token into their prompt (internal/handler). Local
// browsers hold no token and must log in.
func IsAITokenRequest(r *http.Request) bool {
	return IsLocalhost(r) && model.VerifyAIToken(r.Header.Get(model.AITokenHeader), time.Now())
}

// Auth wraps a handler with password auth if configured.
// Requests carrying a valid AI token from localhost bypass auth; every other
// request requires a valid "clawbench_session" cookie.
func Auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// No password configured — open access
		if model.SessionToken == "" && model.CookieToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Local AI subprocess holding a short-lived signed token
		if IsAITokenRequest(r) {
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
