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
// Because the h2 transport has no config switch that can turn it off, there is
// no configuration in which the registry is provably unused, so it is created
// unconditionally. The old gate was `cfg.PortForward.Enabled` alone, which made
// port_forward.enabled=false hand every tunnel request a nil registry and a
// 503 (internal/handler/tunnel_stream.go:59, tunnel_control.go:297). Do not
// reintroduce it.
//
// Creating the registry is side-effect-free with respect to SSH: it starts only
// a 5s health-check goroutine and restores persisted port rows from the
// database. It binds no port — listening remains ssh.NewServer's job, still
// gated on PortForward.Enabled.
//
// T8 接入点：`port_forward.transport: ssh|h2|both` 落地后，本函数收敛为
// 「SSH 启用 **或** transport 含 h2」。在 h2 拥有独立开关之前，h2 由主
// HTTP 服务器无条件提供，故此处无条件返回 true。
func shouldCreateProxyRegistry(cfg model.Config) bool {
	// cfg is currently unread: h2 availability is a compile-time property of
	// this binary (serverProtocols always enables an h2 variant), not a config
	// value. T8 will read cfg.PortForward.Transport here.
	_ = cfg
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
