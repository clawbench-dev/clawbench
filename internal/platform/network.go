package platform

import (
	"context"
	"net"
	"time"
)

// outboundDialer is the dialer used by the default dialOutbound implementation.
// Package-level variable for testability.
var outboundDialer = net.Dialer{Timeout: 1 * time.Second}

// dialOutbound dials the outbound UDP connection used by GetOutboundIP.
// Package-level function variable for testability — tests can override to
// inject connections with custom LocalAddr values.
var dialOutbound = func() (net.Conn, error) {
	return outboundDialer.DialContext(context.Background(), "udp", "8.8.8.8:53")
}

// GetOutboundIP returns the preferred outbound IP address of this machine
// by attempting a UDP connection to a public DNS server.
// Returns empty string if the IP cannot be determined.
func GetOutboundIP() string {
	conn, err := dialOutbound()
	if err != nil {
		return ""
	}
	defer conn.Close() //nolint:errcheck // best-effort close on UDP
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	// Skip loopback addresses
	if addr.IP.IsLoopback() {
		return ""
	}
	return addr.IP.String()
}

// GetAllLanIPs returns all non-loopback IPv4/IPv6 addresses from all network interfaces.
// Useful for multi-NIC machines where QR code should include all reachable LAN addresses.
func GetAllLanIPs() []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.To4() == nil {
				continue
			}
			ips = append(ips, ip.String())
		}
	}
	return ips
}
