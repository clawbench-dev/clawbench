//go:build !windows

package ai

import (
	"context"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTerminal_ProcessGroupIsolation(t *testing.T) {
	// Verify that CreateTerminal calls setProcessGroup, which puts the
	// terminal command in its own session without a controlling terminal.
	// This prevents commands like `sudo` from blocking on /dev/tty.
	client := NewClawBenchACPClient()
	cwd := t.TempDir()
	client.connRef = &ACPConn{cwd: cwd}

	resp, err := client.CreateTerminal(context.Background(), acp.CreateTerminalRequest{
		Command: "echo hello",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.TerminalId)

	// Command should complete quickly (not hang)
	ts, ok := client.terminals[resp.TerminalId]
	require.True(t, ok)

	select {
	case <-ts.done:
		// Command exited — good
	case <-time.After(5 * time.Second):
		t.Fatal("terminal command hung — process group isolation may not be working")
	}

	// Clean up
	_, _ = client.ReleaseTerminal(context.Background(), acp.ReleaseTerminalRequest{
		TerminalId: resp.TerminalId,
	})
}

func TestCreateTerminal_InteractiveCommandFailsFast(t *testing.T) {
	// Commands that try to access /dev/tty (the controlling terminal) should
	// fail fast with a non-zero exit code instead of blocking forever waiting
	// for input. This is the core fix: setProcessGroup + Setsid:true means the
	// child has no controlling terminal, so /dev/tty open fails with ENXIO.
	//
	// This covers the `sudo` password-prompt scenario: sudo reads from /dev/tty
	// when stdin is not a tty, so it fails with "no tty present" in a new session.
	client := NewClawBenchACPClient()
	cwd := t.TempDir()
	client.connRef = &ACPConn{cwd: cwd}

	resp, err := client.CreateTerminal(context.Background(), acp.CreateTerminalRequest{
		// `cat /dev/tty` is the canonical test: it tries to open the
		// controlling terminal. In a new session (Setsid), there's no
		// controlling terminal, so it fails immediately with ENXIO.
		Command: "cat /dev/tty",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.TerminalId)

	ts, ok := client.terminals[resp.TerminalId]
	require.True(t, ok)

	// cat /dev/tty should fail within seconds (no controlling terminal),
	// not hang forever waiting for terminal input.
	select {
	case <-ts.done:
		ts.mu.Lock()
		require.NotNil(t, ts.exitCode, "cat /dev/tty should have exited with a non-zero code")
		assert.NotEqual(t, 0, *ts.exitCode, "cat /dev/tty should fail (no controlling terminal) instead of blocking")
		ts.mu.Unlock()
	case <-time.After(10 * time.Second):
		t.Fatal("cat /dev/tty hung — expected fast failure due to no controlling terminal")
	}

	_, _ = client.ReleaseTerminal(context.Background(), acp.ReleaseTerminalRequest{
		TerminalId: resp.TerminalId,
	})
}

func TestCreateTerminal_CancelKillsProcessGroup(t *testing.T) {
	// Verify that cancelling the terminal context kills the entire process
	// group, not just the leader process. This prevents orphaned children.
	client := NewClawBenchACPClient()
	cwd := t.TempDir()
	client.connRef = &ACPConn{cwd: cwd}

	resp, err := client.CreateTerminal(context.Background(), acp.CreateTerminalRequest{
		Command: "sleep 60",
	})
	require.NoError(t, err)

	ts, ok := client.terminals[resp.TerminalId]
	require.True(t, ok)

	// Command should still be running
	select {
	case <-ts.done:
		t.Fatal("sleep 60 exited immediately — unexpected")
	default:
		// Still running — good
	}

	// Kill the terminal
	_, err = client.KillTerminal(context.Background(), acp.KillTerminalRequest{
		TerminalId: resp.TerminalId,
	})
	require.NoError(t, err)

	// Process should exit quickly
	select {
	case <-ts.done:
		// Good — killed. Verify it died from a signal (not clean exit).
		// On Unix, a SIGKILL'd process produces an ExitError with exit
		// code -1 (or 128+9=137 depending on OS), not a clean exit code 0.
		ts.mu.Lock()
		assert.NotNil(t, ts.exitCode, "sleep 60 should have an exit code after being killed")
		assert.NotEqual(t, 0, *ts.exitCode, "sleep 60 should exit with non-zero code (killed by signal)")
		ts.mu.Unlock()
	case <-time.After(5 * time.Second):
		t.Fatal("sleep 60 was not killed after KillTerminal")
	}

	_, _ = client.ReleaseTerminal(context.Background(), acp.ReleaseTerminalRequest{
		TerminalId: resp.TerminalId,
	})
}
