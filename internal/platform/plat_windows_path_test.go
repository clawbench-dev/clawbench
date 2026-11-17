//go:build windows

package platform

import (
	"path/filepath"
	"strings"
	"testing"
)

// The Windows branch of managedNodeBinDir puts node.exe at the root of the
// install directory (Node's zip has no bin/), and ManagedNodeHome prefers
// LOCALAPPDATA. Both are only reachable on Windows, so they live in a
// build-tagged file: the Unix CI would silently skip them otherwise.
func TestWindows_ManagedNodeBinDirIsTheInstallRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAWBENCH_HOME", home)

	if got, want := managedNodeBinDir(), filepath.Join(home, "node"); got != want {
		t.Fatalf("managedNodeBinDir() = %q, want %q (node.exe sits at the root on Windows)", got, want)
	}
}

// Windows without CLAWBENCH_HOME uses LOCALAPPDATA\clawbench; a blank value
// must not produce a path relative to the current directory.
func TestWindows_ManagedNodeHomePrefersLocalAppData(t *testing.T) {
	local := t.TempDir()
	t.Setenv("CLAWBENCH_HOME", "")
	t.Setenv("LOCALAPPDATA", local)

	want := filepath.Join(local, "clawbench")
	if got := ManagedNodeHome(); got != want {
		t.Fatalf("ManagedNodeHome() = %q, want %q", got, want)
	}

	// No LOCALAPPDATA: falls back to the user profile, never to a relative path.
	t.Setenv("LOCALAPPDATA", "")
	if got := ManagedNodeHome(); !filepath.IsAbs(got) || !strings.HasSuffix(got, "clawbench") {
		t.Fatalf("ManagedNodeHome() = %q, want an absolute ...\\clawbench path", got)
	}
}
