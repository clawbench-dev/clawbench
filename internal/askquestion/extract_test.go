package askquestion

import (
	"strings"
	"testing"
)

// tag wraps a Markdown payload in the clawbench-ask-question tag.
func tag(payload string) string {
	return "<" + tagName + ">\n" + payload + "\n</" + tagName + ">"
}

func TestExtract_WellFormed(t *testing.T) {
	text := "前言\n" + tag("**H**\nQ?\n- A") + "\n后记"
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

// A tag with no close must not consume what follows it. An earlier
// "accept the next closing token" rule ran the span to the </details> and
// deleted real prose.
func TestExtract_UnclosedTagDoesNotSwallowFollowingBlock(t *testing.T) {
	text := "分析如下\n<" + tagName + ">\n**H**\nQ?\n- A\n\n<details>\n<summary>更多</summary>\n正文内容必须保留\n</details>"
	ms := Extract(text)
	if len(ms) != 1 {
		t.Fatalf("expected one match, got %+v", ms)
	}
	if ms[0].Parsed {
		t.Fatal("a tag with no close has no payload to parse")
	}
	stripped := Strip(text, ms)
	if !strings.Contains(stripped, "正文内容必须保留") {
		t.Fatalf("the details body was swallowed: %q", stripped)
	}
	if !strings.Contains(stripped, "<details>") {
		t.Fatalf("the details element was swallowed: %q", stripped)
	}
}

// Several tags in one block must all convert; converting only the last leaked
// the rest as raw markup.
func TestExtract_MultipleTagsAllConverted(t *testing.T) {
	text := tag("**Q1**\n第一个?\n- A") + "\n中间\n" + tag("**Q2**\n第二个?\n- B")
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

// An unparseable payload must be retained: deleting it is the silent
// content-loss defect.
func TestExtract_UnparseableIsRetained(t *testing.T) {
	text := "前言\n" + tag("这里没有列表，只是一段说明。") + "\n后记"
	ms := Extract(text)
	if len(ms) != 1 {
		t.Fatalf("expected 1 match, got %d", len(ms))
	}
	if ms[0].Parsed {
		t.Fatal("prose with no list must not become a card")
	}
	// The wrapper is stripped and the inner text is shown, so the payload
	// degrades to readable prose instead of exposing raw markup. The question
	// text itself must still be present — that is the no-loss rule.
	got := Strip(text, ms)
	if strings.Contains(got, "<"+tagName) {
		t.Errorf("the wrapper must be stripped, got %q", got)
	}
	if !strings.Contains(got, "这里没有列表") {
		t.Errorf("the payload text must survive, got %q", got)
	}
	if !strings.Contains(got, "前言") || !strings.Contains(got, "后记") {
		t.Errorf("surrounding text must be untouched, got %q", got)
	}
}

// A tag with no close tag at all has no payload and must be retained verbatim.
func TestExtract_NoCloseTagIsRetained(t *testing.T) {
	text := "前言\n<" + tagName + ">\n**H**\nQ?\n- A"
	ms := Extract(text)
	if len(ms) != 1 {
		t.Fatalf("expected 1 match, got %d", len(ms))
	}
	if ms[0].Parsed {
		t.Fatal("expected unparsed when there is no close tag")
	}
	if got := Strip(text, ms); got != text {
		t.Errorf("unparseable payload must be retained verbatim, got %q", got)
	}
}

func TestExtract_TagInsideCodeFenceIsIgnored(t *testing.T) {
	text := "你可以这样用：\n\n```\n" + tag("**Choice**\nPick one\n- A") + "\n```\n\n这就是全部。"
	if ms := Extract(text); len(ms) != 0 {
		t.Fatalf("a tag inside a fenced block is documentation, got %+v", ms)
	}
}

func TestExtract_TagInsideInlineCodeIsIgnored(t *testing.T) {
	text := "用 `<" + tagName + ">` 标签来提问。"
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
		tag("**助手消息宽度**\n当前助手消息已是 `align-self: stretch`，两侧各留了 10px。具体指什么？\n- 去掉左右 padding")
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

func TestStrip_OnlyRemovesParsedSpans(t *testing.T) {
	text := "A" + tag("**H**\nQ?\n- A") + "B"
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
	text := "前言\n" + tag("没有列表的说明") + "\n后记"
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
	text := "要发起提问，就用 <" + tagName + "> 标签包起来。\n\n现在问你：\n" +
		tag("**Q2**\n第二个?\n- B")
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

// A close that belongs to a later tag must not be consumed by the mention
// before it.
func TestExtract_StandardCloseOfLaterTagIsNotConsumed(t *testing.T) {
	text := "说明：<" + tagName + "> 只是个标签名。\n" + tag("**H**\nQ?\n- A")
	ms := Extract(text)
	stripped := Strip(text, ms)
	if !strings.Contains(stripped, "说明：") {
		t.Fatalf("prose before the real tag was deleted: %q", stripped)
	}
	if strings.Contains(stripped, "Q?") {
		t.Fatalf("the real payload should have been removed: %q", stripped)
	}
}

// A close that belongs to an OUTER element must not be consumed.
func TestExtract_OuterCloseIsNotConsumed(t *testing.T) {
	text := "分析\n<details>\n<summary>更多</summary>\n<" + tagName + ">\n**H**\nQ?\n- A\n</details>\n正文"
	ms := Extract(text)
	stripped := Strip(text, ms)
	if !strings.Contains(stripped, "</details>") {
		t.Fatalf("the outer </details> was consumed: %q", stripped)
	}
	if !strings.Contains(stripped, "正文") {
		t.Fatalf("following prose was consumed: %q", stripped)
	}
}

// In the Markdown format the bold title often IS the question (only a checkbox
// list follows), so it must lead rather than render as an empty "(): ...".
func TestPlainText_HeaderOnlyQuestion(t *testing.T) {
	got := PlainText([]Item{{
		Header:      "需要启用哪些",
		MultiSelect: true,
		Options:     []Option{{Label: "语法高亮"}, {Label: "自动换行"}},
	}})
	want := "需要启用哪些: 语法高亮, 自动换行"
	if got != want {
		t.Errorf("PlainText = %q, want %q", got, want)
	}
}

// With separate question text the header stays a parenthetical label.
func TestPlainText_HeaderAsLabel(t *testing.T) {
	got := PlainText([]Item{{
		Header:   "方案选择",
		Question: "你更倾向哪种？",
		Options:  []Option{{Label: "方案 A"}},
	}})
	want := "你更倾向哪种？ (方案选择): 方案 A"
	if got != want {
		t.Errorf("PlainText = %q, want %q", got, want)
	}
}

// A mention of the tag inside the payload's own text must not be mistaken for a
// sibling payload. Regression: a line-start mention inside a fenced block or an
// indented example used to make the enclosing tag unparseable, so the card was
// lost and the raw wrapper leaked.
func TestExtract_MentionInsidePayloadIsNotASibling(t *testing.T) {
	cases := map[string]string{
		"fenced":     tag("**Which syntax?**\nUse it:\n```\n<" + tagName + ">\n```\n- Option A\n- Option B"),
		"indented":   tag("**H**\nQ?\n- A\n  <" + tagName + "> note\n- B"),
		"inline":     tag("**H**\n怎么渲染 <" + tagName + "> 这个标签？\n- 保留"),
		"blockquote": tag("**H**\nQ?\n- A\n> <" + tagName + "> 引用\n- B"),
	}
	for name, text := range cases {
		ms := Extract(text)
		if len(ms) != 1 || !ms[0].Parsed {
			t.Errorf("%s: expected one parsed match, got %+v", name, ms)
		}
	}
}

// A bullet whose label is empty must not be dropped: because the span parses,
// the whole span is removed from the text, so the entry would vanish from both
// the card and the visible text. Failing the parse keeps it visible.
func TestExtract_EmptyLabelOptionFailsTheParseInsteadOfVanishing(t *testing.T) {
	text := tag("Pick one?\n- A\n- — orphan description text")
	ms := Extract(text)
	if len(ms) != 1 || ms[0].Parsed {
		t.Fatalf("expected an unparsed match, got %+v", ms)
	}
	stripped := Strip(text, ms)
	if !strings.Contains(stripped, "orphan description text") {
		t.Errorf("the dropped entry must stay visible, got %q", stripped)
	}
}
