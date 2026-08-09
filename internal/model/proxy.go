package model

// ForwardedPort represents a registered forwarded port.
type ForwardedPort struct {
	Port      int    `json:"port"`      // Target port on the remote host (e.g. 8080)
	LocalPort int    `json:"localPort"` // Local listening port (auto-assigned, same as Port if available)
	Host      string `json:"host"`      // Target host to forward to (empty = 127.0.0.1)
	Name      string `json:"name"`      // User-friendly name (e.g. "Vite Dev Server")
	Protocol  string `json:"protocol"`  // "http" or "https" (default: "http")
	Active    bool   `json:"active"`    // Whether the target port is currently listening
	Enabled   bool   `json:"enabled"`   // User-controlled enable/disable; disabled stops forwarding
}
