//go:build !windows

package mcp

import (
	"os"
	"os/exec"
	"syscall"
)

// setProbeProcessGroup puts the server in its own process group so the whole
// tree can be killed at once.
//
// Without this, killing the leader leaves any child it spawned (the interpreter
// a launcher such as `uvx` or `npx` starts) alive. That child inherited our
// stdout/stderr pipes, so it keeps them open and cmd.Wait() blocks forever —
// which is exactly what turned a successful probe into a 15s client timeout.
func setProbeProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProbeProcessGroup SIGKILLs the group led by proc (group id == leader pid,
// guaranteed by setProbeProcessGroup), then the leader itself as a fallback.
func killProbeProcessGroup(proc *os.Process) {
	if proc == nil || proc.Pid <= 0 {
		return
	}
	_ = syscall.Kill(-proc.Pid, syscall.SIGKILL)
	_ = proc.Kill()
}
