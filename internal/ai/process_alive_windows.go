//go:build windows

package ai

import "syscall"

// stillActive is the value GetExitCodeProcess reports while a process is still
// running (the Win32 STILL_ACTIVE constant). syscall does not export it.
const stillActive = 259

// agentProcessAlive reports whether the OS still has a RUNNING process with
// this PID.
//
// os.FindProcess always succeeds on Windows (even for nonexistent PIDs), so it
// cannot be used as a liveness probe. OpenProcess alone is not enough either: a
// process object outlives the process whenever ANY handle still references it,
// so OpenProcess keeps succeeding for a process that has already exited — which
// is exactly the case this probe exists to detect. That made the probe depend
// on unrelated handle holders (CI tooling, inherited handles) and flap between
// runs. GetExitCodeProcess answers from the process itself: it returns the real
// exit code, which equals STILL_ACTIVE only while the process runs.
//
// Caveat: a process that genuinely exits with code 259 is indistinguishable
// from a running one here. That is inherent to the Win32 API, and such an exit
// code is not produced by the agent CLIs this guards.
func agentProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const processQueryLimitedInformation = 0x1000
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = syscall.CloseHandle(handle) }()

	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}
