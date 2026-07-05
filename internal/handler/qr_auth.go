package handler

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"log/slog"

	"clawbench/internal/model"
)

// QRTokenManager manages short-lived one-shot tokens for QR code authentication.
type QRTokenManager struct {
	mu     sync.Mutex
	token  string
	expiry time.Time
	used   bool
	ttl    time.Duration
	lanURL string // LAN address for deep link
	frpURL string // FRP tunnel address for deep link
}

// NewQRTokenManager creates a QR token manager with the given TTL and connection URLs.
func NewQRTokenManager(ttl time.Duration, lanURL, frpURL string) *QRTokenManager {
	return &QRTokenManager{ttl: ttl, lanURL: lanURL, frpURL: frpURL}
}

// Generate creates a new random QR login token.
func (q *QRTokenManager) Generate() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.token = model.GenerateRandomToken(16) // 32 hex chars
	q.expiry = time.Now().Add(q.ttl)
	q.used = false
	return q.token
}

// Validate checks if the given token is valid, unused, and not expired.
// If valid, the token is marked as used (one-shot).
func (q *QRTokenManager) Validate(input string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.used || time.Now().After(q.expiry) {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(input), []byte(q.token)) != 1 {
		return false
	}
	q.used = true
	return true
}

// Regenerate creates a new token (same as Generate, but explicit name for API).
func (q *QRTokenManager) Regenerate() string {
	return q.Generate()
}

// IsExpired returns whether the current token has expired or been used.
func (q *QRTokenManager) IsExpired() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.used || time.Now().After(q.expiry)
}

// qrTokenMgr holds the global QR token manager, set from main.go.
var qrTokenMgr *QRTokenManager

// SetQRTokenManager stores the QR token manager for handler access.
func SetQRTokenManager(m *QRTokenManager) {
	qrTokenMgr = m
}

// ServeQRTokenAuth accepts a short-lived QR token and exchanges it for a session cookie.
// POST /api/auth/qr-token
func ServeQRTokenAuth(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if qrTokenMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": "QR login not available",
		})
		return
	}

	// Rate limit: reuse the same IP-based login rate limiter as /login
	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}
	limiter := getLoginLimiter()
	if limiter.isBlocked(remoteIP) {
		slog.Warn("QR token auth blocked: too many failures", slog.String("ip", remoteIP))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"ok":    false,
			"error": "too many attempts, try again later",
		})
		return
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4*1024)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":    false,
			"error": "invalid request body",
		})
		return
	}

	if !qrTokenMgr.Validate(body.Token) {
		limiter.recordFailure(remoteIP)
		slog.Warn("QR token auth failed: token invalid, expired, or already used",
			slog.String("remote", r.RemoteAddr))
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"ok":    false,
			"error": "token invalid, expired, or already used",
		})
		return
	}

	// Token is valid — issue session cookie (same as /login)
	cookieToken := model.CookieToken
	if cookieToken == "" {
		cookieToken = model.GenerateRandomToken(32)
		model.CookieToken = cookieToken
		model.PersistCookieToken(cookieToken)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     model.ScopedCookieName(model.SessionCookie),
		Value:    cookieToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		MaxAge:   int(7 * 24 * 3600),
		SameSite: http.SameSiteLaxMode,
	})

	slog.Info("QR token auth succeeded", slog.String("remote", r.RemoteAddr))
	limiter.reset(remoteIP)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ServeQRTokenRegenerate generates a new QR token and returns it.
// Requires authentication. POST /api/auth/qr-token/regenerate
func ServeQRTokenRegenerate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if qrTokenMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": "QR login not available",
		})
		return
	}

	newToken := qrTokenMgr.Regenerate()
	slog.Info("QR token regenerated", slog.String("remote", r.RemoteAddr))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"token": newToken,
	})
}
