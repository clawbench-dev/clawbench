package handler

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"clawbench/internal/service"
	"clawbench/internal/tunnel"
)

// tunnelDialTimeout bounds the dial to the forwarded target. The target is
// usually loopback, but a hostname may need DNS and a remote host may need a
// handshake; a connect that never completes must not pin the h2 stream open
// forever. The dial finishes before any response header is written, so this
// timeout maps cleanly onto 502.
const tunnelDialTimeout = 10 * time.Second

// TunnelStream handles POST /api/tunnel/stream — the -L (forward) data plane
// of the HTTP/2 stream tunnel.
//
// Protocol: the request body carries client -> server bytes and the response
// body carries server -> client bytes, both streaming simultaneously. One
// HTTP/2 stream == one forwarded TCP connection; the client opens a stream per
// accepted local connection, so the whole tunnel needs only the main port.
//
// Auth runs in middleware.Auth before this handler (registered via `register`,
// not `registerPublic`).
//
// Validation order is load-bearing: parameters and the port whitelist are
// checked BEFORE dialing, and a dial failure is reported BEFORE any response
// header is written. Once WriteHeader(200) goes out the status code is frozen,
// so a late failure could only be signaled by truncating the body — which the
// client would see as a successful-but-empty stream.
//
// Error mapping (documented in internal/api/openapi.yaml):
//   - 400 target parameters missing/invalid
//   - 401 handled by middleware.Auth
//   - 403 target port outside the configured allowed range
//   - 502 target unreachable (dial failed)
//   - 503 no port registry configured (no forwarding enabled at all)
func TunnelStream(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Nil guard, mirroring internal/handler/ssh_info.go:117. Without a
	// registry there is no whitelist to enforce, so every stream must be
	// refused rather than silently allowed.
	if service.ProxyService == nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "PortForwardUnavailable")
		return
	}

	target, err := tunnel.ParseTarget(r.URL.Query().Get("host"), r.URL.Query().Get("port"))
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPortInQuery")
		return
	}

	// Only the whitelist is consulted — no IsPortRegistered, no
	// IsNonLocalhostTarget, no reverse proxy. This is the SSH forward path's
	// semantics (internal/ssh/server.go:738).
	guard := tunnel.PortGuard{IsAllowed: service.ProxyService.IsPortAllowed}
	if !guard.ForwardAllowed(target.Port) {
		slog.Debug("tunnel: port not allowed", slog.Int("port", target.Port))
		writeLocalizedErrorf(w, r, http.StatusForbidden, "AccessDenied")
		return
	}

	// Dial before writing any header so a failure can still be a 502.
	//
	// The target host/port come from the request, which makes this an
	// intentionally caller-directed dial (an SSRF surface by construction).
	// That is the endpoint's entire purpose — it is an authenticated forward
	// tunnel, exactly like the SSH direct-tcpip path — and the port is
	// constrained by the allowed_ports whitelist above. The host is
	// deliberately unconstrained: the SSH path accepts any host too, and the
	// server's own network position is the real boundary.
	dialer := &net.Dialer{Timeout: tunnelDialTimeout}
	backend, err := dialer.DialContext(r.Context(), "tcp", target.Addr())
	if err != nil {
		slog.Debug("tunnel: backend unreachable",
			slog.String("target", target.Addr()), slog.String("err", err.Error()))
		writeLocalizedErrorf(w, r, http.StatusBadGateway, "TunnelTargetUnreachable")
		return
	}
	defer func() { _ = backend.Close() }()

	// Release the target when the client goes away, so the response pump
	// unblocks instead of leaking a goroutine and a socket per abandoned
	// stream. AfterFunc returns a stop func that must be called to avoid
	// leaking the registration when the handler exits normally.
	stop := context.AfterFunc(r.Context(), func() { _ = backend.Close() })
	defer stop()

	rc := http.NewResponseController(w)

	// h1 fallback only: without this net/http consumes the unread request body
	// before the handler may write, which deadlocks a duplex stream
	// (net/http/server.go:1392). On h2 it is a documented no-op
	// (net/http/h2_bundle.go:6856).
	if err := rc.EnableFullDuplex(); err != nil {
		slog.Debug("tunnel: EnableFullDuplex unsupported", slog.String("err", err.Error()))
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	// Flush the 200 immediately: the client waits for response headers before
	// it starts pumping the request body in some transports.
	if err := rc.Flush(); err != nil {
		return
	}

	slog.Debug("tunnel: relaying", slog.String("target", target.Addr()))

	// client -> target, target -> client, flushing every chunk.
	tunnel.RelayDuplex(backend, r.Body, w, rc.Flush)
}
