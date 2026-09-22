//go:build windows

package service

import (
	"os"
	"os/exec"
	"strconv"
)

// killTaskScriptProcessGroup kills the entire process tree rooted at the given
// process. On Windows, proc.Kill() only terminates the parent, leaving child
// processes orphaned and holding the inherited pipes open. taskkill /T /F
// recursively terminates the tree, matching the Unix process-group kill.
func killTaskScriptProcessGroup(proc *os.Process) {
	if proc.Pid > 0 {
		// taskkill /T: kill the process and all children; /F: force.
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(proc.Pid), "/T", "/F").Run()
	}
	_ = proc.Kill()
}
