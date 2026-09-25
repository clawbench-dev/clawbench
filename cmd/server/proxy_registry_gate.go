package main

import "clawbench/internal/model"

// shouldCreateProxyRegistry reports whether main() should create the global port
// registry (service.ProxyService).
//
// The registry is no longer SSH-only. It is the shared port-whitelist,
// reserved-port and SetReverseBound surface for BOTH transports:
//
//   - the SSH tunnel server (ssh.NewServer), and
//   - the h2 stream tunnel handlers (POST /api/tunnel/stream and
//     /api/tunnel/control), which are served by the always-on main HTTP server
//     (see cmd/server/server_protocols.go).
//
// The registry is needed whenever either transport could carry traffic:
//
//	transport   | SSH enabled | registry needed | why
//	------------+-------------+-----------------+-----------------------------
//	ssh         | true        | yes             | SSH tunnel needs it
//	ssh         | false       | NO              | no SSH listener, no h2
//	h2 / both   | either      | yes             | h2 rides the main HTTP server
//	(bad value) | either      | yes             | ApplyDefaults converges it to
//	            |             |                 | "both", so treat it as h2-capable
//
// The single "no" cell is `transport: "ssh"` with PortForward.Enabled == false:
// the SSH listener is off and h2 is explicitly excluded, so nothing can reach
// the registry and creating it would only start a health-check goroutine and
// restore port rows for a dead feature.
//
// Everything else returns true. In particular the zero-value config — an empty
// Transport with Enabled false, which is what a caller that has not run
// ApplyDefaults would pass — is treated as h2-capable and therefore true: the
// old gate (`cfg.PortForward.Enabled` alone) made port_forward.enabled=false
// hand every tunnel request a nil registry and a 503
// (internal/handler/tunnel_stream.go:59, tunnel_control.go:297). Do not
// reintroduce it.
//
// Creating the registry is side-effect-free with respect to SSH: it starts only
// a 5s health-check goroutine and restores persisted port rows from the
// database. It binds no port — listening remains ssh.NewServer's job, still
// gated on PortForward.Enabled.
func shouldCreateProxyRegistry(cfg model.Config) bool {
	// h2 is available unless the operator explicitly confined the tunnel to SSH.
	h2Available := cfg.PortForward.Transport != model.TransportSSH
	return cfg.PortForward.Enabled || h2Available
}

// reservedPortsFor returns the ports a reverse mapping must never bind on the
// server: ClawBench's own HTTP port, plus the SSH port when SSH is enabled.
//
// sshPort == 0 means "no SSH port to protect" (SSH disabled, or not yet
// resolved). It is deliberately NOT expanded to mainPort+1 here: with SSH off
// nothing listens on mainPort+1, and the h2 tunnel's own guard
// (internal/handler/tunnel_control.go tunnelGuard) already hard-denies
// model.ServerPort+1. SetReservedPorts drops any p <= 0, so passing 0 through
// is safe (internal/service/proxy.go:173-181).
//
// mainPort is never dropped. Binding 20000 in reverse would let a client take
// down the very server the tunnel rides on, so it stays reserved even in
// h2-only deployments.
func reservedPortsFor(mainPort, sshPort int) []int {
	ports := []int{mainPort}
	if sshPort > 0 {
		ports = append(ports, sshPort)
	}
	return ports
}
