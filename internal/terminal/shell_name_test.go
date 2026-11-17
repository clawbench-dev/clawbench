//go:build linux

package terminal

import "testing"

// isShellProcessName lives in foreground_linux.go alongside the /proc probe it
// serves, so the test for it belongs behind the same tag: the other platforms
// compile this package's tests too, and a reference to a Linux-only helper
// fails their build before any test can skip.
func TestIsShellProcessName(t *testing.T) {
	for _, name := range []string{"bash", "-bash", "zsh", "sh", "fish", "dash"} {
		if !isShellProcessName(name) {
			t.Errorf("%q should be a shell", name)
		}
	}
	for _, name := range []string{"vim", "ssh", "less", "python3", ""} {
		if isShellProcessName(name) {
			t.Errorf("%q should not be a shell", name)
		}
	}
}
