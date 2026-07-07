package model

// FRPConfig holds the FRP (Fast Reverse Proxy) client configuration.
// The YAML key is "frp". FRP is disabled by default; users must deploy
// their own frps server and fill in the configuration.
// The frp client runs in-process as a Go library — no external binary needed.
type FRPConfig struct {
	Enabled       bool   `yaml:"enabled"`         // Enable FRP tunnel (default: false)
	ServerAddr    string `yaml:"server_addr"`     // FRP server address, e.g. "120.26.168.245"
	ServerPort    int    `yaml:"server_port"`     // FRP server port (default: 7000)
	Token         string `yaml:"token"`           // FRP authentication token
	AutoPort      bool   `yaml:"auto_port"`       // Auto-assign remote ports from frps allowPorts range
	RemotePort    int    `yaml:"remote_port"`     // Remote TCP forwarding port (used when AutoPort=false)
	SSHRemotePort int    `yaml:"ssh_remote_port"` // SSH port forwarding remote port (used when AutoPort=false)
}
