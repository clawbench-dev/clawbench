package service

import (
	"encoding/json"
	"strings"
	"testing"

	"clawbench/internal/model"
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
	out := assistantConclusionFromBlocks(parseBlocksForTest(t, content))
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
	out := assistantConclusionFromBlocks(parseBlocksForTest(t, content))
	if strings.Contains(out, "Question:") {
		t.Fatalf("unexpected ask-question marker, got: %q", out)
	}
	if !strings.Contains(out, "Here is the plan.") {
		t.Fatalf("expected conclusion, got: %q", out)
	}
}

func parseBlocksForTest(t *testing.T, content string) []model.ContentBlock {
	t.Helper()
	var wrapper struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(content), &wrapper); err != nil {
		t.Fatalf("parse blocks: %v", err)
	}
	return wrapper.Blocks
}

// TestQuickCommandDetails_OmitsLabel verifies only the command body is injected
// (no "label: " prefix), so the recommendation recommends the actual command.
func TestQuickCommandDetails_OmitsLabel(t *testing.T) {
	items := []ChatQuickSendItem{
		{Label: "生成测试", Command: "run tests"},
		{Label: "", Command: "  run lint  "},
		{Label: "空命令", Command: "   "},
	}
	out := quickCommandDetails(items)
	want := []string{"run tests", "run lint"}
	if len(out) != len(want) {
		t.Fatalf("expected %d commands, got %d: %v", len(want), len(out), out)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("command[%d] = %q, want %q", i, out[i], want[i])
		}
	}
}
