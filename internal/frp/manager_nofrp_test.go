//go:build nofrp

package frp

import (
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The no-FRP build must fail fast rather than pretend. These tests pin that
// contract: Available() is false, and every operation that could imply a tunnel
// exists returns an explicit error instead of silently doing nothing.

func TestNoFRP_AvailableIsFalse(t *testing.T) {
	assert.False(t, Available(), "a `nofrp` build must report FRP as unavailable")
}

func TestNoFRP_StartFailsFast(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "frp.example.com", ServerPort: 7000}
	m := NewManager(cfg, 20000, 20001)

	err := m.Start()
	require.Error(t, err, "Start must fail on a no-FRP build, not silently no-op")
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Contains(t, err.Error(), "nofrp", "the error should name the build tag that removed the feature")
}

func TestNoFRP_StatusReportsDisabledWithMessage(t *testing.T) {
	cfg := model.FRPConfig{Enabled: true, ServerAddr: "frp.example.com"}
	m := NewManager(cfg, 20000, 0)

	status := m.Status()
	assert.False(t, status.Enabled, "a no-FRP build can never report enabled")
	assert.False(t, status.Running)
	assert.Equal(t, StateDisabled, status.State)
	assert.NotEmpty(t, status.Message, "the status should explain why FRP is unavailable")
}

func TestNoFRP_StopIsSafe(t *testing.T) {
	m := NewManager(model.FRPConfig{Enabled: true}, 20000, 0)
	// Stop must not panic even though nothing was started.
	assert.NotPanics(t, func() { m.Stop() })
}

func TestNoFRP_ReconfigureFailsFast(t *testing.T) {
	m := NewManager(model.FRPConfig{Enabled: true}, 20000, 0)

	needsRestart, err := m.Reconfigure(model.FRPConfig{Enabled: true}, 20000, 20001)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.False(t, needsRestart)
}

func TestNoFRP_OnReadyNeverSignals(t *testing.T) {
	m := NewManager(model.FRPConfig{Enabled: true}, 20000, 0)

	// The channel is never sent to, so a caller's own timeout is the only exit.
	// Assert that directly instead of trusting the absence of a value.
	select {
	case s := <-m.OnReady():
		t.Fatalf("OnReady must never deliver a status on a no-FRP build, got %+v", s)
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}
