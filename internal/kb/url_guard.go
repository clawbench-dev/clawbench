package kb

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"time"
)

// ErrPrivateAddress means the URL points at this machine, its LAN, or a
// cloud-metadata style link-local address. Such a page can be fetched from
// inside the server but never from a browser on the internet, and its content
// would be indexed and auto-injected into AI prompts.
var ErrPrivateAddress = errors.New("this address is on a private network and cannot be added")

// ReasonPrivateAddress is the FileError reason for ErrPrivateAddress.
const ReasonPrivateAddress = "private_address"

// privatePrefixes are the ranges a URL fetch may not reach: loopback, the
// RFC1918 blocks, link-local (169.254.169.254 and friends), CGNAT, "this
// network", multicast and reserved space, plus their IPv6 equivalents.
var privatePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"), // IPv4-mapped/NAT64 space
	netip.MustParsePrefix("fc00::/7"),     // unique local
	netip.MustParsePrefix("fe80::/10"),    // link local
	netip.MustParsePrefix("ff00::/8"),     // multicast
}

// isPrivateIP reports whether addr must not be fetched over the network.
func isPrivateIP(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	// A 4-in-6 address must be judged by its IPv4 value, otherwise
	// ::ffff:127.0.0.1 would read as an ordinary global IPv6 address.
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() {
		return true
	}
	for _, p := range privatePrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// isPrivateHost checks a URL's host ("example.com", "127.0.0.1:8080",
// "[::1]:80"). Names other than localhost are allowed here because they can
// only be judged after resolution — dialPublicOnly does that for the
// addresses a connection would actually use.
func isPrivateHost(host string) bool {
	if host == "" {
		return true
	}
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.TrimSuffix(strings.TrimPrefix(name, "["), "]")
	trimmed := strings.TrimSuffix(strings.ToLower(name), ".")
	if trimmed == "" || trimmed == "localhost" || strings.HasSuffix(trimmed, ".localhost") ||
		strings.HasSuffix(trimmed, ".local") || strings.HasSuffix(trimmed, ".internal") {
		return true
	}
	if addr, err := netip.ParseAddr(trimmed); err == nil {
		return isPrivateIP(addr)
	}
	return false
}

// dialPublicOnly resolves, filters and dials itself instead of delegating to
// net.Dialer's own resolver: that is the only point where the address actually
// used is known, so a public hostname resolved to 10.x (or a rebinding answer
// served after the URL-level check) is still refused.
func dialPublicOnly(timeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if isPrivateHost(host) {
			return nil, &FileError{Reason: ReasonPrivateAddress, Detail: host, Err: ErrPrivateAddress}
		}
		addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		dialer := &net.Dialer{Timeout: timeout}
		var lastErr error
		for _, a := range addrs {
			if isPrivateIP(a) {
				lastErr = &FileError{Reason: ReasonPrivateAddress, Detail: a.String(), Err: ErrPrivateAddress}
				continue
			}
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(a.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr == nil {
			lastErr = &net.DNSError{Err: "no usable address", Name: host}
		}
		return nil, lastErr
	}
}
