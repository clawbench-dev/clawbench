package handler

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

// DeepLink builds the clawbench://connect deep link with the current token.
func (q *QRTokenManager) DeepLink() string {
	q.mu.Lock()
	tok := q.token
	q.mu.Unlock()
	return fmt.Sprintf("clawbench://connect?lan=%s&frp=%s&token=%s",
		url.QueryEscape(q.lanURL), url.QueryEscape(q.frpURL), tok)
}

// Expiry returns the token expiry time.
func (q *QRTokenManager) Expiry() time.Time {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.expiry
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

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":    false,
			"error": "invalid request body",
		})
		return
	}

	if !qrTokenMgr.Validate(body.Token) {
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

// ServeQRCode returns the QR code deep link URL and expiry for frontend rendering.
// No auth required — this is used on the login page before authentication.
// GET /api/auth/qr-code
func ServeQRCode(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if qrTokenMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": "QR login not available",
		})
		return
	}

	// If token is expired or used, regenerate automatically
	if qrTokenMgr.IsExpired() {
		qrTokenMgr.Regenerate()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"url":      qrTokenMgr.DeepLink(),
		"expiresAt": qrTokenMgr.Expiry().Unix(),
	})
}
