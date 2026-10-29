package handler

import (
	"net"
	"net/http"
	"strconv"

	"clawbench/internal/platform"
)

// ServeServerAddresses handles GET /api/server/addresses.
//
// Returns every address this server can be reached on, each labeled with its
// OS interface and a kind (lan / virtual / public). The share dialogs build
// their links from this instead of `window.location.origin`, which only knows
// the ONE address the browser happened to use — on a machine with Wi-Fi, an
// Ethernet adapter and a Docker bridge, a colleague handed that link may be on
// a different subnet and unable to open it.
//
// Addresses are NOT filtered: a virtual adapter is still listed, just labeled,
// because silently dropping it leaves the user unable to explain why a
// particular address does not work for a colleague.
//
// The port comes from the request rather than a package variable so a server
// listening on several ports (or behind a port-forward) reports the one the
// caller actually reached.
func ServeServerAddresses(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	// Prefer the Host header: it is what the client actually used, so it stays
	// correct behind a port-forward or when the server listens on several ports.
	_, port, err := net.SplitHostPort(r.Host)
	if err != nil || port == "" {
		port = "0"
		// Fall back to the listening socket. A type assertion here would panic
		// on a Unix socket or a test recorder, so it is guarded.
		if la, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
			if tcp, ok := la.(*net.TCPAddr); ok {
				port = strconv.Itoa(tcp.Port)
			}
		}
	}
	const (
		keyPort      = "port"
		keyAddresses = "addresses"
	)
	writeJSON(w, http.StatusOK, map[string]any{
		keyPort:      port,
		keyAddresses: platform.GetLocalAddresses(),
	})
}
