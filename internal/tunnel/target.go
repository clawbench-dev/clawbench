// Package tunnel holds the transport-agnostic logic shared by the HTTP/2
// stream tunnel handlers: target parsing/validation, the port-whitelist guard
// (kept semantically identical to internal/ssh/server.go) and the duplex relay
// primitives.
//
// Everything here is deliberately free of net/http and of internal/service so
// it can be unit-tested table-first.
package tunnel

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

// DefaultTargetHost is the host dialed when the client omits `host`. A tunnel
// target is overwhelmingly a service on the server's own loopback interface,
// and the SSH forward path makes the same substitution.
const DefaultTargetHost = "127.0.0.1"

// ErrInvalidPort is returned when the requested target port is absent, not a
// number, or outside 1..65535.
var ErrInvalidPort = errors.New("invalid target port")

// Target is a validated tunnel destination.
type Target struct {
	Host string
	Port int
}

// Addr returns the dialable "host:port" form, using net.JoinHostPort so IPv6
// literals are bracketed.
func (t Target) Addr() string {
	return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

// ParseTarget validates the `host` / `port` query parameters of a stream
// request.
//
// The port must be a decimal integer in 1..65535. An empty host defaults to
// 127.0.0.1; "localhost" is normalized the same way, mirroring
// internal/ssh/server.go:730 (the two transports must agree on what a target
// means). The host itself is intentionally NOT validated here — the SSH
// forward path accepts any host and relies on the port whitelist plus the
// server's own network position, so adding a host allowlist would be a
// behavior change, not a parity fix.
func ParseTarget(host, port string) (Target, error) {
	if port == "" {
		return Target{}, fmt.Errorf("%w: missing", ErrInvalidPort)
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return Target{}, fmt.Errorf("%w: %q is not a number", ErrInvalidPort, port)
	}
	if p < 1 || p > 65535 {
		return Target{}, fmt.Errorf("%w: %d out of range 1-65535", ErrInvalidPort, p)
	}
	if host == "" || host == "localhost" {
		host = DefaultTargetHost
	}
	return Target{Host: host, Port: p}, nil
}
