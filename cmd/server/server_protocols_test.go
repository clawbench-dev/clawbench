package main

import (
	"context"
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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

// --- Step 1: the pure helper's contract -------------------------------------

func TestServerProtocols_PlaintextGetters(t *testing.T) {
	p := serverProtocols(false)
	require.NotNil(t, p)
	require.True(t, p.HTTP1(), "HTTP/1.1 must stay on: 5 WebSocket endpoints need Hijacker")
	require.True(t, p.UnencryptedHTTP2(), "plaintext deployments must accept h2c")
	require.False(t, p.HTTP2(), "h2-over-TLS is meaningless without TLS")
}

func TestServerProtocols_TLSGetters(t *testing.T) {
	p := serverProtocols(true)
	require.NotNil(t, p)
	require.True(t, p.HTTP1(), "HTTP/1.1 must stay on: 5 WebSocket endpoints need Hijacker")
	require.True(t, p.HTTP2(), "TLS deployments must advertise ALPN h2")
	require.False(t, p.UnencryptedHTTP2(), "h2c must not be enabled when TLS is on")
}

// --- shared test handler ----------------------------------------------------

// echoHandler serves /echo (replies with the negotiated protocol) and /ws
// (a coder/websocket upgrade that needs http.Hijacker, i.e. HTTP/1.1 only).
func echoHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, r.Proto)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		_ = c.Write(r.Context(), websocket.MessageText, []byte("hello"))
	})
	return mux
}

// h2cClient is an h2c prior-knowledge client. The stdlib http.Transport does
// NOT do prior-knowledge even when Protocols has UnencryptedHTTP2 set (measured:
// it still negotiates HTTP/1.1), so this must use x/net/http2 directly.
func h2cClient() *http.Client {
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
}

// --- Step 2: the real acceptance test: three protocols on one listener ------

func TestServerProtocols_PlaintextCoexistence(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := &http.Server{Handler: echoHandler(), Protocols: serverProtocols(false)}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	base := "http://" + ln.Addr().String()

	// (a) h1 must survive: Protocols non-nil replaces the defaults, so this
	// guards the five WebSocket endpoints against a missing SetHTTP1.
	r1, err := http.Get(base + "/echo")
	require.NoError(t, err, "h1 must survive: Protocols non-nil replaces defaults")
	b1, err := io.ReadAll(r1.Body)
	require.NoError(t, err)
	r1.Body.Close()
	require.Equal(t, "HTTP/1.1", string(b1))
	t.Logf("h1  -> %s %s", r1.Proto, string(b1))

	// (b) h2c prior-knowledge on the very same listener.
	c2 := h2cClient()
	r2, err := c2.Get(base + "/echo")
	require.NoError(t, err, "h2c prior-knowledge must work")
	b2, err := io.ReadAll(r2.Body)
	require.NoError(t, err)
	r2.Body.Close()
	require.Equal(t, "HTTP/2.0", string(b2))
	require.Equal(t, 2, r2.ProtoMajor, "integration test must really exercise h2c")
	t.Logf("h2c -> %s %s (ProtoMajor=%d)", r2.Proto, string(b2), r2.ProtoMajor)

	// (c) WS upgrade still works — coder/websocket needs http.Hijacker, which
	// only exists on HTTP/1.1. This is the endpoint class most at risk.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + base[len("http"):] + "/ws"
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err, "WS upgrade must survive (needs SetHTTP1(true))")
	defer ws.Close(websocket.StatusNormalClosure, "")
	_, msg, err := ws.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "hello", string(msg))
	t.Logf("ws  -> upgrade ok, message=%q", string(msg))
}

// --- Step 3: h2-over-TLS through the production ServeTLS path --------------

// writeSelfSignedCert writes a throwaway cert/key pair for 127.0.0.1.
func writeSelfSignedCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "clawbench-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	certOut, err := os.Create(certFile)
	require.NoError(t, err)
	require.NoError(t, pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}))
	require.NoError(t, certOut.Close())

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyOut, err := os.Create(keyFile)
	require.NoError(t, err)
	require.NoError(t, pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	require.NoError(t, keyOut.Close())

	return certFile, keyFile
}

// TestServerProtocols_TLSCoexistence drives the exact production path
// (http.Server.Protocols + ServeTLS) and asserts that HTTP/1.1 and h2 are
// both reachable over TLS. ServeTLS derives its ALPN list from
// s.protocols(), so this fails if SetHTTP2 is not explicit.
func TestServerProtocols_TLSCoexistence(t *testing.T) {
	certFile, keyFile := writeSelfSignedCert(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := &http.Server{Handler: echoHandler(), Protocols: serverProtocols(true)}
	go func() { _ = srv.ServeTLS(ln, certFile, keyFile) }()
	defer func() { _ = srv.Close() }()

	// Trust the throwaway cert but do NOT negotiate h2: this forces ALPN to
	// fall back to http/1.1, proving "http/1.1" is still in NextProtos.
	h1Tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    x509.NewCertPool(),
			NextProtos: []string{"http/1.1"},
			MinVersion: tls.VersionTLS12,
		},
	}
	pool := h1Tr.TLSClientConfig.RootCAs
	pemBytes, err := os.ReadFile(certFile)
	require.NoError(t, err)
	require.True(t, pool.AppendCertsFromPEM(pemBytes))

	base := "https://" + ln.Addr().String()
	c1 := &http.Client{Transport: h1Tr}
	r1, err := c1.Get(base + "/echo")
	require.NoError(t, err, "h1-over-TLS must survive")
	b1, err := io.ReadAll(r1.Body)
	require.NoError(t, err)
	r1.Body.Close()
	require.Equal(t, "HTTP/1.1", string(b1))
	require.Equal(t, "http/1.1", r1.TLS.NegotiatedProtocol,
		"server must still advertise http/1.1 in ALPN, or h1 clients get 'no application protocol'")
	t.Logf("h1-over-TLS  -> %s %s ALPN=%q", r1.Proto, string(b1), r1.TLS.NegotiatedProtocol)

	// h2-over-TLS: use x/net/http2's transport. A stdlib http.Transport with a
	// custom TLSClientConfig leaves ForceAttemptHTTP2=false, so it ignores
	// NextProtos and writes an HTTP/1.1 request down an ALPN-h2 connection —
	// the server then logs "bogus greeting" and the client sees raw frames.
	h2Tr := &http2.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    pool,
			MinVersion: tls.VersionTLS12,
		},
	}
	r2, err := (&http.Client{Transport: h2Tr}).Get(base + "/echo")
	require.NoError(t, err, "h2-over-TLS must work (ServeTLS needs explicit SetHTTP2)")
	b2, err := io.ReadAll(r2.Body)
	require.NoError(t, err)
	r2.Body.Close()
	require.Equal(t, "HTTP/2.0", string(b2))
	require.Equal(t, 2, r2.ProtoMajor)
	t.Logf("h2-over-TLS  -> %s %s (ProtoMajor=%d)", r2.Proto, string(b2), r2.ProtoMajor)

	// TLS state must agree with the negotiated protocol.
	require.Equal(t, "h2", r2.TLS.NegotiatedProtocol)
}
