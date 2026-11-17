//go:build !linux && !android

package terminal

import (
	"os"
	"testing"
)

// The unsupported-platform probe has no /proc to ask, so its only real question
// is what it says when it cannot look. Both answers matter:
//   - no PTY means nothing is running the user's input, so it must NOT report a
//     shell (that is what gates "is this line a command at all");
//   - a live PTY must report a shell, because the caller then falls back to the
//     line-shape heuristic — answering "no" would silently stop recording every
//     command on macOS and Windows.
func TestForegroundIsShell_StubAnswers(t *testing.T) {
	if ForegroundIsShell(nil) {
		t.Error("nil PTY must not report a shell")
	}
	f, err := os.CreateTemp(t.TempDir(), "pty-stub")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	defer f.Close()
	if !ForegroundIsShell(f) {
		t.Error("a live PTY must report a shell on platforms without a foreground probe")
	}
}
