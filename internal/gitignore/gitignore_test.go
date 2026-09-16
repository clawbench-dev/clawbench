package gitignore

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file validates the matcher against the real `git check-ignore`, which is
// the only authority on gitignore semantics. Unit-testing our own expectations
// would just re-assert whatever we already believe; comparing against git
// catches drift in negation, anchoring, nested .gitignore and the tracked-file
// rules that a hand-written expectation set would miss.

// gitAvailable reports whether the git binary can be used. Tests that need the
// real thing skip when it is absent rather than asserting weaker behavior.
func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// gitRun runs a git command in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Keep the developer's identity and config out of the fixture: a global
	// core.excludesFile would otherwise change the expected ignore set.
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir(),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// gitIgnored returns the subset of repo-relative paths that `git check-ignore`
// reports as ignored. It queries from rootDir so relative paths resolve against
// the repository, and passes -z to survive spaces and newlines in names.
func gitIgnored(t *testing.T, rootDir string, paths []string) map[string]bool {
	t.Helper()
	if len(paths) == 0 {
		return map[string]bool{}
	}
	cmd := exec.Command("git", "check-ignore", "--stdin", "-z")
	cmd.Dir = rootDir
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir(),
	)
	var in strings.Builder
	for _, p := range paths {
		in.WriteString(p)
		in.WriteByte(0)
	}
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.Output()
	// check-ignore exits 1 when nothing matched, which is not an error here.
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			t.Fatalf("git check-ignore failed: %v", err)
		}
	}
	got := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			got[p] = true
		}
	}
	return got
}

// matcherIgnored runs our matcher over the same paths and returns the ignored
// set, expressed the same way (repo-relative slash paths).
func matcherIgnored(t *testing.T, repoRoot, dir string, paths []string) map[string]bool {
	t.Helper()
	resetCacheForTest()
	m := ForDir(dir)
	require.NotNil(t, m, "expected %s to be inside a repository", dir)

	got := map[string]bool{}
	for _, rel := range paths {
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		st, err := os.Lstat(abs)
		require.NoError(t, err, "stat %s", rel)
		isDir := st.IsDir()
		if st.Mode()&os.ModeSymlink != 0 {
			// Follow symlinks for the type, matching how the file manager
			// classifies them; a broken link counts as a file.
			if target, err := os.Stat(abs); err == nil {
				isDir = target.IsDir()
			}
		}
		if m.Ignored(abs, isDir) {
			got[rel] = true
		}
	}
	return got
}

// diffIgnored asserts our matcher agrees with git for every listed path.
func diffIgnored(t *testing.T, repoRoot, dir string, paths []string) {
	t.Helper()
	want := gitIgnored(t, repoRoot, paths)
	got := matcherIgnored(t, repoRoot, dir, paths)

	var falseNeg, falsePos []string
	for p := range want {
		if !got[p] {
			falseNeg = append(falseNeg, p)
		}
	}
	for p := range got {
		if !want[p] {
			falsePos = append(falsePos, p)
		}
	}
	sort.Strings(falseNeg)
	sort.Strings(falsePos)
	assert.Empty(t, falseNeg, "matcher missed paths git ignores (dir=%s)", dir)
	assert.Empty(t, falsePos, "matcher ignored paths git keeps (dir=%s)", dir)
}

// writeFixture creates files under root, making parent directories as needed.
func writeFixture(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
}

// listTree returns every file and directory below root as repo-relative slash
// paths, excluding .git.
func listTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		out = append(out, rel)
		return nil
	})
	require.NoError(t, err)
	sort.Strings(out)
	return out
}

// --- non-repository behavior ---

func TestForDir_NotARepository(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644))

	resetCacheForTest()
	assert.Nil(t, ForDir(dir), "a plain directory has no ignore rules")
}

func TestNilMatcher_IgnoresNothing(t *testing.T) {
	// The nil receiver is what callers hold for non-repositories, so it must be
	// safe and must report "not ignored".
	var m *Matcher
	assert.False(t, m.Ignored("/anything/at/all", false))
	assert.False(t, m.Ignored("/anything/at/all", true))
	assert.Equal(t, "", m.RepoRoot())
}

// --- differential coverage against real git ---

// TestMatchesGit_BasicPatterns covers the pattern forms that a naive matcher
// gets wrong: anchoring, directory-only, "**", escaped leading "#"/"!" and
// trailing spaces.
func TestMatchesGit_BasicPatterns(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")

	writeFixture(t, repo, map[string]string{
		".gitignore": strings.Join([]string{
			"# comment",
			"node_modules/",
			"*.log",
			"build/",
			"!build/keep.txt",
			"/root-only.txt",
			"docs/**/*.tmp",
			"web/dist/",
			"tmp*",
			"**/anywhere.md",
			"spaced\\ name.txt",
			"trailing   ",
			"\\#hash.txt",
			"\\!bang.txt",
			"*.min.js",
		}, "\n") + "\n",
		"node_modules/pkg/x.js": "x\n",
		"build/gen.js":          "g\n",
		"build/keep.txt":        "k\n",
		"docs/a/b/c.tmp":        "t\n",
		"root-only.txt":         "r\n",
		"sub/root-only.txt":     "r\n",
		"web/dist/bundle.js":    "b\n",
		"tmpdir/f.go":           "f\n",
		"sub/tmp2/g.go":         "g\n",
		"anywhere.md":           "a\n",
		"deep/anywhere.md":      "a\n",
		"spaced name.txt":       "s\n",
		"trailing":              "t\n",
		"#hash.txt":             "h\n",
		"!bang.txt":             "b\n",
		"app.min.js":            "m\n",
		"app.js":                "a\n",
	})
	// The negated file stays tracked, which is what makes the negation real.
	gitRun(t, repo, "add", "-f", "build/keep.txt", ".gitignore")

	paths := listTree(t, repo)
	diffIgnored(t, repo, repo, paths)
}

// TestMatchesGit_TrackedFilesWin covers the rule that a tracked file is never
// ignored even when a pattern matches it, and that a directory holding tracked
// files stays visible so those files remain reachable.
func TestMatchesGit_TrackedFilesWin(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")

	writeFixture(t, repo, map[string]string{
		// Both directories are excluded by pattern, but only one holds a
		// tracked file. git keeps the tracked one listed.
		".gitignore":         "withtracked/\nnotracked/\n",
		"withtracked/t.js":   "t\n",
		"withtracked/u.js":   "u\n",
		"notracked/u.js":     "u\n",
		"notracked/sub/d.js": "d\n",
	})
	// Force-add a file that the pattern excludes, so it is tracked anyway.
	gitRun(t, repo, "add", "-f", "withtracked/t.js", ".gitignore")

	paths := listTree(t, repo)
	diffIgnored(t, repo, repo, paths)
	// Also check from inside the excluded-but-tracked directory.
	diffIgnored(t, repo, filepath.Join(repo, "withtracked"), paths)
}

// TestMatchesGit_NestedGitignore covers a .gitignore in a subdirectory, whose
// patterns resolve against that subdirectory rather than the repository root.
func TestMatchesGit_NestedGitignore(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")

	writeFixture(t, repo, map[string]string{
		".gitignore":             "*.log\n",
		"sub/.gitignore":         "nested-only.txt\nlocal-dir/\n",
		"sub/nested-only.txt":    "n\n",
		"sub/other.txt":          "o\n",
		"sub/local-dir/x.js":     "x\n",
		"sub/deep/other.log":     "l\n",
		"nested-only.txt":        "root-level stays visible\n",
		"deep/local-dir/keep.md": "k\n",
	})

	paths := listTree(t, repo)
	diffIgnored(t, repo, repo, paths)
	diffIgnored(t, repo, filepath.Join(repo, "sub"), paths)
	diffIgnored(t, repo, filepath.Join(repo, "sub", "deep"), paths)
}

// TestMatchesGit_InfoExclude covers .git/info/exclude, which git honors in
// addition to .gitignore files.
func TestMatchesGit_InfoExclude(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")

	writeFixture(t, repo, map[string]string{
		".gitignore":        "*.log\n",
		".git/info/exclude": "info-excluded.txt\nexcluded-dir/\n",
		"info-excluded.txt": "i\n",
		"excluded-dir/a.js": "a\n",
		"kept.txt":          "k\n",
		"some.log":          "l\n",
	})

	paths := listTree(t, repo)
	diffIgnored(t, repo, repo, paths)
}

// TestMatchesGit_AncestorExclusion covers the rule that a path stays ignored
// while any parent directory is excluded, so a negation deeper down is inert.
// git does not list excluded directories at all, so patterns for their contents
// can never re-include anything.
func TestMatchesGit_AncestorExclusion(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")

	writeFixture(t, repo, map[string]string{
		".gitignore":        "build/\n!build/keep.txt\n!build/sub/\n",
		"build/keep.txt":    "k\n",
		"build/sub/deep.js": "d\n",
		"build/other.js":    "o\n",
		"src/app.js":        "a\n",
	})

	paths := listTree(t, repo)
	diffIgnored(t, repo, repo, paths)
	// From inside the excluded directory git still reports the children.
	diffIgnored(t, repo, filepath.Join(repo, "build"), paths)
}

// TestMatchesGit_Worktree covers a linked worktree, where .git is a file and
// .git/info/exclude lives in the shared common git dir rather than the
// per-worktree one.
func TestMatchesGit_Worktree(t *testing.T) {
	gitAvailable(t)
	main := t.TempDir()
	gitRun(t, main, "init", "-q")
	writeFixture(t, main, map[string]string{
		".gitignore":        "node_modules/\n*.log\n",
		"node_modules/x.js": "x\n",
		"src/a.go":          "package a\n",
		"b.log":             "l\n",
	})
	gitRun(t, main, "add", "-A", "-f")
	gitRun(t, main, "commit", "-qm", "init")

	// info/exclude lives in the main repo's common git dir and must still apply
	// inside the linked worktree.
	excludePath := filepath.Join(main, ".git", "info", "exclude")
	require.NoError(t, os.WriteFile(excludePath, []byte("common-excluded.txt\n"), 0o644))

	wt := filepath.Join(filepath.Dir(main), filepath.Base(main)+"-wt")
	gitRun(t, main, "worktree", "add", "-q", wt, "-b", "feature")
	t.Cleanup(func() { _ = exec.Command("git", "-C", main, "worktree", "remove", "--force", wt).Run() })

	require.NoError(t, os.WriteFile(filepath.Join(wt, "common-excluded.txt"), []byte("c\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(wt, "fresh.log"), []byte("l\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(wt, "node_modules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wt, "node_modules", "y.js"), []byte("y\n"), 0o644))

	paths := listTree(t, wt)
	diffIgnored(t, wt, wt, paths)
}

// TestMatchesGit_Symlinks covers git's lstat-based classification: a symlink is
// a FILE, so a directory-only pattern does not match it even when its target is
// an ignored directory, while a name pattern still does. Callers must therefore
// pass the lstat type rather than following the link.
func TestMatchesGit_Symlinks(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")

	writeFixture(t, repo, map[string]string{
		".gitignore":       "ignored-dir/\n*.ignored\n",
		"realdir/.keep":    "",
		"ignored-dir/x.js": "x\n",
		"realfile.txt":     "r\n",
	})
	require.NoError(t, os.Symlink("realdir", filepath.Join(repo, "link-to-dir")))
	require.NoError(t, os.Symlink("ignored-dir", filepath.Join(repo, "link-to-ignored-dir")))
	require.NoError(t, os.Symlink("realfile.txt", filepath.Join(repo, "link.ignored")))

	paths := listTree(t, repo)
	diffIgnored(t, repo, repo, paths)

	// Pin the specific outcomes so a future change to symlink handling cannot
	// silently flip them while still "agreeing" with a broken git invocation.
	resetCacheForTest()
	m := ForDir(repo)
	require.NotNil(t, m)
	assert.True(t, m.Ignored(filepath.Join(repo, "ignored-dir"), true), "ignored directory")
	assert.False(t, m.Ignored(filepath.Join(repo, "link-to-ignored-dir"), false),
		"a symlink is a file, so a directory-only pattern must not match it")
	assert.True(t, m.Ignored(filepath.Join(repo, "link.ignored"), false),
		"a name pattern still matches a symlink")
	assert.False(t, m.Ignored(filepath.Join(repo, "link-to-dir"), false))
}

// TestMatchesGit_PathOutsideRepo covers paths that are not below the repository
// root: they can never be ignored.
func TestMatchesGit_PathOutsideRepo(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	writeFixture(t, repo, map[string]string{".gitignore": "*.log\n", "a.log": "l\n"})

	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "b.log"), []byte("l\n"), 0o644))

	resetCacheForTest()
	m := ForDir(repo)
	require.NotNil(t, m)
	assert.False(t, m.Ignored(filepath.Join(outside, "b.log"), false),
		"a path outside the repository is never ignored")
}

// TestMatchesGit_EmptyRepo covers a repository with no commits and no index
// entries: pattern matching alone decides, and nothing is tracked.
func TestMatchesGit_EmptyRepo(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	writeFixture(t, repo, map[string]string{
		".gitignore": "*.log\n",
		"a.log":      "l\n",
		"kept.go":    "package k\n",
	})

	paths := listTree(t, repo)
	diffIgnored(t, repo, repo, paths)
}

// TestMatchesGit_NoGitignore covers a repository without any .gitignore: only
// .git/info/exclude (empty by default) applies, so nothing is ignored.
func TestMatchesGit_NoGitignore(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	writeFixture(t, repo, map[string]string{"a.go": "package a\n", "sub/b.js": "b\n"})

	paths := listTree(t, repo)
	diffIgnored(t, repo, repo, paths)
}

// TestMatchesGit_FromSubdirectory covers building the matcher for a directory
// deep inside the repository: the .gitignore chain must include every ancestor
// file, and paths are still compared against the repository root.
func TestMatchesGit_FromSubdirectory(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")

	writeFixture(t, repo, map[string]string{
		".gitignore":       "*.log\nroot-out.txt\n",
		"a/b/.gitignore":   "b-only.txt\n",
		"a/b/b-only.txt":   "b\n",
		"a/b/keep.txt":     "k\n",
		"a/b/deep/c.log":   "l\n",
		"a/root-out.txt":   "r\n",
		"a/b/root-out.txt": "r\n",
	})

	paths := listTree(t, repo)
	for _, sub := range []string{"a", "a/b", "a/b/deep"} {
		diffIgnored(t, repo, filepath.Join(repo, filepath.FromSlash(sub)), paths)
	}
}

// TestForDir_CacheReuse covers memoization: repeated lookups for the same
// directory must return the same matcher, and a repository change must be
// picked up once the TTL expires. The TTL is exercised by clearing the cache,
// which is the same code path a rebuild takes.
func TestForDir_CacheReuse(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	writeFixture(t, repo, map[string]string{".gitignore": "*.log\n", "a.log": "l\n"})

	resetCacheForTest()
	first := ForDir(repo)
	require.NotNil(t, first)
	assert.Same(t, first, ForDir(repo), "second lookup should reuse the cached matcher")

	// A new rule must take effect after the cache is dropped.
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("*.log\nnewly.txt\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "newly.txt"), []byte("n\n"), 0o644))
	assert.False(t, first.Ignored(filepath.Join(repo, "newly.txt"), false),
		"cached matcher keeps its snapshot")

	resetCacheForTest()
	rebuilt := ForDir(repo)
	require.NotNil(t, rebuilt)
	assert.True(t, rebuilt.Ignored(filepath.Join(repo, "newly.txt"), false),
		"rebuilt matcher sees the new rule")
}

// TestMatcher_RepoRoot covers the accessor used by callers that need to know
// which repository a listing belongs to.
func TestMatcher_RepoRoot(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "sub"), 0o755))

	resetCacheForTest()
	m := ForDir(filepath.Join(repo, "sub"))
	require.NotNil(t, m)
	// The temp dir may be a symlinked path on macOS, so compare resolved forms.
	want, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)
	got, err := filepath.EvalSymlinks(m.RepoRoot())
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestIgnored_RepoRootItselfIsNeverIgnored covers the root path, whose relative
// form is empty and must not be reported as ignored.
func TestIgnored_RepoRootItselfIsNeverIgnored(t *testing.T) {
	gitAvailable(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	writeFixture(t, repo, map[string]string{".gitignore": "*\n"})

	resetCacheForTest()
	m := ForDir(repo)
	require.NotNil(t, m)
	assert.False(t, m.Ignored(repo, true), "the repository root is never ignored")
}

// TestAncestorDirs covers the helper that marks directories holding tracked
// files, including its early exit when a parent was already recorded.
func TestAncestorDirs(t *testing.T) {
	tracked := map[string]bool{
		"a/b/c.go": true,
		"a/d.go":   true,
		"top.go":   true,
	}
	got := ancestorDirs(tracked)
	assert.True(t, got["a"], "a contains tracked files")
	assert.True(t, got["a/b"], "a/b contains tracked files")
	assert.False(t, got["b"], "b is not a prefix of a tracked path")
	assert.False(t, got["top.go"], "a file is not a directory")
	assert.False(t, got[""], "the empty path is never recorded")
}

// TestParsePatterns covers the line-level filtering: comments and blank lines
// are dropped, CRLF is tolerated, and everything else reaches the engine.
func TestParsePatterns(t *testing.T) {
	content := "# a comment\r\n\n*.log\r\n   \nbuild/\n"
	ps := parsePatterns(content, []string{"sub"})
	require.Len(t, ps, 2, "only the two real patterns survive")
}

// TestResolveGitDir covers both .git layouts: a directory (normal clone) and a
// file pointing at the real git dir (linked worktree).
func TestResolveGitDir(t *testing.T) {
	t.Run("directory layout", func(t *testing.T) {
		repo := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
		assert.Equal(t, filepath.Join(repo, ".git"), resolveGitDir(repo))
	})

	t.Run("worktree file layout", func(t *testing.T) {
		repo := t.TempDir()
		target := filepath.Join(repo, "real-git-dir")
		require.NoError(t, os.MkdirAll(target, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: real-git-dir\n"), 0o644))
		assert.Equal(t, target, resolveGitDir(repo))
	})

	t.Run("missing .git", func(t *testing.T) {
		assert.Equal(t, "", resolveGitDir(t.TempDir()))
	})

	t.Run("empty gitdir pointer", func(t *testing.T) {
		repo := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir:\n"), 0o644))
		assert.Equal(t, "", resolveGitDir(repo))
	})
}

// TestCommonGitDir covers the worktree indirection: a `commondir` file points
// at the shared git dir where info/exclude lives.
func TestCommonGitDir(t *testing.T) {
	t.Run("no commondir returns input", func(t *testing.T) {
		dir := t.TempDir()
		assert.Equal(t, dir, commonGitDir(dir))
	})

	t.Run("relative commondir is resolved", func(t *testing.T) {
		wtGit := t.TempDir()
		common := filepath.Join(filepath.Dir(wtGit), "common.git")
		require.NoError(t, os.MkdirAll(common, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(wtGit, "commondir"), []byte("../common.git\n"), 0o644))
		assert.Equal(t, filepath.Clean(common), commonGitDir(wtGit))
	})
}

// TestReadTracked covers index decoding, including the empty result for a
// repository whose index does not exist yet.
func TestReadTracked(t *testing.T) {
	t.Run("missing index", func(t *testing.T) {
		assert.Empty(t, readTracked(t.TempDir()))
	})

	t.Run("decodes tracked paths", func(t *testing.T) {
		gitAvailable(t)
		repo := t.TempDir()
		gitRun(t, repo, "init", "-q")
		writeFixture(t, repo, map[string]string{"a.go": "package a\n", "sub/b.go": "package b\n"})
		gitRun(t, repo, "add", "-A")

		got := readTracked(filepath.Join(repo, ".git"))
		assert.True(t, got["a.go"])
		assert.True(t, got["sub/b.go"])
	})
}

// TestFindRepoRoot covers upward discovery, including the "no repository" case.
func TestFindRepoRoot(t *testing.T) {
	t.Run("finds ancestor", func(t *testing.T) {
		repo := t.TempDir()
		deep := filepath.Join(repo, "a", "b", "c")
		require.NoError(t, os.MkdirAll(deep, 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
		assert.Equal(t, repo, findRepoRoot(deep))
	})

	t.Run("no repository", func(t *testing.T) {
		assert.Equal(t, "", findRepoRoot(t.TempDir()))
	})
}
