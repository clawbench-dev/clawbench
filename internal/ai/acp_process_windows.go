//go:build windows

package ai

import (
	"os"
	"os/exec"
	"strconv"
)

// setProcessGroup is a no-op on Windows. POSIX process groups don't exist;
// taskkill /T is used instead to kill the entire process tree.
func setProcessGroup(cmd *exec.Cmd) {
	// Windows: no POSIX process groups
}

// killProcessGroup kills the entire process tree rooted at the given process.
// On Windows, proc.Kill() only terminates the parent process, leaving child
// processes (e.g. npx → node → claude) orphaned and leaking memory.
// taskkill /T /F recursively kills the entire tree, matching the behavior of
// the Unix killProcessGroup which sends SIGKILL to the process group.
func killProcessGroup(proc *os.Process) {
	if proc.Pid > 0 {
		// taskkill /T: kill the specified process and all child processes
		// taskkill /F: force terminate
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(proc.Pid), "/T", "/F").Run()
	}
	_ = proc.Kill()
}
