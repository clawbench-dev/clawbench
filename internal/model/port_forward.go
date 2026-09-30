package model

// PortForwardConfig holds the SSH tunnel server configuration for remote port forwarding.
// The YAML key is "port_forward".
type PortForwardConfig struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`             // Enable port forward (SSH tunnel) server (default: true)
	Port         int    `yaml:"port" json:"port"`                   // SSH port (0 = auto = main_port + 1, e.g. 20000→20001)
	HostKey      string `yaml:"host_key" json:"host_key"`           // Path to host key file (empty = auto-persist to DataDir/ssh_host_key)
	AllowedPorts string `yaml:"allowed_ports" json:"allowed_ports"` // Port ranges allowed for forwarding, e.g. "1024-65535" or "3000,5173,8080" (default: "1024-65535")
	// Transport is a legacy tunnel-transport hint that the server now PINS to
	// "both" in ApplyDefaults. It is still serialized (the web client reads it
	// from /api/config) but every accepted value is ignored server-side.
	Transport string `yaml:"transport" json:"transport"` // Pinned to "both"; see DefaultPortForwardTransport
}

// Tunnel transport values for PortForwardConfig.Transport.
//
// These are the three values the wire has ever known. Only "both" is reachable
// after ApplyDefaults: the field is a compatibility hint (the web client's
// tunnelTransportAllowsH2() reads it and needs a value it recognizes), not a
// server-side switch. The constants are kept so that value stays expressible
// and so the PATCH validator can still recognize — and refuse — a caller that
// sends garbage.
const (
	TransportSSH  = "ssh"
	TransportH2   = "h2"
	TransportBoth = "both"
)

// DefaultPortForwardTransport is the value ApplyDefaults pins
// port_forward.transport to, unconditionally — an omitted field, "ssh", "h2",
// garbage and an explicit "both" all come out as "both".
//
// Why pin it: the transport chain is probed h2-over-TLS → h2c → SSH, and a
// client configured for "both" still reaches an SSH-only server through the
// fallback, so "both" is behavior-neutral for every existing install (it was
// already the default). The alternative — letting an operator say "ssh" — is
// not enforceable anyway: the two native clients no longer read the field
// (Electron clamps to "ssh" at its IPC boundary, Android uses a local
// SharedPreferences switch), so a client could always reach h2 regardless. A
// setting the server cannot enforce is worse than no setting.
const DefaultPortForwardTransport = TransportBoth

// IsValidPortForwardTransport reports whether t is one of the three transport
// values the tunnel understands.
//
// It survives only for the settings PATCH validator, which keeps rejecting a
// value it does not recognize with an explicit 400 rather than silently
// ignoring it (a stored "quic" would otherwise reach config.yaml and be
// converged on the next load). The accepted value is then ignored: PATCH also
// pins the field to DefaultPortForwardTransport, so `transport: ssh` is
// accepted, echoed as OK, and stored as "both".
func IsValidPortForwardTransport(t string) bool {
	switch t {
	case TransportSSH, TransportH2, TransportBoth:
		return true
	default:
		return false
	}
}
