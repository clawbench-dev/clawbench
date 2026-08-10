//go:build !windows

package ai

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetProcessGroup(t *testing.T) {
	cmd := exec.Command("echo", "test")
	setProcessGroup(cmd)
	// setProcessGroup sets SysProcAttr on non-Windows
	assert.NotNil(t, cmd.SysProcAttr, "SysProcAttr should be set after setProcessGroup")
}

func TestSetProcessGroup_DetachesControllingTerminal(t *testing.T) {
	// Setsid:true puts the child in a new session without a controlling
	// terminal, so agent-spawned commands like `sudo` can't prompt on the
	// server's /dev/tty and block the session. It also makes the child its
	// own process group leader (pgid == pid) so killProcessGroup's -pid
	// signal still reaches the whole tree.
	cmd := exec.Command("echo", "test")
	setProcessGroup(cmd)
	assert.True(t, cmd.SysProcAttr.Setsid, "Setsid should be true to detach the controlling terminal")
	assert.False(t, cmd.SysProcAttr.Setpgid, "Setpgid must NOT be combined with Setsid (causes EPERM)")
}

func TestKillProcessGroup_ZeroPid(t *testing.T) {
	// Process with PID 0 should be handled safely (only proc.Kill called, not group kill)
	p := &os.Process{Pid: 0}
	assert.NotPanics(t, func() {
		killProcessGroup(p)
	})
}

func TestKillProcessGroup_PositivePid(t *testing.T) {
	// A process with positive PID but not actually running — proc.Kill will fail
	// but killProcessGroup should not panic
	p := &os.Process{Pid: 99999999}
	assert.NotPanics(t, func() {
		killProcessGroup(p)
	})
}
