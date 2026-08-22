package ai

import (
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// reapProcess — unified, serialized process kill+reap (regression tests for the
// idle-sweep vs. new-prompt concurrent Wait deadlock).
// ---------------------------------------------------------------------------

// newTestSleep starts a `sleep` process in its own process group (matching
// production spawnLocked) and returns it. The test must reap/kill it.
func newTestSleep(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	setProcessGroup(cmd)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { killProcessGroup(cmd.Process) })
	return cmd
}

// stdoutFilterClosed reports whether f.Close() has been called.
func stdoutFilterClosed(f *acpStdoutFilter) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// TestRefactor_ConcurrentReapProcess_NoDeadlock reproduces the race that hung
// a session: the ACP idle sweep and a newly arriving prompt both try to reap
// the SAME agent process at the same time. exec.Cmd.Wait()/Process.Wait() are
// not safe for concurrent invocation and can deadlock, leaving GetOrCreateConn
// (and thus the whole session) blocked forever. reapProcess must serialize all
// reaps via procMu so they never overlap and every caller returns promptly.
func TestRefactor_ConcurrentReapProcess_NoDeadlock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping: process reaping semantics differ on Windows")
	}
	orig := crashDiagWaitTimeout
	crashDiagWaitTimeout = 2 * time.Second
	defer func() { crashDiagWaitTimeout = orig }()

	agent := &model.Agent{ID: "test-reap-race", Backend: "acp-stdio", AcpCommand: "sleep"}
	conn := newACPConn(agent, "test-reap-race")

	cmd := newTestSleep(t)

	conn.mu.Lock()
	conn.cmd = cmd
	conn.mu.Unlock()

	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			conn.reapProcess(cmd, nil)
		}()
	}
	close(start)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		// All reapProcess calls returned — no deadlock.
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent reapProcess calls deadlocked on the same process")
	}
}

// TestRefactor_ReapProcess_NilOrNoProcess verifies reapProcess is a safe no-op
// when given a nil Cmd or a Cmd with no process, and never blocks.
func TestRefactor_ReapProcess_NilOrNoProcess(t *testing.T) {
	agent := &model.Agent{ID: "test-reap-nil", Backend: "acp-stdio", AcpCommand: "sleep"}
	conn := newACPConn(agent, "test-reap-nil")

	conn.reapProcess(nil, nil)         // nil cmd — no-op
	conn.reapProcess(&exec.Cmd{}, nil) // cmd with nil Process — no-op

	// Should not panic or block.
}

// TestRefactor_ReapProcess_SerializedAgainstSpawnKill verifies that two
// different code paths that tear down the same process (e.g. killAndMarkDead
// from the idle sweep and spawnLocked's old-process teardown) serialize via
// procMu instead of racing on Wait. Both complete within a bounded window.
func TestRefactor_ReapProcess_SerializedAgainstSpawnKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping: process reaping semantics differ on Windows")
	}
	orig := crashDiagWaitTimeout
	crashDiagWaitTimeout = 2 * time.Second
	defer func() { crashDiagWaitTimeout = orig }()

	agent := &model.Agent{ID: "test-reap-serialize", Backend: "acp-stdio", AcpCommand: "sleep"}
	conn := newACPConn(agent, "test-reap-serialize")

	cmd := newTestSleep(t)

	conn.mu.Lock()
	conn.cmd = cmd
	conn.alive = true
	conn.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	// Path A: idle sweep → killAndMarkDead (preserves acpSID).
	go func() {
		defer wg.Done()
		conn.killAndMarkDead()
	}()
	// Path B: new prompt → spawnLocked teardown (reap the current process).
	go func() {
		defer wg.Done()
		conn.mu.Lock()
		cur := conn.cmd
		filter := conn.stdoutFilter
		conn.mu.Unlock()
		conn.reapProcess(cur, filter)
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		// Both paths completed without deadlock.
	case <-time.After(5 * time.Second):
		t.Fatal("killAndMarkDead and reapProcess deadlocked on the same process")
	}

	conn.mu.Lock()
	alive := conn.alive
	conn.mu.Unlock()
	require.False(t, alive, "connection should be marked dead after reap")
}

// TestRefactor_ReapProcess_DoesNotCloseRespawnedFilter guards the filter-binding
// contract: reapProcess must close ONLY the stdout filter tied to the process
// being reaped (oldFilter), never the connection's current c.stdoutFilter. In
// the idle-sweep vs. new-prompt race, the sweep captures the OLD filter, then a
// respawn installs a NEW filter before the sweep's reap runs. If reapProcess
// closed the connection's current filter instead, it would break the freshly
// respawned session's stdout. This test simulates that exact interleaving.
func TestRefactor_ReapProcess_DoesNotCloseRespawnedFilter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping: process reaping semantics differ on Windows")
	}
	orig := crashDiagWaitTimeout
	crashDiagWaitTimeout = 2 * time.Second
	defer func() { crashDiagWaitTimeout = orig }()

	agent := &model.Agent{ID: "test-reap-filter", Backend: "acp-stdio", AcpCommand: "sleep"}
	conn := newACPConn(agent, "test-reap-filter")

	// Old process + its filter (belongs to the process the sweep is reaping).
	oldCmd := newTestSleep(t)
	oldFilter := newACPStdoutFilter(strings.NewReader(""))

	// The connection currently points at the OLD process/filter.
	conn.mu.Lock()
	conn.cmd = oldCmd
	conn.stdoutFilter = oldFilter
	conn.mu.Unlock()

	// Simulate the respawn that happens between the sweep capturing oldFilter
	// and the sweep's reapProcess actually running: a NEW process + filter is
	// installed on the connection.
	newCmd := newTestSleep(t)
	newFilter := newACPStdoutFilter(strings.NewReader(""))
	conn.mu.Lock()
	conn.cmd = newCmd
	conn.stdoutFilter = newFilter
	conn.mu.Unlock()

	// The sweep's reap now runs with the OLD filter binding it captured.
	conn.reapProcess(oldCmd, oldFilter)

	require.True(t, stdoutFilterClosed(oldFilter),
		"old process's filter should be closed by reapProcess")
	require.False(t, stdoutFilterClosed(newFilter),
		"respawned process's filter must NOT be closed by reaping the old process")

	conn.mu.Lock()
	still := conn.stdoutFilter
	conn.mu.Unlock()
	require.Same(t, newFilter, still, "connection must still own the new filter")
}
