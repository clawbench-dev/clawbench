package summarize

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Pre-compiled regexes for StripMarkdown.
var (
	reCodeBlock = regexp.MustCompile("(?s)```.*?```")
	// reAskQuestion matches <ask-question>...</ask-question> blocks.
	// The inner content is XML with <item>, <question>, <option> etc. that must be
	// preserved for TTS summarization.
	reAskQuestion    = regexp.MustCompile(`(?s)<ask-question>\s*(.*?)\s*</ask-question>`)
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

	// Phase 0.5: Preserve <ask-question> structured question content.
	// These contain XML with questions/options that should be spoken aloud.
	// Extract the content before code-block stripping removes it.
	// Convert <ask-question><item>...</item></ask-question> into
	// a plain-text summary of the questions and options.
	text = reAskQuestion.ReplaceAllStringFunc(text, preserveAskQuestion)

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

// Pre-compiled regexes for XML ask-question parsing.
var (
	reItem     = regexp.MustCompile("(?s)<item>(.*?)</item>")
	reHeader   = regexp.MustCompile("(?s)<header>(.*?)</header>")
	reQuestion = regexp.MustCompile("(?s)<question>(.*?)</question>")
	reOption   = regexp.MustCompile("(?s)<option>(.*?)</option>")
	reLabel    = regexp.MustCompile("(?s)<label>(.*?)</label>")
	reDesc     = regexp.MustCompile("(?s)<description>(.*?)</description>")
)

// preserveAskQuestion converts a <ask-question>...</ask-question> block
// (whose content is XML with <item> child elements, or JSON with "questions" array)
// into a plain-text summary suitable for TTS. If the content cannot be parsed,
// the raw content is returned as-is so that the summarizer can still see it.
func preserveAskQuestion(match string) string {
	sub := reAskQuestion.FindStringSubmatch(match)
	if len(sub) < 2 {
		return match
	}
	content := strings.TrimSpace(sub[1])

	// Try XML format first
	items := reItem.FindAllStringSubmatch(content, -1)
	if len(items) > 0 {
		return preserveAskQuestionXML(items)
	}

	// Try JSON format
	return preserveAskQuestionJSON(content)
}

// preserveAskQuestionXML converts XML-format ask-question items into plain text for TTS.
func preserveAskQuestionXML(items [][]string) string {
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			b.WriteString(" ")
		}
		itemContent := item[1]

		qMatch := reQuestion.FindStringSubmatch(itemContent)
		if len(qMatch) >= 2 {
			b.WriteString(strings.TrimSpace(qMatch[1]))
		}

		hMatch := reHeader.FindStringSubmatch(itemContent)
		if len(hMatch) >= 2 && strings.TrimSpace(hMatch[1]) != "" {
			fmt.Fprintf(&b, " (%s)", strings.TrimSpace(hMatch[1]))
		}

		opts := reOption.FindAllStringSubmatch(itemContent, -1)
		if len(opts) > 0 {
			b.WriteString(": ")
			formatXMLOptions(&b, opts)
		}
	}
	return b.String()
}

// formatXMLOptions writes XML option labels and descriptions to the builder.
func formatXMLOptions(b *strings.Builder, opts [][]string) {
	for j, opt := range opts {
		if j > 0 {
			b.WriteString(", ")
		}
		labelMatch := reLabel.FindStringSubmatch(opt[1])
		descMatch := reDesc.FindStringSubmatch(opt[1])
		if len(labelMatch) >= 2 {
			b.WriteString(strings.TrimSpace(labelMatch[1]))
		}
		if len(descMatch) >= 2 {
			desc := strings.TrimSpace(descMatch[1])
			if desc != "" && (len(labelMatch) < 2 || desc != strings.TrimSpace(labelMatch[1])) {
				fmt.Fprintf(b, " — %s", desc)
			}
		}
	}
}

// preserveAskQuestionJSON converts JSON-format ask-question content into plain text for TTS.
func preserveAskQuestionJSON(jsonContent string) string {
	var data struct {
		Questions []struct {
			Header      string `json:"header"`
			Question    string `json:"question"`
			MultiSelect bool   `json:"multiSelect"`
			Options     []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal([]byte(jsonContent), &data); err != nil || len(data.Questions) == 0 {
		return stripXMLTags(jsonContent)
	}

	var b strings.Builder
	for i, q := range data.Questions {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(q.Question)
		if q.Header != "" {
			fmt.Fprintf(&b, " (%s)", q.Header)
		}
		if len(q.Options) > 0 {
			b.WriteString(": ")
			for j, opt := range q.Options {
				if j > 0 {
					b.WriteString(", ")
				}
				b.WriteString(opt.Label)
				if opt.Description != "" && opt.Description != opt.Label {
					fmt.Fprintf(&b, " — %s", opt.Description)
				}
			}
		}
	}
	return b.String()
}

// stripXMLTags removes all XML/HTML tags from text.
func stripXMLTags(text string) string {
	return reXMLTag.ReplaceAllString(text, "")
}
