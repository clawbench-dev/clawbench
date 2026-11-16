//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the command in its own process group so a cancel can
// reap the whole tree.
//
// Without this, killing the shell leaves every child it spawned running: they
// inherited the stdout/stderr pipes, so the read side never sees EOF and the
// tool call hangs until its timeout instead of stopping.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid signals the group. ESRCH means it already exited, which
		// is not a cancellation failure.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return err
		}
		return nil
	}
}

// shellCommand returns the interpreter and its "run this string" flag.
func shellCommand() (string, string) {
	return "/bin/sh", "-c"
}
