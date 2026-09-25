package model

// Port forwarding directions.
//
// DirectionForward ("forward") is the classic ssh -L mapping: the client
// listens locally and the server dials the target. DirectionReverse
// ("reverse") is ssh -R: the server binds a loopback port and every connection
// is handed back to the client, which dials the target on its own machine.
const (
	DirectionForward = "forward"
	DirectionReverse = "reverse"
)

// ForwardedPort represents a registered forwarded port.
//
// Port/LocalPort/Host are reused across directions; their meaning depends on
// Direction:
//
//	forward: Port = target port on the server side, LocalPort = client-side
//	         listening port, Host = server-side target host.
//	reverse: Port = port to expose on the client, LocalPort = port bound on the
//	         server, Host = client-side target host.
type ForwardedPort struct {
	Port      int    `json:"port"`      // Target port (see Direction)
	LocalPort int    `json:"localPort"` // Listening port (see Direction)
	Host      string `json:"host"`      // Target host (see Direction; empty = 127.0.0.1)
	Name      string `json:"name"`      // User-friendly name (e.g. "Vite Dev Server")
	Protocol  string `json:"protocol"`  // "http" or "https" (default: "http")
	Direction string `json:"direction"` // "forward" (default) or "reverse"
	Active    bool   `json:"active"`    // Whether the mapping is currently live
	Enabled   bool   `json:"enabled"`   // User-controlled enable/disable; disabled stops forwarding
}

// NormalizeDirection maps any input to a valid direction, defaulting to
// "forward". Mirrors how Protocol is normalized: an unknown value is not an
// error, it is simply the default.
func NormalizeDirection(direction string) string {
	if direction == DirectionReverse {
		return DirectionReverse
	}
	return DirectionForward
}

// IsReverse reports whether this port is an ssh -R (client → server) mapping.
func (p *ForwardedPort) IsReverse() bool {
	return p.Direction == DirectionReverse
}
