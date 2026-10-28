package handler

import "net/http"

// wsOriginPatterns is the origin allow-list shared by every WebSocket endpoint.
//
// It admits the server's own host plus loopback on any port (the dev server
// proxies through localhost, and the Android/desktop shells may talk to
// 127.0.0.1). A cross-origin page is refused, which is what stops a third-party
// site from opening these sockets with the user's cookies.
func wsOriginPatterns(r *http.Request) []string {
	return []string{
		"http://" + r.Host,
		"https://" + r.Host,
		"http://localhost:*",
		"https://localhost:*",
		"http://127.0.0.1:*",
		"https://127.0.0.1:*",
	}
}
