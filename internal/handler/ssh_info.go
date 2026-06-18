//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"clawbench/internal/service"
	"clawbench/internal/ssh"
)

// sshServerRef holds a reference to the SSH server, set from main.go.
var sshServerRef *ssh.Server

// SetSSHServer stores a reference to the SSH server for handler access.
func SetSSHServer(s *ssh.Server) {
	sshServerRef = s
}

// ServeSSHInfo returns SSH connection info for tunnel setup.
// GET /api/ssh/info
func ServeSSHInfo(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if sshServerRef == nil {
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

	port := sshServerRef.Port()
	fingerprint := sshServerRef.Fingerprint()

	// Build ssh -L command from all registered ports.
	// For non-localhost targets with a reverse proxy, the SSH tunnel must route
	// through the reverse proxy (127.0.0.1:{localPort}) instead of directly to
	// the remote host, so that the Host header is rewritten correctly.
	var forwardArgs []string
	if service.ProxyService != nil {
		ports := service.ProxyService.ListPorts()
		for _, p := range ports {
			if p.HasReverseProxy {
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
		command = fmt.Sprintf("ssh %s clawbench@%s -p %d",
			strings.Join(forwardArgs, " "), host, port)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":         true,
		"host":            host,
		"port":            port,
		"username":        "clawbench",
		"fingerprint":     fingerprint,
		"command":         command,
		"connectionStats": sshServerRef.ConnectionStats(),
	})
}
