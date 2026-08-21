package proxy

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// ReverseProxy is an HTTP reverse proxy that listens on a local address and
// forwards requests to a target host:port, rewriting the Host header to match
// the original target. This solves the problem where SSH tunnel (TCP-level)
// forwarding preserves the browser's "Host: localhost:port" header, which
// breaks virtual-host backends that expect their own hostname.
type ReverseProxy struct {
	listener   net.Listener
	server     *http.Server
	proxy      *httputil.ReverseProxy
	transport  *http.Transport
	targetAddr string // host:port of the backend
	targetURL  *url.URL
	protocol   string // schemeHTTP or schemeHTTPS
}

// NewReverseProxy creates a new HTTP reverse proxy.
// listenHost is typically "127.0.0.1", listenPort 0 means auto-assign.
// targetAddr is "host:port" of the backend to forward to.
// protocol is "http" or "https" (for the connection to the backend).
func NewReverseProxy(listenHost string, listenPort int, targetAddr string, protocol string) (*ReverseProxy, error) {
	if protocol == "" {
		protocol = schemeHTTP
	}

	// Build the target URL for httputil.ReverseProxy
	scheme := protocol
	host := targetAddr
	// Strip any existing scheme from targetAddr
	if strings.Contains(targetAddr, "://") {
		parsed, err := url.Parse(targetAddr)
		if err == nil {
			scheme = parsed.Scheme
			host = parsed.Host
		}
	}

	targetURL, err := url.Parse(scheme + "://" + host)
	if err != nil {
		return nil, fmt.Errorf("invalid target address %s: %w", targetAddr, err)
	}

	rp := &ReverseProxy{
		targetAddr: host,
		targetURL:  targetURL,
		protocol:   protocol,
	}

	// Create transport with InsecureSkipVerify for self-signed certs on LAN targets
	rp.transport = &http.Transport{
		DialContext:     (&net.Dialer{}).DialContext,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	// Create the httputil.ReverseProxy
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = rp.transport

	// Customize the Director to rewrite Host header
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// Set Host to the target address, omitting default ports per HTTP spec.
		// e.g. "192.168.100.1:80" with scheme "http" → "192.168.100.1"
		// but "192.168.100.1:8080" → "192.168.100.1:8080"
		req.Host = stripDefaultPort(host, scheme)
		// Ensure the scheme is correct
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
	}

	// Rewrite Location headers in 3xx responses from the target host back to
	// the proxy's listen address. Without this, backends return
	// "Location: http://192.168.100.1:8080/path" which clients cannot reach
	// when accessing through the reverse proxy on localhost.
	proxy.ModifyResponse = func(resp *http.Response) error {
		loc := resp.Header.Get("Location")
		if loc == "" || resp.StatusCode < 300 || resp.StatusCode >= 400 {
			return nil
		}
		rewritten := rewriteLocation(loc, targetURL, rp.listener.Addr())
		if rewritten != loc {
			slog.Debug("reverse proxy rewriting Location", slog.String("from", loc), slog.String("to", rewritten))
			resp.Header.Set("Location", rewritten)
		}
		return nil
	}

	rp.proxy = proxy
	rp.server = &http.Server{
		Handler: proxy,
	}

	// Start listening
	addr := fmt.Sprintf("%s:%d", listenHost, listenPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	rp.listener = listener

	return rp, nil
}

// Serve starts accepting connections. Blocks until the listener is closed.
func (rp *ReverseProxy) Serve() {
	if err := rp.server.Serve(rp.listener); err != nil && err != http.ErrServerClosed {
		slog.Debug("reverse proxy server stopped", slog.String("err", err.Error()))
	}
}

// Addr returns the listener address (e.g., "127.0.0.1:54321").
// Returns empty string if the proxy is not listening.
func (rp *ReverseProxy) Addr() string {
	if rp.listener == nil {
		return ""
	}
	return rp.listener.Addr().String()
}

// Port returns the listening port number.
// Returns 0 if the proxy is not listening.
func (rp *ReverseProxy) Port() int {
	if rp.listener == nil {
		return 0
	}
	_, portStr, _ := net.SplitHostPort(rp.listener.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	return port
}

// Close shuts down the reverse proxy.
func (rp *ReverseProxy) Close() {
	if rp.server != nil {
		_ = rp.server.Close()
	}
}

// stripDefaultPort removes the port from a host:port string if it's the default
// port for the given scheme (80 for http, 443 for https).
// e.g. ("192.168.100.1:80", "http") → "192.168.100.1"
//
//	("192.168.100.1:8080", "http") → "192.168.100.1:8080"
func stripDefaultPort(hostPort, scheme string) string {
	h, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return hostPort // no port, return as-is
	}
	if port == defaultPort(scheme) {
		return h
	}
	return hostPort
}

// rewriteLocation rewrites a redirect Location header from the target host to
// the proxy's listen address. It handles both absolute URLs
// (e.g. "http://192.168.100.1:8080/path") and same-host variants with/without
// default ports. Relative Locations and external URLs are left unchanged
// (no open-redirect risk).
func rewriteLocation(location string, targetURL *url.URL, listenAddr net.Addr) string {
	if location == "" {
		return location
	}
	// Relative redirect (e.g. "/path" or "path") — no rewriting needed
	if !strings.HasPrefix(location, "http://") && !strings.HasPrefix(location, "https://") {
		return location
	}

	parsed, err := url.Parse(location)
	if err != nil {
		return location
	}

	// Check if Location points to the target host.
	// Try matching with both the target's scheme and the Location's own scheme
	// to handle cross-scheme redirects (e.g. target is http://host:80 but
	// Location is https://host:443/). If neither scheme produces a port match
	// but the bare hostnames are the same, still rewrite — the target host
	// is the same machine, just a different scheme/port.
	targetHost := targetURL.Host // e.g. "192.168.100.1:8080" or "192.168.100.1"
	locHost := parsed.Host
	if !hostMatches(locHost, targetHost, targetURL.Scheme) &&
		!hostMatches(locHost, targetHost, parsed.Scheme) &&
		!sameBareHost(locHost, targetHost) {
		return location // Not pointing to our target — leave as-is
	}

	// Replace host with proxy listen address
	listenHost, listenPort, _ := net.SplitHostPort(listenAddr.String())
	parsed.Scheme = "http" // Proxy always serves HTTP on localhost
	parsed.Host = net.JoinHostPort(listenHost, listenPort)
	return parsed.String()
}

// hostMatches checks whether a Location header's host matches the target host,
// accounting for default port differences (e.g. "192.168.100.1" matches
// "192.168.100.1:80" for HTTP, and vice versa).
func hostMatches(locHost, targetHost, targetScheme string) bool {
	if locHost == targetHost {
		return true
	}
	// Compare bare hostnames (IP addresses) — if they differ, no match
	locBare, locPort, locErr := net.SplitHostPort(locHost)
	if locErr != nil {
		locBare = locHost // no port present
		locPort = defaultPort(targetScheme)
	}
	targetBare, targetPort, targetErr := net.SplitHostPort(targetHost)
	if targetErr != nil {
		targetBare = targetHost
		targetPort = defaultPort(targetScheme)
	}
	if locBare != targetBare {
		return false
	}
	// Same bare host — check if ports are equivalent (default ports match bare host)
	locPortIsDefault := locPort == defaultPort(targetScheme)
	targetPortIsDefault := targetPort == defaultPort(targetScheme)
	if locPortIsDefault && targetPortIsDefault {
		return true
	}
	return locPort == targetPort
}

// defaultPort returns the default port for a given scheme.
func defaultPort(scheme string) string {
	switch scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

// sameBareHost checks whether two host:port strings share the same bare hostname,
// ignoring any port differences. This is used for cross-scheme redirect matching
// where the same device may redirect from HTTP to HTTPS on a different port.
func sameBareHost(a, b string) bool {
	aBare, _, aErr := net.SplitHostPort(a)
	if aErr != nil {
		aBare = a
	}
	bBare, _, bErr := net.SplitHostPort(b)
	if bErr != nil {
		bBare = b
	}
	return aBare == bBare
}
