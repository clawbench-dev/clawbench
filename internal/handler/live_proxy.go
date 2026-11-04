package handler

// Reverse proxy for the relay's signaling endpoint.
//
// This is the ONLY way a page reaches the relay. Two reasons:
//
//   - A page served over https cannot open a ws:// socket — the browser refuses
//     it as mixed content — while the relay is deployed plain (no certificate).
//     Asking for /livekit/rtc on the ClawBench origin makes the socket inherit
//     the page's scheme.
//   - The relay therefore never needs TLS, and no certificate has to be trusted
//     by every viewer's browser. The hop from here to the relay stays plain.
//
// Media is not involved: WebRTC carries the picture and sound directly between
// the browser and LiveKit. What crosses this proxy is the signaling handshake
// and its trickle of candidates — a few KB per participant.
//
// It is deliberately NOT a general-purpose forwarder. The only address it will
// ever dial is the relay configured for this installation, and never one taken
// from the request. A loopback name is normalized to 127.0.0.1 so a config edit
// (or a poisoned row) cannot point this endpoint at another local service.
//
// Being public is intentional and matches the viewer socket: the LiveKit token
// is the credential, and an anonymous caller reaching LiveKit's API surface
// through here still has none. Only the signaling handshake is anonymous.

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"clawbench/internal/service"
)

// Relay proxy schemes. Named because the literals appear throughout the
// handler package and a bare "http" here reads as a URL, not as a scheme.
const (
	relayProxySchemeHTTP  = "http"
	relayProxySchemeHTTPS = "https"
)

// liveKitProxyPrefix is the public subtree that mirrors LiveKit's own paths:
// /livekit/rtc → /rtc, /livekit/rtc/validate → /rtc/validate.
const liveKitProxyPrefix = "/livekit"

// ServeLiveKitProxy forwards /livekit/* to the configured relay.
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
// Any configured host is accepted — a relay on another machine is the normal
// deployment, and the page has no other way to reach it over plain ws. The
// boundary is "exactly the configured relay", never an address derived from the
// request.
func liveKitRelayTarget() (*url.URL, bool) {
	cfg, err := service.GetLiveSFUConfig()
	if err != nil || cfg.URL == "" {
		return nil, false
	}
	u, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil || u.Host == "" {
		return nil, false
	}

	// The dialed scheme follows the configured one: ws:// answers plain HTTP,
	// wss:// terminates TLS. New deployments use ws:// — the relay needs no
	// certificate — but a stored wss:// endpoint still works through here.
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

	host := u.Hostname()
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1":
		host = "127.0.0.1"
	}
	return &url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port)}, true
}
