package askquestion

import (
	"regexp"
	"strconv"
	"strings"
)

// Payload parsing for the clawbench-ask-question tag.
//
// The only supported payload is native Markdown (see markdown.go). It is the
// model's own format, so it is written correctly far more often than any
// bespoke markup. Anything else is a parse failure, and the caller then strips
// the wrapper and renders the inner text as ordinary Markdown.
//
// There is deliberately no fallback parser: an earlier version carried a
// tolerant XML reader plus a JSON recovery path, and the extra acceptance
// masked malformed output instead of surfacing it. A payload that does not
// parse is visible as prose, which is the signal to fix the prompt rather than
// a reason to guess.

// ParseItems understands the inner payload of a clawbench-ask-question block.
// It returns nil when nothing renderable was found.
//
// A Markdown payload is a question only when it carries a list; see
// parseMarkdownItems for why.
func ParseItems(inner string) []Item {
	if strings.TrimSpace(inner) == "" {
		return nil
	}
	return parseMarkdownItems(inner)
}

// namedEntities is the shared Go/TS entity table. It covers the five XML
// entities plus the common typographic and symbol entities that appear in
// assistant output. The TypeScript mirror holds an identical table; adding an
// entry here without adding it there breaks the parity tests.
var namedEntities = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": "\"", "apos": "'",
	"nbsp": "\u00a0", "hellip": "\u2026", "mdash": "\u2014", "ndash": "\u2013",
	"copy": "\u00a9", "reg": "\u00ae", "trade": "\u2122",
	"laquo": "\u00ab", "raquo": "\u00bb", "times": "\u00d7", "divide": "\u00f7",
	"deg": "\u00b0", "plusmn": "\u00b1", "middot": "\u00b7", "bull": "\u2022",
	"lsquo": "\u2018", "rsquo": "\u2019", "ldquo": "\u201c", "rdquo": "\u201d",
}

var reEntityRef = regexp.MustCompile(`&(#x?[0-9a-fA-F]+|[a-zA-Z][a-zA-Z0-9]*);`)

// unescapeEntities decodes the shared entity set plus decimal/hex numeric
// references. Unknown entities are left verbatim.
func unescapeEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	return reEntityRef.ReplaceAllStringFunc(s, func(ref string) string {
		body := ref[1 : len(ref)-1]
		if body[0] == '#' {
			return decodeNumericEntity(body[1:], ref)
		}
		if v, ok := namedEntities[body]; ok {
			return v
		}
		return ref
	})
}

// decodeNumericEntity decodes a decimal (&#9745;) or hex (&#x1F600;) reference,
// returning the original text when the code point is invalid.
func decodeNumericEntity(body, original string) string {
	base := 10
	if len(body) > 1 && (body[0] == 'x' || body[0] == 'X') {
		base = 16
		body = body[1:]
	}
	n, err := strconv.ParseInt(body, base, 32)
	if err != nil || n <= 0 || n > 0x10FFFF {
		return original
	}
	return string(rune(n))
}
