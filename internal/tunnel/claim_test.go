package tunnel

import (
	"encoding/hex"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeConn is a net.Conn whose Close is observable, so tests can prove a parked
// connection was closed rather than merely forgotten.
type fakeConn struct {
	net.Conn // nil: the methods we do not use would panic if called
	closed   chan struct{}
}

func newFakeConn() *fakeConn {
	return &fakeConn{closed: make(chan struct{})}
}

func (c *fakeConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func (c *fakeConn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func TestClaimRegistry_ParkThenClaimSucceeds(t *testing.T) {
	reg := NewClaimRegistry()
	binding := Binding{AuthID: "token-a", ConnID: "conn-1"}
	conn := newFakeConn()

	token, err := reg.Park(binding, 8080, conn, time.Minute)
	if err != nil {
		t.Fatalf("Park: %v", err)
	}
	if token == "" {
		t.Fatal("Park must return a non-empty token")
	}
	if reg.Len() != 1 {
		t.Fatalf("registry should hold 1 pending connection, got %d", reg.Len())
	}

	got, err := reg.Claim(token, binding)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if got != conn {
		t.Fatal("Claim must return the parked connection")
	}
	if conn.isClosed() {
		t.Fatal("a successfully claimed connection must not be closed")
	}
	if reg.Len() != 0 {
		t.Fatalf("a claimed token must be removed, got %d pending", reg.Len())
	}
}

func TestClaimRegistry_TokenIsSingleUse(t *testing.T) {
	reg := NewClaimRegistry()
	binding := Binding{AuthID: "token-a", ConnID: "conn-1"}
	conn := newFakeConn()

	token, err := reg.Park(binding, 8080, conn, time.Minute)
	if err != nil {
		t.Fatalf("Park: %v", err)
	}
	if _, err := reg.Claim(token, binding); err != nil {
		t.Fatalf("first claim must succeed: %v", err)
	}

	if _, err := reg.Claim(token, binding); !errors.Is(err, ErrClaimRejected) {
		t.Fatalf("second claim of the same token must be rejected, got %v", err)
	}
}

// TestClaimRegistry_TokenIsBoundToItsConnection is the cross-connection guard:
// another authenticated client that somehow learned the token value must not be
// able to steal the parked connection.
func TestClaimRegistry_TokenIsBoundToItsConnection(t *testing.T) {
	reg := NewClaimRegistry()
	owner := Binding{AuthID: "token-a", ConnID: "conn-1"}
	other := Binding{AuthID: "token-b", ConnID: "conn-2"}
	conn := newFakeConn()

	token, err := reg.Park(owner, 8080, conn, time.Minute)
	if err != nil {
		t.Fatalf("Park: %v", err)
	}

	if _, err := reg.Claim(token, other); !errors.Is(err, ErrClaimRejected) {
		t.Fatalf("claim from a different connection must be rejected, got %v", err)
	}
	if !conn.isClosed() {
		t.Fatal("a stolen claim attempt must close the parked connection")
	}
	// The token was consumed by the failed attempt, so even the rightful owner
	// can no longer redeem it — a leaked token must not survive misuse.
	if _, err := reg.Claim(token, owner); !errors.Is(err, ErrClaimRejected) {
		t.Fatalf("the token must be burned by a failed claim, got %v", err)
	}
}

func TestClaimRegistry_SameAuthDifferentConnIsRejected(t *testing.T) {
	// Two h2 connections from the same logged-in user are still different
	// sessions: the claim stream must travel on the control stream's own
	// connection.
	reg := NewClaimRegistry()
	owner := Binding{AuthID: "same", ConnID: "conn-1"}
	other := Binding{AuthID: "same", ConnID: "conn-2"}
	conn := newFakeConn()

	token, err := reg.Park(owner, 8080, conn, time.Minute)
	if err != nil {
		t.Fatalf("Park: %v", err)
	}
	if _, err := reg.Claim(token, other); !errors.Is(err, ErrClaimRejected) {
		t.Fatalf("a different connection must be rejected even with the same auth, got %v", err)
	}
}

func TestClaimRegistry_UnknownTokenIsRejected(t *testing.T) {
	reg := NewClaimRegistry()
	if _, err := reg.Claim("not-a-real-token", Binding{}); !errors.Is(err, ErrClaimRejected) {
		t.Fatalf("unknown token must be rejected, got %v", err)
	}
	if _, err := reg.Claim("", Binding{}); !errors.Is(err, ErrClaimRejected) {
		t.Fatalf("empty token must be rejected, got %v", err)
	}
}

func TestClaimRegistry_TimeoutClosesAndInvalidates(t *testing.T) {
	reg := NewClaimRegistry()
	binding := Binding{ConnID: "conn-1"}
	conn := newFakeConn()

	token, err := reg.Park(binding, 8080, conn, 30*time.Millisecond)
	if err != nil {
		t.Fatalf("Park: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for !conn.isClosed() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !conn.isClosed() {
		t.Fatal("an unclaimed connection must be closed after the timeout")
	}
	if reg.Len() != 0 {
		t.Fatalf("the expired token must be removed, got %d pending", reg.Len())
	}
	if _, err := reg.Claim(token, binding); !errors.Is(err, ErrClaimRejected) {
		t.Fatalf("an expired token must be rejected, got %v", err)
	}
}

func TestClaimRegistry_ReleasePortClosesOnlyThatPort(t *testing.T) {
	reg := NewClaimRegistry()
	binding := Binding{ConnID: "conn-1"}
	keep := newFakeConn()
	drop := newFakeConn()

	if _, err := reg.Park(binding, 8080, keep, time.Minute); err != nil {
		t.Fatalf("Park: %v", err)
	}
	if _, err := reg.Park(binding, 9090, drop, time.Minute); err != nil {
		t.Fatalf("Park: %v", err)
	}

	reg.ReleasePort(binding, 9090)

	if !drop.isClosed() {
		t.Fatal("the released port's pending connection must be closed")
	}
	if keep.isClosed() {
		t.Fatal("a pending connection on another port must survive")
	}
	if reg.Len() != 1 {
		t.Fatalf("exactly one pending connection should remain, got %d", reg.Len())
	}
}

func TestClaimRegistry_ReleasePortIgnoresOtherBindings(t *testing.T) {
	reg := NewClaimRegistry()
	owner := Binding{ConnID: "conn-1"}
	other := Binding{ConnID: "conn-2"}
	conn := newFakeConn()

	if _, err := reg.Park(other, 8080, conn, time.Minute); err != nil {
		t.Fatalf("Park: %v", err)
	}
	reg.ReleasePort(owner, 8080)

	if conn.isClosed() {
		t.Fatal("a different control stream's pending connection must not be released")
	}
}

func TestClaimRegistry_ReleaseBindingClosesAllItsPorts(t *testing.T) {
	reg := NewClaimRegistry()
	owner := Binding{ConnID: "conn-1"}
	other := Binding{ConnID: "conn-2"}
	mineA := newFakeConn()
	mineB := newFakeConn()
	theirs := newFakeConn()

	if _, err := reg.Park(owner, 8080, mineA, time.Minute); err != nil {
		t.Fatalf("Park: %v", err)
	}
	if _, err := reg.Park(owner, 9090, mineB, time.Minute); err != nil {
		t.Fatalf("Park: %v", err)
	}
	if _, err := reg.Park(other, 8080, theirs, time.Minute); err != nil {
		t.Fatalf("Park: %v", err)
	}

	reg.ReleaseBinding(owner)

	if !mineA.isClosed() || !mineB.isClosed() {
		t.Fatal("every pending connection of the ending stream must be closed")
	}
	if theirs.isClosed() {
		t.Fatal("another stream's pending connection must survive")
	}
	if reg.Len() != 1 {
		t.Fatalf("only the other stream's connection should remain, got %d", reg.Len())
	}
}

func TestClaimRegistry_DiscardUnknownTokenIsNoop(t *testing.T) {
	reg := NewClaimRegistry()
	reg.Discard("nope") // must not panic
}

// TestNewClaimToken_IsUnpredictableAndDistinct guards the security property:
// tokens must be crypto/rand hex, not a counter or timestamp.
// TestClaimRegistry_ConcurrentClaimsOnlyOneWins races N goroutines against one
// token. Single-use is a stated requirement (claim.go:127-132): the entry is
// deleted under the lock, so exactly one claimant may receive the connection.
// A double-relay of one parked socket would interleave two streams' bytes.
func TestClaimRegistry_ConcurrentClaimsOnlyOneWins(t *testing.T) {
	reg := NewClaimRegistry()
	binding := Binding{ConnID: "conn-1"}
	conn := newFakeConn()

	token, err := reg.Park(binding, 8080, conn, time.Minute)
	if err != nil {
		t.Fatalf("Park: %v", err)
	}

	const claimants = 32
	var (
		start   = make(chan struct{})
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners []net.Conn
		rejects int
	)
	for range claimants {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := reg.Claim(token, binding)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if !errors.Is(err, ErrClaimRejected) {
					t.Errorf("claim returned %v, want ErrClaimRejected", err)
				}
				rejects++
				return
			}
			winners = append(winners, got)
		}()
	}
	close(start)
	wg.Wait()

	if len(winners) != 1 {
		t.Fatalf("exactly one claimant must win, got %d", len(winners))
	}
	if winners[0] != conn {
		t.Fatal("the winner must receive the parked connection")
	}
	if rejects != claimants-1 {
		t.Fatalf("expected %d rejections, got %d", claimants-1, rejects)
	}
	if reg.Len() != 0 {
		t.Fatalf("the registry must be empty after the single successful claim, got %d", reg.Len())
	}
}

// TestClaimRegistry_ParkZeroTimeoutUsesDefault pins claim.go:76-78: a
// non-positive timeout means "use DefaultClaimTimeout". Every other test passes
// an explicit timeout, so this substitution branch is otherwise dead in tests —
// and the production handler is the only caller that could pass 0.
func TestClaimRegistry_ParkZeroTimeoutUsesDefault(t *testing.T) {
	reg := NewClaimRegistry()
	binding := Binding{ConnID: "conn-1"}
	conn := newFakeConn()

	token, err := reg.Park(binding, 8080, conn, 0)
	if err != nil {
		t.Fatalf("Park with a zero timeout must succeed: %v", err)
	}
	if token == "" {
		t.Fatal("Park must still return a token")
	}
	if reg.Len() != 1 {
		t.Fatalf("the connection must be parked, got %d pending", reg.Len())
	}

	// A default-length timeout is minutes, so the connection must still be
	// alive well after a zero would have expired it instantly.
	time.Sleep(50 * time.Millisecond)
	if conn.isClosed() {
		t.Fatal("a zero timeout must mean DefaultClaimTimeout, not immediate expiry")
	}
	if _, err := reg.Claim(token, binding); err != nil {
		t.Fatalf("the token must still be redeemable under the default timeout: %v", err)
	}
}

func TestNewClaimToken_IsUnpredictableAndDistinct(t *testing.T) {
	seen := make(map[string]bool)
	for range 64 {
		tok, err := newClaimToken()
		if err != nil {
			t.Fatalf("newClaimToken: %v", err)
		}
		if len(tok) != claimTokenBytes*2 {
			t.Fatalf("token %q has length %d, want %d hex chars", tok, len(tok), claimTokenBytes*2)
		}
		if _, err := hex.DecodeString(tok); err != nil {
			t.Fatalf("token %q is not hex: %v", tok, err)
		}
		if seen[tok] {
			t.Fatalf("duplicate token generated: %q", tok)
		}
		seen[tok] = true
	}
}
