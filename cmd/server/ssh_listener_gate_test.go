package main

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// TestShouldListenSSH pins the transport → listener mapping. It is the one
// predicate shared by startup and hot-reload, so a regression here would either
// leave 20001 listening after the operator switched to h2 (the switch would be
// a lie) or stop listening while the transport is still ssh (every client would
// lose its tunnel).
func TestShouldListenSSH(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		want      bool
	}{
		{name: "ssh listens", transport: model.TransportSSH, want: true},
		{name: "h2 does not listen", transport: model.TransportH2, want: false},
		// ApplyDefaults never leaves these behind, but the predicate is also
		// reachable from a hand-built config in tests and from hot-reload
		// before normalization — anything but ssh means h2.
		{name: "retired both does not listen", transport: model.TransportBoth, want: false},
		{name: "empty does not listen", transport: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := model.Config{}
			cfg.PortForward.Transport = tt.transport
			assert.Equal(t, tt.want, shouldListenSSH(cfg))
		})
	}
}
