//go:build !linux && !android

package terminal

import "os"

// ForegroundIsShell is the unsupported-platform stub. It returns true so the
// caller keeps the line-shape heuristic instead of recording nothing at all.
func ForegroundIsShell(_ *os.File) bool {
	return true
}
