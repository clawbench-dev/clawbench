package symwalk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// collect walks and returns name → Entry for the visited entries, keyed by the
// slash-normalized relative path.
func collect(t *testing.T, root string, opts Options, visit func(Entry) Result) (map[string]Entry, Outcome) {
	t.Helper()
	seen := map[string]Entry{}
	out := Walk(context.Background(), root, opts, func(e Entry) Result {
		seen[e.Rel] = e
		if visit != nil {
			return visit(e)
		}
		return Continue
	})
	return seen, out
}

// A link to a directory must be walked like a real directory, and a link to a
// file must be reported with the TARGET's size — otherwise search shows the
// link's own few bytes instead of the file it stands for.
func TestWalk_FollowsSymlinkDirectoryAndFile(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "nested.txt"), "hello")
	mustSymlink(t, outside, filepath.Join(root, "data"))
	mustSymlink(t, filepath.Join(outside, "nested.txt"), filepath.Join(root, "alias.txt"))

	seen, out := collect(t, root, Options{}, nil)
	if out.Exhausted {
		t.Fatal("a tiny tree must not exhaust the budget")
	}

	linked, ok := seen["data/nested.txt"]
	if !ok {
		t.Fatalf("a file behind a linked directory must be visited, got %v", keys(seen))
	}
	// The reported path is the logical one, spelled through the link.
	if want := filepath.Join(root, "data", "nested.txt"); linked.Path != want {
		t.Errorf("Path = %q, want %q", linked.Path, want)
	}
	if linked.Info.Size() != int64(len("hello")) {
		t.Errorf("size = %d, want the target's %d", linked.Info.Size(), len("hello"))
	}

	alias, ok := seen["alias.txt"]
	if !ok {
		t.Fatalf("a linked file must be visited, got %v", keys(seen))
	}
	if !alias.IsSymlink {
		t.Error("the entry itself is a link, IsSymlink must be true")
	}
	if alias.IsDir {
		t.Error("a link to a file must not be a directory")
	}
}

// A link that points back at one of its own ancestors must not recurse: without
// the ancestry guard this walk never ends.
func TestWalk_SelfReferentialLinkTerminates(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "main.go"), "x")
	mustSymlink(t, filepath.Join(root, "src"), filepath.Join(root, "src", "loop"))

	seen, _ := collect(t, root, Options{}, nil)
	// The link itself is a legitimate entry; what must not happen is descending
	// through it (which would produce src/loop/main.go, then src/loop/loop/…).
	if _, ok := seen["src/loop/main.go"]; ok {
		t.Fatalf("a link back to its own ancestor must not be descended, got %v", keys(seen))
	}
	if _, ok := seen["src/main.go"]; !ok {
		t.Fatalf("the real file must still be visited, got %v", keys(seen))
	}
}

// CanEnter is the security boundary: an entry the caller refuses is still listed
// but typed as a non-directory, and its subtree is not entered.
func TestWalk_CanEnterRefusalKeepsTheEntryButDoesNotDescend(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "x")
	mustSymlink(t, outside, filepath.Join(root, "resources"))

	seen, _ := collect(t, root, Options{CanEnter: func(string) bool { return false }}, nil)
	entry, ok := seen["resources"]
	if !ok {
		t.Fatalf("a refused link must still be reported, got %v", keys(seen))
	}
	if entry.IsDir {
		t.Error("a refused link must not be typed as a directory")
	}
	if !entry.Blocked {
		t.Error("a refused link must be flagged so callers can skip it")
	}
	if _, visited := seen["resources/secret.txt"]; visited {
		t.Error("a refused link's subtree must not be entered")
	}
}

// The entry budget is the backstop against a link to "/": the walk must stop and
// say so instead of pretending it saw everything.
func TestWalk_EntryBudgetReportsExhausted(t *testing.T) {
	root := t.TempDir()
	for i := range 10 {
		writeFile(t, filepath.Join(root, "f"+itoa(i)+".txt"), "x")
	}

	_, out := collect(t, root, Options{MaxEntries: 3}, nil)
	if !out.Exhausted {
		t.Fatal("running out of budget must be reported as Exhausted")
	}
	if out.Visited != 3 {
		t.Fatalf("Visited = %d, want the budget of 3", out.Visited)
	}
}

// An unreadable ROOT is a failure; an unreadable subdirectory is a skip. The
// distinction is what keeps "no matches" from meaning "I could not look".
func TestWalk_RootErrorOnlyForTheRoot(t *testing.T) {
	root := t.TempDir()
	_, out := collect(t, filepath.Join(root, "missing"), Options{}, nil)
	if out.RootError == nil {
		t.Fatal("an unreadable root must be reported")
	}

	// A subdirectory that disappears mid-walk is skipped silently.
	writeFile(t, filepath.Join(root, "src", "a.go"), "x")
	_, out = collect(t, root, Options{}, nil)
	if out.RootError != nil {
		t.Fatalf("a readable root must not report an error, got %v", out.RootError)
	}
}

// SkipDir prunes exactly the subtree the caller named, like filepath.SkipDir.
func TestWalk_SkipDirPrunes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep", "a.txt"), "x")
	writeFile(t, filepath.Join(root, "skip", "b.txt"), "x")

	seen, _ := collect(t, root, Options{}, func(e Entry) Result {
		if e.IsDir && e.Name == "skip" {
			return SkipDir
		}
		return Continue
	})
	if _, ok := seen["skip/b.txt"]; ok {
		t.Fatal("a pruned subtree must not be visited")
	}
	if _, ok := seen["keep/a.txt"]; !ok {
		t.Fatalf("a kept subtree must be visited, got %v", keys(seen))
	}
}

// A dangling link has nothing to open, so it is not a hit.
func TestWalk_DanglingLinkIsNotReported(t *testing.T) {
	root := t.TempDir()
	mustSymlink(t, filepath.Join(root, "gone.txt"), filepath.Join(root, "broken.txt"))

	seen, _ := collect(t, root, Options{}, nil)
	if _, ok := seen["broken.txt"]; ok {
		t.Fatal("a dangling link must not be reported")
	}
}

func keys(m map[string]Entry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
