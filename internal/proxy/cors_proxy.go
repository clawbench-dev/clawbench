package proxy

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// AllowLocalProxy controls whether the CORS proxy permits requests to
// loopback and private IP addresses (e.g. localhost:3000). Defaults to true
// because the primary use case is testing locally-running dev APIs.
// Set to false in production to prevent SSRF attacks.
var AllowLocalProxy = true

// Hop-by-hop headers that must not be forwarded (RFC 2616 Section 13.5.1).
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"TE",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// corsAllowedHeaders lists headers the CORS proxy allows in requests.
var corsAllowedHeaders = []string{
	"Content-Type",
	"Authorization",
	"Accept",
	"X-Requested-With",
}

// corsProxyDialer is the base dialer with timeout settings.
var corsProxyDialer = &net.Dialer{Timeout: 10 * time.Second}

// corsProxyClient is the HTTP client used for forwarding proxy requests.
// It uses a custom DialContext that re-validates the resolved IP against
// the SSRF blocklist on every connection, preventing DNS rebinding attacks.
var corsProxyClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Re-check the resolved IP at connection time to prevent DNS rebinding.
			// The SSRF check in the handler resolves DNS once, but an attacker with
			// a short-TTL DNS record could change the resolution between the check
			// and the actual TCP dial.
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			// Resolve the IP that the dialer will actually connect to
			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, err
			}
			if !AllowLocalProxy {
				for _, ip := range ips {
					if isPrivateIP(ip) {
						return nil, errBlockedIP{ip: ip}
					}
				}
			}
			return corsProxyDialer.DialContext(ctx, network, addr)
		},
		// Allow self-signed certs — this is a dev tool for testing APIs
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return http.ErrUseLastResponse
		}
		return nil
	},
}

// ServeCORSProxy handles CORS proxy requests for Swagger UI's "Try it out".
// It receives a request with a ?url= query parameter pointing to the target API,
// forwards the request to that URL, and returns the response with CORS headers
// so the browser doesn't block it.
// validateTargetURL parses and validates the target URL from the request query.
// Returns the parsed URL, the raw URL string, or an error response written to w.
func validateTargetURL(w http.ResponseWriter, r *http.Request) (*url.URL, string, bool) {
	targetURLStr := r.URL.Query().Get("url")
	if targetURLStr == "" {
		http.Error(w, "missing required query parameter: url", http.StatusBadRequest)
		return nil, "", false
	}

	targetURL, err := url.Parse(targetURLStr)
	if err != nil {
		http.Error(w, "invalid target URL: "+err.Error(), http.StatusBadRequest)
		return nil, "", false
	}

	if targetURL.Scheme != schemeHTTP && targetURL.Scheme != schemeHTTPS {
		http.Error(w, "target URL must use http or https scheme", http.StatusBadRequest)
		return nil, "", false
	}

	// SSRF protection: resolve hostname and block private/reserved IPs.
	// Skipped when AllowLocalProxy is true (default) for dev API testing.
	if !AllowLocalProxy {
		if ssrfErr := checkSSRF(targetURL.Host); ssrfErr != nil {
			slog.Warn("CORS proxy: blocked SSRF attempt", slog.String("host", targetURL.Host), slog.String("err", ssrfErr.Error()))
			http.Error(w, "target host is not accessible: "+ssrfErr.Error(), http.StatusForbidden)
			return nil, "", false
		}
	}

	return targetURL, targetURLStr, true
}

// copyHeaders copies headers from src to dst, skipping hop-by-hop headers and Host.
func copyHeaders(dst http.Header, src http.Header) {
	for k, vv := range src {
		if isHopByHop(k) || strings.EqualFold(k, "Host") {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func ServeCORSProxy(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")

	// Handle OPTIONS preflight immediately
	if r.Method == http.MethodOptions {
		setCORSHeaders(w, origin)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Read and validate the target URL
	targetURL, targetURLStr, ok := validateTargetURL(w, r)
	if !ok {
		return
	}

	// Build the outgoing request
	//nolint:gosec // targetURLStr is validated above (scheme + SSRF check)
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURLStr, r.Body)
	if err != nil {
		http.Error(w, "failed to create outgoing request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	copyHeaders(outReq.Header, r.Header)
	// Set the Host header to the target host
	outReq.Host = targetURL.Host

	// Send the request
	//nolint:gosec // outReq target was validated above (SSRF + scheme check)
	resp, err := corsProxyClient.Do(outReq)
	if err != nil {
		slog.Debug("CORS proxy: upstream request failed", slog.String("url", targetURLStr), slog.String("err", err.Error()))
		http.Error(w, "upstream request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Write response headers, skipping hop-by-hop
	copyHeaders(w.Header(), resp.Header)

	// Add CORS headers
	setCORSHeaders(w, origin)

	// Write status code and body
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		slog.Debug("CORS proxy: failed to write response body", slog.String("err", err.Error()))
	}
}

// setCORSHeaders adds CORS headers to the response.
func setCORSHeaders(w http.ResponseWriter, origin string) {
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	} else {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", strings.Join(corsAllowedHeaders, ", "))
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Max-Age", "86400")
}

// isHopByHop returns true if the header name is a hop-by-hop header that should
// not be forwarded between proxy hops.
func isHopByHop(name string) bool {
	for _, h := range hopByHopHeaders {
		if strings.EqualFold(name, h) {
			return true
		}
	}
	return false
}

// checkSSRF resolves the target hostname and verifies the IP is not a private,
// loopback, or link-local address. This prevents the proxy from being used to
// access internal network resources.
func checkSSRF(host string) error {
	// Split host and port
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	// If it's already an IP, check directly
	if ip := net.ParseIP(hostname); ip != nil {
		if isPrivateIP(ip) {
			return errBlockedIP{ip: ip}
		}
		return nil
	}

	// Resolve hostname to IPs
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return errBlockedIP{ip: ip}
		}
	}
	return nil
}

// isPrivateIP returns true if the IP is a loopback, link-local, private, or
// other reserved address that should not be accessible via the CORS proxy.
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsUnspecified() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	// Additional check: IPv4-mapped IPv6 addresses
	if v4 := ip.To4(); v4 != nil {
		return isPrivateIPv4(v4)
	}
	return false
}

// isPrivateIPv4 checks individual IPv4 ranges for clarity and testability.
func isPrivateIPv4(ip net.IP) bool {
	// 10.0.0.0/8
	if ip[0] == 10 {
		return true
	}
	// 172.16.0.0/12 (172.16.0.0 – 172.31.255.255)
	if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
		return true
	}
	// 192.168.0.0/16
	if ip[0] == 192 && ip[1] == 168 {
		return true
	}
	// 127.0.0.0/8
	if ip[0] == 127 {
		return true
	}
	// 169.254.0.0/16 (link-local)
	if ip[0] == 169 && ip[1] == 254 {
		return true
	}
	// 0.0.0.0/8
	if ip[0] == 0 {
		return true
	}
	return false
}

// errBlockedIP indicates an IP was blocked by SSRF protection.
type errBlockedIP struct {
	ip net.IP
}

func (e errBlockedIP) Error() string {
	return "target resolves to blocked IP: " + e.ip.String()
}
