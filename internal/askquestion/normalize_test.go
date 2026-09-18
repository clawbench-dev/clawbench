package askquestion

import (
	"reflect"
	"testing"
)

// Every case here is a real malformed payload harvested from
// ClawBench.db chat_tool_calls.input (Path A).
func TestNormalizeInput_RealMalformedShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
		want []Item
	}{
		{
			name: "flat question+options with stray quote key",
			raw: map[string]any{
				`"question`: "以上理解是否正确？",
				"options":   `[{"label": "旋转 Spinner"}, {"label": "脉冲骨架条"}]`,
				"question":  "以上理解是否正确？",
			},
			want: []Item{{
				Question: "以上理解是否正确？",
				Options:  []Option{{Label: "旋转 Spinner"}, {Label: "脉冲骨架条"}},
			}},
		},
		{
			name: "items wrapper with underscore multi_select",
			raw: map[string]any{
				"items": []any{map[string]any{
					"header": "费用行", "multi_select": false,
					"question": "怎么展示？",
					"options":  []any{map[string]any{"label": "有币种显符号"}},
				}},
			},
			want: []Item{{
				Header: "费用行", Question: "怎么展示？",
				Options: []Option{{Label: "有币种显符号"}},
			}},
		},
		{
			name: "params.items double wrap",
			raw: map[string]any{
				"params": map[string]any{
					"items": []any{map[string]any{
						"header": "识别方式", "multi_select": false,
						"question": "怎么识别？",
						"options":  []any{map[string]any{"label": "围栏约定"}},
					}},
				},
			},
			want: []Item{{
				Header: "识别方式", Question: "怎么识别？",
				Options: []Option{{Label: "围栏约定"}},
			}},
		},
		{
			name: "parameters wrapper with choices",
			raw: map[string]any{
				"parameters": []any{map[string]any{
					"question": "背景层怎么处理？",
					"choices": []any{
						map[string]any{"label": "顺手一起修"},
						map[string]any{"label": "只做模糊"},
					},
				}},
			},
			want: []Item{{
				Question: "背景层怎么处理？",
				Options:  []Option{{Label: "顺手一起修"}, {Label: "只做模糊"}},
			}},
		},
		{
			name: "message instead of question",
			raw: map[string]any{
				"message": "架构图现在有两种改法，想确认你的偏好：",
				"options": []any{map[string]any{"label": "只删截图素材"}},
			},
			want: []Item{{
				Question: "架构图现在有两种改法，想确认你的偏好：",
				Options:  []Option{{Label: "只删截图素材"}},
			}},
		},
		{
			name: "title instead of question",
			raw: map[string]any{
				"title": "架构图改动怎么处理",
				"options": []any{
					map[string]any{"label": "只删截图素材", "description": "保留深色化"},
				},
			},
			want: []Item{{
				Question: "架构图改动怎么处理",
				Options:  []Option{{Label: "只删截图素材", Description: "保留深色化"}},
			}},
		},
		{
			name: "string multiSelect coerced",
			raw: map[string]any{
				"questions": []any{map[string]any{
					"header": "Features", "multiSelect": "true",
					"question": "选哪些？",
					"options":  []any{map[string]any{"label": "Auth"}},
				}},
			},
			want: []Item{{
				Header: "Features", MultiSelect: true, Question: "选哪些？",
				Options: []Option{{Label: "Auth"}},
			}},
		},
		{
			name: "string options coerced to objects",
			raw: map[string]any{
				"questions": []any{map[string]any{
					"question": "选哪个？",
					"options":  []any{"A", "B"},
				}},
			},
			want: []Item{{
				Question: "选哪个？",
				Options:  []Option{{Label: "A"}, {Label: "B"}},
			}},
		},
		{
			name: "option label from value key",
			raw: map[string]any{
				"questions": []any{map[string]any{
					"question": "选哪个？",
					"options": []any{
						map[string]any{"value": "restore_only", "description": "只改滚动"},
					},
				}},
			},
			want: []Item{{
				Question: "选哪个？",
				Options:  []Option{{Label: "restore_only", Description: "只改滚动"}},
			}},
		},
		{
			name: "hallucinated type object",
			raw:  map[string]any{"type": "ask-question"},
			want: nil,
		},
		{
			name: "hallucinated askUserQuestion boolean",
			raw:  map[string]any{"askUserQuestion": true},
			want: nil,
		},
		{
			name: "hallucinated taskId",
			raw:  map[string]any{"taskId": ""},
			want: nil,
		},
		{
			name: "hallucinated schema array",
			raw: map[string]any{"schema": []any{
				map[string]any{"name": "header", "type": "string"},
			}},
			want: nil,
		},
		{
			name: "empty object",
			raw:  map[string]any{},
			want: nil,
		},
		{
			name: "bare enum map yields nothing",
			raw: map[string]any{
				`"1`: "Browser (Chrome/Firefox on desktop)",
				"1":  "Browser (Chrome/Firefox on desktop)",
				"2":  "Android App (WebView)",
			},
			want: nil,
		},
		{
			name: "flat question only is kept",
			raw:  map[string]any{"question": "存量统计展示在哪里？"},
			want: []Item{{Question: "存量统计展示在哪里？", Options: []Option{}}},
		},
		{
			name: "raw xml string payload",
			raw: map[string]any{
				"ask": `<item><header>触发范围</header><question>覆盖范围？</question><option><label>全局</label><description>一处改动全局生效</description></option></item>`,
			},
			want: []Item{{
				Header: "触发范围", Question: "覆盖范围？",
				Options: []Option{{Label: "全局", Description: "一处改动全局生效"}},
			}},
		},
		{
			name: "extra unknown keys ignored",
			raw: map[string]any{
				"questions": []any{map[string]any{
					"question": "问题", "note": "备注", "sub": "副标题",
					"options": []any{map[string]any{"label": "A"}},
				}},
			},
			want: []Item{{Question: "问题", Options: []Option{{Label: "A"}}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeInput(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("NormalizeInput mismatch\n got: %+v\nwant: %+v", got, tc.want)
			}
		})
	}
}

func TestNormalizeInput_QuestionArrayPreferredOverWrapper(t *testing.T) {
	// When both a direct questions array and a wrapper exist, the direct array
	// wins (the wrapper is a transport artifact, not the payload).
	raw := map[string]any{
		"questions": []any{map[string]any{"question": "direct"}},
		"params": map[string]any{
			"questions": []any{map[string]any{"question": "wrapped"}},
		},
	}
	got := NormalizeInput(raw)
	if len(got) != 1 || got[0].Question != "direct" {
		t.Fatalf("expected the direct questions array to win, got %+v", got)
	}
}

// Go map iteration is randomized, so a payload with two keys folding to the
// same canonical form used to resolve differently run to run. Loop enough
// times that the old first-match behavior would fail deterministically.
func TestNormalizeInput_DuplicateCanonicalKeysAreDeterministic(t *testing.T) {
	for i := range 300 {
		got := NormalizeInput(map[string]any{
			"question":       "UNQUOTED",
			`"question`:      "QUOTED",
			"QUESTion":       "OTHER",
			"question" + " ": "TRAILING",
		})
		if len(got) != 1 {
			t.Fatalf("run %d: expected 1 item, got %+v", i, got)
		}
		// The exact canonical key wins over every decorated variant.
		if got[0].Question != "UNQUOTED" {
			t.Fatalf("run %d: expected the exact key to win, got %q", i, got[0].Question)
		}
	}
}

func TestCanonicalKey_Separators(t *testing.T) {
	// A tab IS a separator (aligned with the TS mirror); a NBSP is not.
	if got := canonicalKey("multi\tselect"); got != "multiselect" {
		t.Errorf("tab should be a separator, got %q", got)
	}
	if got := canonicalKey("multi-select"); got != "multiselect" {
		t.Errorf("hyphen should be a separator, got %q", got)
	}
	if got := canonicalKey("multi_select"); got != "multiselect" {
		t.Errorf("underscore should be a separator, got %q", got)
	}
	if got := canonicalKey("multiSelect"); got != "multiselect" {
		t.Errorf("camelCase should fold, got %q", got)
	}
	if got := canonicalKey(`"question`); got != "question" {
		t.Errorf("stray quote should be stripped, got %q", got)
	}
}
