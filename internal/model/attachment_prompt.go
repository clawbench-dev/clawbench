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
}

// ClassifyAttachments splits entries into prompt buckets. excludePaths holds
// paths already carried by the legacy filePaths channel; those are skipped so
// the same path is not prefixed twice.
//
// A nil excludePaths is valid and means "exclude nothing".
func ClassifyAttachments(entries []FileEntry, excludePaths map[string]struct{}) AttachmentPromptParts {
	var parts AttachmentPromptParts
	for _, f := range entries {
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

// ApplyAttachmentPrefixes prepends the attachment headers to the prompt.
//
// Every header is a single line so the session-title stripper can drop it with
// its stripToNewline rule; a new header MUST be registered in
// clientInjectedStripRules (internal/handler/session_resume.go) or it would be
// mistaken for the user's own words when deriving a session title.
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
	return prompt
}

// HasAttachmentEntries reports whether any attachment entry was supplied.
// It is the single definition of "this message carries attachments", used to
// gate the media-handling rules injection.
func HasAttachmentEntries(entries []FileEntry) bool { return len(entries) > 0 }
