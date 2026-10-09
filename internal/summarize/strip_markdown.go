package summarize

import (
	"regexp"
	"strings"

	"clawbench/internal/askquestion"
)

// Pre-compiled regexes for StripMarkdown.
var (
	// LaTeX math (must run before the backslash-escape phase: \[ \( \) \] are
	// themselves in the escape set and would otherwise be unwrapped first).
	reDisplayMathDollar  = regexp.MustCompile(`(?s)\$\$.*?\$\$`)
	reDisplayMathBracket = regexp.MustCompile(`(?s)\\\[.*?\\\]`)
	reInlineMathParen    = regexp.MustCompile(`(?s)\\\(.*?\\\)`)
	// Inline math with $ delimiters is only stripped when the body carries a
	// math signal (backslash command, _, ^, {}). A bare "$5 and $10" (currency)
	// has none, so it survives — matching every $...$ would eat prices.
	reInlineMathDollar = regexp.MustCompile(`\$[^$\n]*[\\_^{}][^$\n]*\$`)
	// Residual LaTeX commands (e.g. \frac{a}{b}) outside math delimiters. A
	// WHITELIST, not `\\[a-zA-Z]+`: the latter would eat Windows path segments
	// ("C:\Users\test\file.txt" → "C:.txt") since \Users / \test / \file look
	// like commands.
	reLatexCommand = regexp.MustCompile(`\\(?:frac|dfrac|tfrac|sqrt|sum|prod|int|oint|alpha|beta|gamma|delta|epsilon|varepsilon|zeta|eta|theta|vartheta|iota|kappa|lambda|mu|nu|xi|pi|rho|sigma|tau|upsilon|phi|varphi|chi|psi|omega|Gamma|Delta|Theta|Lambda|Xi|Pi|Sigma|Phi|Psi|Omega|times|cdot|div|pm|mp|leq|geq|neq|approx|equiv|propto|infty|partial|nabla|forall|exists|notin|subset|supset|cup|cap|vec|hat|bar|tilde|overline|underline|begin|end|text|mathrm|mathbf|mathbb|mathcal|left|right|quad|qquad|ldots|cdots|dots|to|rightarrow|leftarrow|mapsto)(?:\{[^{}]*\})*`)

	reCodeBlock = regexp.MustCompile("(?s)```.*?```")
	// reCodeFenceTail matches an UNCLOSED opening fence; everything from it to
	// the end is treated as code and dropped (streaming truncation commonly
	// cuts mid-block, leaving no closing fence for reCodeBlock to match).
	reCodeFenceTail = regexp.MustCompile("(?s)```.*$")
	reInlineCode    = regexp.MustCompile("`[^`]+`")
	// reBoldAsterisk / reItalicAsterisk require the delimiters to hug their
	// content (no leading/trailing space), so "3 * 4 * 5" is not read as italic.
	reBoldAsterisk   = regexp.MustCompile(`\*\*([^*\s][^*]*?[^*\s]|\S)\*\*`)
	reBoldUnderscore = regexp.MustCompile(`__([^_\s][^_]*?[^_\s]|\S)__`)
	reItalicAsterisk = regexp.MustCompile(`\*([^*\s][^*]*?[^*\s]|\S)\*`)
	// reItalicUnder requires a non-word character (or line edge) on both sides of
	// the underscore pair, so identifiers like user_id / file_name survive while
	// a genuine _emphasis_ is still unwrapped.
	reItalicUnder    = regexp.MustCompile(`(^|[^0-9A-Za-z_])_([^_\s][^_]*?[^_\s]|[^_\s])_($|[^0-9A-Za-z_])`)
	reHeaders        = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reLinks          = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reImages         = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	reHorizontalRule = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`)
	reMultiBlank     = regexp.MustCompile(`\n{3,}`)
	// Extended markdown patterns for thorough TTS cleaning
	reStrikethrough = regexp.MustCompile(`~~([^~]+)~~`)
	reBlockquote    = regexp.MustCompile(`(?m)^>\s?`)
	reUnorderedList = regexp.MustCompile(`(?m)^[\s]*[-*+]\s+`)
	reOrderedList   = regexp.MustCompile(`(?m)^[\s]*\d+\.\s+`)
	reTaskList      = regexp.MustCompile(`(?m)^[\s]*[-*+]\s+\[[ xX]\]\s*`)
	reTablePipe     = regexp.MustCompile(`\|`)
	reTableDivider  = regexp.MustCompile(`(?m)^[\s|]*([-:]+[\s|:-]*)+$`)
	reHTMLTag       = regexp.MustCompile(`<[^>]+>`)
	reXMLTag        = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	reAutolink      = regexp.MustCompile(`<([^>]+)>`)
	reFootnoteRef   = regexp.MustCompile(`\[\^[^\]]+\]`)
	reFootnoteDef   = regexp.MustCompile(`(?m)^\[\^[^\]]+\]:\s+.*$`)
	// reEmojiShortcode requires the body to carry a letter or a +/- sign, so a
	// numeric "12:30:45" or a "host:8080" port is NOT mistaken for a shortcode
	// (the old `:[a-zA-Z0-9_+-]+:` ate both). Genuine digit-only shortcodes
	// (":100:") are rare enough to leave in prose.
	reEmojiShortcode  = regexp.MustCompile(`:[a-zA-Z0-9_+-]*(?:[a-zA-Z_]|[+-])[a-zA-Z0-9_+-]*:`)
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
	cleaned, _, _ := StripMarkdownStats(text)
	return cleaned
}

// StripMarkdownStats removes markdown formatting and additionally reports how
// much of the original text was unreadable (removed) versus kept as speakable
// prose. The two rune counts let a caller decide whether the cleaned text is
// coherent enough to speak directly, or whether it needs an LLM to stitch the
// fragments back into sentences (see AutoSummarizer).
//
// originalRunes is the rune count of the input; keptRunes is the rune count of
// the cleaned output. removedRunes = originalRunes - keptRunes (never negative).
func StripMarkdownStats(text string) (cleaned string, originalRunes, keptRunes int) {
	originalRunes = len([]rune(text))

	// Phase 0: strip math before backslash-unescaping, because \[ \( \) \] are
	// part of the escape set and would be unwrapped by the next phase.
	text = stripMath(text)

	// Phase 0.5: Resolve backslash escapes so that \* becomes *
	// and subsequent patterns can match the unescaped characters.
	text = reBackslashEscape.ReplaceAllString(text, "$1")

	// Phase 1: Preserve <clawbench-ask-question> structured question content.
	// These contain questions/options that should be spoken aloud.
	// Parsing is delegated to internal/askquestion, so TTS understands the same
	// payloads the UI does (unclosed tags, option attributes, the plural
	// <options> wrapper). An unparseable payload falls back to stripping its
	// tags, so raw XML is never spoken.
	text = replaceAskQuestions(text)

	// Phase 2: Remove block-level elements. Closed fences first; any ``` left
	// afterwards is an unclosed fence (streaming truncation), whose tail to the
	// end is dropped so a half-open code block never leaks its contents.
	text = reCodeBlock.ReplaceAllString(text, "")
	text = reCodeFenceTail.ReplaceAllString(text, "")
	text = reFootnoteDef.ReplaceAllString(text, "")
	text = reTableDivider.ReplaceAllString(text, "")
	text = reHTMLTag.ReplaceAllString(text, "")
	text = reXMLTag.ReplaceAllString(text, "")

	// Phase 3: Remove inline formatting — task lists before unordered lists
	text = reTaskList.ReplaceAllString(text, "")
	text = reUnorderedList.ReplaceAllString(text, "")
	text = reOrderedList.ReplaceAllString(text, "")
	text = reBlockquote.ReplaceAllString(text, "")
	text = reStrikethrough.ReplaceAllString(text, "$1")
	text = stripInlineCode(text)
	text = reBoldAsterisk.ReplaceAllString(text, "$1")
	text = reBoldUnderscore.ReplaceAllString(text, "$1")
	text = reItalicAsterisk.ReplaceAllString(text, "$1")
	text = reItalicUnder.ReplaceAllString(text, "$1$2$3")
	text = reHeaders.ReplaceAllString(text, "")
	text = reLinks.ReplaceAllString(text, "$1")
	text = reAutolink.ReplaceAllString(text, "$1")
	text = reImages.ReplaceAllString(text, "")
	text = reHorizontalRule.ReplaceAllString(text, "")
	text = reFootnoteRef.ReplaceAllString(text, "")
	text = reEmojiShortcode.ReplaceAllString(text, "")

	// Phase 4: Remove table pipes (after content extraction)
	text = reTablePipe.ReplaceAllString(text, "")

	// Phase 5: Remove bare URLs (not useful for TTS)
	text = reBareURL.ReplaceAllString(text, "")

	// Phase 6: Clean up whitespace
	text = reMultiBlank.ReplaceAllString(text, "\n\n")

	cleaned = strings.TrimSpace(text)
	keptRunes = len([]rune(cleaned))
	if keptRunes > originalRunes {
		keptRunes = originalRunes
	}
	return cleaned, originalRunes, keptRunes
}

// stripMath removes LaTeX/math notation that cannot be read aloud. Order
// matters: block delimiters before inline ones.
func stripMath(text string) string {
	text = reDisplayMathDollar.ReplaceAllString(text, "")
	text = reDisplayMathBracket.ReplaceAllString(text, "")
	text = reInlineMathParen.ReplaceAllString(text, "")
	text = reInlineMathDollar.ReplaceAllString(text, "")
	text = reLatexCommand.ReplaceAllString(text, "")
	return text
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
