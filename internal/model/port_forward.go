package model

// PortForwardConfig holds the SSH tunnel server configuration for remote port forwarding.
// The YAML key is "port_forward".
type PortForwardConfig struct {
	Enabled      bool   `yaml:"enabled"`       // Enable port forward (SSH tunnel) server (default: true)
	Port         int    `yaml:"port"`          // SSH port (0 = auto = main_port + 1, e.g. 20000→20001)
	HostKey      string `yaml:"host_key"`      // Path to host key file (empty = auto-persist to DataDir/ssh_host_key)
	AllowedPorts string `yaml:"allowed_ports"` // Port ranges allowed for forwarding, e.g. "1024-65535" or "3000,5173,8080" (default: "1024-65535")
}
