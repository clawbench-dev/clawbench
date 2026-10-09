// Package grouprouting parses a group-chat agent's routing/mention decision
// from its assistant text.
//
// Both group-chat modes express themselves through ONE unified tag:
//
//		<clawbench-mention targets="A,B">给 A、B 的内容</clawbench-mention>
//		<clawbench-mention targets="C" private>只有 C 能看到的密送</clawbench-mention>
//		<clawbench-group-end/>
//
//	  - HOST mode: the host names the next speakers (targets) and hands them a
//	    directive (the tag body).
//	  - FREE mode: any member @s the member(s) it wants to hand the floor to; the
//	    tag body is what it says to them.
//
// The `private` attribute marks a note that only its targets may see (the old
// <clawbench-bcc> concept, folded into this tag). The tag body is the CONTENT
// (it used to live AFTER the tag in the old <clawbench-speaker> design).
//
// Contract (mirrors internal/askquestion): detect-then-parse; an unparseable
// tag is NEVER stripped — Raw is returned so the caller keeps the original text
// visible. Parse failure degrades at the orchestrator; it never loses content.
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

// MentionEntry is one parsed <clawbench-mention> tag.
type MentionEntry struct {
	// Targets are the named members the tag addresses, in order, trimmed,
	// empties dropped.
	Targets []string
	// Content is the tag body, trimmed. For a public mention it is the
	// directive/utterance handed to the targets; for a private one it is the
	// note only the targets may see.
	Content string
	// Private reports whether the tag carried the `private` attribute.
	Private bool
	// Mode is the delivery mode for this tag's targets: ModeParallel when the
	// tag carried `mode="parallel"`, else ModeSequential. It only affects
	// PUBLIC mentions (a private note's delivery is governed by the pending-BCC
	// contract, not by this field). An unknown/absent mode value is
	// ModeSequential and does NOT make the tag malformed.
	Mode string
}

// Delivery modes for a mention tag (the `mode` attribute).
const (
	// ModeSequential means the tag's targets speak one after another, each
	// seeing the previous speaker's output. This is the default and preserves
	// pre-existing behavior for untagged mentions.
	ModeSequential = "sequential"
	// ModeParallel means the tag's targets speak CONCURRENTLY and must not see
	// each other's output this round (the "simultaneous, mutually
	// non-referencing" semantics). See the design's parallel-speaking section.
	ModeParallel = "parallel"
)

// MentionGroup is one public mention tag flattened for the orchestrator: the
// members it names plus how they should speak. It is the source of truth for
// routing (Result.Speakers is derived from it), because it preserves the
// PER-TAG boundary that the flat Speakers list loses — and `mode` is a per-tag
// property.
type MentionGroup struct {
	// Members are the group's targets, in order, trimmed, empties dropped, and
	// de-duplicated ACROSS groups (a member appears in at most one group — the
	// first that names it).
	Members []string `json:"members"`
	// Parallel reports whether this group speaks concurrently.
	Parallel bool `json:"parallel"`
	// Instruction is this tag's body (the directive handed to its members).
	Instruction string `json:"instruction"`
}

// BccEntry is one private note from a speaker to a subset of the named
// participants. It is the display shape of a `private` mention.
type BccEntry struct {
	// Targets are the named members the note is addressed to, in order,
	// trimmed, empties dropped.
	Targets []string
	// Content is the note's inner text, trimmed.
	Content string
}

// Result is the outcome of parsing an agent message.
type Result struct {
	// Found reports whether at least one well-formed mention tag was located.
	Found bool
	// Mentions is every well-formed mention tag in the order it appeared,
	// public and private interleaved.
	Mentions []MentionEntry
	// Groups is the ordered list of PUBLIC mention tags, each flattened to its
	// members + mode + instruction. It is the source of truth for routing: the
	// orchestrator iterates groups so it can run a `parallel` group
	// concurrently and a `sequential` group one-at-a-time. Groups whose members
	// are all already claimed by an earlier group are dropped (they would be
	// empty). Speakers is derived from Groups.
	Groups []MentionGroup
	// Speakers is the ordered, de-duplicated list of targets across the PUBLIC
	// mentions (== the concatenation of Groups' members). Non-empty only when
	// Found is true. It is the host orchestrator's routing list and the free
	// orchestrator's relay list, for callers that do not need grouping.
	Speakers []string
	// Instruction is the PUBLIC content: every public mention's body joined by
	// a blank line, trimmed. It is the directive handed to the named members
	// (empty when there is none).
	Instruction string
	// Bcc holds the private notes, in the order they appeared.
	Bcc []BccEntry
	// Before is the text OUTSIDE every well-formed mention tag that precedes the
	// first tag, trimmed. It is the speaker's background context. It is a
	// positional slice, not a parse result: it is populated whenever a tag is
	// located, even when the tag itself is malformed. Callers that require a
	// valid routing decision must gate on Found. Parse never strips.
	Before string
	// After is the text outside every well-formed mention tag that follows the
	// last tag, trimmed (empty when there is none).
	After string
	// End reports whether the discussion is over (<clawbench-group-end/>).
	End bool
	// Raw is the matched tag text, retained for logging. Callers keep the
	// original message text regardless (this package never mutates input).
	Raw string
}

var (
	// reMention matches a well-formed mention tag: a targets attribute plus an
	// optional private attribute, then a body. The attribute soup is captured
	// raw and parsed by parseMentionAttrs so single-quoted / extra-attribute /
	// uppercase forms can be recognized as malformed consistently on both sides.
	reMention = regexp.MustCompile(`(?s)<clawbench-mention\b([^>]*)>(.*?)</clawbench-mention>`)
	// reMentionSpanAny matches ANY mention-like span, well-formed or not, for the
	// fail-closed INJECTION contract (see StripProtocolTags). [^>]* absorbs any
	// attribute shape; the body is non-greedy so the first close tag ends it.
	reMentionSpanAny = regexp.MustCompile(`(?s)<clawbench-mention\b[^>]*>.*?</clawbench-mention>`)
	// reMentionCloseAny matches a stray closing tag left behind after peeling a
	// malformed/nested span.
	reMentionCloseAny = regexp.MustCompile(`</clawbench-mention>`)
	// reTargets extracts the double-quoted targets attribute (the DISPLAY
	// contract: single-quoted / attribute-less / differently-cased forms are
	// malformed and left untouched, never stripped). Case-sensitive, matching
	// the old <clawbench-bcc targets=...> display contract. The leading
	// `(?:^|[\s\p{Z}])` anchors the attribute NAME to a real attribute boundary
	// so `data-targets="x"` is not mistaken for `targets` (`\b` alone matches
	// after the `-` of a hyphenated attribute name).
	reTargets = regexp.MustCompile(`(?:^|[\s\p{Z}])targets[\s\p{Z}]*=[\s\p{Z}]*"([^"]*)"`)
	// rePrivate detects the boolean `private` attribute (case-sensitive),
	// anchored to an attribute boundary for the same reason as reTargets.
	rePrivate = regexp.MustCompile(`(?:^|[\s\p{Z}])private(?:[\s\p{Z}]|$)`)
	// reMode extracts the double-quoted `mode` attribute value (the display
	// contract mirrors reTargets: only the double-quoted form is recognized).
	// An unknown value falls back to sequential; it never makes the tag
	// malformed.
	reMode = regexp.MustCompile(`(?:^|[\s\p{Z}])mode[\s\p{Z}]*=[\s\p{Z}]*"([^"]*)"`)
	// reEnd matches the discussion-end signal. The whitespace class is
	// [\s\p{Z}] — Go's \s alone is ASCII-only, while JS's \s includes Unicode
	// spaces (NBSP, U+3000). Without \p{Z} the two sides would disagree on a
	// CJK-width space. Parity is pinned by the corpus.
	reEnd = regexp.MustCompile(`<clawbench-group-end[\s\p{Z}]*/>`)
)

// parseMentionAttrs extracts (targets, private, mode) from a tag's raw
// attribute string. ok is false when there is no well-formed double-quoted
// targets attribute or it is empty — such a tag is malformed and NOT parsed
// (the fail-open display contract: its text stays visible). mode is
// ModeParallel only for the exact value `parallel`; any other value (including
// absent) is ModeSequential and never affects ok.
func parseMentionAttrs(attrs string) (targets []string, private bool, mode string, ok bool) {
	m := reTargets.FindStringSubmatch(attrs)
	if m == nil {
		return nil, false, "", false
	}
	targets = splitNames(m[1])
	if len(targets) == 0 {
		return nil, false, "", false
	}
	private = rePrivate.MatchString(attrs)
	mode = ModeSequential
	if mm := reMode.FindStringSubmatch(attrs); len(mm) > 1 && strings.EqualFold(strings.TrimSpace(mm[1]), ModeParallel) {
		mode = ModeParallel
	}
	return targets, private, mode, true
}

// Parse locates every mention tag in an agent message and the end signal.
//
// A well-formed mention (a targets attribute with at least one non-empty name)
// is parsed for display; a malformed one is NOT parsed and NOT stripped (the
// detect-then-parse / never-lose-content contract). Parse never mutates input.
func Parse(text string) Result {
	var res Result

	// Locate every well-formed mention for display, in order.
	matches := reMention.FindAllStringSubmatchIndex(text, -1)
	firstPublicStart, lastPublicEnd := -1, -1
	for _, m := range matches {
		// m: [start, end, attrsStart, attrsEnd, bodyStart, bodyEnd]
		targets, private, mode, ok := parseMentionAttrs(text[m[2]:m[3]])
		if !ok {
			continue // malformed: not a mention, leave the span in place
		}
		res.Mentions = append(res.Mentions, MentionEntry{
			Targets: targets,
			Content: strings.TrimSpace(text[m[4]:m[5]]),
			Private: private,
			Mode:    mode,
		})
		res.Raw = text[m[0]:m[1]]
		if !private {
			if firstPublicStart < 0 {
				firstPublicStart = m[0]
			}
			lastPublicEnd = m[1]
		}
	}

	// Before / After are the prose surrounding the mentions (the shared
	// background and any trailing prose). Before is sliced around the first
	// PUBLIC mention, falling back to the first mention-like span when there is
	// no public one (so a malformed tag still delimits it). Every mention span
	// is stripped from the slice, so a private note placed ahead of a public
	// mention never leaks into Before. Positional slices, populated whenever a
	// mention-like span is located.
	res.Before, res.After = surroundingProse(text, firstPublicStart, lastPublicEnd)

	// End is computed on the text OUTSIDE every mention span: an end tag inside
	// a private note must not end the discussion.
	res.End = reEnd.MatchString(stripAllMentionSpans(text))
	if len(res.Mentions) == 0 {
		if res.End {
			res.Raw = reEnd.FindString(stripAllMentionSpans(text))
		}
		return res
	}

	res.Found = true
	res.deriveRoutingView()
	return res
}

// surroundingProse returns the trimmed text outside every mention span that
// precedes the first public mention (Before) and follows the last one (After).
// When there is no public mention, firstPublicStart falls back to the first
// mention-like span so a malformed tag still delimits Before.
func surroundingProse(text string, firstPublicStart, lastPublicEnd int) (before, after string) {
	if firstPublicStart < 0 {
		if span := reMentionSpanAny.FindStringIndex(text); span != nil {
			firstPublicStart = span[0]
		}
	}
	if firstPublicStart >= 0 {
		before = strings.TrimSpace(StripEndTag(stripAllMentionSpans(text[:firstPublicStart])))
	}
	if lastPublicEnd >= 0 {
		after = strings.TrimSpace(StripEndTag(stripAllMentionSpans(text[lastPublicEnd:])))
	}
	return before, after
}

// deriveRoutingView fills Bcc, Groups, Speakers and Instruction from Mentions.
// Groups is the source of truth: it preserves the PER-TAG boundary (needed
// because `mode` is a per-tag property) and the per-tag instruction. Speakers
// is the flat de-duplicated concatenation of the groups' members, kept for
// callers that do not care about grouping.
//
// Cross-group de-duplication: a member named by an earlier group is not
// re-listed in a later one (it would otherwise speak twice in one round). A
// group left with no unclaimed members is dropped (empty group).
func (r *Result) deriveRoutingView() {
	claimed := map[string]bool{}
	for _, e := range r.Mentions {
		if e.Private {
			r.Bcc = append(r.Bcc, BccEntry{Targets: e.Targets, Content: e.Content})
			continue
		}
		group := MentionGroup{
			Parallel:    e.Mode == ModeParallel,
			Instruction: e.Content,
		}
		for _, t := range e.Targets {
			if claimed[t] {
				continue
			}
			claimed[t] = true
			group.Members = append(group.Members, t)
			r.Speakers = append(r.Speakers, t)
		}
		if len(group.Members) > 0 {
			r.Groups = append(r.Groups, group)
		}
		if e.Content != "" {
			if r.Instruction != "" {
				r.Instruction += "\n\n"
			}
			r.Instruction += e.Content
		}
	}
}

// StripEndTag removes the end-signal tag from text, returning the text
// unchanged when there is none. It exists because the end tag is internal
// protocol and must not leak into a member's injected context.
func StripEndTag(text string) string {
	if !reEnd.MatchString(text) {
		return text
	}
	return strings.TrimSpace(reEnd.ReplaceAllString(text, ""))
}

// StripProtocolTags removes every ClawBench protocol tag from text while keeping
// the PUBLIC prose: public mention tags are unwrapped (their body survives),
// private mentions are removed entirely (fail-closed — a private note must never
// reach a non-target), and the end signal is dropped.
//
// It is what a group MEMBER should see of the HOST's speech in host mode: the
// host's rules and public directives survive, but the tags and every private
// note do not, so a member cannot imitate the routing format (the "everyone
// declares itself the chair" incident) and cannot read someone else's note.
//
// It is ALSO the fail-closed primitive for the reading-summary / TTS /
// push-notification / quote boundaries: those texts leave the device or are
// spoken aloud, so a private note would leak. Same rule, same function.
//
// The text is NOT re-parsed: a message may legitimately contain several mention
// tags, and each must be handled without consuming the text after it.
func StripProtocolTags(text string) string {
	cleaned := replaceMentionSpans(text, func(entry *MentionEntry) string {
		if entry == nil || entry.Private {
			return "" // malformed or private: drop the whole span
		}
		return entry.Content
	})
	return strings.TrimSpace(StripEndTag(cleaned))
}

// replaceMentionSpans rewrites every mention-like span (well-formed or not) in
// text using fn: fn receives the parsed entry (nil when the span is malformed)
// and returns the replacement text. Malformed spans are passed to fn with nil so
// the caller can decide (the injection boundary drops them).
//
// Fail-closed: an UNCLOSED opening tag and its tail are dropped, and a stray
// close tag is removed. Go has no display caller (Parse reads mentions directly,
// not through this function), so it is always fail-closed here — matching the
// TS side's stripGroupProtocolTags. The stray-close removal runs even when there
// is no opening tag (a lone `</clawbench-mention>` is protocol residue too),
// which is why the early guard only skips the rewrite loop.
func replaceMentionSpans(text string, fn func(*MentionEntry) string) string {
	cleaned := text
	if strings.Contains(text, "<clawbench-mention") {
		// Two passes for nested spans (the non-greedy body ends at the first
		// close tag, so a nested tag needs another pass).
		for {
			next := reMentionSpanAny.ReplaceAllStringFunc(cleaned, func(span string) string {
				var entry *MentionEntry
				if m := reMention.FindStringSubmatch(span); m != nil {
					if targets, private, mode, ok := parseMentionAttrs(m[1]); ok {
						entry = &MentionEntry{Targets: targets, Content: strings.TrimSpace(m[2]), Private: private, Mode: mode}
					}
				}
				return fn(entry)
			})
			if next == cleaned {
				break
			}
			cleaned = next
		}
		// Any opening tag still present is unclosed: drop it and its tail (a
		// truncated/streaming span's tail is just as private).
		if idx := strings.Index(cleaned, "<clawbench-mention"); idx >= 0 {
			cleaned = cleaned[:idx]
		}
	}
	cleaned = reMentionCloseAny.ReplaceAllString(cleaned, "")
	return cleaned
}

// stripAllMentionSpans removes every mention-like span entirely. Used to derive
// the prose around tags (Before / After) without protocol residue.
func stripAllMentionSpans(text string) string {
	return replaceMentionSpans(text, func(*MentionEntry) string { return "" })
}

// ReplaceMentions rewrites every mention-like span (well-formed or not) in text
// using fn: fn receives the parsed entry (nil when the span is malformed) and
// returns the replacement text. It is the seam the injection layer uses to
// render mentions as readable "@name" prose (public) while dropping private
// notes (fail-closed), and to derive the prose around tags.
func ReplaceMentions(text string, fn func(*MentionEntry) string) string {
	return replaceMentionSpans(text, fn)
}

// splitNames splits a comma-separated target list, trimming each name and
// dropping empties. Order is preserved.
func splitNames(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if name := strings.TrimSpace(p); name != "" {
			out = append(out, name)
		}
	}
	return out
}
