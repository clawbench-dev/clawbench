package platform

import (
	"context"
	"net"
	"sort"
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

// netInterfaces is a package-level function variable for testability —
// tests can override to inject custom network interfaces.
var netInterfaces = net.Interfaces

// ifaceAddrs is a package-level function variable for testability —
// tests can override to inject custom interface addresses.
// Matches the signature of net.Interface.Addrs.
var ifaceAddrs = func(iface *net.Interface) ([]net.Addr, error) {
	return iface.Addrs()
}

// GetLocalIPs returns all non-loopback IP addresses on all active network
// interfaces, sorted with IPv4 before IPv6 and by address value within each family.
// Returns nil if no addresses are found.
func GetLocalIPs() []string {
	ifaces, err := netInterfaces()
	if err != nil {
		return nil
	}
	var ips []string
	seen := make(map[string]bool)
	for i := range ifaces {
		iface := &ifaces[i]
		// Skip interfaces that are down or loopback
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifaceAddrs(iface)
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
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}
			s := ip.String()
			if !seen[s] {
				seen[s] = true
				ips = append(ips, s)
			}
		}
	}
	sort.Slice(ips, func(i, j int) bool {
		a, b := net.ParseIP(ips[i]), net.ParseIP(ips[j])
		a4, b4 := a.To4() != nil, b.To4() != nil
		if a4 != b4 {
			return a4 // IPv4 before IPv6
		}
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})
	return ips
}
