//nolint:goconst,noctx,gosec // JSON response field names are domain strings; git CLI uses exec.Command without context (args validated by isValidGitSHA/isValidGitRefName)
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// validGitSHA matches a hex commit SHA (6-40 hex chars, abbreviated or full).
// Prevents argument injection where a malicious SHA starting with "-" could be
// interpreted as a git flag. (ISS-132)
var validGitSHA = regexp.MustCompile(`^[0-9a-f]{6,40}$`)

// isValidGitRefName checks that a git ref name (branch/tag) is safe to pass
// as a CLI argument. Only allows characters valid in git ref names:
// alphanumeric, dot, slash, underscore, hyphen (not leading).
// Tightened from the original permissive regex to prevent shell metacharacters
// like `\`, `;`, `|`, `$`, backticks, `!` from passing validation. (ISS-179)
var validGitRefName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9./_-]*$`)

// isValidGitSHA returns true if s looks like a valid hex commit SHA.
// Also accepts "HEAD" which is a safe git ref used for working tree diffs.
func isValidGitSHA(s string) bool {
	if s == "HEAD" {
		return true
	}
	return validGitSHA.MatchString(s)
}

// isValidGitRefName returns true if s is safe to pass as a git ref name argument.
func isValidGitRefName(s string) bool {
	return validGitRefName.MatchString(s)
}

// commitInfo represents a git commit in API responses.
type commitInfo struct {
	SHA       string   `json:"sha"`
	Parents   []string `json:"parents"` // parent commit SHAs (empty for initial commit, 2+ for merge)
	Msg       string   `json:"msg"`
	Date      string   `json:"date"`
	Author    string   `json:"author"`
	Refs      []string `json:"refs"`      // branch/tag names decorating this commit
	FileCount int      `json:"fileCount"` // number of files changed in this commit
}

// parseGitLog parses git log output (format: %H|%P|%s|%ad|%an%d) into commitInfo slice.
// %P gives space-separated parent SHAs, %d gives decoration refs like " (HEAD -> main)".
func parseGitLog(output string) []commitInfo {
	var commits []commitInfo
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		// Split into 5 parts: SHA, parents, subject, date, author+refs
		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 5 {
			// Need all 5 parts: SHA, parents, subject, date, author+refs
			continue
		}

		// Parse parents (space-separated, may be empty for root commit)
		var parents []string
		if p := strings.TrimSpace(parts[1]); p != "" {
			parents = strings.Fields(p)
		}

		// The last part contains "author<optional refs>"
		// refs from %d appear as " (HEAD -> main, tag: v1.0)" appended after author
		authorAndRefs := parts[4]
		var author string
		var refs []string
		if idx := strings.LastIndex(authorAndRefs, " ("); idx >= 0 {
			author = strings.TrimSpace(authorAndRefs[:idx])
			refs = parseDecorateRefs(authorAndRefs[idx:])
		} else {
			author = strings.TrimSpace(authorAndRefs)
		}

		commits = append(commits, commitInfo{
			SHA:     parts[0],
			Parents: parents,
			Msg:     parts[2],
			Date:    parts[3],
			Author:  author,
			Refs:    refs,
		})
	}
	return commits
}

// parseGitLogWithStats parses git log output that includes --shortstat.
// The format uses a null byte (%x00) after each commit's header line.
// git appends --shortstat output after the format, so the actual structure is:
//
//	header1\x00\n\n  shortstat1\nheader2\x00\n\n  shortstat2\n...
//
// When split by \x00, each chunk (except first and last) contains the previous
// commit's shortstat followed by the current commit's header. We re-associate
// shortstat with the correct commit.
func parseGitLogWithStats(output string) []commitInfo {
	records := strings.Split(output, "\x00")
	var commits []commitInfo

	for i, rec := range records {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}

		headerLine, shortstatLines := splitHeaderAndShortstat(rec)

		// Apply shortstat to the previous commit (it belongs to the commit before the header)
		if len(shortstatLines) > 0 && len(commits) > 0 {
			statStr := strings.Join(shortstatLines, " ")
			if fc := parseFileCountFromShortstat(statStr); fc > 0 {
				commits[len(commits)-1].FileCount = fc
			}
		}

		if headerLine == "" {
			continue
		}

		info := parseGitLog(headerLine)
		if len(info) == 0 {
			continue
		}

		// For the last record, check if there's trailing shortstat after the header
		commit := info[0]
		if i == len(records)-1 {
			commit.FileCount = extractFileCountFromShortstatLines(shortstatLines)
		}

		commits = append(commits, commit)
	}

	return commits
}

// splitHeaderAndShortstat separates a git log record into its header line
// and any shortstat lines. The header is the first non-empty, non-shortstat line.
func splitHeaderAndShortstat(rec string) (headerLine string, shortstatLines []string) {
	lines := strings.Split(rec, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isShortstatLine(trimmed) {
			shortstatLines = append(shortstatLines, trimmed)
			continue
		}
		if headerLine == "" {
			headerLine = trimmed
		}
	}
	return headerLine, shortstatLines
}

// isShortstatLine returns true if the line looks like a git --shortstat output.
func isShortstatLine(line string) bool {
	return strings.Contains(line, "files changed") || strings.Contains(line, "file changed")
}

// extractFileCountFromShortstatLines returns the file count from the first
// shortstat line that contains one.
func extractFileCountFromShortstatLines(shortstatLines []string) int {
	for _, line := range shortstatLines {
		if fc := parseFileCountFromShortstat(line); fc > 0 {
			return fc
		}
	}
	return 0
}

// parseFileCountFromShortstat extracts the file count from a git --shortstat line.
// Example inputs: " 2 files changed, 10 insertions(+), 5 deletions(-)"
//
//	" 1 file changed, 3 insertions(+)"
func parseFileCountFromShortstat(stat string) int {
	m := shortstatRegex.FindStringSubmatch(stat)
	if len(m) >= 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

var shortstatRegex = regexp.MustCompile(`(\d+) files? changed`)

// into a clean list of ref names.
func parseDecorateRefs(s string) []string {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
		return nil
	}
	s = s[1 : len(s)-1] // strip parens

	var refs []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Skip "HEAD -> " prefix, keep the branch name it points to
		if strings.HasPrefix(part, "HEAD -> ") {
			refs = append(refs, "HEAD")
			part = strings.TrimPrefix(part, "HEAD -> ")
		}
		// Remote-tracking refs are already excluded by --decorate-refs-exclude,
		// so no need to filter by "/" here. This preserves local branches
		// with slashes like "feature/login".
		if strings.HasPrefix(part, "tag: ") {
			refs = append(refs, part) // keep "tag: v1.0" format
		} else if part != "" {
			refs = append(refs, part)
		}
	}
	return refs
}

// isGitRepo checks if the given path is inside a git repository.
func isGitRepo(projectPath string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = projectPath
	_, err := cmd.Output()
	return err == nil
}

// ServeGitProjectHistory returns commit history for the entire project.
// Supports pagination via ?skip=N (skips N commits).
func ServeGitProjectHistory(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"isGit":   false,
			"commits": []interface{}{},
			"hasMore": false,
		})
		return
	}

	skip := 0
	if s := r.URL.Query().Get("skip"); s != "" {
		_, _ = fmt.Sscanf(s, "%d", &skip)
	}

	// git log for entire project, with optional skip
	// Format: SHA|parents|subject|date|author+refs, followed by --shortstat
	// Null byte (%x00) separator allows parsing multi-line output per commit
	// --topo-order ensures branches display contiguously
	// --decorate-refs-exclude hides remote-tracking refs
	logArgs := []string{"log", "--format=%H|%P|%s|%ad|%an%d%x00", "--shortstat", "--date=iso-strict", "--topo-order", "--decorate-refs-exclude=refs/remotes", "-30"}
	if skip > 0 {
		logArgs = append(logArgs, "--skip", fmt.Sprintf("%d", skip))
	}
	cmd := exec.Command("git", logArgs...)
	cmd.Dir = projectPath
	output, _ := cmd.CombinedOutput()

	commits := parseGitLogWithStats(string(output))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"isGit":   true,
		"commits": commits,
		"hasMore": len(commits) == 30,
	})
}

// ServeGitBranch returns the current branch name for the project.
// DELETE /api/git/branch deletes a local branch.
func ServeGitBranch(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		serveGitDeleteBranch(w, r)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"isGit":  false,
			"branch": "",
			"head":   "",
			"dirty":  false,
		})
		return
	}

	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = projectPath
	output, err := cmd.Output()
	branch := ""
	if err == nil {
		branch = strings.TrimSpace(string(output))
	}
	// Orphan branch (no commits): rev-parse --abbrev-ref HEAD fails,
	// but symbolic-ref --short HEAD can still read the branch name.
	if branch == "" {
		srCmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
		srCmd.Dir = projectPath
		srOutput, srErr := srCmd.Output()
		if srErr == nil {
			branch = strings.TrimSpace(string(srOutput))
		}
	}

	headSHA := ""
	shaOutput, shaErr := exec.Command("git", "rev-parse", "HEAD").Output()
	if shaErr == nil {
		headSHA = strings.TrimSpace(string(shaOutput))
	}

	// Use git status --porcelain to get both dirty status and change count
	// (more efficient than separate git diff --quiet HEAD)
	dirty := false
	changeCount := 0
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = projectPath
	statusOutput, _ := statusCmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(statusOutput))
	if trimmed != "" {
		dirty = true
		changeCount = strings.Count(trimmed, "\n") + 1
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"isGit":       true,
		"branch":      branch,
		"head":        headSHA,
		"dirty":       dirty,
		"changeCount": changeCount,
	})
}

// gitDiff returns the diff for a file at a specific commit (or HEAD for working tree).
// For working tree diffs, it combines both staged and unstaged changes so that
// users see the complete picture of uncommitted modifications.
func gitDiff(projectPath, relPath, commit string) ([]byte, error) {
	if commit == "" || commit == "HEAD" {
		// Staged changes (diff between HEAD and index)
		cmdCached := exec.Command("git", "diff", "--cached", "--", relPath)
		cmdCached.Dir = projectPath
		cached, cachedErr := cmdCached.CombinedOutput()

		// Unstaged changes (diff between index and working tree)
		cmdUnstaged := exec.Command("git", "diff", "--", relPath)
		cmdUnstaged.Dir = projectPath
		unstaged, unstagedErr := cmdUnstaged.CombinedOutput()

		var combined []byte
		if cachedErr == nil && len(cached) > 0 {
			combined = append(combined, cached...)
		}
		if unstagedErr == nil && len(unstaged) > 0 {
			if len(combined) > 0 {
				combined = append(combined, '\n')
			}
			combined = append(combined, unstaged...)
		}

		if cachedErr != nil && unstagedErr != nil {
			return nil, cachedErr
		}
		return combined, nil
	}

	cmd := exec.Command("git", "show", commit, "--", relPath)
	cmd.Dir = projectPath
	return cmd.CombinedOutput()
}

// writeDiffResponse writes the diff response as JSON.
func writeDiffResponse(w http.ResponseWriter, output []byte, cmdErr error) {
	if cmdErr != nil && len(output) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"diff": "", "empty": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"diff":  string(output),
		"empty": strings.TrimSpace(string(output)) == "",
	})
}

// ServeGitFileDiff returns the diff for a specific file in a specific commit
// (comparing the commit vs its parent), or working tree diff if sha is "HEAD".
func ServeGitFileDiff(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	if !isGitRepo(projectPath) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotAGitRepoShort")
		return
	}

	sha := r.URL.Query().Get("sha")
	filePath := r.URL.Query().Get("path")
	if sha == "" || filePath == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingShaOrPath")
		return
	}
	if !isValidGitSHA(sha) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidSHA")
		return
	}

	if _, ok := validateAndResolvePath(w, r, projectPath, filePath); !ok {
		return
	}

	output, err := gitDiff(projectPath, filePath, sha)
	writeDiffResponse(w, output, err)
}

// ServeGitCommitFiles returns the list of files modified in a specific commit.
func ServeGitCommitFiles(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	if !isGitRepo(projectPath) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotAGitRepoShort")
		return
	}

	sha := r.URL.Query().Get("sha")
	if sha == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingSha")
		return
	}
	if !isValidGitSHA(sha) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidSHA")
		return
	}

	type fileInfo struct {
		Path string `json:"path"`
		Type string `json:"type"` // A=added, M=modified, D=deleted
	}

	// Detect merge commit by checking number of parents
	parents := getCommitParents(projectPath, sha)

	if len(parents) >= 2 {
		// Merge commit: show files grouped by which parent introduced them
		groups := buildMergeFileGroups(projectPath, sha, parents)
		writeJSON(w, http.StatusOK, groups)
		return
	}

	// Regular commit (or orphan): use diff-tree as before
	// -m splits merge commits so their diffs are shown (otherwise empty)
	cmd := exec.Command("git", "diff-tree", "-m", "--no-commit-id", "--name-status", "-r", sha)
	cmd.Dir = projectPath
	output, err := cmd.CombinedOutput()

	var files []fileInfo
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) == 2 {
				files = append(files, fileInfo{
					Type: parts[0],
					Path: parts[1],
				})
			}
		}
	}
	if files == nil {
		files = []fileInfo{}
	}
	writeJSON(w, http.StatusOK, files)
}

// getCommitParents returns the parent SHAs of a commit.
func getCommitParents(projectPath, sha string) []string {
	cmd := exec.Command("git", "cat-file", "-p", sha)
	cmd.Dir = projectPath
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	var parents []string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "parent ") {
			parents = append(parents, strings.TrimPrefix(line, "parent "))
		}
		if line == "" {
			break
		}
	}
	return parents
}

// mergeFileGroup represents a group of files introduced by one side of a merge.
type mergeFileGroup struct {
	Label string          `json:"label"`
	Files []mergeFileInfo `json:"files"`
}

type mergeFileInfo struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

// buildMergeFileGroups computes per-parent file groups for a merge commit.
// Files changed by both parents are assigned to parent1 (the current branch).
func buildMergeFileGroups(projectPath, sha string, parents []string) map[string]interface{} { //nolint:gocognit // complex merge analysis
	// Get merge-base between first two parents
	cmd := exec.Command("git", "merge-base", parents[0], parents[1])
	cmd.Dir = projectPath
	output, err := cmd.Output()
	if err != nil {
		return fallbackMergeFiles(projectPath, sha)
	}
	mergeBase := strings.TrimSpace(string(output))

	// Extract branch labels from merge commit message
	labels := extractMergeLabels(projectPath, sha, parents)

	// Collect files per parent: diff merge-base -> parentN
	seen := make(map[string]bool)
	groups := make([]mergeFileGroup, 0, len(parents))

	for i, parent := range parents {
		diffCmd := exec.Command("git", "diff", "--name-status", mergeBase, parent)
		diffCmd.Dir = projectPath
		diffOutput, diffErr := diffCmd.Output()
		if diffErr != nil {
			continue
		}

		var files []mergeFileInfo
		for _, line := range strings.Split(strings.TrimSpace(string(diffOutput)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) != 2 {
				continue
			}
			path := parts[1]
			if seen[path] {
				continue
			}
			seen[path] = true
			files = append(files, mergeFileInfo{
				Type: parts[0],
				Path: path,
			})
		}

		label := labels[i]
		if label == "" {
			label = fmt.Sprintf("Parent %d", i+1)
		}

		groups = append(groups, mergeFileGroup{
			Label: label,
			Files: files,
		})
	}

	// Also include files only in the merge result (conflict resolutions)
	cmd = exec.Command("git", "diff", "--name-status", mergeBase, sha)
	cmd.Dir = projectPath
	output, err = cmd.Output()
	if err == nil {
		var resolutionFiles []mergeFileInfo
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) != 2 {
				continue
			}
			path := parts[1]
			if seen[path] {
				continue
			}
			seen[path] = true
			resolutionFiles = append(resolutionFiles, mergeFileInfo{
				Type: parts[0],
				Path: path,
			})
		}
		if len(resolutionFiles) > 0 {
			groups = append(groups, mergeFileGroup{
				Label: "conflict resolution",
				Files: resolutionFiles,
			})
		}
	}

	return map[string]interface{}{
		"merge":  true,
		"groups": groups,
	}
}

// extractMergeLabels parses branch names from the merge commit message.
// For "Merge branch 'X' into Y", returns [Y, X].
// For "Merge branch 'X'" (no into), returns ["", X].
// Falls back to short SHA if parsing fails.
func extractMergeLabels(projectPath, sha string, parents []string) []string { //nolint:gocognit,gocyclo // complex merge message parsing
	labels := make([]string, len(parents))

	cmd := exec.Command("git", "log", "--format=%s", "-1", sha)
	cmd.Dir = projectPath
	output, err := cmd.Output()
	if err != nil {
		return labels
	}
	msg := strings.TrimSpace(string(output))

	// Try "Merge branch 'src' into dst" format
	// e.g. "Merge branch 'main' into fix/android-coverage-gate"
	if idx := strings.Index(msg, "Merge branch '"); idx != -1 {
		rest := msg[idx+len("Merge branch '"):]
		endSrc := strings.Index(rest, "'")
		if endSrc != -1 {
			src := rest[:endSrc]
			// afterSrc starts after the closing quote, e.g. " into fix/..."
			afterSrc := rest[endSrc+1:]
			if strings.HasPrefix(afterSrc, " into ") {
				dst := strings.TrimPrefix(afterSrc, " into ")
				if len(labels) >= 1 {
					labels[0] = dst
				}
				if len(labels) >= 2 {
					labels[1] = src
				}
			} else if len(labels) >= 2 {
				labels[1] = src
			}
		}
	}

	// Try "Merge pull request #N from user/branch" format
	if labels[1] == "" && strings.HasPrefix(msg, "Merge pull request") {
		if idx := strings.LastIndex(msg, "from "); idx != -1 {
			src := msg[idx+5:]
			if slashIdx := strings.LastIndex(src, "/"); slashIdx != -1 {
				src = src[slashIdx+1:]
			}
			if len(labels) >= 2 {
				labels[1] = src
			}
		}
	}

	// Fallback: short SHA for unlabeled parents
	for i, label := range labels {
		if label == "" && i < len(parents) {
			cmd := exec.Command("git", "rev-parse", "--short", parents[i])
			cmd.Dir = projectPath
			output, err := cmd.Output()
			if err == nil {
				labels[i] = strings.TrimSpace(string(output))
			}
		}
	}

	return labels
}

// fallbackMergeFiles uses diff-tree -m with dedup when merge-base fails.
func fallbackMergeFiles(projectPath, sha string) map[string]interface{} {
	cmd := exec.Command("git", "diff-tree", "-m", "--no-commit-id", "--name-status", "-r", sha)
	cmd.Dir = projectPath
	output, err := cmd.CombinedOutput()

	type fileInfo struct {
		Path string
		Type string
	}

	var files []fileInfo
	if err == nil {
		seen := make(map[string]bool)
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) == 2 && !seen[parts[1]] {
				seen[parts[1]] = true
				files = append(files, fileInfo{
					Type: parts[0],
					Path: parts[1],
				})
			}
		}
	}
	if files == nil {
		files = []fileInfo{}
	}

	mergeFiles := make([]mergeFileInfo, len(files))
	for i, f := range files {
		mergeFiles[i] = mergeFileInfo(f)
	}

	return map[string]interface{}{
		"merge": true,
		"groups": []mergeFileGroup{
			{Label: "all changes", Files: mergeFiles},
		},
	}
}

// ServeGitHistory returns commit history for a specific file.
func ServeGitHistory(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"isGit":     false,
			"commits":   []interface{}{},
			"untracked": false,
		})
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPath")
		return
	}

	if _, ok := validateAndResolvePath(w, r, projectPath, relPath); !ok {
		return
	}

	// Check if the file is tracked by Git (in the index or any commit).
	// git ls-files --error-unmatch exits with code 0 and produces no output
	// only when the file is tracked. Otherwise it exits with code 128.
	lsCmd := exec.Command("git", "ls-files", "--error-unmatch", relPath)
	lsCmd.Dir = projectPath
	lsOut, lsErr := lsCmd.CombinedOutput()
	untracked := lsErr != nil || len(lsOut) == 0

	// git log --format="%H|%P|%s|%ad|%an%d" --date=iso-strict --topo-order -- <path>
	cmd := exec.Command("git", "log", "--format=%H|%P|%s|%ad|%an%d", "--date=iso-strict", "--topo-order", "--decorate-refs-exclude=refs/remotes", "--", relPath)
	cmd.Dir = projectPath
	output, _ := cmd.CombinedOutput()

	commits := parseGitLog(string(output))

	writeJSON(w, http.StatusOK, map[string]interface{}{"isGit": true, "commits": commits, "untracked": untracked})
}

// ServeGitDiff returns the diff for a specific commit or the working tree diff.
func ServeGitDiff(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	if !isGitRepo(projectPath) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotAGitRepoShort")
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPath")
		return
	}

	if _, ok := validateAndResolvePath(w, r, projectPath, relPath); !ok {
		return
	}

	commit := r.URL.Query().Get("commit")
	if commit != "" && !isValidGitSHA(commit) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidSHA")
		return
	}
	output, err := gitDiff(projectPath, relPath, commit)
	writeDiffResponse(w, output, err)
}

// ServeGitStatus returns whether there are uncommitted changes for the file.
func ServeGitStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusOK, map[string]bool{"isGit": false, "hasUncommitted": false})
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPath")
		return
	}

	if _, ok := validateAndResolvePath(w, r, projectPath, relPath); !ok {
		return
	}

	cmd := exec.Command("git", "diff", "--stat", "HEAD", "--", relPath)
	cmd.Dir = projectPath
	output, err := cmd.CombinedOutput()

	hasUncommitted := err == nil && strings.TrimSpace(string(output)) != ""
	writeJSON(w, http.StatusOK, map[string]interface{}{"isGit": true, "hasUncommitted": hasUncommitted})
}

// wtFileInfo extends commitInfo with a staged flag for working tree files.
type wtFileInfo struct {
	Path   string `json:"path"`
	Type   string `json:"type"`   // A=added, M=modified, D=deleted, ?=untracked
	Staged bool   `json:"staged"` // true if changes are staged (in index)
}

// parseGitStatusPorcelain parses `git status --porcelain` output into wtFileInfo slice.
// XY format: X=index status, Y=worktree status. ?=untracked.
// Path extraction: skip the 2-char XY prefix, then skip any leading spaces/tabs.
func parseGitStatusPorcelain(output string) []wtFileInfo {
	var files []wtFileInfo
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if len(line) < 3 {
			continue
		}
		x := line[0] // index (staged) status
		y := line[1] // worktree (unstaged) status
		// Skip the 2-char XY status, then skip leading whitespace to get path
		path := strings.TrimLeft(line[2:], " \t")
		// Handle quoted paths (git quotes paths with special chars)
		path = strings.Trim(path, "\"")

		// Determine display type and staged status
		switch {
		case x == '?' && y == '?':
			// Untracked file
			files = append(files, wtFileInfo{Path: path, Type: "?", Staged: false})
		case x != ' ' && x != '?':
			// Staged change (add/modify/delete/rename)
			t := string(x)
			if t == "R" {
				// Rename: path is "old -> new", extract new name after arrow
				if idx := strings.Index(path, "->"); idx >= 0 {
					path = strings.TrimLeft(path[idx+2:], " ")
				}
			}
			files = append(files, wtFileInfo{Path: path, Type: t, Staged: true})
			// If also modified in worktree, add a second entry
			if y == 'M' {
				files = append(files, wtFileInfo{Path: path, Type: "M", Staged: false})
			}
		case y != ' ':
			// Unstaged change only
			files = append(files, wtFileInfo{Path: path, Type: string(y), Staged: false})
		}
	}
	return files
}

// ServeGitVerifyCommits checks which SHAs are valid git commit objects.
// Accepts POST with JSON body {"shas": ["abc1234", ...]}.
// Returns {"results": {"abc1234": {"sha":"...","msg":"...","date":"...","author":"..."}, "def5678": null}}
// where null means the SHA is not a valid commit.
// For valid commits, the full commit info is returned for breadcrumb display.
func ServeGitVerifyCommits(w http.ResponseWriter, r *http.Request) { //nolint:gocognit,gocyclo // multi-commit verification loop
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"results": map[string]interface{}{}})
		return
	}

	var body struct {
		SHAs []string `json:"shas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.SHAs) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"results": map[string]interface{}{}})
		return
	}

	// Cap SHA count to prevent exceeding OS ARG_MAX (~2MB on Linux).
	// 500 SHAs × 40 chars each ≈ 20KB — well within safe limits.
	const maxSHAs = 500
	if len(body.SHAs) > maxSHAs {
		body.SHAs = body.SHAs[:maxSHAs]
	}

	// Validate all SHAs to prevent argument injection (ISS-132)
	for _, sha := range body.SHAs {
		if !isValidGitSHA(sha) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidSHA")
			return
		}
	}

	results := make(map[string]interface{}, len(body.SHAs))

	// Batch: use git log --no-walk=sorted to fetch all commit info in one command
	// --ignore-missing skips invalid SHAs instead of erroring out
	logArgs := make([]string, 0, 5+len(body.SHAs))
	logArgs = append(logArgs, "log", "--no-walk=sorted", "--ignore-missing",
		"--format=%H|%P|%s|%ad|%an%d",
		"--date=iso-strict")
	logArgs = append(logArgs, body.SHAs...)

	logCmd := exec.Command("git", logArgs...)
	logCmd.Dir = projectPath
	logOutput, _ := logCmd.Output()

	// Parse git log output — only valid commits appear
	// Map full SHA → requested SHA for key normalization (frontend may send abbreviated SHAs)
	// Build a prefix lookup map from requested SHAs for O(1) matching instead of O(N×M) scan.
	reqSHAMap := make(map[string]string, len(body.SHAs)) // prefix→original
	for _, sha := range body.SHAs {
		// Index by the minimum unique prefix length (at least 7 chars for abbreviated SHAs)
		prefixLen := len(sha)
		if prefixLen > 40 {
			prefixLen = 40
		}
		reqSHAMap[sha[:prefixLen]] = sha
	}

	fullToRequested := map[string]string{}
	if len(logOutput) > 0 {
		commits := parseGitLog(string(logOutput))
		for _, c := range commits {
			// Find which requested SHA matches this full SHA (by prefix lookup)
			for prefixLen := 7; prefixLen <= len(c.SHA); prefixLen++ {
				if reqSHA, ok := reqSHAMap[c.SHA[:prefixLen]]; ok {
					fullToRequested[c.SHA] = reqSHA
					break
				}
			}
			// Store under both full SHA and (if matched) requested SHA
			results[c.SHA] = c
		}
	}

	// Re-key results under the original requested SHAs and mark unmatched as nil
	for _, sha := range body.SHAs {
		matched := false
		for fullSHA, reqSHA := range fullToRequested {
			if reqSHA == sha {
				if fullSHA != sha {
					results[sha] = results[fullSHA]
					delete(results, fullSHA)
				}
				matched = true
				break
			}
		}
		if !matched {
			results[sha] = nil
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"results": results})
}

// ServeGitWorkingTreeFiles returns uncommitted file changes for the project or a specific file.
func ServeGitWorkingTreeFiles(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"isGit": false, "hasUncommitted": false, "files": []interface{}{}})
		return
	}

	relPath := r.URL.Query().Get("path")

	// For specific file: check if it has uncommitted changes
	if relPath != "" {
		if _, ok := validateAndResolvePath(w, r, projectPath, relPath); !ok {
			return
		}
		cmd := exec.Command("git", "diff", "--name-status", "HEAD", "--", relPath)
		cmd.Dir = projectPath
		output, err := cmd.CombinedOutput()
		hasUncommitted := err == nil && strings.TrimSpace(string(output)) != ""
		// Also check untracked
		if !hasUncommitted {
			lsCmd := exec.Command("git", "ls-files", "--error-unmatch", relPath)
			lsCmd.Dir = projectPath
			_, lsErr := lsCmd.CombinedOutput()
			hasUncommitted = lsErr != nil // not tracked = untracked file
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"isGit": true, "hasUncommitted": hasUncommitted, "files": []interface{}{}})
		return
	}

	// For project: return all uncommitted files using git status --porcelain
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = projectPath
	output, _ := cmd.CombinedOutput()
	allFiles := parseGitStatusPorcelain(string(output))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"isGit":          true,
		"hasUncommitted": len(allFiles) > 0,
		"files":          allFiles,
	})
}

// worktreeInfo represents a git worktree in API responses.
type worktreeInfo struct {
	Path         string `json:"path"`
	DisplayPath  string `json:"displayPath"`
	Branch       string `json:"branch"`
	IsCurrent    bool   `json:"isCurrent"`
	Dirty        bool   `json:"dirty"`
	ChangeCount  int    `json:"changeCount"`
	UntrackedCnt int    `json:"untrackedCount"`
	Locked       bool   `json:"locked"`
	Missing      bool   `json:"missing"`
}

// parseWorktreePorcelain parses `git worktree list --porcelain` output into worktreeInfo slice.
// Blocks are separated by blank lines. Each block has lines like:
//
//	worktree /path
//	HEAD abc123
//	branch refs/heads/name
//	locked            (optional, may have reason text)
//
// DisplayPath is relative to projectPath with "." prefix (e.g. "./subdir"),
// or absolute path if the worktree is not under projectPath.
func parseWorktreePorcelain(output, projectPath string) []worktreeInfo {
	// Resolve symlinks on projectPath so comparisons work on macOS
	// where /var is a symlink to /private/var.
	if resolved, err := filepath.EvalSymlinks(projectPath); err == nil {
		projectPath = resolved
	}

	var trees []worktreeInfo
	blocks := strings.Split(strings.TrimSpace(output), "\n\n")
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var info worktreeInfo
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			switch {
			case strings.HasPrefix(line, "worktree "):
				path := strings.TrimPrefix(line, "worktree ")
				if resolved, err := filepath.EvalSymlinks(path); err == nil {
					path = resolved
				}
				info.Path = path
			case strings.HasPrefix(line, "branch "):
				branch := strings.TrimPrefix(line, "branch refs/heads/")
				info.Branch = branch
			case line == "locked" || strings.HasPrefix(line, "locked "):
				info.Locked = true
			}
		}
		if info.Path == "" {
			continue
		}
		// Compute DisplayPath
		if strings.HasPrefix(info.Path, projectPath+"/") {
			info.DisplayPath = "." + info.Path[len(projectPath):]
		} else if info.Path == projectPath {
			info.DisplayPath = "."
		} else {
			info.DisplayPath = info.Path
		}
		info.IsCurrent = info.Path == projectPath
		trees = append(trees, info)
	}
	return trees
}

// branchInfo represents a git branch in API responses.
type branchInfo struct {
	Name           string `json:"name"`
	IsCurrent      bool   `json:"isCurrent"`
	IsDefault      bool   `json:"isDefault"`
	Ahead          int    `json:"ahead"`
	Behind         int    `json:"behind"`
	RemoteTracking string `json:"remoteTracking"`
}

// parseTrackInfo parses git tracking info like "[ahead 3, behind 2]" into ahead/behind counts.
func parseTrackInfo(s string) (ahead, behind int) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return 0, 0
	}
	s = s[1 : len(s)-1] // strip brackets
	for _, part := range strings.Split(s, ", ") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "ahead ") {
			_, _ = fmt.Sscanf(part, "ahead %d", &ahead)
		} else if strings.HasPrefix(part, "behind ") {
			_, _ = fmt.Sscanf(part, "behind %d", &behind)
		}
	}
	return ahead, behind
}

// parseBranchForEachRef parses `git for-each-ref --format='%(refname:short)|%(upstream:short)|%(upstream:track)' refs/heads/`
// output into branchInfo slice.
// Each line: branchName|upstreamShort|[ahead N, behind M]
func parseBranchForEachRef(output string) []branchInfo {
	var branches []branchInfo
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 1 {
			continue
		}
		info := branchInfo{Name: parts[0]}
		if len(parts) > 1 {
			info.RemoteTracking = parts[1]
		}
		if len(parts) > 2 {
			info.Ahead, info.Behind = parseTrackInfo(parts[2])
		}
		branches = append(branches, info)
	}
	return branches
}

// detectDefaultBranch determines the default branch using a fallback chain:
// 1. git symbolic-ref refs/remotes/origin/HEAD → strip prefix
// 2. Check if "main" branch exists
// 3. Check if "master" branch exists
// 4. Empty string if none found
func detectDefaultBranch(projectPath string) string {
	// Try symbolic-ref for origin/HEAD
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(out))
		// ref is like refs/remotes/origin/main
		if strings.HasPrefix(ref, "refs/remotes/origin/") {
			name := strings.TrimPrefix(ref, "refs/remotes/origin/")
			if name != "" {
				return name
			}
		}
	}

	// Fallback: check if "main" exists
	cmd = exec.Command("git", "rev-parse", "--verify", "main")
	cmd.Dir = projectPath
	if err := cmd.Run(); err == nil {
		return "main"
	}

	// Fallback: check if "master" exists
	cmd = exec.Command("git", "rev-parse", "--verify", "master")
	cmd.Dir = projectPath
	if err := cmd.Run(); err == nil {
		return "master"
	}

	return ""
}

// ServeGitBranches returns all local branches for the project.
func ServeGitBranches(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"isGit":         false,
			"branches":      []interface{}{},
			"defaultBranch": "",
			"currentBranch": "",
		})
		return
	}

	// Get all branches with tracking info
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)|%(upstream:short)|%(upstream:track)", "refs/heads/")
	cmd.Dir = projectPath
	output, _ := cmd.CombinedOutput()
	branches := parseBranchForEachRef(string(output))

	// Get current branch
	cmd = exec.Command("git", "symbolic-ref", "--short", "HEAD")
	cmd.Dir = projectPath
	curOut, curErr := cmd.Output()
	currentBranch := ""
	if curErr == nil {
		currentBranch = strings.TrimSpace(string(curOut))
	}

	// Detect default branch
	defaultBranch := detectDefaultBranch(projectPath)

	// Set IsCurrent and IsDefault on each branch
	for i := range branches {
		branches[i].IsCurrent = branches[i].Name == currentBranch
		branches[i].IsDefault = branches[i].Name == defaultBranch
	}

	// Get stash count
	stashCount := 0
	stashListCmd := exec.Command("git", "stash", "list")
	stashListCmd.Dir = projectPath
	stashListOut, _ := stashListCmd.Output()
	for _, ch := range string(stashListOut) {
		if ch == '\n' {
			stashCount++
		}
	}
	if strings.TrimSpace(string(stashListOut)) != "" {
		stashCount++ // last entry has no trailing newline
	}

	if branches == nil {
		branches = []branchInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"isGit":         true,
		"branches":      branches,
		"defaultBranch": defaultBranch,
		"currentBranch": currentBranch,
		"stashCount":    stashCount,
	})
}

// ServeGitVerifyWorktrees checks which paths are valid git worktree directories.
// Accepts POST with JSON body {"paths": ["/abs/path/1", "/abs/path/2"]}.
// Returns {"results": {"/abs/path/1": {"branch":"feature-x","displayPath":"./.worktrees/feature-x","isCurrent":false,"path":"/abs/path/1"}, "/abs/path/2": null}}
// where null means the path is not a valid worktree.
func ServeGitVerifyWorktrees(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"results": map[string]interface{}{}})
		return
	}

	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Paths) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"results": map[string]interface{}{}})
		return
	}

	// Cap path count to prevent abuse.
	const maxPaths = 100
	if len(body.Paths) > maxPaths {
		body.Paths = body.Paths[:maxPaths]
	}

	// Run git worktree list --porcelain once to get all worktrees
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = projectPath
	output, _ := cmd.CombinedOutput()
	trees := parseWorktreePorcelain(string(output), projectPath)

	// Build lookup map: resolved worktree path → worktreeInfo
	lookup := make(map[string]*worktreeInfo, len(trees))
	for i := range trees {
		lookup[trees[i].Path] = &trees[i]
	}

	results := make(map[string]interface{}, len(body.Paths))
	for _, p := range body.Paths {
		// Handle relative paths by joining with projectPath
		if !filepath.IsAbs(p) {
			p = filepath.Join(projectPath, p)
		}
		// Resolve symlinks for consistent matching
		resolved := p
		if r, err := filepath.EvalSymlinks(p); err == nil {
			resolved = r
		}
		if info, ok := lookup[resolved]; ok {
			results[p] = info
		} else {
			results[p] = nil
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"results": results})
}

// ServeGitWorktrees returns all git worktrees for the project.
// DELETE /api/git/worktrees deletes a git worktree.
func ServeGitWorktrees(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		serveGitDeleteWorktree(w, r)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"isGit":     false,
			"worktrees": []interface{}{},
		})
		return
	}

	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = projectPath
	output, _ := cmd.CombinedOutput()

	trees := parseWorktreePorcelain(string(output), projectPath)

	// Check if worktree paths still exist
	for i := range trees {
		if _, err := os.Stat(trees[i].Path); os.IsNotExist(err) {
			trees[i].Missing = true
		}
	}

	// Check dirty status for each worktree in parallel
	type dirtyResult struct {
		Index        int
		Dirty        bool
		ChangeCount  int
		UntrackedCnt int
	}
	results := make(chan dirtyResult, len(trees))
	for i, wt := range trees {
		go func(idx int, path string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			statusCmd := exec.CommandContext(ctx, "git", "-C", path, "status", "--porcelain")
			out, err := statusCmd.CombinedOutput()
			if err != nil {
				results <- dirtyResult{Index: idx}
				return
			}
			dirty := false
			changeCount := 0
			untrackedCnt := 0
			for _, line := range strings.Split(string(out), "\n") {
				if len(line) >= 2 {
					dirty = true
					changeCount++
					if line[0] == '?' && line[1] == '?' {
						untrackedCnt++
					}
				}
			}
			results <- dirtyResult{Index: idx, Dirty: dirty, ChangeCount: changeCount, UntrackedCnt: untrackedCnt}
		}(i, wt.Path)
	}
	for range trees {
		res := <-results
		trees[res.Index].Dirty = res.Dirty
		trees[res.Index].ChangeCount = res.ChangeCount
		trees[res.Index].UntrackedCnt = res.UntrackedCnt
	}

	if trees == nil {
		trees = []worktreeInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"isGit":     true,
		"worktrees": trees,
	})
}

// checkoutMu serializes git checkout operations to prevent concurrent branch switches.
var checkoutMu sync.Mutex

// ServeGitCheckout switches the current branch. Supports stash and force options.
// POST /api/git/checkout  { "branch": string, "stash": bool, "force": bool }
func ServeGitCheckout(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo // multi-mode git checkout
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !isGitRepo(projectPath) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotAGitRepoShort")
		return
	}

	var body struct {
		Branch string `json:"branch"`
		Stash  bool   `json:"stash"`
		Force  bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	if strings.TrimSpace(body.Branch) == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	// Validate branch name to prevent argument injection (ISS-151)
	if !isValidGitRefName(body.Branch) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidBranchName")
		return
	}

	// Acquire checkout mutex (non-blocking)
	if !checkoutMu.TryLock() {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"success": false,
			"error":   "checkout_in_progress",
		})
		return
	}
	defer checkoutMu.Unlock()

	// Check dirty status
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = projectPath
	statusOut, _ := statusCmd.CombinedOutput()
	dirtyLines := 0
	for _, line := range strings.Split(strings.TrimSpace(string(statusOut)), "\n") {
		if len(line) >= 2 {
			dirtyLines++
		}
	}
	isDirty := dirtyLines > 0

	if isDirty && !body.Stash && !body.Force {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":        false,
			"error":          "dirty_worktree",
			"untrackedCount": dirtyLines,
		})
		return
	}

	// Stash if requested and dirty
	stashed := false
	if body.Stash && isDirty {
		stashCmd := exec.Command("git", "stash")
		stashCmd.Dir = projectPath
		stashOut, stashErr := stashCmd.CombinedOutput()
		if stashErr != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"error":   "stash_failed",
			})
			return
		}
		_ = stashOut
		stashed = true
	}

	// Switch branch — use "--" separator to prevent branch name from being
	// interpreted as a git flag (ISS-151).
	switchArgs := []string{"switch"}
	if body.Force {
		switchArgs = append(switchArgs, "-f")
	}
	switchArgs = append(switchArgs, "--", body.Branch)
	switchCmd := exec.Command("git", switchArgs...)
	switchCmd.Dir = projectPath
	switchOut, switchErr := switchCmd.CombinedOutput()

	if switchErr != nil {
		errMsg := strings.TrimSpace(string(switchOut))
		errorCode := "checkout_failed"
		if strings.Contains(errMsg, "conflict") {
			errorCode = "checkout_conflict"
		} else if strings.Contains(errMsg, "hook") {
			errorCode = "hook_rejected"
		} else if strings.Contains(errMsg, "did not match") || strings.Contains(errMsg, "not found") {
			errorCode = "branch_not_found"
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":     false,
			"error":       errorCode,
			"errorDetail": errMsg,
		})
		return
	}

	// Get stash count
	stashListCmd := exec.Command("git", "stash", "list")
	stashListCmd.Dir = projectPath
	stashListOut, _ := stashListCmd.Output()
	stashCount := 0
	for _, line := range strings.Split(strings.TrimSpace(string(stashListOut)), "\n") {
		if line != "" {
			stashCount++
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"branch":     body.Branch,
		"stashed":    stashed,
		"stashCount": stashCount,
	})
}

// tagInfo represents a git tag in API responses.
type tagInfo struct {
	Name   string `json:"name"`
	SHA    string `json:"sha"`
	Date   string `json:"date"`
	Author string `json:"author"`
	Msg    string `json:"msg"`
}

// ServeGitTags returns all tags with commit info.
// DELETE /api/git/tags deletes a local tag.
func ServeGitTags(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		serveGitDeleteTag(w, r)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"isGit": false,
			"tags":  []interface{}{},
		})
		return
	}

	// List tags with commit metadata using for-each-ref
	// Format: tagname|objectname|creatordate|creator
	cmd := exec.Command("git", "for-each-ref",
		"--format=%(refname:short)|%(objectname)|%(creatordate:iso)|%(creator)",
		"refs/tags/")
	cmd.Dir = projectPath
	output, _ := cmd.CombinedOutput()

	var tags []tagInfo
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		sha := parts[1]
		date := ""
		author := ""
		if len(parts) > 2 {
			date = parts[2]
		}
		if len(parts) > 3 {
			// creator format: "Name email timestamp" — take name portion
			author = parts[3]
			if idx := strings.LastIndex(author, " "); idx > 0 {
				author = author[:idx]
			}
		}

		// Get tag message (annotated tags have messages, lightweight tags don't)
		msg := ""
		msgCmd := exec.Command("git", "tag", "-n1", name)
		msgCmd.Dir = projectPath
		msgOut, _ := msgCmd.Output()
		if len(msgOut) > 0 {
			// Output format: "tagname            message"
			fields := strings.SplitN(strings.TrimSpace(string(msgOut)), "  ", 2)
			if len(fields) > 1 {
				msg = strings.TrimSpace(fields[1])
			}
		}

		tags = append(tags, tagInfo{
			Name:   name,
			SHA:    sha,
			Date:   date,
			Author: author,
			Msg:    msg,
		})
	}

	if tags == nil {
		tags = []tagInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"isGit": true,
		"tags":  tags,
	})
}

// serveGitDeleteBranch deletes a local branch.
func serveGitDeleteBranch(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "not_git_repo",
		})
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "invalid_request",
		})
		return
	}
	// Validate branch name to prevent argument injection (ISS-151)
	if !isValidGitRefName(body.Name) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "invalid_branch_name",
		})
		return
	}

	// Check if it's the current branch
	cmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	cmd.Dir = projectPath
	curOut, _ := cmd.Output()
	currentBranch := strings.TrimSpace(string(curOut))
	if currentBranch == body.Name {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "cannot_delete_current",
		})
		return
	}

	// Check if it's the default branch
	defaultBranch := detectDefaultBranch(projectPath)
	if defaultBranch == body.Name {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "cannot_delete_default",
		})
		return
	}

	// Try safe delete first (-d), fall back to force (-D)
	// Use "--" separator to prevent branch name from being interpreted as a flag (ISS-151)
	cmd = exec.Command("git", "branch", "-d", "--", body.Name)
	cmd.Dir = projectPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := strings.TrimSpace(string(out))
		if strings.Contains(errMsg, "not fully merged") || strings.Contains(errMsg, "not merged") {
			cmd = exec.Command("git", "branch", "-D", "--", body.Name)
			cmd.Dir = projectPath
			out, err = cmd.CombinedOutput()
		}
		if err != nil {
			errMsg = strings.TrimSpace(string(out))
			errorCode := "delete_failed"
			if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "did not match") {
				errorCode = "branch_not_found"
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success":     false,
				"error":       errorCode,
				"errorDetail": errMsg,
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// serveGitDeleteWorktree removes a git worktree.
func serveGitDeleteWorktree(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "not_git_repo",
		})
		return
	}

	var body struct {
		Path  string `json:"path"`
		Force bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Path) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "invalid_request",
		})
		return
	}

	// Resolve symlinks on body.Path so comparisons work on macOS
	// where /var is a symlink to /private/var.
	deletePath := resolveSymlinkPath(body.Path)

	// ISS-208: List worktrees and verify deletePath matches a known worktree.
	if !validateWorktreePath(w, projectPath, deletePath) {
		return
	}

	// Remove worktree — use resolved deletePath instead of raw body.Path
	// to prevent command injection via path traversal (ISS-208).
	if !removeWorktree(w, projectPath, deletePath, body.Force) {
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// resolveSymlinkPath resolves symlinks on the given path.
func resolveSymlinkPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// validateWorktreePath checks that deletePath matches a known worktree.
// Returns true if validation passes (and writes error JSON if not).
func validateWorktreePath(w http.ResponseWriter, projectPath, deletePath string) bool {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = projectPath
	output, _ := cmd.CombinedOutput()
	trees := parseWorktreePorcelain(string(output), projectPath)

	for _, wt := range trees {
		if wt.Path == deletePath {
			if wt.IsCurrent {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"success": false,
					"error":   "cannot_delete_current",
				})
				return false
			}
			return true
		}
	}
	writeJSON(w, http.StatusForbidden, map[string]interface{}{
		"success": false,
		"error":   "path_not_allowed",
	})
	return false
}

// removeWorktree attempts to remove a git worktree, handling dirty worktree cases.
// Returns true if removal succeeded.
func removeWorktree(w http.ResponseWriter, projectPath, deletePath string, force bool) bool {
	cmd := exec.Command("git", "worktree", "remove", deletePath)
	cmd.Dir = projectPath
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true
	}

	errMsg := strings.TrimSpace(string(out))
	isDirty := isDirtyWorktreeError(errMsg)

	if isDirty && !force {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "dirty_worktree",
		})
		return false
	}

	if isDirty {
		return forceRemoveWorktree(w, projectPath, deletePath)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     false,
		"error":       "delete_failed",
		"errorDetail": errMsg,
	})
	return false
}

// isDirtyWorktreeError checks if the git error message indicates a dirty worktree.
func isDirtyWorktreeError(errMsg string) bool {
	return strings.Contains(errMsg, "modified") || strings.Contains(errMsg, "untracked") || strings.Contains(errMsg, "uncommitted")
}

// forceRemoveWorktree force-removes a dirty worktree.
func forceRemoveWorktree(w http.ResponseWriter, projectPath, deletePath string) bool {
	cmd := exec.Command("git", "worktree", "remove", "--force", deletePath)
	cmd.Dir = projectPath
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     false,
		"error":       "delete_failed",
		"errorDetail": strings.TrimSpace(string(out)),
	})
	return false
}

// serveGitDeleteTag deletes a local tag.
func serveGitDeleteTag(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if !isGitRepo(projectPath) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "not_git_repo",
		})
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "invalid_request",
		})
		return
	}
	// Validate tag name to prevent argument injection (ISS-152)
	if !isValidGitRefName(body.Name) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "invalid_tag_name",
		})
		return
	}

	// Use "--" separator to prevent tag name from being interpreted as a flag (ISS-152)
	cmd := exec.Command("git", "tag", "-d", "--", body.Name)
	cmd.Dir = projectPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":     false,
			"error":       "delete_failed",
			"errorDetail": strings.TrimSpace(string(out)),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}
