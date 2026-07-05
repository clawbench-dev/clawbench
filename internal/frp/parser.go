package frp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"log/slog"
)

// LogEvent represents a parsed event from frpc stdout.
type LogEvent struct {
	Type       string // "proxy_start", "port_assigned"
	ProxyName  string // e.g. "clawbench-http"
	RemotePort int    // assigned remote port
}

var (
	// frpc v0.52+ log format:
	//   [I] [proxy.go:xxx] [clawbench-http] start proxy success
	reProxyStart = regexp.MustCompile(`\[(\S+)\] start proxy success`)

	// frpc logs the remote port assignment:
	//   [I] [proxy.go:xxx] [clawbench-http] start tcp proxy, local: 127.0.0.1:20000, remote: 120.26.168.245:20050
	// We extract the remote port from the "remote: <addr>:<port>" part.
	reRemotePort = regexp.MustCompile(`\[(\S+)\] start tcp proxy.*remote: \S+:(\d+)`)
)

// ParseLine parses a single line of frpc stdout and returns a LogEvent
// if the line contains a recognized event. Returns nil for unrecognized lines.
func ParseLine(line string) *LogEvent {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	// Check for "start proxy success"
	if m := reProxyStart.FindStringSubmatch(line); m != nil {
		return &LogEvent{
			Type:      "proxy_start",
			ProxyName: m[1],
		}
	}

	// Check for remote port assignment
	if m := reRemotePort.FindStringSubmatch(line); m != nil {
		port, err := strconv.Atoi(m[2])
		if err != nil {
			slog.Warn("frp: failed to parse remote port", slog.String("raw", m[2]), slog.String("err", err.Error()))
			return nil
		}
		return &LogEvent{
			Type:       "port_assigned",
			ProxyName:  m[1],
			RemotePort: port,
		}
	}

	return nil
}

// FormatRemoteURL builds the public URL from server address and remote port.
func FormatRemoteURL(serverAddr string, remotePort int) string {
	return fmt.Sprintf("http://%s:%d", serverAddr, remotePort)
}
