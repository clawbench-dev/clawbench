package model

// PortForwardConfig holds the SSH tunnel server configuration for remote port forwarding.
// The YAML key is "port_forward".
type PortForwardConfig struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`             // Enable port forward (SSH tunnel) server (default: true)
	Port         int    `yaml:"port" json:"port"`                   // SSH port (0 = auto = main_port + 1, e.g. 20000→20001)
	HostKey      string `yaml:"host_key" json:"host_key"`           // Path to host key file (empty = auto-persist to DataDir/ssh_host_key)
	AllowedPorts string `yaml:"allowed_ports" json:"allowed_ports"` // Port ranges allowed for forwarding, e.g. "1024-65535" or "3000,5173,8080" (default: "1024-65535")
	Transport    string `yaml:"transport" json:"transport"`         // Tunnel transport: "ssh", "h2" or "both" (default: "both")
}

// Tunnel transport values for PortForwardConfig.Transport.
//
// The transport decides which wire the client uses to reach the port-forward
// data plane: the legacy SSH listener on mainPort+1 ("ssh"), the HTTP/2 stream
// tunnel served by the always-on main HTTP server ("h2"), or h2 with an SSH
// fallback ("both"). "both" is the default so an existing install keeps its
// SSH behavior while gaining h2 without any manual switch.
const (
	TransportSSH  = "ssh"
	TransportH2   = "h2"
	TransportBoth = "both"
)

// DefaultPortForwardTransport is the value ApplyDefaults writes when the config
// omits port_forward.transport (or carries an unrecognized one).
//
// "both" — not "ssh" — is deliberate: the transport chain is probed
// h2-over-TLS → h2c → SSH, and a client configured for "both" still reaches an
// SSH-only server through the fallback. That keeps the new field backward
// compatible for both sides without asking anyone to flip a switch.
const DefaultPortForwardTransport = TransportBoth

// IsValidPortForwardTransport reports whether t is one of the three transport
// values the tunnel understands. It is the single membership test shared by
// ApplyDefaults (which converges invalid values in place, since there is no
// standalone validator) and the settings PATCH validator (which rejects them).
func IsValidPortForwardTransport(t string) bool {
	switch t {
	case TransportSSH, TransportH2, TransportBoth:
		return true
	default:
		return false
	}
}
