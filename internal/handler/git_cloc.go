package handler

import (
	"net/http"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/hhatto/gocloc"
)

// Code-inventory (cloc) statistics for the current working tree, computed with
// the gocloc library. Unlike /api/git/stats (which reads committed history
// within a time range), this is a snapshot of the checked-out files right now:
// per-language code / comment / blank line counts plus file totals.
//
// It does NOT require the project to be a git repository — cloc just walks the
// directory tree. The frontend shows this block regardless of isGit.

// gitClocDefaultExclude matches directory components that are never source
// code: build artifacts, vendored deps, VCS internals, virtualenvs, tool data
// dirs and hidden dirs. The regex runs against the file's parent dir (gocloc
// ReNotMatchDir, i.e. filepath.Dir(path)), so it must match a path segment like
// "/node_modules/..." — or "\node_modules\..." on Windows, where
// filepath.Dir yields backslash separators. The separator class therefore
// accepts both / and \.
//
// This mirrors the repo-level .gitignore intent: without it a project tree
// containing e.g. .venv/ (Python env), models/ or a data dir would pull in
// hundreds of thousands of third-party lines and make the inventory look like
// it covers the whole machine rather than the project sources.
var gitClocDefaultExclude = regexp.MustCompile(`(^|[/\\])(\.git|\.hg|\.svn|\.bzr|\.cache|\.venv|venv|__pycache__|\.idea|\.vscode|\.clawbench|\.clawbench-ci|\.worktrees|\.agents|\.codebuddy|node_modules|vendor|dist|build|out|target|coverage|public|models)([/\\]|$)`)

type clocLanguageSummary struct {
	Name    string `json:"name"`
	Files   int    `json:"files"`
	Code    int64  `json:"code"`
	Comment int64  `json:"comment"`
	Blank   int64  `json:"blank"`
}

// clocResult is the full code-inventory response.
type clocResult struct {
	Languages []clocLanguageSummary `json:"languages"`
	Total     clocLanguageSummary   `json:"total"`
	ScannedAt time.Time             `json:"scannedAt"`
}

// clocCache avoids re-scanning the whole tree on every panel refresh.
// Keyed by project path; entries live a short TTL because the tree can change.
var clocCache = struct {
	sync.Mutex
	entries map[string]clocCacheEntry
}{entries: make(map[string]clocCacheEntry)}

type clocCacheEntry struct {
	res      clocResult
	loadedAt time.Time
}

const clocCacheTTL = 60 * time.Second

// collectCloc runs the gocloc scan over projectPath and returns a sorted
// summary. scan errors are returned; empty (no source files) returns an empty
// result without error.
func collectCloc(projectPath string) (clocResult, error) {
	langs := gocloc.NewDefinedLanguages()
	opts := gocloc.NewClocOptions()
	// Ignore build artifacts / vendored deps / VCS / hidden dirs at any depth,
	// plus common generated-file suffixes.
	opts.ReNotMatchDir = gitClocDefaultExclude
	opts.ReNotMatch = regexp.MustCompile(`\.(min\.js|min\.css|map)$`)

	p := gocloc.NewProcessor(langs, opts)
	res, err := p.Analyze([]string{projectPath})
	if err != nil {
		return clocResult{}, err
	}

	out := clocResult{ScannedAt: time.Now()}
	for _, l := range res.Languages {
		if l.Code == 0 && l.Comments == 0 && l.Blanks == 0 {
			continue // nothing countable (e.g. empty file)
		}
		out.Languages = append(out.Languages, clocLanguageSummary{
			Name:    l.Name,
			Files:   len(l.Files),
			Code:    int64(l.Code),
			Comment: int64(l.Comments),
			Blank:   int64(l.Blanks),
		})
	}
	sort.Slice(out.Languages, func(i, j int) bool {
		// Code lines desc; ties broken by name asc for stable output.
		if out.Languages[i].Code != out.Languages[j].Code {
			return out.Languages[i].Code > out.Languages[j].Code
		}
		return out.Languages[i].Name < out.Languages[j].Name
	})

	var total clocLanguageSummary
	for _, l := range out.Languages {
		total.Files += l.Files
		total.Code += l.Code
		total.Comment += l.Comment
		total.Blank += l.Blank
	}
	out.Total = total
	return out, nil
}

// getClocResult returns cached or freshly-scanned cloc data for projectPath.
func getClocResult(projectPath string) (clocResult, error) {
	clocCache.Lock()
	if e, ok := clocCache.entries[projectPath]; ok && time.Since(e.loadedAt) < clocCacheTTL {
		clocCache.Unlock()
		return e.res, nil
	}
	clocCache.Unlock()

	res, err := collectCloc(projectPath)
	if err != nil {
		return clocResult{}, err
	}

	// Keep the cache bounded: if over capacity, evict expired entries first.
	clocCache.Lock()
	if len(clocCache.entries) >= 32 {
		now := time.Now()
		for k, v := range clocCache.entries {
			if now.Sub(v.loadedAt) >= clocCacheTTL {
				delete(clocCache.entries, k)
			}
		}
		if len(clocCache.entries) >= 32 {
			var oldestKey string
			var oldest time.Time
			for k, v := range clocCache.entries {
				if oldestKey == "" || v.loadedAt.Before(oldest) {
					oldestKey, oldest = k, v.loadedAt
				}
			}
			if oldestKey != "" {
				delete(clocCache.entries, oldestKey)
			}
		}
	}
	clocCache.entries[projectPath] = clocCacheEntry{res: res, loadedAt: time.Now()}
	clocCache.Unlock()
	return res, nil
}

// ServeGitCloc handles GET /api/git/cloc.
// Returns per-language code-inventory of the current working tree.
//
// Query params: none. (Inventory is a point-in-time snapshot of the project
// root from the project cookie; it is independent of any time range.)
//
// Never fails on a non-git directory — cloc is a plain tree walk.
func ServeGitCloc(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	res, err := getClocResult(projectPath)
	if err != nil {
		writeLocalizedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
