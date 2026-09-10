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

// gitCommitDated creates a commit at a fixed committer/author date.
func gitCommitDated(t *testing.T, dir, msg, date string) {
	t.Helper()
	env := "GIT_AUTHOR_DATE=" + date
	envC := "GIT_COMMITTER_DATE=" + date
	cmd := exec.Command("git", "commit", "-q", "-m", msg)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env, envC)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit dated %q failed: %v\n%s", msg, err, out)
	}
}

// --- Unit tests for the numstat parser ---

func TestParseGitStatsCommitHeader(t *testing.T) {
	sha, author, date, ok := parseGitStatsCommitHeader("@@abc123\x00Jane Doe\x002026-06-01T10:30:00+08:00")
	require.True(t, ok)
	assert.Equal(t, "abc123", sha)
	assert.Equal(t, "Jane Doe", author)
	assert.Equal(t, time.Date(2026, 6, 1, 2, 30, 0, 0, time.UTC), date.UTC())

	_, _, _, ok = parseGitStatsCommitHeader("plain line")
	assert.False(t, ok)
	_, _, _, ok = parseGitStatsCommitHeader("@@short")
	assert.False(t, ok)
	_, _, _, ok = parseGitStatsCommitHeader("@@sha\x00author\x00not-a-date")
	assert.False(t, ok)
}

func TestParseNumstatValue(t *testing.T) {
	v, ok := parseNumstatValue("0")
	require.True(t, ok)
	assert.Equal(t, int64(0), v)

	v, ok = parseNumstatValue("1234")
	require.True(t, ok)
	assert.Equal(t, int64(1234), v)

	_, ok = parseNumstatValue("-")
	assert.False(t, ok)
	_, ok = parseNumstatValue("12x")
	assert.False(t, ok)
	_, ok = parseNumstatValue("")
	assert.False(t, ok)
}

// --- Integration: parser against a real repo ---

// buildGitStatsRepo creates a repo with several dated commits and returns its
// path plus the fixed test start/end window.
func buildGitStatsRepo(t *testing.T) (string, time.Time, time.Time) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")

	// Commit 1: author A — 3 added lines (incl. the trailing newline of the
	// printf) at 2026-01-10T00:00:00Z.
	writeAndStage := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		run("add", rel)
	}
	writeAndStage("a.txt", "l1\nl2\nl3\n")
	gitCommitDated(t, dir, "c1", "2026-01-10T00:00:00Z")

	// Commit 2: author A — modify a.txt (1 added, 1 deleted); author stays A.
	writeAndStage("a.txt", "l1\nl3\nl4\n")
	gitCommitDated(t, dir, "c2", "2026-01-15T00:00:00Z")

	// Commit 3: author B — new file b.txt, 2 added lines.
	run("config", "user.name", "Bob")
	writeAndStage("b.txt", "x\nxx\n")
	gitCommitDated(t, dir, "c3", "2026-02-01T00:00:00Z")

	// Commit 4 (rename) — outside the window we care about but useful to keep
	// the repo realistic.
	run("mv", "b.txt", "b2.txt")
	gitCommitDated(t, dir, "c4 rename", "2026-03-01T00:00:00Z")

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	return dir, start, end
}

func TestGitStatsCommitsParsesRealRepo(t *testing.T) {
	dir, start, end := buildGitStatsRepo(t)

	commits, err := gitStatsCommits(dir, start, end)
	require.NoError(t, err)
	require.Len(t, commits, 3, "window [01-01..02-15] must include c1/c2/c3, exclude the March rename commit")

	// c1: 3 added, 0 deleted — author A.
	assert.Equal(t, int64(3), commits[2].added)
	assert.Equal(t, int64(0), commits[2].deleted)
	// c2: 1 added, 1 deleted.
	assert.Equal(t, int64(1), commits[1].added)
	assert.Equal(t, int64(1), commits[1].deleted)
	// c3: 2 added, 0 deleted — author Bob.
	assert.Equal(t, int64(2), commits[0].added)
	assert.Equal(t, "Bob", commits[0].author)
}

func TestCollectGitStatsTotalsAndTrend(t *testing.T) {
	dir, start, end := buildGitStatsRepo(t)

	res, err := collectGitStats(dir, start, end)
	require.NoError(t, err)
	require.True(t, res.IsGit)
	require.NotNil(t, res.Totals)

	// Totals: 3 + 1 + 2 added, 1 deleted → net 5.
	assert.Equal(t, int64(6), res.Totals.Added)
	assert.Equal(t, int64(1), res.Totals.Deleted)
	assert.Equal(t, int64(5), res.Totals.Net)
	assert.Equal(t, int64(3), res.Totals.CommitCnt)

	// Rows: two authors, ordered by added desc → A(4) then Bob(2).
	require.Len(t, res.Rows, 2)
	assert.Equal(t, "Test", res.Rows[0].Author)
	assert.Equal(t, int64(4), res.Rows[0].Added)
	assert.Equal(t, int64(1), res.Rows[0].Deleted)
	assert.Equal(t, int64(3), res.Rows[0].Net)
	assert.Equal(t, int64(2), res.Rows[0].CommitCnt)
	assert.Equal(t, "Bob", res.Rows[1].Author)
	assert.Equal(t, int64(2), res.Rows[1].Added)

	// Trend: c1/c2 on 2026-01-10 & 2026-01-15 (UTC day buckets), c3 on
	// 2026-02-01. Ordered by day then author.
	require.Len(t, res.Trend, 3)
	assert.Equal(t, "2026-01-10", res.Trend[0].Day)
	assert.Equal(t, int64(3), res.Trend[0].Added)
	assert.Equal(t, "2026-01-15", res.Trend[1].Day)
	assert.Equal(t, "2026-02-01", res.Trend[2].Day)
	assert.Equal(t, "Bob", res.Trend[2].Author)
}

func TestCollectGitStatsOutsideRange(t *testing.T) {
	dir, _, _ := buildGitStatsRepo(t)

	// A window entirely before the first commit → zero totals, no rows.
	res, err := collectGitStats(dir,
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 12, 31, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.True(t, res.IsGit)
	require.Equal(t, int64(0), res.Totals.CommitCnt)
	require.Empty(t, res.Rows)
	require.Empty(t, res.Trend)
}

func TestCollectGitStatsNotARepo(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	res, err := collectGitStats(dir, start, end)
	require.NoError(t, err)
	assert.False(t, res.IsGit)
	assert.Nil(t, res.Totals)
	assert.Nil(t, res.Rows)
}

// --- HTTP handler ---

func TestServeGitStats_ValidResponse(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)
	// Two extra commits inside the window.
	createTestFile(t, env.ProjectDir, "one.go", "package x\n")
	gitCommitAll(t, env.ProjectDir, "add one.go")
	createTestFile(t, env.ProjectDir, "two.go", "package y\n// c\n")
	gitCommitAll(t, env.ProjectDir, "add two.go")

	// Wide-but-legal window (≤370d) centered around "now" so all commits made
	// by initGitRepo + gitCommitAll fall inside.
	req := withProjectCookie(newRequest(t, http.MethodGet,
		"/api/git/stats?start=2026-08-01T00:00:00Z&end=2026-09-30T00:00:00Z&trend=1", nil), env.ProjectDir)
	w := callHandler(ServeGitStats, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		IsGit  bool `json:"isGit"`
		Totals struct {
			Added     int64 `json:"added"`
			Deleted   int64 `json:"deleted"`
			Net       int64 `json:"net"`
			CommitCnt int64 `json:"commitCnt"`
		} `json:"totals"`
		Rows  []map[string]any `json:"rows"`
		Trend []map[string]any `json:"trend"`
	}
	decodeRespJSON(t, w.Body, &body)
	require.True(t, body.IsGit)
	require.Greater(t, body.Totals.CommitCnt, int64(0), "init + 2 commits must be counted")
	assert.Equal(t, body.Totals.Added, body.Totals.Net+body.Totals.Deleted)
	require.NotEmpty(t, body.Rows)
	require.NotEmpty(t, body.Trend)
}

func TestServeGitStats_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	// env.ProjectDir exists but is not a git repo.

	req := withProjectCookie(newRequest(t, http.MethodGet,
		"/api/git/stats?start=2026-01-01T00:00:00Z&end=2026-02-01T00:00:00Z", nil), env.ProjectDir)
	w := callHandler(ServeGitStats, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		IsGit  bool             `json:"isGit"`
		Totals *json.RawMessage `json:"totals"`
		Rows   []map[string]any `json:"rows"`
		Trend  []map[string]any `json:"trend"`
	}
	decodeRespJSON(t, w.Body, &body)
	assert.False(t, body.IsGit)
	// Not-a-repo responses carry no totals/rows/trend content.
	assert.Nil(t, body.Totals)
	assert.Nil(t, body.Rows)
	assert.Nil(t, body.Trend)
}

func TestServeGitStats_ValidationErrors(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	projectPath := env.ProjectDir

	cases := []struct {
		name   string
		params string
	}{
		{"missing start", "end=2026-02-01T00:00:00Z"},
		{"missing end", "start=2026-01-01T00:00:00Z"},
		{"bad start", "start=garbage&end=2026-02-01T00:00:00Z"},
		{"end before start", "start=2026-02-01T00:00:00Z&end=2026-01-01T00:00:00Z"},
		{"range too long", "start=2020-01-01T00:00:00Z&end=2030-01-01T00:00:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := withProjectCookie(newRequest(t, http.MethodGet, "/api/git/stats?"+tc.params, nil), projectPath)
			w := callHandler(ServeGitStats, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestServeGitStats_MissingProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/stats?start=2026-01-01T00:00:00Z&end=2026-02-01T00:00:00Z", nil)
	w := callHandler(ServeGitStats, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeGitStats_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/git/stats?start=2026-01-01T00:00:00Z&end=2026-02-01T00:00:00Z", nil), env.ProjectDir)
	w := callHandler(ServeGitStats, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
