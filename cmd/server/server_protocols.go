package main

import "net/http"

// serverProtocols returns the HTTP protocols the main server accepts.
//
// http.Protocols is all-or-nothing: the moment it is non-nil it fully replaces
// the defaults (net/http/server.go protocols() only fills in HTTP1 when
// s.Protocols == nil), so every protocol we still serve must be enabled
// explicitly.
//
// HTTP/1.1 is not optional here — the five coder/websocket endpoints
// (/api/ai/events/ws, /api/terminal/ws, /api/file/watch/ws, /api/tts/audio/ws,
// /api/stt/transcribe/ws) upgrade via http.Hijacker, which HTTP/2 does not
// support. Dropping SetHTTP1 turns them all into ECONNRESET.
//
// h2-over-TLS is likewise opt-in once Protocols is non-nil: ServeTLS computes
// its ALPN list from s.protocols(), so SetHTTP2(true) must be set explicitly
// or the TLS listener stops advertising "h2".
//
// tlsEnabled selects the encrypted variant: plaintext deployments get h2c
// (prior-knowledge), TLS deployments get h2 over ALPN. The two are mutually
// exclusive because UnencryptedHTTP2 is only meaningful without TLS.
func serverProtocols(tlsEnabled bool) *http.Protocols {
	p := new(http.Protocols)
	p.SetHTTP1(true) // required for the existing WebSocket endpoints
	if tlsEnabled {
		p.SetHTTP2(true) // ALPN "h2" when served via ServeTLS
	} else {
		p.SetUnencryptedHTTP2(true) // h2c prior-knowledge on the plaintext listener
	}
	return p
}
