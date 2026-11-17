package kb

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIsPrivateIP pins the address table the URL fetch relies on. Each case is
// an address that has actually been used to reach an internal service; a
// regression here turns "add a page from a link" back into a way to read the
// LAN and the cloud metadata endpoint.
func TestIsPrivateIP(t *testing.T) {
	private := []string{
		"127.0.0.1", "127.1.2.3",
		"10.0.0.1", "10.255.255.254",
		"172.16.0.1", "172.31.255.254",
		"192.168.0.1", "192.168.255.254",
		"169.254.169.254",
		"100.64.0.1",
		"0.0.0.0",
		"::1", "::",
		"fe80::1", "fc00::1", "fd12:3456::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1",
		"224.0.0.1",
	}
	for _, s := range private {
		require.True(t, isPrivateIP(netip.MustParseAddr(s)), s)
	}

	public := []string{
		"1.1.1.1", "8.8.8.8", "93.184.216.34",
		"172.15.0.1", "172.32.0.1",
		"192.169.0.1",
		"2001:4860:4860::8888",
	}
	for _, s := range public {
		require.False(t, isPrivateIP(netip.MustParseAddr(s)), s)
	}

	// An unparsable address is treated as unsafe.
	require.True(t, isPrivateIP(netip.Addr{}))
}

func TestIsPrivateHost(t *testing.T) {
	for _, host := range []string{
		"127.0.0.1", "127.0.0.1:8080", "localhost", "LOCALHOST:3000",
		"service.localhost", "printer.local", "db.internal",
		"10.0.0.1:80", "[::1]", "[::1]:9000", "[fe80::1]:80",
		"169.254.169.254",
	} {
		require.True(t, isPrivateHost(host), host)
	}
	for _, host := range []string{
		"example.com", "example.com:443", "www.wikipedia.org",
		"8.8.8.8", "[2001:4860:4860::8888]:443",
	} {
		require.False(t, isPrivateHost(host), host)
	}
	// No host at all can never be fetched safely.
	require.True(t, isPrivateHost(""))
}
