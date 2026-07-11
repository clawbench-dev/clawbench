//nolint:noctx // PTY subprocess, context not applicable
package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
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
