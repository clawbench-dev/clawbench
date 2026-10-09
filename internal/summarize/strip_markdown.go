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
	// Inline math with $ delimiters. Two guards keep currency intact:
	//   1. the body must OPEN with a letter or a backslash command ("$x_1$",
	//      "$\alpha$"); a currency amount opens with a digit ("$5"), so it is
	//      never mistaken for math; and
	//   2. the body must carry a math signal (backslash, _, ^, {}) — a plain
	//      "$word$" is left alone.
	// The body is length-bounded so an unmatched $ in prose cannot swallow a
	// long span up to the next $.
	reInlineMathDollar = regexp.MustCompile(`\$(?:\\[^$\n]{0,40}|[A-Za-z][^$\n]{0,40}[\\_^{}][^$\n]{0,40})\$`)
	// Residual LaTeX commands (e.g. \frac{a}{b}) outside math delimiters. A
	// WHITELIST, not `\\[a-zA-Z]+`: the latter would eat Windows path segments
	// ("C:\Users\test\file.txt" → "C:.txt") since \Users / \test / \file look
	// like commands. The trailing \b stops a whitelisted name from matching a
	// mere PREFIX of a path segment: "\pictures" / "\todo" / "\internal" /
	// "\summary" must survive, so \pi / \to / \int / \sum may only match when
	// followed by a non-word character (or end of input). RE2 has no lookahead,
	// so a word-boundary assertion is the tool here.
	reLatexCommand = regexp.MustCompile(`\\(?:frac|dfrac|tfrac|sqrt|sum|prod|int|oint|alpha|beta|gamma|delta|epsilon|varepsilon|zeta|eta|theta|vartheta|iota|kappa|lambda|mu|nu|xi|pi|rho|sigma|tau|upsilon|phi|varphi|chi|psi|omega|Gamma|Delta|Theta|Lambda|Xi|Pi|Sigma|Phi|Psi|Omega|times|cdot|div|pm|mp|leq|geq|neq|approx|equiv|propto|infty|partial|nabla|forall|exists|notin|subset|supset|cup|cap|vec|hat|bar|tilde|overline|underline|begin|end|text|mathrm|mathbf|mathbb|mathcal|left|right|quad|qquad|ldots|cdots|dots|to|rightarrow|leftarrow|mapsto)\b(?:\{[^{}]*\})*`)

	reCodeBlock = regexp.MustCompile("(?s)```.*?```")
	// reCodeFenceTail matches an UNCLOSED opening fence at the START of a line;
	// everything from it to the end is treated as code and dropped (streaming
	// truncation commonly cuts mid-block, leaving no closing fence for
	// reCodeBlock to match). The line-start anchor matters: a fence written
	// mid-prose ("use ``` to open a fence") is not an opening fence and must not
	// swallow the rest of the message.
	reCodeFenceTail = regexp.MustCompile("(?ms)^[ \t]*```.*")
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
	// reOrphanMarker removes a RUN of emphasis markers (2+ `*` or `~`) that
	// survived because its partner was truncated mid-message. A doubled marker
	// is never literal prose, so this is safe — unlike a single `*`/`_`, which
	// must be left alone (multiplication, identifiers, snake_case).
	reOrphanMarker = regexp.MustCompile(`\*{2,}|~{2,}`)
	reHTMLTag      = regexp.MustCompile(`<[^>]+>`)
	reXMLTag       = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	reAutolink     = regexp.MustCompile(`<([^>]+)>`)
	reFootnoteRef  = regexp.MustCompile(`\[\^[^\]]+\]`)
	reFootnoteDef  = regexp.MustCompile(`(?m)^\[\^[^\]]+\]:\s+.*$`)
	// reEmojiShortcode matches a shortcode whose body STARTS with a letter or
	// underscore ("smile", "_ok_") or is exactly a sign+digits ("+1", "-1").
	// Requiring that start keeps time ranges ("10:30-11:45" → body "30-11"
	// starts with a digit) and ports ("host:8080") from being eaten, while the
	// old `:[a-zA-Z0-9_+-]+:` swallowed both. Digit-only shortcodes (":100:")
	// are rare enough to leave in prose.
	reEmojiShortcode  = regexp.MustCompile(`:(?:[a-zA-Z_][a-zA-Z0-9_+-]*|[+-][0-9]+):`)
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
	text = stripUnderscoreEmphasis(text)
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

	// Phase 7: Drop emphasis-marker RUNS orphaned by a truncated message
	// ("**bold truncated" → "bold truncated"). Deliberately narrow: single
	// `*`/`_` are left alone so multiplication and identifiers survive.
	text = reOrphanMarker.ReplaceAllString(text, "")

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

// stripUnderscoreEmphasis unwraps `_emphasis_` while preserving identifiers
// like `user_id` / `file_name`. The surrounding-character guards mean a single
// regex pass consumes the separator that would anchor the NEXT span, so
// "_a_ _b_" left the second span untouched ("a _b_"). Looping until the
// underscore count stops falling fixes adjacent spans; it terminates because
// every iteration that changes the text strictly removes underscores.
func stripUnderscoreEmphasis(text string) string {
	for {
		next := reItalicUnder.ReplaceAllString(text, "$1$2$3")
		if next == text {
			return text
		}
		text = next
	}
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
