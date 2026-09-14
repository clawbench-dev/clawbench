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
