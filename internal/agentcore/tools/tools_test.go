package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// mustRun is a helper that fails the test on a malformed result.
func mustRun(t *testing.T, tool Tool, env Env, args map[string]any) Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return tool.Run(env, raw)
}

func newEnv(t *testing.T) (Env, string) {
	t.Helper()
	root := t.TempDir()
	return Env{Root: root, CommandTimeoutSec: 10}, root
}

// TestResolvePath_RejectsEscape pins the core safety boundary.
//
// Every one of these shapes is a real escape attempt a model produces (by
// accident or because a file it read told it to). Allowing any of them makes
// the read-only policy meaningless.
func TestResolvePath_RejectsEscape(t *testing.T) {
	root := t.TempDir()
	escapes := []string{
		"../outside.txt",
		"../../etc/passwd",
		"sub/../../outside.txt",
		"/etc/passwd",
		"/tmp/whatever",
	}
	for _, p := range escapes {
		t.Run(p, func(t *testing.T) {
			if _, err := resolvePath(root, p); err == nil {
				t.Fatalf("%q must be rejected", p)
			}
		})
	}
}

// TestResolvePath_AllowsInside pins that the guard does not reject legitimate
// paths — an over-eager check would make the tool useless.
func TestResolvePath_AllowsInside(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"a.txt", "sub/a.txt", "./a.txt", "sub/../a.txt"} {
		if _, err := resolvePath(root, p); err != nil {
			t.Fatalf("%q must be allowed inside the root: %v", p, err)
		}
	}
	// The root itself is legal (glob/grep use it).
	if _, err := resolvePath(root, "."); err != nil {
		t.Fatalf("the root itself must resolve: %v", err)
	}
}

// TestResolvePath_RejectsSymlinkEscape pins the case a naive
// filepath.Clean prefix check misses: a symlink INSIDE the root that points
// outside it.
//
// This is the shape a poisoned repository produces ("resources -> /etc"), and
// it must fail even though the textual path is inside the root.
func TestResolvePath_RejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "resources")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// Both an existing file through the link and a not-yet-existing one must
	// be refused.
	if _, err := resolvePath(root, "resources/secret.txt"); err == nil {
		t.Fatal("reading through a symlink that escapes the root must be rejected")
	}
	if _, err := resolvePath(root, "resources/new.txt"); err == nil {
		t.Fatal("writing through a symlink that escapes the root must be rejected")
	}
}

// TestResolvePath_AllowsSymlinkInside pins that a symlink which stays inside
// the root is not rejected — otherwise a normal project layout would break.
func TestResolvePath_AllowsSymlinkInside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "alias")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, err := resolvePath(root, "alias/file.txt"); err != nil {
		t.Fatalf("an internal symlink must be allowed: %v", err)
	}
}

func TestResolvePath_RequiresRoot(t *testing.T) {
	if _, err := resolvePath("", "a.txt"); err == nil {
		t.Fatal("an empty root must be rejected rather than resolving against the process CWD")
	}
	if _, err := resolvePath(t.TempDir(), ""); err == nil {
		t.Fatal("an empty path must be rejected")
	}
}

// TestReadTool_ReturnsLineNumbersAndContinuation pins the two behaviours a
// model depends on: numbered lines (so it can address them) and an explicit
// "there is more" note (so it does not assume the file ended).
func TestReadTool_ReturnsLineNumbersAndContinuation(t *testing.T) {
	env, root := newEnv(t)
	lines := make([]string, MaxReadLines+10)
	for i := range lines {
		lines[i] = "line"
	}
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res := mustRun(t, ReadTool{}, env, map[string]any{"path": "big.txt"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if !strings.Contains(res.Text, "1: line") {
		t.Fatalf("output must be line-numbered, got %q", res.Text[:min(80, len(res.Text))])
	}
	if !strings.Contains(res.Text, "more lines") {
		t.Fatalf("output must announce the remaining lines, got %q", res.Text[max(0, len(res.Text)-200):])
	}

	// The continuation hint must point at a line that is actually returned
	// next, otherwise the model loops on the same window.
	res2 := mustRun(t, ReadTool{}, env, map[string]any{"path": "big.txt", "offset": MaxReadLines + 1})
	if res2.IsError {
		t.Fatalf("unexpected error on continuation: %s", res2.Text)
	}
	if !strings.Contains(res2.Text, "line") {
		t.Fatal("continuation must return content")
	}
}

func TestReadTool_RejectsEscapeAndMissingFile(t *testing.T) {
	env, _ := newEnv(t)
	res := mustRun(t, ReadTool{}, env, map[string]any{"path": "../../etc/passwd"})
	if !res.IsError {
		t.Fatal("reading outside the root must fail")
	}
	res = mustRun(t, ReadTool{}, env, map[string]any{"path": "nope.txt"})
	if !res.IsError || !strings.Contains(res.Text, "does not exist") {
		t.Fatalf("a missing file must be reported clearly, got %q", res.Text)
	}
}

// TestReadTool_ImageNeedsVisionCapableTool pins that the no-vision variant
// refuses an image with a reason the model can act on, instead of returning
// bytes it cannot read.
func TestReadTool_ImageNeedsVisionCapableTool(t *testing.T) {
	env, root := newEnv(t)
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32))
	if err := os.WriteFile(filepath.Join(root, "pic.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}

	res := mustRun(t, ReadTool{}, env, map[string]any{"path": "pic.png"})
	if res.IsError || res.Image == nil {
		t.Fatalf("the vision-capable read must return an image, got err=%v image=%v", res.IsError, res.Image)
	}
	if res.Image.MediaType != "image/png" {
		t.Fatalf("media type must be detected, got %q", res.Image.MediaType)
	}

	res = mustRun(t, ReadToolNoVision{}, env, map[string]any{"path": "pic.png"})
	if !res.IsError || !strings.Contains(res.Text, "cannot read images") {
		t.Fatalf("the text-only read must explain itself, got %q", res.Text)
	}
	if res.Image != nil {
		t.Fatal("the text-only read must not return image bytes")
	}
}

func TestWriteTool_CreatesParentDirs(t *testing.T) {
	env, root := newEnv(t)
	res := mustRun(t, WriteTool{}, env, map[string]any{
		"path":    "a/b/c.txt",
		"content": "hello",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	got, err := os.ReadFile(filepath.Join(root, "a", "b", "c.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("content mismatch: %q", got)
	}
}

// TestEditTool_RefusesAmbiguousMatch pins the "do not guess" rule: silently
// replacing the first of four matches corrupts the file in a way the model's
// next read will not reveal.
func TestEditTool_RefusesAmbiguousMatch(t *testing.T) {
	env, root := newEnv(t)
	path := filepath.Join(root, "f.txt")
	if err := os.WriteFile(path, []byte("foo\nfoo\nfoo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := mustRun(t, EditTool{}, env, map[string]any{
		"path": "f.txt", "old_string": "foo", "new_string": "bar",
	})
	if !res.IsError {
		t.Fatal("an ambiguous edit must be refused")
	}
	if !strings.Contains(res.Text, "3 times") {
		t.Fatalf("the refusal must say how many matches there were, got %q", res.Text)
	}
	// The file must be untouched.
	got, _ := os.ReadFile(path)
	if string(got) != "foo\nfoo\nfoo\n" {
		t.Fatal("a refused edit must not modify the file")
	}

	// replace_all is the explicit opt-in.
	res = mustRun(t, EditTool{}, env, map[string]any{
		"path": "f.txt", "old_string": "foo", "new_string": "bar", "replace_all": true,
	})
	if res.IsError {
		t.Fatalf("replace_all must succeed: %s", res.Text)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "bar\nbar\nbar\n" {
		t.Fatalf("replace_all must change every occurrence, got %q", got)
	}
}

func TestEditTool_ReportsMissingMatch(t *testing.T) {
	env, root := newEnv(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := mustRun(t, EditTool{}, env, map[string]any{
		"path": "f.txt", "old_string": "absent", "new_string": "x",
	})
	if !res.IsError || !strings.Contains(res.Text, "not found") {
		t.Fatalf("a missing match must be reported, got %q", res.Text)
	}
}

// TestGlobTool_DoubleStarMatchesAcrossDirectories pins ** support, which
// filepath.Match does not have.
func TestGlobTool_DoubleStarMatchesAcrossDirectories(t *testing.T) {
	env, root := newEnv(t)
	for _, p := range []string{"a.go", "sub/b.go", "sub/deep/c.go", "sub/readme.md"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	res := mustRun(t, GlobTool{}, env, map[string]any{"pattern": "**/*.go"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	for _, want := range []string{"a.go", "sub/b.go", "sub/deep/c.go"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("expected %q in results, got %q", want, res.Text)
		}
	}
	if strings.Contains(res.Text, "readme.md") {
		t.Fatal("a non-matching file must not be listed")
	}
}

// TestGlobTool_SkipsVendorDirs pins that a broad pattern does not drown the
// context in generated files.
func TestGlobTool_SkipsVendorDirs(t *testing.T) {
	env, root := newEnv(t)
	for _, p := range []string{"src/a.go", "node_modules/pkg/b.go"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res := mustRun(t, GlobTool{}, env, map[string]any{"pattern": "**/*.go"})
	if strings.Contains(res.Text, "node_modules") {
		t.Fatalf("node_modules must be skipped, got %q", res.Text)
	}
	if !strings.Contains(res.Text, "src/a.go") {
		t.Fatalf("source files must still be found, got %q", res.Text)
	}
}

func TestGrepTool_ReportsFileAndLine(t *testing.T) {
	env, root := newEnv(t)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha\nneedle here\nomega\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := mustRun(t, GrepTool{}, env, map[string]any{"pattern": "needle"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if !strings.Contains(res.Text, "a.txt:2: needle here") {
		t.Fatalf("grep must report file:line: content, got %q", res.Text)
	}
}

func TestGrepTool_RejectsInvalidRegexp(t *testing.T) {
	env, _ := newEnv(t)
	res := mustRun(t, GrepTool{}, env, map[string]any{"pattern": "("})
	if !res.IsError || !strings.Contains(res.Text, "invalid regular expression") {
		t.Fatalf("an invalid regexp must be reported, got %q", res.Text)
	}
}

func TestBashTool_RunsInRootAndReportsExitStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	env, root := newEnv(t)
	res := mustRun(t, BashTool{}, env, map[string]any{"command": "pwd"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if !strings.Contains(res.Text, filepath.Base(root)) {
		t.Fatalf("the command must run in the root, got %q", res.Text)
	}

	res = mustRun(t, BashTool{}, env, map[string]any{"command": "exit 3"})
	if !res.IsError {
		t.Fatal("a non-zero exit must be reported as an error")
	}
	if !strings.Contains(res.Text, "exit status") {
		t.Fatalf("the exit status must be reported, got %q", res.Text)
	}
}

// TestBashTool_HonoursTurnCancellation pins that the user's stop button stops
// the command: a tool that ignores the turn context leaves an orphan process
// running after the reply was cancelled.
func TestBashTool_HonoursTurnCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	env := Env{Root: root, CommandTimeoutSec: 30, Ctx: ctx}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	res := mustRun(t, BashTool{}, env, map[string]any{"command": "sleep 20"})
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("cancellation must stop the command promptly, took %v", elapsed)
	}
	if !res.IsError {
		t.Fatal("a cancelled command must be reported as an error")
	}
}

func TestBashTool_TruncatesLargeOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	env, _ := newEnv(t)
	res := mustRun(t, BashTool{}, env, map[string]any{
		"command": "for i in $(seq 1 20000); do echo aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; done",
	})
	if len(res.Text) > MaxBashOutputBytes+2048 {
		t.Fatalf("output must be bounded, got %d bytes", len(res.Text))
	}
	if !strings.Contains(res.Text, "bytes omitted") {
		t.Fatal("truncation must be announced so the model knows output is missing")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
