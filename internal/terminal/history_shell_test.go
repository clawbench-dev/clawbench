//go:build !windows

package terminal

// Test files in this package that touch the user's shell history are POSIX
// semantics: `$HOME`/`$XDG_DATA_HOME` layout, `.bash_history` / `.zsh_history` /
// `fish_history` discovery, and `exec`-based shells. On Windows the history
// files live elsewhere (PSReadLine under %APPDATA%) and `os.UserHomeDir` reads
// %USERPROFILE%, so a fixture that sets HOME proves nothing there.
//
// All of them are hosted from this file behind a build tag rather than skipped
// at runtime so the rest of the package keeps compiling unchanged on Windows:
// the tests move with the code they exercise.

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadShellHistory_MergesEveryShell(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))
	if err := os.WriteFile(filepath.Join(home, ".bash_history"), []byte("git status\nls\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(": 1700000000:0;make\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fishDir := filepath.Join(home, "xdg", "fish")
	if err := os.MkdirAll(fishDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fishDir, "fish_history"), []byte("- cmd: echo hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := ReadShellHistory()
	want := []string{"git status", "ls", "make", "echo hi"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestShellHistoryFiles_PrefersHistfile(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(home, "custom_history")
	if err := os.WriteFile(custom, []byte("custom\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".bash_history"), []byte("bash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("HISTFILE", custom)
	files := ShellHistoryFiles()
	if len(files) < 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}
	// HISTFILE comes FIRST (it is the file the user's current shell actually
	// appends to), and the default ~/.bash_history is still read after it.
	if files[0].path != custom {
		t.Errorf("HISTFILE must be consulted first, got %v", files)
	}
	var sawBash bool
	for _, f := range files[1:] {
		if f.path == filepath.Join(home, ".bash_history") {
			sawBash = true
		}
	}
	if !sawBash {
		t.Errorf("the default bash history must still be merged, got %v", files)
	}
}

func TestReadShellHistory_FishWithoutXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_DATA_HOME", "")
	fishDir := filepath.Join(home, ".local", "share", "fish")
	if err := os.MkdirAll(fishDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fishDir, "fish_history"), []byte("- cmd: fish-cmd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadShellHistory(); !reflect.DeepEqual(got, []string{"fish-cmd"}) {
		t.Errorf("got %v, want [fish-cmd]", got)
	}
}
