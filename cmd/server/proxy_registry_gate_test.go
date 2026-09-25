package main

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/require"
)

// TestShouldCreateProxyRegistry pins the gate that used to be
// `cfg.PortForward.Enabled` alone. The h2 stream tunnel handlers are always
// reachable (they ride the main HTTP server), so disabling SSH must NOT nil the
// registry — that produced a blanket 503 from
// internal/handler/tunnel_stream.go:59 and tunnel_control.go:297.
func TestShouldCreateProxyRegistry(t *testing.T) {
	tests := []struct {
		name string
		cfg  model.Config
		want bool
	}{
		{
			name: "SSH enabled",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: true, Port: 20001}},
			want: true,
		},
		{
			name: "SSH disabled — h2 tunnel still needs the registry",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: false}},
			want: true,
		},
		{
			name: "zero-value config",
			cfg:  model.Config{},
			want: true,
		},
		// T8 接入点：`port_forward.transport` 落地后，此处补齐
		// ssh / h2 / both 三个取值的用例。在 h2 拥有独立开关之前，判定与
		// transport 无关，恒为 true。
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldCreateProxyRegistry(tt.cfg))
		})
	}
}

// TestReservedPortsFor_MainPortAlwaysReserved is the regression guard for the
// "client binds 20000 in reverse and takes the platform down" failure mode.
// mainPort must survive every sshPort value, including the SSH-disabled 0.
func TestReservedPortsFor_MainPortAlwaysReserved(t *testing.T) {
	const mainPort = 20000

	tests := []struct {
		name    string
		sshPort int
		want    []int
	}{
		{name: "SSH enabled with explicit port", sshPort: 2222, want: []int{20000, 2222}},
		{name: "SSH enabled on default port", sshPort: 20001, want: []int{20000, 20001}},
		{name: "SSH disabled (no SSH port to protect)", sshPort: 0, want: []int{20000}},
		{name: "negative SSH port is dropped", sshPort: -1, want: []int{20000}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reservedPortsFor(mainPort, tt.sshPort)
			require.Equal(t, tt.want, got)
			require.Contains(t, got, mainPort, "mainPort must never be dropped")
		})
	}
}

// TestReservedPortsFor_AppliedToRegistry verifies the ports actually reach the
// registry, and that the SSH-disabled sentinel 0 is not registered as a port
// (SetReservedPorts ignores p <= 0 — internal/service/proxy.go:173-181).
func TestReservedPortsFor_AppliedToRegistry(t *testing.T) {
	const mainPort = 20000

	t.Run("SSH disabled reserves mainPort only", func(t *testing.T) {
		r := service.NewProxyRegistry(mainPort)
		defer r.Stop()

		r.SetReservedPorts(reservedPortsFor(mainPort, 0)...)

		require.True(t, r.IsPortReserved(mainPort), "mainPort must be reserved with SSH off")
		require.False(t, r.IsPortReserved(0), "0 must not be registered as a reserved port")
		require.False(t, r.IsPortReserved(20001), "SSH port is not reserved when SSH is off")
	})

	t.Run("SSH enabled reserves mainPort and sshPort", func(t *testing.T) {
		r := service.NewProxyRegistry(mainPort)
		defer r.Stop()

		r.SetReservedPorts(reservedPortsFor(mainPort, 20001)...)

		require.True(t, r.IsPortReserved(mainPort))
		require.True(t, r.IsPortReserved(20001))
		require.False(t, r.IsPortReserved(0))
	})
}
