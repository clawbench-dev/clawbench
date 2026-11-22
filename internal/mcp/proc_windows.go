//go:build windows

package mcp

import (
	"os"
	"os/exec"
	"strconv"
)

// setProcessGroup is a no-op: Windows has no process groups to join.
// Tree termination is handled by killProcessGroup (taskkill /T).
func setProcessGroup(_ *exec.Cmd) {}

// killProcessGroup terminates proc and every descendant.
//
// proc.Kill() alone leaves children orphaned and holding the inherited pipes,
// which keeps cmd.Wait() blocked — the same hang the Unix process-group kill
// prevents.
func killProcessGroup(proc *os.Process) {
	if proc == nil || proc.Pid <= 0 {
		return
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(proc.Pid), "/T", "/F").Run()
	_ = proc.Kill()
}
