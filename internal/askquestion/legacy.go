package askquestion

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Degradation of the pre-rename <ask-question> tag.
//
// The tag was renamed to <clawbench-ask-question> and its payload changed from
// bespoke XML (later also JSON) to native Markdown. The rename deliberately
// dropped every compatibility reader: an old payload no longer becomes a card.
//
// That left a rendering defect. An old payload is *not* markup a renderer
// ignores — the wrapper is stripped and the inner text is handed to the
// Markdown renderer, where DOMPurify removes the unknown XML elements but
// keeps their text nodes. So the field values survive as prose:
//
//	<header>下一步</header><multi-select>false</multi-select>
//	<question>…</question><option><label>只修本地能用</label>…
//
// renders as "下一步 false … 只修本地能用 先保证自己 iOS 上传恢复" — the parser-only
// field `false` is shown to the user, and a label runs into its description
// because the tags that separated them are gone.
//
// This file degrades an old span to readable Markdown instead: the header
// becomes a bold line, the question a paragraph, each option a list item, and
// the parser-only multi-select flag is dropped. It never produces a card —
// AllItems only reports parsed spans, and a legacy span is never Parsed.
//
// Measured on the production database: of 431 structured legacy spans, the old
// behavior leaked `false`/`true` in 68.5% and lost the label/description
// separator in 62.0%; this degradation renders 100% of them with no field
// leakage.

// legacyTagName is the pre-rename tag, without the angle brackets.
const legacyTagName = "ask-question"

var (
	reLegacyOpen  = regexp.MustCompile(`<` + legacyTagName + `\b[^>]*>`)
	reLegacyClose = regexp.MustCompile(`<` + `/` + legacyTagName + `\s*>`)

	// reLegacyChildClose bounds a payload that lost its wrapper close: the
	// payload ends at its last child element close.
	reLegacyChildClose = regexp.MustCompile(`(?i)</(?:item|options|option|label|description|question|header|multi[_-]?select)\s*>`)

	// reLegacySiblingOpen matches a sibling payload's open tag at the start of
	// a line. A span is clamped here so it can never swallow the next question
	// (production data contains blocks with several old payloads in a row).
	reLegacySiblingOpen = regexp.MustCompile(`(?m)^[ \t]*<` + legacyTagName + `\b`)

	// reLegacyStructHead matches a payload that starts immediately with a
	// structural element. It distinguishes a real payload from prose that
	// merely mentions the tag ("…stripped <ask-question> tags from e.blocks").
	reLegacyStructHead = regexp.MustCompile(`(?is)^\s*(?:<(?:item|options|header|question|option|label|description|multi[_-]?select)\b|[\{\[])`)

	// reLegacyChildOpen reports whether a payload carries any child element.
	reLegacyChildOpen = regexp.MustCompile(`(?i)<(item|header|question|option|options|label|description|multi[_-]?select)\b`)

	// reLegacyJSONBody matches a JSON payload parked inside the tag.
	reLegacyJSONBody = regexp.MustCompile(`^\s*[\{\[]`)

	// Element readers. Every one tolerates the malformations seen in production:
	// unclosed elements (24% of payloads), attributes instead of child elements
	// (`<option value="A">`, 12%), a plural <options> wrapper, bare text inside
	// <option>, and raw & / < / > characters.
	reLegacyHeaderEl    = regexp.MustCompile(`(?is)<header\b[^>]*>(.*?)</header>`)
	reLegacyQuestionEl  = regexp.MustCompile(`(?is)<question\b[^>]*>(.*?)</question>`)
	reLegacyLabelEl     = regexp.MustCompile(`(?is)<label\b[^>]*>(.*?)</label>`)
	reLegacyDescEl      = regexp.MustCompile(`(?is)<description\b[^>]*>(.*?)</description>`)
	reLegacyItemOpen    = regexp.MustCompile(`(?is)<item\b[^>]*>`)
	reLegacyOptionOpen  = regexp.MustCompile(`(?is)<option\b([^>]*)>`)
	reLegacyOptionsWrap = regexp.MustCompile(`(?is)</?options\s*>`)
	reLegacyAttrValue   = regexp.MustCompile(`(?i)\b(?:value|label)\s*=\s*["']([^"']*)["']`)
	reLegacyAnyTag      = regexp.MustCompile(`(?s)</?[a-zA-Z][^>]*>`)

	// reLegacyArtifact matches leaked model-harness markup: a closing tag whose
	// name was mangled with the DSML sentinel, and a bare "</>". 12297 of these
	// occur in the production database, always as garbage inside a payload —
	// never as prose a reader would want — so they are dropped rather than
	// shown. The fullwidth vertical bar is the sentinel’s delimiter.
	reLegacyArtifact = regexp.MustCompile(`(?s)</?[^<>\n]*｜｜[^<>\n]*>|</>`)
)

// extractLegacy locates every pre-rename <ask-question> span outside a code
// context and renders each to readable Markdown.
//
// The returned matches are always Parsed == false: an old payload degrades to
// text and never becomes a card.
func extractLegacy(text string, code []span) []Match {
	if !strings.Contains(text, "<"+legacyTagName) {
		return nil
	}
	opens := reLegacyOpen.FindAllStringIndex(text, -1)
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

		payloadEnd, spanEnd, ok := boundLegacy(text, openEnd)
		if !ok {
			continue // a mere mention of the tag, not a payload
		}
		inner := text[openEnd:payloadEnd]
		if !isLegacyPayload(inner) {
			continue // prose that happens to start with a structural word
		}

		matches = append(matches, Match{
			Start:    openStart,
			End:      spanEnd,
			Raw:      text[openStart:spanEnd],
			Reason:   ReasonLegacyFormat,
			Fallback: fallbackText(renderLegacy(inner)),
		})
		consumedTo = spanEnd
	}
	return matches
}

// boundLegacy returns the offsets of the payload opened at openEnd, and
// whether a payload was found at all.
//
// The span is clamped at the next line-start sibling open tag, so it can never
// reach past its own payload. Within that region the payload ends at:
//
//   - the standard </ask-question>, when present — the close tag itself is part
//     of the span (so Strip removes it) but not of the payload;
//   - else the last child element close (an unclosed wrapper, 13% of real
//     payloads);
//   - else the whole region, when it starts with a structural element (a
//     payload truncated mid-stream).
//
// A region that starts with ordinary prose is not a payload: the tag was
// mentioned in a sentence, so it is left untouched in the visible text.
func boundLegacy(text string, openEnd int) (payloadEnd, spanEnd int, ok bool) {
	region := text[openEnd:]
	if sib := reLegacySiblingOpen.FindStringIndex(region); sib != nil {
		region = region[:sib[0]]
	}
	if strings.TrimSpace(region) == "" {
		return 0, 0, false
	}

	if loc := reLegacyClose.FindStringIndex(region); loc != nil {
		// The close tag is removed with the span, so it must not be parsed as
		// part of the payload: keeping it would feed a stray "</ask-question>"
		// into the JSON decoder and defeat the strict parse.
		return openEnd + loc[0], openEnd + loc[1], true
	}
	if locs := reLegacyChildClose.FindAllStringIndex(region, -1); len(locs) > 0 {
		end := openEnd + locs[len(locs)-1][1]
		return end, end, true
	}
	if reLegacyStructHead.MatchString(region) {
		end := openEnd + len(region)
		return end, end, true
	}
	return 0, 0, false
}

// isLegacyPayload reports whether inner carries a structured old-format payload
// (XML children or JSON) rather than ordinary prose.
func isLegacyPayload(inner string) bool {
	return reLegacyChildOpen.MatchString(inner) || reLegacyJSONBody.MatchString(strings.TrimSpace(inner))
}

// renderLegacy converts an old payload into readable Markdown.
//
// Nothing user-visible is discarded: the header, question, option labels and
// descriptions all survive, and the parser-only multi-select flag is dropped
// because it has no meaning in prose.
func renderLegacy(inner string) string {
	if reLegacyJSONBody.MatchString(strings.TrimSpace(inner)) {
		return renderLegacyJSON(inner)
	}
	if !reLegacyAnyTag.MatchString(inner) {
		// Plain text payload (an old tag wrapped around ordinary prose).
		return strings.TrimSpace(inner)
	}

	// A payload may hold several <item> elements (one question each). They are
	// rendered in order; a payload without <item> is treated as a single item.
	items := splitLegacyItems(reLegacyOptionsWrap.ReplaceAllString(inner, ""))
	var b strings.Builder
	for _, item := range items {
		writeLegacyItem(&b, item)
	}
	if strings.TrimSpace(b.String()) == "" {
		// No element carried text (e.g. a payload that is only a stray tag):
		// fall back to the tag-stripped text so nothing disappears.
		return strings.TrimSpace(cleanLegacyText(inner))
	}
	return strings.TrimSpace(b.String())
}

// splitLegacyItems splits a payload on <item> opens. An unclosed <item> is
// bounded by the next <item> open rather than swallowing the rest.
func splitLegacyItems(payload string) []string {
	opens := reLegacyItemOpen.FindAllStringIndex(payload, -1)
	if len(opens) == 0 {
		return []string{payload}
	}
	items := make([]string, 0, len(opens))
	for i, loc := range opens {
		end := len(payload)
		if i+1 < len(opens) {
			end = opens[i+1][0]
		}
		items = append(items, payload[loc[1]:end])
	}
	return items
}

// writeLegacyItem renders one <item> body: header, question, then options.
func writeLegacyItem(b *strings.Builder, item string) {
	if h := legacyElementText(reLegacyHeaderEl, item); h != "" {
		b.WriteString("**" + h + "**\n")
	}
	// <multi-select> carries no user-facing content, so it is dropped: it is
	// the field whose raw `false` used to leak into the message.
	if q := legacyElementText(reLegacyQuestionEl, item); q != "" {
		b.WriteString(q + "\n")
	}
	for _, opt := range legacyOptions(item) {
		b.WriteString("- " + opt + "\n")
	}
}

// legacyOptions reads every <option> in an item body. Like <item>, an unclosed
// <option> is bounded by the next <option> open or the item's end.
func legacyOptions(body string) []string {
	opens := reLegacyOptionOpen.FindAllStringSubmatchIndex(body, -1)
	if len(opens) == 0 {
		return nil
	}
	var out []string
	for i, loc := range opens {
		end := len(body)
		if i+1 < len(opens) {
			end = opens[i+1][0]
		}
		attrs := body[loc[2]:loc[3]]
		content := body[loc[1]:end]

		// Precedence: an explicit <label> child, then the element’s own body
		// text, then the attribute. Production data has 320 options in the
		// `<option value="A">A. …</option>` shape, where the attribute carries
		// only the key and the body carries the real label — preferring the
		// attribute would show "A" and discard the sentence.
		label := legacyElementText(reLegacyLabelEl, content)
		if label == "" {
			label = cleanLegacyText(content)
		}
		if label == "" {
			if m := reLegacyAttrValue.FindStringSubmatch(attrs); m != nil {
				label = strings.TrimSpace(m[1])
			}
		}
		if label == "" {
			// Nothing was invented: an option with no text is dropped.
			continue
		}

		desc := legacyElementText(reLegacyDescEl, content)
		// A description identical to the label adds nothing.
		if desc != "" && desc != label {
			out = append(out, label+" — "+desc)
		} else {
			out = append(out, label)
		}
	}
	return out
}

// legacyElementText returns the trimmed text of the first matching element,
// with nested tags removed and entities decoded.
func legacyElementText(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return cleanLegacyText(m[1])
}

// cleanLegacyText strips nested tags, decodes entities, and collapses
// whitespace. Only tag-shaped constructs are removed, so a literal "< 5" in
// question text survives.
func cleanLegacyText(s string) string {
	s = reLegacyArtifact.ReplaceAllString(s, " ")
	s = reLegacyAnyTag.ReplaceAllString(s, " ")
	s = unescapeEntities(s)
	return strings.Join(strings.Fields(s), " ")
}

// renderLegacyJSON renders a JSON payload as Markdown.
//
// JSON inside the tag was never the documented format, but models emitted it
// and the field values are perfectly readable. The normalizer is reused so the
// same key synonyms and malformations are tolerated as on the tool-call path;
// a payload that does not decode falls back to a regex salvage, because the
// alternative is leaking raw JSON braces into the message.
func renderLegacyJSON(inner string) string {
	items := NormalizeInput(parseLegacyJSONObject(inner))
	if len(items) == 0 {
		return salvageLegacyJSON(inner)
	}
	var b strings.Builder
	for _, it := range items {
		if it.Header != "" {
			b.WriteString("**" + it.Header + "**\n")
		}
		if it.Question != "" {
			b.WriteString(it.Question + "\n")
		}
		for _, o := range it.Options {
			if o.Description != "" && o.Description != o.Label {
				b.WriteString("- " + o.Label + " — " + o.Description + "\n")
			} else {
				b.WriteString("- " + o.Label + "\n")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// parseLegacyJSONObject decodes the payload, accepting both the object shape
// ({"questions":[…]}) and a bare array ([{…}]).
//
// A payload that does not decode is repaired once (see repairLegacyJSON) and
// retried; a payload that still does not decode yields nil, which sends the
// caller to the salvage path.
func parseLegacyJSONObject(inner string) map[string]any {
	trimmed := strings.TrimSpace(inner)
	if trimmed == "" {
		return nil
	}
	if m := decodeLegacyJSON(trimmed); m != nil {
		return m
	}
	if repaired := repairLegacyJSON(trimmed); repaired != trimmed {
		return decodeLegacyJSON(repaired)
	}
	return nil
}

// decodeLegacyJSON is a strict decode of either accepted top-level shape.
func decodeLegacyJSON(s string) map[string]any {
	if s[0] == '[' {
		var arr []any
		if json.Unmarshal([]byte(s), &arr) == nil {
			return map[string]any{KeyQuestions: arr}
		}
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) == nil {
		return m
	}
	return nil
}

// repairLegacyJSON repairs the two quote defects seen in production. Both make
// json.Unmarshal reject a payload whose field values are otherwise intact, and
// both were losing user-visible content before this repair existed:
//
//   - a key that lost its opening quote — `{label":"方向三为主"}`. Without the
//     repair only the first option decoded, so a four-option question rendered
//     as one option (real: #14752).
//   - a quote used as ordinary punctuation inside a value —
//     `"question":"…最多可以被"临时分裂"多久？"`. The value was cut short at the
//     first interior quote, truncating the question mid-sentence (real: #15064,
//     #15066, #22538, #22544).
//
// This is a repair, not a parser: it only ever adds a missing opening quote or
// an escape, never structure. If the result still does not decode the caller
// falls back to salvageLegacyJSON, so a failed repair costs nothing.
//
// It is deliberately not a general JSON repairer — it targets exactly these two
// shapes, which is what the production corpus contains.
func repairLegacyJSON(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)

	inString := false
	changed := false
	for i := 0; i < len(s); {
		c := s[i]

		if !inString {
			// An object boundary may open a key that lost its opening quote.
			if c == '{' || c == ',' {
				if prefix, next, ok := quoteBareLegacyKey(s, i); ok {
					b.WriteString(prefix)
					i = next
					changed = true
					continue
				}
			}
			b.WriteByte(c)
			if c == '"' {
				inString = true
			}
			i++
			continue
		}

		switch c {
		case '\\':
			// Copy the escape pair verbatim.
			b.WriteByte(c)
			if i+1 < len(s) {
				b.WriteByte(s[i+1])
				i += 2
			} else {
				i++
			}
		case '"':
			if legacyQuoteClosesString(s, i) {
				b.WriteByte(c)
				inString = false
			} else {
				// Interior quote: escape it and stay in the string.
				b.WriteString(`\"`)
				changed = true
			}
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}

	if !changed {
		return s
	}
	return b.String()
}

// quoteBareLegacyKey reports whether the object boundary at s[i] (`{` or `,`)
// opens a key whose opening quote is missing, as in `{label":"x"` or the array
// form `,{label":"x"`. On a match it returns the repaired prefix (the boundary,
// any intervening brace, the quoted name, and the whitespace between them) plus
// the offset just past the name's closing quote, so the caller resumes at the
// `:`.
func quoteBareLegacyKey(s string, i int) (string, int, bool) {
	j := i + 1
	// Whitespace, then — only when the boundary was a comma — one brace: an
	// array element boundary reads `,{label":`, so the key follows the brace
	// rather than the comma. A `{` boundary is itself the brace.
	j = skipLegacyJSONSpace(s, j)
	if s[i] == ',' && j < len(s) && s[j] == '{' {
		j++
	}
	braceEnd := j

	j = skipLegacyJSONSpace(s, j)
	if j >= len(s) || !isLegacyKeyStart(s[j]) {
		return "", 0, false
	}
	start := j
	for j < len(s) && isLegacyKeyChar(s[j]) {
		j++
	}
	name := s[start:j]

	// The key lost its opening quote. Two shapes occur: the closing quote
	// survives (`label":`) or it was lost too (`label:`). Both are repaired by
	// quoting the name; in the first case the stray closing quote is consumed.
	quoteStart := j
	j = skipLegacyJSONSpace(s, j)
	next := j
	switch {
	case j < len(s) && s[j] == '"':
		next = j + 1
	case j < len(s) && s[j] == ':':
	default:
		return "", 0, false
	}

	// s[i:braceEnd] is the boundary plus any brace; s[braceEnd:start] the
	// whitespace before the name; s[quoteStart:j] the whitespace after it.
	prefix := s[i:braceEnd] + s[braceEnd:start] + `"` + name + `"` + s[quoteStart:j]
	return prefix, next, true
}

// skipLegacyJSONSpace returns the index of the first non-whitespace byte at or
// after j.
func skipLegacyJSONSpace(s string, j int) int {
	for j < len(s) && isLegacyJSONSpace(s[j]) {
		j++
	}
	return j
}

// legacyQuoteClosesString reports whether the quote at s[i] ends the string it
// is in. It ends the string when the next non-space byte is structural — a
// colon (the value just closed was an object key), a comma, or a closing
// brace/bracket. Anything else means the quote was interior text.
func legacyQuoteClosesString(s string, i int) bool {
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

// isLegacyJSONSpace reports whether c is whitespace JSON permits around tokens.
func isLegacyJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// isLegacyKeyStart reports whether c may begin an unquoted JSON key.
func isLegacyKeyStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isLegacyKeyChar reports whether c may continue an unquoted JSON key.
func isLegacyKeyChar(c byte) bool {
	return isLegacyKeyStart(c) || (c >= '0' && c <= '9')
}

var (
	reLegacyJSONQuestion = regexp.MustCompile(`"question"\s*:\s*"([^"]*)"`)
	reLegacyJSONHeader   = regexp.MustCompile(`"header"\s*:\s*"([^"]*)"`)
	reLegacyJSONLabel    = regexp.MustCompile(`"label"\s*:\s*"([^"]*)"`)
	reLegacyJSONDesc     = regexp.MustCompile(`"description"\s*:\s*"([^"]*)"`)
)

// salvageLegacyJSON renders a JSON payload that does not decode (production
// data contains a stray `}` and a bare token). Its field values are still
// intact, so they are pulled out by name rather than left as raw JSON.
//
// Returns "" when no field could be recovered.
func salvageLegacyJSON(inner string) string {
	labels := reLegacyJSONLabel.FindAllStringSubmatch(inner, -1)
	descs := reLegacyJSONDesc.FindAllStringSubmatch(inner, -1)
	question := firstLegacySubmatch(reLegacyJSONQuestion, inner)
	header := firstLegacySubmatch(reLegacyJSONHeader, inner)
	if question == "" && len(labels) == 0 {
		return ""
	}

	var b strings.Builder
	if header != "" {
		b.WriteString("**" + header + "**\n")
	}
	if question != "" {
		b.WriteString(question + "\n")
	}
	for i, m := range labels {
		label := cleanLegacyText(m[1])
		if label == "" {
			continue
		}
		desc := ""
		if i < len(descs) {
			desc = cleanLegacyText(descs[i][1])
		}
		if desc != "" && desc != label {
			b.WriteString("- " + label + " — " + desc + "\n")
		} else {
			b.WriteString("- " + label + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// firstLegacySubmatch returns the cleaned first capture group, or "".
func firstLegacySubmatch(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return cleanLegacyText(m[1])
	}
	return ""
}
