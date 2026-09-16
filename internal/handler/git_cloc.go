package handler

import (
	"net/http"
	"regexp"
	"sort"
	"sync"
	"time"

	"clawbench/internal/gitignore"

	"github.com/hhatto/gocloc"
)

// Code-inventory (cloc) statistics for the current working tree, computed with
// the gocloc library. Unlike /api/git/stats (which reads committed history
// within a time range), this is a snapshot of the checked-out files right now:
// per-language code / comment / blank line counts plus file totals.
//
// It does NOT require the project to be a git repository — cloc just walks the
// directory tree. The frontend shows this block regardless of isGit.
//
// Exclusion is two-layered and intersected: the name-based regex below always
// applies, and in a git repository the project's own .gitignore rules are
// applied on top (see collectCloc). The regex is kept as the fallback for
// projects whose .gitignore is missing or incomplete, so a tree holding a Python
// virtualenv or a model directory cannot inflate the inventory.

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
//
// Two exclusion layers apply, intersected:
//
//  1. gitClocDefaultExclude / the generated-file suffix regex, which prune the
//     tree gocloc walks. These are name-based heuristics and stay as the
//     fallback for projects whose .gitignore is missing or incomplete.
//  2. The project's own gitignore rules. gocloc cannot consult them, so the
//     per-file results are filtered afterwards and re-aggregated.
//
// Filtering after the scan (rather than feeding gocloc an explicit file list)
// keeps the subtraction exact: the result is precisely "what gocloc counted,
// minus the files git would not track". Passing an explicit list instead would
// change unrelated behaviour, because gocloc's VCS check does a substring match
// on the path — it drops a tracked .github/ tree when walking a directory but
// keeps it for an explicit list.
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

	// nil for a non-repository, in which case nothing extra is excluded.
	ign := gitignore.ForDir(projectPath)

	// Re-aggregate the per-file results, dropping gitignored files. Aggregating
	// here rather than reusing res.Languages is what makes the filter apply to
	// the line counts and the file totals alike.
	type langAgg struct {
		files   int
		code    int64
		comment int64
		blank   int64
	}
	byLang := make(map[string]*langAgg)

	for path, cf := range res.Files {
		if ign.Ignored(path, false) {
			continue
		}
		agg := byLang[cf.Lang]
		if agg == nil {
			agg = &langAgg{}
			byLang[cf.Lang] = agg
		}
		agg.files++
		agg.code += int64(cf.Code)
		agg.comment += int64(cf.Comments)
		agg.blank += int64(cf.Blanks)
	}

	out := clocResult{ScannedAt: time.Now()}
	for name, agg := range byLang {
		if agg.code == 0 && agg.comment == 0 && agg.blank == 0 {
			continue // nothing countable (e.g. empty file)
		}
		out.Languages = append(out.Languages, clocLanguageSummary{
			Name:    name,
			Files:   agg.files,
			Code:    agg.code,
			Comment: agg.comment,
			Blank:   agg.blank,
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
