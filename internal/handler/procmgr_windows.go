//go:build windows

package handler

import "os/exec"

// setProcessGroup is a no-op on Windows; process groups are not
// managed via SysProcAttr.Setpgid.
func setProcessGroup(_ *exec.Cmd) {}

// killProcessGroup is a no-op on Windows; there is no syscall.Kill
// equivalent for process groups. The process will be cleaned up by
// cmd.Process.Kill() in the caller if needed.
func killProcessGroup(_ int) {}
