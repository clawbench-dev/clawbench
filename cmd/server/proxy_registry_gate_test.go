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

// TestShouldCreateProxyRegistry pins the fact that the gate is now
// unconditionally true. The h2 stream tunnel handlers ride the main HTTP
// server, so disabling SSH must NOT nil the registry — that produced a blanket
// 503 from internal/handler/tunnel_stream.go:59 and tunnel_control.go:297.
//
// The gate used to have exactly one false cell: `transport: ssh` with SSH
// disabled. That cell is gone — model.ApplyDefaults pins the transport to
// "both", so the comparison that produced it was always true, and the
// comparison has been deleted rather than left as a tautology. Every row below
// therefore wants true, including the one that used to be the exception.
func TestShouldCreateProxyRegistry(t *testing.T) {
	tests := []struct {
		name string
		cfg  model.Config
	}{
		{
			name: "SSH enabled, transport unset (pre-T8 config)",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: true, Port: 20001}},
		},
		{
			name: "SSH disabled, transport unset — h2 tunnel still needs the registry",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: false}},
		},
		{
			name: "zero-value config",
			cfg:  model.Config{},
		},

		// --- port_forward.transport is pinned to "both", so it can no longer
		// --- select a "no registry" configuration. These are the old T8 rows.
		{
			name: "transport=ssh, SSH enabled",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: true, Transport: model.TransportSSH}},
		},
		{
			name: "transport=ssh, SSH disabled — the former false cell, now unreachable",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: false, Transport: model.TransportSSH}},
		},
		{
			name: "transport=h2, SSH disabled — h2 rides the main server",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: false, Transport: model.TransportH2}},
		},
		{
			name: "transport=h2, SSH enabled",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: true, Transport: model.TransportH2}},
		},
		{
			name: "transport=both, SSH disabled — the default",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: false, Transport: model.TransportBoth}},
		},
		{
			name: "transport=both, SSH enabled",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: true, Transport: model.TransportBoth}},
		},
		{
			name: "unrecognized transport still creates the registry",
			cfg:  model.Config{PortForward: model.PortForwardConfig{Enabled: false, Transport: "quic"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.True(t, shouldCreateProxyRegistry(tt.cfg),
				"the registry gate is unconditional — every config must create it")
		})
	}
}

// TestShouldCreateProxyRegistry_FormerFalseCellNowTrue is the focused guard for
// the exact configuration that used to be the only "no registry" cell. It is
// asserted directly (not via the table) so a regression that reintroduces a
// transport-based condition fails on the intended line.
func TestShouldCreateProxyRegistry_FormerFalseCellNowTrue(t *testing.T) {
	require.True(t, shouldCreateProxyRegistry(model.Config{
		PortForward: model.PortForwardConfig{Enabled: false, Transport: model.TransportSSH},
	}), "enabled:false + transport:ssh must no longer disable the registry — the transport is pinned to both")
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
		"the pinned transport must be 'both' — 'ssh' would disable the registry for installs that never set it")
	require.True(t,
		shouldCreateProxyRegistry(model.Config{
			PortForward: model.PortForwardConfig{Transport: model.DefaultPortForwardTransport},
		}),
		"a defaulted config (transport=both, SSH disabled) must still create the registry")
}

// TestProxyRegistryGate_NilRegistryYields503 wires the remaining chain together:
// with no registry (manually nil'd here — no longer reachable through
// configuration, see TestShouldCreateProxyRegistry_FormerFalseCellNowTrue) BOTH
// h2 tunnel endpoints must answer 503 rather than dereference a nil singleton.
//
// The gate no longer produces this state, but the handler nil guard is still
// load-bearing (a failed creation, a future refactor, a test double), so the
// handlers' behavior under nil is pinned independently of how the nil arose.
func TestProxyRegistryGate_NilRegistryYields503(t *testing.T) {
	origProxy := service.ProxyService
	service.ProxyService = nil
	t.Cleanup(func() { service.ProxyService = origProxy })

	// No config can ask for "no registry" any more.
	require.True(t, shouldCreateProxyRegistry(model.Config{
		PortForward: model.PortForwardConfig{Enabled: false, Transport: model.TransportSSH},
	}), "the former no-registry config now creates one")
	require.Nil(t, service.ProxyService, "no registry exists (manually nil'd)")

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
// registry-creation path in hotReloadSSH (cmd/server/main.go). It is kept even
// though the gate is now unconditional: the nil check remains the safety net
// for a server that somehow has no registry, and the whitelist application must
// not be skipped when the registry is created here.
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

	// Precondition: a server with no registry.
	require.Nil(t, service.ProxyService)

	hotReloadSSH(model.Config{
		PortForward: model.PortForwardConfig{
			Enabled:      false,
			Transport:    model.TransportH2,
			AllowedPorts: "3000-4000",
		},
	}, 20000)

	require.NotNil(t, service.ProxyService,
		"a hot-reload with no registry must create one so the h2 handlers stop returning 503")

	// The configured whitelist must be applied, not NewProxyRegistry's fallback.
	require.True(t, service.ProxyService.IsPortAllowed(3500),
		"the configured allowed_ports must be applied to the freshly created registry")
	require.False(t, service.ProxyService.IsPortAllowed(8080),
		"the 1024-65535 fallback must not survive the hot-reload")
}

// TestHotReloadSSH_CreatesRegistryForFormerDisabledCell is the updated negative
// control: the configuration that used to be the only no-registry cell
// (`enabled:false, transport:ssh`) now creates a registry like every other one,
// because the transport is pinned to "both".
func TestHotReloadSSH_CreatesRegistryForFormerDisabledCell(t *testing.T) {
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

	hotReloadSSH(model.Config{
		PortForward: model.PortForwardConfig{Enabled: false, Transport: model.TransportSSH},
	}, 20000)

	require.NotNil(t, service.ProxyService,
		"the former disabled cell now creates a registry — the transport is pinned to both")
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
