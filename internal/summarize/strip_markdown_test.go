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

// --- Ask-question preservation tests ---

func TestStripMarkdown_AskQuestion_PlainXML(t *testing.T) {
	input := `Some text before.

<ask-question>
  <item>
    <header>Approach</header>
    <multi-select>false</multi-select>
    <question>Which approach do you prefer?</question>
    <option>
      <label>Option A</label>
      <description>Fast but less safe</description>
    </option>
    <option>
      <label>Option B</label>
      <description>Safe but slower</description>
    </option>
  </item>
</ask-question>

Some text after.`
	result := StripMarkdown(input)
	assert.Contains(t, result, "Which approach do you prefer")
	assert.Contains(t, result, "Option A")
	assert.Contains(t, result, "Option B")
	assert.Contains(t, result, "Fast but less safe")
	assert.Contains(t, result, "Safe but slower")
	assert.NotContains(t, result, "<ask-question>")
	assert.NotContains(t, result, "</ask-question>")
	assert.Contains(t, result, "Some text before")
	assert.Contains(t, result, "Some text after")
}

func TestStripMarkdown_AskQuestion_XMLElements(t *testing.T) {
	input := `Here is a question:

<ask-question>
  <item>
    <header>Method</header>
    <multi-select>false</multi-select>
    <question>Which caching method?</question>
    <option>
      <label>Redis</label>
      <description>In-memory cache</description>
    </option>
    <option>
      <label>SQLite</label>
      <description>File-based storage</description>
    </option>
  </item>
</ask-question>

Continue here.`
	result := StripMarkdown(input)
	assert.Contains(t, result, "Which caching method")
	assert.Contains(t, result, "Redis")
	assert.Contains(t, result, "SQLite")
	assert.Contains(t, result, "In-memory cache")
	assert.Contains(t, result, "File-based storage")
	assert.NotContains(t, result, "<ask-question>")
}

func TestStripMarkdown_AskQuestion_MultipleItems(t *testing.T) {
	input := `<ask-question>
  <item>
    <header>DB</header>
    <question>Which database?</question>
    <option><label>PostgreSQL</label><description>Relational</description></option>
    <option><label>MongoDB</label><description>Document</description></option>
    <multi-select>false</multi-select>
  </item>
  <item>
    <header>Deploy</header>
    <question>Deploy where?</question>
    <option><label>AWS</label><description>Cloud</description></option>
    <option><label>On-prem</label><description>Self-hosted</description></option>
    <multi-select>true</multi-select>
  </item>
</ask-question>`
	result := StripMarkdown(input)
	assert.Contains(t, result, "Which database")
	assert.Contains(t, result, "PostgreSQL")
	assert.Contains(t, result, "MongoDB")
	assert.Contains(t, result, "Deploy where")
	assert.Contains(t, result, "AWS")
	assert.Contains(t, result, "On-prem")
}

func TestStripMarkdown_AskQuestion_OptionsNoDescription(t *testing.T) {
	input := `<ask-question>
  <item>
    <header>Confirm</header>
    <multi-select>false</multi-select>
    <question>Proceed?</question>
    <option><label>Yes</label></option>
    <option><label>No</label></option>
  </item>
</ask-question>`
	result := StripMarkdown(input)
	assert.Contains(t, result, "Proceed")
	assert.Contains(t, result, "Yes")
	assert.Contains(t, result, "No")
}

func TestStripMarkdown_AskQuestion_InvalidXML(t *testing.T) {
	input := `<ask-question>
not valid xml content
</ask-question>`
	result := StripMarkdown(input)
	// No <item> elements found — should fall back to stripped text
	assert.Contains(t, result, "not valid xml content")
}

func TestStripMarkdown_AskQuestion_RegularCodeBlockUnaffected(t *testing.T) {
	input := "Normal code:\n```go\nfmt.Println(\"hello\")\n```\n<ask-question>\n  <item>\n    <header>Go</header>\n    <question>Use Go?</question>\n    <option><label>Yes</label><description>Go ahead</description></option>\n    <multi-select>false</multi-select>\n  </item>\n</ask-question>"
	result := StripMarkdown(input)
	// Regular code block should still be removed
	assert.NotContains(t, result, "fmt.Println")
	// Ask-question should be preserved
	assert.Contains(t, result, "Use Go")
	assert.Contains(t, result, "Yes")
}

func TestStripMarkdown_AskQuestion_JSONFormat(t *testing.T) {
	input := `<ask-question>
{"questions":[{"header":"Approach","multiSelect":false,"question":"Which approach?","options":[{"label":"Option A","description":"Fast"},{"label":"Option B","description":"Safe"}]}]}
</ask-question>`
	result := StripMarkdown(input)
	assert.NotContains(t, result, "<ask-question>")
	assert.NotContains(t, result, "</ask-question>")
	assert.Contains(t, result, "Which approach?")
	assert.Contains(t, result, "Option A")
	assert.Contains(t, result, "Safe")
}

func TestStripMarkdown_AskQuestion_JSONFormat_MultipleQuestions(t *testing.T) {
	input := `<ask-question>
{"questions":[{"header":"Q1","multiSelect":false,"question":"First?","options":[{"label":"A"}]},{"header":"Q2","multiSelect":true,"question":"Second?","options":[{"label":"B","description":"Beta"}]}]}
</ask-question>`
	result := StripMarkdown(input)
	assert.Contains(t, result, "First?")
	assert.Contains(t, result, "Second?")
	assert.Contains(t, result, "B")
	assert.Contains(t, result, "Beta")
}

func TestStripMarkdown_AskQuestion_JSONFormat_NoDescription(t *testing.T) {
	input := `<ask-question>
{"questions":[{"header":"Pick","multiSelect":false,"question":"Choose","options":[{"label":"Yes"}]}]}
</ask-question>`
	result := StripMarkdown(input)
	assert.Contains(t, result, "Choose")
	assert.Contains(t, result, "Yes")
}

func TestStripMarkdown_AskQuestion_InvalidJSON(t *testing.T) {
	input := `<ask-question>
{not valid json}
</ask-question>`
	result := StripMarkdown(input)
	// Should fall back to stripped text
	assert.Contains(t, result, "not valid json")
}
