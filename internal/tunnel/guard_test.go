package tunnel

import (
	"errors"
	"testing"
)

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
	if g.ReverseBindAllowed(8080) {
		t.Fatal("nil IsAllowed must fail closed on the reverse path too")
	}
}

// TestPortGuard_ReverseBindDenied_MatchesSSHSemantics is the parity table:
// every branch of internal/ssh/server.go:589+616-624, plus the deliberate
// port==0 carve-out the SSH code cannot express.
func TestPortGuard_ReverseBindDenied_MatchesSSHSemantics(t *testing.T) {
	reserved := map[int]bool{9000: true}
	g := PortGuard{
		MainPort:   20000,
		SSHPort:    20001,
		IsAllowed:  func(p int) bool { return p >= 1024 && p <= 65535 },
		IsReserved: func(p int) bool { return reserved[p] },
	}

	cases := []struct {
		name    string
		port    int
		wantErr error
	}{
		{"zero means OS-assigned and is allowed through", 0, nil},
		{"negative is rejected", -1, ErrBindNotAllowed},
		{"main port is reserved", 20000, ErrBindReservedOrTaken},
		{"ssh port is reserved", 20001, ErrBindReservedOrTaken},
		{"registry-reserved port is rejected", 9000, ErrBindReservedOrTaken},
		{"below the allowed range is rejected", 80, ErrBindNotAllowed},
		{"inside the allowed range is accepted", 5173, nil},
		{"top of the allowed range is accepted", 65535, nil},
		{"above 65535 is rejected", 65536, ErrBindNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := g.ReverseBindDenied(tc.port)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ReverseBindDenied(%d) = %v, want nil", tc.port, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ReverseBindDenied(%d) = %v, want %v", tc.port, err, tc.wantErr)
			}
		})
	}
}

// TestPortGuard_ReverseBindDenied_ZeroBypassesReservedPredicate documents the
// trap this guard exists to avoid: isReservedPort's `port <= 0` branch would
// reject the legal bind(0) request if it were reused verbatim. Even a registry
// that claims 0 is reserved must not block the OS-assigned path — the actual
// port is validated after Listen instead.
func TestPortGuard_ReverseBindDenied_ZeroBypassesReservedPredicate(t *testing.T) {
	g := PortGuard{
		MainPort:   20000,
		SSHPort:    20001,
		IsAllowed:  func(int) bool { return true },
		IsReserved: func(p int) bool { return p <= 0 },
	}
	if err := g.ReverseBindDenied(0); err != nil {
		t.Fatalf("port 0 must pass the pre-listen guard, got %v", err)
	}
}

func TestPortGuard_ReverseBindDenied_MainPortComparedBeforeWhitelist(t *testing.T) {
	// Even if the operator whitelists 20000, the reserved check must win:
	// binding ClawBench's own HTTP port would take the platform down.
	g := PortGuard{
		MainPort:  20000,
		SSHPort:   20001,
		IsAllowed: func(p int) bool { return p == 20000 },
	}
	if err := g.ReverseBindDenied(20000); !errors.Is(err, ErrBindReservedOrTaken) {
		t.Fatalf("main port must stay reserved regardless of the whitelist, got %v", err)
	}
}

func TestPortGuard_ReverseBindDenied_NilIsReservedIsNotAFailure(t *testing.T) {
	g := PortGuard{MainPort: 20000, IsAllowed: func(int) bool { return true }}
	if err := g.ReverseBindDenied(8080); err != nil {
		t.Fatalf("nil IsReserved must be treated as 'nothing reserved', got %v", err)
	}
}
