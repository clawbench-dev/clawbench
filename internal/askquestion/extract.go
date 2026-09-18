package askquestion

import (
	"regexp"
	"strings"
)

// Locating <ask-question> spans in free text.
//
// The naive approach — match the open tag, then the next closing token — is
// what produced the over-strip defect: with an unclosed tag followed by a
// <details> block, the next closing token is the details close, so the whole
// details block was treated as payload and deleted. The algorithm here bounds
// every span at the last real child close (item/option) and only extends past
// it when the intervening text is provably empty of content.
//
// It also returns EVERY tag, not just the last one: 27% of production text
// blocks contain two or more <ask-question> tags, and only converting the last
// leaked the rest as raw XML.

// gapLimit bounds how much text may sit between the last child close and a
// standard ask-question close. A larger gap means the close belongs to
// something else and the tag should be treated as unclosed.
const gapLimit = 400

var (
	reOpenTag     = regexp.MustCompile(`<ask-question\b[^>]*>`)
	reStdCloseTag = regexp.MustCompile(`<` + `/ask-question\s*>`)
	reAnyCloseTag = regexp.MustCompile(`<` + `/[^>]+>`)
	reChildClose  = regexp.MustCompile(`<` + `/(?:item|option)\s*>`)
	// Structural openers that end an unclosed payload's reach.
	reStructuralBoundary = regexp.MustCompile(`(?s)<(?:details|summary|table|div|pre|section|blockquote|ul|ol|h[1-6])[\s>]|` + "```" + `|(?m)^#{1,6}\s`)
	// Code contexts — a tag inside code is documentation, not a question.
	reCodeFence = regexp.MustCompile("(?s)```.*?```")
	// Inline code cannot span a line break, so the span is restricted to one
	// line. Without that, an orphaned backtick earlier in a long message pairs
	// with a backtick inside a real payload and swallows the whole question.
	reInlineTag = regexp.MustCompile("`[^`\\n]+`")
)

// span is a half-open byte range.
type span struct{ start, end int }

// Extract locates every <ask-question> span outside a code context.
//
// A returned Match with Parsed == false could not be understood; its Raw must
// be kept in the visible text.
func Extract(text string) []Match {
	if !strings.Contains(text, "<ask-question") {
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
	innerEnd, spanEnd, reason := boundSpan(text, openEnd)

	inner := text[openEnd:innerEnd]
	items := ParseItems(inner)
	if len(items) > 0 {
		return Match{
			Start:  openStart,
			End:    spanEnd,
			Raw:    text[openStart:spanEnd],
			Items:  items,
			Parsed: true,
		}
	}

	// Unparseable: report the span but mark it so callers retain the text.
	end := spanEnd
	if end <= openStart {
		end = openEnd
	}
	return Match{
		Start:  openStart,
		End:    end,
		Raw:    text[openStart:end],
		Reason: reason,
	}
}

// boundSpan decides where the payload ends. It returns the end of the text to
// parse, the end of the span to remove, and a reason code.
func boundSpan(text string, openEnd int) (innerEnd, spanEnd int, reason string) {
	if loc := reStdCloseTag.FindStringIndex(text[openEnd:]); loc != nil {
		closeStart := openEnd + loc[0]
		closeEnd := closeStart + (loc[1] - loc[0])
		childEnd := lastChildEnd(text, openEnd, closeStart)
		// No child close at all: the payload has a standard close but no
		// <item>/<option> (a JSON body, or an empty tag). Hand the whole inner
		// range to the parser; it will fail and the caller retains the text.
		if childEnd < 0 {
			return closeStart, closeEnd, ReasonParseFailed
		}
		gap := text[childEnd:closeStart]
		if isCleanGap(gap) && !hasStructuralBoundary(gap) {
			return closeStart, closeEnd, ""
		}
	}

	// No usable standard close. A non-standard close is accepted only when the
	// text between the last child close and it is pure junk — otherwise that
	// close belongs to unrelated content (the <details> case).
	childEnd := lastChildEnd(text, openEnd, len(text))
	if childEnd < 0 {
		return openEnd, openEnd, ReasonNoChildClose
	}
	if loc := reAnyCloseTag.FindStringIndex(text[childEnd:]); loc != nil {
		closeStart := childEnd + loc[0]
		if isCleanGap(text[childEnd:closeStart]) {
			return childEnd, closeStart + (loc[1] - loc[0]), ""
		}
	}
	return childEnd, childEnd, ReasonNoStandardClose
}

// lastChildEnd returns the end offset of the last </item> or </option> in
// text[from:to], or -1 when there is none.
func lastChildEnd(text string, from, to int) int {
	if to > len(text) {
		to = len(text)
	}
	locs := reChildClose.FindAllStringIndex(text[from:to], -1)
	if len(locs) == 0 {
		return -1
	}
	return from + locs[len(locs)-1][1]
}

// isCleanGap reports whether the text between a payload and its closing token
// carries no content — only whitespace and stray punctuation. Anything else
// (in particular any letter, digit or CJK character) means the closing token
// terminates different content, as when an unclosed payload is followed by a
// <details> block.
func isCleanGap(gap string) bool {
	if len(gap) > gapLimit {
		return false
	}
	for _, r := range gap {
		if isGapNoise(r) {
			continue
		}
		return false
	}
	return true
}

// isGapNoise reports whether a rune is acceptable between a payload and its
// closing token: whitespace, or the punctuation that an obfuscated close
// leaves behind (fullwidth pipes, colons, slashes).
func isGapNoise(r rune) bool {
	if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
		return true
	}
	switch r {
	case '｜', '|', ':', '：', '/', '\\', '-', '_', '.', '·':
		return true
	}
	return false
}

// hasStructuralBoundary reports whether the gap opens a block-level element or
// a heading/fence — a strong signal the payload ended earlier.
func hasStructuralBoundary(gap string) bool {
	return reStructuralBoundary.MatchString(gap)
}
