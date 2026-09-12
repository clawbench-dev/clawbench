package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/platform"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- shouldShortCircuit (pure) ---

func TestShouldShortCircuit(t *testing.T) {
	tests := []struct {
		name   string
		disk   string
		target string
		want   bool
	}{
		{name: "equal versions", disk: "0.93.0", target: "0.93.0", want: true},
		{name: "disk ahead of target", disk: "0.94.0", target: "0.93.0", want: true},
		{name: "disk behind target", disk: "0.92.0", target: "0.93.0", want: false},
		{name: "disk is dev build", disk: "5f4584b5", target: "0.93.0", want: false},
		{name: "disk is dev literal", disk: "dev", target: "0.93.0", want: false},
		{name: "empty disk version", disk: "", target: "0.93.0", want: false},
		{name: "empty target version", disk: "0.93.0", target: "", want: false},
		{name: "both empty", disk: "", target: "", want: false},
		{name: "v-prefixed disk", disk: "v0.93.0", target: "0.93.0", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldShortCircuit(tt.disk, tt.target))
		})
	}
}

// --- performUpgrade: version short-circuit ---

// shortCircuitHarness wires up the globals performUpgrade depends on, with a
// registry that reports targetVersion and counts tarball downloads.
type shortCircuitHarness struct {
	targetVersion string
	tarballHits   int
	restartCalls  int
}

// setup installs the harness. The returned server serves registry metadata.
func (h *shortCircuitHarness) setup(t *testing.T, diskVersion string) {
	t.Helper()

	dir := withTempDataDir(t)

	// A real file stands in for the on-disk binary, so resolveSelfBinary
	// accepts the recorded path.
	live := filepath.Join(dir, "bin", "clawbench")
	require.NoError(t, os.MkdirAll(filepath.Dir(live), 0o755))
	require.NoError(t, os.WriteFile(live, []byte("binary"), 0o755))
	require.NoError(t, WriteSelfPath(live))

	origVer := selfBinaryVersion
	selfBinaryVersion = func(string) (string, error) { return diskVersion, nil }
	t.Cleanup(func() { selfBinaryVersion = origVer })

	// Registry: metadata always served; a tarball request is a failure signal.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".tgz") {
			h.tarballHits++
			http.Error(w, "tarball must not be requested", http.StatusInternalServerError)
			return
		}
		resp := npmRegistryResponse{}
		resp.Version = h.targetVersion
		resp.Dist.Tarball = "https://registry.npmjs.org/test/-/test-" + h.targetVersion + ".tgz"
		resp.Dist.Integrity = "sha512-abcdef"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ts.Close)

	origClient := upgradeHTTPClient
	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{defaultBase: ts.URL}}
	t.Cleanup(func() { upgradeHTTPClient = origClient })

	origChina := platform.ChinaMirrorChecked.Load()
	platform.ChinaMirrorChecked.Store(2) // non-China → default base
	t.Cleanup(func() { platform.ChinaMirrorChecked.Store(origChina) })

	// Restart is the only action a short-circuit may take; replacing it keeps
	// the test from shutting down or spawning a real sentinel.
	origRestart := upgradeRestartFunc
	upgradeRestartFunc = func() { h.restartCalls++ }
	t.Cleanup(func() { upgradeRestartFunc = origRestart })

	ResetUpgradeState()
	t.Cleanup(ResetUpgradeState)
}

// TestPerformUpgrade_ShortCircuitsWhenDiskAtTarget is the core of the version
// short-circuit: the npm-updated binary is already on disk, so re-downloading
// it would waste a ~42MB transfer. Restarting is enough to pick it up.
func TestPerformUpgrade_ShortCircuitsWhenDiskAtTarget(t *testing.T) {
	h := &shortCircuitHarness{targetVersion: "0.99.0"}
	h.setup(t, "0.99.0")

	performUpgrade(context.Background())

	s := GetUpgradeState()
	assert.Equal(t, 0, h.tarballHits, "tarball must not be downloaded when disk is already at target")
	assert.Equal(t, 1, h.restartCalls, "restart must be triggered")
	assert.Equal(t, UpgradePhaseRestarting, s.Phase)
}

// TestPerformUpgrade_DownloadsWhenDiskBehind guards the short-circuit from
// firing when the on-disk binary is older than the target.
func TestPerformUpgrade_DownloadsWhenDiskBehind(t *testing.T) {
	h := &shortCircuitHarness{targetVersion: "0.99.0"}
	h.setup(t, "0.10.0")

	performUpgrade(context.Background())

	assert.Equal(t, 1, h.tarballHits, "an older disk binary must be downloaded")
	assert.Equal(t, 0, h.restartCalls, "no restart before the download completes")
}

// TestPerformUpgrade_ShortCircuitSkipsWhenVersionProbeFails verifies a failing
// probe degrades to the normal download path rather than short-circuiting on
// an unknown version.
func TestPerformUpgrade_ShortCircuitSkipsWhenVersionProbeFails(t *testing.T) {
	h := &shortCircuitHarness{targetVersion: "0.99.0"}
	h.setup(t, "0.99.0")

	// Override the probe installed by setup to fail.
	selfBinaryVersion = func(string) (string, error) { return "", fmt.Errorf("probe failed") }

	performUpgrade(context.Background())

	assert.Equal(t, 1, h.tarballHits, "a failed probe must fall back to downloading")
}

// TestPerformUpgrade_ShortCircuitSkippedWithoutRestartFunc verifies the
// short-circuit is not taken when no restart function is wired: it would
// otherwise report "restarting" and never actually restart, leaving the
// upgrade stuck. Falling through to the normal download path is the safe
// behavior.
func TestPerformUpgrade_ShortCircuitSkippedWithoutRestartFunc(t *testing.T) {
	h := &shortCircuitHarness{targetVersion: "0.99.0"}
	h.setup(t, "0.99.0")

	upgradeRestartFunc = nil // not wired

	performUpgrade(context.Background())

	assert.Equal(t, 0, h.restartCalls, "no restart func → no restart call")
	assert.Equal(t, 1, h.tarballHits, "must fall through to the normal download path")
}

// TestPerformUpgrade_SelfPathUnresolved verifies that when no usable path to
// the running binary can be found — the situation after a package manager
// replaced the package and the recorded path is gone too — the upgrade fails
// fast with an actionable code instead of a bare ENOENT during backup.
func TestPerformUpgrade_SelfPathUnresolved(t *testing.T) {
	dir := withTempDataDir(t) // empty data dir → no self-path record

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := npmRegistryResponse{}
		resp.Version = "99.0.0"
		resp.Dist.Tarball = "https://registry.npmjs.org/test/-/test-99.0.0.tgz"
		resp.Dist.Integrity = "sha512-abcdef"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ts.Close)

	origClient := upgradeHTTPClient
	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{defaultBase: ts.URL}}
	t.Cleanup(func() { upgradeHTTPClient = origClient })

	origChina := platform.ChinaMirrorChecked.Load()
	platform.ChinaMirrorChecked.Store(2)
	t.Cleanup(func() { platform.ChinaMirrorChecked.Store(origChina) })

	// No record, and os.Executable() points into a deleted retire directory —
	// exactly what a package manager replacement leaves behind.
	origExe := upgradeExecutable
	upgradeExecutable = func() (string, error) {
		return filepath.Join(dir, ".clawbench-gone", "bin", "clawbench"), nil
	}
	t.Cleanup(func() { upgradeExecutable = origExe })

	ResetUpgradeState()
	t.Cleanup(ResetUpgradeState)

	performUpgrade(context.Background())

	s := GetUpgradeState()
	require.Equal(t, UpgradePhaseFailed, s.Phase)
	assert.Equal(t, UpgradeErrSelfPathUnresolved, s.ErrorCode)
	assert.Contains(t, s.Error, "restart ClawBench")
}
