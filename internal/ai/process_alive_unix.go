//go:build !windows

package ai

import "syscall"

// agentProcessAlive reports whether the OS still has a process with this PID.
//
// Signal 0 performs the kernel's existence and permission checks without
// actually delivering a signal, which is the standard liveness probe on Unix.
// A non-nil error means the process is gone (ESRCH) or we may not signal it
// (EPERM, which still implies it exists — treated as gone here because an
// unkillable agent cannot be managed by this server anyway).
func agentProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
