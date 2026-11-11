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
	sentinelScript := buildUnixSentinelScript(pid, exe, args, sentinelWaitSeconds)
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

// buildUnixSentinelScript renders the /bin/sh script the sentinel runs: wait
// for the outgoing process to EXIT — that is when the data-directory lock is
// released — then re-exec this binary with the SAME arguments.
//
// The wait is bounded, and expiring the bound kills the target. Waiting forever
// looks harmless because the target is already shutting down, but a drain that
// never finishes would leave the sentinel waiting for good and the service down
// with nobody left to start it. Killing is safe: shutdown was requested before
// the sentinel was launched, so the target is never a healthy server. See
// sentinelWaitSeconds for the budget.
//
// After SIGKILL the script waits (bounded) for the process to actually
// disappear before starting the replacement: starting while the dying process
// still holds the lock would just burn the new process's own lock wait.
//
// waitSeconds is a parameter only so tests can drive the escalation in about a
// second instead of thirty; production passes sentinelWaitSeconds.
func buildUnixSentinelScript(pid int, exe string, args []string, waitSeconds int) string {
	// The loop ticks at 0.1s, so one second of waiting is ten ticks. After the
	// kill, five seconds is plenty for SIGKILL to take effect (only an
	// uninterruptible-sleep process outlives that, and the replacement's own
	// lock wait is the backstop).
	ticks := waitSeconds * 10
	const killTicks = 50
	return fmt.Sprintf(
		"PID=%d; EXE=%s; n=0; "+
			"while kill -0 $PID 2>/dev/null; do "+
			"if [ $n -ge %d ]; then "+
			"kill -9 $PID 2>/dev/null; m=0; "+
			"while kill -0 $PID 2>/dev/null && [ $m -lt %d ]; do sleep 0.1; m=$((m+1)); done; "+
			"break; "+
			"fi; "+
			"sleep 0.1; n=$((n+1)); "+
			"done; "+
			"for i in 1 2 3 4 5; do sleep 0.2; if [ -x \"$EXE\" ]; then exec \"$EXE\" %s; fi; done; "+
			"echo 'restart-failed: no longer executable' >&2",
		pid, shellQuote(exe), ticks, killTicks, joinArgs(args),
	)
}
