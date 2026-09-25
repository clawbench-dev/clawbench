package tunnel

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"sync"
	"time"
)

// DefaultClaimTimeout bounds how long an accepted reverse connection waits to
// be claimed by a client stream. A reverse connection is parked (not relayed)
// until the client that owns the bound port sends a claim stream, so without a
// deadline a token leak or a crashed client would pin the socket forever. The
// client is expected to claim within milliseconds; this is generous.
const DefaultClaimTimeout = 15 * time.Second

// claimTokenBytes is the entropy of a claim token. 32 bytes of crypto/rand is
// far beyond guessable; the token is the only thing standing between an
// arbitrary authenticated client and someone else's parked connection.
const claimTokenBytes = 32

// ErrClaimRejected is returned when a claim token is unknown, already used,
// expired, or presented by a different authenticated connection. All four
// cases are deliberately indistinguishable to the caller: telling an attacker
// which one it was would confirm that a guessed token exists.
var ErrClaimRejected = errors.New("claim rejected")

// Binding identifies the authenticated connection a control stream belongs to.
//
// The claim token is scoped to this value, so a token minted for one client
// cannot be redeemed by another. Both halves are needed:
//
//   - AuthID is the session cookie the request authenticated with. It stops a
//     different logged-in client from redeeming a token even if it somehow
//     learned the value.
//   - ConnID is the underlying TCP connection (the HTTP/2 session). Every
//     stream on one h2 connection shares it, which is exactly the granularity
//     the design requires — the claim stream must travel on the same
//     authenticated connection as its control stream. It is also what makes
//     the binding meaningful when authentication is disabled (AuthID is then
//     empty for everyone).
type Binding struct {
	AuthID string
	ConnID string
}

// ClaimRegistry parks accepted reverse connections until the owning client
// redeems the single-use token it was handed over the control stream.
//
// Every token is removed from the table the moment it is redeemed, discarded,
// or expired, so a token can never be used twice.
type ClaimRegistry struct {
	mu      sync.Mutex
	pending map[string]*pendingConn
}

// pendingConn is one parked connection plus the identity allowed to claim it.
type pendingConn struct {
	conn    net.Conn
	port    int
	binding Binding
	timer   *time.Timer
}

// NewClaimRegistry returns an empty registry.
func NewClaimRegistry() *ClaimRegistry {
	return &ClaimRegistry{pending: make(map[string]*pendingConn)}
}

// Park stores conn until it is claimed, discarded, or timeout elapses, and
// returns the single-use token the client must present. On timeout the
// connection is closed and the token invalidated.
func (r *ClaimRegistry) Park(binding Binding, port int, conn net.Conn, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = DefaultClaimTimeout
	}

	token, err := newClaimToken()
	if err != nil {
		return "", err
	}

	pc := &pendingConn{conn: conn, port: port, binding: binding}

	r.mu.Lock()
	// A collision is cryptographically impossible, but silently overwriting an
	// existing entry would orphan that connection's token, so retry instead.
	for r.pending[token] != nil {
		if token, err = newClaimToken(); err != nil {
			r.mu.Unlock()
			return "", err
		}
	}
	r.pending[token] = pc
	pc.timer = time.AfterFunc(timeout, func() { r.Discard(token) })
	r.mu.Unlock()

	return token, nil
}

// Claim redeems token for the connection it was parked for and returns the
// parked connection. It returns a non-nil error and a nil connection when the
// token is unknown, expired, already used, or was minted for a different
// connection.
//
// The token is consumed before the binding is compared, so a wrong-connection
// attempt burns the token rather than leaving it live for a retry — a stolen
// token must not survive a failed redemption.
func (r *ClaimRegistry) Claim(token string, binding Binding) (net.Conn, error) {
	pc, err := r.ClaimConn(token, binding)
	if err != nil {
		return nil, err
	}
	return pc.Conn, nil
}

// ClaimConn is Claim plus the metadata the handler needs to re-arm the parked
// connection: its port (for logging and to release it with the right bind) and
// the binding it was parked for (to re-derive ownership).
func (r *ClaimRegistry) ClaimConn(token string, binding Binding) (*ParkedConn, error) {
	if token == "" {
		return nil, ErrClaimRejected
	}

	r.mu.Lock()
	pc, ok := r.pending[token]
	if ok {
		delete(r.pending, token)
	}
	r.mu.Unlock()

	if !ok {
		return nil, ErrClaimRejected
	}
	if pc.timer != nil {
		pc.timer.Stop()
	}
	if pc.binding != binding {
		_ = pc.conn.Close()
		return nil, ErrClaimRejected
	}
	return &ParkedConn{Conn: pc.conn, Port: pc.port, Binding: pc.binding}, nil
}

// ParkedConn is a parked connection together with the bind it arrived on.
type ParkedConn struct {
	Conn    net.Conn
	Port    int
	Binding Binding
}

// Discard removes token (if still present) and closes its connection. It is the
// timeout path and is also used when a control stream dies before its
// connection could be claimed.
func (r *ClaimRegistry) Discard(token string) {
	r.mu.Lock()
	pc, ok := r.pending[token]
	if ok {
		delete(r.pending, token)
	}
	r.mu.Unlock()

	if !ok {
		return
	}
	if pc.timer != nil {
		pc.timer.Stop()
	}
	_ = pc.conn.Close()
}

// ReleasePort closes every pending connection bound to binding on port. Called
// when a single reverse bind is released: connections accepted on that port can
// no longer be claimed.
func (r *ClaimRegistry) ReleasePort(binding Binding, port int) {
	r.closeMatching(func(pc *pendingConn) bool {
		return pc.binding == binding && pc.port == port
	})
}

// ReleaseBinding closes every pending connection owned by binding. Called when
// the control stream ends, so no parked socket outlives its session.
func (r *ClaimRegistry) ReleaseBinding(binding Binding) {
	r.closeMatching(func(pc *pendingConn) bool { return pc.binding == binding })
}

// Len reports the number of parked connections. Used by tests.
func (r *ClaimRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}

// closeMatching removes and closes every entry satisfying match. Entries are
// collected under the lock and closed outside it so a slow Close cannot stall
// unrelated claims.
func (r *ClaimRegistry) closeMatching(match func(*pendingConn) bool) {
	r.mu.Lock()
	victims := make([]*pendingConn, 0, len(r.pending))
	for token, pc := range r.pending {
		if match(pc) {
			delete(r.pending, token)
			victims = append(victims, pc)
		}
	}
	r.mu.Unlock()

	for _, pc := range victims {
		if pc.timer != nil {
			pc.timer.Stop()
		}
		_ = pc.conn.Close()
	}
}

// newClaimToken returns a fresh hex-encoded crypto/rand token.
func newClaimToken() (string, error) {
	buf := make([]byte, claimTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
