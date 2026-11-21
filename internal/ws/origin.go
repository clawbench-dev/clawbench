package ws

import "net/http"

// OriginPatterns returns the WebSocket origin patterns accepted for a request.
//
// It used to live in internal/handler/ws_origin.go. Meeting (live) and file
// watch both need it, and the meeting plugin cannot import handler.
func OriginPatterns(r *http.Request) []string {
	return []string{
		"http://" + r.Host,
		"https://" + r.Host,
		"http://localhost:*",
		"https://localhost:*",
		"http://127.0.0.1:*",
		"https://127.0.0.1:*",
	}
}
