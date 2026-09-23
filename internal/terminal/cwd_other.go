//go:build !linux && !android

package terminal

import (
	"errors"
	"os"
)

// cwdProbeSupported reports whether foregroundCwd can resolve the shell's live
// working directory on this platform.
//
// Only Linux/Android are supported: they expose /proc/<pid>/cwd for free.
// macOS would need libproc (proc_pidinfo) via cgo, but build.sh --darwin
// cross-compiles with CGO_ENABLED=0 while release.yml builds darwin with
// CGO_ENABLED=1 — taking on cgo would make those two paths diverge. Windows
// never reaches here at all: startPTY rejects it with PlatformError.
const cwdProbeSupported = false

// errCwdProbeUnsupported is returned by foregroundCwd on platforms without a
// live-cwd probe. Callers fall back to the session's launch directory.
var errCwdProbeUnsupported = errors.New("terminal: live cwd probe not supported on this platform")

// foregroundCwd is the unsupported-platform stub. It always fails so callers
// take the launch-directory fallback.
func foregroundCwd(_ *os.File) (string, error) {
	return "", errCwdProbeUnsupported
}
