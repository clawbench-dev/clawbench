package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The switch under test is "may the FILE tools leave the working directory".
// It defaults to allowed (Env.ConfineToWorkDir == false) because that is how it
// shipped; the fence is the opt-in. These tests pin both halves, because the
// failure mode of getting it wrong is silent: either the agent is refused work
// it was told it could do, or the fence the user asked for does nothing.

// TestResolve_OpenReachesOutside pins that with the fence off, the escapes the
// old code refused are now resolved rather than rejected.
func TestResolve_OpenReachesOutside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	env := Env{Root: root} // ConfineToWorkDir false = open

	got, err := env.resolve(filepath.Join(outside, "x.txt"))
	if err != nil {
		t.Fatalf("an absolute path outside the root must be allowed: %v", err)
	}
	if got != filepath.Join(outside, "x.txt") {
		t.Fatalf("absolute path must resolve to itself, got %q", got)
	}

	got, err = env.resolve("../x.txt")
	if err != nil {
		t.Fatalf("a relative path that leaves the root must be allowed: %v", err)
	}
	if want := filepath.Clean(filepath.Join(root, "..", "x.txt")); got != want {
		t.Fatalf("relative escape must resolve against the root: got %q want %q", got, want)
	}
}

// TestResolve_OpenKeepsRelativeInside pins that turning the fence off does not
// change what a plain relative path means — the working directory is still the
// base. Getting this wrong would make every relative path in a task resolve
// against the process CWD instead.
func TestResolve_OpenKeepsRelativeInside(t *testing.T) {
	root := t.TempDir()
	env := Env{Root: root}
	got, err := env.resolve("src/main.go")
	if err != nil {
		t.Fatalf("a plain relative path must resolve: %v", err)
	}
	if want := filepath.Join(root, "src", "main.go"); got != want {
		t.Fatalf("relative path must resolve against the root: got %q want %q", got, want)
	}
}

// TestResolve_ConfinedStillRefusesEscape pins that the fence, when on, is the
// old behaviour exactly: the same escape shapes stay refused.
func TestResolve_ConfinedStillRefusesEscape(t *testing.T) {
	root := t.TempDir()
	env := Env{Root: root, ConfineToWorkDir: true}
	for _, p := range []string{"../outside.txt", "../../etc/passwd", "/etc/passwd"} {
		if _, err := env.resolve(p); err == nil {
			t.Fatalf("%q must be refused while confined", p)
		}
	}
}

// TestResolve_OpenAllowsSymlinkEscape pins the case that distinguishes "cleans
// the path" from "checks containment": a symlink inside the project pointing
// out of it. With the fence off this is a normal layout, not an escape.
func TestResolve_OpenAllowsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "resources")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	if _, err := (Env{Root: root}).resolve("resources/secret.txt"); err != nil {
		t.Fatalf("an outward symlink must resolve with the fence off: %v", err)
	}
	if _, err := (Env{Root: root, ConfineToWorkDir: true}).resolve("resources/secret.txt"); err == nil {
		t.Fatal("the same symlink must be refused with the fence on")
	}
}

// TestReadTool_HonoursOutsideAccess pins the setting end-to-end through a real
// tool, not just the resolver: a file outside the project is readable when the
// fence is off and refused when it is on.
//
// A resolver test alone would not catch a tool that bypasses Env.resolve.
func TestReadTool_HonoursOutsideAccess(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "note.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	open := mustRun(t, ReadTool{}, Env{Root: root}, map[string]any{"path": target})
	if open.IsError || !strings.Contains(open.Text, "hello") {
		t.Fatalf("read outside the project must succeed with the fence off: %+v", open)
	}

	fenced := mustRun(t, ReadTool{}, Env{Root: root, ConfineToWorkDir: true}, map[string]any{"path": target})
	if !fenced.IsError {
		t.Fatal("read outside the project must fail with the fence on")
	}
}

// TestWriteTool_HonoursOutsideAccess is the write-side twin of the read test:
// the switch must govern writes too, since a fence that only stops reads is not
// a fence.
func TestWriteTool_HonoursOutsideAccess(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "new.txt")

	open := mustRun(t, WriteTool{}, Env{Root: root}, map[string]any{"path": target, "content": "x"})
	if open.IsError {
		t.Fatalf("write outside the project must succeed with the fence off: %+v", open)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("the file must exist after the write: %v", err)
	}

	fenced := mustRun(t, WriteTool{}, Env{Root: root, ConfineToWorkDir: true}, map[string]any{"path": filepath.Join(outside, "no.txt"), "content": "x"})
	if !fenced.IsError {
		t.Fatal("write outside the project must fail with the fence on")
	}
}

// TestGlobTool_HonoursOutsideAccess pins that a search rooted outside the
// project actually searches there when allowed — the glob base is derived from
// the pattern, so it is the tool most likely to silently ignore the setting.
func TestGlobTool_HonoursOutsideAccess(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "found.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	pattern := filepath.ToSlash(filepath.Join(outside, "*.txt"))
	open := mustRun(t, GlobTool{}, Env{Root: root}, map[string]any{"pattern": pattern})
	if open.IsError || !strings.Contains(open.Text, "found.txt") {
		t.Fatalf("glob outside the project must find the file with the fence off: %+v", open)
	}

	fenced := mustRun(t, GlobTool{}, Env{Root: root, ConfineToWorkDir: true}, map[string]any{"pattern": pattern})
	if !fenced.IsError && strings.Contains(fenced.Text, "found.txt") {
		t.Fatal("glob outside the project must not report outside files with the fence on")
	}
}
