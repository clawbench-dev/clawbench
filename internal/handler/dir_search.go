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
	"build":        true,
	".cache":       true,
	".next":        true,
	"target":       true,
	"Pods":         true,
	".gradle":      true,
}

const (
	maxSearchQueryLen  = 256  // Maximum query string length
	maxSearchLimit     = 500  // Maximum number of results a client can request
	defaultSearchLimit = 100  // Default number of results
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

// DirSearch handles GET /api/dir/search — SSE stream for file search with fuzzy matching.
// Query params: path (relative dir to search from), q (query string),
// recursive (optional, default "true"), limit (optional, default 100, max 500).
func DirSearch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	relPath := strings.TrimPrefix(r.URL.Query().Get("path"), "/")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SearchQueryRequired")
		return
	}
	if len(query) > maxSearchQueryLen {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SearchQueryRequired")
		return
	}

	recursiveStr := r.URL.Query().Get("recursive")
	recursive := true
	if recursiveStr != "" {
		parsed, err := strconv.ParseBool(recursiveStr)
		if err == nil {
			recursive = parsed
		}
	}

	limit := defaultSearchLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	basePath, err := filepath.Abs(projectPath)
	if err != nil {
		slog.Error("failed to resolve project path", slog.String("path", projectPath), slog.String("err", err.Error()))
		model.WriteError(w, model.Internal(err))
		return
	}

	absPath, ok := validateAndResolvePath(w, r, basePath, relPath)
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
	var truncated bool

	onMatch := func(name, relPathStr, entryType string, matchedIndexes []int) {
		if sentCount >= limit {
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

	if recursive {
		walkAndMatchRecursive(ctx, absPath, absPath, query, onMatch)
	} else {
		walkAndMatchFlat(ctx, absPath, absPath, query, onMatch)
	}

	// Check if context was cancelled
	select {
	case <-ctx.Done():
		slog.Debug("dir search SSE disconnected")
		return
	default:
	}

	// Send done event — total is the number of results sent; truncated indicates more exist
	done := DirSearchDone{Total: sentCount, Truncated: truncated}
	data, _ := json.Marshal(done)
	_, _ = fmt.Fprintf(w, "event: done\ndata: %s\n\n", data)
	if canFlush {
		flusher.Flush()
	}
}

// walkAndMatchRecursive walks the directory tree, fuzzy-matching each entry against the query.
// On match, it calls onMatch. It respects context cancellation.
func walkAndMatchRecursive(ctx context.Context, absPath string, basePath string, query string, onMatch func(string, string, string, []int)) {
	filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible entries
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if d.IsDir() && path != absPath && ignoredSearchDirs[d.Name()] {
			return fs.SkipDir
		}
		if path == absPath {
			return nil
		}

		relPath, relErr := filepath.Rel(basePath, path)
		if relErr != nil {
			return nil //nolint:nilerr // skip entries with invalid relative paths
		}

		name := d.Name()
		matches := fuzzy.Find(query, []string{name})
		if len(matches) > 0 {
			entryType := "file"
			if d.IsDir() {
				entryType = "dir"
			} else if model.IsImageFile(name) {
				entryType = "image"
			}
			onMatch(name, filepath.ToSlash(relPath), entryType, matches[0].MatchedIndexes)
		}

		return nil
	})
}

// walkAndMatchFlat reads only the top-level entries and fuzzy-matches against the query.
func walkAndMatchFlat(ctx context.Context, absPath string, basePath string, query string, onMatch func(string, string, string, []int)) {
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
		matches := fuzzy.Find(query, []string{name})
		if len(matches) > 0 {
			entryType := "file"
			if d.IsDir() {
				entryType = "dir"
			} else if model.IsImageFile(name) {
				entryType = "image"
			}
			onMatch(name, filepath.ToSlash(relPath), entryType, matches[0].MatchedIndexes)
		}
	}
}
