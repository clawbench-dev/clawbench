//go:build windows

package mcp

import (
	"os"
	"os/exec"
	"strconv"
)

// setProbeProcessGroup is a no-op: Windows has no process groups to join.
// Tree termination is handled by killProbeProcessGroup (taskkill /T).
func setProbeProcessGroup(_ *exec.Cmd) {}

// killProbeProcessGroup terminates proc and every descendant.
//
// proc.Kill() alone leaves children orphaned and holding the inherited pipes,
// which keeps cmd.Wait() blocked — the same hang the Unix process-group kill
// prevents.
func killProbeProcessGroup(proc *os.Process) {
	if proc == nil || proc.Pid <= 0 {
		return
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(proc.Pid), "/T", "/F").Run()
	_ = proc.Kill()
}
