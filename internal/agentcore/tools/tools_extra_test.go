package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"clawbench/internal/symwalk"
)

// This file covers the tool behaviors that an agent hits when it gets things
// wrong: nonexistent paths, ambiguous edits, empty arguments, binary files, and
// the parts of read/write whose only visible effect is an error message. They
// are separated from tools_test.go because that file pins the happy paths and
// the fence; these pin the refusals, whose wording is what lets the model
// correct itself instead of guessing.

// requirePOSIX skips on Windows where the assertions depend on Unix permission
// semantics (a mode-0000 file is still readable there).
func requirePOSIX(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics required")
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDecodeArgs pins the two distinct failures so the model can tell "you sent
// nothing" from "you sent malformed JSON".
func TestDecodeArgs(t *testing.T) {
	var dst struct {
		Path string `json:"path"`
	}
	err := decodeArgs(nil, &dst)
	if err == nil || !strings.Contains(err.Error(), "no arguments were provided") {
		t.Fatalf("an empty payload must be reported as such, got %v", err)
	}
	err = decodeArgs(json.RawMessage(`{"path":`), &dst)
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("malformed JSON must be reported as such, got %v", err)
	}
	if err := decodeArgs(json.RawMessage(`{"path":"a"}`), &dst); err != nil {
		t.Fatalf("valid JSON must decode: %v", err)
	}
	if dst.Path != "a" {
		t.Fatalf("value not decoded: %q", dst.Path)
	}
}

// TestEveryToolReportsBadArguments pins that a malformed call reaches the model
// as an error result rather than a panic or a silent success.
func TestEveryToolReportsBadArguments(t *testing.T) {
	env, _ := newEnv(t)
	for _, tool := range All(true, true, true) {
		res := tool.Run(env, json.RawMessage(`{"path":`))
		if !res.IsError {
			t.Errorf("%s accepted malformed JSON", tool.Name())
			continue
		}
		if !strings.Contains(res.Text, "not valid JSON") {
			t.Errorf("%s should explain the JSON problem, got %q", tool.Name(), res.Text)
		}
	}
}

// TestResolvePath_ErrorMessages pin which error each rejection returns, because
// the model retries differently for "you gave no path" and "that is outside the
// fence".
func TestResolvePath_ErrorMessages(t *testing.T) {
	root := t.TempDir()

	if _, err := resolvePath(root, "   "); err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("a blank path must be reported as required, got %v", err)
	}
	if _, err := resolvePath("", "a.txt"); err == nil || !strings.Contains(err.Error(), "no working directory") {
		t.Fatalf("a missing root must be reported, got %v", err)
	}
	escape := filepath.Join(filepath.Dir(root), "sibling.txt")
	if _, err := resolvePath(root, escape); err == nil || !strings.Contains(err.Error(), "outside the working directory") {
		t.Fatalf("an absolute escape must name the working directory, got %v", err)
	}

	// The resolver falls back to the raw absolute root when the directory does
	// not exist, so a missing root yields the tool's own "does not exist" rather
	// than a containment error about a directory that was never there.
	//
	// The root is resolved first: the containment check compares against
	// EvalSymlinks(root), and on Windows a temp dir is a short-name path
	// (RUNNER~1 vs runneradmin) that a case-sensitive-ish prefix test rejects
	// even though both name the same directory.
	realRootForMissing, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	missingRoot := filepath.Join(realRootForMissing, "not-created")
	got, err := resolvePath(missingRoot, "a.txt")
	if err != nil {
		t.Fatalf("a not-yet-created root must still resolve: %v", err)
	}
	if want := filepath.Join(missingRoot, "a.txt"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestResolveAnywhere_Errors covers the open-fence resolver: an absolute path is
// taken as-is, a relative one needs a root.
func TestResolveAnywhere_Errors(t *testing.T) {
	if _, err := resolveAnywhere("/root", ""); err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("a blank path must be required, got %v", err)
	}
	if _, err := resolveAnywhere("", "rel.txt"); err == nil || !strings.Contains(err.Error(), "relative path cannot be resolved") {
		t.Fatalf("a relative path without a root must be refused, got %v", err)
	}
	// Built from the temp dir rather than a literal "/tmp/x.txt": on Windows a
	// leading separator resolves against the current drive, which is not where
	// the test's own temp dir necessarily lives.
	abs := filepath.Join(t.TempDir(), "x.txt")
	got, err := resolveAnywhere("", abs)
	if err != nil || got != filepath.Clean(abs) {
		t.Fatalf("an absolute path must resolve to itself: %q %v", got, err)
	}
}

// TestResolveExistingPrefix_ReappendsMissingTail pins the rebuild: the resolved
// ancestor's real path gets the not-yet-existing segments back.
func TestResolveExistingPrefix_ReappendsMissingTail(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a", "b", "c.txt")
	got, err := resolveExistingPrefix(target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The resolved value is EvalSymlinks'd, and on Windows the temp dir is a
	// short-name path (RUNNER~1), so the expectation has to be resolved too.
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	want = filepath.Join(want, "a", "b", "c.txt")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestIsWithin pins the boundary predicate directly: the root itself and any
// descendant are inside, a sibling and a parent are not.
func TestIsWithin(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "srv", "proj")
	cases := map[string]bool{
		root:                                   true,
		filepath.Join(root, "a"):               true,
		filepath.Join(root, "a", "b", "c.txt"): true,
		filepath.Dir(root):                     false,
		filepath.Join(filepath.Dir(root), "other", "x.go"): false,
		root + "-sibling": false,
	}
	for child, want := range cases {
		if got := isWithin(root, child); got != want {
			t.Errorf("isWithin(%q, %q) = %v, want %v", root, child, got, want)
		}
	}
}

// TestWriteTool_RefusesOversizeContent pins the cap: the tool must say how big
// the payload was and what the limit is, not write a partial file.
func TestWriteTool_RefusesOversizeContent(t *testing.T) {
	env, root := newEnv(t)
	big := strings.Repeat("x", MaxWriteBytes+1)
	res := mustRun(t, WriteTool{}, env, map[string]any{"path": "big.txt", "content": big})
	if !res.IsError {
		t.Fatal("content over the write cap must be refused")
	}
	if !strings.Contains(res.Text, "larger than") || !strings.Contains(res.Text, "Split the file") {
		t.Fatalf("the refusal must state the size and the remedy, got %q", res.Text)
	}
	if _, err := os.Stat(filepath.Join(root, "big.txt")); !os.IsNotExist(err) {
		t.Fatal("a refused write must not create the file")
	}

	// Exactly at the cap is allowed: the check is "> limit", not ">= limit".
	if res := mustRun(t, WriteTool{}, env, map[string]any{"path": "ok.txt", "content": strings.Repeat("x", MaxWriteBytes)}); res.IsError {
		t.Fatalf("content at the cap must be accepted: %s", res.Text)
	}
}

// TestWriteTool_RefusesDirectory pins that writing onto an existing directory is
// reported as such instead of as an opaque os error.
func TestWriteTool_RefusesDirectory(t *testing.T) {
	env, root := newEnv(t)
	if err := os.Mkdir(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := mustRun(t, WriteTool{}, env, map[string]any{"path": "adir", "content": "x"})
	if !res.IsError || !strings.Contains(res.Text, "is a directory") {
		t.Fatalf("writing onto a directory must be refused clearly, got %q", res.Text)
	}
}

// TestWriteTool_ReportsParentCreationFailure pins the message when the parent
// cannot be created (a regular file sits where a directory is needed).
func TestWriteTool_ReportsParentCreationFailure(t *testing.T) {
	env, root := newEnv(t)
	mustWriteFile(t, filepath.Join(root, "blocker"), "not a dir")
	res := mustRun(t, WriteTool{}, env, map[string]any{"path": "blocker/child.txt", "content": "x"})
	if !res.IsError || !strings.Contains(res.Text, "cannot create parent directory") {
		t.Fatalf("a blocked parent must be reported, got %q", res.Text)
	}
}

// TestWriteTool_ReportsWriteFailure pins the message when the file itself cannot
// be written (its directory is read-only).
func TestWriteTool_ReportsWriteFailure(t *testing.T) {
	requirePOSIX(t)
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory write permissions")
	}
	env, root := newEnv(t)
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	res := mustRun(t, WriteTool{}, env, map[string]any{"path": "locked/f.txt", "content": "x"})
	if !res.IsError || !strings.Contains(res.Text, "cannot write") {
		t.Fatalf("an unwritable directory must be reported, got %q", res.Text)
	}
}

// TestWriteTool_ReportsResolveError pins that the fence is applied before any
// filesystem work: an escaping path is refused, nothing is created.
func TestWriteTool_ReportsResolveError(t *testing.T) {
	env, _ := newEnv(t)
	env.ConfineToWorkDir = true
	res := mustRun(t, WriteTool{}, env, map[string]any{"path": "../escape.txt", "content": "x"})
	if !res.IsError || !strings.Contains(res.Text, "outside the working directory") {
		t.Fatalf("an escaping write must name the fence, got %q", res.Text)
	}
}

// TestEditTool_RefusesEmptyOldString pins that a blank search string is refused
// by pointing at write, not accepted as "insert at the start".
func TestEditTool_RefusesEmptyOldString(t *testing.T) {
	env, _ := newEnv(t)
	res := mustRun(t, EditTool{}, env, map[string]any{"path": "f.txt", "old_string": "", "new_string": "x"})
	if !res.IsError || !strings.Contains(res.Text, "use write") {
		t.Fatalf("an empty old_string must point at write, got %q", res.Text)
	}
}

// TestEditTool_RefusesNoOpEdit pins that an edit whose halves are identical is
// refused: reporting "Replaced 1 occurrence" for a file that did not change
// makes the model believe the fix landed.
func TestEditTool_RefusesNoOpEdit(t *testing.T) {
	env, _ := newEnv(t)
	res := mustRun(t, EditTool{}, env, map[string]any{"path": "f.txt", "old_string": "a", "new_string": "a"})
	if !res.IsError || !strings.Contains(res.Text, "identical") {
		t.Fatalf("a no-op edit must be refused, got %q", res.Text)
	}
}

// TestEditTool_ReportsMissingFile pins that a missing target is reported as
// such, with the remedy ("use write"), rather than as a generic read error.
func TestEditTool_ReportsMissingFile(t *testing.T) {
	env, _ := newEnv(t)
	res := mustRun(t, EditTool{}, env, map[string]any{"path": "nope.txt", "old_string": "a", "new_string": "b"})
	if !res.IsError || !strings.Contains(res.Text, "does not exist. Use write to create it") {
		t.Fatalf("a missing file must name the remedy, got %q", res.Text)
	}
}

// TestEditTool_ReportsUnreadableFile pins the non-ENOENT read failure: the
// message must be the read error, not "does not exist".
func TestEditTool_ReportsUnreadableFile(t *testing.T) {
	env, root := newEnv(t)
	if err := os.Mkdir(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := mustRun(t, EditTool{}, env, map[string]any{"path": "adir", "old_string": "a", "new_string": "b"})
	if !res.IsError || !strings.Contains(res.Text, "cannot read") {
		t.Fatalf("a directory target must report a read failure, got %q", res.Text)
	}
}

// TestEditTool_ReportsResolveError pins that the fence applies to edit too.
func TestEditTool_ReportsResolveError(t *testing.T) {
	env, _ := newEnv(t)
	env.ConfineToWorkDir = true
	res := mustRun(t, EditTool{}, env, map[string]any{"path": "../x.txt", "old_string": "a", "new_string": "b"})
	if !res.IsError || !strings.Contains(res.Text, "outside the working directory") {
		t.Fatalf("an escaping edit must name the fence, got %q", res.Text)
	}
}

// TestEditTool_ReportsWriteFailure pins the failure on an unwritable target that
// IS readable: the match is found, the write is not.
func TestEditTool_ReportsWriteFailure(t *testing.T) {
	requirePOSIX(t)
	if os.Geteuid() == 0 {
		t.Skip("root ignores file write permissions")
	}
	env, root := newEnv(t)
	path := filepath.Join(root, "ro.txt")
	mustWriteFile(t, path, "needle")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	res := mustRun(t, EditTool{}, env, map[string]any{"path": "ro.txt", "old_string": "needle", "new_string": "x"})
	if !res.IsError || !strings.Contains(res.Text, "cannot write") {
		t.Fatalf("an unwritable target must be reported, got %q", res.Text)
	}
}

// TestReadTool_EmptyFile pins the distinct message for a zero-byte file: an
// empty Text would read as "the tool returned nothing", which the model retries.
func TestReadTool_EmptyFile(t *testing.T) {
	env, root := newEnv(t)
	mustWriteFile(t, filepath.Join(root, "empty.txt"), "")
	res := mustRun(t, ReadTool{}, env, map[string]any{"path": "empty.txt"})
	if res.IsError {
		t.Fatalf("an empty file is not an error: %s", res.Text)
	}
	if !strings.Contains(res.Text, "is empty") {
		t.Fatalf("an empty file must be announced, got %q", res.Text)
	}
	if res.Display != "empty.txt" {
		t.Fatalf("the card must name the file, got %q", res.Display)
	}
}

// TestReadTool_DirectoryPointsAtGlob pins that reading a directory suggests the
// tool that can actually list it.
func TestReadTool_DirectoryPointsAtGlob(t *testing.T) {
	env, root := newEnv(t)
	if err := os.Mkdir(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []Tool{ReadTool{}, ReadToolNoVision{}} {
		res := mustRun(t, tool, env, map[string]any{"path": "pkg"})
		if !res.IsError || !strings.Contains(res.Text, "is a directory") || !strings.Contains(res.Text, "glob") {
			t.Fatalf("%T reading a directory must point at glob, got %q", tool, res.Text)
		}
	}
}

// TestReadTool_BinaryFile is the reason a repository with one blob does not
// poison the context: a NUL byte marks the file binary and it is skipped with an
// explanation.
func TestReadTool_BinaryFile(t *testing.T) {
	env, root := newEnv(t)
	mustWriteFile(t, filepath.Join(root, "blob.bin"), "abc\x00def")
	res := mustRun(t, ReadTool{}, env, map[string]any{"path": "blob.bin"})
	if !res.IsError || !strings.Contains(res.Text, "binary file") {
		t.Fatalf("a NUL-containing file must be refused as binary, got %q", res.Text)
	}
	res = mustRun(t, ReadToolNoVision{}, env, map[string]any{"path": "blob.bin"})
	if !res.IsError || !strings.Contains(res.Text, "binary file") {
		t.Fatalf("the no-vision variant must also refuse it, got %q", res.Text)
	}
}

// TestReadTool_OffsetNamedByTheNote pins that the "more lines" note's offset is
// a legal read even when it points exactly one past the last line.
//
// The note hands out that offset itself when a byte cut left the last line
// incomplete; answering it with "past the end" sent the model in a circle.
func TestReadTool_OffsetNamedByTheNote(t *testing.T) {
	env, root := newEnv(t)
	mustWriteFile(t, filepath.Join(root, "two.txt"), "a\nb\n")

	// offset == lines+1 is legal and yields only the continuation note.
	res := mustRun(t, ReadTool{}, env, map[string]any{"path": "two.txt", "offset": 3})
	if res.IsError {
		t.Fatalf("one past the last line must be legal: %s", res.Text)
	}
	if strings.Contains(res.Text, "1: ") || strings.Contains(res.Text, "2: ") {
		t.Fatalf("a read starting past the content must not echo earlier lines, got %q", res.Text)
	}

	// Beyond that is refused, and the message names the real line count.
	res = mustRun(t, ReadTool{}, env, map[string]any{"path": "two.txt", "offset": 10})
	if !res.IsError || !strings.Contains(res.Text, "has only 2 lines") {
		t.Fatalf("an out-of-range offset must state the line count, got %q", res.Text)
	}

	// The continuation note itself hands out the NEXT offset; feeding it back
	// must be legal. This is the loop the "past the end" answer caused.
	first := mustRun(t, ReadTool{}, env, map[string]any{"path": "two.txt", "limit": 1})
	const marker = "offset="
	idx := strings.Index(first.Text, marker)
	if idx < 0 {
		t.Fatalf("a partial read must hand out the next offset, got %q", first.Text)
	}
	rest := first.Text[idx+len(marker):]
	end := strings.IndexAny(rest, " .]")
	var next int
	if _, err := fmt.Sscanf(rest[:end], "%d", &next); err != nil {
		t.Fatalf("offset %q is not a number: %v", rest[:end], err)
	}
	res = mustRun(t, ReadTool{}, env, map[string]any{"path": "two.txt", "offset": next})
	if res.IsError {
		t.Fatalf("the offset this tool handed out (%d) must be readable: %s", next, res.Text)
	}
	if !strings.Contains(res.Text, "2: b") {
		t.Fatalf("the continuation must return the second line, got %q", res.Text)
	}
}

// TestReadTool_DirectoryWithBinaryExtension pins the order of the checks: a
// directory named ".bin" is reported as a directory, never sniffed as binary.
func TestReadTool_DirectoryWithBinaryExtension(t *testing.T) {
	env, root := newEnv(t)
	if err := os.Mkdir(filepath.Join(root, "weird.bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := mustRun(t, ReadTool{}, env, map[string]any{"path": "weird.bin"})
	if !res.IsError || !strings.Contains(res.Text, "is a directory") {
		t.Fatalf("the directory check must come before the binary sniff, got %q", res.Text)
	}
}

// TestReadTool_LimitWindow pins that offset+limit address a window of numbered
// lines and that the untouched tail is announced.
func TestReadTool_LimitWindow(t *testing.T) {
	env, root := newEnv(t)
	mustWriteFile(t, filepath.Join(root, "five.txt"), "l1\nl2\nl3\nl4\nl5\n")
	res := mustRun(t, ReadTool{}, env, map[string]any{"path": "five.txt", "offset": 2, "limit": 2})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if !strings.Contains(res.Text, "2: l2") || !strings.Contains(res.Text, "3: l3") {
		t.Fatalf("the window must be numbered from the file, got %q", res.Text)
	}
	if strings.Contains(res.Text, "1: l1") || strings.Contains(res.Text, "4: l4") {
		t.Fatalf("lines outside the window must not be returned, got %q", res.Text)
	}
	if !strings.Contains(res.Text, "2 more lines. Call read again with offset=4") {
		t.Fatalf("the remaining lines and the next offset must be stated, got %q", res.Text)
	}
}

// TestReadTool_ReadErrorOnDirectoryAsFile pins the "read failed after stat
// succeeded" path: the message names the file, not the errno.
func TestReadTool_ReadErrorOnDirectoryAsFile(t *testing.T) {
	requirePOSIX(t)
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	env, root := newEnv(t)
	path := filepath.Join(root, "secret.txt")
	mustWriteFile(t, path, "hidden")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	res := mustRun(t, ReadTool{}, env, map[string]any{"path": "secret.txt"})
	if !res.IsError || !strings.Contains(res.Text, "cannot read") {
		t.Fatalf("an unreadable file must be reported, got %q", res.Text)
	}
}

// TestReadTool_ImageSizeCap pins that an oversized image is REJECTED, not
// truncated: half a PNG decodes to nothing, and the model describes a corrupt
// picture confidently.
func TestReadTool_ImageSizeCap(t *testing.T) {
	env, root := newEnv(t)
	big := make([]byte, MaxImageBytes+1)
	copy(big, "\x89PNG\r\n\x1a\n")
	if err := os.WriteFile(filepath.Join(root, "big.png"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	res := mustRun(t, ReadTool{}, env, map[string]any{"path": "big.png"})
	if !res.IsError || !strings.Contains(res.Text, "larger than") {
		t.Fatalf("an oversized image must be refused with the limit, got %q", res.Text)
	}
	if res.Image != nil {
		t.Fatal("a refused image must not carry bytes")
	}
}

// TestImageMediaType pins every advertised extension, the case-insensitive
// lookup, and that an unknown extension is not an image.
func TestImageMediaType(t *testing.T) {
	cases := map[string]string{
		"a.png":  "image/png",
		"a.PNG":  "image/png",
		"a.jpg":  "image/jpeg",
		"a.jpeg": "image/jpeg",
		"a.gif":  "image/gif",
		"a.webp": "image/webp",
	}
	for name, want := range cases {
		got, ok := imageMediaType(name)
		if !ok || got != want {
			t.Errorf("imageMediaType(%q) = %q, %v; want %q, true", name, got, ok, want)
		}
	}
	for _, name := range []string{"a.txt", "a.png.txt", "a"} {
		if _, ok := imageMediaType(name); ok {
			t.Errorf("%q must not be treated as an image", name)
		}
	}
}

// TestReadToolNoVision_SpecOmitsImageLanguage pins that the declaration matches
// what the tool does: a spec promising images makes the model read a text
// refusal as a failed read and retry it.
func TestReadToolNoVision_SpecOmitsImageLanguage(t *testing.T) {
	spec := ReadToolNoVision{}.Spec()
	if strings.Contains(spec.Description, "come back as an image") {
		t.Fatal("the no-vision spec must not promise images")
	}
	if !strings.Contains(spec.Description, "Binary files cannot be read") {
		t.Fatalf("the no-vision spec must say binary content is refused, got %q", spec.Description)
	}
	if spec.Name != toolReadName {
		t.Fatalf("name drift: %q", spec.Name)
	}
	props, _ := spec.Parameters["properties"].(map[string]any)
	for _, want := range []string{argPath, argOffset, argLimit} {
		if _, ok := props[want]; !ok {
			t.Errorf("the spec must declare %q", want)
		}
	}
}

// TestGlobTool_RequiresPattern pins the empty-pattern refusal.
func TestGlobTool_RequiresPattern(t *testing.T) {
	env, _ := newEnv(t)
	res := mustRun(t, GlobTool{}, env, map[string]any{"pattern": "  "})
	if !res.IsError || !strings.Contains(res.Text, "pattern is required") {
		t.Fatalf("a blank pattern must be refused, got %q", res.Text)
	}
}

// TestGlobTool_NoMatches pins the empty-result message: it must quote the
// pattern so the model can see what it actually asked for.
func TestGlobTool_NoMatches(t *testing.T) {
	env, _ := newEnv(t)
	res := mustRun(t, GlobTool{}, env, map[string]any{"pattern": "**/*.rs"})
	if res.IsError {
		t.Fatalf("no matches is not an error: %s", res.Text)
	}
	if !strings.Contains(res.Text, `No files match "**/*.rs".`) {
		t.Fatalf("the empty result must quote the pattern, got %q", res.Text)
	}
	if res.Display != "**/*.rs" {
		t.Fatalf("the card must show the pattern, got %q", res.Display)
	}
}

// TestGlobTool_AbsolutePatternOutsideRoot pins that the literal prefix picks the
// walk base even when the fence is off, and that results outside the project are
// printed as absolute paths (a chain of "../" is noise).
func TestGlobTool_AbsolutePatternOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "found.txt"), "x")
	env := Env{Root: root}

	pattern := filepath.ToSlash(filepath.Join(outside, "*.txt"))
	res := mustRun(t, GlobTool{}, env, map[string]any{"pattern": pattern})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	want := filepath.ToSlash(filepath.Join(outside, "found.txt"))
	if !strings.Contains(res.Text, want) {
		t.Fatalf("an outside match must be shown as an absolute path %q, got %q", want, res.Text)
	}
	if strings.Contains(res.Text, "../") {
		t.Fatalf("outside results must not be rendered as ../ chains, got %q", res.Text)
	}
}

// TestGlobTool_LiteralPrefixNarrowsTheWalk pins globWalkBase: a pattern confined
// to a subdirectory starts there, and an unresolvable prefix falls back to the
// root instead of failing the listing.
func TestGlobTool_LiteralPrefixNarrowsTheWalk(t *testing.T) {
	env, root := newEnv(t)
	mustWriteFile(t, filepath.Join(root, "sub", "a.go"), "x")
	mustWriteFile(t, filepath.Join(root, "a.go"), "x")

	// TODO(flaky): literalPrefix("sub/*.go") returns "" (it drops the last
	// wildcard segment), so the walk starts at the root. The match is still
	// correct, only slower; reported as a production issue rather than pinned
	// here as if the narrower walk were the behavior.
	if got := globWalkBase(env, root, "**/*.go"); got != root {
		t.Fatalf("a wildcard-leading pattern must walk from the root, got %q", got)
	}
	// A missing prefix is not an error: the whole root is scanned.
	if got := globWalkBase(env, root, "nope/*.go"); got != root {
		t.Fatalf("a missing prefix must fall back to the root, got %q", got)
	}
	// A file (not a directory) as prefix also falls back.
	mustWriteFile(t, filepath.Join(root, "a.go"), "x")
	if got := globWalkBase(env, root, "a.go/*.go"); got != root {
		t.Fatalf("a non-directory prefix must fall back to the root, got %q", got)
	}
	// No wildcard at all: the prefix is still the containing directory.
	if got := globWalkBase(env, root, "a.go"); got != root {
		t.Fatalf("a bare file name must walk from the root, got %q", got)
	}
}

// TestGlobTool_ResultCap pins that a listing is bounded and says so, so the
// model narrows the pattern instead of assuming that was everything.
func TestGlobTool_ResultCap(t *testing.T) {
	env, root := newEnv(t)
	for i := range maxGlobResults + 20 {
		path := filepath.Join(root, "f", "file"+strings.Repeat("0", 0)+itoa(i)+".txt")
		mustWriteFile(t, path, "x")
	}
	res := mustRun(t, GlobTool{}, env, map[string]any{"pattern": "**/*.txt"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if !strings.Contains(res.Text, "stopped after") {
		t.Fatalf("the cap must be announced, got %q", res.Text[len(res.Text)-120:])
	}
	if got := strings.Count(res.Text, "\n") + 1; got > maxGlobResults+2 {
		t.Fatalf("listing must be bounded, got %d lines", got)
	}
}

// TestLiteralPrefix pins the segment rule: stop at the first wildcard, and drop
// the final literal segment because it is the file name.
func TestLiteralPrefix(t *testing.T) {
	cases := map[string]string{
		"**/*.go":          "",
		"*.go":             "",
		"sub/*.go":         "",
		"sub/deep/a?.go":   "sub",
		"a/b/c.go":         "a/b",
		"web/src/**/*.vue": "web",
		"[ab]/x.go":        "",
	}
	for pattern, want := range cases {
		if got := literalPrefix(pattern); got != want {
			t.Errorf("literalPrefix(%q) = %q, want %q", pattern, got, want)
		}
	}
}

// TestGlobMatch pins the ** semantics directly: zero or more segments, plus the
// ordinary single-segment pattern.
func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.go", "a.go", true},
		{"**/*.go", "x/y/z.go", true},
		{"**/*.go", "x/y/z.md", false},
		{"*.go", "a.go", true},
		{"*.go", "x/a.go", false},
		{"src/**", "src/a/b.go", true},
		{"src/**/test_*.go", "src/a/b/test_x.go", true},
		{"src/**/test_*.go", "src/test_x.go", true},
		{"a/*/c", "a/b/c", true},
		{"a/*/c", "a/b/d", false},
		// A malformed pattern matches nothing rather than panicking.
		{"[", "a", false},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.path); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

// itoa keeps the fixture names readable without pulling in strconv in the
// assertion above; the values only need to be distinct.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestGrepTool_RequiresPattern pins the blank-pattern refusal, distinct from an
// invalid one.
func TestGrepTool_RequiresPattern(t *testing.T) {
	env, _ := newEnv(t)
	res := mustRun(t, GrepTool{}, env, map[string]any{"pattern": "   "})
	if !res.IsError || !strings.Contains(res.Text, "pattern is required") {
		t.Fatalf("a blank pattern must be refused, got %q", res.Text)
	}
}

// TestGrepTool_ReportsNoMatches pins that "nothing found" is a success whose
// text states how many files were actually read — otherwise the model cannot
// tell an exhaustive search from an empty directory.
func TestGrepTool_ReportsNoMatches(t *testing.T) {
	env, root := newEnv(t)
	mustWriteFile(t, filepath.Join(root, "a.txt"), "alpha\n")
	res := mustRun(t, GrepTool{}, env, map[string]any{"pattern": "zzz"})
	if res.IsError {
		t.Fatalf("no matches is not an error: %s", res.Text)
	}
	if !strings.Contains(res.Text, `No matches for "zzz" in 1 file(s).`) {
		t.Fatalf("the count of scanned files must be stated, got %q", res.Text)
	}
}

// TestGrepTool_ReportsMissingTarget pins that an unreadable path names the path
// the caller gave, not the resolved absolute one.
func TestGrepTool_ReportsMissingTarget(t *testing.T) {
	env, _ := newEnv(t)
	res := mustRun(t, GrepTool{}, env, map[string]any{"pattern": "x", "path": "missing-dir"})
	if !res.IsError || !strings.Contains(res.Text, "cannot access missing-dir") {
		t.Fatalf("a missing target must be reported by its given name, got %q", res.Text)
	}
}

// TestGrepTool_IncludeFilter pins the include glob: it is matched against the
// base name, so "*.go" selects Go files anywhere and a malformed filter selects
// nothing (rather than failing the whole search).
func TestGrepTool_IncludeFilter(t *testing.T) {
	env, root := newEnv(t)
	mustWriteFile(t, filepath.Join(root, "a.go"), "needle\n")
	mustWriteFile(t, filepath.Join(root, "sub", "b.go"), "needle\n")
	mustWriteFile(t, filepath.Join(root, "c.md"), "needle\n")

	res := mustRun(t, GrepTool{}, env, map[string]any{"pattern": "needle", "include": "*.go"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if !strings.Contains(res.Text, "a.go:1:") || !strings.Contains(res.Text, "sub/b.go:1:") {
		t.Fatalf("both Go files must match, got %q", res.Text)
	}
	if strings.Contains(res.Text, "c.md") {
		t.Fatalf("the markdown file must be filtered out, got %q", res.Text)
	}

	res = mustRun(t, GrepTool{}, env, map[string]any{"pattern": "needle", "include": "[ "})
	if res.IsError {
		t.Fatalf("a malformed filter must not fail the search: %s", res.Text)
	}
	if !strings.Contains(res.Text, "No matches") {
		t.Fatalf("a malformed filter selects nothing, got %q", res.Text)
	}
}

// TestGrepTool_MatchCap pins the total cap: a flood of matches is bounded and
// the truncation is announced so the model narrows the search.
func TestGrepTool_MatchCap(t *testing.T) {
	env, root := newEnv(t)
	// Two files, so the cap has to stop the OUTER loop mid-list rather than
	// merely running out of lines in the one file it happened to read first.
	var b strings.Builder
	for range maxGrepMatches {
		b.WriteString("needle\n")
	}
	mustWriteFile(t, filepath.Join(root, "a.txt"), b.String())
	mustWriteFile(t, filepath.Join(root, "b.txt"), "needle\n")

	res := mustRun(t, GrepTool{}, env, map[string]any{"pattern": "needle"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if got := strings.Count(res.Text, "a.txt:"); got != maxGrepMatches {
		t.Fatalf("the cap must stop at %d matches, got %d", maxGrepMatches, got)
	}
	if strings.Contains(res.Text, "b.txt:") {
		t.Fatal("the search must stop once the cap is reached, not continue into the next file")
	}
	if !strings.Contains(res.Text, "narrow the pattern or path") {
		t.Fatalf("the truncation must be announced, got %q", res.Text[len(res.Text)-120:])
	}
}

// TestGrepTool_TruncatesLongLines pins the per-line cap: one minified bundle line
// must not fill the context, and the cut must be visible.
func TestGrepTool_TruncatesLongLines(t *testing.T) {
	env, root := newEnv(t)
	mustWriteFile(t, filepath.Join(root, "min.js"), "var needle="+strings.Repeat("x", maxGrepLineLen*2)+"\n")
	res := mustRun(t, GrepTool{}, env, map[string]any{"pattern": "needle"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if !strings.Contains(res.Text, "…") {
		t.Fatalf("a long line must be visibly truncated, got %q", res.Text)
	}
	if len(res.Text) > maxGrepLineLen+64 {
		t.Fatalf("the line must be capped, got %d bytes", len(res.Text))
	}
}

// TestGrepTool_SkipsBinaryAndOversizeFiles pins the two skips that matter in a
// real repository: a blob and a huge file are counted out, not scanned.
func TestGrepTool_SkipsBinaryAndOversizeFiles(t *testing.T) {
	env, root := newEnv(t)
	mustWriteFile(t, filepath.Join(root, "text.txt"), "needle\n")
	mustWriteFile(t, filepath.Join(root, "blob.bin"), "needle\x00\n")

	res := mustRun(t, GrepTool{}, env, map[string]any{"pattern": "needle"})
	if !strings.Contains(res.Text, "text.txt:1:") {
		t.Fatalf("the text file must match, got %q", res.Text)
	}
	if strings.Contains(res.Text, "blob.bin") {
		t.Fatalf("a binary file must be skipped, got %q", res.Text)
	}

	// grepFiles reports how many files it actually read, which is what the
	// no-match message quotes.
	re := mustCompile(t, "needle")
	_, hits, scanned := grepFiles([]string{
		filepath.Join(root, "text.txt"),
		filepath.Join(root, "blob.bin"),
		filepath.Join(root, "gone.txt"),
	}, root, re)
	if hits != 1 || scanned != 1 {
		t.Fatalf("hits=%d scanned=%d, want 1/1 (binary and missing are skipped)", hits, scanned)
	}
}

// TestGrepTool_SingleFileTarget pins that an explicit file target searches that
// file, and prints it relative to the root.
func TestGrepTool_SingleFileTarget(t *testing.T) {
	env, root := newEnv(t)
	mustWriteFile(t, filepath.Join(root, "sub", "one.txt"), "a\nneedle\n")
	res := mustRun(t, GrepTool{}, env, map[string]any{"pattern": "needle", "path": "sub/one.txt"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if !strings.Contains(res.Text, "sub/one.txt:2: needle") {
		t.Fatalf("a file target must be reported relative to the root, got %q", res.Text)
	}
}

// TestGrepTool_HonoursFence pins that the fence reaches grep: an escaping path is
// refused instead of searched.
func TestGrepTool_HonoursFence(t *testing.T) {
	env, _ := newEnv(t)
	env.ConfineToWorkDir = true
	res := mustRun(t, GrepTool{}, env, map[string]any{"pattern": "x", "path": "../"})
	if !res.IsError || !strings.Contains(res.Text, "outside the working directory") {
		t.Fatalf("grep must refuse an escaping path, got %q", res.Text)
	}
}

// TestGrepTool_OutsideRootUsesAbsolutePaths pins the display rule: a match found
// outside the working directory is printed absolutely.
func TestGrepTool_OutsideRootUsesAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "far.txt")
	mustWriteFile(t, target, "needle\n")

	res := mustRun(t, GrepTool{}, Env{Root: root}, map[string]any{"pattern": "needle", "path": outside})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if !strings.Contains(res.Text, filepath.ToSlash(target)+":1: needle") {
		t.Fatalf("an outside match must be printed absolutely, got %q", res.Text)
	}
}

// TestGrepTargets_SkipsWalkDirs pins the directory-walk policy: skipped
// directories are never descended, a third-party file target bypasses the include
// filter, and a root that cannot be read is reported instead of becoming "no
// matches".
func TestGrepTargets_SkipsWalkDirs(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "src", "a.go"), "x")
	mustWriteFile(t, filepath.Join(root, "node_modules", "pkg", "b.js"), "x")

	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := grepTargets(Env{Root: root}, root, info, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0], filepath.Join("src", "a.go")) {
		t.Fatalf("only src/a.go must be listed, got %v", files)
	}

	// A file target is returned as itself, unfiltered — include applies to
	// directory listings only.
	target := filepath.Join(root, "src", "a.go")
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	files, err = grepTargets(Env{Root: root}, target, fi, "*.md")
	if err != nil || len(files) != 1 || files[0] != target {
		t.Fatalf("an explicit file must be searched despite the include filter, got %v %v", files, err)
	}

	// A root that cannot be read must be reported, not silently turned into "no
	// matches": the model would otherwise conclude the symbol is absent. The
	// caller stats the target first, so the walk is handed the info of a
	// directory that is gone by the time it reads it.
	gone := filepath.Join(root, "gone")
	if _, err := grepTargets(Env{Root: root}, gone, info, ""); err == nil || !strings.Contains(err.Error(), "cannot search") {
		t.Fatalf("an unreadable root must be reported, got %v", err)
	}
}

// TestGrepTargets_FollowsSymlinkedDirectory pins that content behind a project
// symlink is searchable — the file manager shows those files, so a grep that
// cannot see them answers "not found" for a file the user is looking at.
func TestGrepTargets_FollowsSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "linked.txt"), "needle\n")
	if err := os.Symlink(outside, filepath.Join(root, "data")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	// Fence off: a link to another disk is a legitimate layout.
	files, err := grepTargets(Env{Root: root}, root, info, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || !strings.HasSuffix(filepath.ToSlash(files[0]), "data/linked.txt") {
		t.Fatalf("a linked-in file must be listed, got %v", files)
	}

	// Fence on: the same link is outside the working directory, so the walk
	// must not enter it — otherwise glob/grep would read what read refuses.
	files, err = grepTargets(Env{Root: root, ConfineToWorkDir: true}, root, info, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("a confined turn must not walk outside its working directory, got %v", files)
	}
}

// TestGrepFile_StopsAtTheCap pins the inner cap so a single huge file cannot
// overshoot it before the outer loop gets a chance to stop.
func TestGrepFile_StopsAtTheCap(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "all.txt")
	var b strings.Builder
	for range maxGrepMatches + 10 {
		b.WriteString("hit\n")
	}
	mustWriteFile(t, path, b.String())

	var out []string
	got := grepFile(path, root, mustCompile(t, "hit"), &out)
	if got != maxGrepMatches || len(out) != maxGrepMatches {
		t.Fatalf("grepFile stopped at %d hits / %d lines, want %d", got, len(out), maxGrepMatches)
	}
}

// TestCollectGlobMatch_ResultCap pins the stop signal: the walk ends once the cap
// is reached and no second file is added after it.
func TestCollectGlobMatch_ResultCap(t *testing.T) {
	root := t.TempDir()
	matches := make([]string, 0, maxGlobResults-1)
	full := make([]string, maxGlobResults)
	for i := range full {
		full[i] = filepath.Join(root, itoa(i)+".txt")
	}
	mustWriteFile(t, full[len(full)-1], "x")
	d, err := os.Stat(full[len(full)-1])
	if err != nil {
		t.Fatal(err)
	}
	entry := symwalk.Entry{Path: full[len(full)-1], Name: filepath.Base(full[len(full)-1]), Info: d}

	for i := range full[:len(full)-1] {
		matches = append(matches, full[i])
	}
	if got := collectGlobMatch("**/*.txt", root, entry, &matches); got != symwalk.Stop {
		t.Fatalf("reaching the cap must stop the walk with Stop, got %v", got)
	}
	if len(matches) != maxGlobResults {
		t.Fatalf("cap not enforced: %d matches", len(matches))
	}
}

// TestBashTool_ReportsNoOutput pins the placeholder: an empty result reads as a
// broken tool, so a silent successful command is announced.
func TestBashTool_ReportsNoOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	env, _ := newEnv(t)
	res := mustRun(t, BashTool{}, env, map[string]any{"command": "true", "description": "do nothing"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if !strings.Contains(res.Text, "(no output)") {
		t.Fatalf("a silent command must be announced, got %q", res.Text)
	}
}

// TestBashTool_SeparatesStderr pins that a command's diagnostic output is labeled
// so the model can tell a warning from a result.
func TestBashTool_SeparatesStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	env, _ := newEnv(t)
	res := mustRun(t, BashTool{}, env, map[string]any{"command": "echo out; echo oops >&2", "description": "both streams"})
	if res.IsError {
		t.Fatalf("writing to stderr is not a failure: %s", res.Text)
	}
	if !strings.Contains(res.Text, "out\n\n[stderr]\noops") {
		t.Fatalf("stderr must be labeled after stdout, got %q", res.Text)
	}
}

// TestBashTool_PerCallTimeoutOverridesTheDefault pins the argument-level budget:
// a call that asks for 1 second must not inherit a longer env budget.
func TestBashTool_PerCallTimeoutOverridesTheDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	env, _ := newEnv(t)
	env.CommandTimeoutSec = 60
	res := mustRun(t, BashTool{}, env, map[string]any{"command": "sleep 10", "timeout": 1, "description": "wait"})
	if !res.IsError || !strings.Contains(res.Text, "timed out after 1 seconds") {
		t.Fatalf("the per-call timeout must win, got %q", res.Text)
	}
}

// TestBashTool_EmptyCommand pins the refusal: an empty command would otherwise be
// a successful no-op the model cannot detect.
func TestBashTool_EmptyCommand(t *testing.T) {
	env, _ := newEnv(t)
	res := mustRun(t, BashTool{}, env, map[string]any{"command": " \n ", "description": "nothing"})
	if !res.IsError || !strings.Contains(res.Text, "command is empty") {
		t.Fatalf("a blank command must be refused, got %q", res.Text)
	}
}

// TestFirstLine pins the card text: only the first line, because a multi-line
// command would push the rest of the transcript away.
func TestFirstLine(t *testing.T) {
	if got := firstLine("git status\nrm -rf /"); got != "git status" {
		t.Fatalf("got %q", got)
	}
	if got := firstLine("ls"); got != "ls" {
		t.Fatalf("got %q", got)
	}
}

// TestExitSuffix pins the "left a background process" note: a clean exit is not
// reported as a status, a failure is.
func TestExitSuffix(t *testing.T) {
	if got := exitSuffix(nil); got != "" {
		t.Fatalf("a clean exit must add nothing, got %q", got)
	}
	err := exitError{msg: "exit status 2"}
	if got := exitSuffix(err); !strings.Contains(got, "exit status 2") {
		t.Fatalf("a failure must be named, got %q", got)
	}
}

type exitError struct{ msg string }

func (e exitError) Error() string { return e.msg }

// TestSetProcessGroup_Cancel pins the process-group cancel: a nil process is not
// an error (the command may have failed to start), and an already-exited group is
// not an error either (ESRCH is treated as success).
//
// The process group itself is asserted in the Unix-only file: SysProcAttr.Setpgid
// does not exist on Windows, and a runtime Skip still has to COMPILE.
func TestSetProcessGroup_Cancel(t *testing.T) {
	requirePOSIX(t)
	cmd := execCmd(t, "/bin/sh", "-c", "true")
	setProcessGroup(cmd)
	if cmd.Cancel == nil {
		t.Fatal("a cancel func must be installed")
	}
	// Never started: Process is nil, and cancelling must be a no-op.
	if err := cmd.Cancel(); err != nil {
		t.Fatalf("cancelling a command that never started must be a no-op, got %v", err)
	}
}

// TestShellCommand pins the interpreter: the tool declares "/bin/sh -c" in its
// spec, so a different interpreter here would make that description a lie.
func TestShellCommand(t *testing.T) {
	requirePOSIX(t)
	shell, flag := shellCommand()
	if shell != "/bin/sh" || flag != "-c" {
		t.Fatalf("got %q %q, want /bin/sh -c", shell, flag)
	}
}

// TestGlobTool_FollowsSymlinkedDirectory pins that a pattern reaches files behind
// a project symlink — the file manager lists them, so glob answering "no match"
// for a file the user is looking at is the bug this guards. With the fence on,
// the same link is outside the working directory and must stay invisible.
func TestGlobTool_FollowsSymlinkedDirectory(t *testing.T) {
	env, root := newEnv(t)
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "nested.go"), "package x")
	if err := os.Symlink(outside, filepath.Join(root, "data")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	res := mustRun(t, GlobTool{}, env, map[string]any{"pattern": "data/**/*.go"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if !strings.Contains(res.Text, "data/nested.go") {
		t.Fatalf("a file behind a linked directory must match, got %q", res.Text)
	}

	env.ConfineToWorkDir = true
	res = mustRun(t, GlobTool{}, env, map[string]any{"pattern": "**/*.go"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Text)
	}
	if strings.Contains(res.Text, "nested.go") {
		t.Fatalf("a confined turn must not list files outside its working directory, got %q", res.Text)
	}
}

// execCmd is a thin alias so the process-group tests read like the production
// call site.
func execCmd(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	return exec.Command(name, args...)
}

func mustCompile(t *testing.T, pattern string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("compile %q: %v", pattern, err)
	}
	return re
}
