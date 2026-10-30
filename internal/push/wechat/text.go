package wechat

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// wechatPreviewMaxRunes caps the response preview in a WeChat push.
//
// WeChat renders plain text with no card wrapper to budget around, and its
// single-message ceiling is far below the ~20000 bytes DingTalk/Feishu allow.
// 2000 runes (~6000 bytes for CJK) keeps a full answer readable on a phone
// while leaving room for the template header and reply hint.
const wechatPreviewMaxRunes = 2000

var (
	// reHeading strips ATX headings but keeps their text.
	reHeading = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]*`)
	// reTableSep removes the |---|---| separator row of a Markdown table,
	// including its line break, so the header and body rows stay adjacent
	// instead of being split by a blank line.
	//
	// The cell pattern is spelled out rather than using a loose [\s:|-]+ class:
	// that class also matches "|", so it would greedily swallow a whole data row
	// ("| a | b |") and delete it.
	reTableSep = regexp.MustCompile(`(?m)^[ \t]*\|(?:[ \t]*:?-+:?[ \t]*\|)+[ \t]*\n?`)
	// reBlockquote strips the leading "> " marker, keeping the quoted text.
	reBlockquote = regexp.MustCompile(`(?m)^[ \t]{0,3}>[ \t]?`)
	// reHRule removes horizontal rules.
	reHRule = regexp.MustCompile(`(?m)^[ \t]{0,3}(?:-{3,}|\*{3,}|_{3,})[ \t]*$`)
	// reBoldItalic strips emphasis markers (**, __, *, _, ~~).
	reBoldItalic = regexp.MustCompile(`\*{1,3}|_{1,3}|~~`)
	// reInlineCode strips single-backtick spans' delimiters, keeping the code.
	reInlineCode = regexp.MustCompile("`+")
	// reLink keeps the label of [label](url) AND the target. Dropping the target
	// would strip the detail link out of every forge notification
	// (service/forge_notify.go appends "\n\n[查看详情](https://…)"): the user would
	// get a bare "查看详情" with nothing to tap, which is exactly the failure this
	// custom stripper exists to avoid.
	reLink = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
	// reMultiBlank collapses runs of blank lines left by the removals above.
	reMultiBlank = regexp.MustCompile(`\n{3,}`)
)

// stripMarkdownForWeChat flattens Markdown into the plain text WeChat renders.
//
// Deliberately NOT summarize.StripMarkdown: that one is a TTS contract, and it
// deletes bare URLs (reBareURL → "") plus entire fenced code blocks. WeChat is
// read on a screen, so dropping a forge notification's detail link or a code
// answer's body would be a regression, not a cleanup. Here URLs and code block
// contents survive; only the markers go.
func stripMarkdownForWeChat(text string) string {
	if text == "" {
		return ""
	}

	// Pull fenced blocks out first. The emphasis/heading passes below would
	// otherwise rewrite code: `def __init__` → `def init`, `x ** 2` → `x  2`,
	// `# install deps` → `install deps`. A placeholder (NUL-wrapped index) is
	// safe because no marker regex can match it.
	withoutCode, fenced := extractFencedBlocks(text)

	result := withoutCode

	result = reHRule.ReplaceAllString(result, "")
	result = reHeading.ReplaceAllString(result, "")
	result = reBlockquote.ReplaceAllString(result, "")
	result = reTableSep.ReplaceAllString(result, "")
	result = reLink.ReplaceAllString(result, "$1 ($2)")
	result = reBoldItalic.ReplaceAllString(result, "")
	result = reInlineCode.ReplaceAllString(result, "")

	result = reMultiBlank.ReplaceAllString(result, "\n\n")
	result = strings.TrimSpace(result)

	// Restore each block: the fence lines go, the body stays verbatim.
	for i, block := range fenced {
		body := reFenceLine.ReplaceAllString(block, "")
		body = strings.Trim(body, "\n")
		result = strings.ReplaceAll(result, fmt.Sprintf("\x00%d\x00", i), body)
	}
	return result
}

// reFenceLine matches an opening or closing ``` fence line (with optional
// language tag), leaving the fenced body intact.
var reFenceLine = regexp.MustCompile("(?m)^[ \t]*```[^\n]*$")

// extractFencedBlocks replaces every fenced code block with a NUL-wrapped
// placeholder and returns the blocks themselves.
//
// Line-scanned rather than regex-matched: an unclosed fence (streaming
// truncation) has no matching close, and expressing "to the end of the text OR
// the closing fence" in a single pattern is both fragile and hard to read.
func extractFencedBlocks(text string) (string, []string) {
	if !strings.Contains(text, "```") {
		return text, nil
	}

	var (
		out    strings.Builder
		blocks []string
		cur    strings.Builder
		inCode bool
	)

	for _, line := range strings.SplitAfter(text, "\n") {
		isFence := strings.HasPrefix(strings.TrimLeft(line, " \t"), "```")
		switch {
		case isFence && !inCode:
			inCode = true
			cur.Reset()
			cur.WriteString(line)
		case isFence && inCode:
			cur.WriteString(line)
			blocks = append(blocks, cur.String())
			fmt.Fprintf(&out, "\x00%d\x00", len(blocks)-1)
			inCode = false
		case inCode:
			cur.WriteString(line)
		default:
			out.WriteString(line)
		}
	}

	// An unclosed fence still counts as a block: its body is code either way.
	if inCode {
		blocks = append(blocks, cur.String())
		fmt.Fprintf(&out, "\x00%d\x00", len(blocks)-1)
	}

	return out.String(), blocks
}

// truncateForWeChat applies stripMarkdownForWeChat and then truncates to
// wechatPreviewMaxRunes.
//
// Stripping runs first on purpose: the budget should measure what is actually
// sent, and cutting before stripping could leave a dangling "**" in the output.
func truncateForWeChat(text string) string {
	plain := stripMarkdownForWeChat(text)
	if plain == "" {
		return ""
	}
	if utf8.RuneCountInString(plain) <= wechatPreviewMaxRunes {
		return plain
	}
	return string([]rune(plain)[:wechatPreviewMaxRunes]) + "…"
}
