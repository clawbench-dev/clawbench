//go:build !windows

package ai

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup puts the ACP process in its own process group so we can
// kill the entire tree (npx + child processes) when closing the connection.
//
// Setsid:true both starts a new session and makes the child its own process
// group leader (pgid == pid, since a session leader is also a group leader).
// This serves two purposes:
//  1. killProcessGroup's `-pid` signal reaches the whole process tree.
//  2. The child is detached from the server's controlling terminal. Without
//     this, an agent-spawned command such as `sudo` would inherit the server
//     tty and prompt for a password on the ClawBench server console (via
//     /dev/tty), blocking the whole session. A new session has no controlling
//     terminal, so sudo fails fast with "no tty present" instead of hanging.
//
// NOTE: We must NOT combine Setpgid:true with Setsid:true — Go's fork/exec
// rejects that combination with EPERM ("operation not permitted").
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// killProcessGroup sends SIGKILL to the process group of the given process.
// When ACP agents are spawned via npx (which creates child processes),
// killing only the parent (npx) leaves children alive, holding pipes open
// and causing cmd.Wait() to hang. Killing the process group ensures all
// children are terminated, which closes the pipes and unblocks Wait().
//
// The process must have been started with Setpgid:true in SysProcAttr
// for this to work; otherwise the kill signal applies to the single process.
func killProcessGroup(proc *os.Process) {
	if proc.Pid > 0 {
		_ = syscall.Kill(-proc.Pid, syscall.SIGKILL)
	}
	_ = proc.Kill()
}
