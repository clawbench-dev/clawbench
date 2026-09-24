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
	// Text is the quoted content, verbatim. EMPTY for a whole-file or
	// whole-issue quote: those reference the object rather than inlining it, so
	// the prompt carries the path/address and the AI reads it itself. A quote
	// with empty Text must NOT render a fence — an empty code block is noise
	// and would misrepresent "reference this file" as "here is its content".
	Text string
	// URL is the external address for a forge-sourced quote (empty otherwise).
	// Without it the prompt names an issue/PR the AI cannot reach.
	URL string
	// StartLine/EndLine are 1-based file lines, 0 when not applicable.
	StartLine int
	EndLine   int
	// Source locators. These are the machine keys that let the AI (and the
	// client's jump handler) find the origin. They are emitted in the header's
	// parenthetical group; the human-readable name stays in Label.
	CommitSHA   string
	TaskID      int64
	SessionID   string
	MessageID   int64
	ExecutionID string
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
				Label:       f.Path,
				Language:    f.Language,
				Note:        f.Note,
				Text:        f.Text,
				URL:         f.URL,
				StartLine:   f.StartLine,
				EndLine:     f.EndLine,
				CommitSHA:   f.CommitSHA,
				TaskID:      f.TaskID,
				SessionID:   f.SessionID,
				MessageID:   f.MessageID,
				ExecutionID: f.ExecutionID,
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
// It opens a numbered envelope, e.g. "[Quote 1/3] /src/a.go:3". The number and
// total are what make consecutive quotes visually separable: blank lines alone
// were used at every structural boundary (between quotes, between a header and
// its note, between a note and the content), so the AI could not tell from the
// layout where one quote ended and the next began.
//
// Exported so the session-title stripper (internal/handler) can reference the
// exact string instead of duplicating a literal that could drift — a header
// with no strip rule would become the session title.
const QuotePromptPrefix = "[Quote "

// QuoteNotePrefix labels a quote's annotation.
//
// The annotation used to be emitted as a bare line, which read as a stray
// sentence — or as part of the quoted content. The label states that it belongs
// to the quote above it.
const QuoteNotePrefix = "[Note] "

// quoteSourceKeys renders the machine-readable source locators for a quote, in
// a stable order, as "key: value" pairs. Empty when the quote has none.
//
// These are what let the AI reach the origin rather than merely knowing a
// human-readable name: "每日构建" is not addressable, "task: 12" is. They are
// the same keys the client uses to jump, so the AI and the UI agree on what
// identifies a source.
func quoteSourceKeys(q QuotePrompt) []string {
	var keys []string
	if q.CommitSHA != "" {
		keys = append(keys, "commit: "+q.CommitSHA)
	}
	if q.TaskID != 0 {
		keys = append(keys, fmt.Sprintf("task: %d", q.TaskID))
	}
	if q.SessionID != "" {
		keys = append(keys, "session: "+q.SessionID)
	}
	if q.MessageID != 0 {
		keys = append(keys, fmt.Sprintf("message: %d", q.MessageID))
	}
	if q.ExecutionID != "" {
		keys = append(keys, "execution: "+q.ExecutionID)
	}
	return keys
}

// quoteHeader renders a quote's header line, e.g.
//
//	[Quote 1/3] /src/a.go:10-20
//	[Quote 2/3] acme/widgets#7 (https://github.com/acme/widgets/issues/7)
//	[Quote 1/2] 每日构建 (#12) (task: 12)
//	[Quote 2/2] 修复登录 (session: sess-abc, message: 42)
//
// index/total number the envelope. They are what separates consecutive quotes
// now that the same blank-line separator is used at every structural boundary
// (between quotes, between a header and its note, between a note and the
// content) — without them the AI had no cue for where one quote ended.
//
// The address is included for forge quotes: without it the AI knows an
// issue/PR was referenced but has no way to reach it. Source locators (commit,
// task, session, message, execution) join it in the same parenthetical group,
// comma-separated, so the AI can address the origin too.
//
// Everything stays on ONE line so the session-title stripper's stripToNewline
// rule consumes the whole header (a second line would leak into the derived
// title).
//
// The parenthetical group is emitted ONLY when it has content, so a plain file
// quote stays as short as it can be.
func quoteHeader(q QuotePrompt, index, total int) string {
	label := q.Label
	if q.StartLine > 0 && q.EndLine > 0 && q.StartLine != q.EndLine {
		label = fmt.Sprintf("%s:%d-%d", q.Label, q.StartLine, q.EndLine)
	} else if q.StartLine > 0 {
		label = fmt.Sprintf("%s:%d", q.Label, q.StartLine)
	}
	var detail []string
	if q.URL != "" {
		detail = append(detail, q.URL)
	}
	detail = append(detail, quoteSourceKeys(q)...)
	if len(detail) > 0 {
		label = fmt.Sprintf("%s (%s)", label, strings.Join(detail, ", "))
	}
	return fmt.Sprintf("%s%d/%d] %s", QuotePromptPrefix, index, total, label)
}

// RenderQuoteBlock renders one quote's content as a fenced block whose fence
// info string carries the language, source label and optional line range.
//
// This is the only place the quoted CONTENT is rendered, and the fence is
// deliberately left alone by the envelope change: it is the channel the AI
// reads the content through, and its shape is depended on elsewhere.
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
	//
	// Each quote is wrapped in a NUMBERED envelope ("[Quote i/n] …") and its
	// annotation is labelled ("[Note] …"). Both exist because the same blank
	// line separates every structural boundary here — between quotes, between a
	// header and its note, and between a note and the content — so the layout
	// alone gave no cue for where one quote ended and the next began, and a bare
	// annotation line read as either a stray sentence or part of the content.
	if len(parts.Quotes) > 0 {
		var b strings.Builder
		b.WriteString(prompt)
		total := len(parts.Quotes)
		for i, q := range parts.Quotes {
			// Separate only when something precedes it — a quote-only message
			// must not open with blank lines, or the title stripper's trim
			// would be doing load-bearing work it was not written for.
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(quoteHeader(q, i+1, total))
			if note := strings.TrimSpace(q.Note); note != "" {
				b.WriteString("\n")
				b.WriteString(QuoteNotePrefix)
				b.WriteString(note)
			}
			// A fence only when there IS quoted content. A whole-file or
			// whole-issue quote references the object instead of inlining it,
			// so it emits the path/address above and nothing else — an empty
			// fence would be noise and would misrepresent "reference this file"
			// as "here is its (empty) content".
			if q.Text != "" {
				b.WriteString("\n\n")
				b.WriteString(RenderQuoteBlock(q))
			}
		}
		prompt = b.String()
	}
	return prompt
}

// HasAttachmentEntries reports whether any attachment entry was supplied.
// It is the single definition of "this message carries attachments", used to
// gate the media-handling rules injection.
//
// Quote-only messages are NOT counted. A quote does reference a file or an
// issue/PR, but it is an explicit "look at this" from the user, whereas the
// media rules exist to stop the AI from reading media files on an ambiguous
// request — so injecting them here would contradict the quote itself.
func HasAttachmentEntries(entries []FileEntry) bool {
	for _, f := range entries {
		if !f.IsQuote() {
			return true
		}
	}
	return false
}
