//go:build !windows

package handler

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the command in its own process group so the
// entire group (including children) can be killed on timeout or disconnect.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the entire process group.
func killProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
