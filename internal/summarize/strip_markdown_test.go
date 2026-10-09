package summarize

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- StripMarkdown tests ---

func TestStripMarkdown_CodeBlock(t *testing.T) {
	input := "Here is some code:\n```go\nfmt.Println(\"hello\")\n```\nAnd more text."
	result := StripMarkdown(input)
	assert.NotContains(t, result, "```")
	assert.NotContains(t, result, "fmt.Println")
	assert.Contains(t, result, "Here is some code")
	assert.Contains(t, result, "And more text")
}

func TestStripMarkdown_InlineCode(t *testing.T) {
	input := "Use the `fmt.Println` function to print."
	result := StripMarkdown(input)
	assert.NotContains(t, result, "`")
	assert.Contains(t, result, "fmt.Println")
	assert.Contains(t, result, "Use the")
	assert.Contains(t, result, "function to print")
}

func TestStripMarkdown_InlineCode_Short(t *testing.T) {
	input := "设置 `GOPATH` 环境变量，然后运行 `go build`。"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "`")
	assert.Contains(t, result, "GOPATH")
	assert.Contains(t, result, "go build")
}

func TestStripMarkdown_InlineCode_Long(t *testing.T) {
	longCode := "for i := 0; i < len(items); i++ { if items[i].IsActive { process(items[i]) } else { skip(items[i]) } } // handle active items"
	input := "代码如下 `" + longCode + "` 继续文本。"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "`")
	assert.NotContains(t, result, "process")
	assert.Contains(t, result, "继续文本")
}

func TestStripMarkdown_Bold(t *testing.T) {
	input := "This is **bold** and __also bold__ text."
	result := StripMarkdown(input)
	assert.Equal(t, "This is bold and also bold text.", result)
}

func TestStripMarkdown_Italic(t *testing.T) {
	input := "This is *italic* and _also italic_ text."
	result := StripMarkdown(input)
	assert.Equal(t, "This is italic and also italic text.", result)
}

func TestStripMarkdown_Headers(t *testing.T) {
	input := "# Title\n## Subtitle\n### H3\nNormal text"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "#")
	assert.Contains(t, result, "Title")
	assert.Contains(t, result, "Normal text")
}

func TestStripMarkdown_Links(t *testing.T) {
	input := "Visit [the website](https://example.com) for details."
	result := StripMarkdown(input)
	assert.NotContains(t, result, "https://")
	assert.NotContains(t, result, "(")
	assert.Contains(t, result, "Visit")
	assert.Contains(t, result, "the website")
	assert.Contains(t, result, "for details")
}

func TestStripMarkdown_Images(t *testing.T) {
	input := "Here is an image: ![alt text](image.png) and text after."
	result := StripMarkdown(input)
	assert.NotContains(t, result, "![]")
	assert.NotContains(t, result, "image.png")
	assert.Contains(t, result, "Here is an image")
	assert.Contains(t, result, "and text after")
}

func TestStripMarkdown_HorizontalRule(t *testing.T) {
	input := "Above\n---\nBelow"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "---")
	assert.Contains(t, result, "Above")
	assert.Contains(t, result, "Below")
}

func TestStripMarkdown_MultipleBlankLines(t *testing.T) {
	input := "A\n\n\n\n\nB"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "\n\n\n")
	assert.Contains(t, result, "A")
	assert.Contains(t, result, "B")
}

func TestStripMarkdown_PlainText(t *testing.T) {
	input := "Just plain text without any formatting."
	result := StripMarkdown(input)
	assert.Equal(t, input, result)
}

func TestStripMarkdown_EmptyString(t *testing.T) {
	result := StripMarkdown("")
	assert.Equal(t, "", result)
}

func TestStripMarkdown_Strikethrough(t *testing.T) {
	input := "This is ~~deleted~~ text."
	result := StripMarkdown(input)
	assert.Equal(t, "This is deleted text.", result)
}

func TestStripMarkdown_Blockquote(t *testing.T) {
	input := "> 引用文本\n> 另一行引用\n正常文本"
	result := StripMarkdown(input)
	assert.NotContains(t, result, ">")
	assert.Contains(t, result, "引用文本")
	assert.Contains(t, result, "另一行引用")
	assert.Contains(t, result, "正常文本")
}

func TestStripMarkdown_UnorderedList(t *testing.T) {
	input := "- 项目一\n- 项目二\n* 项目三\n+ 项目四"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "- ")
	assert.NotContains(t, result, "* ")
	assert.NotContains(t, result, "+ ")
	assert.Contains(t, result, "项目一")
	assert.Contains(t, result, "项目四")
}

func TestStripMarkdown_OrderedList(t *testing.T) {
	input := "1. 第一项\n2. 第二项\n10. 第十项"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "1.")
	assert.NotContains(t, result, "10.")
	assert.Contains(t, result, "第一项")
	assert.Contains(t, result, "第十项")
}

func TestStripMarkdown_TaskList(t *testing.T) {
	input := "- [x] 已完成\n- [ ] 未完成"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "[x]")
	assert.NotContains(t, result, "[ ]")
	assert.NotContains(t, result, "- ")
	assert.Contains(t, result, "已完成")
	assert.Contains(t, result, "未完成")
}

func TestStripMarkdown_Table(t *testing.T) {
	input := "| 列1 | 列2 |\n| --- | --- |\n| 值1 | 值2 |"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "|")
	assert.NotContains(t, result, "---")
	assert.Contains(t, result, "列1")
	assert.Contains(t, result, "值1")
}

func TestStripMarkdown_HTMLTags(t *testing.T) {
	input := "<b>加粗</b>和<br>换行"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "<")
	assert.NotContains(t, result, ">")
	assert.Contains(t, result, "加粗")
	assert.Contains(t, result, "换行")
}

func TestStripMarkdown_XMLTags(t *testing.T) {
	input := "<tool_use>工具调用</tool_use>"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "<")
	assert.NotContains(t, result, ">")
	assert.Contains(t, result, "工具调用")
}

func TestStripMarkdown_EmojiShortcode(t *testing.T) {
	input := "开心 :smile: 和 :+1: 继续"
	result := StripMarkdown(input)
	assert.NotContains(t, result, ":smile:")
	assert.NotContains(t, result, ":+1:")
	assert.Contains(t, result, "开心")
	assert.Contains(t, result, "继续")
}

func TestStripMarkdown_Footnote(t *testing.T) {
	input := "正文[^1]\n[^1]: 脚注内容"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "[^1]")
	assert.NotContains(t, result, "脚注内容")
	assert.Contains(t, result, "正文")
}

func TestStripMarkdown_EscapedChars(t *testing.T) {
	input := `\*不斜体\*和\#不标题`
	result := StripMarkdown(input)
	assert.NotContains(t, result, "\\")
	assert.Contains(t, result, "不斜体")
	assert.Contains(t, result, "不标题")
}

func TestStripMarkdown_BareURL(t *testing.T) {
	input := "访问 https://example.com 查看详情"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "https://")
	assert.Contains(t, result, "访问")
	assert.Contains(t, result, "查看详情")
}

func TestStripMarkdown_Autolink(t *testing.T) {
	input := "点击 <https://example.com> 查看详情"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "<")
	assert.NotContains(t, result, "https://")
	assert.Contains(t, result, "点击")
	assert.Contains(t, result, "查看详情")
}

func TestStripMarkdown_ComplexMix(t *testing.T) {
	input := `# Project Setup

First, install **dependencies** using ` + "`npm install`" + `.

Then configure the [settings](/config):

` + "```json" + `
{
  "port": 3000
}
` + "```" + `

---

Run with *npm start*.`
	result := StripMarkdown(input)
	assert.NotContains(t, result, "#")
	assert.NotContains(t, result, "```")
	assert.NotContains(t, result, "`")
	assert.NotContains(t, result, "**")
	assert.NotContains(t, result, "http")
	assert.Contains(t, result, "Project Setup")
	assert.Contains(t, result, "dependencies")
	assert.Contains(t, result, "settings")
}

// --- Constants ---

func TestConstants(t *testing.T) {
	assert.Equal(t, 0, MaxTextRunes)
	assert.Equal(t, 16, CacheKeyHexLen)
}

// --- Regression: literal technical characters must survive stripping ---
// These guard against the former blunt residual sweep that deleted every
// \ # * ~ ` | character regardless of whether it was markdown.

func TestStripMarkdown_PreservesSnakeCaseIdentifiers(t *testing.T) {
	// Two underscores on one line used to be paired as _emphasis_ and eaten.
	in := "变量 user_id 和 file_name 要保留。"
	assert.Equal(t, in, StripMarkdown(in))

	in2 := "字段 a_b 与 c_d 与 e_f"
	assert.Equal(t, in2, StripMarkdown(in2))
}

func TestStripMarkdown_PreservesMultiplicationAsterisks(t *testing.T) {
	in := "计算 3 * 4 * 5 的结果"
	assert.Equal(t, in, StripMarkdown(in))

	in2 := "面积 = 3 * 4 平方米"
	assert.Equal(t, in2, StripMarkdown(in2))
}

func TestStripMarkdown_PreservesTimesAndPorts(t *testing.T) {
	// The old emoji-shortcode regex `:[a-zA-Z0-9_+-]+:` ate ":30:" and ":8080:".
	in := "会议在 12:30:45 开始"
	assert.Equal(t, in, StripMarkdown(in))

	in2 := "服务跑在 localhost:8080: 端口"
	assert.Equal(t, in2, StripMarkdown(in2))
}

func TestStripMarkdown_PreservesWindowsPaths(t *testing.T) {
	// Backslashes used to be deleted wholesale.
	in := `路径 C:\Users\test\file.txt 结束`
	assert.Equal(t, in, StripMarkdown(in))

	in2 := `打开 C:\temp 目录`
	assert.Equal(t, in2, StripMarkdown(in2))
}

func TestStripMarkdown_PreservesPathSegmentsMatchingLatexPrefixes(t *testing.T) {
	// The LaTeX whitelist must match a whole command, not a PREFIX of a path
	// segment. \pi / \to / \int / \sum used to eat "pictures" / "todo" /
	// "internal" / "summary" (C:\Users\me\pictures → C:\Users\mectures).
	for _, in := range []string{
		`C:\Users\me\pictures\img.png`,
		`D:\projects\todo\list.txt`,
		`D:\work\internal\handler.go`,
		`D:\docs\summary.md`,
	} {
		assert.Equal(t, in, StripMarkdown(in), "path must survive verbatim")
	}
}

func TestStripMarkdown_PreservesCurrencyWithMathSignal(t *testing.T) {
	// A currency amount must not be mistaken for inline math even when a later
	// $...$ pair on the same line carries a math signal. The old body pattern
	// `[^$\n]*` spanned across both amounts and ate the text between them.
	assert.Equal(t,
		"cost $5 for item_1 and $10 total",
		StripMarkdown("cost $5 for item_1 and $10 total"))
	assert.Equal(t,
		"价格 $5 元, 变量 user_id, 还有 $10",
		StripMarkdown("价格 $5 元, 变量 user_id, 还有 $10"))
}

func TestStripMarkdown_UnwrapsAdjacentUnderscoreEmphasis(t *testing.T) {
	// A single pass consumed the separator anchoring the next span.
	assert.Equal(t, "a b", StripMarkdown("_a_ _b_"))
	assert.Equal(t, "a x y z b", StripMarkdown("a _x_ _y_ _z_ b"))
}

func TestStripMarkdown_PreservesTimeRanges(t *testing.T) {
	// The old emoji body allowed a sign anywhere, so the "-" in a range
	// satisfied it: "10:30-11:45" → "1045".
	assert.Equal(t, "10:30-11:45", StripMarkdown("10:30-11:45"))
	assert.Equal(t, "1:2-3:4", StripMarkdown("1:2-3:4"))
}

func TestStripMarkdown_KeepsInlineTripleBacktickProse(t *testing.T) {
	// A fence written mid-prose is not an opening fence; it must not swallow
	// the rest of the message. Only a line-start fence is treated as unclosed.
	assert.Equal(t,
		"use ``` to open a code fence here",
		StripMarkdown("use ``` to open a code fence here"))
	assert.Equal(t,
		"discuss ``` triple backticks",
		StripMarkdown("discuss ``` triple backticks"))
}

func TestStripMarkdown_DropsOrphanedEmphasisMarkers(t *testing.T) {
	// A truncated message leaves a marker run with no partner; it must not be
	// spoken. Single markers (multiplication, identifiers) are untouched.
	assert.Equal(t, "bold truncated", StripMarkdown("**bold truncated"))
	assert.Equal(t, "strike truncated", StripMarkdown("~~strike truncated"))
	assert.Equal(t, "计算 3 * 4 * 5 的结果", StripMarkdown("计算 3 * 4 * 5 的结果"))
}

func TestStripMarkdown_PreservesLiteralHashAndHashTag(t *testing.T) {
	assert.Equal(t, "学习 C# 语言", StripMarkdown("学习 C# 语言"))
	assert.Equal(t, "关注 #热点 话题", StripMarkdown("关注 #热点 话题"))
}

func TestStripMarkdown_PreservesCurrencyDollars(t *testing.T) {
	// A bare $...$ with no math signal is currency, not inline math.
	in := "价格是 $5 和 $10 元"
	assert.Equal(t, in, StripMarkdown(in))
}

func TestStripMarkdown_StripsUnclosedCodeFence(t *testing.T) {
	// A truncated (unclosed) fence must not leak the code body.
	in := "说明：\n```go\nfunc main() {}\n没有闭合"
	result := StripMarkdown(in)
	assert.NotContains(t, result, "func main")
	assert.NotContains(t, result, "```")
	assert.Contains(t, result, "说明")
}

func TestStripMarkdown_StripsLatexFormulas(t *testing.T) {
	for _, in := range []string{
		"公式 $x_1 + x_2$ 求和",
		`公式 \frac{a}{b} 结束`,
		"公式 \\[ a^2 + b^2 \\] 结束",
	} {
		result := StripMarkdown(in)
		assert.NotContains(t, result, "x_1")
		assert.NotContains(t, result, "frac")
		assert.NotContains(t, result, "$")
		assert.NotContains(t, result, "\\")
	}
}

func TestStripMarkdown_StillUnwrapsRealEmphasis(t *testing.T) {
	// De-blunting must not break genuine markdown.
	assert.Equal(t, "This is bold text.", StripMarkdown("This is **bold** text."))
	assert.Equal(t, "This is italic text.", StripMarkdown("This is *italic* text."))
	assert.Equal(t, "这是 下划线强调 文本", StripMarkdown("这是 _下划线强调_ 文本"))
	assert.Equal(t, "这是 删除 文本", StripMarkdown("这是 ~~删除~~ 文本"))
}

func TestStripMarkdownStats_ReportsJunkRatio(t *testing.T) {
	// Mostly code → most runes removed.
	text := "看：\n```go\nfunc main() { println(1) }\n```\n以上"
	cleaned, original, kept := StripMarkdownStats(text)
	assert.Equal(t, len([]rune(text)), original)
	assert.Equal(t, len([]rune(cleaned)), kept)
	assert.Less(t, kept, original/2, "a code-dominated message should lose most runes")

	// Clean prose → nothing removed.
	_, orig2, kept2 := StripMarkdownStats("这是一段干净的纯文本。")
	assert.Equal(t, orig2, kept2)
}

// --- Ask-question preservation tests ---

func TestStripMarkdown_AskQuestion_SingleSelect(t *testing.T) {
	input := `Some text before.

<clawbench-ask-question>
**Approach**
Which approach do you prefer?
- Option A — Fast but less safe
- Option B — Safe but slower
</clawbench-ask-question>

Some text after.`
	result := StripMarkdown(input)
	assert.Contains(t, result, "Which approach do you prefer")
	assert.Contains(t, result, "Option A")
	assert.Contains(t, result, "Option B")
	assert.Contains(t, result, "Fast but less safe")
	assert.Contains(t, result, "Safe but slower")
	assert.NotContains(t, result, "<clawbench-ask-question>")
	assert.NotContains(t, result, "</clawbench-ask-question>")
	assert.Contains(t, result, "Some text before")
	assert.Contains(t, result, "Some text after")
}

func TestStripMarkdown_AskQuestion_MultiSelect(t *testing.T) {
	input := `Here is a question:

<clawbench-ask-question>
**Method**
Which caching method?
- [ ] Redis — In-memory cache
- [ ] SQLite — File-based storage
</clawbench-ask-question>

Continue here.`
	result := StripMarkdown(input)
	assert.Contains(t, result, "Which caching method")
	assert.Contains(t, result, "Redis")
	assert.Contains(t, result, "SQLite")
	assert.Contains(t, result, "In-memory cache")
	assert.Contains(t, result, "File-based storage")
	assert.NotContains(t, result, "<clawbench-ask-question>")
}

// Several tags are several questions.
func TestStripMarkdown_AskQuestion_MultipleTags(t *testing.T) {
	input := "<clawbench-ask-question>\n**DB**\nWhich database?\n- PostgreSQL — Relational\n- MongoDB — Document\n</clawbench-ask-question>\n" +
		"<clawbench-ask-question>\n**Deploy**\nDeploy where?\n- AWS — Cloud\n- On-prem — Self-hosted\n</clawbench-ask-question>"
	result := StripMarkdown(input)
	assert.Contains(t, result, "Which database")
	assert.Contains(t, result, "PostgreSQL")
	assert.Contains(t, result, "MongoDB")
	assert.Contains(t, result, "Deploy where")
	assert.Contains(t, result, "AWS")
	assert.Contains(t, result, "On-prem")
}

func TestStripMarkdown_AskQuestion_OptionsNoDescription(t *testing.T) {
	input := "<clawbench-ask-question>\n**Confirm**\nProceed?\n- Yes\n- No\n</clawbench-ask-question>"
	result := StripMarkdown(input)
	assert.Contains(t, result, "Proceed")
	assert.Contains(t, result, "Yes")
	assert.Contains(t, result, "No")
}

// A payload with no list is not a question; the wrapper is stripped and the
// text is kept so nothing is lost.
func TestStripMarkdown_AskQuestion_ProseDegrades(t *testing.T) {
	input := "<clawbench-ask-question>\nnot a valid question payload\n</clawbench-ask-question>"
	result := StripMarkdown(input)
	assert.Contains(t, result, "not a valid question payload")
	assert.NotContains(t, result, "<clawbench-ask-question>")
}

func TestStripMarkdown_AskQuestion_RegularCodeBlockUnaffected(t *testing.T) {
	input := "Normal code:\n```go\nfmt.Println(\"hello\")\n```\n<clawbench-ask-question>\n**Go**\nUse Go?\n- Yes — Go ahead\n</clawbench-ask-question>"
	result := StripMarkdown(input)
	// Regular code block should still be removed
	assert.NotContains(t, result, "fmt.Println")
	// Ask-question should be preserved
	assert.Contains(t, result, "Use Go")
	assert.Contains(t, result, "Yes")
}

// JSON is not a supported payload and is not recovered; the tag is stripped and
// the raw text remains so the malformed output is visible.
func TestStripMarkdown_AskQuestion_JSONContentStripped(t *testing.T) {
	input := "<clawbench-ask-question>\n{\"questions\":[{\"question\":\"Which approach?\",\"options\":[{\"label\":\"Option A\"}]}]}\n</clawbench-ask-question>"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "<clawbench-ask-question>")
	assert.NotContains(t, result, "</clawbench-ask-question>")
	assert.Contains(t, result, "Which approach?")
}

// An unparseable payload must never be read as raw markup: the tag is stripped
// and only the inner text survives.
func TestStripMarkdown_AskQuestion_UnparseableNeverSpeaksTags(t *testing.T) {
	input := "<clawbench-ask-question>\n{\"questions\":[{\"question\":\"你最喜欢哪种水果？\"}]}\n</clawbench-ask-question>"
	result := StripMarkdown(input)
	assert.NotContains(t, result, "<clawbench-ask-question>")
	assert.NotContains(t, result, "</clawbench-ask-question>")
	assert.Contains(t, result, "你最喜欢哪种水果")
}
