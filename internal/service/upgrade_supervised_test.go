package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- performSupervisedUpgrade ---
//
// The replacement primitive itself (rename + cross-device fallback) is
// platform.ReplaceBinary and is covered by internal/platform/replace_test.go.
// These tests cover the service-level behavior around it: it must replace the
// target and only then trigger shutdown, and must not shut down when the
// replacement failed (shutting down with a failed swap would take the service
// down with no new binary in place).

func TestPerformSupervisedUpgrade_Success(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(newPath, []byte("new-binary"), 0o600))
	require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o600))

	shutdownCalled := false
	origShutdown := upgradeShutdownFunc
	upgradeShutdownFunc = func() { shutdownCalled = true }
	defer func() { upgradeShutdownFunc = origShutdown }()

	ResetUpgradeState()
	defer ResetUpgradeState()

	err := performSupervisedUpgrade(newPath, target)
	require.NoError(t, err)

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(data))
	assert.True(t, shutdownCalled, "shutdown func should be called to let supervisor restart")

	state := GetUpgradeState()
	assert.Equal(t, UpgradePhaseRestarting, state.Phase)
}

func TestPerformSupervisedUpgrade_ReplaceError(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "clawbench-missing")
	target := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o600))

	shutdownCalled := false
	origShutdown := upgradeShutdownFunc
	upgradeShutdownFunc = func() { shutdownCalled = true }
	defer func() { upgradeShutdownFunc = origShutdown }()

	ResetUpgradeState()
	defer ResetUpgradeState()

	err := performSupervisedUpgrade(newPath, target)
	assert.Error(t, err)
	assert.False(t, shutdownCalled, "shutdown should NOT be called when replacement fails")

	// The failed swap must leave the existing binary untouched: shutting down
	// (or the supervisor restarting) with no valid binary would be the
	// unrecoverable state this guards against.
	data, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "old-binary", string(data), "target must survive a failed replacement")
}

func TestPerformSupervisedUpgrade_NilShutdownFunc(t *testing.T) {
	dir := t.TempDir()
	newPath := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(newPath, []byte("new-binary"), 0o600))
	require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o600))

	origShutdown := upgradeShutdownFunc
	upgradeShutdownFunc = nil
	defer func() { upgradeShutdownFunc = origShutdown }()

	ResetUpgradeState()
	defer ResetUpgradeState()

	// Should not panic when no shutdown func is wired up.
	err := performSupervisedUpgrade(newPath, target)
	require.NoError(t, err)
}

// --- path selection: container always wins ---

// A container can never use the self-restart subprocess path: the runtime tears
// the namespace down when PID 1 exits, killing the upgrade-replace child before
// it finishes. Even when the supervisor probe says "not supervised" (k8s,
// runit, supervisord), a container must still be treated as supervised.
func TestResolveReplaceInPlace_ContainerForcesInPlaceEvenWhenProbeSaysNo(t *testing.T) {
	assert.True(t, resolveReplaceInPlace(true, false),
		"container + probe says not supervised => still in-place")
}

func TestResolveReplaceInPlace_ContainerAndProbeBothTrue(t *testing.T) {
	assert.True(t, resolveReplaceInPlace(true, true))
}

// Outside a container the probe is authoritative: a genuinely unsupervised
// deployment must use the self-restart subprocess path (nothing else restarts
// it), while a supervised one replaces in place.
func TestResolveReplaceInPlace_NonContainerFollowsProbe(t *testing.T) {
	assert.False(t, resolveReplaceInPlace(false, false),
		"not a container + not supervised => subprocess path")
	assert.True(t, resolveReplaceInPlace(false, true),
		"not a container + supervised (systemd) => in-place")
}

// The container override must survive a probe that reports false — this is the
// exact combination an unrecognized runtime (k8s/runit/supervisord) produces.
func TestResolveReplaceInPlace_ProbeOverrideCannotDefeatContainer(t *testing.T) {
	origContainer := upgradeIsContainer
	origProbe := upgradeIsSupervised
	defer func() {
		upgradeIsContainer = origContainer
		upgradeIsSupervised = origProbe
	}()

	upgradeIsContainer = func() bool { return true }
	upgradeIsSupervised = func() bool { return false } // unrecognized runtime

	got := resolveReplaceInPlace(upgradeIsContainer(), upgradeIsSupervised())
	assert.True(t, got, "container must force in-place even when the probe says false")
}
