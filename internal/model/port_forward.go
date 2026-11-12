package model

// PortForwardConfig holds the port-forward server configuration.
// The YAML key is "port_forward".
type PortForwardConfig struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`             // Enable port forward server (default: true)
	Port         int    `yaml:"port" json:"port"`                   // SSH port (0 = auto = main_port + 1, e.g. 20000→20001)
	HostKey      string `yaml:"host_key" json:"host_key"`           // Path to host key file (empty = auto-persist to DataDir/ssh_host_key)
	AllowedPorts string `yaml:"allowed_ports" json:"allowed_ports"` // Port ranges allowed for forwarding, e.g. "1024-65535" or "3000,5173,8080" (default: "1024-65535")
	// Transport is the SERVER-side switch for which wire carries forwarded
	// ports: TransportSSH (listen on Port) or TransportH2 (ride the main HTTP
	// port). Clients read it from /api/ssh/info and follow — they no longer
	// keep a local choice.
	//
	// The server enforces it: with TransportH2 no SSH listener is opened at
	// all, and with TransportSSH the h2 stream endpoints answer 503.
	Transport string `yaml:"transport" json:"transport"` // "ssh" | "h2" (default: "h2")
}

// Tunnel transport values for PortForwardConfig.Transport.
const (
	TransportSSH = "ssh"
	TransportH2  = "h2"
	// TransportBoth is retired. It is still ACCEPTED (old clients PATCH it,
	// old config files contain it) and normalized to TransportH2 — its
	// historical meaning was "h2 is available, probe it first".
	TransportBoth = "both"
)

// DefaultPortForwardTransport is the transport an install gets when the value
// is absent or unrecognized. It is "h2" because that is what every native
// client already defaulted to: h2 rides the main HTTP port, so it works on
// deployments that never expose the SSH port at all.
const DefaultPortForwardTransport = TransportH2

// NormalizePortForwardTransport returns the effective transport for a stored or
// requested value: "ssh" stays "ssh"; "h2", the retired "both", an empty
// string and anything unrecognized become "h2".
//
// This is the single repair point for hand-edited config.yaml — the load path
// has no standalone validator, so garbage would otherwise survive until the
// first PATCH.
func NormalizePortForwardTransport(t string) string {
	if t == TransportSSH {
		return TransportSSH
	}
	return TransportH2
}

// IsValidPortForwardTransport reports whether t is a value the settings PATCH
// validator should accept.
//
// The retired "both" is still accepted (and normalized) so an older client's
// save does not fail with a 400; a value it does not recognize is rejected
// explicitly rather than silently stored.
func IsValidPortForwardTransport(t string) bool {
	switch t {
	case TransportSSH, TransportH2, TransportBoth:
		return true
	default:
		return false
	}
}
