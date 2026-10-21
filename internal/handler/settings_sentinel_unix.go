//go:build !windows

//nolint:noctx // sentinel subprocess, context not applicable
package handler

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"syscall"

	"clawbench/internal/service"
)

func launchSentinel() (*exec.Cmd, error) {
	// Resolve through the recorded self-path rather than os.Executable(): the
	// sentinel re-execs this path after the current process exits, so it must
	// survive a package manager replacing the package mid-run. os.Executable()
	// would point at the retired-and-deleted package directory, and all five
	// exec retries would fail, leaving the service down.
	//
	// ResolveRestartBinary (not ResolveSelfBinary) because when the package
	// manager has ALSO deleted the recorded path — the npm reinstall case that
	// retired the directory to `@scope/.<name>-<random>` — the only surviving
	// route is the manager's own shim on PATH. Restarting through it is correct:
	// it resolves to the build that is installed now, which is exactly the one
	// the user asked for. An in-place upgrade must keep using ResolveSelfBinary.
	exe, err := service.ResolveRestartBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}
	args := os.Args[1:]
	pid := os.Getpid()

	// The retry loop must NOT put `exec` in the loop body's tail position.
	// `exec` replaces the shell, so on success the loop is over — but on
	// FAILURE a non-interactive POSIX shell exits immediately (status 127)
	// instead of continuing to the next iteration. That made both the retries
	// and the failure report unreachable dead code, so a re-exec failure left
	// the service down with nothing but a shell error on stderr.
	//
	// `[ -x ]` keeps the shell alive so a transient failure (the file being
	// replaced at that instant) is actually retried. Permanent failures are
	// caught earlier and surfaced as a 500 — see ResolveRestartBinary, which
	// verifies the target is executable before the sentinel is ever started.
	sentinelScript := fmt.Sprintf(
		"PID=%d; EXE=%s; "+
			"while kill -0 $PID 2>/dev/null; do sleep 0.1; done; "+
			"for i in 1 2 3 4 5; do sleep 0.2; if [ -x \"$EXE\" ]; then exec \"$EXE\" %s; fi; done; "+
			"echo 'restart-failed: no longer executable' >&2",
		pid, shellQuote(exe), joinArgs(args),
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
