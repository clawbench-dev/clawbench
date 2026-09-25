//go:build linux || android

package terminal

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// cwdProbeSupported reports whether foregroundCwd can resolve the shell's live
// working directory on this platform. Linux (and Android, which shares the
// Linux /proc filesystem) exposes /proc/<pid>/cwd, so the probe works without
// any shell integration.
const cwdProbeSupported = true

// deletedSuffix is appended by the kernel to the /proc/<pid>/cwd symlink target
// when the directory has been removed after the process entered it. The raw
// link text is then e.g. "/tmp/gone (deleted)" — a path that does not exist, so
// returning it verbatim would hand callers an unusable directory.
const deletedSuffix = " (deleted)"

// foregroundCwd returns the working directory of the PTY's foreground process
// group leader.
//
// The shell that owns the terminal is not necessarily the process the user is
// interacting with: `cd` in a nested shell (bash, sudo -i, docker exec) changes
// that child's directory, not the outer shell's. Reading the foreground process
// group — rather than the PTY's direct child — keeps the answer correct through
// nesting, and it is exactly the process whose prompt the user is looking at.
func foregroundCwd(ptmx *os.File) (string, error) {
	if ptmx == nil {
		return "", errors.New("terminal: no PTY")
	}

	pgrp, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPGRP)
	if err != nil {
		return "", fmt.Errorf("terminal: TIOCGPGRP failed: %w", err)
	}
	if pgrp <= 0 {
		return "", fmt.Errorf("terminal: invalid foreground process group %d", pgrp)
	}

	target, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pgrp))
	if err != nil {
		return "", fmt.Errorf("terminal: readlink /proc/%d/cwd failed: %w", pgrp, err)
	}

	return strings.TrimSuffix(target, deletedSuffix), nil
}
