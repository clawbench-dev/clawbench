package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
)

// --- HTTP login rate limiter (ISS-003c) ---
// Ported from internal/ssh/server.go authTracker pattern.

type ipRecord struct {
	failCount    int
	lastFail     time.Time
	blockedUntil time.Time
}

type loginLimiter struct {
	mu      sync.Mutex
	records map[string]*ipRecord
}

const (
	maxLoginFails        = 5
	initialLoginBlock    = 5 * time.Minute
	maxLoginBlock        = 1 * time.Hour
	loginCleanupInterval = 10 * time.Minute
	loginRecordTTL       = 30 * time.Minute
)

var (
	globalLoginLimiter     *loginLimiter
	globalLoginLimiterOnce sync.Once
)

func getLoginLimiter() *loginLimiter {
	globalLoginLimiterOnce.Do(func() {
		globalLoginLimiter = &loginLimiter{records: make(map[string]*ipRecord)}
		go globalLoginLimiter.cleanupLoop()
	})
	return globalLoginLimiter
}

func (l *loginLimiter) isBlocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.records[ip]
	if !ok {
		return false
	}
	if rec.blockedUntil.IsZero() || time.Now().Before(rec.blockedUntil) {
		return !rec.blockedUntil.IsZero()
	}
	rec.blockedUntil = time.Time{}
	rec.failCount = 0
	return false
}

func (l *loginLimiter) recordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.records[ip]
	if !ok {
		rec = &ipRecord{}
		l.records[ip] = rec
	}
	rec.failCount++
	rec.lastFail = time.Now()
	if rec.failCount >= maxLoginFails {
		infractions := rec.failCount / maxLoginFails
		dur := initialLoginBlock * time.Duration(1<<uint(infractions-1))
		if dur > maxLoginBlock {
			dur = maxLoginBlock
		}
		rec.blockedUntil = rec.lastFail.Add(dur)
	}
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.records, ip)
}

func (l *loginLimiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for ip, rec := range l.records {
		if (rec.blockedUntil.IsZero() || now.After(rec.blockedUntil)) &&
			now.Sub(rec.lastFail) > loginRecordTTL {
			delete(l.records, ip)
		}
	}
}

func (l *loginLimiter) cleanupLoop() {
	ticker := time.NewTicker(loginCleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		l.cleanup()
	}
}

// --- Auth handlers ---

// ServeAuthCheck returns 200 if the session cookie is valid, 401 otherwise.
// Localhost requests are always considered authenticated (same as middleware.Auth).
func ServeAuthCheck(w http.ResponseWriter, r *http.Request) {
	if model.SessionToken == "" && model.CookieToken == "" {
		// No password set, always authenticated
		w.WriteHeader(http.StatusOK)
		return
	}
	// Localhost bypass (respects localhost_auth_exempt setting)
	if middleware.ShouldBypassAuth(r) {
		w.WriteHeader(http.StatusOK)
		return
	}
	// Use CookieToken (cryptographically random) if available; fall back
	// to SessionToken for backward compatibility during migration.
	// (ISS-117, ISS-131, ISS-183)
	validateToken := model.CookieToken
	if validateToken == "" {
		validateToken = model.SessionToken
	}
	token, err := r.Cookie(model.ScopedCookieName(model.SessionCookie))
	if err != nil || token == nil || subtle.ConstantTimeCompare([]byte(token.Value), []byte(validateToken)) != 1 {
		writeLocalizedError(w, r, model.Unauthorized(nil))
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ServeLogin handles GET (login page) and POST (login attempt).
func ServeLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Serve index.html which mounts the Vue app (LoginView handles auth UI)
		http.ServeFile(w, r, "public/index.html")
		return
	}
	if r.Method == http.MethodPost {
		remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
		if remoteIP == "" {
			remoteIP = r.RemoteAddr
		}

		// Rate limiting check (ISS-003c)
		limiter := getLoginLimiter()
		if limiter.isBlocked(remoteIP) {
			slog.Warn("login blocked: too many failures", slog.String("ip", remoteIP))
			writeLocalizedErrorf(w, r, http.StatusTooManyRequests, "TooManyLoginAttempts")
			return
		}

		var body struct{ Password string }
		// ISS-118: Limit request body to 4KB to prevent memory exhaustion DoS.
		// A password never needs more than a few hundred bytes.
		limitedReader := io.LimitReader(r.Body, 4*1024)
		// ISS-146: Check Decode error explicitly — malformed JSON must return 400
		// rather than silently falling through to bcrypt with an empty password.
		if err := json.NewDecoder(limitedReader).Decode(&body); err != nil {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}

		// Password verification (ISS-003a)
		var authenticated bool
		if model.SessionToken == "" {
			// No password configured
			authenticated = true
		} else if model.PasswordIsSHA256 {
			// Password stored as SHA-256 hash — hash the submitted password and compare
			hash := sha256.Sum256([]byte(body.Password + "clawbench-salt"))
			candidate := hex.EncodeToString(hash[:])
			authenticated = subtle.ConstantTimeCompare([]byte(candidate), []byte(model.SessionToken)) == 1
		} else if model.PasswordHash != nil {
			// bcrypt verification (plaintext password in config)
			authenticated = bcrypt.CompareHashAndPassword(model.PasswordHash, []byte(body.Password)) == nil
		} else {
			// No bcrypt hash available — bcrypt generation must have failed at startup.
			// Reject the login rather than falling back to insecure comparison.
			slog.Error("password hash not available, rejecting login", slog.String("remoteIP", remoteIP))
			writeLocalizedError(w, r, model.Internal(nil))
			limiter.recordFailure(remoteIP)
			return
		}

		if authenticated {
			limiter.reset(remoteIP)
			// Generate a cryptographically random session token for the cookie.
			// This decouples the cookie value from the password hash, so that
			// obtaining the cookie does not reveal or allow computation of the
			// password hash. (ISS-117, ISS-131, ISS-183)
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
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		} else {
			limiter.recordFailure(remoteIP)
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": false})
		}
		return
	}
	writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
}
