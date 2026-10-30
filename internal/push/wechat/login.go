package wechat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	ilink "github.com/openilink/openilink-sdk-go"
)

// LoginState is the lifecycle stage of one QR login session.
type LoginState string

const (
	LoginPending   LoginState = "pending"   // QR issued, waiting for a scan
	LoginScanned   LoginState = "scanned"   // scanned, waiting for the user to confirm
	LoginConnected LoginState = "connected" // credentials persisted
	LoginExpired   LoginState = "expired"   // 8-minute timeout or too many QR refreshes
	LoginFailed    LoginState = "failed"    // transport error, or persisting the credentials failed
	LoginCancelled LoginState = "cancelled" // cancelled by the client or on shutdown
)

// terminal reports whether the session will not change again.
func (s LoginState) terminal() bool {
	switch s {
	case LoginConnected, LoginExpired, LoginFailed, LoginCancelled:
		return true
	}
	return false
}

// LoginResult is what a successful scan yields.
type LoginResult struct {
	BotToken string
	BotID    string
	BaseURL  string
	UserID   string
}

// LoginSnapshot is the JSON-facing view of one login session.
type LoginSnapshot struct {
	ID         string     `json:"login_id"`
	State      LoginState `json:"state"`
	QRURL      string     `json:"qr_url"`
	QRSeq      int        `json:"qr_seq"`
	Refresh    int        `json:"refresh"`
	RefreshMax int        `json:"refresh_max"`
	BotID      string     `json:"bot_id"`
	Message    string     `json:"message"`
	Error      string     `json:"error"`
	CreatedAt  time.Time  `json:"-"`
	UpdatedAt  time.Time  `json:"-"`
	ExpiresAt  time.Time  `json:"expires_at"`
}

// loginSessionTTL bounds how long a finished session stays readable, so a
// client that polls late still sees the outcome instead of a 404.
const loginSessionTTL = 10 * time.Minute

// maxQRRefreshCount mirrors the SDK's internal cap. It is surfaced so the UI
// can show "refreshing 2/3" rather than an unexplained new QR code.
const maxQRRefreshCount = 3

// LoginServiceOptions configures a LoginService.
type LoginServiceOptions struct {
	// BaseURL overrides the iLink endpoint; empty means the SDK default.
	// Tests point this at an httptest server.
	BaseURL string
	// HTTPClient is the transport for the login client. Nil means the SDK's
	// default.
	HTTPClient ilink.HTTPDoer
	// Persist stores the credentials. Called exactly once, BEFORE the state
	// becomes connected: a session that reports success must be durable.
	Persist func(LoginResult) error
	// Reload is called after a successful persist to make the new credentials
	// take effect (hot-reload restart of the Manager). Never called on failure.
	Reload func()
	// Now is the clock, injectable for tests.
	Now func() time.Time
}

// LoginService runs QR logins in the background.
//
// The SDK's LoginWithQR blocks for up to 8 minutes (it polls the scan status and
// refreshes the QR up to 3 times). An HTTP handler cannot sit on that, and a
// browser cannot poll it directly either (a single poll is itself a 35-second
// long-poll). So each login runs in its own goroutine and the HTTP layer only
// reads snapshots.
type LoginService struct {
	opts LoginServiceOptions

	mu       sync.Mutex
	sessions map[string]*loginSession
	activeID string
}

type loginSession struct {
	snap   LoginSnapshot
	cancel context.CancelFunc
	done   chan struct{}
}

// NewLoginService creates a LoginService.
func NewLoginService(opts LoginServiceOptions) *LoginService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &LoginService{
		opts:     opts,
		sessions: make(map[string]*loginSession),
	}
}

// Start begins a login, or returns the one already in flight.
//
// Idempotent on purpose: the settings panel is re-rendered on every entry, and a
// second Start that spawned a second goroutine would leave the first one polling
// for up to 8 minutes with no way to reach it.
func (s *LoginService) Start(ctx context.Context) LoginSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reapLocked()

	if s.activeID != "" {
		if sess, ok := s.sessions[s.activeID]; ok && !sess.snap.State.terminal() {
			return sess.snap
		}
	}

	id := newLoginID()
	// Detached from the caller's context on purpose. Start is called from an HTTP
	// handler, and net/http cancels r.Context() the moment the handler returns —
	// which is immediately after the 202 is written. Deriving the login goroutine
	// from it would abort FetchQRCode within a millisecond of the response, so no
	// QR would ever reach the user. The goroutine's lifetime is bounded by
	// LoginWithQR's own 8-minute budget plus the explicit Cancel/CancelAll paths.
	loginCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	sess := &loginSession{
		snap: LoginSnapshot{
			ID:         id,
			State:      LoginPending,
			RefreshMax: maxQRRefreshCount,
			CreatedAt:  s.opts.Now(),
			UpdatedAt:  s.opts.Now(),
			ExpiresAt:  s.opts.Now().Add(defaultLoginTimeout),
		},
		cancel: cancel,
		done:   make(chan struct{}),
	}
	s.sessions[id] = sess
	s.activeID = id

	go s.run(loginCtx, id)

	return sess.snap
}

// defaultLoginTimeout matches the SDK's internal login timeout.
const defaultLoginTimeout = 8 * time.Minute

// Poll returns the current snapshot, or false when the id is unknown (the
// client should start a new login).
func (s *LoginService) Poll(id string) (LoginSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return LoginSnapshot{}, false
	}
	return sess.snap, true
}

// Cancel aborts a login. Idempotent: an unknown or finished id is a no-op.
func (s *LoginService) Cancel(id string) {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	if ok && s.activeID == id {
		s.activeID = ""
	}
	s.mu.Unlock()

	if !ok {
		return
	}
	sess.cancel()
}

// CancelAll aborts every login. Called on server shutdown.
func (s *LoginService) CancelAll() {
	s.mu.Lock()
	sessions := make([]*loginSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.activeID = ""
	s.mu.Unlock()

	for _, sess := range sessions {
		sess.cancel()
	}
}

// reapLocked drops sessions that finished long ago, so the map cannot grow
// without bound across many login attempts.
func (s *LoginService) reapLocked() {
	now := s.opts.Now()
	for id, sess := range s.sessions {
		if !sess.snap.State.terminal() {
			continue
		}
		if now.Sub(sess.snap.UpdatedAt) > loginSessionTTL {
			delete(s.sessions, id)
		}
	}
}

// update rewrites a session's snapshot under the lock.
func (s *LoginService) update(id string, fn func(*LoginSnapshot)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return
	}
	fn(&sess.snap)
	sess.snap.UpdatedAt = s.opts.Now()
}

// run performs the login and records its outcome.
func (s *LoginService) run(ctx context.Context, id string) {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return
	}
	defer close(sess.done)

	opts := []ilink.Option{}
	if s.opts.BaseURL != "" {
		opts = append(opts, ilink.WithBaseURL(s.opts.BaseURL))
	}
	if s.opts.HTTPClient != nil {
		opts = append(opts, ilink.WithHTTPClient(s.opts.HTTPClient))
	}
	// A dedicated client, never the Manager's: a failed login must not leave a
	// half-configured client behind, and a successful one only contributes its
	// credentials to the hot-reload restart.
	client := ilink.NewClient("", opts...)

	seq := 0
	result, err := client.LoginWithQR(ctx, &ilink.LoginCallbacks{
		OnQRCode: func(content string) {
			seq++
			s.update(id, func(sn *LoginSnapshot) {
				sn.QRURL = content
				sn.QRSeq = seq
				if seq > 1 {
					sn.Refresh = seq - 1
				}
				if sn.State == LoginPending {
					return
				}
				sn.State = LoginPending
			})
		},
		OnScanned: func() {
			s.update(id, func(sn *LoginSnapshot) { sn.State = LoginScanned })
		},
		OnExpired: func(attempt, max int) {
			s.update(id, func(sn *LoginSnapshot) {
				sn.Refresh = attempt
				sn.RefreshMax = max
			})
		},
	})

	// Cancellation wins over whatever the SDK returned: the user asked to stop,
	// so the outcome is "cancelled" even if the client happened to finish.
	if ctx.Err() != nil {
		s.update(id, func(sn *LoginSnapshot) {
			if !sn.State.terminal() {
				sn.State = LoginCancelled
			}
		})
		return
	}

	if err != nil {
		slog.Warn("wechat: QR login failed", "error", err)
		s.update(id, func(sn *LoginSnapshot) {
			sn.State = LoginFailed
			sn.Error = err.Error()
		})
		return
	}

	if !result.Connected {
		// The SDK reports a timeout or a user-side abort as a nil error with
		// Connected=false; the Message field explains which.
		msg := result.Message
		if msg == "" {
			msg = "login did not complete"
		}
		s.update(id, func(sn *LoginSnapshot) {
			sn.State = LoginExpired
			sn.Message = msg
		})
		return
	}

	login := LoginResult{
		BotToken: result.BotToken,
		BotID:    result.BotID,
		BaseURL:  result.BaseURL,
		UserID:   result.UserID,
	}

	if s.opts.Persist != nil {
		if persistErr := s.opts.Persist(login); persistErr != nil {
			slog.Warn("wechat: persist login credentials failed", "error", persistErr)
			s.update(id, func(sn *LoginSnapshot) {
				sn.State = LoginFailed
				sn.Error = persistErr.Error()
			})
			return
		}
	}

	s.update(id, func(sn *LoginSnapshot) {
		sn.State = LoginConnected
		sn.BotID = login.BotID
	})

	// Called OUTSIDE the lock: it restarts the Manager, which must not happen
	// while a Poll could be blocked behind s.mu.
	if s.opts.Reload != nil {
		s.opts.Reload()
	}
}

// newLoginID returns a short opaque session id.
func newLoginID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("wl-%d", time.Now().UnixNano())
	}
	return "wl-" + hex.EncodeToString(buf)
}
