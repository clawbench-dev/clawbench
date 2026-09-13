//go:build !windows

//nolint:noctx // sentinel subprocess, context not applicable
package handler

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"syscall"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

func launchSentinel() (*exec.Cmd, error) {
	// Resolve through the recorded self-path rather than os.Executable(): the
	// sentinel re-execs this path after the current process exits, so it must
	// survive a package manager replacing the package mid-run. os.Executable()
	// would point at the retired-and-deleted package directory, and all five
	// exec retries would fail, leaving the service down.
	exe, err := service.ResolveSelfBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}
	args := os.Args[1:]
	pid := os.Getpid()

	sentinelScript := fmt.Sprintf(
		"PID=%d; EXE=%s; "+
			"while kill -0 $PID 2>/dev/null; do sleep 0.1; done; "+
			"for i in 1 2 3 4 5; do sleep 0.2; exec \"$EXE\" %s && exit 0; done; "+
			"echo 'restart-failed' > %s/restart-status",
		pid, shellQuote(exe), joinArgs(args), shellQuote(model.DataDir),
	)
	cmd := exec.Command("/bin/sh", "-c", sentinelScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start sentinel process: %w", err)
	}

	slog.Info("sentinel process started", "pid", cmd.Process.Pid, "parent_pid", pid)
	return cmd, nil
}
