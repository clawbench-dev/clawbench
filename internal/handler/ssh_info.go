//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"clawbench/internal/service"
	"clawbench/internal/ssh"
)

// sshServerRef holds a reference to the SSH server, set from main.go.
var (
	sshServerMu  sync.RWMutex
	sshServerRef *ssh.Server
)

// SetSSHServer stores a reference to the SSH server for handler access.
func SetSSHServer(s *ssh.Server) {
	sshServerMu.Lock()
	sshServerRef = s
	sshServerMu.Unlock()
}

// GetSSHServer returns the SSH server reference (for hot-reload).
func GetSSHServer() *ssh.Server {
	sshServerMu.RLock()
	s := sshServerRef
	sshServerMu.RUnlock()
	return s
}

// ServeSSHInfo returns the minimal SSH info an unauthenticated client needs to
// discover the tunnel port.
// GET /api/ssh/info
//
// Deliberately public, mirroring the /api/frp/status split: Android's
// BackgroundService.fetchSSHPort() calls this from native Java with no cookie
// to learn the port before it can connect. It only reports whether SSH is on
// and which port to dial.
//
// Everything else — the host key fingerprint, the username, the generated
// `ssh -L` command, and connection stats — is NOT here. The command in
// particular enumerates every forwarded local port and its internal target
// host, which is a map of the operator's internal network and has no business
// being served to anonymous callers. Those fields moved to the authenticated
// ServeSSHInfoFull.
func ServeSSHInfo(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	sshRef := GetSSHServer()
	if sshRef == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false,
			"port":    0,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": true,
		"port":    sshRef.Port(),
	})
}

// ServeSSHInfoFull returns the complete SSH tunnel setup payload for the
// authenticated web UI (ProxyPanelContent shows the command and fingerprint;
// usePortForward polls connection stats).
// GET /api/ssh/info/full
func ServeSSHInfoFull(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	sshRef := GetSSHServer()

	if sshRef == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":         false,
			"host":            "",
			"port":            0,
			"username":        "",
			"fingerprint":     "",
			"command":         "",
			"connectionStats": nil,
		})
		return
	}

	// Determine the server host from the request Host header
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	port := sshRef.Port()
	fingerprint := sshRef.Fingerprint()

	// Build the ssh command from all registered ports.
	//
	// Forward mappings use -L: the client listens locally and the server dials
	// the target. For non-localhost targets with an active reverse proxy, the
	// tunnel must route through the reverse proxy (127.0.0.1:{localPort})
	// instead of directly to the remote host, so that the Host header is
	// rewritten correctly. If the reverse proxy failed to start (e.g., port in
	// use), fall back to direct connection — the Host header will be wrong, but
	// at least the connection works.
	//
	// Reverse mappings use -R: the server binds the port and hands connections
	// back to the client, which dials the target on its own machine.
	var forwardArgs []string
	if service.ProxyService != nil {
		ports := service.ProxyService.ListPorts()
		for _, p := range ports {
			if p.IsReverse() {
				targetHost := p.Host
				if targetHost == "" {
					targetHost = "localhost"
				}
				forwardArgs = append(forwardArgs, fmt.Sprintf("-R %d:%s:%d", p.LocalPort, targetHost, p.Port))
				continue
			}
			if service.ProxyService.HasReverseProxy(p.LocalPort) {
				forwardArgs = append(forwardArgs, fmt.Sprintf("-L %d:127.0.0.1:%d", p.LocalPort, p.LocalPort))
			} else {
				targetHost := p.Host
				if targetHost == "" {
					targetHost = "localhost"
				}
				forwardArgs = append(forwardArgs, fmt.Sprintf("-L %d:%s:%d", p.LocalPort, targetHost, p.Port))
			}
		}
	}

	command := ""
	if len(forwardArgs) > 0 {
		command = fmt.Sprintf("ssh -N %s clawbench@%s -p %d",
			strings.Join(forwardArgs, " "), host, port)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":         true,
		"host":            host,
		"port":            port,
		"username":        "clawbench",
		"fingerprint":     fingerprint,
		"command":         command,
		"connectionStats": sshRef.ConnectionStats(),
	})
}
