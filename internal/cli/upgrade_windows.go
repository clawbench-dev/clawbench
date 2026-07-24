//go:build windows

package cli

import (
	"fmt"
	"os/exec"
	"syscall"
)

func processAlive(pid int) bool {
	// os.FindProcess always returns nil error on Windows, even for non-existent PIDs.
	// Use OpenProcess to actually check if the process exists.
	handle, err := syscall.OpenProcess(0x1000 /* PROCESS_QUERY_LIMITED_INFORMATION */, false, uint32(pid))
	if err != nil {
		return false
	}
	syscall.CloseHandle(handle)
	return true
}

func killProcessForce(pid int) {
	cmd := exec.Command("taskkill", "/F", "/PID", fmt.Sprintf("%d", pid))
	_ = cmd.Run()
}

func setNewProcessGroup(_ *exec.Cmd) {
	// No Setpgid on Windows
}
