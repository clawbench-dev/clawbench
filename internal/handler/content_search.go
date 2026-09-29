package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"clawbench/internal/gitignore"
	"clawbench/internal/model"

	gogitignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

const (
	// maxContentQueryLen bounds the raw query string (before regex compilation).
	maxContentQueryLen = 512
	// maxContentSearchLimit caps how many FILES a client may request.
	maxContentSearchLimit = 500
	// maxMatchesPerFile caps the matches reported for one file. A single file
	// with tens of thousands of hits (a lockfile, a minified bundle) would
	// otherwise dominate the payload and the DOM; the file is still reported,
	// flagged as truncated, so the user can open it and search in place.
	maxMatchesPerFile = 200
	// maxContentSearchFileBytes skips files larger than this. Content search
	// reads whole files, so an unbounded walk would stall on multi-hundred-MB
	// artifacts; 5 MiB covers normal source files with room to spare.
	maxContentSearchFileBytes = 5 * 1024 * 1024
	// maxMatchLineRunes caps the rendered line length. Minified bundles are one
	// multi-megabyte line; the line is windowed around the first match instead
	// of shipping the whole thing to the browser.
	maxMatchLineRunes = 400
	// matchLineWindowBefore is how much context precedes the first match when a
	// line has to be windowed.
	matchLineWindowBefore = 80
	// contentSearchReaderBuf is the initial read buffer per file.
	contentSearchReaderBuf = 64 * 1024
	// errDetailKey is the template-data key for an error message handed to the
	// i18n layer (the `{{.Error}}` placeholder in the InvalidRegex message).
	errDetailKey = "Error"
)

// ContentSearchMatch is one matching line inside a file.
type ContentSearchMatch struct {
	// Line is the 1-based line number in the original file.
	Line int `json:"line"`
	// Text is the (leading-whitespace-trimmed, possibly windowed) line content.
	Text string `json:"text"`
	// Ranges are RUNE offsets into Text, for highlighting. Rune (not byte)
	// offsets because the frontend highlights by character position.
	Ranges []ContentSearchRange `json:"ranges"`
}

// ContentSearchRange is a rune-offset span within ContentSearchMatch.Text.
type ContentSearchRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ContentSearchFileResult is one matched file, sent as an SSE result event.
// Matches for a file are grouped into a single event so the client never has
// to reassemble a file's hits across messages.
type ContentSearchFileResult struct {
	Name    string               `json:"name"`
	Path    string               `json:"path"`
	Matches []ContentSearchMatch `json:"matches"`
	// Total is the number of matches found in the file, which may exceed
	// len(Match) when the per-file cap was hit.
	Total int `json:"total"`
	// Truncated reports that the per-file cap was hit.
	Truncated bool `json:"truncated,omitempty"`
	// Ignored mirrors the directory listing's flag so a hit in a git-ignored
	// file is dimmed the same way the file manager dims it.
	Ignored bool `json:"ignored,omitempty"`
}

// ContentSearchDone is the terminating SSE event.
type ContentSearchDone struct {
	// Files is the number of files with at least one match.
	Files int `json:"files"`
	// Matches is the total number of matching lines across reported files.
	Matches int `json:"matches"`
	// Searched is how many files were actually read.
	Searched int `json:"searched"`
	// Truncated reports that more matching files existed than `limit` allowed.
	Truncated bool `json:"truncated"`
}

// contentSearchParams holds the parsed query parameters for a content search.
type contentSearchParams struct {
	path          string
	query         string
	recursive     bool
	useRegex      bool
	wholeWord     bool
	caseSensitive bool
	include       string
	exclude       string
	limit         int
}

// parseContentSearchParams extracts and validates the content-search query
// parameters. Query and regex validation happen before any SSE header is
// written so a bad request is a real 400 rather than a 200 stream.
func parseContentSearchParams(w http.ResponseWriter, r *http.Request) (contentSearchParams, bool) {
	// Absolute paths are project-EXTERNAL and keep their leading separator;
	// relative paths resolve against the project root (same contract as
	// /api/dir/search — see parseSearchParams).
	rawPath := r.URL.Query().Get("path")
	pathParam := rawPath
	if !filepath.IsAbs(rawPath) {
		pathParam = strings.TrimPrefix(rawPath, "/")
	}

	query := r.URL.Query().Get("q")
	if strings.TrimSpace(query) == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SearchQueryRequired")
		return contentSearchParams{}, false
	}
	if len(query) > maxContentQueryLen {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SearchQueryRequired")
		return contentSearchParams{}, false
	}

	boolParam := func(name string, def bool) bool {
		v := r.URL.Query().Get(name)
		if v == "" {
			return def
		}
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return def
		}
		return parsed
	}

	limit := model.ConfigInstance.FileSearch.DisplayLimit + 1
	// A zero DisplayLimit means the config was never defaulted (tests, early
	// startup). Falling through with limit=1 would silently return a single
	// file, so apply the same default the config loader would.
	if limit <= 1 {
		limit = 101
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > maxContentSearchLimit {
		limit = maxContentSearchLimit
	}

	return contentSearchParams{
		path:  pathParam,
		query: query,
		// Content search is recursive by default: a non-recursive content
		// search only reads the top level, which is rarely what is wanted.
		recursive:     boolParam("recursive", true),
		useRegex:      boolParam("regex", false),
		wholeWord:     boolParam("wholeWord", false),
		caseSensitive: boolParam("caseSensitive", false),
		include:       r.URL.Query().Get("include"),
		exclude:       r.URL.Query().Get("exclude"),
		limit:         limit,
	}, true
}

// buildContentRegex compiles the search pattern. A literal query is quoted so
// regex metacharacters are inert; whole-word wraps the pattern in \b; case
// insensitivity is applied with an inline flag. \b is ASCII-only in Go's RE2,
// which is why whole-word is documented as an ASCII-word feature.
func buildContentRegex(query string, useRegex, wholeWord, caseSensitive bool) (*regexp.Regexp, error) {
	pattern := query
	if !useRegex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if wholeWord {
		pattern = `\b(?:` + pattern + `)\b`
	}
	if !caseSensitive {
		pattern = `(?i)` + pattern
	}
	return regexp.Compile(pattern)
}

// compileGlobBox parses a comma-separated glob list into ordered patterns.
// Patterns follow gitignore glob syntax (so `**` crosses directories), which is
// the same engine the file manager already uses for dimming ignored entries —
// reusing it keeps include/exclude and ignore semantics consistent.
func compileGlobBox(spec string) []gogitignore.Pattern {
	if strings.TrimSpace(spec) == "" {
		return nil
	}
	var out []gogitignore.Pattern
	for _, raw := range strings.Split(spec, ",") {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		out = append(out, gogitignore.ParsePattern(p, nil))
	}
	return out
}

// matchGlobBox evaluates an ordered glob list with last-match-wins semantics.
// `def` is the decision when no pattern matches.
//
// gitignore's engine reports a plain pattern as Exclude and a `!`-prefixed one
// as Include, so both boxes map the same way: a plain match means "the box
// matched this path" (true) and `!` means "the box explicitly does NOT match"
// (false). The boxes differ only in their default: an empty include box means
// "everything is included", while an empty exclude box means "nothing is
// excluded".
func matchGlobBox(pats []gogitignore.Pattern, parts []string, def bool) bool {
	result := def
	for _, p := range pats {
		switch p.Match(parts, false) {
		case gogitignore.Exclude:
			result = true
		case gogitignore.Include:
			result = false
		}
	}
	return result
}

// shouldSearchFile decides whether a file passes the include/exclude boxes.
// `relPathSlash` is the project- (or root-) relative slash path.
func shouldSearchFile(include, exclude []gogitignore.Pattern, relPathSlash string) bool {
	parts := strings.Split(relPathSlash, "/")
	if len(parts) == 0 {
		return false
	}
	if !matchGlobBox(include, parts, len(include) == 0) {
		return false
	}
	return !matchGlobBox(exclude, parts, false)
}

// binaryContentExts are extensions whose contents are never searched. They are
// excluded by extension rather than by sniffing so large media files are
// skipped without opening them at all.
//
// literals are the point (they must each be present), and the same list is
// intentionally independent of the display-oriented lists elsewhere.
//
//nolint:goconst // an exhaustive media/archive extension list; the repeated
var binaryContentExts = []string{
	".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico", ".tiff", ".tif", ".avif",
	".mp3", ".wav", ".ogg", ".m4a", ".aac", ".flac", ".wma", ".opus",
	".mp4", ".mkv", ".avi", ".mov", ".webm", ".flv", ".wmv", ".m4v", ".3gp", ".m3u8",
	".docx", ".xlsx", ".pptx", ".xls", ".doc", ".ppt",
	".zip", ".gz", ".tar", ".bz2", ".xz", ".7z", ".rar", ".jar", ".war",
	".exe", ".dll", ".so", ".dylib", ".a", ".o", ".class", ".pyc", ".wasm",
	".pdf", ".ttf", ".otf", ".woff", ".woff2", ".eot", ".icns", ".db", ".sqlite",
}

// isBinaryContentExt reports whether name has an extension that is never
// content-searched.
func isBinaryContentExt(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range binaryContentExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// byteOffsetsToRuneRanges converts regexp byte offsets into rune offsets within
// line. Regexp offsets always land on rune boundaries.
func byteOffsetsToRuneRanges(line string, idx [][]int) []ContentSearchRange {
	if len(idx) == 0 {
		return nil
	}
	ranges := make([]ContentSearchRange, 0, len(idx))
	// Offsets are ascending, so one forward walk serves every match.
	runeOffset := 0
	byteOffset := 0
	advanceTo := func(target int) {
		for byteOffset < target {
			_, size := utf8.DecodeRuneInString(line[byteOffset:])
			if size <= 0 {
				size = 1
			}
			byteOffset += size
			runeOffset++
		}
	}
	for _, m := range idx {
		if len(m) < 2 {
			continue
		}
		advanceTo(m[0])
		start := runeOffset
		advanceTo(m[1])
		ranges = append(ranges, ContentSearchRange{Start: start, End: runeOffset})
	}
	return ranges
}

// buildMatchLine trims leading whitespace and, for very long lines, windows the
// text around the first match. Returned ranges are rune offsets into the
// returned text.
func buildMatchLine(line string, ranges []ContentSearchRange) (string, []ContentSearchRange) {
	if len(ranges) == 0 {
		return line, ranges
	}

	runes := []rune(line)

	// Drop common leading indentation: VSCode shows the line without it, and it
	// wastes the visible width on deeply nested code. Never trim past the first
	// match.
	lead := 0
	for lead < len(runes) && unicode.IsSpace(runes[lead]) {
		lead++
	}
	if lead > ranges[0].Start {
		lead = ranges[0].Start
	}

	if len(runes)-lead <= maxMatchLineRunes {
		return string(runes[lead:]), shiftRanges(ranges, lead)
	}

	windowStart := ranges[0].Start - matchLineWindowBefore
	if windowStart < lead {
		windowStart = lead
	}
	windowEnd := windowStart + maxMatchLineRunes
	if windowEnd > len(runes) {
		windowEnd = len(runes)
	}

	clipped := make([]ContentSearchRange, 0, len(ranges))
	for _, rg := range ranges {
		if rg.Start < windowStart || rg.End > windowEnd {
			continue
		}
		clipped = append(clipped, ContentSearchRange{Start: rg.Start - windowStart, End: rg.End - windowStart})
	}
	return string(runes[windowStart:windowEnd]), clipped
}

// shiftRanges offsets every range left by n (used after trimming indentation).
func shiftRanges(ranges []ContentSearchRange, n int) []ContentSearchRange {
	out := make([]ContentSearchRange, 0, len(ranges))
	for _, rg := range ranges {
		start := rg.Start - n
		if start < 0 {
			start = 0
		}
		end := rg.End - n
		if end < start {
			end = start
		}
		out = append(out, ContentSearchRange{Start: start, End: end})
	}
	return out
}

// searchFileContent reads one file and returns its matches (capped) plus the
// total match count. Returns ok=false when the file cannot be read or is binary.
func searchFileContent(absPath string, re *regexp.Regexp) (matches []ContentSearchMatch, total int, ok bool) {
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() || info.Size() > maxContentSearchFileBytes {
		return nil, 0, false
	}

	f, err := os.Open(absPath)
	if err != nil {
		return nil, 0, false
	}
	defer func() { _ = f.Close() }()

	// Sniff for NUL bytes before treating the file as text. Extensions are
	// filtered earlier; this catches extensionless binaries (compiled output,
	// data blobs) that would otherwise produce garbage matches.
	sniff := make([]byte, binarySniffSize)
	n, _ := io.ReadFull(f, sniff)
	if hasBinaryContent(sniff[:n]) {
		return nil, 0, false
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, 0, false
	}

	reader := bufio.NewReaderSize(f, contentSearchReaderBuf)
	lineNo := 0
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			lineNo++
			trimmed := strings.TrimRight(line, "\r\n")
			idx := re.FindAllStringIndex(trimmed, -1)
			if len(idx) > 0 {
				total += len(idx)
				if len(matches) < maxMatchesPerFile {
					runeRanges := byteOffsetsToRuneRanges(trimmed, idx)
					text, adj := buildMatchLine(trimmed, runeRanges)
					matches = append(matches, ContentSearchMatch{Line: lineNo, Text: text, Ranges: adj})
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	return matches, total, true
}

// ContentSearch handles GET /api/file/content-search — an SSE stream of files
// whose contents match the query.
//
// Query params: q (required), path, recursive (default "true"), regex,
// wholeWord, caseSensitive, include, exclude, limit.
//
// One `result` event is emitted per matching file (with all its matches
// grouped), followed by a single `done` event. An invalid regex emits an
// `error` event rather than a 400 so the client can surface the reason — an
// EventSource cannot read the body of a non-2xx response.
func ContentSearch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	params, ok := parseContentSearchParams(w, r)
	if !ok {
		return
	}

	basePath, err := filepath.Abs(projectPath)
	if err != nil {
		slog.Error("content search: failed to resolve project path",
			slog.String("path", projectPath), slog.String("err", err.Error()))
		model.WriteError(w, model.Internal(err))
		return
	}

	absPath, ok := resolveSearchRoot(w, r, basePath, params.path)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()

	re, err := buildContentRegex(params.query, params.useRegex, params.wholeWord, params.caseSensitive)
	if err != nil {
		writeSSEEvent(w, "error", ContentSearchError{
			Message: T(r, "InvalidRegex", map[string]any{errDetailKey: err.Error()}),
		})
		return
	}

	stats := contentSearchStats{}

	// An absolute root is project-external: hits are reported as absolute paths
	// so they match how the manager addresses external entries (mirrors
	// DirSearch).
	externalRoot := filepath.IsAbs(params.path)
	walkBase := basePath
	if externalRoot {
		walkBase = absPath
	}

	collector := newContentSearchCollector(contentSearchCollectorConfig{
		w:            w,
		params:       params,
		re:           re,
		include:      compileGlobBox(params.include),
		exclude:      compileGlobBox(params.exclude),
		ignored:      gitignore.ForDir(absPath),
		walkBase:     walkBase,
		externalRoot: externalRoot,
		stats:        &stats,
	})

	if params.recursive {
		walkContentRecursive(ctx, absPath, walkBase, collector.handleFile)
	} else {
		walkContentFlat(ctx, absPath, collector.handleFile)
	}

	select {
	case <-ctx.Done():
		slog.Debug("content search SSE disconnected")
		return
	default:
	}

	writeSSEEvent(w, "done", ContentSearchDone{
		Files:     stats.files,
		Matches:   stats.matches,
		Searched:  stats.searched,
		Truncated: stats.truncated,
	})
}

// contentSearchStats accumulates the counters reported in the done event.
type contentSearchStats struct {
	files     int
	matches   int
	searched  int
	truncated bool
}

// contentSearchCollectorConfig carries everything the per-file handler needs.
// Bundling it keeps ContentSearch's own cyclomatic complexity down: the branchy
// per-file decision lives in handleFile instead of inline in the handler.
type contentSearchCollectorConfig struct {
	w            http.ResponseWriter
	params       contentSearchParams
	re           *regexp.Regexp
	include      []gogitignore.Pattern
	exclude      []gogitignore.Pattern
	ignored      *gitignore.Matcher
	walkBase     string
	externalRoot bool
	stats        *contentSearchStats
}

type contentSearchCollector struct {
	cfg contentSearchCollectorConfig
}

func newContentSearchCollector(cfg contentSearchCollectorConfig) *contentSearchCollector {
	return &contentSearchCollector{cfg: cfg}
}

// handleFile inspects one candidate file, emitting a result event when it has
// matches. It returns false to signal the walk should stop (the file limit was
// reached).
func (c *contentSearchCollector) handleFile(path string, d fs.DirEntry) bool {
	relPath, relErr := filepath.Rel(c.cfg.walkBase, path)
	if relErr != nil {
		return true
	}
	relPathSlash := filepath.ToSlash(relPath)

	name := d.Name()
	if isBinaryContentExt(name) || !shouldSearchFile(c.cfg.include, c.cfg.exclude, relPathSlash) {
		return true
	}

	matches, total, ok := searchFileContent(path, c.cfg.re)
	if !ok {
		return true
	}
	c.cfg.stats.searched++
	if total == 0 {
		return true
	}

	// The limit is checked only once a file is known to MATCH. Checking it for
	// every candidate would report truncation as soon as the limit was reached
	// even when no further match exists — a false "there is more" signal.
	if c.cfg.stats.files >= c.cfg.params.limit {
		c.cfg.stats.truncated = true
		return false
	}

	c.cfg.stats.files++
	c.cfg.stats.matches += len(matches)
	c.emit(name, relPathSlash, path, matches, total)
	return true
}

// emit writes one result event. An absolute search root reports absolute paths
// so they match how the manager addresses project-external entries.
func (c *contentSearchCollector) emit(name, relPathSlash, absFile string, matches []ContentSearchMatch, total int) {
	resultPath := relPathSlash
	if c.cfg.externalRoot {
		resultPath = filepath.ToSlash(absFile)
	}
	writeSSEEvent(c.cfg.w, "result", ContentSearchFileResult{
		Name:      name,
		Path:      resultPath,
		Matches:   matches,
		Total:     total,
		Truncated: total > len(matches),
		Ignored:   isIgnored(c.cfg.ignored, filepath.Dir(absFile), name, false),
	})
}

// ContentSearchError is sent as an SSE error event.
type ContentSearchError struct {
	Message string `json:"message"`
}

// writeSSEEvent serializes one SSE event and flushes it. Flushing is best
// effort: httptest recorders do not implement http.Flusher, and the response is
// complete either way.
func writeSSEEvent(w http.ResponseWriter, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// walkContentRecursive walks the tree, handing each regular file to handle.
// Directory pruning mirrors /api/dir/search so the two searches agree on which
// subtrees are worth descending into.
func walkContentRecursive(ctx context.Context, absPath, basePath string, handle func(string, fs.DirEntry) bool) {
	_ = filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible entries
		}
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

		if d.IsDir() {
			if shouldSkipSearchDir(relPathSlash, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // symlinks, sockets, devices
		}
		if !handle(path, d) {
			return fs.SkipAll
		}
		return nil
	})
}

// walkContentFlat reads only the top-level files of absPath.
func walkContentFlat(ctx context.Context, absPath string, handle func(string, fs.DirEntry) bool) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return
	}
	for _, d := range entries {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if d.IsDir() || !d.Type().IsRegular() {
			continue
		}
		if !handle(filepath.Join(absPath, d.Name()), d) {
			return
		}
	}
}
