package askquestion

import (
	"reflect"
	"testing"
)

// Path B shapes — the <ask-question> XML payloads that reach Extract().
func TestParseItems_RealMalformedShapes(t *testing.T) {
	cases := []struct {
		name  string
		inner string
		want  []Item
	}{
		{
			name:  "well formed",
			inner: `<item><header>Approach</header><multi-select>false</multi-select><question>Which?</question><option><label>A</label><description>Fast</description></option></item>`,
			want: []Item{{
				Header: "Approach", Question: "Which?",
				Options: []Option{{Label: "A", Description: "Fast"}},
			}},
		},
		{
			name:  "unclosed option (24% of real payloads)",
			inner: "<item>\n<header>操作确认</header>\n<multi-select>false</multi-select>\n<question>可以停掉吗？</question>\n<option>\n<label>停掉主实例</label>\n<description>kill 后重启</description>\n</item>",
			want: []Item{{
				Header: "操作确认", Question: "可以停掉吗？",
				Options: []Option{{Label: "停掉主实例", Description: "kill 后重启"}},
			}},
		},
		{
			name:  "unclosed option bounded by next option",
			inner: "<item><header>H</header><multi-select>false</multi-select><question>Q?</question><option><label>A</label><option><label>B</label><description>second</description></option></item>",
			want: []Item{{
				Header: "H", Question: "Q?",
				Options: []Option{{Label: "A"}, {Label: "B", Description: "second"}},
			}},
		},
		{
			name:  "option with value attribute",
			inner: `<item><header>Pick</header><multi-select>false</multi-select><question>Which?</question><option value="A"><label>A</label></option></item>`,
			want: []Item{{
				Header: "Pick", Question: "Which?",
				Options: []Option{{Label: "A"}},
			}},
		},
		{
			name:  "option with attribute but no label element",
			inner: `<item><header>Pick</header><multi-select>false</multi-select><question>Which?</question><option value="restore_only"></option></item>`,
			want: []Item{{
				Header: "Pick", Question: "Which?",
				Options: []Option{{Label: "restore_only"}},
			}},
		},
		{
			name:  "plural options wrapper",
			inner: `<item><header>人月数调整</header><multi-select>false</multi-select><question>回滚?</question><options><option><label>保留现状</label><description>按 git 数据</description></option></options></item>`,
			want: []Item{{
				Header: "人月数调整", Question: "回滚?",
				Options: []Option{{Label: "保留现状", Description: "按 git 数据"}},
			}},
		},
		{
			name:  "bare text option becomes label",
			inner: `<item><header>确认</header><multi-select>false</multi-select><question>是吗？</question><option>是，就是它</option><option>不是</option></item>`,
			want: []Item{{
				Header: "确认", Question: "是吗？",
				Options: []Option{{Label: "是，就是它"}, {Label: "不是"}},
			}},
		},
		{
			name:  "underscore multi_select spelling",
			inner: `<item><header>H</header><multi_select>true</multi_select><question>Q?</question><option><label>A</label></option></item>`,
			want: []Item{{
				Header: "H", MultiSelect: true, Question: "Q?",
				Options: []Option{{Label: "A"}},
			}},
		},
		{
			name:  "raw ampersand and angle brackets repaired",
			inner: `<item><header>A & B</header><multi-select>false</multi-select><question>选哪个 < 5 还是 > 5?</question><option><label>R&D</label></option></item>`,
			want: []Item{{
				Header: "A & B", Question: "选哪个 < 5 还是 > 5?",
				Options: []Option{{Label: "R&D"}},
			}},
		},
		{
			name:  "multiple items",
			inner: `<item><header>Q1</header><multi-select>false</multi-select><question>First?</question><option><label>A</label></option></item><item><header>Q2</header><multi-select>true</multi-select><question>Second?</question><option><label>B</label></option></item>`,
			want: []Item{
				{Header: "Q1", Question: "First?", Options: []Option{{Label: "A"}}},
				{Header: "Q2", MultiSelect: true, Question: "Second?", Options: []Option{{Label: "B"}}},
			},
		},
		{
			name:  "item without options but with question is kept",
			inner: `<item><header>变更提案</header><multi-select>false</multi-select><question>您想要提出什么变更？</question></item>`,
			want: []Item{{
				Header: "变更提案", Question: "您想要提出什么变更？", Options: []Option{},
			}},
		},
		{
			name:  "item with neither question nor options is dropped",
			inner: `<item><header>Empty</header><multi-select>false</multi-select></item>`,
			want:  nil,
		},
		{
			// JSON is not the documented format, but it is recovered when
			// possible: models still emit it and the alternative is discarding
			// a readable question.
			name:  "json payload is recovered",
			inner: `{"questions":[{"question":"你最喜欢哪种水果？","options":[{"label":"苹果"}]}]}`,
			want: []Item{{
				Question: "你最喜欢哪种水果？",
				Options:  []Option{{Label: "苹果"}},
			}},
		},
		{
			name:  "plain prose is not a payload",
			inner: `Each question needs item, question and option elements`,
			want:  nil,
		},
		{
			name:  "empty",
			inner: ``,
			want:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseItems(tc.inner)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseItems mismatch\n got: %+v\nwant: %+v", got, tc.want)
			}
		})
	}
}

func TestParseItems_DescriptionEqualToLabelIsDropped(t *testing.T) {
	inner := `<item><header>H</header><question>Q?</question><option><label>A</label><description>A</description></option></item>`
	got := ParseItems(inner)
	if len(got) != 1 || len(got[0].Options) != 1 {
		t.Fatalf("unexpected items: %+v", got)
	}
	if got[0].Options[0].Description != "" {
		t.Errorf("a description identical to the label must be dropped, got %q", got[0].Options[0].Description)
	}
}

func TestParseItems_NestedTagsInTextAreStripped(t *testing.T) {
	inner := `<item><header>H</header><question>用 <code>x</code> 还是 <code>y</code>?</question><option><label>用 <b>x</b></label></option></item>`
	got := ParseItems(inner)
	if len(got) != 1 {
		t.Fatalf("expected one item, got %+v", got)
	}
	if got[0].Question != "用 x 还是 y?" {
		t.Errorf("nested tags must be stripped from question text, got %q", got[0].Question)
	}
	if got[0].Options[0].Label != "用 x" {
		t.Errorf("nested tags must be stripped from the label, got %q", got[0].Options[0].Label)
	}
}

// --- Native Markdown format ---

func TestParseItems_MarkdownFormat(t *testing.T) {
	cases := []struct {
		name string
		in   string
		item Item
	}{
		{
			name: "single select with header and descriptions",
			in:   "**方案选择**\n你更倾向哪种实现方式？\n- 方案 A — 快但不够安全\n- 方案 B — 安全但慢",
			item: Item{
				Header: "方案选择", Question: "你更倾向哪种实现方式？",
				Options: []Option{
					{Label: "方案 A", Description: "快但不够安全"},
					{Label: "方案 B", Description: "安全但慢"},
				},
			},
		},
		{
			name: "checkbox list means multi select",
			in:   "**需要启用哪些**\n- [ ] 语法高亮\n- [ ] 自动换行",
			item: Item{
				Header: "需要启用哪些", MultiSelect: true,
				Options: []Option{{Label: "语法高亮"}, {Label: "自动换行"}},
			},
		},
		{
			name: "checked boxes are still multi select",
			in:   "- [x] 已完成项\n- [ ] 未完成项",
			item: Item{
				MultiSelect: true,
				Options:     []Option{{Label: "已完成项"}, {Label: "未完成项"}},
			},
		},
		{
			name: "no header",
			in:   "你选哪个？\n- 甲\n- 乙",
			item: Item{Question: "你选哪个？", Options: []Option{{Label: "甲"}, {Label: "乙"}}},
		},
		{
			name: "ordered list is single select",
			in:   "选一个：\n1. 第一\n2. 第二",
			item: Item{Question: "选一个：", Options: []Option{{Label: "第一"}, {Label: "第二"}}},
		},
		{
			name: "star bullets",
			in:   "选一个：\n* 甲\n* 乙",
			item: Item{Question: "选一个：", Options: []Option{{Label: "甲"}, {Label: "乙"}}},
		},
		{
			name: "hyphen description separator",
			in:   "Q?\n- 甲 - 说明",
			item: Item{Question: "Q?", Options: []Option{{Label: "甲", Description: "说明"}}},
		},
		{
			name: "bold option label is not the header",
			in:   "Q?\n- **甲**\n- 乙",
			item: Item{Question: "Q?", Options: []Option{{Label: "甲"}, {Label: "乙"}}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseItems(c.in)
			if len(got) != 1 {
				t.Fatalf("expected 1 item, got %d (%+v)", len(got), got)
			}
			if !reflect.DeepEqual(got[0], c.item) {
				t.Errorf("mismatch\n got: %+v\nwant: %+v", got[0], c.item)
			}
		})
	}
}

// A tag that is one question yields exactly one item; several questions are
// written as several tags.
func TestParseItems_MarkdownOneQuestionPerTag(t *testing.T) {
	got := ParseItems("**Q1**\n选一个\n- 甲\n- 乙")
	if len(got) != 1 {
		t.Fatalf("one tag must yield one item, got %d", len(got))
	}
}

// Prose with no list is not a card — that is what makes "parse failure"
// meaningful, and it is the signal the caller uses to strip the wrapper and
// render the text as Markdown.
func TestParseItems_MarkdownProseIsNotAPayload(t *testing.T) {
	for _, in := range []string{
		"Each question needs item, question and option elements",
		"**标题**\n只有说明文字，没有列表",
		"",
		"   \n  \n",
	} {
		if got := ParseItems(in); len(got) != 0 {
			t.Errorf("prose must not become a card, input %q gave %+v", in, got)
		}
	}
}

// A fenced code block inside the payload must not have its lines read as
// options.
func TestParseItems_MarkdownFenceIsContent(t *testing.T) {
	got := ParseItems("Q?\n- 甲\n```\n- not an option\n```")
	if len(got) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got))
	}
	if len(got[0].Options) != 1 || got[0].Options[0].Label != "甲" {
		t.Fatalf("the fenced line must not become an option: %+v", got[0].Options)
	}
}

// A question that merely mentions a legacy tag in prose must still parse as
// Markdown, not be mistaken for XML.
func TestParseItems_MentioningTagNamesInProseStaysMarkdown(t *testing.T) {
	got := ParseItems("怎么处理 option 这个标签？\n- 保留\n- 删除")
	if len(got) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got))
	}
	if got[0].Question != "怎么处理 option 这个标签？" {
		t.Errorf("unexpected question: %q", got[0].Question)
	}
}

// --- Format robustness ---

// The markers models actually emit, beyond CommonMark's strict set. Each is a
// shape that previously failed to parse (so the whole payload degraded).
func TestParseItems_MarkerVariants(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		opts  []Option
		multi bool
	}{
		{"no space after dash", "Q?\n-甲\n-乙", []Option{{Label: "甲"}, {Label: "乙"}}, false},
		{"fullwidth hyphen", "Q?\n－ 甲\n－ 乙", []Option{{Label: "甲"}, {Label: "乙"}}, false},
		{"plus bullet", "Q?\n+ 甲\n+ 乙", []Option{{Label: "甲"}, {Label: "乙"}}, false},
		{"cjk ordinal dot", "Q?\n1、甲\n2、乙", []Option{{Label: "甲"}, {Label: "乙"}}, false},
		{"cjk numeral", "Q?\n一、甲\n二、乙", []Option{{Label: "甲"}, {Label: "乙"}}, false},
		{"paren ordered", "Q?\n1) 甲\n2) 乙", []Option{{Label: "甲"}, {Label: "乙"}}, false},
		{"ideographic indent", "Q?\n\u3000- 甲\n\u3000- 乙", []Option{{Label: "甲"}, {Label: "乙"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseItems(c.in)
			if len(got) != 1 {
				t.Fatalf("expected 1 item, got %d (%+v)", len(got), got)
			}
			if len(got[0].Options) != len(c.opts) {
				t.Fatalf("options mismatch: %+v", got[0].Options)
			}
			for i, o := range c.opts {
				if got[0].Options[i].Label != o.Label {
					t.Errorf("option %d: got %q want %q", i, got[0].Options[i].Label, o.Label)
				}
			}
		})
	}
}

// The relaxations must not turn ordinary prose into list items.
func TestParseItems_NotMistakenForList(t *testing.T) {
	cases := []struct{ name, in string }{
		{"negative number", "温度是 -5 度"},
		{"horizontal rule", "Q?\n---"},
		{"decimal", "Q?\n1.5 倍速"},
		{"bold line", "Q?\n**重点**"},
		{"italic line", "Q?\n*斜体*"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// None of these carry a list, so none may become a card.
			if got := ParseItems(c.in); len(got) != 0 {
				t.Errorf("prose must not become a card: %+v", got)
			}
		})
	}
}

// Checkbox spellings beyond ASCII "[ ]".
func TestParseItems_CheckboxVariants(t *testing.T) {
	for _, in := range []string{
		"Q?\n- [ ] 甲\n- [ ] 乙",
		"Q?\n- [x] 甲\n- [X] 乙",
		"Q?\n- ［ ］ 甲\n- ［ ］ 乙",
		"Q?\n- 【 】 甲\n- 【 】 乙",
		"Q?\n- []甲\n- []乙",
	} {
		got := ParseItems(in)
		if len(got) != 1 || !got[0].MultiSelect {
			t.Errorf("expected multi-select for %q, got %+v", in, got)
		}
	}
}

// Headings beyond a standalone bold line.
func TestParseItems_HeaderVariants(t *testing.T) {
	for _, in := range []string{
		"# 方案选择\nQ?\n- 甲\n- 乙",
		"### 方案选择\nQ?\n- 甲\n- 乙",
		"__方案选择__\nQ?\n- 甲\n- 乙",
		"**方案选择**\nQ?\n- 甲\n- 乙",
	} {
		got := ParseItems(in)
		if len(got) != 1 || got[0].Header != "方案选择" {
			t.Errorf("expected header for %q, got %+v", in, got)
		}
	}
}

// An en dash separates a description just like an em dash.
func TestParseItems_EnDashDescription(t *testing.T) {
	got := ParseItems("Q?\n- 甲 \u2013 说明")
	if len(got) != 1 || got[0].Options[0].Description != "说明" {
		t.Fatalf("expected the en dash to separate a description, got %+v", got)
	}
}

// --- Tolerant JSON recovery ---

// JSON is not the documented format, but models still emit it. Recovering it
// beats discarding a readable question.
func TestParseItems_JSONRecovery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		q    string
		opts int
	}{
		{"object with questions", `{"questions":[{"question":"你最喜欢哪种水果？","options":[{"label":"苹果"}]}]}`, "你最喜欢哪种水果？", 1},
		{"bare array", `[{"question":"选哪个？","options":[{"label":"甲"},{"label":"乙"}]}]`, "选哪个？", 2},
		{"flat question without wrapper", `{"question":"Q?","options":["A","B"]}`, "Q?", 2},
		{"unescaped quotes in value", `{"questions":[{"question":"可以被"临时分裂"多久？","options":[{"label":"数秒"}]}]}`, `可以被"临时分裂"多久？`, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseItems(c.in)
			if len(got) != 1 {
				t.Fatalf("expected 1 item, got %d (%+v)", len(got), got)
			}
			if got[0].Question != c.q {
				t.Errorf("question = %q, want %q", got[0].Question, c.q)
			}
			if len(got[0].Options) != c.opts {
				t.Errorf("options = %d, want %d", len(got[0].Options), c.opts)
			}
		})
	}
}

// Recovery must not invent a card from JSON that carries no question.
func TestParseItems_JSONWithoutQuestionIsNotACard(t *testing.T) {
	for _, in := range []string{
		`{"taskId":""}`,
		`{"schema":[{"name":"header"}]}`,
		`{"questions":[]}`,
	} {
		if got := ParseItems(in); len(got) != 0 {
			t.Errorf("expected no card for %q, got %+v", in, got)
		}
	}
}

// A payload that is neither Markdown-with-a-list nor recoverable JSON must
// yield nothing, so the caller degrades it to plain text.
func TestParseItems_TrulyUnparseable(t *testing.T) {
	for _, in := range []string{
		"这里没有列表也没有 JSON，只是一段说明。",
		`{not valid json at all`,
	} {
		if got := ParseItems(in); len(got) != 0 {
			t.Errorf("expected no card for %q, got %+v", in, got)
		}
	}
}
