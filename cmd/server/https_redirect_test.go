package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// selfSigned writes a throwaway cert/key pair for localhost and returns the
// file paths, so the mux can be exercised against a real ServeTLS.
func selfSigned(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

// startMuxInstance boots the mux + TLS server on an ephemeral port, on the same
// wiring main() uses, and returns the port plus a stop function.
func startMuxInstance(t *testing.T, handler http.Handler) (port int, stop func()) {
	t.Helper()
	certFile, keyFile := selfSigned(t)

	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port = raw.Addr().(*net.TCPAddr).Port

	mux, tlsLn := newRedirectMux(raw, port)
	srv := &http.Server{Handler: handler, Protocols: serverProtocols(true)}

	errc := make(chan error, 1)
	go func() { errc <- srv.ServeTLS(tlsLn, certFile, keyFile) }()

	stopped := make(chan struct{})
	stop = func() {
		select {
		case <-stopped:
			return
		default:
		}
		close(stopped)
		_ = mux.Close()
		srv.Close()
		<-errc
	}
	t.Cleanup(stop)
	return port, stop
}

// plainClient never follows redirects, so the 301 itself is observable.
func plainClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func TestRedirectMux_PlainHTTPGets301ToSamePort(t *testing.T) {
	port, _ := startMuxInstance(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("plaintext request reached the TLS handler: %s %s", r.Method, r.URL)
	}))

	resp, err := plainClient().Get(fmt.Sprintf("http://127.0.0.1:%d/some/path?q=1", port))
	if err != nil {
		t.Fatalf("plain GET failed: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", resp.StatusCode)
	}
	// The redirect must point at the SAME port, not the port plus something or
	// the default 443.
	want := fmt.Sprintf("https://127.0.0.1:%d/some/path?q=1", port)
	if got := resp.Header.Get("Location"); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
}

func TestRedirectMux_HTTPSReachesHandler(t *testing.T) {
	port, _ := startMuxInstance(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Scheme", "https")
		_, _ = w.Write([]byte("hello-tls"))
	}))

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/", port))
	if err != nil {
		t.Fatalf("https GET failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if string(body) != "hello-tls" {
		t.Fatalf("body = %q, want hello-tls", body)
	}
}

// The TLS bytes consumed by the classifier must be replayed to the TLS server,
// otherwise the handshake is missing its ClientHello record header.
func TestRedirectMux_TLSClientHelloSurvivesSniffing(t *testing.T) {
	port, _ := startMuxInstance(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("tls dial failed (sniffed bytes not replayed?): %v", err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	if !strings.Contains(line, "200") {
		t.Fatalf("status line = %q, want 200", line)
	}
}

// A socket that connects and sends nothing must not be able to keep the mux
// (and thus the port) alive after shutdown.
func TestRedirectMux_CloseDropsSniffingConns(t *testing.T) {
	port, stop := startMuxInstance(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	time.Sleep(100 * time.Millisecond) // let it reach the sniff read

	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked on a silent connection")
	}

	// The raw port must actually be free again.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("port still bound after Close: %v", err)
	}
	_ = ln.Close()
}

func TestSniffTLS(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want bool
	}{
		{"client hello", []byte{0x16, 0x03, 0x01}, true},
		{"tls1.3 hello", []byte{0x16, 0x03, 0x03}, true},
		{"app data", []byte{0x17, 0x03, 0x03}, true},
		{"alert", []byte{0x15, 0x03, 0x03}, true},
		{"http GET", []byte("GET"), false},
		{"http POST", []byte("POS"), false},
		{"only content type", []byte{0x16}, false},
		{"bad version", []byte{0x16, 0x00, 0x01}, false},
		{"future version rejected", []byte{0x16, 0x03, 0x09}, false},
		{"empty", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sniffTLS(tc.in); got != tc.want {
				t.Fatalf("sniffTLS(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestRedirectToHTTPS_StripsIncomingPort(t *testing.T) {
	// A Host that already carries a port must not produce "host:port:port".
	h := redirectToHTTPS(20000)
	req := httptest.NewRequest("GET", "http://box:8080/a?b=c", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got, want := rec.Header().Get("Location"), "https://box:20000/a?b=c"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestRedirectToHTTPS_NoHost(t *testing.T) {
	h := redirectToHTTPS(20000)
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = ""
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (a redirect with no host would be useless)", rec.Code)
	}
}
