package ai

import (
	"os/exec"
	"runtime"
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

	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())

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
			conn.reapProcess(cmd)
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

	conn.reapProcess(nil)         // nil cmd — no-op
	conn.reapProcess(&exec.Cmd{}) // cmd with nil Process — no-op

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

	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())

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
		conn.mu.Unlock()
		conn.reapProcess(cur)
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
