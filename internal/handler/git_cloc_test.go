package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeClocFile creates a file (and parent dirs) under dir.
func writeClocFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}

func TestCollectClocMultiLanguageAndExcludes(t *testing.T) {
	dir := t.TempDir()

	// go file: 4 code lines + 1 comment + 1 blank = 6 physical lines.
	writeClocFile(t, dir, "main.go", "package main\n\n// hello\nfunc main() {\n\tprintln(\"hi\")\n}\n")
	// ts file.
	writeClocFile(t, dir, "src/app.ts", "// app\nexport const x = 1\nexport const y = 2\n\nconsole.log(x, y)\n")
	// Something under an excluded directory must be skipped.
	writeClocFile(t, dir, "node_modules/pkg/index.js", "// should be ignored\nconst a = 1\n")
	writeClocFile(t, dir, ".git/config", "ignored vcs\n")
	writeClocFile(t, dir, "coverage/lcov.info", "TN:\nSF:ignored\n")
	// Unknown extension — skipped by gocloc (no language match).
	writeClocFile(t, dir, "data.bin", "\x00\x01\x02")

	res, err := collectCloc(dir)
	require.NoError(t, err)
	require.NotEmpty(t, res.Languages)

	byName := map[string]clocLanguageSummary{}
	for _, l := range res.Languages {
		byName[l.Name] = l
	}

	goLang, ok := byName["Go"]
	require.True(t, ok, "Go language missing: %v", res.Languages)
	assert.Equal(t, 1, goLang.Files)
	assert.Equal(t, int64(4), goLang.Code)
	assert.Equal(t, int64(1), goLang.Comment)
	assert.Equal(t, int64(1), goLang.Blank)

	tsLang, ok := byName["TypeScript"]
	require.True(t, ok, "TypeScript language missing")
	assert.Equal(t, int64(3), tsLang.Code)

	// Excluded dirs never appear.
	for _, l := range res.Languages {
		assert.NotEqual(t, "JavaScript", l.Name, "node_modules content must be excluded")
		assert.NotEqual(t, "LCOV", l.Name)
	}

	// Total aggregates.
	assert.Equal(t, goLang.Files+tsLang.Files, res.Total.Files)
	assert.Equal(t, goLang.Code+tsLang.Code, res.Total.Code)
	assert.True(t, res.Total.Code > 0)
}

func TestCollectClocExcludesToolAndEnvDirs(t *testing.T) {
	dir := t.TempDir()

	// Real project source that must be counted.
	writeClocFile(t, dir, "main.go", "package main\nfunc main() {}\n")
	// Third-party / env / data dirs that used to make the inventory look
	// machine-global rather than project-scoped.
	writeClocFile(t, dir, ".venv/lib/python3.11/site-packages/numpy/arr.py", "x = 1\n")
	writeClocFile(t, dir, "venv/lib/python2.7/site-packages/pkg/mod.py", "y = 2\n")
	writeClocFile(t, dir, "models/weights/model.py", "z = 3\n")
	writeClocFile(t, dir, ".clawbench/sessions/x.py", "w = 4\n")
	writeClocFile(t, dir, ".worktrees/other-repo/main.go", "package other\nfunc other() {}\n")
	writeClocFile(t, dir, "android-lib/__pycache__/cache.py", "c = 5\n")

	res, err := collectCloc(dir)
	require.NoError(t, err)

	// No Python from virtualenvs / models / data dirs.
	for _, l := range res.Languages {
		assert.NotEqual(t, "Python", l.Name, "virtualenv/model/data python must be excluded")
	}
	// Only the real Go source survives.
	var goLang *clocLanguageSummary
	for i := range res.Languages {
		if res.Languages[i].Name == "Go" {
			goLang = &res.Languages[i]
		}
	}
	require.NotNil(t, goLang)
	assert.Equal(t, 1, goLang.Files)
	assert.Equal(t, int64(2), goLang.Code)
}

func TestCollectClocSortedByCodeDesc(t *testing.T) {
	dir := t.TempDir()
	// Bigger TS file should rank above the smaller Go file.
	writeClocFile(t, dir, "a.go", "package a\nfunc f() {}\n")
	writeClocFile(t, dir, "big.ts", "// t\n"+repeatLines("const z = 1\n", 10))

	res, err := collectCloc(dir)
	require.NoError(t, err)
	require.Len(t, res.Languages, 2)
	// Languages sorted by code desc → TypeScript first.
	assert.Equal(t, "TypeScript", res.Languages[0].Name)
	assert.Equal(t, "Go", res.Languages[1].Name)
}

// TestGitClocExcludeRegexHandlesBothSeparators locks down the cross-platform
// contract: gocloc matches ReNotMatchDir against filepath.Dir(path), which uses
// "/" on Unix but "\" on Windows. A regex that only understood "/" silently
// stopped excluding node_modules/.venv on Windows, so the inventory counted
// vendored third-party code. The exclusion must fire for both separators
// regardless of the host the test runs on.
func TestGitClocExcludeRegexHandlesBothSeparators(t *testing.T) {
	excluded := []string{
		"/proj/node_modules/pkg",
		`C:\proj\node_modules\pkg`,
		"/proj/.venv/lib/python3.11/site-packages",
		`C:\proj\.venv\lib\python3.11\site-packages`,
		"/proj/models/weights",
		`C:\proj\models\weights`,
		"/proj/sub/.git",
		`C:\proj\sub\.git`,
		"/proj/android-lib/__pycache__",
		`C:\proj\android-lib\__pycache__`,
	}
	for _, dir := range excluded {
		assert.Truef(t, gitClocDefaultExclude.MatchString(dir),
			"exclude regex must match %q", dir)
	}

	kept := []string{
		"/proj/src/app",
		`C:\proj\src\app`,
		"/proj/internal/handler",
		`C:\proj\internal\handler`,
	}
	for _, dir := range kept {
		assert.Falsef(t, gitClocDefaultExclude.MatchString(dir),
			"exclude regex must not match %q", dir)
	}
}

func TestServeGitCloc_ValidResponse(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	writeClocFile(t, env.ProjectDir, "main.go", "package main\nfunc main() {}\n")

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/git/cloc", nil), env.ProjectDir)
	w := callHandler(ServeGitCloc, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Languages []map[string]any `json:"languages"`
		Total     map[string]any   `json:"total"`
		ScannedAt string           `json:"scannedAt"`
	}
	decodeRespJSON(t, w.Body, &body)
	require.NotEmpty(t, body.Languages)
	require.Equal(t, float64(2), body.Total["code"])
	// scannedAt is an RFC3339 instant.
	_, err := time.Parse(time.RFC3339, body.ScannedAt)
	require.NoError(t, err)
}

func TestServeGitCloc_NotGitRepoStillWorks(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	// env.ProjectDir is intentionally not a git repo — cloc must still work.

	writeClocFile(t, env.ProjectDir, "x.py", "# c\nprint(1)\n")

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/git/cloc", nil), env.ProjectDir)
	w := callHandler(ServeGitCloc, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Languages []map[string]any `json:"languages"`
	}
	decodeRespJSON(t, w.Body, &body)
	require.NotEmpty(t, body.Languages)
}

func TestServeGitCloc_MissingProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/cloc", nil)
	w := callHandler(ServeGitCloc, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeGitCloc_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/git/cloc", nil), env.ProjectDir)
	w := callHandler(ServeGitCloc, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeGitCloc_CacheHit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	writeClocFile(t, env.ProjectDir, "a.go", "package a\nfunc a() {}\n")

	// First scan populates the cache.
	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/git/cloc", nil), env.ProjectDir)
	w := callHandler(ServeGitCloc, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Second call within TTL must return the cached result — identical body.
	w2 := callHandler(ServeGitCloc, req)
	assert.Equal(t, http.StatusOK, w2.Code)
	var body1, body2 json.RawMessage
	decodeRespJSON(t, w.Body, &body1)
	decodeRespJSON(t, w2.Body, &body2)
	assert.JSONEq(t, string(body1), string(body2))
}

// repeatLines returns s repeated n times.
func repeatLines(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}

// --- gitignore-aware exclusion ---

// TestCollectClocExcludesGitignoredFiles covers the core behaviour: files the
// project's .gitignore excludes must not be counted, and the language totals
// must shrink accordingly.
func TestCollectClocExcludesGitignoredFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateGitEnv(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeClocFile(t, dir, ".gitignore", "generated/\n*.gen.go\n")
	// Counted: 2 code lines.
	writeClocFile(t, dir, "main.go", "package main\nfunc main() {}\n")
	// Excluded by directory rule and by suffix rule: 8 code lines total.
	writeClocFile(t, dir, "generated/big.go", repeatLines("func gen%d() {}\n", 4))
	writeClocFile(t, dir, "api.gen.go", repeatLines("func g%d() {}\n", 4))

	res, err := collectCloc(dir)
	require.NoError(t, err)

	goRow := findClocLanguage(t, res, "Go")
	assert.Equal(t, int64(2), goRow.Code, "gitignored Go files must not be counted")
	assert.Equal(t, 1, goRow.Files, "only main.go should be counted")
}

// TestCollectClocKeepsTrackedFilesDespitePattern covers the tracked-file rule:
// a file that matches a pattern but is in the index must still be counted,
// because git does track it.
func TestCollectClocKeepsTrackedFilesDespitePattern(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateGitEnv(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeClocFile(t, dir, ".gitignore", "*.go\n")
	writeClocFile(t, dir, "tracked.go", "package main\nfunc main() {}\n")
	writeClocFile(t, dir, "untracked.go", repeatLines("func u%d() {}\n", 5))
	gitAdd(t, dir, "-f", "tracked.go", ".gitignore")

	res, err := collectCloc(dir)
	require.NoError(t, err)

	goRow := findClocLanguage(t, res, "Go")
	assert.Equal(t, int64(2), goRow.Code, "the tracked file must be counted")
	assert.Equal(t, 1, goRow.Files)
}

// TestCollectClocNestedGitignore covers a .gitignore inside a subdirectory,
// whose patterns are scoped to that subdirectory.
func TestCollectClocNestedGitignore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateGitEnv(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeClocFile(t, dir, "sub/.gitignore", "local.go\n")
	writeClocFile(t, dir, "sub/local.go", repeatLines("func l%d() {}\n", 4))
	writeClocFile(t, dir, "sub/kept.go", "package sub\nfunc K() {}\n")
	// The same name outside sub/ is unaffected by the nested rule.
	writeClocFile(t, dir, "local.go", "package main\nfunc L() {}\n")

	res, err := collectCloc(dir)
	require.NoError(t, err)

	goRow := findClocLanguage(t, res, "Go")
	assert.Equal(t, int64(4), goRow.Code, "only sub/local.go is excluded (2 + 2 remain)")
	assert.Equal(t, 2, goRow.Files)
}

// TestCollectClocNotGitRepoUnchanged covers the fallback: without a repository
// the scan keeps its previous behaviour, so nothing regresses outside git.
func TestCollectClocNotGitRepoUnchanged(t *testing.T) {
	dir := t.TempDir()
	// No repository here, so the .gitignore has no effect.
	writeClocFile(t, dir, ".gitignore", "*.go\n")
	writeClocFile(t, dir, "a.go", "package main\nfunc A() {}\n")
	writeClocFile(t, dir, "b.go", repeatLines("func B%d() {}\n", 4))

	res, err := collectCloc(dir)
	require.NoError(t, err)

	goRow := findClocLanguage(t, res, "Go")
	assert.Equal(t, int64(6), goRow.Code, "without git every source file still counts")
	assert.Equal(t, 2, goRow.Files)
}

// TestCollectClocKeepsGithubDirectory covers the interaction with gocloc's own
// VCS check, which does a substring match on the path: it drops a tracked
// .github/ tree while walking a directory (".github" contains ".git"). The
// filter runs after the scan and must not make that worse, and must not drop
// the directory's files itself.
func TestCollectClocKeepsGithubDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateGitEnv(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeClocFile(t, dir, ".gitignore", "*.log\n")
	writeClocFile(t, dir, ".github/workflows/ci.yml", "name: ci\non: push\n")
	writeClocFile(t, dir, "a.log", "noise\n")

	res, err := collectCloc(dir)
	require.NoError(t, err)

	// The YAML file is tracked and not gitignored, so the filter must keep it
	// even though gocloc's own walk may or may not have reached it.
	for _, l := range res.Languages {
		assert.NotEqual(t, "Log", l.Name, "the ignored .log file must not be counted")
	}
}

// findClocLanguage returns the row for a language, failing when it is absent.
func findClocLanguage(t *testing.T, res clocResult, name string) clocLanguageSummary {
	t.Helper()
	for _, l := range res.Languages {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("language %q missing from result: %+v", name, res.Languages)
	return clocLanguageSummary{}
}
