// These tests cover the parts of the frp package that do not depend on the
// fatedier/frp client library (types.go), so they run under BOTH build
// variants — including `nofrp`, where the manager itself is a stub. Keeping
// them untagged is what makes the no-FRP build actually tested rather than
// merely compiled.
package frp

import (
	"sync"
	"testing"

	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
)

// --- FormatRemoteURL ---

func TestFormatRemoteURL(t *testing.T) {
	tests := []struct {
		serverAddr string
		remotePort int
		want       string
	}{
		{"120.26.168.245", 20050, "http://120.26.168.245:20050"},
		{"localhost", 8080, "http://localhost:8080"},
		{"192.168.1.1", 443, "http://192.168.1.1:443"},
	}
	for _, tt := range tests {
		got := FormatRemoteURL(tt.serverAddr, tt.remotePort)
		assert.Equal(t, tt.want, got)
	}
}

// --- parsePortFromAddr ---

func TestParsePortFromAddr(t *testing.T) {
	tests := []struct {
		addr string
		want int
	}{
		{"120.26.168.245:20050", 20050},
		{"localhost:8080", 8080},
		{"127.0.0.1:443", 443},
		{"[::1]:8080", 8080},
		{"invalid", 0},
		{"host:nonnumeric", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := parsePortFromAddr(tt.addr)
		assert.Equal(t, tt.want, got, "parsePortFromAddr(%q)", tt.addr)
	}
}

// --- State constants ---

func TestStateConstants(t *testing.T) {
	assert.Equal(t, State("disabled"), StateDisabled)
	assert.Equal(t, State("starting"), StateStarting)
	assert.Equal(t, State("running"), StateRunning)
	assert.Equal(t, State("failed"), StateFailed)
	assert.Equal(t, State("stopped"), StateStopped)
}

// --- ProxyPhaseRunning constant ---

func TestProxyPhaseRunning(t *testing.T) {
	assert.Equal(t, "running", ProxyPhaseRunning)
}

// --- emitEvent tests ---

func TestEmitEvent_NilManager(t *testing.T) {
	// ws.GetManager() returns nil in tests — emitEvent should not panic
	assert.NotPanics(t, func() {
		emitEvent("stopped")
	})
}

func TestEmitEvent_WithData(t *testing.T) {
	assert.NotPanics(t, func() {
		emitEvent("running", map[string]any{
			"remote_url":  "http://server:20050",
			"remote_port": 20050,
		})
	})
}

func TestEmitEvent_WithWSManager(t *testing.T) {
	// Set up a real ws.Manager to exercise the broadcast path
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-frp", "")

	emitEvent("running", map[string]any{
		"remote_url":  "http://server:20050",
		"remote_port": 20050,
	})

	// Verify the event was broadcast
	buffered := sub.GetBufferedEvents()
	assert.Len(t, buffered, 1)
	assert.Equal(t, "frp_status", buffered[0].Event)

	data, ok := buffered[0].Data.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "running", data["status"])
	assert.Equal(t, "http://server:20050", data["remote_url"])
}

func TestEmitEvent_StoppedWithWSManager(t *testing.T) {
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(nil)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "test-client-frp-stopped", "")

	emitEvent("stopped")

	buffered := sub.GetBufferedEvents()
	assert.Len(t, buffered, 1)
	assert.Equal(t, "frp_status", buffered[0].Event)

	data, ok := buffered[0].Data.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "stopped", data["status"])
}
