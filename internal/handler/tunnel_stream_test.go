package handler

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/tunnel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

// ---------- harness ----------

// setupTunnelTest starts an httptest server exposing the real route with the
// real auth middleware, and installs a ProxyRegistry so the whitelist has a
// source. It returns the server URL and a teardown func.
//
// The server is configured for HTTP/1.1 + h2c so the same harness can exercise
// both the h1 fallback and real HTTP/2 framing. A plain httptest.NewServer is
// h1-only: h2c prior-knowledge is refused unless the server's Protocols
// enables UnencryptedHTTP2 (the same configuration T1 applies to the real
// server), which is why this uses NewUnstartedServer.
func setupTunnelTest(t *testing.T) (string, func()) {
	t.Helper()
	env, teardown := setupTestEnv(t)
	_ = env

	origProxy := service.ProxyService
	service.ProxyService = service.NewProxyRegistry(0) // default allowed_ports = 1024-65535

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tunnel/stream", middleware.Auth(TunnelStream))

	server := httptest.NewUnstartedServer(mux)
	server.Config.Protocols = new(http.Protocols)
	server.Config.Protocols.SetHTTP1(true)            // 5 WS endpoints need Hijacker; also the h1 fallback
	server.Config.Protocols.SetUnencryptedHTTP2(true) // h2c prior-knowledge
	server.Start()

	cleanup := func() {
		server.Close()
		service.ProxyService.Stop()
		service.ProxyService = origProxy
		teardown()
	}
	return server.URL, cleanup
}

// startTunnelEchoServer starts a TCP echo server on a random loopback port.
// It mirrors internal/ssh/server_test.go:75's helper, which is in another
// package and so cannot be reused directly.
func startTunnelEchoServer(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "failed to start echo server")
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 32*1024)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						_, _ = c.Write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	return port
}

// h2cTunnelClient is an h2c prior-knowledge client. The stdlib http.Transport
// does NOT do prior-knowledge even with Protocols.UnencryptedHTTP2 set
// (measured), so the x/net transport must be used directly.
func h2cTunnelClient() *http.Client {
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

// h1TunnelClient is a plain HTTP/1.1 client whose header wait is bounded. The
// tunnel is long-lived, so a whole-request timeout would be wrong; bounding
// only the response-header wait turns the h1 half-duplex deadlock (server
// swallowing the body before writing headers) into a fast failure instead of a
// hung test.
func h1TunnelClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{ResponseHeaderTimeout: 5 * time.Second},
	}
}

// streamURL builds the tunnel URL with a properly escaped target.
func streamURL(base string, host string, port int) string {
	return base + "/api/tunnel/stream?host=" + host + "&port=" + strconv.Itoa(port)
}

// openTunnelStream issues a streaming POST with an unbounded duplex body. The
// returned request is already sent; callers write to the writer and read the
// response.
func openTunnelStream(t *testing.T, client *http.Client, url string) (*io.PipeWriter, *http.Response) {
	t.Helper()
	pr, pw := io.Pipe()
	req, err := http.NewRequest(http.MethodPost, url, pr)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := client.Do(req)
	require.NoError(t, err)
	return pw, resp
}

// ---------- tests ----------

func TestTunnelStream_RequiresPost(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	resp, err := http.Get(streamURL(url, "127.0.0.1", 8080))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestTunnelStream_UnauthenticatedIsRejected(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	// setupTestEnv blanks the tokens (open access), so a real token has to be
	// installed to exercise middleware.Auth's rejection path.
	model.SessionToken = "secret-token"
	model.CookieToken = "secret-cookie-token"
	defer func() {
		model.SessionToken = ""
		model.CookieToken = ""
	}()

	req, err := http.NewRequest(http.MethodPost, streamURL(url, "127.0.0.1", 8080), bytes.NewReader(nil))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestTunnelStream_NilProxyServiceIsUnavailable(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	orig := service.ProxyService
	service.ProxyService = nil
	defer func() { service.ProxyService = orig }()

	req, err := http.NewRequest(http.MethodPost, streamURL(url, "127.0.0.1", 8080), bytes.NewReader(nil))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode,
		"without a registry there is no whitelist, so the stream must be refused")
}

func TestTunnelStream_DisallowedPortIsForbidden(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	service.ProxyService.SetAllowedPorts("3000-4000")

	req, err := http.NewRequest(http.MethodPost, streamURL(url, "127.0.0.1", 9999), bytes.NewReader(nil))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestTunnelStream_InvalidPortIsBadRequest(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	for _, port := range []string{"", "abc", "0", "65536", "-1"} {
		t.Run("port="+port, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, url+"/api/tunnel/stream?port="+port, bytes.NewReader(nil))
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
}

// TestTunnelStream_AllowedButUnregisteredPortWorks pins the SSH parity point
// (internal/ssh/server_test.go:242): the whitelist only checks the port range,
// it does not require the port to be pre-registered.
func TestTunnelStream_AllowedButUnregisteredPortWorks(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	echoPort := startTunnelEchoServer(t)
	// Deliberately do NOT register echoPort; it is inside 1024-65535.

	pw, resp := openTunnelStream(t, http.DefaultClient, streamURL(url, "127.0.0.1", echoPort))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	msg := []byte("hello unregistered port")
	_, err := pw.Write(msg)
	require.NoError(t, err)

	buf := make([]byte, len(msg))
	_, err = io.ReadFull(resp.Body, buf)
	require.NoError(t, err)
	assert.Equal(t, msg, buf)

	_ = pw.Close()
}

func TestTunnelStream_EmptyHostDefaultsToLoopback(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	echoPort := startTunnelEchoServer(t)
	pw, resp := openTunnelStream(t, http.DefaultClient, streamURL(url, "", echoPort))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	_, err := pw.Write([]byte("loopback"))
	require.NoError(t, err)
	buf := make([]byte, len("loopback"))
	_, err = io.ReadFull(resp.Body, buf)
	require.NoError(t, err)
	assert.Equal(t, "loopback", string(buf))
	_ = pw.Close()
}

// TestTunnelStream_FullDuplex_WriteAfterResponseHeader is the core duplex
// assertion, run over the h1 fallback path: the client receives the 200 (and
// echoed data) while it is STILL able to write more request body. Without
// EnableFullDuplex the server swallows the unread body before writing headers
// and this deadlocks until ResponseHeaderTimeout fires.
func TestTunnelStream_FullDuplex_WriteAfterResponseHeader(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	echoPort := startTunnelEchoServer(t)
	pw, resp := openTunnelStream(t, h1TunnelClient(), streamURL(url, "127.0.0.1", echoPort))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "response header must arrive before the body is finished")
	require.Equal(t, 1, resp.ProtoMajor, "this case must exercise the HTTP/1.1 fallback")

	// Round 1.
	_, err := pw.Write([]byte("first"))
	require.NoError(t, err)
	buf := make([]byte, len("first"))
	_, err = io.ReadFull(resp.Body, buf)
	require.NoError(t, err)
	assert.Equal(t, "first", string(buf))

	// The stream is still writable after a full round trip.
	_, err = pw.Write([]byte("second"))
	require.NoError(t, err)
	buf = make([]byte, len("second"))
	_, err = io.ReadFull(resp.Body, buf)
	require.NoError(t, err)
	assert.Equal(t, "second", string(buf))

	_ = pw.Close()
}

func TestTunnelStream_LargeDataTransfer(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	echoPort := startTunnelEchoServer(t)
	pw, resp := openTunnelStream(t, http.DefaultClient, streamURL(url, "127.0.0.1", echoPort))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	large := make([]byte, 64*1024)
	for i := range large {
		large[i] = byte(i % 256)
	}

	writeErr := make(chan error, 1)
	go func() {
		_, err := pw.Write(large)
		if err == nil {
			err = pw.Close()
		}
		writeErr <- err
	}()

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, <-writeErr)
	require.Len(t, got, len(large), "must receive all 64 KiB back")
	assert.True(t, bytes.Equal(got, large), "64 KiB payload must round-trip byte-for-byte")
}

// TestTunnelStream_MultipleConcurrentStreams mirrors SSH's
// TestSSHPortForward_MultiplePorts: several independent streams, each echoing
// its own payload, must not interfere.
func TestTunnelStream_MultipleConcurrentStreams(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	echo1 := startTunnelEchoServer(t)
	echo2 := startTunnelEchoServer(t)

	const streams = 8
	var wg sync.WaitGroup
	errs := make(chan error, streams)

	for i := range streams {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			port := echo1
			if i%2 == 1 {
				port = echo2
			}
			pw, resp := openTunnelStream(t, http.DefaultClient, streamURL(url, "127.0.0.1", port))
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("stream %d: status %d", i, resp.StatusCode)
				return
			}
			payload := fmt.Sprintf("stream-%d", i)
			if _, err := pw.Write([]byte(payload)); err != nil {
				errs <- fmt.Errorf("stream %d write: %w", i, err)
				return
			}
			buf := make([]byte, len(payload))
			if _, err := io.ReadFull(resp.Body, buf); err != nil {
				errs <- fmt.Errorf("stream %d read: %w", i, err)
				return
			}
			if string(buf) != payload {
				errs <- fmt.Errorf("stream %d: got %q, want %q", i, buf, payload)
				return
			}
			_ = pw.Close()
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestTunnelStream_DialFailureReturns502BeforeAnyBody is the "no half-written
// response" guard: a dead target must produce a clean 502, never a 200 with a
// truncated body.
func TestTunnelStream_DialFailureReturns502BeforeAnyBody(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	// Reserve a port then close the listener so nothing is listening on it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	deadPort, _ := strconv.Atoi(portStr)
	require.NoError(t, ln.Close())

	resp, err := http.Post(streamURL(url, "127.0.0.1", deadPort), "application/octet-stream", bytes.NewReader(nil))
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode,
		"dial failure must be reported before the 200 header is written")
	assert.NotContains(t, string(body), "200", "body must not carry a successful-stream payload")
}

// TestTunnelStream_TargetHalfCloseStillReceivesLateClientWrites is the
// end-to-end version of the counterexample an independent verifier found by
// driving a real downstream client through h2 -L: a target that sends FIN
// (CloseWrite) but keeps reading must still receive bytes the client writes
// afterwards. Over SSH the bytes arrive because relayBidir waits for both
// directions; the h2 path used to drop them, because RelayDuplex returned as
// soon as the target->client direction ended and the handler then closed the
// backend. The bounded drain grace is what makes the late write land here.
//
// This runs over h2c (the transport the verifier used) so the assertion covers
// the real framing, not the h1 fallback.
func TestTunnelStream_TargetHalfCloseStillReceivesLateClientWrites(t *testing.T) {
	restore := tunnel.SetRelayDrainGraceForTest(2 * time.Second)
	defer restore()

	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	received := make(chan string, 1)
	halfClosed := make(chan struct{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		first := make([]byte, len("first"))
		if _, err := io.ReadFull(conn, first); err != nil {
			received <- "read-first-failed: " + err.Error()
			return
		}

		// Send FIN: the response direction ends, but keep reading.
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		close(halfClosed)

		// Everything else the client sends must still arrive.
		rest, err := io.ReadAll(conn)
		if err != nil {
			received <- "read-rest-failed: " + err.Error()
			return
		}
		received <- string(first) + string(rest)
	}()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	targetPort, _ := strconv.Atoi(portStr)

	pw, resp := openTunnelStream(t, h2cTunnelClient(), streamURL(url, "127.0.0.1", targetPort))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 2, resp.ProtoMajor, "the counterexample must be exercised over HTTP/2")

	_, err = pw.Write([]byte("first"))
	require.NoError(t, err)

	// Wait until the target has half-closed, so the late write lands strictly
	// after the target->client direction ended.
	select {
	case <-halfClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("target never observed the first write")
	}

	_, err = pw.Write([]byte("late"))
	require.NoError(t, err)
	require.NoError(t, pw.Close())

	select {
	case got := <-received:
		assert.Equal(t, "firstlate", got,
			"bytes written after the target half-closed must reach the still-reading target")
	case <-time.After(5 * time.Second):
		t.Fatal("target never reported its received bytes")
	}
}

// TestTunnelStream_TargetCloseWhileClientBodyOpenIsBounded pins the T3
// deadlock fix at the HTTP layer: a target that closes outright (short-lived
// backend, or a reverse-tunnel visitor hanging up) while the client keeps its
// request body open must not park the handler. The response body must reach
// EOF within the grace plus margin, otherwise the client hangs forever waiting
// for a response that never ends.
func TestTunnelStream_TargetCloseWhileClientBodyOpenIsBounded(t *testing.T) {
	const grace = 200 * time.Millisecond
	restore := tunnel.SetRelayDrainGraceForTest(grace)
	defer restore()

	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("bye"))
		_ = conn.Close() // close outright, not half-close
	}()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	targetPort, _ := strconv.Atoi(portStr)

	pw, resp := openTunnelStream(t, http.DefaultClient, streamURL(url, "127.0.0.1", targetPort))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Deliberately never close pw: the client body stays open, modeling a
	// client that is still waiting for the response to finish.
	start := time.Now()
	body, err := io.ReadAll(resp.Body)
	elapsed := time.Since(start)

	require.NoError(t, err, "the response must end cleanly once the grace expires")
	assert.Equal(t, "bye", string(body), "the target's bytes must arrive before the response ends")
	assert.Less(t, elapsed, 3*time.Second,
		"the handler must end within the grace plus margin, not hang until the client closes its body")
	_ = pw.Close()
}

// TestTunnelStream_HalfCloseLetsTargetKeepResponding exercises the half-close
// contract end to end: after the client finishes its request body, the target
// still gets to send its remaining bytes.
func TestTunnelStream_HalfCloseLetsTargetKeepResponding(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	// A target that reads until EOF, then replies with a fixed marker.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn) // blocks until the client half-closes
		_, _ = conn.Write([]byte("after-eof"))
	}()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	targetPort, _ := strconv.Atoi(portStr)

	pw, resp := openTunnelStream(t, http.DefaultClient, streamURL(url, "127.0.0.1", targetPort))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	_, err = pw.Write([]byte("request"))
	require.NoError(t, err)
	// Ending the request body = END_STREAM / half-close.
	require.NoError(t, pw.Close())

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "after-eof", string(got),
		"the target must still be able to write after the client half-closes")
}

// TestTunnelStream_H2CFullDuplex proves the tunnel works over real h2c, not
// just the h1 fallback path. The h2c transport buffers the request body, so
// the echo round trip is driven by writing the body up front and closing it —
// what this asserts is that the whole stream (whitelist, dial, relay,
// half-close) works under HTTP/2 framing.
func TestTunnelStream_H2CFullDuplex(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	echoPort := startTunnelEchoServer(t)
	client := h2cTunnelClient()

	payload := bytes.Repeat([]byte("h2c-payload-"), 512)
	req, err := http.NewRequest(http.MethodPost, streamURL(url, "127.0.0.1", echoPort), bytes.NewReader(payload))
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 2, resp.ProtoMajor, "the test must really exercise HTTP/2, not h1")

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(got, payload), "h2c echo must round-trip byte-for-byte")
}

// TestTunnelStream_H2CClientStreamsBodyAfterHeaders drives the duplex
// behavior over h2c: the response headers arrive before the request body is
// finished, and the client keeps writing afterwards.
func TestTunnelStream_H2CClientStreamsBodyAfterHeaders(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	echoPort := startTunnelEchoServer(t)
	client := h2cTunnelClient()

	pw, resp := openTunnelStream(t, client, streamURL(url, "127.0.0.1", echoPort))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, 2, resp.ProtoMajor)

	_, err := pw.Write([]byte("chunk-a"))
	require.NoError(t, err)
	buf := make([]byte, len("chunk-a"))
	_, err = io.ReadFull(resp.Body, buf)
	require.NoError(t, err)
	assert.Equal(t, "chunk-a", string(buf))

	_, err = pw.Write([]byte("chunk-b"))
	require.NoError(t, err)
	buf = make([]byte, len("chunk-b"))
	_, err = io.ReadFull(resp.Body, buf)
	require.NoError(t, err)
	assert.Equal(t, "chunk-b", string(buf))

	_ = pw.Close()
}

// TestTunnelStream_ClientDisconnectClosesTarget guards the leak fix: when the
// client abandons the stream, the dialed target connection must be closed.
func TestTunnelStream_ClientDisconnectClosesTarget(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	closed := make(chan struct{}, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Detect the peer closing: a read returns an error once the server
		// closes the backend on client disconnect.
		buf := make([]byte, 1024)
		for {
			if _, err := conn.Read(buf); err != nil {
				closed <- struct{}{}
				return
			}
		}
	}()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	targetPort, _ := strconv.Atoi(portStr)

	pw, resp := openTunnelStream(t, http.DefaultClient, streamURL(url, "127.0.0.1", targetPort))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Abandon the stream: cancel the request context, which is what a client
	// disconnect looks like to the server.
	_ = pw.CloseWithError(context.Canceled)
	_ = resp.Body.Close()

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("target connection was not closed after the client disconnected")
	}
}

// TestTunnelStream_RegisteredPortWorks is the control case for the
// allowed-but-unregistered test: registering the port must not change the
// outcome.
func TestTunnelStream_RegisteredPortWorks(t *testing.T) {
	url, cleanup := setupTunnelTest(t)
	defer cleanup()

	echoPort := startTunnelEchoServer(t)
	_, err := service.ProxyService.RegisterPort(echoPort, "", "echo", "http", "")
	require.NoError(t, err)

	pw, resp := openTunnelStream(t, http.DefaultClient, streamURL(url, "127.0.0.1", echoPort))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	_, err = pw.Write([]byte("registered"))
	require.NoError(t, err)
	buf := make([]byte, len("registered"))
	_, err = io.ReadFull(resp.Body, buf)
	require.NoError(t, err)
	assert.Equal(t, "registered", string(buf))
	_ = pw.Close()
}
