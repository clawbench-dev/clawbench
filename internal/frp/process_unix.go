//go:build unix

package frp

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the frpc process in its own process group so we can
// kill the entire tree when stopping the FRP tunnel.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the process group of the given process.
func killProcessGroup(pid int) {
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}
