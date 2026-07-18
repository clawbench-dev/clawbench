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
	protocol   string // "http" or "https"
}

// NewReverseProxy creates a new HTTP reverse proxy.
// listenHost is typically "127.0.0.1", listenPort 0 means auto-assign.
// targetAddr is "host:port" of the backend to forward to.
// protocol is "http" or "https" (for the connection to the backend).
func NewReverseProxy(listenHost string, listenPort int, targetAddr string, protocol string) (*ReverseProxy, error) {
	if protocol == "" {
		protocol = "http"
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
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		return h
	}
	return hostPort
}
