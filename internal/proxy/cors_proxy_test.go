package proxy

import (
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
		{"8.8.8.8:443", false},         // Public IP with port
		{"8.8.8.8", false},             // Public IP without port
		{"127.0.0.1:8080", true},       // Loopback
		{"127.0.0.1", true},            // Loopback without port
		{"10.0.0.1:80", true},          // Private A
		{"192.168.1.1:443", true},      // Private C
		{"172.16.0.1:80", true},        // Private B
		{"169.254.1.1:80", true},       // Link-local
		{"0.0.0.0:80", true},           // Unspecified
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

func TestServeCORSProxy_MissingURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy", nil)
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
	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy?url=ftp://example.com/api", nil)
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
	req := httptest.NewRequest(http.MethodOptions, "/api/openapi-proxy?url=http://example.com/api", nil)
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
	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy?url="+url.QueryEscape(targetURL), nil)
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

	req := httptest.NewRequest(http.MethodGet, "/api/openapi-proxy?url=http://127.0.0.1:8080/api", nil)
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
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
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
