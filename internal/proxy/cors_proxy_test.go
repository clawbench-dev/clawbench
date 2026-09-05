package proxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestIsPrivateIPv4(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		// Loopback
		{"127.0.0.1", true},
		{"127.0.0.100", true},
		// Private A
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		// Private B
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.15.255.255", false},
		{"172.32.0.1", false},
		// Private C
		{"192.168.0.1", true},
		{"192.168.255.255", true},
		// Link-local
		{"169.254.0.1", true},
		{"169.254.169.254", true},
		// Unspecified
		{"0.0.0.0", true},
		// Public
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"203.0.113.1", false},
		{"104.16.0.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip).To4()
			if got := isPrivateIPv4(ip); got != tt.want {
				t.Errorf("isPrivateIPv4(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		// IPv4 loopback
		{"127.0.0.1", true},
		// IPv6 loopback
		{"::1", true},
		// IPv4 unspecified
		{"0.0.0.0", true},
		// IPv6 unspecified
		{"::", true},
		// IPv4 link-local
		{"169.254.1.1", true},
		// IPv6 link-local
		{"fe80::1", true},
		// IPv4 private
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		// IPv6 private (fc00::/7)
		{"fc00::1", true},
		{"fd00::1", true},
		// Public IPv4
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		// Public IPv6
		{"2001:4860:4860::8888", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %s", tt.ip)
			}
			if got := isPrivateIP(ip); got != tt.want {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestIsHopByHop(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Connection", true},
		{"connection", true},
		{"Keep-Alive", true},
		{"keep-alive", true},
		{"Transfer-Encoding", true},
		{"Content-Type", false},
		{"Authorization", false},
		{"Accept", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHopByHop(tt.name); got != tt.want {
				t.Errorf("isHopByHop(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestCheckSSRF(t *testing.T) {
	tests := []struct {
		host    string
		wantErr bool
	}{
		// These use raw IPs where possible to avoid DNS resolution issues in CI
		{"8.8.8.8:443", false},       // Public IP with port
		{"8.8.8.8", false},           // Public IP without port
		{"127.0.0.1:8080", true},     // Loopback
		{"127.0.0.1", true},          // Loopback without port
		{"10.0.0.1:80", true},        // Private A
		{"192.168.1.1:443", true},    // Private C
		{"172.16.0.1:80", true},      // Private B
		{"169.254.1.1:80", true},     // Link-local
		{"0.0.0.0:80", true},         // Unspecified
		{"::ffff:192.168.1.1", true}, // IPv4-mapped private
		{"::ffff:8.8.8.8", false},    // IPv4-mapped public
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			err := checkSSRF(tt.host)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkSSRF(%q) error = %v, wantErr %v", tt.host, err, tt.wantErr)
			}
		})
	}
}

func TestCheckSSRF_HostnameResolution(t *testing.T) {
	orig := AllowLocalProxy
	AllowLocalProxy = false
	defer func() { AllowLocalProxy = orig }()

	origLookup := netLookupIP
	defer func() { netLookupIP = origLookup }()

	netLookupIP = func(host string) ([]net.IP, error) {
		if host == "dns.google" {
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host}
	}

	// Public hostname should resolve and pass
	err := checkSSRF("dns.google:443")
	if err != nil {
		t.Errorf("checkSSRF(public hostname) should pass, got error: %v", err)
	}

	// Unresolvable hostname should return DNS error
	err = checkSSRF("this-host-definitely-does-not-exist.invalid:80")
	if err == nil {
		t.Error("checkSSRF(unresolvable hostname) should return error")
	}
}

func TestServeCORSProxy_MissingURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy", http.NoBody)
	w := httptest.NewRecorder()
	ServeCORSProxy(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "missing required query parameter") {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestServeCORSProxy_InvalidScheme(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy?url=ftp://example.com/api", http.NoBody)
	w := httptest.NewRecorder()
	ServeCORSProxy(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "must use http or https") {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestServeCORSProxy_OptionsPreflight(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/api/openapi-proxy?url=http://example.com/api", http.NoBody)
	req.Header.Set("Origin", "http://localhost:20001")
	w := httptest.NewRecorder()
	ServeCORSProxy(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:20001" {
		t.Errorf("expected CORS origin header, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("missing Access-Control-Allow-Methods")
	}
	if w.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Error("missing Access-Control-Allow-Headers")
	}
}

func TestSetCORSHeaders(t *testing.T) {
	tests := []struct {
		name           string
		origin         string
		expectedOrigin string
	}{
		{"with origin", "http://localhost:20001", "http://localhost:20001"},
		{"without origin", "", "*"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			setCORSHeaders(w, tt.origin)
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != tt.expectedOrigin {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, tt.expectedOrigin)
			}
			if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
				t.Errorf("Access-Control-Allow-Credentials = %q, want %q", got, "true")
			}
			if got := w.Header().Get("Access-Control-Max-Age"); got != "86400" {
				t.Errorf("Access-Control-Max-Age = %q, want %q", got, "86400")
			}
		})
	}
}

// TestServeCORSProxy_AllowLocal tests that when AllowLocalProxy is true,
// requests to loopback addresses are allowed (default for dev API testing).
func TestServeCORSProxy_AllowLocal(t *testing.T) {
	// Create a test upstream server on loopback
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "from-upstream")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("upstream-ok"))
	}))
	defer upstream.Close()

	// Ensure AllowLocalProxy is true (default)
	orig := AllowLocalProxy
	AllowLocalProxy = true
	defer func() { AllowLocalProxy = orig }()

	targetURL := upstream.URL + "/test?query=1"
	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy?url="+url.QueryEscape(targetURL), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Origin", "http://localhost:20001")
	w := httptest.NewRecorder()
	ServeCORSProxy(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "upstream-ok" {
		t.Errorf("expected body 'upstream-ok', got %q", w.Body.String())
	}
	if w.Header().Get("X-Custom-Header") != "from-upstream" {
		t.Errorf("expected X-Custom-Header 'from-upstream', got %q", w.Header().Get("X-Custom-Header"))
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:20001" {
		t.Errorf("expected CORS origin, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

// TestServeCORSProxy_SSRFBlock tests that when AllowLocalProxy is false,
// requests to private IPs are blocked.
func TestServeCORSProxy_SSRFBlock(t *testing.T) {
	orig := AllowLocalProxy
	AllowLocalProxy = false
	defer func() { AllowLocalProxy = orig }()

	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy?url=http://127.0.0.1:8080/api", http.NoBody)
	w := httptest.NewRecorder()
	ServeCORSProxy(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

// TestServeCORSProxy_PostBody tests that POST request body and content-type
// are forwarded correctly.
func TestServeCORSProxy_PostBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"received":"` + string(body) + `","ct":"` + ct + `"}`))
	}))
	defer upstream.Close()

	orig := AllowLocalProxy
	AllowLocalProxy = true
	defer func() { AllowLocalProxy = orig }()

	targetURL := upstream.URL + "/submit"
	req := httptest.NewRequest(http.MethodPost, "/api/openapi-proxy?url="+url.QueryEscape(targetURL), strings.NewReader(`{"name":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ServeCORSProxy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `{"name":"test"}`) {
		t.Errorf("upstream should have received the body, got: %s", body)
	}
	if !strings.Contains(body, "application/json") {
		t.Errorf("upstream should have received content-type, got: %s", body)
	}
}

// TestServeCORSProxy_InvalidTargetURL tests various invalid target URLs.
func TestServeCORSProxy_InvalidTargetURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expected    int
		expectedMsg string
	}{
		{"missing url param", "/api/openapi-proxy", http.StatusBadRequest, "missing required query parameter"},
		{"ftp scheme", "/api/openapi-proxy?url=ftp://example.com/api", http.StatusBadRequest, "must use http or https"},
		{"javascript scheme", "/api/openapi-proxy?url=javascript:alert(1)", http.StatusBadRequest, "must use http or https"},
		{"no scheme", "/api/openapi-proxy?url=example.com/api", http.StatusBadRequest, "must use http or https"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, http.NoBody)
			w := httptest.NewRecorder()
			ServeCORSProxy(w, req)
			if w.Code != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, w.Code)
			}
			if !strings.Contains(w.Body.String(), tt.expectedMsg) {
				t.Errorf("body %q should contain %q", w.Body.String(), tt.expectedMsg)
			}
		})
	}
}

func TestValidateTargetURL_InvalidParse(t *testing.T) {
	// url.Parse should fail for completely invalid URLs like "://"
	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy?url=://invalid", http.NoBody)
	w := httptest.NewRecorder()
	_, _, ok := validateTargetURL(w, req)
	if ok {
		t.Error("expected ok=false for unparseable URL")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "invalid target URL") {
		t.Errorf("body should contain 'invalid target URL', got %q", body)
	}
}

func TestServeCORSProxy_UpstreamFailure(t *testing.T) {
	orig := AllowLocalProxy
	AllowLocalProxy = true
	defer func() { AllowLocalProxy = orig }()

	// Target a host that will refuse connections
	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy?url=http://127.0.0.1:1/impossible-port", http.NoBody)
	w := httptest.NewRecorder()
	ServeCORSProxy(w, req)
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for upstream failure, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "upstream request failed") {
		t.Errorf("body should contain 'upstream request failed', got %q", body)
	}
}

func TestServeCORSProxy_NewRequestError(t *testing.T) {
	orig := AllowLocalProxy
	AllowLocalProxy = true
	defer func() { AllowLocalProxy = orig }()

	// A URL with a control character causes http.NewRequestWithContext to fail
	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy?url=http://example.com/api", http.NoBody)
	// Manually override the method to something invalid that passes httptest but fails NewRequestWithContext
	req.Method = "INVALID METHOD"
	w := httptest.NewRecorder()
	ServeCORSProxy(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for new request error, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "failed to create outgoing request") {
		t.Errorf("body should contain 'failed to create outgoing request', got %q", body)
	}
}

func TestValidateTargetURL(t *testing.T) {
	tests := []struct {
		name       string
		reqURL     string
		allowLocal bool
		wantOk     bool
		wantStatus int
		wantMsg    string
	}{
		{"missing url param", "/api/openapi-proxy", true, false, http.StatusBadRequest, "missing required query parameter"},
		{"ftp scheme", "/api/openapi-proxy?url=ftp://example.com/api", true, false, http.StatusBadRequest, "must use http or https"},
		{"javascript scheme", "/api/openapi-proxy?url=javascript:alert(1)", true, false, http.StatusBadRequest, "must use http or https"},
		{"no scheme", "/api/openapi-proxy?url=example.com/api", true, false, http.StatusBadRequest, "must use http or https"},
		{"valid http", "/api/openapi-proxy?url=http://example.com/api", true, true, 0, ""},
		{"valid https", "/api/openapi-proxy?url=https://example.com/api", true, true, 0, ""},
		{"ssrf blocked when local disabled", "/api/openapi-proxy?url=http://127.0.0.1:8080/api", false, false, http.StatusForbidden, "not accessible"},
		{"ssrf allowed when local enabled", "/api/openapi-proxy?url=http://127.0.0.1:8080/api", true, true, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := AllowLocalProxy
			AllowLocalProxy = tt.allowLocal
			defer func() { AllowLocalProxy = orig }()

			req := httptest.NewRequest(http.MethodGet, tt.reqURL, http.NoBody)
			w := httptest.NewRecorder()
			_, _, ok := validateTargetURL(w, req)

			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if !ok {
				if w.Code != tt.wantStatus {
					t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
				}
				if !strings.Contains(w.Body.String(), tt.wantMsg) {
					t.Errorf("body %q should contain %q", w.Body.String(), tt.wantMsg)
				}
			}
		})
	}
}

func TestIsPrivateIP_IPv4Mapped(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"::ffff:8.8.8.8", false},    // IPv4-mapped public address
		{"::ffff:192.168.1.1", true}, // IPv4-mapped private address
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %s", tt.ip)
			}
			if got := isPrivateIP(ip); got != tt.want {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestCORSProxyDialer_SplitHostPortError(t *testing.T) {
	tr := corsProxyClient.Transport.(*http.Transport)
	conn, err := tr.DialContext(context.Background(), "tcp", "no-port")
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("expected error dialing an address without a port")
	}
}

func TestCORSProxyDialer_SSRFBlock(t *testing.T) {
	orig := AllowLocalProxy
	AllowLocalProxy = false
	defer func() { AllowLocalProxy = orig }()

	// Use corsProxyClient directly against a private address to exercise the
	// DialContext SSRF re-check (DNS rebinding defense).
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:1/", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := corsProxyClient.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected error dialing a private IP with AllowLocalProxy=false")
	}
	if !strings.Contains(err.Error(), "blocked IP") {
		t.Errorf("expected blocked IP error, got %v", err)
	}
}

func TestCORSProxyDialer_LookupFailure(t *testing.T) {
	orig := AllowLocalProxy
	AllowLocalProxy = true
	defer func() { AllowLocalProxy = orig }()

	// A hostname that will not resolve exercises the LookupIP error path in DialContext.
	req, err := http.NewRequest(http.MethodGet, "http://nonexistent.invalid:1/", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	_, err = corsProxyClient.Do(req)
	if err == nil {
		t.Fatal("expected error dialing an unresolvable hostname")
	}
}

func TestServeCORSProxy_RedirectLoop(t *testing.T) {
	orig := AllowLocalProxy
	AllowLocalProxy = true
	defer func() { AllowLocalProxy = orig }()

	// Upstream that redirects forever; CheckRedirect should bail after 10 hops.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/loop")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()

	targetURL := upstream.URL + "/loop"
	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy?url="+url.QueryEscape(targetURL), http.NoBody)
	w := httptest.NewRecorder()
	ServeCORSProxy(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("expected 302 from redirect loop, got %d", w.Code)
	}
}

type failingWriter struct {
	http.ResponseWriter
}

func (f *failingWriter) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }

func TestServeCORSProxy_WriteFailure(t *testing.T) {
	orig := AllowLocalProxy
	AllowLocalProxy = true
	defer func() { AllowLocalProxy = orig }()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("some-body"))
	}))
	defer upstream.Close()

	targetURL := upstream.URL + "/data"
	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy?url="+url.QueryEscape(targetURL), http.NoBody)
	w := httptest.NewRecorder()
	ServeCORSProxy(&failingWriter{ResponseWriter: w}, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCopyHeaders(t *testing.T) {
	t.Run("copies regular headers", func(t *testing.T) {
		dst := http.Header{}
		src := http.Header{}
		src.Set("Content-Type", "application/json")
		src.Set("Authorization", "Bearer test")
		copyHeaders(dst, src)
		if dst.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type, got %q", dst.Get("Content-Type"))
		}
		if dst.Get("Authorization") != "Bearer test" {
			t.Errorf("expected Authorization, got %q", dst.Get("Authorization"))
		}
	})

	t.Run("skips hop-by-hop headers", func(t *testing.T) {
		dst := http.Header{}
		src := http.Header{}
		src.Set("Content-Type", "text/plain")
		src.Set("Connection", "keep-alive")
		src.Set("Keep-Alive", "timeout=5")
		src.Set("Transfer-Encoding", "chunked")
		copyHeaders(dst, src)
		if dst.Get("Content-Type") != "text/plain" {
			t.Errorf("expected Content-Type, got %q", dst.Get("Content-Type"))
		}
		if dst.Get("Connection") != "" {
			t.Error("Connection should be skipped")
		}
		if dst.Get("Keep-Alive") != "" {
			t.Error("Keep-Alive should be skipped")
		}
		if dst.Get("Transfer-Encoding") != "" {
			t.Error("Transfer-Encoding should be skipped")
		}
	})

	t.Run("skips Host header", func(t *testing.T) {
		dst := http.Header{}
		src := http.Header{}
		src.Set("Host", "example.com")
		src.Set("Content-Type", "text/plain")
		copyHeaders(dst, src)
		if dst.Get("Host") != "" {
			t.Error("Host should be skipped")
		}
		if dst.Get("Content-Type") != "text/plain" {
			t.Errorf("expected Content-Type, got %q", dst.Get("Content-Type"))
		}
	})

	t.Run("preserves multi-value headers", func(t *testing.T) {
		dst := http.Header{}
		src := http.Header{}
		src.Add("Accept", "text/html")
		src.Add("Accept", "application/json")
		copyHeaders(dst, src)
		if len(dst.Values("Accept")) != 2 {
			t.Errorf("expected 2 Accept values, got %d", len(dst.Values("Accept")))
		}
	})

	t.Run("empty source produces empty destination", func(t *testing.T) {
		dst := http.Header{}
		src := http.Header{}
		copyHeaders(dst, src)
		if len(dst) != 0 {
			t.Errorf("expected empty dst, got %d keys", len(dst))
		}
	})
}
