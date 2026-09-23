package service

import (
	"strings"
	"testing"
	"unicode/utf8"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// These tests live in package service (not service_test) so they can exercise
// the unexported title/derivation helpers directly, without adding a production
// export that exists only for tests.

// TestTitleFromFileEntries_QuotePrefersText verifies a quote-only message gets
// a meaningful title from the quoted text.
//
// A quote's Path is a label — empty for a quote taken from a chat message — so
// deriving the title from filepath.Base(Path) would produce a bare filename or
// nothing at all, even though the quoted text is right there.
func TestTitleFromFileEntries_QuotePrefersText(t *testing.T) {
	cases := []struct {
		name  string
		files []model.FileEntry
		want  string
	}{
		{
			"quote text wins over the label",
			[]model.FileEntry{{Path: "src/a.go", Kind: "quote", Text: "func main() {\n\tprintln(1)\n}"}},
			"func main() {",
		},
		{
			"leading blank lines are skipped",
			[]model.FileEntry{{Path: "", Kind: "quote", Text: "\n\n  解释这段代码  \nmore"}},
			"解释这段代码",
		},
		{
			"whitespace-only text contributes nothing for an unlabelled quote",
			[]model.FileEntry{{Kind: "quote", Text: "   "}},
			"",
		},
		{
			"a quote with empty text but a label still uses the basename",
			[]model.FileEntry{{Path: "src/a.go", Kind: "quote"}},
			"a.go",
		},
		{
			"plain files are unchanged",
			[]model.FileEntry{{Path: "src/a.go"}, {Path: "src/b.ts"}},
			"a.go, b.ts",
		},
		{
			"no entries at all",
			nil,
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, titleFromFileEntries(tc.files))
		})
	}
}

// TestTitleFromFileEntries_QuoteLineIsCapped keeps one long quoted line from
// becoming an unbounded session title.
func TestTitleFromFileEntries_QuoteLineIsCapped(t *testing.T) {
	got := titleFromFileEntries([]model.FileEntry{{Kind: "quote", Text: strings.Repeat("x", 200)}})
	assert.Equal(t, maxTitleLineRunes, utf8.RuneCountInString(got))
}

// TestFilePathsFromFiles_SkipsQuotes guards the legacy filePaths channel on the
// queue_drain event: a quote's Path is a label (often empty), and emitting it
// would surface a bogus "" path to the frontend.
func TestFilePathsFromFiles_SkipsQuotes(t *testing.T) {
	files := []model.FileEntry{
		{Path: "/src/a.go"},
		{Kind: "quote", ID: "q1", Text: "body"}, // Path is empty
		{Path: "src/b.go", Kind: "quote", ID: "q2", Text: "body"},
		{Path: "/src/c.go"},
	}

	assert.Equal(t, []string{"/src/a.go", "/src/c.go"}, filePathsFromFiles(files))
}

// TestFilePathsFromFiles_QuoteOnlyYieldsEmpty ensures the payload is an empty
// slice rather than a slice containing "".
func TestFilePathsFromFiles_QuoteOnlyYieldsEmpty(t *testing.T) {
	got := filePathsFromFiles([]model.FileEntry{{Kind: "quote", ID: "q1", Text: "body"}})
	assert.Empty(t, got)
	assert.NotContains(t, got, "")
}
