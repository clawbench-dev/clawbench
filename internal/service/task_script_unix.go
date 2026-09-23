//go:build !windows

package service

import (
	"os"
	"syscall"
)

// killTaskScriptProcessGroup sends SIGKILL to the process group of the given
// process. When the script spawns children (which inherit the stdout/stderr
// pipes), killing only the leader leaves those children alive holding the
// pipes open, causing cmd.Wait() to hang. Killing the whole group terminates
// the tree and closes the pipes.
//
// The process must have been started with Setpgid:true (see
// setCmdProcessGroup); the group id then equals the leader's pid.
func killTaskScriptProcessGroup(proc *os.Process) {
	if proc.Pid > 0 {
		_ = syscall.Kill(-proc.Pid, syscall.SIGKILL)
	}
	_ = proc.Kill()
}
