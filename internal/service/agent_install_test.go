package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubInstallRun replaces the command runner for one test.
func stubInstallRun(t *testing.T, fn func(ctx context.Context, command string) (string, error)) {
	t.Helper()
	prev := agentInstallRun
	agentInstallRun = fn
	t.Cleanup(func() { agentInstallRun = prev })
}

// stubProbeRun replaces the probe runner for one test.
func stubProbeRun(t *testing.T, fn func(ctx context.Context, command string) (string, error)) {
	t.Helper()
	prev := SetAgentProbeRunForTest(fn)
	t.Cleanup(prev)
}

func TestProbeNpmPackage_Success(t *testing.T) {
	var got string
	stubProbeRun(t, func(_ context.Context, command string) (string, error) {
		got = command
		return "1.2.3\n", nil
	})

	err := ProbeNpmPackage(context.Background(), "@scope/pkg", "https://registry.example.com/npm")
	require.NoError(t, err)
	assert.Equal(t, "npm view @scope/pkg version --registry=https://registry.example.com/npm", got)
}

func TestProbeNpmPackage_AuthFailureIsTyped(t *testing.T) {
	stubProbeRun(t, func(context.Context, string) (string, error) {
		return "npm ERR! code E401\nnpm ERR! 401 Unauthorized", errors.New("exit status 1")
	})

	err := ProbeNpmPackage(context.Background(), "pkg", "https://registry.example.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNpmAuthRequired), "auth failure must be distinguishable from 404/network")
}

func TestProbeNpmPackage_NotFoundIsNotAuth(t *testing.T) {
	stubProbeRun(t, func(context.Context, string) (string, error) {
		return "npm ERR! code E404\nnpm ERR! 404 Not Found", errors.New("exit status 1")
	})

	err := ProbeNpmPackage(context.Background(), "pkg", "https://registry.example.com")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNpmAuthRequired))
}

func TestProbeNpmPackage_RejectsUnsafeInputsWithoutRunning(t *testing.T) {
	called := false
	stubProbeRun(t, func(context.Context, string) (string, error) {
		called = true
		return "", nil
	})

	require.Error(t, ProbeNpmPackage(context.Background(), "pkg; rm -rf /", "https://registry.example.com"))
	require.Error(t, ProbeNpmPackage(context.Background(), "pkg", "https://registry.example.com; rm -rf /"))
	require.Error(t, ProbeNpmPackage(context.Background(), "./local-package", "https://registry.example.com"))
	assert.False(t, called, "unsafe input must not reach a shell")
}

func stubInstallLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	prev := agentInstallLookPath
	agentInstallLookPath = fn
	t.Cleanup(func() { agentInstallLookPath = prev })
}

func stubInstallNodeRuntime(t *testing.T, fn func() (string, error)) {
	t.Helper()
	prev := agentInstallNodeRuntime
	agentInstallNodeRuntime = fn
	t.Cleanup(func() { agentInstallNodeRuntime = prev })
}

func TestInstallAgent_EmptyCommand(t *testing.T) {
	// A backend with no installer must be told apart from a failed install: the
	// UI shows "install manually" instead of an error banner.
	_, err := InstallAgent("claude", "   ")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAgentInstallEmptyCommand)
}

func TestInstallAgent_ReportsOutputAndDuration(t *testing.T) {
	stubInstallLookPath(t, func(string) (string, error) { return "/usr/bin/npm", nil })
	stubInstallRun(t, func(context.Context, string) (string, error) {
		return "added 1 package in 3s", nil
	})

	res, err := InstallAgent("claude", "npm install -g @anthropic-ai/claude-code")
	require.NoError(t, err)
	assert.Equal(t, "claude", res.Backend)
	assert.Equal(t, "added 1 package in 3s", res.Output)
	assert.Equal(t, "npm install -g @anthropic-ai/claude-code", res.Command)
	assert.GreaterOrEqual(t, res.DurationMs, int64(0))
}

func TestInstallAgent_CommandFailureCarriesOutput(t *testing.T) {
	stubInstallLookPath(t, func(string) (string, error) { return "/usr/bin/npm", nil })
	boom := errors.New("exit status 1")
	stubInstallRun(t, func(context.Context, string) (string, error) {
		return "npm ERR! code EACCES", boom
	})

	_, err := InstallAgent("claude", "npm install -g @anthropic-ai/claude-code")
	require.Error(t, err)

	var cmdErr *AgentInstallCommandError
	require.ErrorAs(t, err, &cmdErr)
	assert.Equal(t, "npm ERR! code EACCES", cmdErr.Output)
	assert.ErrorIs(t, err, boom)
}

// The missing-tool check is the one thing that turns "npm: command not found"
// buried in output into an actionable "install Node.js".
func TestInstallAgent_MissingNpmIsNamedUpFront(t *testing.T) {
	stubInstallLookPath(t, func(string) (string, error) { return "", errors.New("not found") })
	// The automatic Node.js install must not reach the network here; it also
	// fails, which is the case this test is about (nothing could provide npm).
	stubInstallNodeRuntime(t, func() (string, error) { return "", errors.New("download blocked") })
	ran := false
	stubInstallRun(t, func(context.Context, string) (string, error) {
		ran = true
		return "", nil
	})

	_, err := InstallAgent("claude", "npm install -g @anthropic-ai/claude-code")
	require.Error(t, err)

	var nodeErr *AgentInstallNodeMissingError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, "npm", nodeErr.Tool)
	assert.False(t, ran, "the command must not be attempted without its runtime")
}

// The runtime is installed rather than reported: a machine that has never had
// Node.js must be able to finish the one-click install.
func TestInstallAgent_InstallsMissingNodeThenRuns(t *testing.T) {
	lookups := 0
	stubInstallLookPath(t, func(string) (string, error) {
		lookups++
		if lookups == 1 {
			return "", errors.New("not found")
		}
		return "/home/user/.local/share/clawbench/node/bin/npm", nil
	})
	installed := false
	stubInstallNodeRuntime(t, func() (string, error) {
		installed = true
		return "ClawBench installed Node.js v22.11.0 (private copy: /x)", nil
	})
	stubInstallRun(t, func(context.Context, string) (string, error) {
		return "added 1 package in 3s", nil
	})

	res, err := InstallAgent("claude", "npm install -g @anthropic-ai/claude-code")
	require.NoError(t, err)
	assert.True(t, installed, "a missing runtime must be installed, not just reported")
	assert.Contains(t, res.Output, "Node.js v22.11.0")
	assert.Contains(t, res.Output, "added 1 package in 3s")
}

// A runtime that installs but still does not resolve is its own failure: the
// install command would otherwise fail with the same "command not found".
func TestInstallAgent_NodeInstalledButStillMissingIsReported(t *testing.T) {
	stubInstallLookPath(t, func(string) (string, error) { return "", errors.New("not found") })
	stubInstallNodeRuntime(t, func() (string, error) { return "", nil })
	ran := false
	stubInstallRun(t, func(context.Context, string) (string, error) {
		ran = true
		return "", nil
	})

	_, err := InstallAgent("claude", "npm install -g @anthropic-ai/claude-code")
	require.Error(t, err)
	var nodeErr *AgentInstallNodeMissingError
	require.ErrorAs(t, err, &nodeErr)
	assert.Contains(t, nodeErr.Error(), "still not on PATH")
	assert.False(t, ran)
}

func TestInstallAgent_MissingNpxIsNamedUpFront(t *testing.T) {
	stubInstallLookPath(t, func(string) (string, error) { return "", errors.New("not found") })
	stubInstallNodeRuntime(t, func() (string, error) { return "", errors.New("download blocked") })

	_, err := InstallAgent("zcode", "npx -y zcode-acp-server")
	var nodeErr *AgentInstallNodeMissingError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, "npx", nodeErr.Tool)
}

// A `curl … | bash` installer has no Node dependency, so the npm check must not
// fire for it — otherwise a machine without npm could not install cursor.
func TestInstallAgent_NonNodeCommandSkipsTheRuntimeCheck(t *testing.T) {
	looked := false
	stubInstallLookPath(t, func(string) (string, error) {
		looked = true
		return "", errors.New("not found")
	})
	stubInstallRun(t, func(context.Context, string) (string, error) { return "ok", nil })

	_, err := InstallAgent("cursor", "curl https://cursor.com/install -fsS | bash")
	require.NoError(t, err)
	assert.False(t, looked, "a shell-installer command must not be gated on npm/npx")
}

func TestInstallAgent_RefusesConcurrentRuns(t *testing.T) {
	stubInstallLookPath(t, func(string) (string, error) { return "/usr/bin/npm", nil })
	release := make(chan struct{})
	started := make(chan struct{})
	stubInstallRun(t, func(context.Context, string) (string, error) {
		close(started)
		<-release
		return "", nil
	})

	done := make(chan error, 1)
	go func() {
		_, err := InstallAgent("claude", "npm install -g a")
		done <- err
	}()
	<-started

	// A second install must be refused, not queued: two concurrent
	// `npm install -g` runs write the same global prefix.
	_, err := InstallAgent("codex", "npm install -g b")
	assert.ErrorIs(t, err, ErrAgentInstallBusy)

	close(release)
	require.NoError(t, <-done)

	// Wait for the first call to COMPLETELY finish before the follow-up.
	//
	// The busy gate is a package-level bool protected by a mutex, and the runner
	// stub is a package-level variable. Reading `<-done` only proves the goroutine
	// sent on the channel — its deferred `agentInstallRunning = false` and this
	// test's own `t.Cleanup` restore both race the next call, which under -race
	// shows up as an intermittent failure or as a stale gate that refuses the
	// follow-up for a reason unrelated to the assertion.
	require.Eventually(t, func() bool {
		agentInstallMu.Lock()
		defer agentInstallMu.Unlock()
		return !agentInstallRunning
	}, 2*time.Second, time.Millisecond, "the install slot must be released once the call returns")

	// The slot must be released afterwards, or the feature would work once.
	stubInstallRun(t, func(context.Context, string) (string, error) { return "", nil })
	_, err = InstallAgent("claude", "npm install -g a")
	assert.NoError(t, err)
}

func TestTailBuffer_KeepsTheEnd(t *testing.T) {
	// Installer output is diagnostics: the failure reason is at the END, so the
	// head is what must be dropped.
	b := &tailBuffer{cap: 8}
	_, _ = b.Write([]byte("PROGRESSPROGRESS"))
	_, _ = b.Write([]byte("DONE"))
	got := b.String()
	assert.Contains(t, got, "DONE")
	assert.NotContains(t, got, "PROGRESS")
	// Truncation is marked so the UI does not imply that is the whole output.
	assert.True(t, strings.HasPrefix(got, "…"))
}

func TestTailBuffer_SingleWriteLargerThanCap(t *testing.T) {
	b := &tailBuffer{cap: 4}
	_, _ = b.Write([]byte("0123456789"))
	assert.Equal(t, "…\n6789", b.String())
}

func TestTailBuffer_NoMarkerWhenItFits(t *testing.T) {
	b := &tailBuffer{cap: 64}
	_, _ = b.Write([]byte("  done  "))
	assert.Equal(t, "done", b.String())
}
