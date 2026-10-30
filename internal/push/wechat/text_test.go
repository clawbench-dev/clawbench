package wechat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripMarkdownForWeChat_Markers(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"heading", "### 标题\n正文", "标题\n正文"},
		{"bold", "**加粗**文本", "加粗文本"},
		{"italic", "*斜体*文本", "斜体文本"},
		{"underscore", "__加粗__与_斜体_", "加粗与斜体"},
		{"strikethrough", "~~删除~~", "删除"},
		{"inline code", "运行 `npm test` 即可", "运行 npm test 即可"},
		{"hrule", "上面\n\n---\n\n下面", "上面\n\n下面"},
		{"blockquote", "> 引用内容", "引用内容"},
		{"table separator", "| a | b |\n|---|---|\n| 1 | 2 |", "| a | b |\n| 1 | 2 |"},
		{"link keeps label and target", "见[文档](https://example.com/x)", "见文档 (https://example.com/x)"},
		{"collapses blank runs", "a\n\n\n\nb", "a\n\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripMarkdownForWeChat(tt.in))
		})
	}
}

// TestStripMarkdownForWeChat_KeepsURLs is the guard on the decision not to
// reuse summarize.StripMarkdown: that function deletes bare URLs entirely
// (reBareURL → ""), which would strip the detail link out of every forge
// notification. WeChat renders on screen, so links must survive.
func TestStripMarkdownForWeChat_KeepsURLs(t *testing.T) {
	in := "查看详情：https://github.com/o/r/pull/42"
	assert.Equal(t, in, stripMarkdownForWeChat(in),
		"a bare URL must survive — summarize.StripMarkdown would delete it")
}

// TestStripMarkdownForWeChat_KeepsCodeBody is the second half of the same
// guard: summarize.StripMarkdown removes whole fenced blocks, but a code answer
// is the point of the message.
func TestStripMarkdownForWeChat_KeepsCodeBody(t *testing.T) {
	in := "修复如下：\n```go\nfunc main() {}\n```\n完毕"
	got := stripMarkdownForWeChat(in)
	assert.Contains(t, got, "func main() {}", "code body must survive")
	assert.NotContains(t, got, "```", "fence markers must go")
	assert.Contains(t, got, "修复如下：")
	assert.Contains(t, got, "完毕")
}

// TestStripMarkdownForWeChat_CodeIsNotMangled pins the placeholder pass: the
// emphasis/heading strippers must not touch fenced content, or a code answer
// arrives rewritten (`__init__` → `init`, `x ** 2` → `x  2`, `# deps` → `deps`).
func TestStripMarkdownForWeChat_CodeIsNotMangled(t *testing.T) {
	in := "```python\n" +
		"def __init__(self):\n" +
		"    if x ** 2 > n:\n" +
		"        foo_bar = 1  # note\n" +
		"# install deps\n" +
		"```\n"
	got := stripMarkdownForWeChat(in)

	for _, want := range []string{"def __init__(self):", "x ** 2 > n:", "foo_bar = 1  # note", "# install deps"} {
		assert.Contains(t, got, want, "fenced content must be verbatim")
	}
}

// TestStripMarkdownForWeChat_KeepsLinkTarget is the guard on the reason this
// stripper exists at all: service/forge_notify.go appends the detail link as
// "[查看详情](https://…)", and dropping the target leaves the user a bare label
// with nothing to tap.
func TestStripMarkdownForWeChat_KeepsLinkTarget(t *testing.T) {
	got := stripMarkdownForWeChat("有新事件\n\n[查看详情](https://github.com/o/r/pull/42)")
	assert.Contains(t, got, "https://github.com/o/r/pull/42",
		"the link target must survive; DingTalk and Feishu both keep it")
	assert.Contains(t, got, "查看详情")
}

func TestTruncateForWeChat(t *testing.T) {
	assert.Equal(t, "", truncateForWeChat(""))

	short := "短文本"
	assert.Equal(t, short, truncateForWeChat(short))

	// 2000 runes is the cap; anything longer is cut at a rune boundary and
	// marked with an ellipsis. Counting runes (not bytes) matters: a CJK
	// string of 2000 characters is 6000 bytes and must not be cut early.
	long := strings.Repeat("字", wechatPreviewMaxRunes+50)
	got := truncateForWeChat(long)
	assert.True(t, strings.HasSuffix(got, "…"))
	assert.Equal(t, wechatPreviewMaxRunes+1, len([]rune(got)),
		"cap is %d runes plus the ellipsis", wechatPreviewMaxRunes)

	exact := strings.Repeat("字", wechatPreviewMaxRunes)
	assert.Equal(t, exact, truncateForWeChat(exact),
		"exactly at the cap must not be truncated")
}

// TestTruncateForWeChat_StripsBeforeTruncating pins the ordering: the budget
// measures what is sent, so a message that is over the cap only because of
// Markdown markers must not be truncated at all.
func TestTruncateForWeChat_StripsBeforeTruncating(t *testing.T) {
	// 1999 visible runes wrapped in ** = 2003 raw runes (over the cap), but
	// only 1999 after stripping — so a correct implementation returns the body
	// untouched while a truncate-first one would cut it.
	body := strings.Repeat("字", wechatPreviewMaxRunes-1)
	in := "**" + body + "**"
	assert.Greater(t, len([]rune(in)), wechatPreviewMaxRunes,
		"fixture must exceed the cap before stripping or the test proves nothing")
	assert.Equal(t, body, truncateForWeChat(in),
		"stripping must happen before the length check")
}
