// Package grouprouting parses a group-chat host agent's routing decision from
// its assistant text.
//
// The host is an ordinary agent, so its routing decision is expressed as a
// structured tag inside its natural-language output:
//
//	<clawbench-speaker>A,B</clawbench-speaker> 请 B 回应 A 的质疑
//	<clawbench-group-end/>
//
// Contract (mirrors internal/askquestion): detect-then-parse; an unparseable
// tag is NEVER stripped — Raw is returned so the caller keeps the original text
// visible. Parse failure degrades to round-robin at the orchestrator; it never
// loses content.
//
// The package deliberately imports nothing from this module, so every layer can
// depend on it without an import cycle. The TypeScript mirror lives in
// web/src/utils/groupRouting.ts and the two are kept in sync by
// testdata/parity_corpus.json.
package grouprouting

import (
	"regexp"
	"strings"
)

// Result is the outcome of parsing a host message.
type Result struct {
	// Found reports whether a speaker tag was located and understood.
	Found bool
	// Speakers is the ordered list of named members the host wants to speak.
	// Non-empty only when Found is true.
	Speakers []string
	// Instruction is the host's directive to those members: the text following
	// the speaker tag, with any end tag removed (a message may carry both).
	// Empty when absent. Well-formed BCC spans are removed first, so a private
	// note can never leak into the public directive.
	Instruction string
	// Before is the text preceding the speaker tag, trimmed. It is the host's
	// background context and is consumed by the injection layer (design
	// decision #68, approach A) to render a member's context without repeating
	// the routing tag or the directive. It is a positional slice, not a parse
	// result: it is populated whenever the tag is located, even when the tag
	// itself is malformed (Found=false). Callers that require a valid routing
	// decision must gate on Found; those that only need the background may read
	// it regardless. Parse never strips — this package does not mutate input.
	// Well-formed BCC spans are removed first, so a private note placed ahead
	// of the tag never leaks into the shared background either.
	Before string
	// Bcc holds the host's private notes to individual members, in the order
	// they appeared. Each entry names its targets (comma-separated, trimmed)
	// and carries the note's content. A note is recognized only when it is
	// well-formed (a targets attribute with at least one non-empty name); a
	// malformed one is NOT parsed and NOT stripped — the same
	// detect-then-parse / never-lose-content contract the other tags follow.
	// Bcc is independent of Found: a message may carry only a note and no
	// speaker tag.
	Bcc []BccEntry
	// End reports whether the host signalled the discussion is over.
	End bool
	// Raw is the matched tag text, retained for logging. Callers keep the
	// original message text regardless (this package never mutates input).
	Raw string
}

// BccEntry is one private note from the host to a subset of the round's named
// speakers: <clawbench-bcc targets="A,B">content</clawbench-bcc>.
type BccEntry struct {
	// Targets are the named members the note is addressed to, in order,
	// trimmed, empties dropped.
	Targets []string
	// Content is the note's inner text, trimmed.
	Content string
}

var (
	reSpeaker = regexp.MustCompile(`(?s)<clawbench-speaker>(.*?)</clawbench-speaker>`)
	reEnd     = regexp.MustCompile(`<clawbench-group-end[\s\p{Z}]*/>`)
	// reBcc matches a well-formed private note (the DISPLAY contract). The
	// targets attribute must use double quotes; single-quoted / attribute-less
	// forms are treated as malformed and left untouched (never stripped).
	// The whitespace class is [\s\p{Z}] — Go's \s alone is ASCII-only, while
	// JS's \s includes Unicode spaces (NBSP, U+3000). Without \p{Z} the two
	// sides would disagree on a CJK-width space: the note would parse on one
	// side and not the other (the injection side is fail-closed, but the card
	// would silently not render). Parity is pinned by the corpus.
	reBcc = regexp.MustCompile(`(?s)<clawbench-bcc[\s\p{Z}]+targets[\s\p{Z}]*=[\s\p{Z}]*"([^"]*)"[\s\p{Z}]*>(.*?)</clawbench-bcc>`)
	// reBccSpanAny matches ANY bcc-like span, well-formed or not, for the
	// fail-closed INJECTION contract (see StripBccSpans). [^>]* absorbs any
	// attribute shape (single quotes, extra attrs, no attrs); the body is
	// non-greedy so the first close tag ends it.
	reBccSpanAny = regexp.MustCompile(`(?s)<clawbench-bcc\b[^>]*>.*?</clawbench-bcc>`)
	// reBccCloseAny matches a stray closing tag left behind after peeling a
	// malformed/nested span.
	reBccCloseAny = regexp.MustCompile(`</clawbench-bcc>`)
)

// StripEndTag removes the end-signal tag from text, returning the text
// unchanged when there is none. It exists because the end tag is internal
// protocol (like the speaker tag) and must not leak into a member's injected
// context — including a message that carries ONLY the end tag, where Parse
// reports Found=false (no speaker tag) and callers that gate on Found would
// otherwise pass it through verbatim (decision #67).
func StripEndTag(text string) string {
	if !reEnd.MatchString(text) {
		return text
	}
	return strings.TrimSpace(reEnd.ReplaceAllString(text, ""))
}

// Parse locates the host's routing decision in an assistant message.
//
// Well-formed BCC notes are extracted FIRST and removed from the text before
// the speaker/end/before/instruction parsing runs. That order is load-bearing:
// a note placed after the speaker tag would otherwise become part of
// Instruction (the directive every addressed member receives), and one placed
// before it would become part of Before (the shared background). Extracting
// first is what keeps a private note private.
func Parse(text string) Result {
	var res Result

	// 1. Extract well-formed notes for DISPLAY (fail-open: a malformed note is
	//    kept verbatim so nothing is lost).
	res.Bcc = extractBccEntries(text)

	// 2. Everything else runs on the FAIL-CLOSED text: every bcc-like span is
	//    removed regardless of shape, so a note can never leak into Before /
	//    Instruction / End — the injection layer is the security boundary and
	//    must not depend on the host formatting the tag correctly.
	cleaned := StripBccSpans(text)

	if m := reEnd.FindString(cleaned); m != "" {
		res.End = true
		res.Raw = m
	}

	loc := reSpeaker.FindStringSubmatchIndex(cleaned)
	if loc == nil {
		return res
	}
	inner := cleaned[loc[2]:loc[3]]
	res.Raw = cleaned[loc[0]:loc[1]]
	// Before = the text ahead of the tag (positional slice, always available
	// once the tag is located — even for a malformed payload).
	res.Before = strings.TrimSpace(cleaned[:loc[0]])

	speakers := splitSpeakers(inner)
	if len(speakers) == 0 {
		// Malformed (empty payload): do not claim Found, keep Raw for logs.
		res.End = reEnd.MatchString(cleaned)
		return res
	}
	res.Found = true
	res.Speakers = speakers

	// Instruction = text after the closing speaker tag, trimmed, with any end
	// tag removed (a message may carry both; the end tag must not leak into the
	// instruction handed to a member or rendered by the card).
	after := reEnd.ReplaceAllString(cleaned[loc[1]:], "")
	res.Instruction = strings.TrimSpace(after)
	return res
}

// extractBccEntries parses the WELL-FORMED notes for display, in order. It is
// the fail-open half of the contract: an unrecognized tag is not parsed (and
// not removed) so its text stays visible.
func extractBccEntries(text string) []BccEntry {
	matches := reBcc.FindAllStringSubmatchIndex(text, -1)
	if matches == nil {
		return nil
	}
	entries := make([]BccEntry, 0, len(matches))
	for _, m := range matches {
		// m: [start, end, targetsStart, targetsEnd, contentStart, contentEnd]
		targets := splitSpeakers(text[m[2]:m[3]])
		if len(targets) == 0 {
			continue // malformed: not a note
		}
		entries = append(entries, BccEntry{
			Targets: targets,
			Content: strings.TrimSpace(text[m[4]:m[5]]),
		})
	}
	if len(entries) == 0 {
		return nil
	}
	return entries
}

// extractBcc scans text for well-formed private notes, returning the text with
// those spans removed and the parsed entries in order. The removal is a plain
// deletion (like StripEndTag): the surrounding whitespace is preserved as-is.
//
// This is the DISPLAY-side stripper (fail-open): only well-formed notes are
// removed; a malformed one is left untouched. The injection layer must use
// StripBccSpans instead (fail-closed).
func extractBcc(text string) (string, []BccEntry) {
	matches := reBcc.FindAllStringSubmatchIndex(text, -1)
	entries := extractBccEntries(text)
	if len(entries) == 0 {
		return text, nil
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		targets := splitSpeakers(text[m[2]:m[3]])
		if len(targets) == 0 {
			continue // malformed: leave the span in place
		}
		b.WriteString(text[last:m[0]])
		last = m[1]
	}
	b.WriteString(text[last:])
	return b.String(), entries
}

// StripBccTags removes well-formed private notes from text, returning the text
// unchanged when there is none. It mirrors StripEndTag: it exists so a note can
// be kept out of a member's injected context even on the fallback path where no
// speaker tag was found. Malformed notes are left untouched (fail-open).
//
// For the injection boundary prefer StripBccSpans, which removes every shape.
func StripBccTags(text string) string {
	cleaned, entries := extractBcc(text)
	if len(entries) == 0 {
		return text
	}
	return strings.TrimSpace(cleaned)
}

// StripBccSpans removes EVERY bcc-like span from text — well-formed, malformed
// (single quotes, extra/absent attributes), nested, or unclosed — and returns
// the remainder trimmed. This is the fail-closed primitive for the INJECTION
// boundary: a private note must never reach a non-target member, and the host
// is an LLM whose tag formatting cannot be trusted.
//
// Two passes: closed spans are peeled in a loop (the non-greedy body ends at
// the first close tag, so a nested <bcc>…<bcc>inner</bcc>…</bcc> needs another
// pass); then any UNCLOSED opening tag — and everything after it, since a
// truncated/streaming note's tail is just as private — is dropped.
//
// Text with no bcc-like tag is returned unchanged (trimmed).
func StripBccSpans(text string) string {
	if !strings.Contains(text, "<clawbench-bcc") {
		return strings.TrimSpace(text)
	}
	cleaned := text
	for {
		next := reBccSpanAny.ReplaceAllString(cleaned, "")
		if next == cleaned {
			break
		}
		cleaned = next
	}
	// Any opening tag still present is unclosed (no matching close tag was left
	// by the loop): drop it and its tail.
	if idx := strings.Index(cleaned, "<clawbench-bcc"); idx >= 0 {
		cleaned = cleaned[:idx]
	}
	// A stray close tag (from a nested/malformed span the loop could not pair)
	// is protocol residue too.
	cleaned = reBccCloseAny.ReplaceAllString(cleaned, "")
	return strings.TrimSpace(cleaned)
}

// splitSpeakers splits a comma-separated speaker list, trimming each name and
// dropping empties. Order is preserved.
func splitSpeakers(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if name := strings.TrimSpace(p); name != "" {
			out = append(out, name)
		}
	}
	return out
}
