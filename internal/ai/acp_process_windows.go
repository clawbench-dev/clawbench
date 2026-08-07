//go:build windows

package ai

import (
	"os"
	"os/exec"
	"strconv"
)

// setProcessGroup is a no-op on Windows. POSIX process groups don't exist;
// process-tree cleanup is handled by killProcessGroup via taskkill /T.
func setProcessGroup(cmd *exec.Cmd) {
	// Windows: no POSIX process groups
}

// killProcessGroup kills the process and its entire process tree.
// ACP agents like Claude are spawned via npx (npx → node → claude); killing
// only the npx parent leaves node and claude alive — leaking memory and
// holding the stdout/stderr pipes open, which makes cmd.Wait() hang.
// taskkill /T terminates the process and all its descendants recursively.
func killProcessGroup(proc *os.Process) {
	// /F force-terminates, /T kills the whole tree, /PID targets the process.
	_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(proc.Pid)).Run()
	// Fallback: kill the parent directly if taskkill failed or already exited.
	_ = proc.Kill()
}
