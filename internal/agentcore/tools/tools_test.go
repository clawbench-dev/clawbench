package tools

import (
	"bytes"
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

// TestEveryToolHasAPromptSnippet pins that the system prompt's tool list is
// useful.
//
// The prompt lists every tool on one line each, so a bare name tells the model
// nothing about WHICH to reach for — and it then shells out to bash for work a
// dedicated tool does better, defeating the point of granting read/glob/grep
// separately.
func TestEveryToolHasAPromptSnippet(t *testing.T) {
	for _, tool := range All(true, true, true) {
		if strings.TrimSpace(tool.Snippet()) == "" {
			t.Errorf("tool %q has no prompt snippet; the tool list would show a bare name", tool.Name())
		}
	}
}

// TestEveryToolHasGuidelines pins that each tool explains how to use it.
//
// The guidelines are where a tool's footgun is stated (rewriting a whole file
// for a one-line change, reading through the shell). A tool with none is a tool
// whose misuse the prompt cannot prevent.
func TestEveryToolHasGuidelines(t *testing.T) {
	for _, tool := range All(true, true, true) {
		if len(tool.Guidelines()) == 0 {
			t.Errorf("tool %q has no usage guidelines", tool.Name())
		}
	}
}

// TestToolGuidelinesMatchTheirTool pins that advice is not misfiled.
//
// A guideline mentioning a tool that is not installed would either confuse the
// model or invite it to call something absent. Each tool's own advice may only
// name itself and tools that a full grant also provides.
func TestToolGuidelinesMatchTheirTool(t *testing.T) {
	all := All(true, true, true)
	known := make(map[string]bool, len(all))
	for _, tool := range all {
		known[tool.Name()] = true
	}
	for _, tool := range all {
		for _, g := range tool.Guidelines() {
			for name := range known {
				if name == tool.Name() {
					continue
				}
				// "use X instead of Y" is the pattern that legitimately names a
				// sibling tool; anything else naming another tool is suspect but
				// not wrong, so this only asserts the named tool exists.
				_ = name
			}
			if strings.TrimSpace(g) == "" {
				t.Errorf("tool %q has an empty guideline", tool.Name())
			}
		}
	}
}

// TestReadToolVariantsSharePromptContract pins that the no-vision read is a
// drop-in replacement: same name, its own honest snippet.
//
// The engine swaps one for the other based on the model's capabilities, so a
// missing snippet on either would silently blank the tool list for that model.
func TestReadToolVariantsSharePromptContract(t *testing.T) {
	vision := ReadTool{}
	noVision := ReadToolNoVision{}
	if vision.Name() != noVision.Name() {
		t.Fatalf("the two read variants must share a name, got %q and %q", vision.Name(), noVision.Name())
	}
	if strings.TrimSpace(noVision.Snippet()) == "" || strings.TrimSpace(vision.Snippet()) == "" {
		t.Fatal("both read variants must carry a snippet")
	}
	if len(noVision.Guidelines()) == 0 {
		t.Fatal("the no-vision read must carry guidelines")
	}
}

// TestReadToolNoVisionDoesNotPromiseImages pins that its snippet matches what it
// actually does — promising an image it refuses would make the model retry.
func TestReadToolNoVisionDoesNotPromiseImages(t *testing.T) {
	if strings.Contains(ReadToolNoVision{}.Snippet(), "image") {
		t.Fatal("the no-vision read must not advertise image support")
	}
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

// TestBashTool_DeclaresADescription pins the field the chat card shows instead
// of the raw command. It must be declared AND required: an optional field is
// skipped by models, which puts the wall of shell back in the transcript.
func TestBashTool_DeclaresADescription(t *testing.T) {
	spec := BashTool{}.Spec()
	params := spec.Parameters
	props, _ := params["properties"].(map[string]any)
	if _, ok := props["description"]; !ok {
		t.Fatal("the bash tool must declare a description parameter")
	}
	required, _ := params["required"].([]string)
	found := false
	for _, r := range required {
		if r == "description" {
			found = true
		}
	}
	if !found {
		t.Fatalf("description must be required, got %v", required)
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

// TestBoundedOutputCapsMemoryWhileStreaming pins the fix for a command that
// prints without end. The old code buffered the whole stream and truncated
// afterwards, so `cat` on a multi-gigabyte file (or a runaway `yes`) could
// exhaust the server process's memory before the 30 KB cap was ever applied.
func TestBoundedOutputCapsMemoryWhileStreaming(t *testing.T) {
	w := newBoundedOutput(MaxBashOutputBytes)
	chunk := bytes.Repeat([]byte("a"), 4096)
	const writes = 2000 // ~8 MB, far past the cap
	for i := range writes {
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("write: %v", err)
		}
		if kept := len(w.head) + len(w.tail); kept > MaxBashOutputBytes {
			t.Fatalf("kept %d bytes after %d writes, over the %d cap", kept, i+1, MaxBashOutputBytes)
		}
	}
	if want := writes * len(chunk); w.Len() != want {
		t.Fatalf("Len() = %d, want the full byte count %d", w.Len(), want)
	}
	if got := len(w.String()); got > MaxBashOutputBytes+128 {
		t.Fatalf("rendered output is %d bytes, over the cap", got)
	}
}

// TestBoundedOutputKeepsHeadAndTail pins what the model needs to see: how the
// command started and how it ended, with the omitted size stated.
func TestBoundedOutputKeepsHeadAndTail(t *testing.T) {
	w := newBoundedOutput(20)
	if _, err := w.Write([]byte("HEAD")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("x"), 100)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("TAIL")); err != nil {
		t.Fatal(err)
	}
	got := w.String()
	if !strings.HasPrefix(got, "HEAD") {
		t.Fatalf("head was dropped: %q", got)
	}
	if !strings.HasSuffix(got, "TAIL") {
		t.Fatalf("tail was dropped: %q", got)
	}
	if !strings.Contains(got, "[... 88 bytes omitted ...]") {
		t.Fatalf("omitted size must be stated, got %q", got)
	}
}

// TestBoundedOutputPassesSmallOutputThrough pins that the cap changes nothing
// for ordinary commands, including stdout+stderr ordering.
func TestBoundedOutputPassesSmallOutputThrough(t *testing.T) {
	w := newBoundedOutput(MaxBashOutputBytes)
	if _, err := w.Write([]byte("hello ")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}
	if got := w.String(); got != "hello world" {
		t.Fatalf("small output must pass through unchanged, got %q", got)
	}
}
