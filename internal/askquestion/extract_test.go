package askquestion

import (
	"strings"
	"testing"
)

func TestExtract_WellFormed(t *testing.T) {
	text := "前言\n<ask-question>\n<item><header>H</header><multi-select>false</multi-select><question>Q?</question><option><label>A</label></option></item>\n</ask-question>\n后记"
	ms := Extract(text)
	if len(ms) != 1 {
		t.Fatalf("expected 1 match, got %d", len(ms))
	}
	if !ms[0].Parsed {
		t.Fatalf("expected parsed, got reason %q", ms[0].Reason)
	}
	if got := Strip(text, ms); got != "前言\n\n后记" {
		t.Errorf("strip mismatch: %q", got)
	}
}

// The over-strip regression: an unclosed tag followed by a <details> block
// must not consume the details block. Before this package, the span ran to the
// next closing token (</details>) and deleted real prose.
func TestExtract_UnclosedTagDoesNotSwallowFollowingBlock(t *testing.T) {
	text := "分析如下\n<ask-question>\n<item><header>H</header><multi-select>false</multi-select><question>Q?</question><option><label>A</label></option></item>\n\n<details>\n<summary>更多</summary>\n正文内容必须保留\n</details>"
	ms := Extract(text)
	if len(ms) != 1 || !ms[0].Parsed {
		t.Fatalf("expected one parsed match, got %+v", ms)
	}
	stripped := Strip(text, ms)
	if !strings.Contains(stripped, "正文内容必须保留") {
		t.Fatalf("the details body was swallowed: %q", stripped)
	}
	if !strings.Contains(stripped, "<details>") {
		t.Fatalf("the details element was swallowed: %q", stripped)
	}
	if strings.Contains(stripped, "<ask-question") {
		t.Fatalf("the parsed tag should have been removed: %q", stripped)
	}
}

// 27% of real text blocks contain more than one tag; before this package only
// the last one was converted and the rest leaked as raw XML.
func TestExtract_MultipleTagsAllConverted(t *testing.T) {
	text := "<ask-question><item><header>Q1</header><multi-select>false</multi-select><question>第一个?</question><option><label>A</label></option></item></ask-question>\n中间\n<ask-question><item><header>Q2</header><multi-select>false</multi-select><question>第二个?</question><option><label>B</label></option></item></ask-question>"
	ms := Extract(text)
	if len(ms) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(ms))
	}
	for i, m := range ms {
		if !m.Parsed {
			t.Fatalf("match %d not parsed (reason %q)", i, m.Reason)
		}
	}
	items := AllItems(ms)
	if len(items) != 2 || items[0].Header != "Q1" || items[1].Header != "Q2" {
		t.Fatalf("expected both tags' items, got %+v", items)
	}
	if got := Strip(text, ms); got != "\n中间\n" {
		t.Errorf("strip mismatch: %q", got)
	}
}

// An unparseable payload must be retained verbatim: deleting it is the silent
// content-loss defect.
func TestExtract_UnparseableIsRetained(t *testing.T) {
	text := "前言\n<ask-question>\n{\"questions\":[{\"question\":\"你最喜欢哪种水果？\"}]}\n</ask-question>\n后记"
	ms := Extract(text)
	if len(ms) != 1 {
		t.Fatalf("expected 1 match, got %d", len(ms))
	}
	if ms[0].Parsed {
		t.Fatal("JSON payload must not parse (support was removed deliberately)")
	}
	if got := Strip(text, ms); got != text {
		t.Errorf("unparseable payload must be retained verbatim\n got: %q\nwant: %q", got, text)
	}
}

func TestExtract_NoChildCloseIsRetained(t *testing.T) {
	text := "前言\n<ask-question>\n<item><header>H</header><question>Q?</question>"
	ms := Extract(text)
	if len(ms) != 1 {
		t.Fatalf("expected 1 match, got %d", len(ms))
	}
	// The item has no option and no child close; there is nothing to parse.
	if ms[0].Parsed {
		t.Fatal("expected unparsed for a payload with no option")
	}
	if got := Strip(text, ms); got != text {
		t.Errorf("unparseable payload must be retained verbatim, got %q", got)
	}
}

func TestExtract_TagInsideCodeFenceIsIgnored(t *testing.T) {
	text := "你可以这样用：\n\n```\n<ask-question>\n<item><header>Choice</header><question>Pick one</question><option><label>A</label></option></item>\n</ask-question>\n```\n\n这就是全部。"
	if ms := Extract(text); len(ms) != 0 {
		t.Fatalf("a tag inside a fenced block is documentation, got %+v", ms)
	}
}

func TestExtract_TagInsideInlineCodeIsIgnored(t *testing.T) {
	text := "用 `<ask-question>` 标签来提问。"
	if ms := Extract(text); len(ms) != 0 {
		t.Fatalf("a tag inside inline code is documentation, got %+v", ms)
	}
}

// Regression from the real corpus: an orphaned backtick earlier in a long
// message used to pair with a backtick inside the payload, making the
// inline-code span cover thousands of characters and hiding a live question.
// Inline code cannot span a line break, so the span is bounded per line.
func TestExtract_OrphanedBacktickDoesNotHideLiveQuestion(t *testing.T) {
	text := "分析如下，前面有个孤立的反引号 ` 没有配对。\n\n" +
		"<ask-question>\n" +
		"  <item>\n" +
		"    <header>助手消息宽度</header>\n" +
		"    <multi-select>false</multi-select>\n" +
		"    <question>当前助手消息已是 `align-self: stretch`，两侧各留了 10px。具体指什么？</question>\n" +
		"    <option><label>去掉左右 padding</label></option>\n" +
		"  </item>\n" +
		"</ask-question>"
	ms := Extract(text)
	if len(ms) != 1 || !ms[0].Parsed {
		t.Fatalf("a live question must not be hidden by an orphaned backtick, got %+v", ms)
	}
	if ms[0].Items[0].Header != "助手消息宽度" {
		t.Errorf("unexpected header: %q", ms[0].Items[0].Header)
	}
}

func TestExtract_NoTag(t *testing.T) {
	if ms := Extract("普通文本，没有任何标签"); len(ms) != 0 {
		t.Fatalf("expected no matches, got %+v", ms)
	}
}

func TestExtract_ObfuscatedCloseTag(t *testing.T) {
	text := "前\n<ask-question><item><header>GitHub 认证</header><multi-select>false</multi-select><question>请完成登录。</question><option><label>已打开链接</label></option></item>\n</\uff5c\uff5cDSML\uff5c\uff5cquestion>"
	ms := Extract(text)
	if len(ms) != 1 || !ms[0].Parsed {
		t.Fatalf("expected the obfuscated close to be tolerated, got %+v", ms)
	}
	if got := Strip(text, ms); strings.Contains(got, "<ask-question") {
		t.Errorf("obfuscated-close tag should be removed, got %q", got)
	}
}

func TestStrip_OnlyRemovesParsedSpans(t *testing.T) {
	text := "A<ask-question><item><header>H</header><question>Q?</question><option><label>A</label></option></item></ask-question>B"
	ms := Extract(text)
	got := Strip(text, ms)
	if got != "AB" {
		t.Errorf("expected surrounding text preserved, got %q", got)
	}
}

func TestStrip_NoMatchesReturnsInputUnchanged(t *testing.T) {
	text := "unchanged"
	if got := Strip(text, nil); got != text {
		t.Errorf("expected %q, got %q", text, got)
	}
}

func TestUnparsedReasons(t *testing.T) {
	text := "前言\n<ask-question>\n{\"questions\":[]}\n</ask-question>\n后记"
	reasons := UnparsedReasons(Extract(text))
	if len(reasons) != 1 {
		t.Fatalf("expected one unparsed reason, got %v", reasons)
	}
	if reasons[0] == "" {
		t.Error("a reason code must be set for an unparsed span")
	}
}

func TestPlainText(t *testing.T) {
	items := []Item{{
		Header: "Approach", Question: "Which approach?",
		Options: []Option{
			{Label: "Option A", Description: "Fast"},
			{Label: "Option B", Description: "Safe"},
		},
	}}
	got := PlainText(items)
	want := "Which approach? (Approach): Option A — Fast, Option B — Safe"
	if got != want {
		t.Errorf("PlainText mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestPlainText_NoOptions(t *testing.T) {
	got := PlainText([]Item{{Question: "确定吗？"}})
	if got != "确定吗？" {
		t.Errorf("unexpected plain text: %q", got)
	}
}

func TestToInputMap(t *testing.T) {
	items := []Item{{
		Header: "H", MultiSelect: true, Question: "Q?",
		Options: []Option{{Label: "A", Description: "d"}, {Label: "B"}},
	}}
	got := ToInputMap(items)
	questions, ok := got["questions"].([]map[string]any)
	if !ok || len(questions) != 1 {
		t.Fatalf("unexpected shape: %+v", got)
	}
	q := questions[0]
	if q["header"] != "H" || q["multiSelect"] != true || q["question"] != "Q?" {
		t.Errorf("unexpected question fields: %+v", q)
	}
	opts, ok := q["options"].([]map[string]any)
	if !ok || len(opts) != 2 {
		t.Fatalf("unexpected options: %+v", q["options"])
	}
	if opts[0]["label"] != "A" || opts[0]["description"] != "d" {
		t.Errorf("unexpected first option: %+v", opts[0])
	}
	if _, has := opts[1]["description"]; has {
		t.Errorf("an option without a description must omit the key: %+v", opts[1])
	}
}

// The span must never reach past its own payload. Regression: a sentence that
// merely MENTIONS the tag, followed by a genuine question, used to have the
// whole sentence deleted because the span ran to the later tag's close.
func TestExtract_ProseMentioningTagIsNotSwallowed(t *testing.T) {
	text := "要发起提问，就用 <ask-question> 标签包起来，里面放 <item> 元素。\n\n现在问你：\n" +
		"<ask-question><item><header>Q2</header><multi-select>false</multi-select>" +
		"<question>第二个?</question><option><label>B</label></option></item></ask-question>"
	ms := Extract(text)
	if len(ms) != 2 {
		t.Fatalf("expected both open tags located, got %d (%+v)", len(ms), ms)
	}
	// Only the second tag is a payload; the first is prose.
	if ms[0].Parsed {
		t.Error("the prose mention must not parse as a payload")
	}
	if !ms[1].Parsed {
		t.Fatalf("the real question must parse, reason=%q", ms[1].Reason)
	}
	stripped := Strip(text, ms)
	if !strings.Contains(stripped, "要发起提问") {
		t.Fatalf("the prose was deleted: %q", stripped)
	}
	if !strings.Contains(stripped, "现在问你") {
		t.Fatalf("the text between the mention and the question was deleted: %q", stripped)
	}
	if strings.Contains(stripped, "第二个?") {
		t.Fatalf("the real payload should have been removed: %q", stripped)
	}
}

// The standard-close branch must not reach a close that belongs to a later tag.
func TestExtract_StandardCloseOfLaterTagIsNotConsumed(t *testing.T) {
	text := "说明：<ask-question> 只是个标签名。\n" +
		"<ask-question><item><header>H</header><question>Q?</question><option><label>A</label></option></item></ask-question>"
	ms := Extract(text)
	stripped := Strip(text, ms)
	if !strings.Contains(stripped, "说明：") {
		t.Fatalf("prose before the real tag was deleted: %q", stripped)
	}
}

// Regression: a non-standard close that belongs to an OUTER element (the
// details block) must not be consumed, even when the gap is punctuation-only.
func TestExtract_OuterCloseIsNotConsumed(t *testing.T) {
	text := "分析\n<details>\n<summary>更多</summary>\n<ask-question>\n" +
		"<item><header>H</header><question>Q?</question><option><label>A</label></option></item>\n" +
		"</details>\n正文"
	ms := Extract(text)
	stripped := Strip(text, ms)
	if !strings.Contains(stripped, "</details>") {
		t.Fatalf("the outer </details> was consumed: %q", stripped)
	}
	if !strings.Contains(stripped, "正文") {
		t.Fatalf("following prose was consumed: %q", stripped)
	}
}

// An obfuscated close with no matching outer open tag is still accepted.
func TestExtract_ObfuscatedCloseStillAcceptedWhenSelfContained(t *testing.T) {
	text := "前\n<ask-question><item><header>H</header><question>Q?</question>" +
		"<option><label>A</label></option></item>\n</\uFF5C\uFF5CDSML\uFF5C\uFF5Cquestion>"
	ms := Extract(text)
	if len(ms) != 1 || !ms[0].Parsed {
		t.Fatalf("expected the obfuscated close to be accepted, got %+v", ms)
	}
	if got := Strip(text, ms); strings.Contains(got, "<ask-question") {
		t.Errorf("the tag should have been removed, got %q", got)
	}
}

func TestCloseTagName(t *testing.T) {
	cases := map[string]string{
		"</details>":           "details",
		"</ask-question>":      "ask-question",
		"</DIV>":               "div",
		"</｜｜DSML｜｜parameter>": "｜｜dsml｜｜parameter",
		"</>":                  "",
	}
	for in, want := range cases {
		if got := closeTagName(in); got != want {
			t.Errorf("closeTagName(%q) = %q, want %q", in, got, want)
		}
	}
}
