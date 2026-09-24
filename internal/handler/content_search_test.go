package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeContentSearchResults unmarshals the result events of a content-search
// response, keyed by file path for order-independent assertions.
func decodeContentSearchResults(t *testing.T, body string) (map[string]ContentSearchFileResult, ContentSearchDone) {
	t.Helper()
	events := parseSearchSSEEvents(body)

	out := make(map[string]ContentSearchFileResult)
	for _, raw := range events["result"] {
		var r ContentSearchFileResult
		require.NoError(t, json.Unmarshal(raw, &r), "result event must decode")
		out[r.Path] = r
	}

	var done ContentSearchDone
	doneEvents := events["done"]
	require.Len(t, doneEvents, 1, "exactly one done event")
	require.NoError(t, json.Unmarshal(doneEvents[0], &done), "done event must decode")
	return out, done
}

func contentSearchRequest(t *testing.T, projectDir, query string) *http.Request {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/file/content-search?"+query, nil)
	return withProjectCookie(req, projectDir)
}

func TestContentSearch_BasicLiteralMatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.go", "package main\n// TODO: fix this\nfunc main() {}\n")
	createTestFile(t, env.ProjectDir, "b.go", "package main\nfunc helper() {}\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=TODO")
	w := callHandler(ContentSearch, req)
	assertOK(t, w)
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected SSE content type, got %s", ct)
	}

	results, done := decodeContentSearchResults(t, w.Body.String())
	require.Len(t, results, 1, "only a.go contains TODO")
	hit := results["a.go"]
	require.Len(t, hit.Matches, 1)
	assert.Equal(t, 2, hit.Matches[0].Line)
	assert.Equal(t, "// TODO: fix this", hit.Matches[0].Text)
	require.Len(t, hit.Matches[0].Ranges, 1)
	assert.Equal(t, 3, hit.Matches[0].Ranges[0].Start)
	assert.Equal(t, 7, hit.Matches[0].Ranges[0].End)
	assert.Equal(t, 1, done.Files)
	assert.Equal(t, 1, done.Matches)
}

func TestContentSearch_CaseSensitivity(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.go", "Error and error\n")

	// Case-insensitive by default → two matches on one line.
	req := contentSearchRequest(t, env.ProjectDir, "path=&q=error")
	w := callHandler(ContentSearch, req)
	results, _ := decodeContentSearchResults(t, w.Body.String())
	require.Len(t, results["a.go"].Matches, 1)
	assert.Len(t, results["a.go"].Matches[0].Ranges, 2)

	// Case-sensitive → only the lowercase one.
	req = contentSearchRequest(t, env.ProjectDir, "path=&q=error&caseSensitive=true")
	w = callHandler(ContentSearch, req)
	results, _ = decodeContentSearchResults(t, w.Body.String())
	require.Len(t, results["a.go"].Matches, 1)
	assert.Len(t, results["a.go"].Matches[0].Ranges, 1)
	assert.Equal(t, 10, results["a.go"].Matches[0].Ranges[0].Start)
}

func TestContentSearch_RegexMode(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.go", "var x1 = 1\nvar x22 = 2\nvar yy = 3\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q="+urlQueryEscape(`x\d+`)+"&regex=true")
	w := callHandler(ContentSearch, req)
	results, done := decodeContentSearchResults(t, w.Body.String())

	require.Len(t, results, 1)
	// x1 and x22 both match, on two lines.
	assert.Len(t, results["a.go"].Matches, 2)
	assert.Equal(t, 2, done.Matches)
}

func TestContentSearch_LiteralQueryTreatsMetacharsAsText(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.go", "a.b = 1\naxb = 2\n")

	// Without regex mode, the dot must be literal — only "a.b" matches.
	req := contentSearchRequest(t, env.ProjectDir, "path=&q="+urlQueryEscape("a.b"))
	w := callHandler(ContentSearch, req)
	results, _ := decodeContentSearchResults(t, w.Body.String())
	require.Len(t, results, 1)
	assert.Len(t, results["a.go"].Matches, 1)
	assert.Equal(t, 1, results["a.go"].Matches[0].Line)
}

func TestContentSearch_WholeWord(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.go", "cat category cat\n")

	// Without wholeWord: three substring hits.
	req := contentSearchRequest(t, env.ProjectDir, "path=&q=cat")
	w := callHandler(ContentSearch, req)
	results, _ := decodeContentSearchResults(t, w.Body.String())
	assert.Len(t, results["a.go"].Matches[0].Ranges, 3)

	// With wholeWord: only the two standalone "cat" tokens.
	req = contentSearchRequest(t, env.ProjectDir, "path=&q=cat&wholeWord=true")
	w = callHandler(ContentSearch, req)
	results, _ = decodeContentSearchResults(t, w.Body.String())
	assert.Len(t, results["a.go"].Matches[0].Ranges, 2)
}

func TestContentSearch_RecursiveDefaultAndFlat(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "top.go", "needle\n")
	createTestFile(t, env.ProjectDir, "src/nested.go", "needle\n")

	// Recursive is the default.
	req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle")
	w := callHandler(ContentSearch, req)
	results, _ := decodeContentSearchResults(t, w.Body.String())
	assert.Len(t, results, 2)

	// Flat only sees the top level.
	req = contentSearchRequest(t, env.ProjectDir, "path=&q=needle&recursive=false")
	w = callHandler(ContentSearch, req)
	results, _ = decodeContentSearchResults(t, w.Body.String())
	assert.Len(t, results, 1)
	assert.Contains(t, results, "top.go")
}

func TestContentSearch_IncludeExcludeGlobs(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.go", "needle\n")
	createTestFile(t, env.ProjectDir, "b.ts", "needle\n")
	// A nested directory that is NOT in the built-in prune list (dist,
	// node_modules, …), so it is reachable and exercises the glob boxes rather
	// than the directory pruning.
	createTestFile(t, env.ProjectDir, "out/c.js", "needle\n")

	t.Run("include restricts to matching files", func(t *testing.T) {
		req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle&include="+urlQueryEscape("*.go"))
		w := callHandler(ContentSearch, req)
		results, _ := decodeContentSearchResults(t, w.Body.String())
		require.Len(t, results, 1)
		assert.Contains(t, results, "a.go")
	})

	t.Run("exclude removes matching files", func(t *testing.T) {
		req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle&exclude="+urlQueryEscape("out/**"))
		w := callHandler(ContentSearch, req)
		results, _ := decodeContentSearchResults(t, w.Body.String())
		assert.Len(t, results, 2)
		assert.NotContains(t, results, "out/c.js")
	})

	t.Run("exclude by extension glob", func(t *testing.T) {
		req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle&exclude="+urlQueryEscape("*.ts"))
		w := callHandler(ContentSearch, req)
		results, _ := decodeContentSearchResults(t, w.Body.String())
		assert.Len(t, results, 2)
		assert.NotContains(t, results, "b.ts")
	})

	t.Run("negation in exclude re-includes", func(t *testing.T) {
		req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle&exclude="+urlQueryEscape("out/**,!out/c.js"))
		w := callHandler(ContentSearch, req)
		results, _ := decodeContentSearchResults(t, w.Body.String())
		assert.Contains(t, results, "out/c.js")
	})
}

func TestContentSearch_PrunesBuiltinDirsDespiteInclude(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// `dist` is in the built-in prune list; an explicit include must not
	// resurrect it, because the directory is never descended into.
	createTestFile(t, env.ProjectDir, "dist/bundle.js", "needle\n")
	createTestFile(t, env.ProjectDir, "a.js", "needle\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle&include="+urlQueryEscape("*.js"))
	w := callHandler(ContentSearch, req)
	results, _ := decodeContentSearchResults(t, w.Body.String())

	assert.Len(t, results, 1)
	assert.Contains(t, results, "a.js")
}

func TestContentSearch_SkipsBinaryAndMedia(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "text.go", "needle\n")
	createTestFile(t, env.ProjectDir, "image.png", "needle\n")
	// Extensionless file with NUL bytes — must be skipped by the sniff.
	createTestFile(t, env.ProjectDir, "blob", "needle\x00binary\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle")
	w := callHandler(ContentSearch, req)
	results, _ := decodeContentSearchResults(t, w.Body.String())

	assert.Len(t, results, 1)
	assert.Contains(t, results, "text.go")
	assert.NotContains(t, results, "image.png")
	assert.NotContains(t, results, "blob")
}

func TestContentSearch_PrunesIgnoredDirs(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "src/a.go", "needle\n")
	createTestFile(t, env.ProjectDir, "node_modules/pkg/index.js", "needle\n")
	createTestFile(t, env.ProjectDir, ".git/config", "needle\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle")
	w := callHandler(ContentSearch, req)
	results, _ := decodeContentSearchResults(t, w.Body.String())

	assert.Len(t, results, 1)
	assert.Contains(t, results, "src/a.go")
}

func TestContentSearch_PerFileMatchCap(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	var sb strings.Builder
	for range maxMatchesPerFile + 25 {
		sb.WriteString("needle\n")
	}
	createTestFile(t, env.ProjectDir, "many.txt", sb.String())

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle")
	w := callHandler(ContentSearch, req)
	results, _ := decodeContentSearchResults(t, w.Body.String())

	hit := results["many.txt"]
	require.Len(t, hit.Matches, maxMatchesPerFile, "matches are capped")
	assert.Equal(t, maxMatchesPerFile+25, hit.Total, "total reports the true count")
	assert.True(t, hit.Truncated)
}

func TestContentSearch_LimitTruncatesFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	for i := range 5 {
		createTestFile(t, env.ProjectDir, fmt.Sprintf("f%d.go", i), "needle\n")
	}

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle&limit=2")
	w := callHandler(ContentSearch, req)
	results, done := decodeContentSearchResults(t, w.Body.String())

	assert.Len(t, results, 2)
	assert.True(t, done.Truncated)
}

// A limit exactly equal to the number of matches must NOT be reported as
// truncated. Checking the limit before reading a file would set the flag as
// soon as the count was reached, even with nothing further to find — so the
// fixture includes a trailing NON-matching file to walk past after the limit.
func TestContentSearch_LimitEqualToMatchCountIsNotTruncated(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a1.go", "needle\n")
	createTestFile(t, env.ProjectDir, "a2.go", "needle\n")
	createTestFile(t, env.ProjectDir, "a3.go", "needle\n")
	// Walked after the limit is reached, but does not match.
	createTestFile(t, env.ProjectDir, "z_no_match.go", "nothing here\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle&limit=3")
	w := callHandler(ContentSearch, req)
	results, done := decodeContentSearchResults(t, w.Body.String())

	assert.Len(t, results, 3)
	assert.False(t, done.Truncated, "the trailing file does not match, so nothing was dropped")
}

// A limit larger than the match count is likewise not truncated.
func TestContentSearch_LimitAboveMatchCountIsNotTruncated(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.go", "needle\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle&limit=50")
	w := callHandler(ContentSearch, req)
	results, done := decodeContentSearchResults(t, w.Body.String())

	assert.Len(t, results, 1)
	assert.False(t, done.Truncated)
}

func TestContentSearch_LongLineIsWindowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// One very long line with the needle in the middle.
	long := strings.Repeat("x", 2000) + "needle" + strings.Repeat("y", 2000)
	createTestFile(t, env.ProjectDir, "min.js", long)

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle")
	w := callHandler(ContentSearch, req)
	results, _ := decodeContentSearchResults(t, w.Body.String())

	hit := results["min.js"]
	require.Len(t, hit.Matches, 1)
	text := []rune(hit.Matches[0].Text)
	assert.LessOrEqual(t, len(text), maxMatchLineRunes, "long line is windowed")
	// The reported range must point at "needle" inside the windowed text.
	require.Len(t, hit.Matches[0].Ranges, 1)
	rng := hit.Matches[0].Ranges[0]
	assert.Equal(t, "needle", string(text[rng.Start:rng.End]))
}

func TestContentSearch_RuneOffsetsForCJK(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Multi-byte characters before the match: byte offsets would be wrong.
	createTestFile(t, env.ProjectDir, "cn.go", "// 中文注释 TODO\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=TODO")
	w := callHandler(ContentSearch, req)
	results, _ := decodeContentSearchResults(t, w.Body.String())

	hit := results["cn.go"]
	require.Len(t, hit.Matches, 1)
	text := []rune(hit.Matches[0].Text)
	rng := hit.Matches[0].Ranges[0]
	assert.Equal(t, "TODO", string(text[rng.Start:rng.End]), "ranges are rune offsets, not byte offsets")
}

func TestContentSearch_LeadingIndentTrimmed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.go", "        needle here\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle")
	w := callHandler(ContentSearch, req)
	results, _ := decodeContentSearchResults(t, w.Body.String())

	hit := results["a.go"]
	assert.Equal(t, "needle here", hit.Matches[0].Text)
	rng := hit.Matches[0].Ranges[0]
	assert.Equal(t, 0, rng.Start, "range shifts with the trimmed indent")
	assert.Equal(t, 6, rng.End)
}

func TestContentSearch_IgnoredFlag(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)
	createTestFile(t, env.ProjectDir, ".gitignore", "ignored.go\n")
	createTestFile(t, env.ProjectDir, "ignored.go", "needle\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=needle")
	w := callHandler(ContentSearch, req)
	results, _ := decodeContentSearchResults(t, w.Body.String())

	require.Contains(t, results, "ignored.go")
	assert.True(t, results["ignored.go"].Ignored, "git-ignored hits are flagged for dimming")
}

func TestContentSearch_InvalidRegexSendsErrorEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.go", "needle\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q="+urlQueryEscape("([unclosed")+"&regex=true")
	w := callHandler(ContentSearch, req)

	// The stream starts (200) because EventSource cannot read a 400 body.
	assertOK(t, w)
	events := parseSearchSSEEvents(w.Body.String())
	require.Len(t, events["error"], 1)
	var e ContentSearchError
	require.NoError(t, json.Unmarshal(events["error"][0], &e))
	assert.NotEmpty(t, e.Message)
	assert.Empty(t, events["done"], "no done event after a fatal error")
}

func TestContentSearch_MissingQuery(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=")
	w := callHandler(ContentSearch, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestContentSearch_QueryTooLong(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	long := strings.Repeat("a", maxContentQueryLen+1)
	req := contentSearchRequest(t, env.ProjectDir, "path=&q="+long)
	w := callHandler(ContentSearch, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestContentSearch_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/file/content-search?q=x", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ContentSearch, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestContentSearch_ExternalAbsoluteRoot(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// An absolute root outside the project must be refused.
	req := contentSearchRequest(t, env.ProjectDir, "path="+urlQueryEscape("/etc")+"&q=root")
	w := callHandler(ContentSearch, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestContentSearch_EmptyResultStillSendsDone(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	createTestFile(t, env.ProjectDir, "a.go", "package main\n")

	req := contentSearchRequest(t, env.ProjectDir, "path=&q=zzz_no_match")
	w := callHandler(ContentSearch, req)
	assertOK(t, w)

	events := parseSearchSSEEvents(w.Body.String())
	assert.Empty(t, events["result"])
	require.Len(t, events["done"], 1)
	var done ContentSearchDone
	require.NoError(t, json.Unmarshal(events["done"][0], &done))
	assert.Equal(t, 0, done.Files)
	assert.False(t, done.Truncated)
}

// ── Unit tests for the pure helpers ──

func TestBuildContentRegex(t *testing.T) {
	t.Run("literal quotes metacharacters", func(t *testing.T) {
		re, err := buildContentRegex("a.b", false, false, false)
		require.NoError(t, err)
		assert.True(t, re.MatchString("a.b"))
		assert.False(t, re.MatchString("axb"))
	})

	t.Run("regex mode passes the pattern through", func(t *testing.T) {
		re, err := buildContentRegex(`a.b`, true, false, false)
		require.NoError(t, err)
		assert.True(t, re.MatchString("axb"))
	})

	t.Run("case insensitive by default", func(t *testing.T) {
		re, err := buildContentRegex("todo", false, false, false)
		require.NoError(t, err)
		assert.True(t, re.MatchString("TODO"))
	})

	t.Run("case sensitive when requested", func(t *testing.T) {
		re, err := buildContentRegex("todo", false, false, true)
		require.NoError(t, err)
		assert.False(t, re.MatchString("TODO"))
		assert.True(t, re.MatchString("todo"))
	})

	t.Run("whole word does not match substrings", func(t *testing.T) {
		re, err := buildContentRegex("cat", false, true, false)
		require.NoError(t, err)
		assert.True(t, re.MatchString("a cat here"))
		assert.False(t, re.MatchString("category"))
	})

	t.Run("invalid regex returns an error", func(t *testing.T) {
		_, err := buildContentRegex("([", true, false, false)
		assert.Error(t, err)
	})
}

func TestShouldSearchFile(t *testing.T) {
	cases := []struct {
		name    string
		include string
		exclude string
		path    string
		want    bool
	}{
		{"no boxes searches everything", "", "", "src/a.go", true},
		{"include matches", "*.go", "", "src/a.go", true},
		{"include rejects non-match", "*.go", "", "src/a.ts", false},
		{"exclude removes", "", "*.ts", "src/a.ts", false},
		{"exclude leaves others", "", "*.ts", "src/a.go", true},
		{"exclude dir glob", "", "dist/**", "dist/a.js", false},
		{"include dir glob", "src/**", "", "src/a.go", true},
		{"include dir glob rejects sibling", "src/**", "", "lib/a.go", false},
		{"negation re-includes", "*.go,!vendor/**", "", "vendor/a.go", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			include := compileGlobBox(tc.include)
			exclude := compileGlobBox(tc.exclude)
			assert.Equal(t, tc.want, shouldSearchFile(include, exclude, tc.path))
		})
	}
}

func TestIsBinaryContentExt(t *testing.T) {
	assert.True(t, isBinaryContentExt("logo.PNG"))
	assert.True(t, isBinaryContentExt("archive.tar.gz"))
	assert.False(t, isBinaryContentExt("main.go"))
	assert.False(t, isBinaryContentExt("README"))
}

func TestBuildMatchLine(t *testing.T) {
	t.Run("trims indentation and shifts ranges", func(t *testing.T) {
		text, ranges := buildMatchLine("    foo", []ContentSearchRange{{Start: 4, End: 7}})
		assert.Equal(t, "foo", text)
		assert.Equal(t, []ContentSearchRange{{Start: 0, End: 3}}, ranges)
	})

	t.Run("never trims past the first match", func(t *testing.T) {
		// The match starts at index 2 (after two spaces), so only those two are
		// trimmed even though more whitespace follows.
		text, ranges := buildMatchLine("  foo", []ContentSearchRange{{Start: 2, End: 5}})
		assert.Equal(t, "foo", text)
		assert.Equal(t, []ContentSearchRange{{Start: 0, End: 3}}, ranges)
	})

	t.Run("drops ranges outside the window", func(t *testing.T) {
		long := strings.Repeat("a", 300) + "hit" + strings.Repeat("b", 300) + "hit"
		ranges := []ContentSearchRange{{Start: 300, End: 303}, {Start: 606, End: 609}}
		text, out := buildMatchLine(long, ranges)
		assert.LessOrEqual(t, len([]rune(text)), maxMatchLineRunes)
		require.Len(t, out, 1, "the far match falls outside the window")
		assert.Equal(t, "hit", string([]rune(text)[out[0].Start:out[0].End]))
	})
}

func TestByteOffsetsToRuneRanges(t *testing.T) {
	// "中文" is 6 bytes / 2 runes, so a byte offset of 6 is rune offset 2.
	line := "中文hit"
	// byte 6..9 is "hit"
	out := byteOffsetsToRuneRanges(line, [][]int{{6, 9}})
	require.Len(t, out, 1)
	assert.Equal(t, ContentSearchRange{Start: 2, End: 5}, out[0])
}

// urlQueryEscape escapes a value for use inside a raw query string.
func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}
