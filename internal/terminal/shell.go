//nolint:noctx // PTY subprocess, context not applicable
package terminal

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"

	"github.com/creack/pty"
)

// runtimeGOOS is a variable wrapper around runtime.GOOS so tests can
// override it to cover platform-specific branches on any OS.
var runtimeGOOS = runtime.GOOS

// resolveShell finds the appropriate shell binary for the current platform.
// Linux/macOS: $SHELL → /bin/sh
// Windows: pwsh → powershell → cmd.exe
func resolveShell() string {
	switch runtimeGOOS {
	case "windows":
		// Try PowerShell Core first, then Windows PowerShell, then cmd
		for _, cmd := range []string{"pwsh", "powershell", "cmd.exe"} {
			if path, err := exec.LookPath(cmd); err == nil {
				return path
			}
		}
		return "cmd.exe"
	default:
		// Linux/macOS: use $SHELL, fallback to /bin/sh
		if shell := os.Getenv("SHELL"); shell != "" {
			return shell
		}
		return "/bin/sh"
	}
}

// PlatformError is returned when the current OS does not support PTY.
// The manager uses this to send the platform_unsupported error code
// instead of the generic shell_start_failed.
type PlatformError struct {
	OS string
}

func (e *PlatformError) Error() string {
	return fmt.Sprintf("terminal not supported on %s", e.OS)
}

// startPTY starts a new PTY session with the given working directory.
// Returns the PTY file, the command, and any error.
// The shell process is started in its own process group for clean cleanup.
func startPTY(cwd string) (*os.File, *exec.Cmd, error) {
	// creack/pty does not support Windows — ConPTY is not implemented.
	// Return PlatformError so the manager can send the correct error code.
	if runtimeGOOS == "windows" {
		return nil, nil, &PlatformError{OS: runtimeGOOS}
	}

	shell := resolveShell()
	slog.Info(
		"terminal: starting PTY",
		slog.String("shell", shell),
		slog.String("cwd", cwd),
	)

	// Verify shell exists and is executable
	if _, err := exec.LookPath(shell); err != nil {
		return nil, nil, fmt.Errorf("shell not found: %w", err)
	}

	cmd := exec.Command(shell)
	cmd.Dir = cwd
	// Set terminal environment variables so TUI applications (vim, htop,
	// OpenCode, etc.) can detect terminal capabilities correctly. Without
	// TERM, ncurses/Bubble Tea cannot initialize and full-screen TUI apps
	// fail to render. COLORTERM=truecolor signals 24-bit color support.
	cmd.Env = append(
		os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)

	// NOTE: Do NOT set Setpgid here. pty.Start -> StartWithSize sets
	// Setsid=true + Setctty=true, and Setpgid conflicts with Setsid
	// on Linux (returns EPERM: "operation not permitted").
	// Setsid already creates a new session and process group.

	// Use StartWithSize to provide a reasonable default (80x24) so that
	// the shell and any TUI apps see correct dimensions from the start.
	// Without this, the PTY starts at 0x0 and TUI apps may render with
	// wrong layout until the first frontend fit() + resize arrives.
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	return ptmx, cmd, nil
}
