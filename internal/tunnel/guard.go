package tunnel

import "fmt"

// PortGuard is the port-whitelist / reserved-port decision surface the tunnel
// handlers need. It mirrors internal/ssh/server.go's reverseBindAllowed and
// isReservedPort so the h2 transport enforces exactly the same policy as SSH.
//
// MainPort and SSHPort are passed in explicitly rather than read from the
// registry because the SSH server derives them from its own listener and the
// process port respectively (internal/ssh/server.go:620), and the registry's
// reserved set is only a secondary source.
type PortGuard struct {
	// MainPort is ClawBench's own HTTP port. Never bindable in reverse.
	MainPort int
	// SSHPort is the SSH tunnel port. Binding it would take down the fallback
	// transport, so it is never bindable in reverse.
	SSHPort int
	// IsAllowed reports whether a port falls inside the configured
	// allowed_ports range. Nil means "no policy available", which fails closed.
	IsAllowed func(port int) bool
	// IsReserved reports whether the registry was told to protect a port.
	IsReserved func(port int) bool
}

// ForwardAllowed reports whether a -L (forward) stream may dial the given
// target port.
//
// This mirrors internal/ssh/server.go:738 (handleDirectTCPIP) exactly: the
// forward path consults ONLY the allowed range. It deliberately does not check
// IsNonLocalhostTarget or IsPortRegistered — SSH tunnels operate at the
// transport layer and need no URL-rewriting metadata, and a port does not have
// to be pre-registered to be forwarded.
//
// A nil predicate fails closed: without a registry the server has no
// configured whitelist, and allowing everything would be strictly worse than
// the SSH path (which rejects when its registry is nil).
func (g PortGuard) ForwardAllowed(port int) bool {
	if g.IsAllowed == nil {
		return false
	}
	return g.IsAllowed(port)
}

// ReverseBindAllowed reports whether a reverse (ssh -R) bind of port is
// permitted. It is the boolean form of ReverseBindDenied, kept for callers and
// tests that only need the decision.
func (g PortGuard) ReverseBindAllowed(port int) bool {
	return g.ReverseBindDenied(port) == nil
}

// ReverseBindDenied returns nil when a reverse bind of port is permitted, or
// the specific reason it is not.
//
// It mirrors internal/ssh/server.go:589 (reverseBindAllowed) composed with
// :616-624 (isReservedPort), with one deliberate difference: port == 0 is legal
// and means "let the OS pick". isReservedPort treats port <= 0 as reserved, so
// a naive port-through would reject the documented bind(0) semantics. Here
// port == 0 is allowed through the guard and the actual OS-assigned port is
// re-checked by ListenReverse after the listener exists.
//
// The returned error is one of ErrBindNotAllowed (outside the whitelist or out
// of range) or ErrBindReservedOrTaken (ClawBench's own ports or a
// registry-reserved port); the handler maps those onto bind_err codes 2 and 3.
func (g PortGuard) ReverseBindDenied(port int) error {
	if port == 0 {
		return nil // OS-assigned; ListenReverse re-checks the real port
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("%w: %d out of range 1-65535", ErrBindNotAllowed, port)
	}
	if port == g.MainPort || port == g.SSHPort {
		return fmt.Errorf("%w: %d is reserved for ClawBench", ErrBindReservedOrTaken, port)
	}
	if g.IsReserved != nil && g.IsReserved(port) {
		return fmt.Errorf("%w: %d is reserved", ErrBindReservedOrTaken, port)
	}
	if g.IsAllowed == nil {
		return fmt.Errorf("%w: no port policy configured", ErrBindNotAllowed)
	}
	if !g.IsAllowed(port) {
		return fmt.Errorf("%w: %d outside allowed_ports", ErrBindNotAllowed, port)
	}
	return nil
}
