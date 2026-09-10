//nolint:noctx,gosec // git log uses exec.Command without context; projectPath (cmd.Dir) and args come from the authenticated project cookie — same trust model as git.go
package handler

import (
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Git code-change statistics for the project — added/deleted line totals per
// commit author, bucketed by commit date, within a time range.
//
// The stats come from `git log --numstat`, which reports for every commit the
// per-file added/deleted line counts relative to that commit's parent. The
// numbers therefore only cover *committed* changes — the working tree / index
// is deliberately ignored. Merge commits never carry file rows in plain
// `git log` output (no diff against the first parent is produced), so merges
// do not double count.
//
// Time-bucketing and filtering use the *committer* date (%cI) — the stable
// commit record timestamp (author date can be rewritten and is less meaningful
// for "when was this change integrated").

const gitStatsMarkerPrefix = "@@"

// gitStatsMaxRangeDays caps how wide a time window one query may cover —
// mirrors the usage-stats guard so an accidental huge window cannot trigger a
// very long git log walk.
const gitStatsMaxRangeDays = 370

// gitStatsHeaderSep separates the git log commit-header fields. Using a NUL
// byte (via %x00) keeps the header unambiguous: an author or commit message
// can never contain NUL, while file paths cannot be mistaken for a header.
const gitStatsHeaderSep = "\x00"

// gitStatsNumstatRow matches one git --numstat file row:
//
//	added	deleted	path\n     (text)
//	-	-	path\n               (binary file)
//
// With --no-renames the path is a plain file path without "old => new"
// rename notation.
var gitStatsNumstatRow = regexp.MustCompile(`^(\d+|-)\t(\d+|-)\t(.+)$`)

// gitStatsTotals is the single-row aggregate over the whole filtered range.
type gitStatsTotals struct {
	Added     int64 `json:"added"`
	Deleted   int64 `json:"deleted"`
	Net       int64 `json:"net"`
	CommitCnt int64 `json:"commitCnt"`
}

// gitStatsRow aggregates one commit author (or one day × author for trend).
type gitStatsRow struct {
	Day       string `json:"day,omitempty"` // "2006-01-02", trend only
	Author    string `json:"author"`
	Added     int64  `json:"added"`
	Deleted   int64  `json:"deleted"`
	Net       int64  `json:"net"`
	CommitCnt int64  `json:"commitCnt"`
}

// gitStatsResult is the full response of a code-change statistics query.
type gitStatsResult struct {
	IsGit  bool            `json:"isGit"`
	Totals *gitStatsTotals `json:"totals"`
	Rows   []*gitStatsRow  `json:"rows"`
	Trend  []*gitStatsRow  `json:"trend,omitempty"`
}

// gitStatsCommit is one parsed commit from git log output.
type gitStatsCommit struct {
	author  string
	date    time.Time
	added   int64
	deleted int64
}

// parseGitStatsCommitHeader parses a commit marker line produced by the
// gitStatsFormat pretty string:
//
//	@@<sha>\x00<author>\x00<committer-RFC3339>
//
// Returns ok=false when the line is not a commit header.
func parseGitStatsCommitHeader(line string) (string, string, time.Time, bool) {
	if !strings.HasPrefix(line, gitStatsMarkerPrefix) {
		return "", "", time.Time{}, false
	}
	rest := strings.TrimPrefix(line, gitStatsMarkerPrefix)
	parts := strings.SplitN(rest, gitStatsHeaderSep, 3)
	if len(parts) != 3 {
		return "", "", time.Time{}, false
	}
	date, err := time.Parse(time.RFC3339, parts[2])
	if err != nil {
		return "", "", time.Time{}, false
	}
	return parts[0], parts[1], date, true
}

// collectGitStats runs git log and aggregates per-author rows plus a per-day
// trend for the given (closed) time range. isGit reports whether projectPath
// is inside a git repository at all. Totals is always non-nil — zero-valued
// when no commits fall in the range.
func collectGitStats(projectPath string, start, end time.Time) (*gitStatsResult, error) {
	if !isGitRepo(projectPath) {
		return &gitStatsResult{IsGit: false}, nil
	}
	res := &gitStatsResult{IsGit: true, Totals: &gitStatsTotals{}, Rows: []*gitStatsRow{}}
	commits, err := gitStatsCommits(projectPath, start, end)
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return res, nil
	}

	// Author rows accumulate all commits of an author; day strings are the UTC
	// calendar day of each commit's committer date.
	byAuthor := map[string]*gitStatsRow{}
	for _, c := range commits {
		res.Totals.Added += c.added
		res.Totals.Deleted += c.deleted
		res.Totals.CommitCnt++
		row, ok := byAuthor[c.author]
		if !ok {
			row = &gitStatsRow{Author: c.author}
			byAuthor[c.author] = row
		}
		row.Added += c.added
		row.Deleted += c.deleted
		row.CommitCnt++
	}
	res.Totals.Net = res.Totals.Added - res.Totals.Deleted
	for _, r := range byAuthor {
		r.Net = r.Added - r.Deleted
		res.Rows = append(res.Rows, r)
	}
	sort.Slice(res.Rows, func(i, j int) bool {
		if res.Rows[i].Added != res.Rows[j].Added {
			return res.Rows[i].Added > res.Rows[j].Added
		}
		return res.Rows[i].Author < res.Rows[j].Author
	})

	// Day × author trend, ordered by day then author so the client can group.
	byDayAuthor := map[string]*gitStatsRow{}
	for _, c := range commits {
		day := c.date.UTC().Format("2006-01-02")
		key := day + gitStatsHeaderSep + c.author
		row, ok := byDayAuthor[key]
		if !ok {
			row = &gitStatsRow{Day: day, Author: c.author}
			byDayAuthor[key] = row
		}
		row.Added += c.added
		row.Deleted += c.deleted
		row.CommitCnt++
	}
	keys := make([]string, 0, len(byDayAuthor))
	for k := range byDayAuthor {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	res.Trend = make([]*gitStatsRow, 0, len(keys))
	for _, k := range keys {
		r := byDayAuthor[k]
		r.Net = r.Added - r.Deleted
		res.Trend = append(res.Trend, r)
	}
	return res, nil
}

// gitStatsCommits returns per-commit added/deleted lines from git log between
// start and end (both inclusive). The command output is parsed defensively:
// every numstat row is validated before being counted.
func gitStatsCommits(projectPath string, start, end time.Time) ([]gitStatsCommit, error) {
	// A single git log invocation returns every commit (marker + file rows) in
	// topological order; merge commits contribute no file rows. Binary files
	// are reported as "-	-" and contribute zero lines.
	//
	// --no-renames makes a rename count as the deletion of the old path plus
	// the addition of the new path — the usual "lines touched" convention —
	// and keeps path output free of "old => new" notation.
	args := []string{
		"log",
		"--numstat",
		"--no-renames",
		// The NUL separators are written as the literal git sequence %x00
		// (git expands it in its own output); an actual NUL byte in argv
		// would make execve fail with EINVAL.
		"--pretty=format:" + gitStatsMarkerPrefix + "%H%x00%an%x00%cI",
		"--since=" + start.UTC().Format(time.RFC3339),
		"--until=" + end.UTC().Format(time.RFC3339),
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	commits := []gitStatsCommit{}
	var cur *gitStatsCommit
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		if _, author, date, ok := parseGitStatsCommitHeader(line); ok {
			if cur != nil {
				commits = append(commits, *cur)
			}
			cur = &gitStatsCommit{author: author, date: date}
			continue
		}
		if cur == nil {
			// A file row without a preceding header cannot be attributed.
			continue
		}
		m := gitStatsNumstatRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		added, aOK := parseNumstatValue(m[1])
		deleted, dOK := parseNumstatValue(m[2])
		if !aOK || !dOK {
			// Binary file ("-") — nothing countable.
			continue
		}
		cur.added += added
		cur.deleted += deleted
	}
	if cur != nil {
		commits = append(commits, *cur)
	}
	return commits, nil
}

// parseNumstatValue parses an added/deleted cell: a non-negative integer, or
// ok=false for the binary marker "-".
func parseNumstatValue(s string) (int64, bool) {
	if s == "-" || s == "" {
		return 0, false
	}
	var n int64
	for i := range len(s) {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	return n, true
}

// ServeGitStats handles GET /api/git/stats.
// Returns added/deleted/net line counts aggregated by commit author, plus a
// per-day trend, for committed changes within a time range.
//
// Query params:
//
//	start / end   RFC3339 timestamps. Required. Commit committer date within
//	              [start,end] (inclusive on both ends).
//	trend         "1" → also return per-day rows
//
// When projectPath is not a git repository the response is
// `{"isGit": false, ...}` — the client shows a hint instead of stats.
func ServeGitStats(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()
	startStr := q.Get("start")
	endStr := q.Get("end")
	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", map[string]any{detailKey: "start"})
		return
	}
	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", map[string]any{detailKey: "end"})
		return
	}
	if !end.After(start) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", map[string]any{detailKey: "range"})
		return
	}
	// Cap the range so an accidental huge window cannot trigger a very long git
	// log walk — mirrors the usage-stats guard (maxRangeDays).
	if end.Sub(start) > gitStatsMaxRangeDays*24*time.Hour {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", map[string]any{detailKey: "range_too_long"})
		return
	}

	res, err := collectGitStats(projectPath, start, end)
	if err != nil {
		writeLocalizedError(w, r, err)
		return
	}
	if q.Get("trend") != "1" {
		res.Trend = nil
	}
	writeJSON(w, http.StatusOK, res)
}
