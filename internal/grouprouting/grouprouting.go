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
	// Empty when absent.
	Instruction string
	// Before is the text preceding the speaker tag, trimmed. It is the host's
	// background context and is consumed by the injection layer (design
	// decision #68, approach A) to render a member's context without repeating
	// the routing tag or the directive. It is a positional slice, not a parse
	// result: it is populated whenever the tag is located, even when the tag
	// itself is malformed (Found=false). Callers that require a valid routing
	// decision must gate on Found; those that only need the background may read
	// it regardless. Parse never strips — this package does not mutate input.
	Before string
	// End reports whether the host signalled the discussion is over.
	End bool
	// Raw is the matched tag text, retained for logging. Callers keep the
	// original message text regardless (this package never mutates input).
	Raw string
}

var (
	reSpeaker = regexp.MustCompile(`(?s)<clawbench-speaker>(.*?)</clawbench-speaker>`)
	reEnd     = regexp.MustCompile(`<clawbench-group-end\s*/>`)
)

// Parse locates the host's routing decision in an assistant message.
func Parse(text string) Result {
	var res Result

	if m := reEnd.FindString(text); m != "" {
		res.End = true
		res.Raw = m
	}

	loc := reSpeaker.FindStringSubmatchIndex(text)
	if loc == nil {
		return res
	}
	inner := text[loc[2]:loc[3]]
	res.Raw = text[loc[0]:loc[1]]
	// Before = the text ahead of the tag (positional slice, always available
	// once the tag is located — even for a malformed payload).
	res.Before = strings.TrimSpace(text[:loc[0]])

	speakers := splitSpeakers(inner)
	if len(speakers) == 0 {
		// Malformed (empty payload): do not claim Found, keep Raw for logs.
		res.End = reEnd.MatchString(text)
		return res
	}
	res.Found = true
	res.Speakers = speakers

	// Instruction = text after the closing speaker tag, trimmed, with any end
	// tag removed (a message may carry both; the end tag must not leak into the
	// instruction handed to a member or rendered by the card).
	after := reEnd.ReplaceAllString(text[loc[1]:], "")
	res.Instruction = strings.TrimSpace(after)
	return res
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
