//go:build nofrp

package frp

import (
	"errors"

	"clawbench/internal/model"
)

// ErrUnavailable is returned by Start when this build excludes FRP support.
// It is deliberately an explicit failure rather than a silent no-op: a user who
// configured an FRP tunnel on a no-FRP build should be told the feature is not
// present, not left wondering why the tunnel never comes up.
var ErrUnavailable = errors.New("FRP support is not included in this build (built with the `nofrp` tag)")

// Available reports whether this build includes FRP support. False here; the
// handler surfaces it so the settings panel can be hidden instead of offering
// a switch that can never take effect.
func Available() bool { return false }

// Manager is the no-FRP stand-in for the real manager. It keeps the same
// exported surface so cmd/server and internal/handler compile unchanged against
// either build variant — the difference is that every operation fails fast.
type Manager struct {
	cfg model.FRPConfig
}

// NewManager creates the no-FRP manager stub. Start will fail.
func NewManager(cfg model.FRPConfig, httpLocalPort, sshLocalPort int) *Manager {
	_ = httpLocalPort
	_ = sshLocalPort
	return &Manager{cfg: cfg}
}

// OnReady returns a channel that is never sent to: the tunnel can never become
// ready, so a caller selecting on it will hit its own timeout.
func (m *Manager) OnReady() <-chan Status {
	return make(chan Status)
}

// Status reports the unavailable state.
func (m *Manager) Status() Status {
	return Status{
		Enabled: false,
		Running: false,
		State:   StateDisabled,
		Message: ErrUnavailable.Error(),
	}
}

// Start always fails — this build has no FRP client to launch.
func (m *Manager) Start() error {
	return ErrUnavailable
}

// Stop is a no-op: nothing was ever started.
func (m *Manager) Stop() {}

// Reconfigure always fails for the same reason as Start.
func (m *Manager) Reconfigure(cfg model.FRPConfig, httpLocalPort, sshLocalPort int) (bool, error) {
	_ = cfg
	_ = httpLocalPort
	_ = sshLocalPort
	return false, ErrUnavailable
}
