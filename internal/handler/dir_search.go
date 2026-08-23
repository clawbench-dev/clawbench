package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"clawbench/internal/model"

	"github.com/sahilm/fuzzy"
)

// ignoredSearchDirs are directory names to skip during recursive search.
var ignoredSearchDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".svn":         true,
	".hg":          true,
	"dist":         true,
	".cache":       true,
	".next":        true,
	"target":       true,
	"Pods":         true,
	".gradle":      true,
}

// isStrictlyBelowBuild reports whether relPath has a `build` segment strictly
// above the leaf (i.e. the path itself is not the `build` directory).
func isStrictlyBelowBuild(relPath string) bool {
	segs := strings.Split(relPath, "/")
	for i, s := range segs {
		if s == "build" && i < len(segs)-1 {
			return true
		}
	}
	return false
}

// isWithinAndroidOutputs reports whether relPath is inside a `build/outputs`
// subtree (e.g. app/build/outputs/apk/release/app-debug.apk).
func isWithinAndroidOutputs(relPath string) bool {
	segs := strings.Split(relPath, "/")
	for i := 0; i < len(segs)-1; i++ {
		if segs[i] == "build" && segs[i+1] == "outputs" {
			return true
		}
	}
	return false
}

// shouldSkipSearchDir decides whether to prune the subtree at relPath (slash-
// normalized, project-root relative) during recursive search. Standard ignored
// dirs are skipped; within a `build` tree only the Android `build/outputs`
// subtree (which holds artifacts like .apk) is kept searchable, so large build
// trees are not fully traversed.
func shouldSkipSearchDir(relPath string, name string) bool {
	if ignoredSearchDirs[name] {
		return true
	}
	return isStrictlyBelowBuild(relPath) && !isWithinAndroidOutputs(relPath)
}

const (
	maxSearchQueryLen = 256 // Maximum query string length
	maxSearchLimit    = 500 // Maximum number of results a client can request

	entryTypeFile  = "file"
	entryTypeDir   = "dir"
	entryTypeImage = "image"
)

// DirSearchResult is one matched file sent as an SSE result event.
type DirSearchResult struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	Type           string `json:"type"`
	MatchedIndices []int  `json:"matchedIndices"`
}

// DirSearchDone is sent as the SSE done event.
type DirSearchDone struct {
	Total     int  `json:"total"`
	Truncated bool `json:"truncated"`
}

// DirSearchError is sent as an SSE error event during search.
type DirSearchError struct {
	Message string `json:"message"`
}

// dirSearchParams holds the parsed query parameters for a directory search.
type dirSearchParams struct {
	relPath   string
	query     string
	recursive bool
	exact     bool
	limit     int
}

// parseSearchParams extracts and validates search query parameters from the request.
// Returns the params and true on success, or writes an error and returns false.
func parseSearchParams(w http.ResponseWriter, r *http.Request) (dirSearchParams, bool) {
	relPath := strings.TrimPrefix(r.URL.Query().Get("path"), "/")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SearchQueryRequired")
		return dirSearchParams{}, false
	}
	if len(query) > maxSearchQueryLen {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SearchQueryRequired")
		return dirSearchParams{}, false
	}

	recursiveStr := r.URL.Query().Get("recursive")
	recursive := true
	if recursiveStr != "" {
		parsed, err := strconv.ParseBool(recursiveStr)
		if err == nil {
			recursive = parsed
		}
	}

	exact := false
	if e := r.URL.Query().Get("exact"); e != "" {
		if parsed, err := strconv.ParseBool(e); err == nil {
			exact = parsed
		}
	}

	limit := model.ConfigInstance.FileSearch.DisplayLimit + 1
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	return dirSearchParams{relPath: relPath, query: query, recursive: recursive, exact: exact, limit: limit}, true
}

// classifyEntry returns the entry type string for a directory entry.
func classifyEntry(d fs.DirEntry, name string) string {
	if d.IsDir() {
		return entryTypeDir
	}
	if model.IsImageFile(name) {
		return entryTypeImage
	}
	return entryTypeFile
}

// DirSearch handles GET /api/dir/search — SSE stream for file search with fuzzy matching.
// Query params: path (relative dir to search from), q (query string),
// recursive (optional, default "true"), limit (optional, default file_search.display_limit+1, max 500).
func DirSearch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	params, ok := parseSearchParams(w, r)
	if !ok {
		return
	}

	basePath, err := filepath.Abs(projectPath)
	if err != nil {
		slog.Error("failed to resolve project path", slog.String("path", projectPath), slog.String("err", err.Error()))
		model.WriteError(w, model.Internal(err))
		return
	}

	absPath, ok := validateAndResolvePath(w, r, basePath, params.relPath)
	if !ok {
		return
	}

	// SSE headers — written before any streaming
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)
	ctx := r.Context()

	// Streaming search: walk + fuzzy match + push results on the fly
	var sentCount int
	var totalMatchCount int
	var truncated bool

	onMatch := func(name, relPathStr, entryType string, matchedIndexes []int) {
		totalMatchCount++
		if sentCount >= params.limit {
			truncated = true
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		result := DirSearchResult{
			Name:           name,
			Path:           relPathStr,
			Type:           entryType,
			MatchedIndices: matchedIndexes,
		}
		data, _ := json.Marshal(result)
		_, _ = fmt.Fprintf(w, "event: result\ndata: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}
		sentCount++
	}

	if params.recursive {
		walkAndMatchRecursive(ctx, absPath, basePath, params.query, params.exact, onMatch)
	} else {
		walkAndMatchFlat(ctx, absPath, basePath, params.query, params.exact, onMatch)
	}

	// Check if context was cancelled
	select {
	case <-ctx.Done():
		slog.Debug("dir search SSE disconnected")
		return
	default:
	}

	// Send done event — total is the total number of matches found; truncated indicates more exist
	done := DirSearchDone{Total: totalMatchCount, Truncated: truncated}
	data, _ := json.Marshal(done)
	_, _ = fmt.Fprintf(w, "event: done\ndata: %s\n\n", data)
	if canFlush {
		flusher.Flush()
	}
}

// walkAndMatchRecursive walks the directory tree, fuzzy-matching each entry against the query.
// On match, it calls onMatch. It respects context cancellation.
func walkAndMatchRecursive(ctx context.Context, absPath string, basePath string, query string, exact bool, onMatch func(string, string, string, []int)) {
	err := filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible entries
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if path == absPath {
			return nil
		}

		relPath, relErr := filepath.Rel(basePath, path)
		if relErr != nil {
			return nil //nolint:nilerr // skip entries with invalid relative paths
		}
		relPathSlash := filepath.ToSlash(relPath)

		if d.IsDir() && shouldSkipSearchDir(relPathSlash, d.Name()) {
			return fs.SkipDir
		}

		name := d.Name()
		matchedIndexes := matchName(name, query, exact)
		if len(matchedIndexes) > 0 {
			entryType := classifyEntry(d, name)
			onMatch(name, relPathSlash, entryType, matchedIndexes)
		}

		return nil
	})
	if err != nil {
		slog.Debug("dir search walk incomplete", slog.String("err", err.Error()))
	}
}

// walkAndMatchFlat reads only the top-level entries and fuzzy-matches against the query.
func walkAndMatchFlat(ctx context.Context, absPath string, basePath string, query string, exact bool, onMatch func(string, string, string, []int)) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	dirEntries, err := os.ReadDir(absPath)
	if err != nil {
		return
	}

	for _, d := range dirEntries {
		select {
		case <-ctx.Done():
			return
		default:
		}

		fullPath := filepath.Join(absPath, d.Name())
		relPath, relErr := filepath.Rel(basePath, fullPath)
		if relErr != nil {
			continue
		}

		name := d.Name()
		matchedIndexes := matchName(name, query, exact)
		if len(matchedIndexes) > 0 {
			entryType := classifyEntry(d, name)
			onMatch(name, filepath.ToSlash(relPath), entryType, matchedIndexes)
		}
	}
}

// matchName returns the matched character indices for name against query.
// In exact mode, only a case-insensitive exact filename match returns indices (covering the whole name);
// otherwise it uses fuzzy matching.
func matchName(name, query string, exact bool) []int {
	if exact {
		if strings.EqualFold(name, query) {
			idx := make([]int, len(name))
			for i := range idx {
				idx[i] = i
			}
			return idx
		}
		return nil
	}
	matches := fuzzy.Find(query, []string{name})
	if len(matches) > 0 {
		return matches[0].MatchedIndexes
	}
	return nil
}
