package handler

import (
	"path/filepath"
	"runtime"
	"testing"
)

// Issue: an attachment persisted with its resolved absolute path is re-sent
// verbatim (最近引用 / restored draft / drained queue row). On Windows the
// frontend may hand back a POSIX-style "/proj/sub": filepath.IsAbs requires a
// volume name there, so it read as RELATIVE, fell through to
// model.ValidatePath, got joined onto the project root and 404'd — the exact
// bug the absolute-path support was added to fix, surviving on one platform.
//
// asAbsolutePath is the single decision point, so it is tested directly. The
// Windows branch cannot run on a Linux CI host (filepath.IsAbs and
// filepath.Abs are compiled per-OS), so the platform-independent half is
// asserted everywhere and the Windows-only expectations are guarded.

func TestAsAbsolutePath_POSIXStylePathIsAbsoluteOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-style absolute paths are the Windows-specific case, covered below")
	}
	got, ok := asAbsolutePath("/proj/root", "/proj/root/sub")
	if !ok {
		t.Fatalf("a leading-slash path must be absolute on %s", runtime.GOOS)
	}
	if got != "/proj/root/sub" {
		t.Errorf("asAbsolutePath = %q, want the path unchanged (/proj/root/sub)", got)
	}
}

func TestAsAbsolutePath_NativeAbsoluteIsAccepted(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "sub")

	got, ok := asAbsolutePath(base, target)
	if !ok {
		t.Fatalf("native absolute path %q was rejected", target)
	}
	if got != target {
		t.Errorf("asAbsolutePath = %q, want %q", got, target)
	}
}

func TestAsAbsolutePath_RelativeStaysRelative(t *testing.T) {
	// Relative paths must keep their existing meaning: resolved against the
	// project root by model.ValidatePath, which also enforces the traversal
	// guard. Returning ok here would bypass both.
	for _, rel := range []string{"sub", "sub/file.txt", "./sub", "../sibling"} {
		if got, ok := asAbsolutePath("/proj/root", rel); ok {
			t.Errorf("asAbsolutePath(%q) = %q, true — relative paths must be left to ValidatePath", rel, got)
		}
	}
}

func TestAsAbsolutePath_EmptyPathIsRelative(t *testing.T) {
	if _, ok := asAbsolutePath("/proj/root", ""); ok {
		t.Error("an empty path must not be treated as absolute")
	}
}

func TestAsAbsolutePath_WindowsPOSIXStyleIsVolumeQualified(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("volume-qualification only applies on Windows; on POSIX the path is already absolute")
	}
	got, ok := asAbsolutePath(`C:\proj\root`, "/proj/root/sub")
	if !ok {
		t.Fatal("a leading-slash path must still be recognized as absolute on Windows")
	}
	want := `C:\proj\root\sub`
	if got != want {
		t.Errorf("asAbsolutePath = %q, want %q (volume taken from the project root)", got, want)
	}
}

func TestAsAbsolutePath_WindowsRelativeIsNotVolumeQualified(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only branch")
	}
	if _, ok := asAbsolutePath(`C:\proj\root`, `sub\file.txt`); ok {
		t.Error("a backslash-relative path must not be treated as absolute")
	}
}

// The Windows branch must not fire on POSIX: "//server/share" is an ordinary
// absolute path there, and treating a leading slash specially would change its
// meaning. filepath.Abs collapses the doubled leading slash (standard library
// behavior, not something this change introduces) — what matters is that the
// path is accepted as absolute and stays a path on the same volume.
func TestAsAbsolutePath_LeadingSlashIsNotSpecialOnPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only expectation")
	}
	got, ok := asAbsolutePath("/proj/root", "//server/share/file.txt")
	if !ok {
		t.Fatal("a double-slash path is absolute on POSIX")
	}
	if got != "/server/share/file.txt" {
		t.Errorf("asAbsolutePath = %q, want %q (filepath.Abs collapses the leading double slash)",
			got, "/server/share/file.txt")
	}
}
