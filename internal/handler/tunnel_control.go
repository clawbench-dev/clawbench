package handler

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/tunnel"
)

// tunnelControlWriteTimeout bounds a single control-line write. The client is
// expected to consume control lines promptly (they are rare and tiny); a client
// that stalls long enough to blow this has gone away, and holding the bind's
// accept loop open for it would let one dead peer back up every reverse port.
const tunnelControlWriteTimeout = 10 * time.Second

// tunnelClaimTimeout bounds how long an accepted reverse connection waits to be
// claimed before it is closed. A var so tests can shorten it.
var tunnelClaimTimeout = tunnel.DefaultClaimTimeout

// Reverse-tunnel registries. They are process-wide because a reverse bind
// outlives the request that created it and a claim arrives on a different HTTP
// request than its control stream, so no single handler invocation can own the
// state. They are package variables (not tunnel package globals) so tests can
// swap in isolated instances instead of mutating state shared across cases.
var (
	// tunnelBinds is the reverse-bind table: which ports are listening for
	// which authenticated connection, and the listeners themselves.
	tunnelBinds = tunnel.NewBindRegistry()
	// tunnelClaims parks accepted reverse connections until the owning client
	// redeems their single-use token.
	tunnelClaims = tunnel.NewClaimRegistry()
)

// controlSession owns the state one POST /api/tunnel/control connection
// created: the write side of its control stream and the identity its claims are
// scoped to. The listeners and parked connections live in the process-wide
// registries (see tunnelBinds / tunnelClaims) because they must be reachable
// from the claim request.
type controlSession struct {
	binding tunnel.Binding

	// w is the response writer; rc is its controller, used to flush after each
	// line (net/http and the h2 framer both buffer) and to bound each write.
	w  io.Writer
	rc *http.ResponseController

	// writeMu serializes control-line writes: the reader goroutine answers
	// `ping` while an accept goroutine pushes `incoming`, and interleaved
	// writes would corrupt the NDJSON stream.
	writeMu sync.Mutex

	// wg tracks the per-listener accept loops so cleanup can wait for them to
	// stop touching shared state.
	wg sync.WaitGroup

	// closeOnce makes Close idempotent: the normal exit path, the deferred
	// cleanup and the context watcher may all race to tear down.
	closeOnce sync.Once
}

// newControlSession builds a session around an established control stream.
func newControlSession(binding tunnel.Binding, w io.Writer, rc *http.ResponseController) *controlSession {
	return &controlSession{binding: binding, w: w, rc: rc}
}

// write sends one control message, serialized against other writers and bounded
// by tunnelControlWriteTimeout so a stalled client cannot block a goroutine
// forever. The deadline is cleared afterwards: the stream is long-lived and
// must not carry a deadline into its next write.
//
// Flushing is mandatory, not an optimization: without it a `bound` or
// `incoming` line sits in net/http's (or the h2 framer's) buffer while the
// client waits for it, which is a deadlock, not a delay.
func (s *controlSession) write(msg tunnel.ControlMessage) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.rc != nil {
		_ = s.rc.SetWriteDeadline(time.Now().Add(tunnelControlWriteTimeout))
		defer func() { _ = s.rc.SetWriteDeadline(time.Time{}) }()
	}

	if _, err := s.w.Write(tunnel.EncodeControl(msg)); err != nil {
		return err
	}
	if s.rc != nil {
		return s.rc.Flush()
	}
	return nil
}

// handleBind validates and establishes a reverse listener for port.
func (s *controlSession) handleBind(port int) {
	ln, actual, err := tunnel.ListenReverse(tunnelGuard(), tunnelBinds, tunnel.BindRequest{
		Port:    port,
		Binding: s.binding,
	})
	if err != nil {
		code := bindErrCode(err)
		slog.Debug("tunnel: reverse bind rejected",
			slog.Int("port", port), slog.Int("code", code), slog.String("err", err.Error()))
		if werr := s.write(tunnel.ControlMessage{
			Type: tunnel.MsgBindErr, Port: port, Code: code, Msg: err.Error(),
		}); werr != nil {
			slog.Debug("tunnel: failed to report bind_err", slog.String("err", werr.Error()))
		}
		return
	}

	// Drive the registry's Active flag, exactly as the SSH server does after a
	// successful tcpip-forward (internal/ssh/server.go:534). Unknown ports are
	// ignored by SetReverseBound, so a manual bind for an unregistered port
	// still works.
	if service.ProxyService != nil {
		service.ProxyService.SetReverseBound(actual, true)
	}

	slog.Info("tunnel: reverse bind established", slog.Int("port", actual))
	if err := s.write(tunnel.ControlMessage{Type: tunnel.MsgBound, Port: actual}); err != nil {
		slog.Debug("tunnel: failed to report bound", slog.String("err", err.Error()))
		// The client never learned the port, so the bind is unusable. Release
		// it rather than leaving a listener nobody will ever unbind.
		releaseReverseBind(s.binding, actual)
		return
	}

	s.wg.Add(1)
	go s.acceptLoop(ln, actual)
}

// handleUnbind releases a previously bound port.
func (s *controlSession) handleUnbind(port int) {
	if !releaseReverseBind(s.binding, port) {
		// Unknown or already-released port: still answer so the client's
		// bookkeeping completes rather than retrying forever.
		slog.Debug("tunnel: unbind for unknown port", slog.Int("port", port))
	}
	if err := s.write(tunnel.ControlMessage{Type: tunnel.MsgUnbound, Port: port}); err != nil {
		slog.Debug("tunnel: failed to report unbound", slog.String("err", err.Error()))
	}
}

// acceptLoop parks every accepted connection and offers it to the client. It
// returns when the listener is closed (unbind, session end, or error).
func (s *controlSession) acceptLoop(ln net.Listener, port int) {
	defer s.wg.Done()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		s.park(conn, port)
	}
}

// park hands one accepted connection to the client: it is held (not relayed)
// until the client redeems the token via POST /api/tunnel/stream?claim=...
func (s *controlSession) park(conn net.Conn, port int) {
	token, err := tunnelClaims.Park(s.binding, port, conn, tunnelClaimTimeout)
	if err != nil {
		slog.Warn("tunnel: failed to mint claim token", slog.String("err", err.Error()))
		_ = conn.Close()
		return
	}

	// Deliberately no deadline is set on conn: it is about to be relayed, and
	// any deadline would fire mid-transfer and kill a healthy connection. The
	// parked connection's own lifetime is bounded by the claim timeout instead.
	if err := s.write(tunnel.ControlMessage{Type: tunnel.MsgIncoming, Port: port, Token: token}); err != nil {
		slog.Debug("tunnel: failed to send incoming", slog.String("err", err.Error()))
		tunnelClaims.Discard(token)
		return
	}
	slog.Debug("tunnel: connection parked awaiting claim", slog.Int("port", port))
}

// Close releases every listener, parked connection and reservation owned by
// this session and clears the registry Active flags. Safe to call repeatedly.
func (s *controlSession) Close() {
	s.closeOnce.Do(func() {
		for _, port := range tunnelBinds.ReleaseBinding(s.binding) {
			if service.ProxyService != nil {
				service.ProxyService.SetReverseBound(port, false)
			}
		}
		tunnelClaims.ReleaseBinding(s.binding)

		s.wg.Wait()
		slog.Info("tunnel: control session closed")
	})
}

// releaseReverseBind closes a port's listener, discards its parked connections
// and clears its Active flag. It reports whether this binding owned the port.
func releaseReverseBind(binding tunnel.Binding, port int) bool {
	if !tunnelBinds.Release(binding, port) {
		return false
	}
	tunnelClaims.ReleasePort(binding, port)
	if service.ProxyService != nil {
		service.ProxyService.SetReverseBound(port, false)
	}
	slog.Info("tunnel: reverse bind released", slog.Int("port", port))
	return true
}

// bindErrCode maps a bind failure onto the bind_err.code documented in design
// doc §4.6.
func bindErrCode(err error) int {
	switch {
	case errors.Is(err, tunnel.ErrBindNotAllowed):
		return tunnel.BindErrNotAllowed
	case errors.Is(err, tunnel.ErrBindReservedOrTaken):
		return tunnel.BindErrReservedOrTaken
	case errors.Is(err, tunnel.ErrBindListenFailed):
		return tunnel.BindErrListenFailed
	default:
		return tunnel.BindErrInternal
	}
}

// tunnelBinding derives the identity a claim token is scoped to.
//
// ConnID is the underlying TCP connection, which for HTTP/2 is the whole h2
// session: every stream on it shares the RemoteAddr, and the claim stream must
// ride the same connection as its control stream (design doc §4.5). AuthID is
// the session cookie, so even a connection-identifier collision (two clients
// behind one NAT) cannot let one client redeem another's token. It is empty
// when authentication is disabled, which is why ConnID is also required.
func tunnelBinding(r *http.Request) tunnel.Binding {
	authID := ""
	if c, err := r.Cookie(model.ScopedCookieName(model.SessionCookie)); err == nil && c != nil {
		authID = c.Value
	}
	return tunnel.Binding{AuthID: authID, ConnID: r.RemoteAddr}
}

// tunnelGuard builds the SSH-equivalent port guard from the live registry.
//
// MainPort comes from model.ServerPort (set once at startup, before listeners
// bind) and SSHPort mirrors the SSH server's own default of mainPort+1. The
// registry's reserved set is consulted as well, matching
// internal/ssh/server.go:616-624 branch for branch.
func tunnelGuard() tunnel.PortGuard {
	g := tunnel.PortGuard{
		MainPort: model.ServerPort,
		SSHPort:  model.ServerPort + 1,
	}
	if service.ProxyService != nil {
		g.IsAllowed = service.ProxyService.IsPortAllowed
		g.IsReserved = service.ProxyService.IsPortReserved
	}
	return g
}

// TunnelControl handles POST /api/tunnel/control — the -R (reverse) control
// plane of the HTTP/2 stream tunnel.
//
// HTTP/2 cannot open a stream from the server side (RFC 9113 §8.4: a pushed
// stream is half-closed(local) on the client and therefore unwritable), so a
// reverse forward cannot be dialed by the server. Instead the client keeps this
// long-lived duplex stream open and sends NDJSON commands on it; when a
// connection arrives on a bound port the server parks it and sends `incoming`
// with a single-use token, which the client redeems by opening a
// POST /api/tunnel/stream?claim=<token> data stream.
//
// Request body = client -> server control lines; response body = server ->
// client control lines. Both directions stream for the life of the tunnel.
//
// Auth runs in middleware.Auth before this handler (registered via `register`).
//
// Error mapping (documented in internal/api/openapi.yaml):
//   - 401 handled by middleware.Auth
//   - 503 no port registry configured (no forwarding enabled at all)
//
// Per-command failures (reserved port, duplicate bind, listen failure) are NOT
// HTTP errors: the stream stays open and the client gets a bind_err line, so
// one bad command does not tear down the whole tunnel.
func TunnelControl(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Nil guard, mirroring internal/handler/ssh_info.go:117. Without a
	// registry there is no whitelist and no way to flag a port Active, so every
	// bind must be refused rather than silently allowed.
	if service.ProxyService == nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "PortForwardUnavailable")
		return
	}

	rc := http.NewResponseController(w)
	// h1 fallback only: without this net/http consumes the unread request body
	// before the handler may write (net/http/server.go:1392). On h2 it is a
	// documented no-op (net/http/h2_bundle.go:6856).
	if err := rc.EnableFullDuplex(); err != nil {
		slog.Debug("tunnel: EnableFullDuplex unsupported on control stream", slog.String("err", err.Error()))
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	// Flush the 200 immediately: the client waits for response headers before
	// it starts writing commands.
	if err := rc.Flush(); err != nil {
		return
	}

	session := newControlSession(tunnelBinding(r), w, rc)

	// If the client disappears, unblock the reader and release everything. The
	// AfterFunc stop must be called on normal exit to avoid leaking the
	// registration.
	stop := context.AfterFunc(r.Context(), func() { session.Close() })
	defer stop()
	defer session.Close()

	slog.Info("tunnel: control stream opened")
	readControlLoop(session, r)
	slog.Info("tunnel: control stream ended")
}

// readControlLoop consumes NDJSON control lines until EOF or an unrecoverable
// error. Malformed lines are skipped rather than fatal: a single bad byte must
// not drop every reverse port the client has established.
func readControlLoop(s *controlSession, r *http.Request) {
	scanner := bufio.NewScanner(r.Body)
	scanner.Buffer(make([]byte, 0, 4096), 64*1024)

	for scanner.Scan() {
		msg, err := tunnel.DecodeControl(scanner.Bytes())
		if err != nil {
			slog.Debug("tunnel: dropping malformed control line", slog.String("err", err.Error()))
			continue
		}
		if !dispatchControl(s, msg) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Debug("tunnel: control stream read error", slog.String("err", err.Error()))
	}
}

// dispatchControl handles one control message, returning false when the loop
// must end.
func dispatchControl(s *controlSession, msg tunnel.ControlMessage) bool {
	switch msg.Type {
	case tunnel.MsgBind:
		s.handleBind(msg.Port)
	case tunnel.MsgUnbind:
		s.handleUnbind(msg.Port)
	case tunnel.MsgPing:
		if err := s.write(tunnel.ControlMessage{Type: tunnel.MsgPong}); err != nil {
			return false
		}
	case tunnel.MsgBound, tunnel.MsgBindErr, tunnel.MsgIncoming, tunnel.MsgUnbound, tunnel.MsgPong:
		// Server -> client types arriving from the client are protocol
		// violations; ignore them rather than trusting their payload.
		slog.Debug("tunnel: ignoring server-to-client message from client", slog.String("type", msg.Type))
	default:
		// DecodeControl rejects unknown types, so this is unreachable.
	}
	return true
}

// claimStream hands a parked reverse connection to the claiming data stream.
//
// It is the `claim` branch of TunnelStream. The token is single-use and scoped
// to the connection that minted it (see tunnel.Binding), so this call is the
// only way to reach a parked socket and it works exactly once.
func claimStream(w http.ResponseWriter, r *http.Request, token string) {
	pc, err := tunnelClaims.ClaimConn(token, tunnelBinding(r))
	if err != nil {
		slog.Debug("tunnel: claim rejected")
		writeLocalizedErrorf(w, r, http.StatusForbidden, "AccessDenied")
		return
	}
	defer func() { _ = pc.Conn.Close() }()

	rc := http.NewResponseController(w)
	if err := rc.EnableFullDuplex(); err != nil {
		slog.Debug("tunnel: EnableFullDuplex unsupported on claim stream", slog.String("err", err.Error()))
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return
	}

	// Release the parked connection when the client goes away so the relay
	// unblocks instead of leaking a socket per abandoned stream.
	stop := context.AfterFunc(r.Context(), func() { _ = pc.Conn.Close() })
	defer stop()

	slog.Debug("tunnel: claim stream relaying", slog.Int("port", pc.Port))
	tunnel.RelayDuplex(pc.Conn, r.Body, w, rc.Flush)
}
