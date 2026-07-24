//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func killProcessForce(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

func setNewProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
