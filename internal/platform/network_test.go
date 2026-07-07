package platform

import (
	"errors"
	"net"
	"testing"
	"time"
)

// mockConn implements net.Conn for testing LocalAddr behavior.
type mockConn struct {
	localAddr net.Addr
}

func (m *mockConn) Read(b []byte) (n int, err error)   { return 0, net.ErrClosed }
func (m *mockConn) Write(b []byte) (n int, err error)  { return 0, net.ErrClosed }
func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return m.localAddr }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func TestGetOutboundIP_ReturnsNonLoopback(t *testing.T) {
	ip := GetOutboundIP()
	if ip == "" {
		t.Log("GetOutboundIP() returned empty string (no outbound route available)")
		return
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		t.Errorf("GetOutboundIP() returned invalid IP %q", ip)
	}
	if parsed.IsLoopback() {
		t.Errorf("GetOutboundIP() returned loopback IP %q, want non-loopback", ip)
	}
}

func TestGetOutboundIP_DialError(t *testing.T) {
	orig := dialOutbound
	defer func() { dialOutbound = orig }()

	dialOutbound = func() (net.Conn, error) {
		return nil, errors.New("dial error")
	}

	ip := GetOutboundIP()
	if ip != "" {
		t.Errorf("GetOutboundIP() = %q, want empty string on dial error", ip)
	}
}

func TestGetOutboundIP_NonUDPAddr(t *testing.T) {
	orig := dialOutbound
	defer func() { dialOutbound = orig }()

	// Return a connection whose LocalAddr is a TCPAddr (not *net.UDPAddr)
	dialOutbound = func() (net.Conn, error) {
		return &mockConn{localAddr: &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 1234}}, nil
	}

	ip := GetOutboundIP()
	if ip != "" {
		t.Errorf("GetOutboundIP() = %q, want empty string when LocalAddr is not *net.UDPAddr", ip)
	}
}

func TestGetOutboundIP_LoopbackAddr(t *testing.T) {
	orig := dialOutbound
	defer func() { dialOutbound = orig }()

	// Return a connection with a loopback UDP address
	dialOutbound = func() (net.Conn, error) {
		return &mockConn{localAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234}}, nil
	}

	ip := GetOutboundIP()
	if ip != "" {
		t.Errorf("GetOutboundIP() = %q, want empty string for loopback address", ip)
	}
}

func TestGetOutboundIP_ValidIP(t *testing.T) {
	orig := dialOutbound
	defer func() { dialOutbound = orig }()

	// Return a connection with a valid non-loopback UDP address
	dialOutbound = func() (net.Conn, error) {
		return &mockConn{localAddr: &net.UDPAddr{IP: net.ParseIP("192.168.1.100"), Port: 12345}}, nil
	}

	ip := GetOutboundIP()
	if ip != "192.168.1.100" {
		t.Errorf("GetOutboundIP() = %q, want %q", ip, "192.168.1.100")
	}
}

func TestGetOutboundIP_IPv6Addr(t *testing.T) {
	orig := dialOutbound
	defer func() { dialOutbound = orig }()

	dialOutbound = func() (net.Conn, error) {
		return &mockConn{localAddr: &net.UDPAddr{IP: net.ParseIP("fe80::1"), Port: 12345}}, nil
	}

	ip := GetOutboundIP()
	if ip == "" {
		t.Errorf("GetOutboundIP() returned empty for IPv6 link-local address")
	}
}

func TestGetOutboundIP_ContextCanceled(t *testing.T) {
	orig := dialOutbound
	defer func() { dialOutbound = orig }()

	// Simulate a canceled context by returning an error
	dialOutbound = func() (net.Conn, error) {
		return nil, errors.New("context canceled")
	}

	ip := GetOutboundIP()
	if ip != "" {
		t.Errorf("GetOutboundIP() = %q, want empty string on canceled context", ip)
	}
}

func TestGetLocalIPs_Basic(t *testing.T) {
	ips := GetLocalIPs()
	// On any real machine we expect at least one non-loopback IP,
	// but in some CI containers there may be none, so just validate format.
	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			t.Errorf("GetLocalIPs() returned invalid IP %q", ip)
		}
		if parsed.IsLoopback() {
			t.Errorf("GetLocalIPs() returned loopback IP %q", ip)
		}
	}
}

func TestGetLocalIPs_InterfaceError(t *testing.T) {
	origI := netInterfaces
	defer func() { netInterfaces = origI }()

	netInterfaces = func() ([]net.Interface, error) {
		return nil, errors.New("interface error")
	}

	ips := GetLocalIPs()
	if ips != nil {
		t.Errorf("GetLocalIPs() = %v, want nil on interface error", ips)
	}
}

func TestGetLocalIPs_SkipsLoopback(t *testing.T) {
	origI, origA := netInterfaces, ifaceAddrs
	defer func() { netInterfaces = origI; ifaceAddrs = origA }()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "lo", Flags: net.FlagUp | net.FlagLoopback},
		}, nil
	}
	ifaceAddrs = func(iface *net.Interface) ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
		}, nil
	}

	ips := GetLocalIPs()
	if len(ips) != 0 {
		t.Errorf("GetLocalIPs() = %v, want empty (loopback skipped)", ips)
	}
}

func TestGetLocalIPs_SkipsLinkLocal(t *testing.T) {
	origI, origA := netInterfaces, ifaceAddrs
	defer func() { netInterfaces = origI; ifaceAddrs = origA }()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "eth0", Flags: net.FlagUp},
		}, nil
	}
	ifaceAddrs = func(iface *net.Interface) ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("169.254.1.1"), Mask: net.CIDRMask(16, 32)},
			&net.IPNet{IP: net.ParseIP("192.168.1.100"), Mask: net.CIDRMask(24, 32)},
		}, nil
	}

	ips := GetLocalIPs()
	if len(ips) != 1 || ips[0] != "192.168.1.100" {
		t.Errorf("GetLocalIPs() = %v, want [192.168.1.100] (link-local skipped)", ips)
	}
}

func TestGetLocalIPs_MultipleInterfaces(t *testing.T) {
	origI, origA := netInterfaces, ifaceAddrs
	defer func() { netInterfaces = origI; ifaceAddrs = origA }()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "eth0", Flags: net.FlagUp},
			{Name: "wlan0", Flags: net.FlagUp},
		}, nil
	}
	addrMap := map[string][]net.Addr{
		"eth0": {&net.IPNet{IP: net.ParseIP("192.168.1.100"), Mask: net.CIDRMask(24, 32)}},
		"wlan0": {&net.IPNet{IP: net.ParseIP("10.0.0.5"), Mask: net.CIDRMask(24, 32)}},
	}
	ifaceAddrs = func(iface *net.Interface) ([]net.Addr, error) {
		return addrMap[iface.Name], nil
	}

	ips := GetLocalIPs()
	if len(ips) != 2 {
		t.Errorf("GetLocalIPs() = %v, want 2 IPs", ips)
	}
	// Should be sorted: 10.0.0.5 before 192.168.1.100
	if ips[0] != "10.0.0.5" || ips[1] != "192.168.1.100" {
		t.Errorf("GetLocalIPs() = %v, want [10.0.0.5, 192.168.1.100]", ips)
	}
}

func TestGetLocalIPs_IPv4BeforeIPv6(t *testing.T) {
	origI, origA := netInterfaces, ifaceAddrs
	defer func() { netInterfaces = origI; ifaceAddrs = origA }()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "eth0", Flags: net.FlagUp},
		}, nil
	}
	ifaceAddrs = func(iface *net.Interface) ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("fd00::1"), Mask: net.CIDRMask(64, 128)},
			&net.IPNet{IP: net.ParseIP("192.168.1.100"), Mask: net.CIDRMask(24, 32)},
		}, nil
	}

	ips := GetLocalIPs()
	if len(ips) != 2 {
		t.Errorf("GetLocalIPs() = %v, want 2 IPs", ips)
	}
	// IPv4 should come before IPv6
	if ips[0] != "192.168.1.100" {
		t.Errorf("GetLocalIPs()[0] = %q, want IPv4 first", ips[0])
	}
}

func TestGetLocalIPs_Deduplicates(t *testing.T) {
	origI, origA := netInterfaces, ifaceAddrs
	defer func() { netInterfaces = origI; ifaceAddrs = origA }()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "eth0", Flags: net.FlagUp},
		}, nil
	}
	ifaceAddrs = func(iface *net.Interface) ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("192.168.1.100"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("192.168.1.100"), Mask: net.CIDRMask(24, 32)},
		}, nil
	}

	ips := GetLocalIPs()
	if len(ips) != 1 {
		t.Errorf("GetLocalIPs() = %v, want 1 IP (deduplicated)", ips)
	}
}
