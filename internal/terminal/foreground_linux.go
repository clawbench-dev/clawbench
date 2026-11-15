//go:build linux || android

package terminal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// ForegroundIsShell reports whether the PTY's foreground process is still a
// shell — i.e. the user is typing at a prompt rather than inside vim, less or
// an ssh session.
//
// This is the accurate half of the "is this line a command?" question: it is
// checked when a line is submitted, so program input (`:wq`, `/needle`) is
// rejected no matter what it looks like. The frontend sends the line BEFORE the
// terminating newline, so by the time this runs the shell has not yet started
// the command the line names.
func ForegroundIsShell(ptmx *os.File) bool {
	if ptmx == nil {
		return false
	}
	pgrp, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPGRP)
	if err != nil || pgrp <= 0 {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pgrp))
	if err != nil {
		return false
	}
	return isShellProcessName(strings.TrimSpace(string(data)))
}

// isShellProcessName matches the process names of the shells we start. A login
// shell may be reported with a leading dash (`-bash`), hence the trim.
func isShellProcessName(name string) bool {
	name = strings.TrimPrefix(strings.TrimSpace(name), "-")
	name = filepath.Base(name)
	switch name {
	case "sh", "bash", "zsh", "dash", "fish", "ksh", "ksh93", "mksh", "tcsh", "csh", "ash":
		return true
	}
	return false
}
