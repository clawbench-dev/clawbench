package main

import "clawbench/internal/model"

// shouldCreateProxyRegistry reports whether main() should create the global port
// registry (service.ProxyService).
//
// It is unconditionally true, and the transport comparison that used to make it
// conditional is deliberately gone rather than left as a tautology. The
// registry is the shared port-whitelist, reserved-port and SetReverseBound
// surface for BOTH transports:
//
//   - the SSH tunnel server (ssh.NewServer), and
//   - the h2 stream tunnel handlers (POST /api/tunnel/stream and
//     /api/tunnel/control), which are served by the always-on main HTTP server
//     (see cmd/server/server_protocols.go).
//
// The h2 endpoints are served by the main HTTP server and therefore exist
// whenever the process runs, so there is no configuration in which "nothing can
// reach the registry". The old predicate was
//
//	cfg.PortForward.Enabled || cfg.PortForward.Transport != model.TransportSSH
//
// and its one false cell was `transport: "ssh"` with Enabled == false. That
// cell is no longer expressible: model.ApplyDefaults pins
// cfg.PortForward.Transport to "both" on every load (internal/model/defaults.go),
// so the comparison was always true. Keeping the expression would advertise a
// configurability that does not exist — and would re-open the hole it was
// written to close if anyone ever made the transport settable again.
//
// Do NOT reintroduce the older gate (`cfg.PortForward.Enabled` alone): that
// handed every tunnel request a nil registry and a 503
// (internal/handler/tunnel_stream.go:59, tunnel_control.go:297).
//
// Creating the registry is side-effect-free with respect to SSH: it starts only
// a 5s health-check goroutine and restores persisted port rows from the
// database. It binds no port — listening remains ssh.NewServer's job, still
// gated on PortForward.Enabled.
func shouldCreateProxyRegistry(_ model.Config) bool {
	return true
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
