package handler

// Reverse proxy for the relay's signaling endpoint.
//
// A page served over https cannot open a ws:// socket — the browser refuses it
// as mixed content — while LiveKit only offers ws:// on a plain deployment.
// The page therefore asks for /livekit/rtc on the ClawBench origin and this
// handler forwards it to the relay, so the socket inherits the page's scheme.
//
// It is deliberately NOT a general-purpose forwarder. The only address it will
// ever dial is 127.0.0.1:<the configured port>, and only when the configured
// relay host is itself a loopback name; anything else is a 404. A remote relay
// needs no proxy (its own wss:// endpoint is reachable directly), so treating
// it as one would only turn this endpoint into a way to reach the rest of the
// network from the server.
//
// Being public is intentional and matches the viewer socket: the LiveKit token
// is the credential, and an anonymous caller reaching LiveKit's API surface
// through here still has none. Only the signaling handshake is anonymous.

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"clawbench/internal/service"
)

// Loopback relay schemes. Named because the literals appear throughout the
// handler package and a bare "http" here reads as a URL, not as a scheme.
const (
	relayProxySchemeHTTP  = "http"
	relayProxySchemeHTTPS = "https"
)

// liveKitProxyPrefix is the public subtree that mirrors LiveKit's own paths:
// /livekit/rtc → /rtc, /livekit/rtc/validate → /rtc/validate.
const liveKitProxyPrefix = "/livekit"

// ServeLiveKitProxy forwards /livekit/* to the loopback relay.
//
// 404 (not 502) when no usable relay is configured: to a browser that means
// "this endpoint does not exist on this deployment", which is exactly true, and
// it keeps the existence of an internal service from leaking to anonymous
// callers.
func ServeLiveKitProxy(w http.ResponseWriter, r *http.Request) {
	target, ok := liveKitRelayTarget()
	if !ok {
		http.NotFound(w, r)
		return
	}
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = liveKitUpstreamPath(req.URL.Path)
			req.Host = target.Host
		},
		// -1 flushes every write immediately. Signaling is a long-lived
		// bidirectional stream (and an upgrade handshake before that);
		// buffering would hold the handshake and the trickle of ICE
		// candidates behind a full buffer.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			slog.Warn("liveshare: relay proxy failed", "err", err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

// liveKitUpstreamPath strips the public prefix. The relay's signaling endpoint
// is /rtc, so /livekit/rtc must arrive as /rtc — forwarding the prefix would
// hit LiveKit's 404 and the page would sit on "connecting" forever.
func liveKitUpstreamPath(p string) string {
	p = strings.TrimPrefix(p, liveKitProxyPrefix)
	if p == "" || p[0] != '/' {
		p = "/" + p
	}
	return p
}

// liveKitRelayTarget resolves the proxy destination, or reports that there is
// none.
//
// Only a loopback host is accepted, and the address dialed is always the
// literal 127.0.0.1 — never the configured host name. That way a config edit
// (or a poisoned row) cannot point this endpoint at an arbitrary machine.
func liveKitRelayTarget() (*url.URL, bool) {
	cfg, err := service.GetLiveSFUConfig()
	if err != nil || cfg.URL == "" {
		return nil, false
	}
	u, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil || u.Host == "" {
		return nil, false
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
	default:
		return nil, false
	}

	// The dialed scheme follows the configured one: a ws:// relay answers plain
	// HTTP on the loopback, a wss:// relay terminates TLS there too.
	scheme := relayProxySchemeHTTP
	port := u.Port()
	switch u.Scheme {
	case "ws":
		if port == "" {
			port = "80"
		}
	case "wss":
		scheme = relayProxySchemeHTTPS
		if port == "" {
			port = "443"
		}
	default:
		return nil, false
	}
	return &url.URL{Scheme: scheme, Host: "127.0.0.1:" + port}, true
}
