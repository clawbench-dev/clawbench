package askquestion

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Native-Markdown payload parsing.
//
// The current format is plain Markdown inside the tag: one tag is one question,
// a `**bold**` line is the header, ordinary lines are the question text, and
// the options are a Markdown list. A checkbox list means multi-select.
//
//	<ask-question>
//	**方案选择**
//	你更倾向哪种实现方式？
//	- 方案 A — 快但不够安全
//	- 方案 B — 安全但慢
//	</ask-question>
//
// Markdown is the model's native format, so it is written correctly far more
// often than the bespoke XML it replaces. The legacy XML shape is still parsed
// (see ParseItems) so historical conversations keep rendering their cards.
var (
	reMdBold     = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reMdItalic   = regexp.MustCompile(`\*([^*\n]+)\*`)
	reMdFence    = regexp.MustCompile("^\\s*```")
	reAtxHeading = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*#*$`)
	reBoldLine   = regexp.MustCompile(`^(?:\*\*(.+?)\*\*|__(.+?)__)$`)

	// reOrderedMarker matches an ordered-list marker: 1. / 1) / 1、 and the CJK
	// numerals (一、二、…). The trailing content is captured separately so the
	// "must be followed by a space" rule can differ per marker.
	reOrderedMarker = regexp.MustCompile(`^(\d+[.)、]|[一二三四五六七八九十]+[、.)])(.*)$`)

	// reCheckbox matches a checkbox at the start of a list item's content.
	// Models emit ASCII, fullwidth and CJK brackets, with or without inner
	// spacing, and with or without a check mark.
	reCheckbox = regexp.MustCompile(`^(?:\[\s*[xX]?\s*\]|［\s*[xX]?\s*］|【\s*[xX]?\s*】)\s*(.*)$`)
)

// ideographicSpace is U+3000, which models use as a fullwidth space. Go's
// strings.Trim treats it as an ordinary rune, so it needs trimming explicitly.
const ideographicSpace = "\u3000"

// trimListSpace removes leading/trailing ASCII whitespace and the ideographic
// space.
func trimListSpace(s string) string {
	return strings.Trim(s, " \t\r\n"+ideographicSpace)
}

// splitListMarker returns the content of a list item when line starts with a
// list marker, and whether a marker was found.
//
// Beyond CommonMark's "-", "*", "+" and "1.", this accepts the forms models
// actually emit: the fullwidth hyphen, CJK ordinal dots (1、), CJK numerals
// (一、), and a bullet with no following space (-甲). Those relaxations are
// guarded so ordinary prose is not mistaken for a list:
//
//   - "*" must be followed by a space, so "**bold**" and "*italic*" are not
//     list items;
//   - "-"/"+"/"－" must not be followed by a digit or hyphen, so "-5" and "---"
//     are not list items;
//   - "1."/"1)" must be followed by a space, so "1.5" is not a list item.
func splitListMarker(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t"+ideographicSpace)
	if trimmed == "" {
		return "", false
	}

	if r, size := utf8.DecodeRuneInString(trimmed); r == '-' || r == '+' || r == '*' || r == '－' {
		if content, ok := bulletContent(r, trimmed[size:]); ok {
			return trimListSpace(content), true
		}
		return "", false
	}

	if m := reOrderedMarker.FindStringSubmatch(trimmed); m != nil {
		if !orderedMarkerIsFollowedBySpace(m[1], m[2]) {
			return "", false
		}
		return trimListSpace(m[2]), true
	}
	return "", false
}

// bulletContent validates the text after a bullet character and returns the
// item content.
//
// "*" must be followed by a space, so "**bold**" and "*italic*" are not list
// items. The other bullets must not be followed by a digit or a hyphen, so
// "-5" (a negative number) and "---" (a horizontal rule) are not list items
// either. A bullet with no following space ("-甲") is accepted: models emit it
// and it cannot be confused with prose.
func bulletContent(bullet rune, rest string) (string, bool) {
	if bullet == '*' {
		if !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "\t") {
			return "", false
		}
		return rest, true
	}
	if rest != "" {
		next, _ := utf8.DecodeRuneInString(rest)
		if unicode.IsDigit(next) || next == '-' {
			return "", false
		}
	}
	return rest, true
}

// orderedMarkerIsFollowedBySpace reports whether an ordered marker is valid for
// its content. "1." and "1)" require a following space, so "1.5" is not a list
// item; the CJK forms ("1、", "一、") do not.
func orderedMarkerIsFollowedBySpace(marker, rest string) bool {
	if !strings.HasSuffix(marker, ".") && !strings.HasSuffix(marker, ")") {
		return true
	}
	return strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t")
}

// markdownHeader returns the header text when the whole line is an ATX heading
// or is entirely bold.
func markdownHeader(line string) (string, bool) {
	trimmed := trimListSpace(line)
	if m := reAtxHeading.FindStringSubmatch(trimmed); m != nil {
		return cleanInline(m[1]), true
	}
	if m := reBoldLine.FindStringSubmatch(trimmed); m != nil {
		if m[1] != "" {
			return cleanInline(m[1]), true
		}
		if m[2] != "" {
			return cleanInline(m[2]), true
		}
	}
	return "", false
}

// parseMarkdownItems parses a Markdown payload. It returns at most one item:
// the format defines one question per tag, and multiple questions are written
// as multiple tags.
func parseMarkdownItems(inner string) []Item {
	var header, question string
	var options []Option
	multi := false
	var qLines []string
	inFence := false

	for _, raw := range strings.Split(inner, "\n") {
		line := strings.TrimRight(raw, " \t\r")

		// A fenced block is content, not structure: keep every line verbatim as
		// question text so nothing inside it is mistaken for an option.
		if reMdFence.MatchString(line) {
			inFence = !inFence
			qLines = append(qLines, line)
			continue
		}
		if inFence {
			qLines = append(qLines, line)
			continue
		}

		// A header line is never a list item, so it is checked first. The title
		// must precede the options; a bold line among the options is an option
		// label, which splitListMarker already handles.
		if h, ok := markdownHeader(line); ok {
			if header == "" && len(options) == 0 {
				header = h
			} else {
				qLines = append(qLines, h)
			}
			continue
		}

		if content, ok := splitListMarker(line); ok {
			if m := reCheckbox.FindStringSubmatch(content); m != nil {
				multi = true
				if opt, ok := markdownOption(m[1]); ok {
					options = append(options, opt)
				}
				continue
			}
			if opt, ok := markdownOption(content); ok {
				options = append(options, opt)
			}
			continue
		}

		if t := trimListSpace(line); t != "" {
			qLines = append(qLines, t)
		}
	}

	question = cleanInline(strings.Join(qLines, " "))
	// A Markdown payload is a question only when it carries a list. Prose with
	// no list is not a card: the assistant discusses the tag format in ordinary
	// sentences, and turning every such mention into a card would be noise.
	// This is also what gives "parse failure" a precise meaning — the caller
	// then strips the wrapper and renders the text as Markdown.
	//
	// The legacy XML and JSON paths deliberately do NOT require options: they
	// are either frozen historical behavior or an explicit structured call,
	// whereas this path parses untrusted free-form prose.
	if len(options) == 0 {
		return nil
	}
	if options == nil {
		// A non-nil empty slice keeps the JSON shape ("options": []) identical
		// to the TypeScript mirror, which always yields an array.
		options = []Option{}
	}
	return []Item{{
		Header:      header,
		MultiSelect: multi,
		Question:    question,
		Options:     options,
	}}
}

// markdownOption reads one list entry. The label and description are separated
// by an em dash (the documented form) or a spaced hyphen.
func markdownOption(s string) (Option, bool) {
	label, desc := splitMarkdownOption(strings.TrimSpace(s))
	label = cleanInline(label)
	desc = cleanInline(desc)
	if label == "" {
		return Option{}, false
	}
	// A description identical to the label adds nothing to the card.
	if desc == label {
		desc = ""
	}
	return Option{Label: label, Description: desc}, true
}

// splitMarkdownOption splits an option on its first separator. The em dash is
// checked first because it is the documented form and never appears inside an
// ordinary label.
func splitMarkdownOption(s string) (label, desc string) {
	for _, sep := range []string{"\u2014", "\u2013", " - "} {
		if i := strings.Index(s, sep); i >= 0 {
			return s[:i], s[i+len(sep):]
		}
	}
	return s, ""
}

// cleanInline removes inline Markdown emphasis markers and decodes entities.
// The card renders plain text, so `**bold**` must not display its asterisks.
func cleanInline(s string) string {
	s = reMdBold.ReplaceAllString(s, "$1")
	s = reMdItalic.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "`", "")
	s = unescapeEntities(s)
	return strings.Join(strings.Fields(s), " ")
}

// stripAskTags removes the ask-question wrapper and its legacy child elements,
// leaving everything else — including unknown tags and all text — untouched.
//
// This is what a failed parse degrades to: the wrapper disappears and the
// content falls through to the Markdown renderer. It never discards content,
// so the failure mode is "renders as plain text", not "question vanishes".
func stripAskTags(s string) string {
	return reAskWrapperTags.ReplaceAllString(s, "")
}

var reAskWrapperTags = regexp.MustCompile(`(?i)</?(?:ask-question|item|header|question|option|options|label|description|multi[_-]?select)\b[^>]*>`)
