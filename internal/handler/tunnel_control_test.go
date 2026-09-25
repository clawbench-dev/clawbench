package handler

import (
	"bufio"
	"bytes"
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
)

// ---------- harness ----------

// setupTunnelControlTest starts an h1+h2c server exposing both tunnel routes
// with the real auth middleware, and installs fresh bind/claim registries plus
// a ProxyRegistry so the whitelist has a source.
//
// model.ServerPort is pinned to 20000 so the guard's reserved ports match
// production (main 20000, SSH 20001) rather than the zero value.
func setupTunnelControlTest(t *testing.T) (string, func()) {
	t.Helper()
	env, teardown := setupTestEnv(t)
	_ = env

	origProxy := service.ProxyService
	service.ProxyService = service.NewProxyRegistry(0) // default allowed_ports = 1024-65535

	origPort := model.ServerPort
	model.ServerPort = 20000
	t.Cleanup(func() { model.ServerPort = origPort })

	origBinds, origClaims := tunnelBinds, tunnelClaims
	tunnelBinds = tunnel.NewBindRegistry()
	tunnelClaims = tunnel.NewClaimRegistry()
	t.Cleanup(func() { tunnelBinds, tunnelClaims = origBinds, origClaims })

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tunnel/stream", middleware.Auth(TunnelStream))
	mux.HandleFunc("/api/tunnel/control", middleware.Auth(TunnelControl))

	server := httptest.NewUnstartedServer(mux)
	server.Config.Protocols = new(http.Protocols)
	server.Config.Protocols.SetHTTP1(true)
	server.Config.Protocols.SetUnencryptedHTTP2(true)
	server.Start()

	cleanup := func() {
		server.Close()
		service.ProxyService.Stop()
		service.ProxyService = origProxy
		teardown()
	}
	return server.URL, cleanup
}

// openControlStream issues the long-lived duplex control request. The returned
// request body is left open; callers write NDJSON and read the response.
func openControlStream(t *testing.T, client *http.Client, base string) (*io.PipeWriter, *bufio.Reader, *http.Response) {
	t.Helper()
	pr, pw := io.Pipe()
	req, err := http.NewRequest(http.MethodPost, base+"/api/tunnel/control", pr)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-ndjson")

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "control stream must be accepted")
	return pw, bufio.NewReader(resp.Body), resp
}

// sendControl writes one NDJSON message to the control stream.
func sendControl(t *testing.T, pw *io.PipeWriter, msg tunnel.ControlMessage) {
	t.Helper()
	_, err := pw.Write(tunnel.EncodeControl(msg))
	require.NoError(t, err)
}

// readControl reads one NDJSON message, failing the test on timeout rather than
// hanging forever.
func readControl(t *testing.T, r *bufio.Reader) tunnel.ControlMessage {
	t.Helper()
	type result struct {
		msg tunnel.ControlMessage
		err error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := r.ReadBytes('\n')
		if err != nil {
			ch <- result{err: err}
			return
		}
		m, err := tunnel.DecodeControl(line)
		ch <- result{msg: m, err: err}
	}()

	select {
	case got := <-ch:
		require.NoError(t, got.err)
		return got.msg
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a control message")
		return tunnel.ControlMessage{}
	}
}

// bindAndExpect issues a bind and returns the resulting message, which is
// either `bound` or `bind_err`.
func bindAndExpect(t *testing.T, pw *io.PipeWriter, r *bufio.Reader, port int) tunnel.ControlMessage {
	t.Helper()
	sendControl(t, pw, tunnel.ControlMessage{Type: tunnel.MsgBind, Port: port})
	msg := readControl(t, r)
	require.Contains(t, []string{tunnel.MsgBound, tunnel.MsgBindErr}, msg.Type,
		"bind must answer with bound or bind_err, got %q", msg.Type)
	return msg
}

// mustBind issues a bind that is expected to succeed and returns the actual
// server-side port.
func mustBind(t *testing.T, pw *io.PipeWriter, r *bufio.Reader, port int) int {
	t.Helper()
	msg := bindAndExpect(t, pw, r, port)
	require.Equal(t, tunnel.MsgBound, msg.Type, "bind(%d) must succeed, got %+v", port, msg)
	require.Greater(t, msg.Port, 0, "bound must carry the real port")
	return msg.Port
}

// waitPortFree polls until a loopback port can be bound again, proving it was
// released. Mirrors internal/ssh/server_test.go's waitPortFree.
func waitPortFree(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			_ = ln.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("port %d was never released", port)
}

// freeLoopbackPort reserves and immediately releases a port so the test has a
// concrete number to ask for.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}

// isActive reports whether the registry has an Active reverse entry bound to
// serverPort. Mirrors internal/ssh/server_test.go's RegistryActiveLifecycle.
func isActive(serverPort int) bool {
	if service.ProxyService == nil {
		return false
	}
	for _, p := range service.ProxyService.ListPorts() {
		if p.LocalPort == serverPort {
			return p.Active
		}
	}
	return false
}

// ---------- method / auth / availability ----------

func TestTunnelControl_RequiresPost(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	resp, err := http.Get(base + "/api/tunnel/control")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestTunnelControl_UnauthenticatedIsRejected(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	// setupTestEnv blanks the tokens (open access), so a real token must be
	// installed to exercise middleware.Auth's rejection path.
	model.SessionToken = "secret-token"
	model.CookieToken = "secret-cookie-token"
	defer func() {
		model.SessionToken = ""
		model.CookieToken = ""
	}()

	req, err := http.NewRequest(http.MethodPost, base+"/api/tunnel/control", bytes.NewReader(nil))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestTunnelControl_NilProxyServiceIsUnavailable(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	orig := service.ProxyService
	service.ProxyService = nil
	defer func() { service.ProxyService = orig }()

	req, err := http.NewRequest(http.MethodPost, base+"/api/tunnel/control", bytes.NewReader(nil))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode,
		"without a registry there is no whitelist, so the control stream must be refused")
}

// ---------- bind guard parity ----------

func TestTunnelControl_RejectsReservedPorts(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()

	cases := []struct {
		name string
		port int
	}{
		{"main HTTP port", 20000},
		{"SSH port", 20001},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := bindAndExpect(t, pw, rd, tc.port)
			require.Equal(t, tunnel.MsgBindErr, msg.Type,
				"binding %s (%d) must be rejected", tc.name, tc.port)
			assert.Equal(t, tunnel.BindErrReservedOrTaken, msg.Code)
			assert.Equal(t, tc.port, msg.Port, "bind_err must echo the requested port")
		})
	}
}

func TestTunnelControl_RejectsPortOutsideAllowedRange(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	service.ProxyService.SetAllowedPorts("3000-4000")

	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()

	msg := bindAndExpect(t, pw, rd, 9999)
	require.Equal(t, tunnel.MsgBindErr, msg.Type)
	assert.Equal(t, tunnel.BindErrNotAllowed, msg.Code)
}

// TestTunnelControl_BindZeroGetsOSAssignedPort covers the trap the guard
// exists for: isReservedPort's `port <= 0` branch would reject the legal
// bind(0) request. The returned port must be inside the whitelist.
func TestTunnelControl_BindZeroGetsOSAssignedPort(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()

	actual := mustBind(t, pw, rd, 0)
	assert.True(t, service.ProxyService.IsPortAllowed(actual),
		"the OS-assigned port %d must satisfy the whitelist", actual)
	assert.False(t, service.ProxyService.IsPortReserved(actual))
	assert.NotEqual(t, 20000, actual)
	assert.NotEqual(t, 20001, actual)

	// The port must really be listening.
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(actual)), 2*time.Second)
	require.NoError(t, err, "the bound port must accept connections")
	_ = conn.Close()
}

// TestTunnelControl_BindZeroRejectedWhenOSPortIsOutsideRange covers the
// post-listen half of the port-0 rule: the guard lets port 0 through (it is the
// legal "OS picks" request), but the port the OS actually hands out must still
// satisfy the whitelist. Ephemeral ports are ~32768+, so a 3000-4000 whitelist
// makes the OS-assigned port invalid and the bind must be rolled back.
func TestTunnelControl_BindZeroRejectedWhenOSPortIsOutsideRange(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	service.ProxyService.SetAllowedPorts("3000-4000")

	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()

	msg := bindAndExpect(t, pw, rd, 0)
	require.Equal(t, tunnel.MsgBindErr, msg.Type,
		"the OS-assigned port must be rejected when it falls outside allowed_ports")
	assert.Equal(t, tunnel.BindErrNotAllowed, msg.Code)
	assert.Equal(t, 0, msg.Port, "a failed port-0 bind echoes the requested 0")
	assert.Equal(t, 0, tunnelBinds.Len(), "a rejected bind must not leave a reservation")
}

func TestTunnelControl_ExplicitPortIsHonored(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	want := freeLoopbackPort(t)

	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()

	got := mustBind(t, pw, rd, want)
	assert.Equal(t, want, got, "an explicit port must be honored when it is free")
}

func TestTunnelControl_DuplicateBindRejected(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	port := freeLoopbackPort(t)

	// First client binds the port.
	pw1, rd1, resp1 := openControlStream(t, h2cTunnelClient(), base)
	defer resp1.Body.Close()
	defer func() { _ = pw1.Close() }()
	require.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw1, rd1, port).Type)

	// Second client asks for the same port and must lose.
	pw2, rd2, resp2 := openControlStream(t, h2cTunnelClient(), base)
	defer resp2.Body.Close()
	defer func() { _ = pw2.Close() }()

	msg := bindAndExpect(t, pw2, rd2, port)
	require.Equal(t, tunnel.MsgBindErr, msg.Type, "the second bind of the same port must fail")
	assert.Equal(t, tunnel.BindErrReservedOrTaken, msg.Code)
}

func TestTunnelControl_DuplicateBindOnSameStreamRejected(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	port := freeLoopbackPort(t)
	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()

	require.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw, rd, port).Type)

	msg := bindAndExpect(t, pw, rd, port)
	require.Equal(t, tunnel.MsgBindErr, msg.Type, "re-binding on the same stream must fail")
	assert.Equal(t, tunnel.BindErrReservedOrTaken, msg.Code)
}

func TestTunnelControl_PortHeldByAnotherProcessIsRejected(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	held, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = held.Close() }()
	port := held.Addr().(*net.TCPAddr).Port

	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()

	msg := bindAndExpect(t, pw, rd, port)
	require.Equal(t, tunnel.MsgBindErr, msg.Type)
	assert.Equal(t, tunnel.BindErrListenFailed, msg.Code)
}

// ---------- unbind / disconnect release ----------

func TestTunnelControl_UnbindReleasesPort(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	port := freeLoopbackPort(t)
	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()

	require.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw, rd, port).Type)

	sendControl(t, pw, tunnel.ControlMessage{Type: tunnel.MsgUnbind, Port: port})
	reply := readControl(t, rd)
	require.Equal(t, tunnel.MsgUnbound, reply.Type)
	assert.Equal(t, port, reply.Port)

	waitPortFree(t, port)
}

func TestTunnelControl_UnbindUnknownPortStillAnswers(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()

	sendControl(t, pw, tunnel.ControlMessage{Type: tunnel.MsgUnbind, Port: 45678})
	reply := readControl(t, rd)
	assert.Equal(t, tunnel.MsgUnbound, reply.Type,
		"an unbind for an unknown port must still be acknowledged")
}

func TestTunnelControl_DisconnectReleasesPort(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	port := freeLoopbackPort(t)
	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	require.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw, rd, port).Type)

	// Drop the whole control stream (not just an unbind).
	_ = pw.Close()
	_ = resp.Body.Close()

	waitPortFree(t, port)
}

func TestTunnelControl_DisconnectReleasesAllPorts(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	portA := freeLoopbackPort(t)
	portB := freeLoopbackPort(t)
	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	require.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw, rd, portA).Type)
	require.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw, rd, portB).Type)

	_ = pw.Close()
	_ = resp.Body.Close()

	waitPortFree(t, portA)
	waitPortFree(t, portB)
}

// ---------- registry Active lifecycle ----------

func TestTunnelControl_RegistryActiveLifecycle(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	port := freeLoopbackPort(t)
	allocated, err := service.ProxyService.RegisterPort(port, "", "local svc", "http", model.DirectionReverse)
	require.NoError(t, err)
	require.Equal(t, port, allocated, "the free port should be allocated as the server port")
	require.False(t, isActive(allocated), "a reverse mapping starts inactive")

	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	defer resp.Body.Close()

	require.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw, rd, port).Type)
	assert.True(t, isActive(allocated), "the mapping must be Active once the client binds it")

	sendControl(t, pw, tunnel.ControlMessage{Type: tunnel.MsgUnbind, Port: port})
	require.Equal(t, tunnel.MsgUnbound, readControl(t, rd).Type)
	assert.False(t, isActive(allocated), "the mapping must be inactive after unbind")

	// Re-bind, then drop the stream: disconnect must also clear Active.
	require.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw, rd, port).Type)
	assert.True(t, isActive(allocated))

	_ = pw.Close()
	_ = resp.Body.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && isActive(allocated) {
		time.Sleep(20 * time.Millisecond)
	}
	assert.False(t, isActive(allocated), "disconnect must clear the Active flag")
}

// ---------- ping/pong ----------

func TestTunnelControl_PingPong(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()

	sendControl(t, pw, tunnel.ControlMessage{Type: tunnel.MsgPing})
	reply := readControl(t, rd)
	assert.Equal(t, tunnel.MsgPong, reply.Type)
}

func TestTunnelControl_MalformedLineDoesNotDropTheStream(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	pw, rd, resp := openControlStream(t, h2cTunnelClient(), base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()

	_, err := pw.Write([]byte("this is not json\n"))
	require.NoError(t, err)
	_, err = pw.Write([]byte(`{"type":"unknown"}` + "\n"))
	require.NoError(t, err)

	// The stream must still work after the bad lines.
	port := freeLoopbackPort(t)
	assert.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw, rd, port).Type)
}

// ---------- incoming + claim ----------

// dialBoundPort connects to a server-side bound port and reads the incoming
// message the server pushes for it.
func dialBoundPort(t *testing.T, rd *bufio.Reader, port int) (net.Conn, tunnel.ControlMessage) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	require.NoError(t, err)
	msg := readControl(t, rd)
	require.Equal(t, tunnel.MsgIncoming, msg.Type, "an accepted connection must produce incoming")
	require.Equal(t, port, msg.Port)
	require.NotEmpty(t, msg.Token, "incoming must carry a claim token")
	return conn, msg
}

// openClaimStream issues the claim data stream, mirroring the client's
// POST /api/tunnel/stream?claim=<token>.
func openClaimStream(t *testing.T, client *http.Client, base, token string) (*io.PipeWriter, *http.Response) {
	t.Helper()
	pr, pw := io.Pipe()
	req, err := http.NewRequest(http.MethodPost, base+"/api/tunnel/stream?claim="+token, pr)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := client.Do(req)
	require.NoError(t, err)
	return pw, resp
}

func TestTunnelControl_ClaimTokenIsSingleUse(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	client := h2cTunnelClient()
	port := freeLoopbackPort(t)
	pw, rd, resp := openControlStream(t, client, base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()
	require.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw, rd, port).Type)

	visitor, incoming := dialBoundPort(t, rd, port)
	defer visitor.Close()

	// First claim succeeds and holds the stream open.
	cpw1, cresp1 := openClaimStream(t, client, base, incoming.Token)
	defer cresp1.Body.Close()
	require.Equal(t, http.StatusOK, cresp1.StatusCode)

	// The same token must not be redeemable a second time.
	cpw2, cresp2 := openClaimStream(t, client, base, incoming.Token)
	defer cresp2.Body.Close()
	assert.Equal(t, http.StatusForbidden, cresp2.StatusCode, "a claim token must be single-use")

	_ = cpw1.Close()
	_ = cpw2.Close()
}

func TestTunnelControl_UnknownClaimTokenIsForbidden(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	_, resp := openClaimStream(t, h2cTunnelClient(), base, "deadbeef")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestTunnelControl_ClaimTokenIsBoundToItsConnection is the cross-connection
// guard: another client that learned the token value must not be able to steal
// the parked connection.
func TestTunnelControl_ClaimTokenIsBoundToItsConnection(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	clientA := h2cTunnelClient()
	clientB := h2cTunnelClient()

	port := freeLoopbackPort(t)
	pw, rd, resp := openControlStream(t, clientA, base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()
	require.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw, rd, port).Type)

	visitor, incoming := dialBoundPort(t, rd, port)
	defer visitor.Close()

	// Client B replays client A's token on its own connection.
	cpw, cresp := openClaimStream(t, clientB, base, incoming.Token)
	defer cresp.Body.Close()
	defer func() { _ = cpw.Close() }()
	assert.Equal(t, http.StatusForbidden, cresp.StatusCode,
		"a token must only be redeemable by the connection that minted it")
}

// TestTunnelControl_ParkedConnectionTimesOut: an accepted connection that is
// never claimed must be closed, and its token invalidated.
func TestTunnelControl_ParkedConnectionTimesOut(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	orig := tunnelClaimTimeout
	tunnelClaimTimeout = 100 * time.Millisecond
	t.Cleanup(func() { tunnelClaimTimeout = orig })

	client := h2cTunnelClient()
	port := freeLoopbackPort(t)
	pw, rd, resp := openControlStream(t, client, base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()
	require.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw, rd, port).Type)

	visitor, incoming := dialBoundPort(t, rd, port)
	defer visitor.Close()

	// Do not claim. The server must close the parked connection.
	_ = visitor.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1)
	_, err := visitor.Read(buf)
	require.Error(t, err, "the parked connection must be closed after the claim timeout")

	// The token is gone, so a late claim is refused.
	cpw, cresp := openClaimStream(t, client, base, incoming.Token)
	defer cresp.Body.Close()
	defer func() { _ = cpw.Close() }()
	assert.Equal(t, http.StatusForbidden, cresp.StatusCode, "an expired token must be rejected")
}

// TestTunnelControl_EndToEndReverseEcho is the full -R chain: server binds a
// port, an external visitor connects, the client is offered `incoming`, claims
// it, and relays to its own local target. Mirrors
// internal/ssh/server_test.go's TestSSHReverseForward_EndToEnd/assertReverseEcho.
func TestTunnelControl_EndToEndReverseEcho(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	echoPort := startTunnelEchoServer(t) // stands in for the client's local target

	client := h2cTunnelClient()
	pw, rd, resp := openControlStream(t, client, base)
	defer resp.Body.Close()
	defer func() { _ = pw.Close() }()

	serverPort := mustBind(t, pw, rd, 0)

	// The client loop: claim each incoming connection and relay it to the echo
	// target. This is what the Electron/Android client will do. Each relay runs
	// on its own goroutine (the real client uses one thread per stream), so the
	// loop can keep reading control lines. It runs off the test goroutine, so it
	// reports failures through a channel rather than calling require (t.Fatal
	// must not be called from another goroutine).
	clientErrs := make(chan error, 1)
	readerDone := make(chan struct{})
	var relays sync.WaitGroup
	go func() {
		defer close(readerDone)
		for {
			line, err := rd.ReadBytes('\n')
			if err != nil {
				return
			}
			msg, err := tunnel.DecodeControl(line)
			if err != nil || msg.Type != tunnel.MsgIncoming {
				continue
			}
			relays.Add(1)
			go func(token string) {
				defer relays.Done()
				if err := relayClaim(client, base, token, echoPort); err != nil {
					select {
					case clientErrs <- err:
					default:
					}
				}
			}(msg.Token)
		}
	}()

	assertReverseEcho(t, serverPort, "hello reverse")

	// Ending the control stream must unwind the client relay loop cleanly.
	_ = pw.Close()

	select {
	case err := <-clientErrs:
		t.Fatalf("client relay loop failed: %v", err)
	case <-readerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("client relay loop did not exit after the control stream closed")
	}
	relays.Wait()
}

// relayClaim performs one claim + relay cycle on its own goroutine and returns
// once the relay finishes.
func relayClaim(client *http.Client, base, token string, echoPort int) error {
	pr, cpw := io.Pipe()
	req, err := http.NewRequest(http.MethodPost, base+"/api/tunnel/stream?claim="+token, pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	cresp, err := client.Do(req)
	if err != nil {
		return err
	}
	if cresp.StatusCode != http.StatusOK {
		_ = cpw.Close()
		_ = cresp.Body.Close()
		return fmt.Errorf("claim returned %d", cresp.StatusCode)
	}

	upstream, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(echoPort)))
	if err != nil {
		_ = cpw.Close()
		_ = cresp.Body.Close()
		return err
	}

	go func() {
		_, _ = io.Copy(upstream, cresp.Body) // server -> client -> target
		_ = upstream.Close()
	}()
	_, _ = io.Copy(cpw, upstream) // target -> client -> server
	_ = cpw.Close()
	_ = cresp.Body.Close()
	return nil
}

// assertReverseEcho connects to the server-side bound port and verifies the
// echo round trip, proving the whole chain works.
func assertReverseEcho(t *testing.T, serverPort int, payload string) {
	t.Helper()
	var conn net.Conn
	var err error
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(serverPort)), 500*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NoError(t, err, "failed to reach server-side reverse port %d", serverPort)
	defer conn.Close()

	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = conn.Write([]byte(payload))
	require.NoError(t, err)

	buf := make([]byte, len(payload))
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)
	assert.Equal(t, payload, string(buf))
}

// TestTunnelControl_ConcurrentBindsGetDistinctPorts exercises the registry
// under concurrency: two streams binding port 0 must not collide.
func TestTunnelControl_ConcurrentBindsGetDistinctPorts(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	const streams = 4
	ports := make(chan int, streams)
	var wg sync.WaitGroup
	errs := make(chan error, streams)

	for range streams {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := h2cTunnelClient()
			pw, rd, resp := openControlStream(t, client, base)
			defer resp.Body.Close()
			defer func() { _ = pw.Close() }()

			sendControl(t, pw, tunnel.ControlMessage{Type: tunnel.MsgBind, Port: 0})
			line, err := rd.ReadBytes('\n')
			if err != nil {
				errs <- fmt.Errorf("read: %w", err)
				return
			}
			msg, err := tunnel.DecodeControl(line)
			if err != nil {
				errs <- fmt.Errorf("decode: %w", err)
				return
			}
			if msg.Type != tunnel.MsgBound {
				errs <- fmt.Errorf("bind failed: %+v", msg)
				return
			}
			ports <- msg.Port
		}()
	}

	wg.Wait()
	close(errs)
	close(ports)
	for err := range errs {
		t.Error(err)
	}

	seen := map[int]bool{}
	for p := range ports {
		if seen[p] {
			t.Errorf("two streams were assigned the same port %d", p)
		}
		seen[p] = true
	}
	assert.Len(t, seen, streams, "every stream must get a distinct port")
}

// TestTunnelControl_ClientDisconnectClosesParkedConnections guards the leak
// fix: when the control stream dies, parked (unclaimed) connections must be
// closed too, not left dangling until their claim timeout.
func TestTunnelControl_ClientDisconnectClosesParkedConnections(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	// A long claim timeout proves the close is driven by the disconnect, not
	// by expiry.
	orig := tunnelClaimTimeout
	tunnelClaimTimeout = time.Minute
	t.Cleanup(func() { tunnelClaimTimeout = orig })

	client := h2cTunnelClient()
	port := freeLoopbackPort(t)
	pw, rd, resp := openControlStream(t, client, base)
	require.Equal(t, tunnel.MsgBound, bindAndExpect(t, pw, rd, port).Type)

	visitor, _ := dialBoundPort(t, rd, port)
	defer visitor.Close()

	_ = pw.Close()
	_ = resp.Body.Close()

	_ = visitor.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, err := visitor.Read(make([]byte, 1))
	assert.Error(t, err, "a parked connection must be closed when its control stream ends")
}

// TestTunnelStream_ClaimIgnoresHostAndPort pins the branch precedence: a claim
// request must not be diverted by a stray host/port pair.
func TestTunnelStream_ClaimIgnoresHostAndPort(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	_, resp := openClaimStream(t, h2cTunnelClient(), base, "deadbeef")
	defer resp.Body.Close()
	// Still 403 from the claim branch, not a 502 from dialing a bogus target.
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestTunnelStream_NoClaimNoPortIsBadRequest keeps the T2 behavior intact.
func TestTunnelStream_NoClaimNoPortIsBadRequest(t *testing.T) {
	base, cleanup := setupTunnelControlTest(t)
	defer cleanup()

	req, err := http.NewRequest(http.MethodPost, base+"/api/tunnel/stream", bytes.NewReader(nil))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
