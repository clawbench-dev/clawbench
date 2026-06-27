//go:build windows

package ai

import (
	"os"
	"os/exec"
)

// setProcessGroup is a no-op on Windows. POSIX process groups don't exist;
// Job Objects would be needed for proper child cleanup. For now, Kill() on
// the parent process is sufficient for most cases.
func setProcessGroup(cmd *exec.Cmd) {
	// Windows: no POSIX process groups
}

// killProcessGroup kills the process. On Windows, there are no POSIX process
// groups, so we can only kill the parent. Child processes spawned by npx may
// survive; cmd.Wait() should still complete once the parent exits.
func killProcessGroup(proc *os.Process) {
	_ = proc.Kill()
}
