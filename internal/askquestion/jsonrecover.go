package askquestion

import (
	"encoding/json"
	"strings"
)

// Tolerant JSON extraction for payloads parked inside a tag.
//
// JSON inside the tag is not the documented format (the system prompt mandates
// Markdown), but models still emit it — and when they do, it is usually almost
// valid: the values contain raw typographic quotes the model forgot to escape
// ("多久？这决定 Layer 2 的设计"). Standard json.Unmarshal rejects the whole
// payload on the first such quote, which would discard a perfectly readable
// question.
//
// This is a recovery path, not a supported input format. It only runs when the
// strict parse fails, and it only ever *removes* escaping ambiguity — it never
// invents structure. If recovery fails the caller falls back to rendering the
// text as Markdown, so a failure costs nothing.

// parseTolerantJSON decodes a JSON object, repairing the unescaped-quote case.
func parseTolerantJSON(s string) (map[string]any, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s[0] != '{' {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) == nil {
		return m, true
	}
	repaired, ok := repairUnescapedQuotes(s)
	if !ok {
		return nil, false
	}
	if json.Unmarshal([]byte(repaired), &m) != nil {
		return nil, false
	}
	return m, true
}

// repairUnescapedQuotes re-escapes double quotes that appear inside a JSON
// string value.
//
// A quote is treated as a *closing* quote only when the next non-space byte is
// structural for the enclosing context (`:`, `,`, `}`, `]`) or the string is
// followed by a colon (an object key). Anything else is interior text and is
// escaped. This is enough for the real payloads, which contain balanced
// typographic quotes inside values, without attempting to be a general JSON
// repairer.
func repairUnescapedQuotes(s string) (string, bool) {
	var b strings.Builder
	b.Grow(len(s) + 16)

	inString := false
	changed := false
	for i := 0; i < len(s); i++ {
		c := s[i]

		if !inString {
			b.WriteByte(c)
			if c == '"' {
				inString = true
			}
			continue
		}

		// Inside a string.
		switch c {
		case '\\':
			// Copy the escaped pair verbatim.
			b.WriteByte(c)
			if i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			}
			continue
		case '"':
			if closesString(s, i) {
				b.WriteByte(c)
				inString = false
			} else {
				// Interior quote: escape it and stay in the string.
				b.WriteString(`\"`)
				changed = true
			}
			continue
		}
		b.WriteByte(c)
	}
	if !changed {
		return s, false
	}
	return b.String(), true
}

// closesString reports whether the quote at s[i] ends the string it is in.
//
// It ends the string when what follows is structural: a colon (the value just
// closed was an object key), a comma, or a closing brace/bracket. Whitespace is
// skipped. Anything else means the quote was interior text.
func closesString(s string, i int) bool {
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case ' ', '\t', '\n', '\r':
			continue
		case ':', ',', '}', ']':
			return true
		default:
			return false
		}
	}
	// A quote at end-of-input closes the string.
	return true
}

// recoverJSONItems attempts to read a JSON payload as one or more items.
//
// The documented format is Markdown; this exists only so that a model that
// still emits JSON does not lose its question. Both the object shape
// ({"questions":[...]}) and a bare array ([{...}]) are accepted, and the
// unescaped-quote repair is applied when the strict parse fails.
func recoverJSONItems(inner string) []Item {
	trimmed := strings.TrimSpace(inner)
	if trimmed == "" {
		return nil
	}

	// Array form: [{"question":...}, ...]
	if trimmed[0] == '[' {
		if arr, ok := parseTolerantJSONArray(trimmed); ok {
			if items := NormalizeInput(map[string]any{KeyQuestions: arr}); len(items) > 0 {
				return items
			}
		}
		return nil
	}

	obj, ok := parseTolerantJSON(trimmed)
	if !ok {
		return nil
	}
	return NormalizeInput(obj)
}

// parseTolerantJSONArray decodes a JSON array, applying the same quote repair.
func parseTolerantJSONArray(s string) ([]any, bool) {
	var arr []any
	if json.Unmarshal([]byte(s), &arr) == nil {
		return arr, true
	}
	repaired, ok := repairUnescapedQuotes(s)
	if !ok {
		return nil, false
	}
	if json.Unmarshal([]byte(repaired), &arr) != nil {
		return nil, false
	}
	return arr, true
}
