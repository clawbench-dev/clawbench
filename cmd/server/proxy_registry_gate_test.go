package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/handler"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/require"
)

// TestShouldCreateProxyRegistry pins the gate that used to be
// `cfg.PortForward.Enabled` alone. The h2 stream tunnel handlers ride the main
// HTTP server, so disabling SSH must NOT nil the registry — that produced a
// blanket 503 from internal/handler/tunnel_stream.go:59 and
// tunnel_control.go:297.
//
// The single "false" cell is `transport: ssh` + SSH disabled: h2 is explicitly
// excluded and the SSH listener is off, so nothing can reach the registry.
func TestShouldCreateProxyRegistry(t *testing.T) {
	tests := []struct {
		name string
		cfg  model.Config
		want bool
	}{
		{
			name: "SSH enabled, transport unset (pre-T8 config)",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: true, Port: 20001}},
			want: true,
		},
		{
			name: "SSH disabled, transport unset — h2 tunnel still needs the registry",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: false}},
			want: true,
		},
		{
			name: "zero-value config",
			cfg:  model.Config{},
			want: true,
		},

		// --- T8: port_forward.transport ---
		{
			name: "transport=ssh, SSH enabled",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: true, Transport: model.TransportSSH}},
			want: true,
		},
		{
			name: "transport=ssh, SSH disabled — the only provably-unused configuration",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: false, Transport: model.TransportSSH}},
			want: false,
		},
		{
			name: "transport=h2, SSH disabled — h2 rides the main server",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: false, Transport: model.TransportH2}},
			want: true,
		},
		{
			name: "transport=h2, SSH enabled",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: true, Transport: model.TransportH2}},
			want: true,
		},
		{
			name: "transport=both, SSH disabled — the default must keep the registry",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: false, Transport: model.TransportBoth}},
			want: true,
		},
		{
			name: "transport=both, SSH enabled",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: true, Transport: model.TransportBoth}},
			want: true,
		},
		{
			// An unvalidated/garbage value must not disable the registry: treating
			// it as h2-capable is the safe direction, and ApplyDefaults converges
			// it to "both" before this function ever sees it in production.
			name: "unrecognized transport is treated as h2-capable",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: false, Transport: "quic"}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldCreateProxyRegistry(tt.cfg))
		})
	}
}

// TestShouldCreateProxyRegistry_DefaultTransportCreatesRegistry is the direct
// regression guard for the pre-T4 failure: the transport a config ends up with
// when the user never wrote one must produce a registry.
//
// The composition is pinned without calling model.ApplyDefaults (which mutates
// process globals and writes files): internal/model's
// TestApplyDefaultsPortForwardTransport already proves "omitted → both", and
// this asserts that this default is h2-capable. Together they mean an existing
// install that never heard of port_forward.transport still gets a registry.
func TestShouldCreateProxyRegistry_DefaultTransportCreatesRegistry(t *testing.T) {
	require.Equal(t, model.TransportBoth, model.DefaultPortForwardTransport,
		"the default transport must be 'both' — 'ssh' would disable the registry for installs that never set it")
	require.True(t,
		shouldCreateProxyRegistry(model.Config{
			PortForward: model.PortForwardConfig{Transport: model.DefaultPortForwardTransport},
		}),
		"a defaulted config (transport=both, SSH disabled) must still create the registry")
}

// TestProxyRegistryGate_SSHDisabledSSHTransportYields503 wires the whole
// documented chain together: `enabled:false, transport:ssh` is the one config
// cell where shouldCreateProxyRegistry is false, so no registry is created, so
// BOTH h2 tunnel endpoints answer 503.
//
// TestShouldCreateProxyRegistry already pins the predicate and the handler
// tests pin the 503 for a manually-nil'd service.ProxyService, but nothing tied
// the two together through the gate. A regression that made the gate return
// true here (or the handler stop checking nil) would have passed both.
func TestProxyRegistryGate_SSHDisabledSSHTransportYields503(t *testing.T) {
	origProxy := service.ProxyService
	service.ProxyService = nil
	t.Cleanup(func() { service.ProxyService = origProxy })

	cfg := model.Config{
		PortForward: model.PortForwardConfig{Enabled: false, Transport: model.TransportSSH},
	}

	// The gate must say "no registry" for exactly this cell.
	require.False(t, shouldCreateProxyRegistry(cfg),
		"transport=ssh with SSH disabled is the only provably-unused configuration")
	// Nothing created a registry, mirroring main()'s `if shouldCreateProxyRegistry`.
	require.Nil(t, service.ProxyService, "no registry must exist for the disabled cell")

	for _, tc := range []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{"stream", "/api/tunnel/stream?host=127.0.0.1&port=8080", handler.TunnelStream},
		{"control", "/api/tunnel/control", handler.TunnelControl},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(""))
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			require.Equal(t, http.StatusServiceUnavailable, rec.Code,
				"with no registry there is no whitelist, so %s must be refused", tc.name)
		})
	}
}

// TestHotReloadSSH_CreatesRegistryWhenTransportSwitchesToH2 covers the
// registry-creation path in hotReloadSSH (cmd/server/main.go:1787). The
// documented behavior is that a running server started with the one
// no-registry configuration (`enabled:false, transport:ssh`) can be switched to
// h2 at runtime without a restart: the next hot-reload creates the registry so
// the h2 handlers stop answering 503.
func TestHotReloadSSH_CreatesRegistryWhenTransportSwitchesToH2(t *testing.T) {
	origProxy := service.ProxyService
	origSSH := handler.GetSSHServer()
	service.ProxyService = nil
	handler.SetSSHServer(nil)
	t.Cleanup(func() {
		if service.ProxyService != nil && service.ProxyService != origProxy {
			service.ProxyService.Stop()
		}
		service.ProxyService = origProxy
		handler.SetSSHServer(origSSH)
	})

	// Precondition: the startup configuration with no registry.
	require.Nil(t, service.ProxyService)

	// The transport is hot-reloaded to h2 (SSH still disabled). The registry
	// must now be created.
	hotReloadSSH(model.Config{
		PortForward: model.PortForwardConfig{
			Enabled:      false,
			Transport:    model.TransportH2,
			AllowedPorts: "3000-4000",
		},
	}, 20000)

	require.NotNil(t, service.ProxyService,
		"switching transport to h2 must create the registry so the h2 handlers stop returning 503")

	// The configured whitelist must be applied, not NewProxyRegistry's fallback.
	require.True(t, service.ProxyService.IsPortAllowed(3500),
		"the configured allowed_ports must be applied to the freshly created registry")
	require.False(t, service.ProxyService.IsPortAllowed(8080),
		"the 1024-65535 fallback must not survive the hot-reload")
}

// TestHotReloadSSH_DoesNotCreateRegistryForDisabledSSHTransport is the negative
// control for the creation path: the same call with the disabled cell must NOT
// create a registry (it would only start a health-check goroutine and restore
// rows for a dead feature).
func TestHotReloadSSH_DoesNotCreateRegistryForDisabledSSHTransport(t *testing.T) {
	origProxy := service.ProxyService
	origSSH := handler.GetSSHServer()
	service.ProxyService = nil
	handler.SetSSHServer(nil)
	t.Cleanup(func() {
		service.ProxyService = origProxy
		handler.SetSSHServer(origSSH)
	})

	hotReloadSSH(model.Config{
		PortForward: model.PortForwardConfig{Enabled: false, Transport: model.TransportSSH},
	}, 20000)

	require.Nil(t, service.ProxyService,
		"the disabled cell must not create a registry")
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
