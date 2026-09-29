package askquestion

import (
	"reflect"
	"testing"
)

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
		{
			// A dash inside the bold run is part of the label, not a
			// separator. Splitting on it truncated the label to "**A" and
			// left the unmatched "**" visible (production message 52484).
			name: "separator inside bold run does not split",
			in:   "Q?\n- **A — 回合结束时失效负缓存（推荐）** — 在 ContentBlocks.vue 里清缓存。",
			item: Item{Question: "Q?", Options: []Option{{
				Label:       "A — 回合结束时失效负缓存（推荐）",
				Description: "在 ContentBlocks.vue 里清缓存。",
			}}},
		},
		{
			name: "bold label with no description",
			in:   "Q?\n- **A — 方案一**",
			item: Item{Question: "Q?", Options: []Option{{Label: "A — 方案一"}}},
		},
		{
			name: "separator after the bold run still splits",
			in:   "Q?\n- **甲** — 说明",
			item: Item{Question: "Q?", Options: []Option{{Label: "甲", Description: "说明"}}},
		},
		{
			name: "later plain option still splits normally",
			in:   "Q?\n- **A — 标签** — 描述\n- 乙 — 乙说明",
			item: Item{Question: "Q?", Options: []Option{
				{Label: "A — 标签", Description: "描述"},
				{Label: "乙", Description: "乙说明"},
			}},
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

// A question that merely mentions an element name in prose must still parse as
// Markdown, not be mistaken for markup.
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

// Anything that is not Markdown-with-a-list yields nothing, so the caller
// degrades it to plain text. There is no fallback reader by design.
func TestParseItems_TrulyUnparseable(t *testing.T) {
	for _, in := range []string{
		"这里没有列表也没有 JSON，只是一段说明。",
		`{"questions":[{"question":"Q?","options":[{"label":"A"}]}]}`,
		`<item><header>H</header><question>Q?</question></item>`,
		`{not valid json at all`,
	} {
		if got := ParseItems(in); len(got) != 0 {
			t.Errorf("expected no card for %q, got %+v", in, got)
		}
	}
}
