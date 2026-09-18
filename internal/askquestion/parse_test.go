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
			name:  "json payload is not accepted",
			inner: `{"questions":[{"question":"你最喜欢哪种水果？","options":[{"label":"苹果"}]}]}`,
			want:  nil,
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
