package ai

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Failed-spawn state cleanup
// ---------------------------------------------------------------------------
//
// A spawn that fails at Initialize must leave the connection explicitly DEAD.
//
// The bug this guards: spawnLocked assigns c.cmd/c.conn/c.client/c.alive/
// c.startedAt only AFTER Initialize succeeds, and the kill-old-process step
// clears c.cmd. So a failed spawn left the PREVIOUS connection's fields in
// place with c.cmd == nil. isAliveLocked() skips its process probe when c.cmd
// is nil, so a stale connection whose Done() had not yet fired (the documented
// "orphaned grandchild holds the stdout write end" case) reported ALIVE, and
// the next ensureAliveWithSession took its early-return branch — reusing a
// connection that could never serve a prompt.
//
// Reachability of the two preconditions is documented in the source itself:
// Initialize failure is the ordinary case this file exercises, and a
// Done()-still-open connection is what isAliveLocked's own comment describes.

// spawnCounterEnv names the env var carrying the counter file path to the
// helper process. Using an env var (rather than a shell script) keeps the fake
// agent portable: Windows cannot execute a `#!/bin/sh` file, so a script-based
// agent made these tests fail there.
const spawnCounterEnv = "CLAWBENCH_TEST_SPAWN_COUNTER"

// TestSpawnCountingAgentHelper is not a test on its own. It is the fake agent
// spawned by newSpawnCountingAgent: it records one launch and exits without
// ever speaking ACP, so Initialize fails fast (the pipe closes).
func TestSpawnCountingAgentHelper(t *testing.T) {
	path := os.Getenv(spawnCounterEnv)
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		_, _ = f.WriteString("launched\n")
		_ = f.Close()
	}
	os.Exit(0)
}

// newSpawnCountingAgent returns an agent whose command appends one line to a
// counter file each time it is launched, then exits immediately. Initialize
// therefore fails fast, and the counter gives a reliable record of spawn
// ATTEMPTS.
//
// The fake agent re-executes the test binary with a helper -test.run filter
// instead of using a shell script, so the same test works on Windows.
//
// c.cmd is not usable as a probe here: spawnLocked only assigns it on a
// successful spawn, so a failed attempt leaves it unchanged.
func newSpawnCountingAgent(t *testing.T) (*model.Agent, func() int) {
	t.Helper()

	dir := t.TempDir()
	counter := filepath.Join(dir, "spawn-count")
	t.Setenv(spawnCounterEnv, counter)

	count := func() int {
		data, err := os.ReadFile(counter)
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "launched")
	}
	return &model.Agent{
		ID:         "spawn-failure-test",
		Backend:    "acp-stdio",
		AcpCommand: os.Args[0] + " -test.run=^TestSpawnCountingAgentHelper$",
	}, count
}

// TestSpawnLocked_FailureLeavesConnectionDead pins the post-condition: after a
// failed spawn the connection must not look reusable.
func TestSpawnLocked_FailureLeavesConnectionDead(t *testing.T) {
	agent, _ := newSpawnCountingAgent(t)
	conn := newACPConn(agent, "spawn-failure-sid")

	// A stale connection whose Done() has NOT fired, plus an already-reaped
	// process so the liveness probe reports the connection dead and a spawn is
	// actually attempted.
	stale, stalePW := newAliveACPConn(t)
	defer stalePW.Close()

	deadProc := exec.Command("true")
	require.NoError(t, deadProc.Start())
	require.NoError(t, deadProc.Wait()) // reaped: PID gone, pipe still open

	conn.mu.Lock()
	conn.alive = true
	conn.conn = stale
	conn.cmd = deadProc
	conn.acpSID = "sid-must-survive"
	conn.cwd = t.TempDir()
	conn.mu.Unlock()

	_, err := conn.ensureAliveWithSession(context.Background(), conn.cwd)
	require.Error(t, err, "the fake agent cannot speak ACP, so the spawn must fail")

	conn.mu.Lock()
	alive := conn.alive
	gotConn := conn.conn
	gotCmd := conn.cmd
	gotSID := conn.acpSID
	startedAt := conn.startedAt
	conn.mu.Unlock()

	assert.False(t, alive,
		"a failed spawn must not leave alive=true: isAliveLocked() skips its process "+
			"probe when cmd is nil, so the next ensureAliveWithSession would reuse a dead connection")
	assert.Nil(t, gotConn, "a failed spawn must not leave the previous SDK connection attached")
	assert.Nil(t, gotCmd, "a failed spawn must not leave a process handle attached")
	assert.True(t, startedAt.IsZero(),
		"startedAt must be reset so crash diagnostics do not report the previous process's lifetime")

	// acpSID is deliberately preserved: the next attempt must still be able to
	// recover the session via ResumeSession.
	assert.Equal(t, "sid-must-survive", gotSID,
		"acpSID must survive a failed spawn so the session can be resumed")
}

// TestEnsureAliveWithSession_RespawnsAfterFailedSpawn is the behavioral
// counterpart: the call AFTER a failed spawn must attempt a new spawn instead
// of early-returning on the stale connection.
func TestEnsureAliveWithSession_RespawnsAfterFailedSpawn(t *testing.T) {
	agent, spawns := newSpawnCountingAgent(t)
	conn := newACPConn(agent, "spawn-retry-sid")

	stale, stalePW := newAliveACPConn(t)
	defer stalePW.Close()

	deadProc := exec.Command("true")
	require.NoError(t, deadProc.Start())
	require.NoError(t, deadProc.Wait())

	conn.mu.Lock()
	conn.alive = true
	conn.conn = stale
	conn.cmd = deadProc
	conn.acpSID = "sid-must-survive"
	conn.cwd = t.TempDir()
	conn.mu.Unlock()

	// First call: the spawn is attempted and fails.
	_, err1 := conn.ensureAliveWithSession(context.Background(), conn.cwd)
	require.Error(t, err1)
	require.Equal(t, 1, spawns(), "the first call must attempt exactly one spawn")

	// Second call: must spawn AGAIN. Pre-fix this early-returned (spawns stayed
	// at 1) because the connection still looked alive.
	_, err2 := conn.ensureAliveWithSession(context.Background(), conn.cwd)
	require.Error(t, err2, "the fake agent still cannot speak ACP")
	assert.Equal(t, 2, spawns(),
		"a failed spawn must not be treated as a live connection: the next call has to respawn")
}

// TestSpawnLocked_FailureKeepsSessionRecoverable guards the deliberate
// asymmetry with close(): a failed spawn preserves acpSID (so the session can
// be resumed) while still clearing liveness.
func TestSpawnLocked_FailureKeepsSessionRecoverable(t *testing.T) {
	agent, _ := newSpawnCountingAgent(t)
	conn := newACPConn(agent, "spawn-recover-sid")

	conn.mu.Lock()
	conn.alive = false
	conn.acpSID = "resume-me"
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	_, err := conn.ensureAliveWithSession(context.Background(), t.TempDir())
	require.Error(t, err)

	conn.mu.Lock()
	gotSID := conn.acpSID
	alive := conn.alive
	startedZero := conn.startedAt.IsZero()
	conn.mu.Unlock()

	assert.Equal(t, "resume-me", gotSID, "acpSID must be preserved for ResumeSession recovery")
	assert.False(t, alive)
	assert.True(t, startedZero, "startedAt must be zeroed after a failed spawn")
}
