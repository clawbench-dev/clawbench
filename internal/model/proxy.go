package model

// ForwardedPort represents a registered forwarded port.
type ForwardedPort struct {
	Port            int    `json:"port"`            // Target port on the remote host (e.g. 8080)
	LocalPort       int    `json:"localPort"`       // Local listening port (auto-assigned, same as Port if available)
	Host            string `json:"host"`            // Target host to forward to (empty = 127.0.0.1)
	Name            string `json:"name"`            // User-friendly name (e.g. "Vite Dev Server")
	Protocol        string `json:"protocol"`        // "http" or "https" (default: "http")
	AutoDetect      bool   `json:"autoDetect"`      // Whether this was auto-detected
	Active          bool   `json:"active"`          // Whether the target port is currently listening
	HasReverseProxy bool   `json:"hasReverseProxy"` // True when a server-side reverse proxy is active for this port
}

// ProxyConfig holds the proxy section from config/config.yaml.
// Note: Enabled field was removed — ProxyRegistry is now automatically enabled
// when port_forward (SSH tunnel) is enabled. AllowedPorts has moved to
// PortForwardConfig, but ProxyConfig.AllowedPorts is kept for backward-compatible
// YAML reading (proxy.allowed_ports → port_forward.allowed_ports migration).
type ProxyConfig struct {
	AllowedPorts string `yaml:"allowed_ports"` // Port ranges, e.g. "1024-65535" or "3000,5173,8080" (default: "1024-65535")
}
