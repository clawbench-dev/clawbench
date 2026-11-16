//go:build windows

package tools

import "os/exec"

// setProcessGroup is a no-op on Windows: there is no process group to signal,
// and exec.CommandContext already terminates the process on context cancel.
// Child processes it spawned may survive — a documented limitation, not a
// silent one (see the bash tool's docs).
func setProcessGroup(cmd *exec.Cmd) {}

// shellCommand returns the interpreter and its "run this string" flag.
func shellCommand() (string, string) {
	return "cmd.exe", "/C"
}
