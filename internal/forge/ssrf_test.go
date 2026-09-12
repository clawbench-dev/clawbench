package forge

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckHostSafety_OfficialHostsAllowed(t *testing.T) {
	for _, host := range []string{"github.com", "gitlab.com", "github.com:443"} {
		t.Run(host, func(t *testing.T) {
			require.NoError(t, CheckHostSafety(host, nil))
		})
	}
}

func TestCheckHostSafety_BlockedAddresses(t *testing.T) {
	tests := []struct {
		name string
		host string
	}{
		{"loopback v4", "127.0.0.1"},
		{"loopback range", "127.10.20.30"},
		{"localhost name", "localhost"},
		{"private 10/8", "10.1.2.3"},
		{"private 172.16/12", "172.16.5.6"},
		{"private 192.168/16", "192.168.1.1"},
		{"link-local metadata", "169.254.169.254"},
		{"unspecified", "0.0.0.0"},
		{"ipv6 loopback", "::1"},
		{"ipv6 unique local", "fd00::1"},
		{"ipv6 link-local", "fe80::1"},
		{"ipv4-mapped loopback", "::ffff:127.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckHostSafety(tt.host, nil)
			assert.Error(t, err, "host %q must be rejected", tt.host)
		})
	}
}

func TestCheckHostSafety_PublicAddressAllowed(t *testing.T) {
	// A literal public IP should pass without DNS resolution.
	require.NoError(t, CheckHostSafety("93.184.216.34", nil))
}

func TestCheckHostSafety_CustomResolverUsed(t *testing.T) {
	// A hostname that resolves to a private IP must be rejected even though the
	// name itself looks benign. This is the DNS-rebinding guard.
	resolver := func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.5")}, nil
	}
	err := CheckHostSafety("git.internal.example", resolver)
	assert.Error(t, err, "hostname resolving to a private IP must be rejected")

	// And a hostname resolving to a public IP is allowed.
	resolver = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	require.NoError(t, CheckHostSafety("git.example.com", resolver))
}

func TestCheckHostSafety_RejectsEmptyAndMalformed(t *testing.T) {
	for _, host := range []string{"", " ", "host with space"} {
		t.Run(host, func(t *testing.T) {
			assert.Error(t, CheckHostSafety(host, nil))
		})
	}
}
