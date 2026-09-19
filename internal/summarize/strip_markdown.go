package summarize

import (
	"regexp"
	"strings"

	"clawbench/internal/askquestion"
)

// Pre-compiled regexes for StripMarkdown.
var (
	reCodeBlock      = regexp.MustCompile("(?s)```.*?```")
	reInlineCode     = regexp.MustCompile("`[^`]+`")
	reBoldAsterisk   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reBoldUnderscore = regexp.MustCompile(`__([^_]+)__`)
	reItalicAsterisk = regexp.MustCompile(`\*([^*]+)\*`)
	reItalicUnder    = regexp.MustCompile(`_([^_]+)_`)
	reHeaders        = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reLinks          = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reImages         = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	reHorizontalRule = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`)
	reMultiBlank     = regexp.MustCompile(`\n{3,}`)
	// Extended markdown patterns for thorough TTS cleaning
	reStrikethrough   = regexp.MustCompile(`~~([^~]+)~~`)
	reBlockquote      = regexp.MustCompile(`(?m)^>\s?`)
	reUnorderedList   = regexp.MustCompile(`(?m)^[\s]*[-*+]\s+`)
	reOrderedList     = regexp.MustCompile(`(?m)^[\s]*\d+\.\s+`)
	reTaskList        = regexp.MustCompile(`(?m)^[\s]*[-*+]\s+\[[ xX]\]\s*`)
	reTablePipe       = regexp.MustCompile(`\|`)
	reTableDivider    = regexp.MustCompile(`(?m)^[\s|]*([-:]+[\s|:-]*)+$`)
	reHTMLTag         = regexp.MustCompile(`<[^>]+>`)
	reXMLTag          = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	reAutolink        = regexp.MustCompile(`<([^>]+)>`)
	reFootnoteRef     = regexp.MustCompile(`\[\^[^\]]+\]`)
	reFootnoteDef     = regexp.MustCompile(`(?m)^\[\^[^\]]+\]:\s+.*$`)
	reEmojiShortcode  = regexp.MustCompile(`:[a-zA-Z0-9_+-]+:`)
	reBackslashEscape = regexp.MustCompile(`\\([\\` + "`" + `*_{}[\]()#+\-.!|~])`)
	// Angle-bracket URLs remaining after other stripping
	reBareURL = regexp.MustCompile(`https?://\S+`)
)

// InlineCodeMaxLen is the maximum content length (in runes) for inline code
// to be preserved (with backticks removed). Longer inline code is removed
// entirely — it typically contains code snippets not suitable for TTS.
// Configurable via config/config.yaml tts.inline_code_max_len.
var InlineCodeMaxLen = 100

// StripMarkdown removes common markdown formatting from text.
// Should be called on LLM output before passing to TTS synthesis.
func StripMarkdown(text string) string {
	// Phase 0: Resolve backslash escapes FIRST so that \* becomes *
	// and subsequent patterns can match the unescaped characters.
	text = reBackslashEscape.ReplaceAllString(text, "$1")

	// Phase 0.5: Preserve <clawbench-ask-question> structured question content.
	// These contain questions/options that should be spoken aloud.
	// Parsing is delegated to internal/askquestion, so TTS understands the same
	// payloads the UI does (unclosed tags, option attributes, the plural
	// <options> wrapper). An unparseable payload falls back to stripping its
	// tags, so raw XML is never spoken.
	text = replaceAskQuestions(text)

	// Phase 1: Remove block-level elements
	text = reCodeBlock.ReplaceAllString(text, "")
	text = reFootnoteDef.ReplaceAllString(text, "")
	text = reTableDivider.ReplaceAllString(text, "")
	text = reHTMLTag.ReplaceAllString(text, "")
	text = reXMLTag.ReplaceAllString(text, "")

	// Phase 2: Remove inline formatting — task lists before unordered lists
	text = reTaskList.ReplaceAllString(text, "")
	text = reUnorderedList.ReplaceAllString(text, "")
	text = reOrderedList.ReplaceAllString(text, "")
	text = reBlockquote.ReplaceAllString(text, "")
	text = reStrikethrough.ReplaceAllString(text, "$1")
	text = stripInlineCode(text)
	text = reBoldAsterisk.ReplaceAllString(text, "$1")
	text = reBoldUnderscore.ReplaceAllString(text, "$1")
	text = reItalicAsterisk.ReplaceAllString(text, "$1")
	text = reItalicUnder.ReplaceAllString(text, "$1")
	text = reHeaders.ReplaceAllString(text, "")
	text = reLinks.ReplaceAllString(text, "$1")
	text = reAutolink.ReplaceAllString(text, "$1")
	text = reImages.ReplaceAllString(text, "")
	text = reHorizontalRule.ReplaceAllString(text, "")
	text = reFootnoteRef.ReplaceAllString(text, "")
	text = reEmojiShortcode.ReplaceAllString(text, "")

	// Phase 3: Remove table pipes (after content extraction)
	text = reTablePipe.ReplaceAllString(text, "")

	// Phase 4: Remove bare URLs (not useful for TTS)
	text = reBareURL.ReplaceAllString(text, "")

	// Phase 5: Clean up whitespace
	text = reMultiBlank.ReplaceAllString(text, "\n\n")

	// Final sweep: remove any remaining stray markdown punctuation that
	// survived the structured passes (loose *, #, ~, backticks, \, etc.)
	text = stripResidualMarkdown(text)

	return strings.TrimSpace(text)
}

// stripResidualMarkdown removes leftover markdown special characters that
// the regex passes above may have missed (e.g. orphaned *, #, ~, `, |, []).
// It preserves Chinese/English letters, digits, and readable punctuation.
var reResidualMarkdown = regexp.MustCompile(`[\\#*~` + "`" + `|]`)

func stripResidualMarkdown(text string) string {
	return reResidualMarkdown.ReplaceAllString(text, "")
}

// stripInlineCode processes inline code spans (`xxx`).
// Short content (≤ InlineCodeMaxLen runes) keeps its text — these are typically
// variable names, command names, or short terms worth reading aloud.
// Long content is removed entirely — these are typically code snippets.
func stripInlineCode(text string) string {
	return reInlineCode.ReplaceAllStringFunc(text, func(match string) string {
		// match includes the backticks; content is match[1:len-1]
		content := match[1 : len(match)-1]
		if len([]rune(content)) <= InlineCodeMaxLen {
			return content
		}
		return ""
	})
}

// replaceAskQuestions converts every <clawbench-ask-question> block into spoken text.
//
// Parsing is delegated to internal/askquestion, so TTS understands the same
// payloads the UI does (unclosed tags, option attributes, the plural <options>
// wrapper). A parsed payload becomes a plain-language summary; an unparseable
// one keeps its inner text with the tags stripped, so raw XML is never spoken.
//
// A tag inside a code block is not located here; the code-block phase of
// StripMarkdown removes it wholesale.
func replaceAskQuestions(text string) string {
	matches := askquestion.Extract(text)
	if len(matches) == 0 {
		return text
	}
	var b strings.Builder
	prev := 0
	for _, m := range matches {
		if m.Start < prev || m.End > len(text) {
			continue
		}
		b.WriteString(text[prev:m.Start])
		if m.Parsed {
			b.WriteString(askquestion.PlainText(m.Items))
		} else {
			b.WriteString(stripXMLTags(m.Raw))
		}
		prev = m.End
	}
	b.WriteString(text[prev:])
	return b.String()
}

// stripXMLTags removes all XML/HTML tags from text.
func stripXMLTags(text string) string {
	return reXMLTag.ReplaceAllString(text, "")
}
