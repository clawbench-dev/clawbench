package askquestion

import (
	"regexp"
	"strings"
)

// Locating <clawbench-ask-question> spans in free text.
//
// A span runs from its open tag to its matching close tag. It never reaches
// past that close: a closing token is only accepted when it plausibly belongs
// to this tag, which is checked by rejecting any candidate that contains the
// start of a later payload. That guard exists because an earlier "accept the
// next closing token" rule deleted real prose — a tag mentioned in a sentence
// followed by a genuine question removed the whole sentence.
//
// Every tag is returned, not just the last one: production text blocks contain
// two or more tags, and converting only the last leaked the rest as raw markup.

// tagName is the tag this package understands, without the angle brackets.
const tagName = "clawbench-ask-question"

var (
	reOpenTag     = regexp.MustCompile(`<` + tagName + `\b[^>]*>`)
	reStdCloseTag = regexp.MustCompile(`<` + `/` + tagName + `\s*>`)
	// Code contexts — a tag inside code is documentation, not a question.
	reCodeFence = regexp.MustCompile("(?s)```.*?```")
	// Inline code cannot span a line break, so the span is restricted to one
	// line. Without that, an orphaned backtick earlier in a long message pairs
	// with a backtick inside a real payload and swallows the whole question.
	reInlineTag = regexp.MustCompile("`[^`\\n]+`")
)

// span is a half-open byte range.
type span struct{ start, end int }

// Extract locates every clawbench-ask-question span outside a code context.
//
// A returned Match with Parsed == false could not be understood; its Raw must
// be kept in the visible text.
func Extract(text string) []Match {
	if !strings.Contains(text, "<"+tagName) {
		return nil
	}
	code := codeSpans(text)
	opens := reOpenTag.FindAllStringIndex(text, -1)
	if len(opens) == 0 {
		return nil
	}

	var matches []Match
	consumedTo := 0
	for _, loc := range opens {
		openStart, openEnd := loc[0], loc[1]
		if openStart < consumedTo {
			continue // inside a span already accounted for
		}
		if inAnySpan(code, openStart) {
			continue // documented inside code, not a live question
		}
		m := locate(text, openStart, openEnd)
		if m.End > consumedTo {
			consumedTo = m.End
		}
		matches = append(matches, m)
	}
	return matches
}

// codeSpans returns the byte ranges occupied by fenced and inline code.
func codeSpans(text string) []span {
	fences := reCodeFence.FindAllStringIndex(text, -1)
	inlines := reInlineTag.FindAllStringIndex(text, -1)
	spans := make([]span, 0, len(fences)+len(inlines))
	for _, loc := range fences {
		spans = append(spans, span{loc[0], loc[1]})
	}
	for _, loc := range inlines {
		spans = append(spans, span{loc[0], loc[1]})
	}
	return spans
}

// inAnySpan reports whether idx falls inside one of the spans.
func inAnySpan(spans []span, idx int) bool {
	for _, s := range spans {
		if idx >= s.start && idx < s.end {
			return true
		}
	}
	return false
}

// locate builds the Match for one open tag.
func locate(text string, openStart, openEnd int) Match {
	closeStart, closeEnd, reason := boundSpan(text, openEnd)

	if closeStart < 0 {
		// No usable close tag: there is no payload to parse. Report the open
		// tag alone so the caller can degrade it to text.
		raw := text[openStart:openEnd]
		return Match{
			Start:    openStart,
			End:      openEnd,
			Raw:      raw,
			Reason:   reason,
			Fallback: fallbackText(raw),
		}
	}

	inner := text[openEnd:closeStart]
	items := ParseItems(inner)
	if len(items) > 0 {
		return Match{
			Start:  openStart,
			End:    closeEnd,
			Raw:    text[openStart:closeEnd],
			Items:  items,
			Parsed: true,
		}
	}

	raw := text[openStart:closeEnd]
	return Match{
		Start:    openStart,
		End:      closeEnd,
		Raw:      raw,
		Reason:   reason,
		Fallback: fallbackText(raw),
	}
}

// boundSpan finds the close tag that belongs to the payload opened at openEnd.
// It returns the close tag's bounds, or -1 when there is no usable close.
//
// The overriding rule is that a span may never reach past its own payload, so a
// candidate close is rejected when another payload starts before it — that
// close belongs to the later tag.
func boundSpan(text string, openEnd int) (closeStart, closeEnd int, reason string) {
	loc := reStdCloseTag.FindStringIndex(text[openEnd:])
	if loc == nil {
		return -1, -1, ReasonNoStandardClose
	}
	cs := openEnd + loc[0]
	ce := openEnd + loc[1]

	// A later payload opening before this close means the close belongs to
	// that tag, not this one.
	if hasSiblingPayload(text, openEnd, cs) {
		return -1, -1, ReasonNoStandardClose
	}
	return cs, ce, ReasonParseFailed
}

// reNestedOpen matches an open tag at the start of a line.
var reNestedOpen = regexp.MustCompile(`(?m)^[ \t]*<` + tagName + `\b`)

// hasSiblingPayload reports whether text[from:closeStart] contains the start of
// a genuine sibling payload rather than a mere mention of the tag.
//
// A payload may legitimately mention the tag in its own text — inline in a
// sentence, inside a fenced block, or in an indented example. Treating such a
// mention as a sibling both leaks the real payload and splits it.
//
// A candidate mention is a genuine sibling only when both hold:
//
//   - the enclosing tag does NOT already form a payload of its own, so the
//     close cannot belong to it, and
//   - the candidate DOES form a payload ending at this close.
//
// Both tests ask whether the text actually parses, which is what distinguishes
// a real payload from a mention: a fenced or indented example leaves the
// enclosing region without a list, while a mention that opens a real payload
// parses. Line-start is checked first only to skip the common inline case
// cheaply.
func hasSiblingPayload(text string, from, closeStart int) bool {
	for _, loc := range reNestedOpen.FindAllStringIndex(text[from:closeStart], -1) {
		sibOpenEnd := from + loc[1]
		if len(ParseItems(text[from:sibOpenEnd])) > 0 {
			// The enclosing tag is itself a payload; the close is its own.
			return false
		}
		if len(ParseItems(text[sibOpenEnd:closeStart])) > 0 {
			return true
		}
	}
	return false
}

// fallbackText renders an unparsed span as plain text: the wrapper is removed,
// everything else is kept.
//
// If stripping the wrapper would leave nothing (an empty tag), the raw span is
// returned unchanged — an empty fallback would silently erase the span.
func fallbackText(raw string) string {
	stripped := strings.TrimSpace(stripAskTags(raw))
	if stripped == "" {
		return raw
	}
	return stripped
}
