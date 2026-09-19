package askquestion

import "strings"

// Strip replaces every located span with what should be shown in its place.
//
//   - A parsed span is removed entirely (the card renders it).
//   - An unparsed span is replaced by its Fallback: the payload with the
//     ask-question wrapper removed, so it renders as ordinary Markdown instead
//     of exposing raw markup.
//
// No content is ever discarded. An unparsed span's Fallback holds everything
// the span contained, so the failure mode is "renders as plain text" — never
// the silent loss this package exists to prevent.
//
// Spans are applied from the end backwards so earlier offsets stay valid.
func Strip(text string, matches []Match) string {
	if len(matches) == 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	prev := 0
	for _, m := range matches {
		if m.Start < prev || m.End > len(text) || m.End <= m.Start {
			continue
		}
		b.WriteString(text[prev:m.Start])
		if m.Parsed {
			// The card renders it; nothing goes into the text stream.
		} else if m.Fallback != "" {
			b.WriteString(m.Fallback)
		} else {
			// Defensive: a fallback is always set for an unparsed span, but an
			// empty one would erase content, so keep the raw text.
			b.WriteString(m.Raw)
		}
		prev = m.End
	}
	b.WriteString(text[prev:])
	return b.String()
}

// HasParsed reports whether at least one match was understood.
func HasParsed(matches []Match) bool {
	for _, m := range matches {
		if m.Parsed {
			return true
		}
	}
	return false
}

// AllItems flattens the items of every parsed match, preserving order.
func AllItems(matches []Match) []Item {
	var items []Item
	for _, m := range matches {
		if m.Parsed {
			items = append(items, m.Items...)
		}
	}
	return items
}

// UnparsedReasons returns the reason codes of the matches that failed, for
// logging.
func UnparsedReasons(matches []Match) []string {
	var reasons []string
	for _, m := range matches {
		if !m.Parsed {
			reasons = append(reasons, m.Reason)
		}
	}
	return reasons
}
