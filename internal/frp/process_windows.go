//go:build !unix

package frp

import (
	"os/exec"
)

// setProcessGroup is a no-op on non-Unix platforms.
func setProcessGroup(cmd *exec.Cmd) {
	// Windows: no POSIX process groups; Taskkill /T would be needed for tree kill.
}

// killProcessGroup is a no-op on non-Unix platforms.
// The caller should also call cmd.Process.Kill() as fallback.
func killProcessGroup(pid int) {
	// Windows: no POSIX process groups
}
