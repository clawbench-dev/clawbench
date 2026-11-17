package terminal

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseBashHistory(t *testing.T) {
	data := []byte("ls -la\n#1700000000\ngit status\n")
	got := parseBashHistory(data)
	want := []string{"ls -la", "git status"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseBashHistory_JoinsContinuations(t *testing.T) {
	data := []byte("for i in 1 2 3; do \\\n  echo $i \\\ndone\nls\n")
	got := parseBashHistory(data)
	want := []string{"for i in 1 2 3; do\n  echo $i\ndone", "ls"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseBashHistory_IgnoresBlankLines(t *testing.T) {
	got := parseBashHistory([]byte("a\n\n   \nb\n"))
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseZshHistory(t *testing.T) {
	data := []byte(": 1700000000:0;git status\n: 1700000001:3;ls -la\n")
	got := parseZshHistory(data)
	want := []string{"git status", "ls -la"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseZshHistory_PlainLines(t *testing.T) {
	got := parseZshHistory([]byte("echo hi\nmake\n"))
	want := []string{"echo hi", "make"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseZshHistory_KeepsColonCommands(t *testing.T) {
	// A command that genuinely starts with a colon must survive the prefix strip.
	got := parseZshHistory([]byte(": 1700000000:0;: noop\n"))
	want := []string{": noop"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseFishHistory(t *testing.T) {
	data := []byte("- cmd: git status\n  when: 1700000000\n- cmd: echo hi\\nworld\n  when: 1700000001\n")
	got := parseFishHistory(data)
	want := []string{"git status", "echo hi\nworld"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadFileTail_DropsPartialFirstLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")

	// Build a file whose tail starts mid-line: "aaaaaaaaa\nbbbb\ncccc\n".
	// Reading the last 10 bytes begins inside "bbbb", so "bbbb" must be dropped.
	if err := os.WriteFile(path, []byte("aaaaaaaaa\nbbbb\ncccc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := readFileTail(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cccc\n" {
		t.Errorf("got %q, want %q", string(data), "cccc\n")
	}
}

func TestReadFileTail_WholeFileWhenSmall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	if err := os.WriteFile(path, []byte("ls\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := readFileTail(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ls\n" {
		t.Errorf("got %q", string(data))
	}
}

func TestReadShellHistory_NoHistoryFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))

	if got := ReadShellHistory(); len(got) != 0 {
		t.Errorf("expected no commands, got %v", got)
	}
}

func TestShellHistoryFiles_SkipsDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HISTFILE", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))

	// A directory at the history path must not be treated as a file.
	if err := os.MkdirAll(filepath.Join(home, ".bash_history"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range ShellHistoryFiles() {
		if f.path == filepath.Join(home, ".bash_history") {
			t.Error("a directory must not be reported as a history file")
		}
	}
}

func TestReadFileTail_MissingFile(t *testing.T) {
	if _, err := readFileTail(filepath.Join(t.TempDir(), "nope"), 1024); err == nil {
		t.Error("expected an error for a missing file")
	}
}
