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
	// setProcessGroup sets SysProcAttr with Setpgid:true on non-Windows
	assert.NotNil(t, cmd.SysProcAttr, "SysProcAttr should be set after setProcessGroup")
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
