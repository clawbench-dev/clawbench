//go:build windows

package handler

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"clawbench/internal/service"
)

func launchSentinel() (*exec.Cmd, error) {
	// See settings_sentinel_unix.go: ResolveRestartBinary falls back to the
	// command on PATH when the recorded path was deleted by a package manager
	// reinstall, which is the only surviving route in that case.
	exe, err := service.ResolveRestartBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}
	// cmd.exe does NOT honour single quotes — `cmd /c 'C:\x\clawbench.exe'`
	// fails with "is not recognized". The POSIX shellQuote used by the Unix
	// sentinel therefore cannot be reused here: quoting the ARGS with it (the
	// original bug) did not fail loudly, it delivered `'--port'` as a literal
	// token so every flag comparison in cmd/server/main.go missed and the new
	// process came up on the default port and default data directory.
	// buildWindowsSentinelScript applies the CommandLineToArgvW rules instead;
	// see settings_winarg.go for why they live outside the build tag.
	sentinelScript := buildWindowsSentinelScript(exe, os.Args[1:])
	cmd := exec.Command("cmd", "/c", sentinelScript)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start sentinel process: %w", err)
	}

	slog.Info("sentinel process started", "pid", cmd.Process.Pid, "parent_pid", os.Getpid())
	return cmd, nil
}
