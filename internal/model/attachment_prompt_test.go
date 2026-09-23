package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The attachment→prompt classification is shared by internal/handler and
// internal/service, which cannot import each other. These tests live with the
// code so the contract is pinned where both callers can see it.

func TestFileEntryLabel(t *testing.T) {
	cases := []struct {
		name  string
		entry FileEntry
		want  string
	}{
		{"no line info", FileEntry{Path: "/src/main.go"}, "/src/main.go"},
		{"single line", FileEntry{Path: "/src/main.go", StartLine: 10, EndLine: 10}, "/src/main.go:10"},
		{"line range", FileEntry{Path: "/src/main.go", StartLine: 10, EndLine: 20}, "/src/main.go:10-20"},
		{"start line only", FileEntry{Path: "/src/main.go", StartLine: 5}, "/src/main.go:5"},
		{"end line without start is ignored", FileEntry{Path: "/src/main.go", EndLine: 7}, "/src/main.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, FileEntryLabel(tc.entry))
		})
	}
}

func TestClassifyAttachments_Buckets(t *testing.T) {
	entries := []FileEntry{
		{Path: "/src/a.go", StartLine: 1, EndLine: 4},
		{Path: "/src/dir", IsDir: true},
		{Path: "acme/widgets#1", Kind: "url", URL: "https://github.com/acme/widgets/issues/1"},
		{Path: "acme/widgets#2", Kind: "url", URL: "https://github.com/acme/widgets/pull/2"},
	}
	parts := ClassifyAttachments(entries, nil)

	assert.Equal(t, []string{"/src/a.go:1-4"}, parts.FileLabels)
	assert.Equal(t, []string{"/src/dir"}, parts.DirPaths)
	assert.Equal(t, []string{
		"https://github.com/acme/widgets/issues/1",
		"https://github.com/acme/widgets/pull/2",
	}, parts.URLs, "a URL is neither a file nor a directory")
}

func TestClassifyAttachments_ExcludesPathsAlreadyInFilePaths(t *testing.T) {
	entries := []FileEntry{{Path: "/src/a.go"}, {Path: "/src/b.go"}}
	parts := ClassifyAttachments(entries, map[string]struct{}{"/src/a.go": {}})

	assert.Equal(t, []string{"/src/b.go"}, parts.FileLabels)
}

// TestClassifyAttachments_URLIsNeverTreatedAsPath is the core regression guard:
// a URL entry must not fall into the file/dir buckets, which is what made the
// AI look for a local file named "acme/widgets#451".
func TestClassifyAttachments_URLIsNeverTreatedAsPath(t *testing.T) {
	entries := []FileEntry{{Path: "acme/widgets#451", Kind: "url", URL: "https://github.com/acme/widgets/issues/451"}}
	parts := ClassifyAttachments(entries, nil)

	assert.Empty(t, parts.FileLabels, "a URL label must never become a file label")
	assert.Empty(t, parts.DirPaths)
	assert.Len(t, parts.URLs, 1)
}

// A kind=url entry with an EMPTY address is not a URL (FileEntry.IsURL requires
// both). Such a row cannot come through the API — validatedURLEntry rejects it
// with 400 — so this only documents the classifier's fallback for a corrupt or
// legacy row: it is treated as a plain file label rather than being dropped.
func TestClassifyAttachments_EmptyURLIsNotAURL(t *testing.T) {
	entries := []FileEntry{{Path: "label", Kind: "url"}}
	parts := ClassifyAttachments(entries, nil)

	assert.Empty(t, parts.URLs)
	assert.Equal(t, []string{"label"}, parts.FileLabels,
		"kind=url with no address is not a usable URL; it falls back to its label")
}

func TestApplyAttachmentPrefixes_LinkLeadsTheBlock(t *testing.T) {
	parts := AttachmentPromptParts{
		FileLabels: []string{"/src/a.go"},
		URLs:       []string{"https://example.com/x"},
	}
	got := ApplyAttachmentPrefixes("问题", nil, nil, parts)

	assert.True(t, strings.HasPrefix(got, ReferencedLinkPrefix+"https://example.com/x]\n"),
		"the link leads the attachment block — it is the subject of a quoted issue/PR, got %q", got)
	assert.Contains(t, got, "[User uploaded 1 file(s): /src/a.go]")
	assert.True(t, strings.HasSuffix(got, "\n问题"), "the user's text stays last")
}

// Every header must be exactly one line: the session-title stripper removes
// them with a strip-to-newline rule, so a header spanning two lines would leak
// its tail into the derived title.
func TestApplyAttachmentPrefixes_EveryHeaderIsOneLine(t *testing.T) {
	parts := AttachmentPromptParts{
		FileLabels: []string{"/src/a.go", "/src/b.go:3"},
		DirPaths:   []string{"/proj/dir"},
		URLs:       []string{"https://github.com/acme/widgets/issues/451"},
	}
	got := ApplyAttachmentPrefixes("问题", []string{"/proj/legacy.go"}, []string{"/proj/legacydir"}, parts)

	lines := strings.Split(got, "\n")
	assert.Equal(t, 6, len(lines), "5 one-line headers + the user's text, got %q", got)
	assert.Equal(t, "问题", lines[len(lines)-1])
	for i, line := range lines[:len(lines)-1] {
		assert.True(t, strings.HasPrefix(line, "["), "header %d must be a bracketed tag: %q", i, line)
		assert.True(t, strings.HasSuffix(line, "]"), "header %d must close its bracket: %q", i, line)
	}
}

func TestApplyAttachmentPrefixes_NoAttachmentsIsANoOp(t *testing.T) {
	assert.Equal(t, "问题", ApplyAttachmentPrefixes("问题", nil, nil, AttachmentPromptParts{}))
}

func TestHasAttachmentEntries(t *testing.T) {
	assert.False(t, HasAttachmentEntries(nil))
	assert.False(t, HasAttachmentEntries([]FileEntry{}))
	assert.True(t, HasAttachmentEntries([]FileEntry{{Path: "/a"}}))
}

// A quote-only message carries no file/image/dir, so the media-handling rules
// must not be injected for it. One real attachment alongside a quote still
// counts.
func TestHasAttachmentEntries_IgnoresQuoteOnly(t *testing.T) {
	assert.False(t, HasAttachmentEntries([]FileEntry{
		{Kind: "quote", ID: "q1", Text: "hello"},
	}), "a quote carries no file for the media rules to act on")

	assert.True(t, HasAttachmentEntries([]FileEntry{
		{Kind: "quote", ID: "q1", Text: "hello"},
		{Path: "/src/a.go"},
	}))
}

func TestClassifyAttachments_QuoteIsItsOwnBucket(t *testing.T) {
	entries := []FileEntry{
		{Path: "/src/a.go", Kind: "quote", ID: "q1", Text: "fmt.Println()", Note: "why?", Language: "go", StartLine: 3, EndLine: 3},
		{Path: "/src/b.go"},
	}
	parts := ClassifyAttachments(entries, nil)

	assert.Len(t, parts.Quotes, 1, "a quote is not a file label")
	assert.Equal(t, []string{"/src/b.go"}, parts.FileLabels)
	assert.Equal(t, QuotePrompt{
		Label: "/src/a.go", Language: "go", Note: "why?", Text: "fmt.Println()", StartLine: 3, EndLine: 3,
	}, parts.Quotes[0])
}

// The quote's Path is only a label and may legitimately equal an attached
// file's path. Checking excludePaths before the quote branch would silently
// drop the quoted text from the prompt — the single most damaging regression
// this feature can have.
func TestClassifyAttachments_QuoteSurvivesExcludePaths(t *testing.T) {
	entries := []FileEntry{{Path: "/src/a.go", Kind: "quote", ID: "q1", Text: "quoted body"}}
	parts := ClassifyAttachments(entries, map[string]struct{}{"/src/a.go": {}})

	assert.Len(t, parts.Quotes, 1, "a quote sharing a path with an attached file must still reach the prompt")
	assert.Empty(t, parts.FileLabels)
}

// A quote with empty text is still a quote: keying on Text would send the
// empty Path into filesystem validation and 404 the whole send.
func TestFileEntry_IsQuoteIgnoresEmptyText(t *testing.T) {
	assert.True(t, FileEntry{Kind: "quote"}.IsQuote())
	assert.False(t, FileEntry{Path: "/a"}.IsQuote())
	assert.False(t, FileEntry{Kind: "url", URL: "https://x"}.IsQuote())
}

func TestRenderQuoteBlock_MatchesFrontendFence(t *testing.T) {
	cases := []struct {
		name string
		q    QuotePrompt
		want string
	}{
		{
			"language and line range",
			QuotePrompt{Label: "/src/a.go", Language: "go", Text: "x := 1", StartLine: 10, EndLine: 20},
			"```go:/src/a.go:10-20\nx := 1\n```",
		},
		{
			"single line",
			QuotePrompt{Label: "/src/a.go", Language: "go", Text: "x := 1", StartLine: 10},
			"```go:/src/a.go:10\nx := 1\n```",
		},
		{
			"no language degrades to a bare colon",
			QuotePrompt{Label: "acme/widgets#7", Text: "body"},
			"```:acme/widgets#7\nbody\n```",
		},
		{
			"equal start and end collapse to one line",
			QuotePrompt{Label: "/a.ts", Language: "ts", Text: "b", StartLine: 4, EndLine: 4},
			"```ts:/a.ts:4\nb\n```",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, RenderQuoteBlock(tc.q))
		})
	}
}

func TestApplyAttachmentPrefixes_QuoteAppendedAfterUserText(t *testing.T) {
	parts := ClassifyAttachments([]FileEntry{
		{Path: "/src/a.go", Kind: "quote", ID: "q1", Text: "x := 1", Note: "why?", Language: "go", StartLine: 3, EndLine: 3},
	}, nil)

	got := ApplyAttachmentPrefixes("解释一下", nil, nil, parts)

	// Mirrors the pre-refactor buildMultiQuoteMessage ordering:
	// "prompt\n\nheader\nnote\n\nfence", with the quote's source header added.
	assert.Equal(t, "解释一下\n\n[Quoted from /src/a.go:3]\nwhy?\n\n```go:/src/a.go:3\nx := 1\n```", got)
}

func TestApplyAttachmentPrefixes_QuoteWithoutNote(t *testing.T) {
	parts := ClassifyAttachments([]FileEntry{
		{Path: "", Kind: "quote", ID: "q1", Text: "picked text"},
	}, nil)

	got := ApplyAttachmentPrefixes("问题", nil, nil, parts)

	assert.Equal(t, "问题\n\n[Quoted from ]\n\n```:\npicked text\n```", got,
		"an unlabelled quote still gets its header line")
}

// A whole-file / whole-issue quote references the object instead of inlining
// it, so it must emit the path (and address) and NO fence. An empty code block
// would misrepresent "reference this file" as "here is its (empty) content".
func TestApplyAttachmentPrefixes_WholeFileQuoteHasNoFence(t *testing.T) {
	parts := ClassifyAttachments([]FileEntry{
		{Path: "/proj/src/main.ts", Kind: "quote", ID: "q1", Note: "explain this file"},
	}, nil)

	got := ApplyAttachmentPrefixes("看看", nil, nil, parts)

	assert.Equal(t, "看看\n\n[Quoted from /proj/src/main.ts]\nexplain this file", got)
	assert.NotContains(t, got, "```", "a quote with no content must not render a fence")
}

// A forge quote must carry its ADDRESS: the label alone ("acme/widgets#7") does
// not let the AI reach the issue, and dropping it was a real regression in the
// handler's validatedQuoteEntry.
func TestApplyAttachmentPrefixes_ForgeQuoteCarriesItsAddress(t *testing.T) {
	parts := ClassifyAttachments([]FileEntry{
		{
			Path: "acme/widgets#7", Kind: "quote", ID: "q1", Note: "why did this fail?",
			URL: "https://github.com/acme/widgets/issues/7",
		},
	}, nil)

	got := ApplyAttachmentPrefixes("看看这个 issue", nil, nil, parts)

	assert.Equal(t,
		"看看这个 issue\n\n[Quoted from acme/widgets#7 (https://github.com/acme/widgets/issues/7)]\nwhy did this fail?",
		got)
}

// The whole header must stay on ONE line: the title stripper removes it with a
// strip-to-newline rule, so a wrapped header would leak its tail into the
// derived session title.
func TestApplyAttachmentPrefixes_QuoteHeaderIsOneLine(t *testing.T) {
	parts := ClassifyAttachments([]FileEntry{
		{
			Path: "/src/a.go", Kind: "quote", ID: "q1", Note: "note here",
			URL: "https://example.com/x", StartLine: 10, EndLine: 20,
		},
	}, nil)

	got := ApplyAttachmentPrefixes("问题", nil, nil, parts)
	lines := strings.Split(got, "\n")

	assert.True(t, strings.HasPrefix(lines[2], "[") && strings.HasSuffix(lines[2], "]"),
		"the header must be one bracketed line, got %q", lines[2])
	assert.Contains(t, lines[2], "/src/a.go:10-20")
	assert.Contains(t, lines[2], "https://example.com/x")
}

// A quote-only message has empty content; the renderer must not open with
// blank lines or the title stripper's trim becomes load-bearing.
func TestApplyAttachmentPrefixes_QuoteOnlyStartsWithHeader(t *testing.T) {
	parts := ClassifyAttachments([]FileEntry{
		{Path: "/a.ts", Kind: "quote", ID: "q1", Text: "body", Language: "ts"},
	}, nil)

	got := ApplyAttachmentPrefixes("", nil, nil, parts)

	assert.True(t, strings.HasPrefix(got, QuotePromptPrefix), "no leading blank lines, got %q", got)
	assert.NotContains(t, got, "\n\n[Quoted from", "the first quote must not be preceded by a blank line")
}

// The header must end with "]" even when the label carries line info AND an
// address, or the stripper's bracket-close rule would not match.
func TestQuoteHeader_ClosesItsBracket(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    QuotePrompt
		want string
	}{
		{"path only", QuotePrompt{Label: "/a.go"}, "[Quoted from /a.go]"},
		{"line range", QuotePrompt{Label: "/a.go", StartLine: 3, EndLine: 9}, "[Quoted from /a.go:3-9]"},
		{"single line", QuotePrompt{Label: "/a.go", StartLine: 3}, "[Quoted from /a.go:3]"},
		{"address", QuotePrompt{Label: "a/b#1", URL: "https://e.com/1"}, "[Quoted from a/b#1 (https://e.com/1)]"},
		{"line range and address", QuotePrompt{Label: "/a.go", StartLine: 3, EndLine: 9, URL: "https://e.com/1"}, "[Quoted from /a.go:3-9 (https://e.com/1)]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := quoteHeader(tc.q)
			assert.Equal(t, tc.want, got)
			assert.True(t, strings.HasSuffix(got, "]"), "header must close its bracket")
			assert.NotContains(t, got, "\n", "header must be a single line")
		})
	}
}

func TestQuotePromptPrefix_IsRegisteredAsStripRule(t *testing.T) {
	// The header literal is exported from model precisely so the handler's
	// strip rule cannot drift from it. This test pins the constant's shape;
	// the rule registration itself is asserted in internal/handler.
	assert.Equal(t, "[Quoted from ", QuotePromptPrefix)
	assert.True(t, strings.HasPrefix(QuotePromptPrefix, "["))
}
