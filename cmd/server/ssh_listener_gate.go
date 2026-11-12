package main

import "clawbench/internal/model"

// shouldListenSSH reports whether the process should open the SSH tunnel
// listener (the dedicated port, default 20001).
//
// Transport is the server-side switch the clients follow:
//
//   - "ssh" — forwards travel over SSH, so the listener must exist.
//   - "h2"  — forwards ride the main HTTP server (/api/tunnel/stream and
//     /api/tunnel/control), so nothing listens on the SSH port at all.
//
// ApplyDefaults has already normalized the stored value, so anything that is
// not "ssh" means h2. Keeping the check in one place matters because three call
// sites share it: startup, hot-reload (start/stop), and the tests that pin the
// mapping.
func shouldListenSSH(cfg model.Config) bool {
	return cfg.PortForward.Transport == model.TransportSSH
}
