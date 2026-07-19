package handler

import (
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
}

const (
	maxSearchQueryLen = 256 // Maximum query string length
	maxSearchEntries  = 50000 // Maximum entries to collect during recursive walk
)

// searchEntry holds metadata for a file or directory found during search.
type searchEntry struct {
	Name    string // filename only
	RelPath string // relative path from search root (e.g. "internal/handler/file.go")
	Type    string // "dir", "file", or "image"
}

// searchSource implements fuzzy.Source for a slice of searchEntry.
type searchSource []searchEntry

func (s searchSource) String(i int) string { return s[i].Name }
func (s searchSource) Len() int            { return len(s) }

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

// DirSearch handles GET /api/dir/search — SSE stream for file search with fuzzy matching.
// Query params: path (relative dir to search from), q (query string),
// recursive (optional, default "true"), limit (optional, default 100).
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
	recursive := recursiveStr == "" || strings.EqualFold(recursiveStr, "true")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
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

	// Collect entries
	var entries []searchEntry
	if recursive {
		entries = collectEntriesRecursive(absPath, absPath)
	} else {
		entries = collectEntriesFlat(absPath, absPath)
	}

	// Fuzzy match
	matches := fuzzy.FindFrom(query, searchSource(entries))

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)

	total := len(matches)
	truncated := total > limit
	if truncated {
		matches = matches[:limit]
	}

	// Stream results
	for _, m := range matches {
		select {
		case <-r.Context().Done():
			slog.Debug("dir search SSE disconnected during streaming")
			return
		default:
		}

		entry := entries[m.Index]
		result := DirSearchResult{
			Name:           entry.Name,
			Path:           entry.RelPath,
			Type:           entry.Type,
			MatchedIndices: m.MatchedIndexes,
		}
		data, _ := json.Marshal(result)
		_, _ = fmt.Fprintf(w, "event: result\ndata: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}
	}

	// Send done event
	done := DirSearchDone{Total: total, Truncated: truncated}
	data, _ := json.Marshal(done)
	_, _ = fmt.Fprintf(w, "event: done\ndata: %s\n\n", data)
	if canFlush {
		flusher.Flush()
	}
}

// collectEntriesRecursive walks the directory tree from absPath, collecting
// all files and directories (excluding ignored dirs).
// Stops collecting after maxSearchEntries to prevent OOM on massive repos.
func collectEntriesRecursive(absPath string, basePath string) []searchEntry {
	var entries []searchEntry
	filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible entries
		}
		if d.IsDir() && path != absPath && ignoredSearchDirs[d.Name()] {
			return fs.SkipDir
		}
		if path == absPath {
			return nil
		}
		if len(entries) >= maxSearchEntries {
			return fs.SkipDir
		}
		relPath, relErr := filepath.Rel(basePath, path)
		if relErr != nil {
			return nil //nolint:nilerr // skip entries with invalid relative paths
		}
		name := d.Name()
		if d.IsDir() {
			entries = append(entries, searchEntry{Name: name, RelPath: filepath.ToSlash(relPath), Type: "dir"})
		} else {
			entryType := "file"
			if model.IsImageFile(name) {
				entryType = "image"
			}
			entries = append(entries, searchEntry{Name: name, RelPath: filepath.ToSlash(relPath), Type: entryType})
		}
		return nil
	})
	return entries
}

// collectEntriesFlat reads only the top-level entries of absPath.
func collectEntriesFlat(absPath string, basePath string) []searchEntry {
	dirEntries, err := os.ReadDir(absPath)
	if err != nil {
		return nil
	}
	var entries []searchEntry
	for _, d := range dirEntries {
		relPath, relErr := filepath.Rel(basePath, filepath.Join(absPath, d.Name()))
		if relErr != nil {
			continue
		}
		name := d.Name()
		if d.IsDir() {
			entries = append(entries, searchEntry{Name: name, RelPath: filepath.ToSlash(relPath), Type: "dir"})
		} else {
			entryType := "file"
			if model.IsImageFile(name) {
				entryType = "image"
			}
			entries = append(entries, searchEntry{Name: name, RelPath: filepath.ToSlash(relPath), Type: entryType})
		}
	}
	return entries
}
