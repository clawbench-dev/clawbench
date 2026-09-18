package askquestion

import (
	"encoding/json"
	"strings"
)

// Canonical key groups. Keys are matched after canonicalKey, so a model may
// spell them `multi-select`, `multi_select`, `multiSelect`, or `multiselect`.
var (
	questionArrayKeys = []string{"questions", "items", "parameters"}
	wrapperKeys       = []string{"params", "parameters", "data", "input", "args"}
	questionKeys      = []string{"question", "message", "title", "text", "prompt"}
	optionKeys        = []string{"options", "choices", "answers", "values"}
	labelKeys         = []string{"label", "value", "text", "title"}
	descKeys          = []string{"description", "desc", "detail"}
	multiKeys         = []string{"multiselect", "multiple", "multi"}
	// xmlStringKeys hold a raw <item> payload serialized as a JSON string.
	xmlStringKeys = []string{"ask", "prompt", "questions", "content"}
)

// canonicalKey folds a raw map key to its comparison form: it strips the
// stray quote/space characters some models emit (real data contains a literal
// `"question` key) and removes separators, so `multi-select`, `multi_select`
// and `multiSelect` all collapse to `multiselect`.
func canonicalKey(k string) string {
	k = strings.TrimSpace(k)
	k = strings.Trim(k, `"'`)
	k = strings.ToLower(k)
	k = strings.NewReplacer("-", "", "_", "", " ", "").Replace(k)
	return k
}

// lookup finds the first key whose canonical form equals canonical(canonicalName).
func lookup(m map[string]any, canonicalName string) (any, bool) {
	for k, v := range m {
		if canonicalKey(k) == canonicalName {
			return v, true
		}
	}
	return nil, false
}

// NormalizeInput converts a Path-A tool input into canonical items.
//
// It accepts every malformed shape observed in production: a flat
// {question, options} object without the `questions` wrapper, `{items:[...]}`,
// `{params:{items:[...]}}`, `{parameters:[...]}`, `choices` instead of
// `options`, `message`/`title` instead of `question`, string-typed booleans and
// option arrays, and a raw `<item>` XML payload parked in a string field.
//
// Objects that carry no question and no option — hallucinated shapes such as
// {type:"ask-question"}, {askUserQuestion:true}, {taskId:""} or {schema:[...]}
// — yield no items rather than an invented question.
func NormalizeInput(raw map[string]any) []Item {
	if len(raw) == 0 {
		return nil
	}
	if arr := findQuestionArray(raw, 0); arr != nil {
		return itemsFromArray(arr)
	}
	if xmlText := findXMLString(raw); xmlText != "" {
		// The string may be a raw <item> payload (no <ask-question> wrapper),
		// so parse it directly as well as through Extract.
		if items := ParseItems(xmlText); len(items) > 0 {
			return items
		}
		for _, m := range Extract(xmlText) {
			if m.Parsed {
				return m.Items
			}
		}
		return nil
	}
	// No wrapper at all: the object may itself be a single question.
	if it, ok := normalizeItem(raw); ok {
		return []Item{it}
	}
	return nil
}

// findQuestionArray locates the questions array, unwrapping one level of
// parameter-style nesting (`{params:{items:[...]}}`). depth caps the recursion.
func findQuestionArray(m map[string]any, depth int) []any {
	for _, key := range questionArrayKeys {
		if v, ok := lookup(m, key); ok {
			if arr := asArray(v); arr != nil {
				return arr
			}
		}
	}
	if depth >= 2 {
		return nil
	}
	for _, key := range wrapperKeys {
		v, ok := lookup(m, key)
		if !ok {
			continue
		}
		if inner, ok := v.(map[string]any); ok {
			if arr := findQuestionArray(inner, depth+1); arr != nil {
				return arr
			}
		}
	}
	return nil
}

// findXMLString returns a string field that looks like a raw <item> payload.
func findXMLString(m map[string]any) string {
	for _, key := range xmlStringKeys {
		v, ok := lookup(m, key)
		if !ok {
			continue
		}
		s, ok := v.(string)
		if ok && strings.Contains(s, "<item") {
			return s
		}
	}
	return ""
}

// asArray accepts a JSON array or a string holding one (models sometimes
// double-encode the options list as a JSON string).
func asArray(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case string:
		s := strings.TrimSpace(t)
		if !strings.HasPrefix(s, "[") {
			return nil
		}
		var arr []any
		if err := json.Unmarshal([]byte(s), &arr); err != nil {
			return nil
		}
		return arr
	}
	return nil
}

// itemsFromArray normalizes each element, dropping unrenderable entries.
func itemsFromArray(arr []any) []Item {
	var items []Item
	for _, el := range arr {
		m, ok := el.(map[string]any)
		if !ok {
			continue
		}
		if it, ok := normalizeItem(m); ok {
			items = append(items, it)
		}
	}
	return items
}

// normalizeItem maps one question object onto Item. It reports false when the
// object carries neither question text nor options, which is how the
// hallucinated shapes are discarded.
func normalizeItem(m map[string]any) (Item, bool) {
	it := Item{
		Question: firstString(m, questionKeys),
		Header:   firstString(m, []string{"header"}),
		Options:  optionsFrom(m),
	}
	it.MultiSelect = firstBool(m, multiKeys)
	if it.Options == nil {
		// Keep the JSON shape identical to the TypeScript mirror, which always
		// yields an array.
		it.Options = []Option{}
	}
	if strings.TrimSpace(it.Question) == "" && len(it.Options) == 0 {
		return Item{}, false
	}
	return it, true
}

// optionsFrom reads the option list under any accepted synonym.
func optionsFrom(m map[string]any) []Option {
	for _, key := range optionKeys {
		v, ok := lookup(m, key)
		if !ok {
			continue
		}
		if arr := asArray(v); arr != nil {
			return normalizeOptions(arr)
		}
	}
	return nil
}

// normalizeOptions converts string and object options to Option, skipping
// entries with no usable label.
func normalizeOptions(arr []any) []Option {
	var opts []Option
	for _, el := range arr {
		switch t := el.(type) {
		case string:
			if s := strings.TrimSpace(t); s != "" {
				opts = append(opts, Option{Label: s})
			}
		case map[string]any:
			label := firstString(t, labelKeys)
			if label == "" {
				continue
			}
			opts = append(opts, Option{
				Label:       label,
				Description: firstString(t, descKeys),
			})
		}
	}
	return opts
}

// firstString returns the first non-empty string under any of the keys.
func firstString(m map[string]any, keys []string) string {
	for _, key := range keys {
		v, ok := lookup(m, key)
		if !ok {
			continue
		}
		if s, ok := v.(string); ok {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// firstBool reads a boolean that may have been emitted as the string
// "true"/"false" (observed in production).
func firstBool(m map[string]any, keys []string) bool {
	for _, key := range keys {
		v, ok := lookup(m, key)
		if !ok {
			continue
		}
		switch t := v.(type) {
		case bool:
			return t
		case string:
			return strings.EqualFold(strings.TrimSpace(t), "true")
		}
	}
	return false
}
