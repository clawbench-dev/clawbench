//go:build windows

package ai

import "syscall"

// agentProcessAlive reports whether the OS still has a process with this PID.
//
// os.FindProcess always succeeds on Windows (even for nonexistent PIDs), so it
// cannot be used as a liveness probe. OpenProcess with
// PROCESS_QUERY_LIMITED_INFORMATION actually fails for a dead PID.
func agentProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const processQueryLimitedInformation = 0x1000
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = syscall.CloseHandle(handle)
	return true
}
