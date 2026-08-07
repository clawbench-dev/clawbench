package service

import (
	"testing"

	"clawbench/internal/model"
)

func TestExtractSummaryCards(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "reasoning"},
		{Type: "tool_use", Name: "Bash", ID: "t1", Input: map[string]any{"command": "ls"}},
		{Type: "tool_use", Name: "AskUserQuestion", ID: "t2", Input: map[string]any{"question": "go?"}},
		{Type: "text", Text: "Answer <scheduled-task id=\"42\">x</scheduled-task> <ask-question>continue?</ask-question>"},
	}
	cards := extractSummaryCards(blocks)
	if len(cards.Tools) != 1 {
		t.Fatalf("expected 1 auto-expand tool, got %d: %+v", len(cards.Tools), cards.Tools)
	}
	if cards.Tools[0].Name != "AskUserQuestion" || cards.Tools[0].ID != "t2" {
		t.Fatalf("expected AskUserQuestion t2 tool, got: %+v", cards.Tools[0])
	}
	if len(cards.TaskIDs) != 1 || cards.TaskIDs[0] != 42 {
		t.Fatalf("taskIDs mismatch: %+v", cards.TaskIDs)
	}
	if len(cards.AskQuestions) != 1 {
		t.Fatalf("expected 1 ask-question, got %d: %+v", len(cards.AskQuestions), cards.AskQuestions)
	}
}
