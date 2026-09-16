package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests cover the `ignored` flag on /api/dir and /api/dir/search. They
// drive the real handler over a real git repository, because the flag's whole
// point is agreeing with git — asserting against a hand-written expectation
// would only re-state our own assumption about gitignore semantics.

// isolateGitEnv pins HOME so neither the handler's global-excludes lookup nor
// the git CLI used for expectations can see the developer's real git config.
// Without this a personal core.excludesFile would change the expected set.
func isolateGitEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
}

// gitAdd stages paths in the repository at dir, so the tracked-file rules have
// something to act on. -f is passed by callers that stage an ignored path.
func gitAdd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"add"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add %v failed: %v\n%s", args, err, out)
	}
}

// gitConfigGlobal writes a key into the user's global git config. Callers must
// have pinned HOME first, so this cannot touch the developer's real config.
func gitConfigGlobal(t *testing.T, key, value string) {
	t.Helper()
	cmd := exec.Command("git", "config", "--global", key, value)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config --global %s failed: %v\n%s", key, err, out)
	}
}

// gitIgnoredNames returns the names git would ignore in dir, queried from that
// directory so relative paths resolve correctly.
func gitIgnoredNames(t *testing.T, dir string, names []string) map[string]bool {
	t.Helper()
	if len(names) == 0 {
		return map[string]bool{}
	}
	cmd := exec.Command("git", "check-ignore", "--stdin", "-z")
	cmd.Dir = dir
	var in strings.Builder
	for _, n := range names {
		in.WriteString(n)
		in.WriteByte(0)
	}
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			t.Fatalf("git check-ignore failed: %v", err)
		}
	}
	got := map[string]bool{}
	for _, n := range strings.Split(string(out), "\x00") {
		if n != "" {
			got[n] = true
		}
	}
	return got
}

// ignoredFlags lists dir through the real handler and returns name -> ignored.
func ignoredFlags(t *testing.T, projectDir, relPath string) map[string]bool {
	t.Helper()
	url := "/api/dir"
	if relPath != "" {
		url += "?path=" + relPath
	}
	req := newRequest(t, http.MethodGet, url, nil)
	withProjectCookie(req, projectDir)
	w := callHandler(ListDir, req)
	assertOK(t, w)

	var result struct {
		Items []struct {
			Name    string `json:"name"`
			Ignored bool   `json:"ignored"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.NotEmpty(t, result.Items)

	out := make(map[string]bool, len(result.Items))
	for _, it := range result.Items {
		out[it.Name] = it.Ignored
	}
	return out
}

// searchIgnoredFlags runs a search and returns name -> ignored for each hit. The
// limit is passed explicitly because the configured default is small.
func searchIgnoredFlags(t *testing.T, projectDir, query string, recursive bool) map[string]bool {
	t.Helper()
	url := "/api/dir/search?q=" + query + "&path=&exact=false&limit=50&recursive="
	if recursive {
		url += "true"
	} else {
		url += "false"
	}
	req := newRequest(t, http.MethodGet, url, nil)
	withProjectCookie(req, projectDir)
	w := callHandler(DirSearch, req)
	assertOK(t, w)

	out := map[string]bool{}
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var res DirSearchResult
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &res); err != nil {
			continue
		}
		out[res.Name] = res.Ignored
	}
	return out
}

// gitRepoEnv creates a test env whose project directory is a git repository.
func gitRepoEnv(t *testing.T) *testEnv {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateGitEnv(t)
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)
	initGitRepo(t, env.ProjectDir)
	return env
}

// TestListDir_IgnoredFlagMatchesGit is the core differential: for a repository
// exercising negation, anchoring, directory-only patterns and nested
// .gitignore files, every entry's flag must equal what git reports.
func TestListDir_IgnoredFlagMatchesGit(t *testing.T) {
	env := gitRepoEnv(t)

	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, ".gitignore"), []byte(strings.Join([]string{
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
		"*.min.js",
	}, "\n")+"\n"), 0o644))

	createTestFile(t, env.ProjectDir, "app.js", "a\n")
	createTestFile(t, env.ProjectDir, "app.min.js", "m\n")
	createTestFile(t, env.ProjectDir, "root-only.txt", "r\n")
	createTestFile(t, env.ProjectDir, "some.log", "l\n")
	createTestFile(t, env.ProjectDir, "anywhere.md", "a\n")
	createTestFile(t, env.ProjectDir, "spaced name.txt", "s\n")
	createTestFile(t, env.ProjectDir, "node_modules/pkg/x.js", "x\n")
	createTestFile(t, env.ProjectDir, "build/gen.js", "g\n")
	createTestFile(t, env.ProjectDir, "build/keep.txt", "k\n")
	createTestFile(t, env.ProjectDir, "docs/a/b/c.tmp", "t\n")
	createTestFile(t, env.ProjectDir, "web/dist/bundle.js", "b\n")
	createTestFile(t, env.ProjectDir, "tmpdir/f.go", "f\n")
	createTestFile(t, env.ProjectDir, "sub/root-only.txt", "r\n")
	// The negated file is force-added, which is what makes the negation real.
	gitAdd(t, env.ProjectDir, "-f", "build/keep.txt", ".gitignore")

	got := ignoredFlags(t, env.ProjectDir, "")

	names := make([]string, 0, len(got))
	for n := range got {
		names = append(names, n)
	}
	want := gitIgnoredNames(t, env.ProjectDir, names)

	for _, n := range names {
		assert.Equal(t, want[n], got[n], "entry %q ignored flag disagrees with git", n)
	}
	// Guard against a fixture that silently ignores nothing.
	assert.True(t, want["node_modules"], "fixture should ignore node_modules")
	assert.True(t, want["app.min.js"], "fixture should ignore app.min.js")
	assert.False(t, want["app.js"], "fixture should keep app.js")
	assert.False(t, want["build"], "a directory holding a tracked file stays visible")
}

// TestListDir_IgnoredFlagTrackedWins covers the rule that a tracked file is
// never dimmed, even when a pattern matches its name.
func TestListDir_IgnoredFlagTrackedWins(t *testing.T) {
	env := gitRepoEnv(t)

	// package.json is excluded by pattern yet force-added to the index.
	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, ".gitignore"), []byte("package.json\n*.log\n"), 0o644))
	createTestFile(t, env.ProjectDir, "package.json", "{}\n")
	createTestFile(t, env.ProjectDir, "untracked.json", "{}\n")
	createTestFile(t, env.ProjectDir, "a.log", "l\n")
	gitAdd(t, env.ProjectDir, "-f", "package.json", ".gitignore")

	got := ignoredFlags(t, env.ProjectDir, "")
	assert.False(t, got["package.json"], "tracked file must not be dimmed")
	assert.False(t, got["untracked.json"], "unmatched file is not ignored")
	assert.True(t, got["a.log"], "pattern-matched untracked file is ignored")
}

// TestListDir_IgnoredFlagNestedGitignore covers a .gitignore in a subdirectory:
// its patterns resolve against that subdirectory, and a listing of the subdir
// must pick them up.
func TestListDir_IgnoredFlagNestedGitignore(t *testing.T) {
	env := gitRepoEnv(t)

	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, ".gitignore"), []byte("*.log\n"), 0o644))
	createTestFile(t, env.ProjectDir, "sub/.gitignore", "local-only.txt\nlocal-dir/\n")
	createTestFile(t, env.ProjectDir, "sub/local-only.txt", "x\n")
	createTestFile(t, env.ProjectDir, "sub/keep.txt", "k\n")
	createTestFile(t, env.ProjectDir, "sub/deep/nested.log", "l\n")
	createTestFile(t, env.ProjectDir, "sub/local-dir/a.js", "a\n")
	// The same name at the repository root must stay visible: the nested rule is
	// scoped to sub/.
	createTestFile(t, env.ProjectDir, "local-only.txt", "root\n")

	rootGot := ignoredFlags(t, env.ProjectDir, "")
	assert.False(t, rootGot["local-only.txt"], "nested rule must not leak to the root")

	subGot := ignoredFlags(t, env.ProjectDir, "sub")
	assert.True(t, subGot["local-only.txt"], "nested .gitignore applies inside sub/")
	assert.True(t, subGot["local-dir"], "nested directory-only pattern applies")
	// The root pattern matches a *.log FILE, not the directory named "deep", so
	// the directory itself stays visible; its ignored file is flagged when the
	// directory is listed.
	assert.False(t, subGot["deep"], "a file pattern must not match a directory")
	assert.False(t, subGot["keep.txt"], "unmatched file stays visible")

	deepGot := ignoredFlags(t, env.ProjectDir, "sub/deep")
	assert.True(t, deepGot["nested.log"], "root pattern *.log applies inside sub/deep")
}

// TestListDir_IgnoredFlagInfoExclude covers .git/info/exclude, which git honors
// alongside .gitignore.
func TestListDir_IgnoredFlagInfoExclude(t *testing.T) {
	env := gitRepoEnv(t)

	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, ".git", "info", "exclude"),
		[]byte("info-excluded.txt\n"), 0o644))
	createTestFile(t, env.ProjectDir, "info-excluded.txt", "i\n")
	createTestFile(t, env.ProjectDir, "kept.txt", "k\n")

	got := ignoredFlags(t, env.ProjectDir, "")
	assert.True(t, got["info-excluded.txt"], ".git/info/exclude must be honored")
	assert.False(t, got["kept.txt"], "unlisted file stays visible")
}

// TestListDir_IgnoredFlagGlobalExcludesFile covers core.excludesFile from the
// user's global git config, which git applies on top of the repository's own
// ignore files.
func TestListDir_IgnoredFlagGlobalExcludesFile(t *testing.T) {
	env := gitRepoEnv(t)

	// HOME was pinned by gitRepoEnv, so this global config is the only one seen.
	globalIgnore := filepath.Join(os.Getenv("HOME"), "global-ignore")
	require.NoError(t, os.WriteFile(globalIgnore, []byte("global-excluded.txt\n"), 0o644))
	gitConfigGlobal(t, "core.excludesFile", globalIgnore)

	createTestFile(t, env.ProjectDir, "global-excluded.txt", "g\n")
	createTestFile(t, env.ProjectDir, "kept.txt", "k\n")

	got := ignoredFlags(t, env.ProjectDir, "")
	assert.True(t, got["global-excluded.txt"], "core.excludesFile must be honored")
	assert.False(t, got["kept.txt"], "unlisted file stays visible")
}

// TestListDir_NotARepository covers the fallback: without a repository nothing
// is flagged, so the file manager behaves exactly as it did before.
func TestListDir_NotARepository(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.log", "l\n")
	createTestFile(t, env.ProjectDir, "node_modules/pkg/x.js", "x\n")

	got := ignoredFlags(t, env.ProjectDir, "")
	assert.False(t, got["a.log"])
	assert.False(t, got["node_modules"])
}

// TestListDir_IgnoredFlagSymlink covers symlinks. git matches on lstat, so a
// symlink counts as a FILE: a directory-only pattern does not match it even when
// its target is an ignored directory, while a name pattern still does. The entry
// is still navigable, so the flag must not be derived from its Type.
func TestListDir_IgnoredFlagSymlink(t *testing.T) {
	env := gitRepoEnv(t)

	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, ".gitignore"),
		[]byte("ignored-dir/\n*.ignored\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "ignored-dir"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, "realdir"), 0o755))
	createTestFile(t, env.ProjectDir, "ignored-file.txt", "i\n")
	require.NoError(t, os.Symlink("realdir", filepath.Join(env.ProjectDir, "link-to-dir")))
	require.NoError(t, os.Symlink("ignored-dir", filepath.Join(env.ProjectDir, "link-to-ignored-dir")))
	require.NoError(t, os.Symlink("ignored-file.txt", filepath.Join(env.ProjectDir, "link.ignored")))

	got := ignoredFlags(t, env.ProjectDir, "")

	names := make([]string, 0, len(got))
	for n := range got {
		names = append(names, n)
	}
	want := gitIgnoredNames(t, env.ProjectDir, names)
	for _, n := range names {
		assert.Equal(t, want[n], got[n], "symlink %q flag disagrees with git", n)
	}

	assert.True(t, got["ignored-dir"], "ignored directory is flagged")
	assert.False(t, got["link-to-ignored-dir"],
		"a symlink is a file to git, so a directory-only pattern must not match it")
	assert.True(t, got["link.ignored"], "a name pattern still matches a symlink")
	assert.False(t, got["link-to-dir"], "symlink to a visible directory is not ignored")
}

// TestListDir_IgnoredFlagBuildDirKeptVisible covers the interaction that is easy
// to get wrong: a directory matched by an exclude pattern stays visible when it
// holds a tracked file, and so do the tracked files inside it.
func TestListDir_IgnoredFlagBuildDirKeptVisible(t *testing.T) {
	env := gitRepoEnv(t)

	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, ".gitignore"), []byte("build/\n"), 0o644))
	createTestFile(t, env.ProjectDir, "build/kept.go", "package k\n")
	createTestFile(t, env.ProjectDir, "build/gen.js", "g\n")
	gitAdd(t, env.ProjectDir, "-f", "build/kept.go", ".gitignore")

	rootGot := ignoredFlags(t, env.ProjectDir, "")
	assert.False(t, rootGot["build"], "directory with a tracked file stays visible")

	buildGot := ignoredFlags(t, env.ProjectDir, "build")
	assert.False(t, buildGot["kept.go"], "tracked file inside is visible")
	assert.True(t, buildGot["gen.js"], "untracked file inside is ignored")
}

// TestDirSearch_IgnoredFlag covers the search stream: hits carry the same flag.
func TestDirSearch_IgnoredFlag(t *testing.T) {
	env := gitRepoEnv(t)

	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, ".gitignore"), []byte("ignored-hit.txt\n"), 0o644))
	createTestFile(t, env.ProjectDir, "ignored-hit.txt", "i\n")
	createTestFile(t, env.ProjectDir, "kept-hit.txt", "k\n")

	byName := searchIgnoredFlags(t, env.ProjectDir, "hit", false)
	require.Contains(t, byName, "ignored-hit.txt")
	require.Contains(t, byName, "kept-hit.txt")
	assert.True(t, byName["ignored-hit.txt"], "ignored search hit is flagged")
	assert.False(t, byName["kept-hit.txt"], "kept search hit is not flagged")
}

// TestDirSearch_IgnoredFlagRecursiveDeepHit covers a recursive hit several
// levels down, whose ignore rules come from a nested .gitignore. This is the
// case where resolving the flag against the search root instead of the hit's own
// directory would give the wrong answer.
func TestDirSearch_IgnoredFlagRecursiveDeepHit(t *testing.T) {
	env := gitRepoEnv(t)

	createTestFile(t, env.ProjectDir, "a/b/.gitignore", "deep-hit.txt\n")
	createTestFile(t, env.ProjectDir, "a/b/deep-hit.txt", "d\n")
	createTestFile(t, env.ProjectDir, "deep-hit-other.txt", "o\n")

	byName := searchIgnoredFlags(t, env.ProjectDir, "deep-hit", true)
	require.Contains(t, byName, "deep-hit.txt")
	assert.True(t, byName["deep-hit.txt"], "nested .gitignore must apply to a deep recursive hit")
	assert.False(t, byName["deep-hit-other.txt"], "a hit outside the nested scope stays visible")
}
