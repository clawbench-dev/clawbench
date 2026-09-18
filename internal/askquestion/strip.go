package askquestion

import "strings"

// Strip removes every successfully parsed span from text and leaves the rest
// byte-for-byte intact.
//
// Unparseable spans are deliberately NOT removed: their Raw is the only
// remaining copy of the question, and deleting it is exactly the silent-loss
// defect this package exists to prevent. Callers that want a readable
// rendering of an unparseable span must keep it visible (the frontend lets it
// fall through to markdown; the backend leaves it in the text block).
//
// Spans are removed from the end backwards so earlier offsets stay valid.
func Strip(text string, matches []Match) string {
	if len(matches) == 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	prev := 0
	for _, m := range matches {
		if !m.Parsed {
			continue
		}
		if m.Start < prev || m.End > len(text) || m.End <= m.Start {
			continue
		}
		b.WriteString(text[prev:m.Start])
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
