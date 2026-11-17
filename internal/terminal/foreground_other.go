//go:build !linux && !android

package terminal

import "os"

// ForegroundIsShell is the unsupported-platform stub.
//
// It answers "not a shell" when it cannot answer at all (no PTY, e.g. after
// Close), because a probe with nothing to look at must not claim the user is at
// a prompt — that is the answer that would let program input be recorded as a
// command. Everywhere else it says "unknown, assume a shell", which is the
// conservative choice for the caller: the line-shape heuristic then stays in
// charge and no command is lost.
//
// (The previous version returned true unconditionally, which made a closed
// session report a live shell.)
func ForegroundIsShell(ptmx *os.File) bool {
	return ptmx != nil
}
