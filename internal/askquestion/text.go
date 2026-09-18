package askquestion

import (
	"strings"
)

// PlainText renders items as a spoken-language summary, used by TTS and by the
// recommendation prompt. It never emits raw tags.
//
// Shape: "Question (Header): Option — description, Option2"
func PlainText(items []Item) string {
	var b strings.Builder
	for i, it := range items {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(it.Question)
		if it.Header != "" {
			b.WriteString(" (")
			b.WriteString(it.Header)
			b.WriteString(")")
		}
		if len(it.Options) > 0 {
			b.WriteString(": ")
			writeOptions(&b, it.Options)
		}
	}
	return b.String()
}

// writeOptions renders the option list, appending a description only when it
// adds information beyond the label.
func writeOptions(b *strings.Builder, opts []Option) {
	for j, opt := range opts {
		if j > 0 {
			b.WriteString(", ")
		}
		b.WriteString(opt.Label)
		if opt.Description != "" && opt.Description != opt.Label {
			b.WriteString(" — ")
			b.WriteString(opt.Description)
		}
	}
}

// ToInputMap converts items into the `{"questions": [...]}` shape stored in a
// ContentBlock.Input for the AskUserQuestion tool. Field names and the
// multiSelect spelling match internal/model's JSON contract.
func ToInputMap(items []Item) map[string]any {
	questions := make([]map[string]any, 0, len(items))
	for _, it := range items {
		opts := make([]map[string]any, 0, len(it.Options))
		for _, o := range it.Options {
			opt := map[string]any{"label": o.Label}
			if o.Description != "" {
				opt["description"] = o.Description
			}
			opts = append(opts, opt)
		}
		questions = append(questions, map[string]any{
			"header":      it.Header,
			"multiSelect": it.MultiSelect,
			"question":    it.Question,
			"options":     opts,
		})
	}
	return map[string]any{"questions": questions}
}
