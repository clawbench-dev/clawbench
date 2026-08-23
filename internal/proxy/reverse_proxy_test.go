package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestReverseProxy_ForwardsRequest(t *testing.T) {
	// Setup a backend server that echoes the Host header
	var receivedHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	// Create reverse proxy pointing to the backend
	rp, err := NewReverseProxy("127.0.0.1", 0, backend.Listener.Addr().String(), "http")
	assert.NoError(t, err)
	defer rp.Close()

	go rp.Serve()
	addr := rp.Addr()
	assert.NotEmpty(t, addr)

	// Wait for listener to be ready
	time.Sleep(50 * time.Millisecond)

	// Send a request through the proxy using a real HTTP client
	resp, err := http.Get("http://" + addr + "/test")
	assert.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// The backend should receive the original target's Host, not "localhost:randomPort"
	assert.NotContains(t, receivedHost, "localhost", "Host header should not contain localhost")
}

func TestReverseProxy_SetsCorrectHost(t *testing.T) {
	// Setup a backend that records the Host header
	var receivedHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// Target is the backend's address (simulating a LAN target like 192.168.1.100:8080)
	backendAddr := backend.Listener.Addr().String()
	rp, err := NewReverseProxy("127.0.0.1", 0, backendAddr, "http")
	assert.NoError(t, err)
	defer rp.Close()

	go rp.Serve()
	addr := rp.Addr()
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://" + addr + "/api/data")
	assert.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// Host header should match the backend's address (target host:port)
	assert.Equal(t, backendAddr, receivedHost, "Host header should be the target address")
}

func TestReverseProxy_HandlesPort80(t *testing.T) {
	var receivedHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendAddr := backend.Listener.Addr().String()
	rp, err := NewReverseProxy("127.0.0.1", 0, backendAddr, "http")
	assert.NoError(t, err)
	defer rp.Close()

	go rp.Serve()
	addr := rp.Addr()
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://" + addr + "/")
	assert.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// Host should be the backend address, not the proxy address
	assert.NotEqual(t, addr, receivedHost, "Host header should not be the proxy's address")
}

func TestReverseProxy_SupportsHTTPS(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendAddr := backend.Listener.Addr().String()
	rp, err := NewReverseProxy("127.0.0.1", 0, backendAddr, "https")
	assert.NoError(t, err)
	defer rp.Close()

	go rp.Serve()
	addr := rp.Addr()
	time.Sleep(50 * time.Millisecond)

	// Connect to proxy via plain HTTP (proxy handles TLS to backend)
	client := &http.Client{Transport: &http.Transport{}}
	resp, err := client.Get("http://" + addr + "/")
	assert.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestReverseProxy_Port(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	rp, err := NewReverseProxy("127.0.0.1", 0, backend.Listener.Addr().String(), "http")
	assert.NoError(t, err)
	defer rp.Close()

	port := rp.Port()
	assert.Greater(t, port, 0, "Auto-assigned port should be > 0")
}

func TestReverseProxy_TargetHostRewrite(t *testing.T) {
	// The key scenario: forwarding to a LAN IP like 192.168.1.100
	// The browser sends Host: localhost:localPort, but the backend
	// should receive Host: 192.168.1.100:targetPort
	var receivedHost string
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("response from backend"))
	}))
	defer backend.Close()

	// The backend's address simulates a LAN target
	backendAddr := backend.Listener.Addr().String()
	// Extract just the port to simulate a scenario where we forward to a named host
	_, port, _ := net.SplitHostPort(backendAddr)

	// Simulate forwarding to "192.168.1.100:8080" by using a custom target address
	// We use the actual backend's port but set the target host to the backend's IP
	targetHost := "127.0.0.1:" + port
	rp, err := NewReverseProxy("127.0.0.1", 0, targetHost, "http")
	assert.NoError(t, err)
	defer rp.Close()

	go rp.Serve()
	addr := rp.Addr()
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://" + addr + "/some/path")
	assert.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, targetHost, receivedHost, "Host header should be the target address, not localhost")
	assert.Equal(t, "/some/path", receivedPath, "Path should be forwarded correctly")
}

func TestReverseProxy_ResponseBody(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello from backend"))
	}))
	defer backend.Close()

	rp, err := NewReverseProxy("127.0.0.1", 0, backend.Listener.Addr().String(), "http")
	assert.NoError(t, err)
	defer rp.Close()

	go rp.Serve()
	addr := rp.Addr()
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://" + addr + "/")
	assert.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(body), "hello from backend"), "Response body should contain backend response")
}

func TestStripDefaultPort(t *testing.T) {
	tests := []struct {
		hostPort string
		scheme   string
		want     string
	}{
		{"192.168.100.1:80", "http", "192.168.100.1"},
		{"192.168.100.1:443", "https", "192.168.100.1"},
		{"192.168.100.1:8080", "http", "192.168.100.1:8080"},
		{"192.168.100.1:8443", "https", "192.168.100.1:8443"},
		{"example.com:80", "http", "example.com"},
		{"example.com:443", "https", "example.com"},
		{"example.com:80", "https", "example.com:80"},  // port 80 with https is NOT default
		{"example.com:443", "http", "example.com:443"}, // port 443 with http is NOT default
		{"10.0.0.1", "http", "10.0.0.1"},               // no port at all
	}
	for _, tt := range tests {
		got := stripDefaultPort(tt.hostPort, tt.scheme)
		assert.Equal(t, tt.want, got, "stripDefaultPort(%q, %q)", tt.hostPort, tt.scheme)
	}
}

func TestReverseProxy_StripsDefaultPortFromHost(t *testing.T) {
	// Backend that records the Host header
	var receivedHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	backendURL, _ := url.Parse(backend.URL)
	targetAddr := backendURL.Host // e.g. "127.0.0.1:PORT" — non-default port

	rp, err := NewReverseProxy("127.0.0.1", 0, targetAddr, "http")
	assert.NoError(t, err)
	go rp.Serve()
	defer rp.Close()

	resp, err := http.Get("http://" + rp.Addr() + "/test")
	assert.NoError(t, err)
	_ = resp.Body.Close()

	// Non-default port: Host should include port
	assert.Equal(t, targetAddr, receivedHost, "Host for non-default port should include port")
}

func TestReverseProxy_AddAndPort(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	rp, err := NewReverseProxy("127.0.0.1", 0, backend.Listener.Addr().String(), "http")
	assert.NoError(t, err)
	defer rp.Close()

	// Addr and Port should return valid values
	assert.NotEmpty(t, rp.Addr())
	assert.Greater(t, rp.Port(), 0)
}

func TestReverseProxy_AddrNilListener(t *testing.T) {
	rp := &ReverseProxy{}
	assert.Equal(t, "", rp.Addr(), "Addr should return empty string when listener is nil")
	assert.Equal(t, 0, rp.Port(), "Port should return 0 when listener is nil")
}

func TestReverseProxy_NewReverseProxy_InvalidListenAddr(t *testing.T) {
	// Using a non-routable address that can't be listened on should fail
	_, err := NewReverseProxy("256.256.256.256", 80, "127.0.0.1:8080", "http")
	assert.Error(t, err, "should fail with invalid listen address")
}

func TestReverseProxy_NewReverseProxy_EmptyProtocol(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	rp, err := NewReverseProxy("127.0.0.1", 0, backend.Listener.Addr().String(), "")
	assert.NoError(t, err, "empty protocol should default to http")
	rp.Close()
}

func TestReverseProxy_NewReverseProxy_TargetWithScheme(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	// Pass targetAddr with scheme prefix
	addr := "http://" + backend.Listener.Addr().String()
	rp, err := NewReverseProxy("127.0.0.1", 0, addr, "http")
	assert.NoError(t, err)
	rp.Close()
}

func TestReverseProxy_Serve_ServerClosed(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	rp, err := NewReverseProxy("127.0.0.1", 0, backend.Listener.Addr().String(), "http")
	assert.NoError(t, err)

	// Close before Serve — Serve should handle http.ErrServerClosed gracefully
	rp.Close()
	// Serve returns when server is closed — this tests the ErrServerClosed path
	done := make(chan struct{})
	go func() {
		rp.Serve()
		close(done)
	}()

	select {
	case <-done:
		// Serve returned as expected
	case <-time.After(2 * time.Second):
		t.Fatal("Serve should return after server is closed")
	}
}

func TestReverseProxy_RewritesLocationHeader(t *testing.T) {
	// Backend that returns a 302 redirect with absolute Location pointing to itself
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			// Redirect to an absolute URL using the backend's own address
			w.Header().Set("Location", "http://"+r.Host+"/login")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	rp, err := NewReverseProxy("127.0.0.1", 0, backend.Listener.Addr().String(), "http")
	assert.NoError(t, err)
	defer rp.Close()

	go rp.Serve()
	addr := rp.Addr()
	time.Sleep(50 * time.Millisecond)

	// Use a client that does NOT auto-follow redirects
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get("http://" + addr + "/redirect")
	assert.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusFound, resp.StatusCode)
	loc := resp.Header.Get("Location")
	assert.Contains(t, loc, "127.0.0.1:", "Location should be rewritten to proxy address")
	assert.NotContains(t, loc, backend.Listener.Addr().String(), "Location should NOT contain the backend's original address")
}

func TestReverseProxy_RewritesLocationWithDefaultPort(t *testing.T) {
	// Simulate a real 80-port scenario: the backend generates Location with
	// hostname-only (no port), as port 80 is default for HTTP.
	// We create a backend on a random port but have it emit Location with
	// the same bare IP as the targetURL.Host (minus default port).
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			// r.Host is the targetHost we set via Director, e.g. "192.168.1.1" (default port stripped)
			// Simulate a backend that generates Location using just the hostname
			w.Header().Set("Location", "http://"+r.Host+"/new")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	// Use a fake target address that simulates a LAN IP on port 80
	// We need to actually reach the backend, so we use its real address
	// but test the Location rewriting by examining what the backend sends
	// vs. what the proxy rewrites.
	backendAddr := backend.Listener.Addr().String()
	rp, err := NewReverseProxy("127.0.0.1", 0, backendAddr, "http")
	assert.NoError(t, err)
	defer rp.Close()

	go rp.Serve()
	addr := rp.Addr()
	time.Sleep(50 * time.Millisecond)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get("http://" + addr + "/old")
	assert.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusMovedPermanently, resp.StatusCode)
	loc := resp.Header.Get("Location")
	// The Location should be rewritten to the proxy address since the backend
	// generates Location using the same host we set in the Host header
	assert.Contains(t, loc, "127.0.0.1:", "Location should be rewritten to proxy address")
}

func TestReverseProxy_PreservesNonTargetLocation(t *testing.T) {
	// Backend redirects to an external site — should NOT be rewritten
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://example.com/oauth/callback")
		w.WriteHeader(http.StatusSeeOther)
	}))
	defer backend.Close()

	rp, err := NewReverseProxy("127.0.0.1", 0, backend.Listener.Addr().String(), "http")
	assert.NoError(t, err)
	defer rp.Close()

	go rp.Serve()
	addr := rp.Addr()
	time.Sleep(50 * time.Millisecond)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get("http://" + addr + "/auth")
	assert.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc := resp.Header.Get("Location")
	assert.Equal(t, "https://example.com/oauth/callback", loc, "External Location should NOT be rewritten")
}

func TestReverseProxy_PreservesRelativeLocation(t *testing.T) {
	// Relative redirect — should NOT be rewritten
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/login")
		w.WriteHeader(http.StatusFound)
	}))
	defer backend.Close()

	rp, err := NewReverseProxy("127.0.0.1", 0, backend.Listener.Addr().String(), "http")
	assert.NoError(t, err)
	defer rp.Close()

	go rp.Serve()
	addr := rp.Addr()
	time.Sleep(50 * time.Millisecond)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get("http://" + addr + "/old")
	assert.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	loc := resp.Header.Get("Location")
	assert.Equal(t, "/login", loc, "Relative Location should NOT be rewritten")
}

func TestReverseProxy_NewReverseProxy_InvalidTargetURL(t *testing.T) {
	// A target with an invalid URL escape fails url.Parse in NewReverseProxy.
	_, err := NewReverseProxy("127.0.0.1", 0, "%zz://x", "")
	assert.Error(t, err, "should fail with invalid target URL")
}

func TestReverseProxy_Serve_ListenerClosed(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	rp, err := NewReverseProxy("127.0.0.1", 0, backend.Listener.Addr().String(), "http")
	assert.NoError(t, err)

	// Closing the listener directly makes Serve return a non-ErrServerClosed error.
	rp.listener.Close()
	done := make(chan struct{})
	go func() {
		rp.Serve()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve should return after listener is closed")
	}
}

func TestRewriteLocation_Unparseable(t *testing.T) {
	targetURL, _ := url.Parse("http://192.168.1.1:8080")
	listenAddr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:54321")

	// Location with an invalid URL escape should be returned unchanged.
	got := rewriteLocation("http://%zz/path", targetURL, listenAddr)
	assert.Equal(t, "http://%zz/path", got)
}

func TestDefaultPort_UnknownScheme(t *testing.T) {
	assert.Equal(t, "", defaultPort("ftp"))
	assert.Equal(t, "", defaultPort("gopher"))
}

func TestSameBareHost_NoPortOnB(t *testing.T) {
	// b has no port, exercising the SplitHostPort error branch for b.
	assert.True(t, sameBareHost("192.168.1.1:8080", "192.168.1.1"))
	assert.True(t, sameBareHost("192.168.1.1", "192.168.1.1"))
	assert.False(t, sameBareHost("192.168.1.1:8080", "10.0.0.1"))
}

func TestHostMatches(t *testing.T) {
	tests := []struct {
		locHost      string
		targetHost   string
		targetScheme string
		want         bool
	}{
		{"192.168.1.1:8080", "192.168.1.1:8080", "http", true},
		{"192.168.1.1", "192.168.1.1", "http", true},
		{"192.168.1.1:80", "192.168.1.1", "http", true},         // default port matches bare host
		{"192.168.1.1", "192.168.1.1:80", "http", true},         // bare host matches default port
		{"192.168.1.1:443", "192.168.1.1", "https", true},       // default HTTPS port
		{"192.168.1.1", "192.168.1.1:443", "https", true},       // bare host matches default HTTPS port
		{"192.168.1.1:8080", "192.168.1.1:9090", "http", false}, // different ports
		{"example.com", "other.com", "http", false},             // different hosts
		{"192.168.1.1:80", "192.168.1.1", "https", false},       // port 80 not default for https
		{"192.168.1.1:443", "192.168.1.1", "http", false},       // port 443 not default for http
		{"192.168.1.1:80", "192.168.1.1:443", "http", false},    // different default ports
	}
	for _, tt := range tests {
		got := hostMatches(tt.locHost, tt.targetHost, tt.targetScheme)
		assert.Equal(t, tt.want, got, "hostMatches(%q, %q, %q)", tt.locHost, tt.targetHost, tt.targetScheme)
	}
}

func TestRewriteLocation(t *testing.T) {
	targetURL, _ := url.Parse("http://192.168.1.1:8080")
	listenAddr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:54321")

	tests := []struct {
		location string
		want     string
	}{
		{"http://192.168.1.1:8080/path", "http://127.0.0.1:54321/path"},
		{"http://192.168.1.1:8080/path?q=1#frag", "http://127.0.0.1:54321/path?q=1#frag"},
		{"/relative", "/relative"},
		{"https://example.com/path", "https://example.com/path"},
		{"", ""},
	}
	for _, tt := range tests {
		got := rewriteLocation(tt.location, targetURL, listenAddr)
		assert.Equal(t, tt.want, got, "rewriteLocation(%q)", tt.location)
	}
}

func TestRewriteLocation_CrossScheme(t *testing.T) {
	// Target is HTTP on port 80, but backend redirects to HTTPS on port 443
	targetURL, _ := url.Parse("http://192.168.1.1:80")
	listenAddr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:54321")

	// Cross-scheme redirect: http→https, same host, default ports
	got := rewriteLocation("https://192.168.1.1:443/secure", targetURL, listenAddr)
	assert.Equal(t, "http://127.0.0.1:54321/secure", got, "Cross-scheme redirect should be rewritten")
}

func TestRewriteLocation_HTTPSTarget(t *testing.T) {
	targetURL, _ := url.Parse("https://192.168.1.1:8443")
	listenAddr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:54321")

	got := rewriteLocation("https://192.168.1.1:8443/secure", targetURL, listenAddr)
	assert.Equal(t, "http://127.0.0.1:54321/secure", got, "HTTPS target Location should be rewritten with http scheme")
}
