package tunnel

import "testing"

func TestPortGuard_ForwardAllowed_OnlyChecksWhitelist(t *testing.T) {
	g := PortGuard{IsAllowed: func(p int) bool { return p == 8080 }}
	if !g.ForwardAllowed(8080) {
		t.Fatal("allowed port must pass")
	}
	if g.ForwardAllowed(9090) {
		t.Fatal("non-whitelisted port must be rejected")
	}
}

// TestPortGuard_ForwardAllowed_DoesNotRequireRegistration pins the SSH parity
// point: internal/ssh/server.go:738 consults ONLY IsPortAllowed, so a port that
// is inside the range but never registered must still be forwardable. The guard
// has no registration input at all, which is exactly the intent.
func TestPortGuard_ForwardAllowed_DoesNotRequireRegistration(t *testing.T) {
	g := PortGuard{IsAllowed: func(p int) bool { return p >= 1024 && p <= 65535 }}
	if !g.ForwardAllowed(54321) {
		t.Fatal("allowed-but-unregistered port must be forwardable (SSH parity)")
	}
}

// The main server port is not special on the forward path: SSH's
// handleDirectTCPIP never compares against mainPort, so if the whitelist
// happens to include 20000 the tunnel must honor it rather than inventing an
// extra reserved-port rule the SSH path does not have.
func TestPortGuard_ForwardAllowed_MainPortNotSpecial(t *testing.T) {
	g := PortGuard{IsAllowed: func(p int) bool { return p == 20000 }}
	if !g.ForwardAllowed(20000) {
		t.Fatal("forward path must defer entirely to the whitelist (SSH parity)")
	}
}

func TestPortGuard_NilPredicateFailsClosed(t *testing.T) {
	var g PortGuard
	if g.ForwardAllowed(8080) {
		t.Fatal("nil IsAllowed must fail closed")
	}
}
