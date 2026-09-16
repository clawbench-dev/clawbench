package ai

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// pidOfSelf returns this test process's PID for the liveness probe.
func pidOfSelf(t *testing.T) int {
	t.Helper()
	return os.Getpid()
}

// newLiveConnForTest returns a connection whose Done() channel is open,
// modeling a peer that has not disconnected. That is exactly the state the
// old liveness check could not see past: the pipe looks fine while the process
// behind it is gone. Reuses newAliveACPConn from acp_refactor_test.go.
func newLiveConnForTest(t *testing.T) *acp.ClientSideConnection {
	t.Helper()
	conn, pw := newAliveACPConn(t)
	t.Cleanup(func() { _ = pw.Close() })
	return conn
}

// ---------------------------------------------------------------------------
// Process-existence liveness probe
//
// isAliveLocked used to rely solely on the SDK connection's Done() channel,
// which never fires when the agent process exits while its stdout/stderr write
// ends stay open (typically an orphaned grandchild holding them). Such a
// connection looked reusable forever and every prompt failed against a process
// that no longer existed.
//
// NOTE: this check does NOT detect a wedged-but-running agent (event loop
// stopped, process alive) — that case is caught after the fact by the
// empty-turn detection and its respawn. These tests only cover the
// process-gone case.
// ---------------------------------------------------------------------------

func TestAgentProcessAlive_CurrentProcess(t *testing.T) {
	// This test process obviously exists.
	assert.True(t, agentProcessAlive(pidOfSelf(t)))
}

func TestAgentProcessAlive_RejectsNonPositivePID(t *testing.T) {
	assert.False(t, agentProcessAlive(0))
	assert.False(t, agentProcessAlive(-1))
}

// A PID that has been reaped must not be reported as alive. PID 2^31-1 is
// outside the default Linux pid_max and never allocated in practice.
func TestAgentProcessAlive_DeadPID(t *testing.T) {
	assert.False(t, agentProcessAlive(1<<31-1), "an unallocated PID must not be reported alive")
}

// A started-then-reaped child is the realistic shape of the bug: the process is
// gone, but the connection object still holds its *os.Process.
func TestIsAliveLocked_ProcessExited(t *testing.T) {
	cmd := exec.Command("true")
	require.NoError(t, cmd.Start())
	require.NoError(t, cmd.Wait()) // reaped: the PID is now gone

	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")
	conn.cmd = cmd
	conn.alive = true
	// A non-nil connection whose Done() channel has not fired models the
	// "pipe still open" state the old check could not see past.
	conn.conn = newLiveConnForTest(t)

	conn.mu.Lock()
	alive := conn.isAliveLocked()
	conn.mu.Unlock()

	assert.False(t, alive, "a connection whose process has exited must not be considered alive")
}

// A live process must keep the connection usable — the probe must not turn a
// healthy but quiet agent into a respawn on every prompt.
func TestIsAliveLocked_ProcessRunning(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")
	conn.cmd = cmd
	conn.alive = true
	conn.conn = newLiveConnForTest(t)

	conn.mu.Lock()
	alive := conn.isAliveLocked()
	conn.mu.Unlock()

	assert.True(t, alive, "a running agent process must keep the connection alive")
}

// No subprocess recorded (e.g. a unit-test connection) must not be reported
// dead merely because c.cmd is nil — only the SDK connection decides.
func TestIsAliveLocked_NoProcessRecorded(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")
	conn.cmd = nil
	conn.alive = true
	conn.conn = newLiveConnForTest(t)

	conn.mu.Lock()
	alive := conn.isAliveLocked()
	conn.mu.Unlock()

	assert.True(t, alive, "without a recorded process the SDK connection alone decides")
}

// A nil SDK connection is dead regardless of the process state.
func TestIsAliveLocked_NilConn(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")
	conn.conn = nil

	conn.mu.Lock()
	alive := conn.isAliveLocked()
	conn.mu.Unlock()

	assert.False(t, alive)
}
