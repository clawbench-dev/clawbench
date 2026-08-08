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
		{Type: "text", Text: "Answer <scheduled-task id=\"42\">x</scheduled-task> <ask-question><item><header>Q</header><question>continue?</question><option><label>Yes</label></option></item></ask-question>"},
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
	if cards.AskQuestions[0].Question != "continue?" {
		t.Fatalf("expected question 'continue?', got: %+v", cards.AskQuestions[0])
	}
	if len(cards.AskQuestions[0].Options) != 1 || cards.AskQuestions[0].Options[0].Label != "Yes" {
		t.Fatalf("expected option 'Yes', got: %+v", cards.AskQuestions[0].Options)
	}
}

func TestExtractSummaryCardsAskQuestion(t *testing.T) {
	blocks := []model.ContentBlock{{
		Type: "text",
		Text: `<ask-question><item><header>Setup</header><multi-select>true</multi-select><question>score < 5 ok?</question><option><label>Yes</label><description>confirm</description></option><option><label>No</label></option></item></ask-question>`,
	}}
	cards := extractSummaryCards(blocks)
	if len(cards.AskQuestions) != 1 {
		t.Fatalf("expected 1 ask-question, got %d", len(cards.AskQuestions))
	}
	aq := cards.AskQuestions[0]
	if aq.Header != "Setup" || !aq.MultiSelect || aq.Question != "score < 5 ok?" {
		t.Fatalf("ask-question mismatch: %+v", aq)
	}
	if len(aq.Options) != 2 {
		t.Fatalf("expected 2 options, got %d: %+v", len(aq.Options), aq.Options)
	}
	if aq.Options[0].Label != "Yes" || aq.Options[0].Description != "confirm" {
		t.Fatalf("option[0] mismatch: %+v", aq.Options[0])
	}
	if aq.Options[1].Label != "No" || aq.Options[1].Description != "" {
		t.Fatalf("option[1] mismatch: %+v", aq.Options[1])
	}
}

func TestExtractSummaryCardsAskQuestionPerItemHeader(t *testing.T) {
	blocks := []model.ContentBlock{{
		Type: "text",
		Text: `<ask-question><item><header>First</header><question>q1?</question><option><label>A</label></option></item><item><header>Second</header><question>q2?</question><option><label>B</label></option></item></ask-question>`,
	}}
	cards := extractSummaryCards(blocks)
	if len(cards.AskQuestions) != 2 {
		t.Fatalf("expected 2 ask-questions, got %d: %+v", len(cards.AskQuestions), cards.AskQuestions)
	}
	if cards.AskQuestions[0].Header != "First" || cards.AskQuestions[1].Header != "Second" {
		t.Fatalf("per-item headers mismatch: %+v", cards.AskQuestions)
	}
	if cards.AskQuestions[0].Question != "q1?" || cards.AskQuestions[1].Question != "q2?" {
		t.Fatalf("per-item questions mismatch: %+v", cards.AskQuestions)
	}
}
