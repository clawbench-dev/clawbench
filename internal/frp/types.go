// Package frp runs the FRP (Fast Reverse Proxy) client in-process to expose a
// local ClawBench instance on a public port.
//
// The client library (github.com/fatedier/frp) is a dual-use networking tool:
// it is widely flagged by endpoint security products as Greyware/riskware
// because it can punch through NAT and firewall inbound rules. Builds tagged
// `nofrp` exclude it entirely (see manager_nofrp.go), which removes the
// capability — and therefore every detection signature — from the binary.
//
// This file holds the parts that do not depend on the client library, so both
// build variants share one definition of the exported surface (State, Status)
// and the handler in internal/handler can compile against either.
package frp

import (
	"fmt"
	"net"
	"strconv"

	"clawbench/internal/ws"
)

// State represents the FRP tunnel lifecycle state.
type State string

const (
	StateDisabled State = "disabled" // FRP not configured
	StateStarting State = "starting" // service started, waiting for proxy ready
	StateRunning  State = "running"  // tunnel established, remote port known
	StateFailed   State = "failed"   // service exited with error
	StateStopped  State = "stopped"  // user stopped the tunnel
)

// ProxyPhaseRunning is the frp proxy phase indicating the proxy is active.
const ProxyPhaseRunning = "running"

// Status holds the current FRP tunnel status.
type Status struct {
	Enabled       bool   `json:"enabled"`
	Running       bool   `json:"running"`
	State         State  `json:"state"`
	ServerAddr    string `json:"server_addr,omitempty"`
	RemotePort    int    `json:"remote_port,omitempty"`
	SSHRemotePort int    `json:"ssh_remote_port,omitempty"`
	RemoteURL     string `json:"remote_url,omitempty"`
	Message       string `json:"message,omitempty"`
}

// FormatRemoteURL builds the public URL from server address and remote port.
func FormatRemoteURL(serverAddr string, remotePort int) string {
	return fmt.Sprintf("http://%s:%d", serverAddr, remotePort)
}

// parsePortFromAddr extracts the port number from a "host:port" address string.
func parsePortFromAddr(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return port
}

// emitEvent broadcasts an frp_status event to connected WS clients.
func emitEvent(status string, data ...map[string]any) {
	mgr := ws.GetManager()
	if mgr == nil {
		return
	}
	payload := map[string]any{"status": status}
	if len(data) > 0 {
		for k, v := range data[0] {
			payload[k] = v
		}
	}
	mgr.BroadcastEvent(ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		Event: "frp_status",
		Data:  payload,
	})
}
