package model

import (
	"fmt"
	"strings"
)

// This file owns the ONE classification of attachments into prompt prefixes.
//
// It lives in model because BOTH internal/handler and internal/service build
// prompts from []FileEntry, and they cannot import each other (handler already
// imports service, so service importing handler would be a cycle). Keeping a
// copy in each package is what let the two drift: one path dropped URL
// attachments entirely, the other rendered the URL's human label as a local
// file path. Any new prompt-building path must call these helpers rather than
// reimplementing the loop.

// FileEntryLabel returns a prompt label for a FileEntry, appending line info
// when present: "path", "path:10", or "path:10-20".
//
// Line info is what lets the AI focus on a region of a large file, so it must
// be preserved — the deprecated PathsFromFileEntries drops it.
func FileEntryLabel(f FileEntry) string {
	if f.StartLine > 0 && f.EndLine > 0 && f.StartLine != f.EndLine {
		return fmt.Sprintf("%s:%d-%d", f.Path, f.StartLine, f.EndLine)
	}
	if f.StartLine > 0 {
		return fmt.Sprintf("%s:%d", f.Path, f.StartLine)
	}
	return f.Path
}

// AttachmentPromptParts classifies attachments into the buckets the prompt
// prefixer needs.
type AttachmentPromptParts struct {
	// FileLabels are "path", "path:10" or "path:10-20".
	FileLabels []string
	// DirPaths are directories attached as context.
	DirPaths []string
	// URLs are external addresses (Kind == "url"). They are NOT filesystem
	// paths: prefixing one as [Current file]/[User uploaded] hands the AI a
	// path that does not exist. The address itself must still reach the
	// prompt, or the AI has no way to reach the referenced item.
	URLs []string
	// Quotes are quoted snippets (Kind == "quote"). They are RENDERED BLOCKS,
	// not paths: the quoted text and the user's annotation must reach the
	// prompt verbatim, or the AI sees an annotation about content it cannot
	// read. Kept in their own bucket so a quote's Path is never emitted as a
	// [User uploaded ...] file label.
	Quotes []QuotePrompt
}

// QuotePrompt is one quote entry reduced to what the prompt renderer needs.
type QuotePrompt struct {
	// Label identifies the source: "path", "path:10-20" for a file quote, or
	// the forge/chat label the client supplied. Empty for an unlabelled quote.
	Label string
	// Language is the fence info string's language prefix (may be empty).
	Language string
	// Note is the user's annotation.
	Note string
	// Text is the quoted content, verbatim.
	Text string
	// StartLine/EndLine are 1-based file lines, 0 when not applicable.
	StartLine int
	EndLine   int
}

// ClassifyAttachments splits entries into prompt buckets. excludePaths holds
// paths already carried by the legacy filePaths channel; those are skipped so
// the same path is not prefixed twice.
//
// A nil excludePaths is valid and means "exclude nothing".
func ClassifyAttachments(entries []FileEntry, excludePaths map[string]struct{}) AttachmentPromptParts {
	var parts AttachmentPromptParts
	for _, f := range entries {
		// Quotes are checked BEFORE excludePaths: a quote's Path is a label
		// that may coincide with an attached file's path, and skipping it
		// there would silently drop the quoted text from the prompt.
		if f.IsQuote() {
			parts.Quotes = append(parts.Quotes, QuotePrompt{
				Label:     f.Path,
				Language:  f.Language,
				Note:      f.Note,
				Text:      f.Text,
				StartLine: f.StartLine,
				EndLine:   f.EndLine,
			})
			continue
		}
		if f.IsURL() {
			parts.URLs = append(parts.URLs, f.URL)
			continue
		}
		if _, exists := excludePaths[f.Path]; exists {
			continue // already covered by the filePaths channel
		}
		if f.IsDir {
			parts.DirPaths = append(parts.DirPaths, f.Path)
		} else {
			parts.FileLabels = append(parts.FileLabels, FileEntryLabel(f))
		}
	}
	return parts
}

// ReferencedLinkPrefix is the machine header for an external-link attachment.
//
// Exported so the session-title stripper (internal/handler) and any test can
// reference the exact string instead of duplicating a literal that could drift.
const ReferencedLinkPrefix = "[Referenced external link: "

// QuotePromptPrefix is the machine header for a quoted-snippet attachment.
//
// Exported so the session-title stripper (internal/handler) can reference the
// exact string instead of duplicating a literal that could drift — a header
// with no strip rule would become the session title.
const QuotePromptPrefix = "[Quoted from "

// RenderQuoteBlock renders one quote as a fenced block whose header carries
// the language, source label and optional line range.
//
// The format is byte-identical to the frontend's buildQuoteBlock
// (web/src/utils/quoteQuestionUtils.ts), which is what the prompt contained
// before quotes became structured attachments. Keeping it identical is the
// whole point: the AI must see exactly what it saw when the fence lived in the
// message text.
func RenderQuoteBlock(q QuotePrompt) string {
	langPrefix := ":"
	if q.Language != "" {
		langPrefix = q.Language + ":"
	}
	lineSuffix := ""
	switch {
	case q.StartLine > 0 && q.EndLine > 0 && q.StartLine != q.EndLine:
		lineSuffix = fmt.Sprintf(":%d-%d", q.StartLine, q.EndLine)
	case q.StartLine > 0:
		lineSuffix = fmt.Sprintf(":%d", q.StartLine)
	}
	return fmt.Sprintf("```%s%s%s\n%s\n```", langPrefix, q.Label, lineSuffix, q.Text)
}

// ApplyAttachmentPrefixes prepends the attachment headers to the prompt.
//
// Every header is a single line so the session-title stripper can drop it with
// its stripToNewline rule; a new header MUST be registered in
// clientInjectedStripRules (internal/handler/session_resume.go) or it would be
// mistaken for the user's own words when deriving a session title.
//
// Quotes are APPENDED (not prepended): they are the subject of the message and
// the user's own words lead, matching the pre-refactor
// buildMultiQuoteMessage ordering ("prompt\n\nnote\n\nfence"). This is a
// deliberate unification — the composer flow used to put the block first.
func ApplyAttachmentPrefixes(prompt string, filePaths, dirPaths []string, parts AttachmentPromptParts) string {
	if len(filePaths) > 0 {
		prompt = fmt.Sprintf("[Current file: %s]\n%s", strings.Join(filePaths, ", "), prompt)
	}
	if len(dirPaths) > 0 {
		prompt = fmt.Sprintf("[Current directory: %s]\n%s", strings.Join(dirPaths, ", "), prompt)
	}
	if len(parts.FileLabels) > 0 {
		prompt = fmt.Sprintf("[User uploaded %d file(s): %s]\n%s", len(parts.FileLabels), strings.Join(parts.FileLabels, ", "), prompt)
	}
	if len(parts.DirPaths) > 0 {
		prompt = fmt.Sprintf("[Current directory: %s]\n%s", strings.Join(parts.DirPaths, ", "), prompt)
	}
	// Prepended last so the link leads the attachment block — it is the subject
	// of the message when the user quoted an issue/PR.
	if len(parts.URLs) > 0 {
		prompt = fmt.Sprintf("%s%s]\n%s", ReferencedLinkPrefix, strings.Join(parts.URLs, ", "), prompt)
	}
	// Appended after everything, including the user's own words.
	if len(parts.Quotes) > 0 {
		var b strings.Builder
		b.WriteString(prompt)
		for _, q := range parts.Quotes {
			// Separate only when something precedes it — a quote-only message
			// must not open with blank lines, or the title stripper's trim
			// would be doing load-bearing work it was not written for.
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(QuotePromptPrefix)
			b.WriteString(q.Label)
			b.WriteString("]\n")
			if note := strings.TrimSpace(q.Note); note != "" {
				b.WriteString(note)
				b.WriteString("\n\n")
			}
			b.WriteString(RenderQuoteBlock(q))
		}
		prompt = b.String()
	}
	return prompt
}

// HasAttachmentEntries reports whether any attachment entry was supplied.
// It is the single definition of "this message carries attachments", used to
// gate the media-handling rules injection.
//
// Quote-only messages are NOT counted: they carry no file, image or directory,
// so injecting the media rules would be noise. This mirrors what the prompt
// actually contains (a quote's text is inlined, nothing for the AI to open).
func HasAttachmentEntries(entries []FileEntry) bool {
	for _, f := range entries {
		if !f.IsQuote() {
			return true
		}
	}
	return false
}
