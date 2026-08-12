package service

import (
	"strings"
	"testing"
)

// TestAssistantConclusion_IncludesAskQuestion ensures the assistant message
// injected into the recommendation prompt also carries AskUserQuestion cards so
// the AI can recommend one of the options.
func TestAssistantConclusion_IncludesAskQuestion(t *testing.T) {
	content := `{"blocks":[
		{"type":"text","text":"I need your choice."},
		{"type":"tool_use","name":"AskUserQuestion","input":{"questions":[
			{"header":"Approach","multiSelect":false,"question":"Which approach?","options":[
				{"label":"Fast","description":"quick but risky"},
				{"label":"Safe","description":"slower"}
			]}
		]}}
	]}`
	out := assistantConclusion(content)
	if !strings.Contains(out, "I need your choice.") {
		t.Fatalf("expected conclusion text preserved, got: %q", out)
	}
	if !strings.Contains(out, "Which approach?") {
		t.Fatalf("expected ask question included, got: %q", out)
	}
	if !strings.Contains(out, "Fast") || !strings.Contains(out, "Safe") {
		t.Fatalf("expected ask options included, got: %q", out)
	}
	if !strings.Contains(out, "quick but risky") {
		t.Fatalf("expected option description included, got: %q", out)
	}
}

// TestAssistantConclusion_NoAskQuestion stays unchanged when no card is present.
func TestAssistantConclusion_NoAskQuestion(t *testing.T) {
	content := `{"blocks":[{"type":"text","text":"Here is the plan."},{"type":"text","text":"Let me know."}]}`
	out := assistantConclusion(content)
	if strings.Contains(out, "Question:") {
		t.Fatalf("unexpected ask-question marker, got: %q", out)
	}
	if !strings.Contains(out, "Here is the plan.") {
		t.Fatalf("expected conclusion, got: %q", out)
	}
}

// TestAssistantConclusion_PlainText passes through non-block content unchanged.
func TestAssistantConclusion_PlainText(t *testing.T) {
	in := "just some plain assistant output"
	if out := assistantConclusion(in); out != in {
		t.Fatalf("expected pass-through, got: %q", out)
	}
}
