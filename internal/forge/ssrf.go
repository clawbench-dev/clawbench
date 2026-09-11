package forge

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// ErrUnsafeHost is returned when a host resolves to an address that must not be
// contacted with credentials (loopback, private, link-local, metadata, etc.).
var ErrUnsafeHost = errors.New("unsafe forge host")

// HostResolver resolves a hostname to its addresses. It exists so callers (and
// tests) can inject a resolver; production uses net.LookupIP.
type HostResolver func(host string) ([]net.IP, error)

// CheckHostSafety rejects hosts that must never receive forge credentials.
//
// It blocks, by literal address or by DNS resolution:
//   - loopback (127.0.0.0/8, ::1) and the "localhost" name
//   - private ranges (10/8, 172.16/12, 192.168/16) and IPv6 unique-local (fc00::/7)
//   - link-local (169.254/16, fe80::/10) — includes the cloud metadata endpoint
//     ​169.254.169.254
//   - unspecified (0.0.0.0, ::)
//   - IPv4-mapped IPv6 forms of the above
//
// github.com and gitlab.com are always allowed. The port, if present, is
// stripped before the check.
//
// A nil resolver uses net.LookupIP. Resolving here is a best-effort guard: a
// TOCTOU DNS-rebinding attack is still possible, so callers should also pin the
// resolved host per binding (see the design doc, SSRF hardening).
func CheckHostSafety(host string, resolve HostResolver) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrUnsafeHost)
	}
	if strings.ContainsAny(host, " \t\n") {
		return fmt.Errorf("%w: malformed host %q", ErrUnsafeHost, host)
	}

	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	} else if strings.Count(host, ":") == 1 {
		// host:port where port is not numeric-friendly to SplitHostPort
		if i := strings.LastIndex(host, ":"); i >= 0 {
			hostname = host[:i]
		}
	}
	hostname = strings.Trim(hostname, "[]")
	hostname = strings.ToLower(hostname)

	if hostname == GitHubHost || hostname == GitLabHost {
		return nil
	}
	if hostname == "localhost" {
		return fmt.Errorf("%w: localhost", ErrUnsafeHost)
	}

	if ip := net.ParseIP(hostname); ip != nil {
		return checkIP(ip)
	}

	// Hostname: resolve and verify every address. A name resolving to any
	// blocked address is rejected outright.
	if resolve == nil {
		resolve = net.LookupIP
	}
	ips, err := resolve(hostname)
	if err != nil {
		return fmt.Errorf("%w: cannot resolve %q: %v", ErrUnsafeHost, hostname, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("%w: %q resolved to no addresses", ErrUnsafeHost, hostname)
	}
	for _, ip := range ips {
		if err := checkIP(ip); err != nil {
			return err
		}
	}
	return nil
}

func checkIP(ip net.IP) error {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsPrivate() {
		return fmt.Errorf("%w: address %s is not routable/public", ErrUnsafeHost, ip)
	}
	return nil
}
