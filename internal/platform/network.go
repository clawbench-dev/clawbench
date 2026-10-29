package platform

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
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
//
//nolint:gocyclo // network interface filtering is inherently multi-branch
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

// ChinaMirrorChecked caches the result of IsChinaMainland() to avoid repeated
// network probes. 0 = not yet checked; 1 = true (China); 2 = false.
// Exported for test access from handler package.
var ChinaMirrorChecked atomic.Int32

var chinaProbeClient = &http.Client{
	Timeout: 3 * time.Second,
	Transport: &http.Transport{
		Proxy: nil,
	},
}

// IsChinaMainland returns true if the server appears to be running in mainland China.
// Uses a multi-strategy approach:
//  1. Direct connectivity: try TCP connect to npmmirror — if fast, likely in China.
//  2. Fallback: IP geolocation via ip-api.com — country_code == "CN".
//
// Result is cached (ChinaMirrorChecked: 0=unchecked, 1=China, 2=non-China).
func IsChinaMainland() bool {
	if v := ChinaMirrorChecked.Load(); v != 0 {
		return v == 1
	}

	// Strategy 1: Direct TCP connectivity test to npmmirror.
	// If we can connect to the China mirror within 500ms, we're likely in China.
	// This is more reliable than IP geolocation because it tests actual network reachability.
	if probeChinaMirror() {
		slog.Debug("china detected via mirror connectivity")
		ChinaMirrorChecked.Store(1)
		return true
	}

	// Strategy 2: Fallback to IP geolocation via ip-api.com
	if probeIPApi() {
		slog.Debug("china detected via ip-api geolocation")
		ChinaMirrorChecked.Store(1)
		return true
	}

	ChinaMirrorChecked.Store(2)
	return false
}

// probeChinaMirror tries to TCP connect to registry.npmmirror.com:443.
// Returns true if the connection succeeds within 500ms, indicating we're
// likely in China (or at least have fast access to the China mirror).
func probeChinaMirror() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	conn, err := probeChinaDialer.DialContext(ctx, "tcp", "registry.npmmirror.com:443")
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// probeChinaDialer is the dialer used by probeChinaMirror.
// Package-level variable for testability.
var probeChinaDialer = net.Dialer{}

// probeIPApi queries ip-api.com for the country code. Returns true if "CN".
func probeIPApi() bool {
	return probeIPApiWithClient(chinaProbeClient)
}

// probeIPApiWithClient is the testable core of probeIPApi.
// Package-level variable for testability — tests can override to inject
// custom behavior.
var probeIPApiWithClient = func(client *http.Client) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://ip-api.com/line/?fields=countryCode", http.NoBody)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("ip-api probe failed", "error", err)
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16))
	if err != nil {
		return false
	}
	code := strings.TrimSpace(string(body))
	return code == "CN"
}

// NpmMirrorRegistry is the China npm mirror URL.
const NpmMirrorRegistry = "https://registry.npmmirror.com"

// InterfaceKind classifies a local address so the UI can label it. The point is
// NOT to hide addresses: a user with a Docker bridge or a VPN needs to see every
// candidate to understand why a colleague cannot open the link. Labeling lets
// them pick the right one instead of guessing.
type InterfaceKind string

const (
	// KindLAN is a normal routable private/public address (Wi-Fi, Ethernet).
	KindLAN InterfaceKind = "lan"
	// KindVirtual is a virtual adapter: container bridges, VM host-only nets,
	// VPN tunnels. Reachable from the host, usually NOT from another machine.
	KindVirtual InterfaceKind = "virtual"
	// KindPublic is a globally routable address.
	KindPublic InterfaceKind = "public"
	// KindOther is anything unclassified.
	KindOther InterfaceKind = "other"
)

// LocalAddress is one address a client might reach this server on.
type LocalAddress struct {
	// IP is the bare address, e.g. "192.168.1.7".
	IP string `json:"ip"`
	// Interface is the OS interface name, e.g. "wlp1s0". Empty when unknown.
	Interface string `json:"interface"`
	// Kind lets the UI label the entry (and sort the likely ones first).
	Kind InterfaceKind `json:"kind"`
}

// virtualIfacePrefixes are interface-name prefixes that identify virtual
// adapters. Deliberately a LABEL, not a filter: the addresses are still
// returned, because hiding one would leave the user unable to explain why a
// particular address does not work.
//
// The list covers the common cases on the three supported platforms:
//   - Linux: docker0, br-<id>, veth*, virbr*, tun*/tap*/wg* (VPN), lxcbr*
//   - macOS: utun* (VPN), bridge*, vmnet*, vboxnet*
//   - Windows: vEthernet (WSL/Hyper-V), "Loopback", "VirtualBox", "VMware"
var virtualIfacePrefixes = []string{
	"docker", "br-", "veth", "virbr", "lxcbr", "cni", "flannel", "kube",
	"tun", "tap", "wg", "utun", "ppp",
	"bridge", "vmnet", "vboxnet",
	"vethernet", "loopback", "virtualbox", "vmware", "hyper-v",
}

// isVirtualIface labels an interface by name. Matching is case-insensitive
// because Windows names its adapters "vEthernet (WSL)".
func isVirtualIface(name string) bool {
	lower := strings.ToLower(name)
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// GetLocalAddresses returns every address the server can be reached on, each
// labeled with its interface and a kind. Sorted so the most useful entries come
// first: LAN before virtual, IPv4 before IPv6, then by address.
//
// Unlike GetLocalIPs this keeps virtual adapters, because the caller wants to
// SHOW them (labeled) rather than silently drop them.
func GetLocalAddresses() []LocalAddress {
	out := collectLocalAddresses()
	sortAddresses(out)
	return out
}

// collectLocalAddresses walks the interfaces and keeps every usable address.
//
// IPv6 needs one extra step. Modern OSes assign a ROTATING privacy address
// (RFC 4941) per interface alongside the stable one; measured on a typical
// dual-stack laptop, 14 of 17 addresses were temporary IPv6 that change every
// few hours. A share link pointing at one is dead on arrival, and 14 rows of
// noise bury the 3 addresses that work.
//
// net.IP carries no "temporary" flag, so the /64 prefix is used instead: only
// the first address per (interface, /64) is kept. The stable address and its
// temporary siblings share a /64, so this collapses the group to one entry
// without needing to tell them apart.
func collectLocalAddresses() []LocalAddress {
	ifaces, err := netInterfaces()
	if err != nil {
		return nil
	}
	var out []LocalAddress
	seen := make(map[string]bool)
	seenV6Prefix := make(map[string]bool)
	for i := range ifaces {
		iface := &ifaces[i]
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifaceAddrs(iface)
		if err != nil {
			continue
		}
		virtual := isVirtualIface(iface.Name)
		for _, addr := range addrs {
			ip := addrIP(addr)
			if ip == nil {
				continue
			}
			s := ip.String()
			if seen[s] {
				continue
			}
			if prefix, ok := v6PrefixKey(iface.Name, ip); ok {
				if seenV6Prefix[prefix] {
					continue
				}
				seenV6Prefix[prefix] = true
			}
			seen[s] = true
			out = append(out, LocalAddress{IP: s, Interface: iface.Name, Kind: addressKind(ip, virtual)})
		}
	}
	return out
}

// v6PrefixKey returns a key identifying an interface's IPv6 /64, and false for
// an IPv4 address (which needs no collapsing).
func v6PrefixKey(ifaceName string, ip net.IP) (string, bool) {
	if ip.To4() != nil {
		return "", false
	}
	return ifaceName + "|" + ip.Mask(net.CIDRMask(64, 128)).String(), true
}

// addrIP extracts the IP from an interface address, returning nil for anything
// a client could not use (loopback, link-local).
func addrIP(addr net.Addr) net.IP {
	var ip net.IP
	switch v := addr.(type) {
	case *net.IPNet:
		ip = v.IP
	case *net.IPAddr:
		ip = v.IP
	}
	if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return nil
	}
	return ip
}

// addressKind classifies an address for the UI.
func addressKind(ip net.IP, virtual bool) InterfaceKind {
	switch {
	case virtual:
		return KindVirtual
	case ip.To4() != nil && !ip.IsPrivate():
		return KindPublic
	default:
		return KindLAN
	}
}

// sortAddresses orders LAN/public first (those are what a colleague can use),
// then virtual, then IPv4 before IPv6.
func sortAddresses(list []LocalAddress) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if usableKind(a.Kind) != usableKind(b.Kind) {
			return usableKind(a.Kind)
		}
		a4, b4 := net.ParseIP(a.IP).To4() != nil, net.ParseIP(b.IP).To4() != nil
		if a4 != b4 {
			return a4
		}
		ai, bi := net.ParseIP(a.IP), net.ParseIP(b.IP)
		for k := 0; k < len(ai) && k < len(bi); k++ {
			if ai[k] != bi[k] {
				return ai[k] < bi[k]
			}
		}
		return a.IP < b.IP
	})
}

// usableKind reports whether an address is likely reachable from another machine.
func usableKind(k InterfaceKind) bool {
	return k == KindLAN || k == KindPublic
}
