package tunnel

// PortGuard is the port-whitelist decision surface the tunnel handlers need.
// It is a thin adapter over service.ProxyRegistry.IsPortAllowed so the HTTP
// layer does not import the registry directly and the policy stays unit
// testable.
type PortGuard struct {
	// IsAllowed reports whether a port falls inside the configured
	// allowed_ports range. Nil means "no policy available", which fails closed.
	IsAllowed func(port int) bool
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
