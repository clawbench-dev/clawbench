package frp

import (
	"fmt"
	"strings"

	"clawbench/internal/model"
)

// GenerateConfig produces frpc.toml content from the given FRPConfig.
// It creates TCP proxy entries for the main ClawBench HTTP port and
// optionally the SSH tunnel port.
// Returns error if ServerAddr is empty (invalid config).
func GenerateConfig(cfg model.FRPConfig, httpLocalPort, sshLocalPort int) (string, error) {
	if cfg.ServerAddr == "" {
		return "", fmt.Errorf("FRP server_addr is required")
	}

	var b strings.Builder

	b.WriteString(fmt.Sprintf("serverAddr = %q\n", cfg.ServerAddr))
	b.WriteString(fmt.Sprintf("serverPort = %d\n", cfg.ServerPort))
	b.WriteString(`auth.method = "token"` + "\n")
	b.WriteString(fmt.Sprintf("auth.token = %q\n", cfg.Token))
	b.WriteString("\n")

	// HTTP proxy — main ClawBench web interface
	b.WriteString("[[proxies]]\n")
	b.WriteString("name = \"clawbench-http\"\n")
	b.WriteString("type = \"tcp\"\n")
	b.WriteString("localIP = \"127.0.0.1\"\n")
	b.WriteString(fmt.Sprintf("localPort = %d\n", httpLocalPort))
	b.WriteString(fmt.Sprintf("remotePort = %d\n", cfg.RemotePort))
	b.WriteString("\n")

	// SSH proxy — optional, only if SSH tunnel is running
	if sshLocalPort > 0 {
		b.WriteString("[[proxies]]\n")
		b.WriteString("name = \"clawbench-ssh\"\n")
		b.WriteString("type = \"tcp\"\n")
		b.WriteString("localIP = \"127.0.0.1\"\n")
		b.WriteString(fmt.Sprintf("localPort = %d\n", sshLocalPort))
		b.WriteString(fmt.Sprintf("remotePort = %d\n", cfg.SSHRemotePort))
	}

	return b.String(), nil
}
