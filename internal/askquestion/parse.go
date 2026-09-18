package askquestion

import (
	"regexp"
	"strconv"
	"strings"
)

// Tolerant parsers for the <ask-question> payload.
//
// A strict XML parser is unusable here: production data contains unclosed
// <item> and <option> tags (24% of payloads), attributes on <option>
// (<option value="A">, 12%), a plural <options> wrapper, bare text inside
// <option> with no <label>, and raw & / < / > characters.
//
// The scanner is deliberately split-based rather than regex-based for the two
// elements that go unclosed in practice (<item> and <option>): a regex cannot
// express "up to the next sibling open tag" without lookahead, and bounding at
// end-of-input instead would swallow following content. Nothing is invented —
// an option with no text at all is dropped rather than given a placeholder.
var (
	reItemOpen     = regexp.MustCompile(`<item\b[^>]*>`)
	reOptionOpen   = regexp.MustCompile(`<option\b[^>]*>`)
	reHeaderTag    = regexp.MustCompile(`(?s)<header\b[^>]*>(.*?)</header>`)
	reQuestionTag  = regexp.MustCompile(`(?s)<question\b[^>]*>(.*?)</question>`)
	reMultiTag     = regexp.MustCompile(`(?s)<multi[_-]?select\b[^>]*>(.*?)</multi[_-]?select>`)
	reLabelTag     = regexp.MustCompile(`(?s)<label\b[^>]*>(.*?)</label>`)
	reDescTag      = regexp.MustCompile(`(?s)<description\b[^>]*>(.*?)</description>`)
	reAnyTag       = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	reChildCloser  = regexp.MustCompile(`</(?:item|option)\s*>`)
	reAttrLabel    = regexp.MustCompile(`(?i)\b(?:value|label)\s*=\s*["']([^"']*)["']`)
	rePluralOption = regexp.MustCompile(`(?i)</?options\s*>`)
)

// ParseItems understands the (possibly repaired) inner payload of an
// <ask-question> block. It returns nil when nothing renderable was found —
// callers must then retain the raw text.
//
// A JSON payload is intentionally not accepted: JSON support was removed on
// purpose (commit d189374e1) and is treated as an unparseable payload.
func ParseItems(inner string) []Item {
	if strings.TrimSpace(inner) == "" {
		return nil
	}
	if looksLikeJSON(inner) {
		return nil
	}
	// <options> is a plural wrapper some models emit around the real <option>
	// elements; drop the wrapper so the option scan sees its children.
	normalized := rePluralOption.ReplaceAllString(inner, "")
	if items := scanItems(normalized); len(items) > 0 {
		return items
	}
	// Second attempt: escape stray entity characters and rescan. Doing this
	// only after a clean attempt avoids corrupting legitimate entities.
	if repaired := escapeStrayEntities(normalized); repaired != normalized {
		return scanItems(repaired)
	}
	return nil
}

// looksLikeJSON reports whether the payload opens with a JSON container.
func looksLikeJSON(s string) bool {
	t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s), "`"))
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

// scanItems splits the payload into item bodies and parses each.
//
// A body runs from the end of one <item> open tag to the next <item> open tag
// (or end of payload), so an unclosed <item> is bounded by its sibling rather
// than swallowing the rest of the message.
func scanItems(payload string) []Item {
	opens := reItemOpen.FindAllStringIndex(payload, -1)
	if len(opens) == 0 {
		return nil
	}
	var items []Item
	for i, loc := range opens {
		bodyEnd := len(payload)
		if i+1 < len(opens) {
			bodyEnd = opens[i+1][0]
		}
		body := payload[loc[1]:bodyEnd]
		if it, ok := parseItem(body); ok {
			items = append(items, it)
		}
	}
	return items
}

// parseItem reads one <item> body. An item is kept when it has question text
// or at least one option — the same bar the renderer applies.
func parseItem(body string) (Item, bool) {
	it := Item{
		Header:      tagText(reHeaderTag, body),
		Question:    tagText(reQuestionTag, body),
		MultiSelect: strings.EqualFold(tagText(reMultiTag, body), "true"),
		Options:     parseOptions(body),
	}
	if it.Options == nil {
		// A non-nil empty slice keeps the JSON shape ("options": []) identical
		// to the TypeScript mirror, which always yields an array.
		it.Options = []Option{}
	}
	if it.Question == "" && len(it.Options) == 0 {
		return Item{}, false
	}
	return it, true
}

// parseOptions reads every <option> in an item body. Like items, an unclosed
// option is bounded by the next <option> open tag or the item's end.
func parseOptions(body string) []Option {
	opens := reOptionOpen.FindAllStringIndex(body, -1)
	if len(opens) == 0 {
		return nil
	}
	var opts []Option
	for i, loc := range opens {
		bodyEnd := len(body)
		if i+1 < len(opens) {
			bodyEnd = opens[i+1][0]
		}
		content := trimChildCloser(body[loc[1]:bodyEnd])
		if opt, ok := parseOption(attrsOf(loc, body), content); ok {
			opts = append(opts, opt)
		}
	}
	return opts
}

// attrsOf returns the raw attribute text of the open tag at loc.
func attrsOf(loc []int, payload string) string {
	open := payload[loc[0]:loc[1]]
	if i := strings.IndexByte(open, ' '); i >= 0 {
		return strings.TrimSuffix(open[i:], ">")
	}
	return ""
}

// trimChildCloser drops a trailing </item> or </option> that the split left on
// the last option's content.
func trimChildCloser(s string) string {
	return reChildCloser.ReplaceAllString(s, "")
}

// parseOption reads one <option>. attrs is the raw attribute text, content the
// element body. A label comes from <label>, else from a value=/label=
// attribute, else from the element's own bare text.
func parseOption(attrs, content string) (Option, bool) {
	label := tagText(reLabelTag, content)
	desc := tagText(reDescTag, content)
	if label == "" {
		if m := reAttrLabel.FindStringSubmatch(attrs); m != nil {
			label = strings.TrimSpace(m[1])
		}
	}
	if label == "" {
		// Bare-text option: strip any child tags and use what remains.
		label = decodeText(content)
	}
	if label == "" {
		return Option{}, false
	}
	// A description identical to the label adds nothing to the card.
	if desc == label {
		desc = ""
	}
	return Option{Label: label, Description: desc}, true
}

// tagText returns the trimmed text of the first matching tag, with nested tags
// removed and entities decoded.
func tagText(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return decodeText(m[1])
}

// decodeText strips nested tags, unescapes entities, and collapses whitespace.
// Only tag-shaped constructs are removed, so literal comparison operators in
// question text ("< 5" / "> 5") survive.
//
// Entity decoding uses an explicit table (see unescapeEntities) rather than
// html.UnescapeString: the TypeScript mirror cannot carry the full HTML5
// table, so using the superset here would silently diverge on any entity the
// mirror does not know.
func decodeText(s string) string {
	s = reAnyTag.ReplaceAllString(s, "")
	s = unescapeEntities(s)
	return strings.Join(strings.Fields(s), " ")
}

// escapeStrayEntities escapes &, < and > that are not part of a real tag or a
// valid entity. Last-resort repair, applied only when a clean scan found
// nothing.
func escapeStrayEntities(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		switch c := s[i]; c {
		case '&':
			if !isEntityAt(s, i) {
				b.WriteString("&amp;")
				continue
			}
		case '<':
			if !looksLikeTagAt(s, i) {
				b.WriteString("&lt;")
				continue
			}
		case '>':
			if !looksLikeTagEndAt(s, i) {
				b.WriteString("&gt;")
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// isEntityAt reports whether s[i:] starts a valid &name; or &#nn; entity.
func isEntityAt(s string, i int) bool {
	end := strings.IndexByte(s[i:], ';')
	if end < 0 || end > 10 {
		return false
	}
	for _, r := range s[i+1 : i+end] {
		if !isEntityChar(r) {
			return false
		}
	}
	return true
}

func isEntityChar(r rune) bool {
	return r == '#' || r == 'x' || r == 'X' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// looksLikeTagAt reports whether s[i:] opens a tag whose name is a known
// ask-question element. An unknown `<` is treated as literal text.
func looksLikeTagAt(s string, i int) bool {
	rest := s[i:]
	if strings.HasPrefix(rest, "</") {
		rest = rest[2:]
	} else {
		rest = rest[1:]
	}
	for _, name := range knownTagNames {
		if hasTagNamePrefix(rest, name) {
			return true
		}
	}
	return false
}

// looksLikeTagEndAt reports whether s[i] closes a tag we recognize.
func looksLikeTagEndAt(s string, i int) bool {
	open := strings.LastIndexByte(s[:i], '<')
	if open < 0 || strings.ContainsAny(s[open:i], "<>") {
		return false
	}
	return looksLikeTagAt(s, open)
}

// hasTagNamePrefix reports whether rest begins with `<name` followed by a
// delimiter, so `<item>` matches but `<items2` does not.
func hasTagNamePrefix(rest, name string) bool {
	if len(rest) < len(name) || !strings.HasPrefix(rest, name) {
		return false
	}
	if len(rest) == len(name) {
		return true
	}
	switch rest[len(name)] {
	case '>', '/', ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

// knownTagNames are the elements that make up a legitimate payload.
var knownTagNames = []string{
	"ask-question", "item", "header", "question", "option", "options",
	"label", "description", "multi-select", "multi_select", "multiSelect",
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
