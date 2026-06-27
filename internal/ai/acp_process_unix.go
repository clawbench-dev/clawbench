//go:build !windows

package ai

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup puts the ACP process in its own process group so we can
// kill the entire tree (npx + child processes) when closing the connection.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the process group of the given process.
// When ACP agents are spawned via npx (which creates child processes),
// killing only the parent (npx) leaves children alive, holding pipes open
// and causing cmd.Wait() to hang. Killing the process group ensures all
// children are terminated, which closes the pipes and unblocks Wait().
//
// The process must have been started with Setpgid:true in SysProcAttr
// for this to work; otherwise the kill signal applies to the single process.
func killProcessGroup(proc *os.Process) {
	if proc.Pid > 0 {
		_ = syscall.Kill(-proc.Pid, syscall.SIGKILL)
	}
	_ = proc.Kill()
}
