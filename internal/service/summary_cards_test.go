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
		{Type: "tool_use", Name: "PermissionApproval", ID: "t3", Input: map[string]any{"toolName": "Bash"}, Done: true, Status: "error", Output: "Cancelled"},
		{Type: "text", Text: "Answer <scheduled-task id=\"42\">x</scheduled-task> <ask-question><item><header>Q</header><question>continue?</question><option><label>Yes</label></option></item></ask-question>"},
	}
	cards := extractSummaryCards(blocks)
	if len(cards.Tools) != 2 {
		t.Fatalf("expected 2 auto-expand tools, got %d: %+v", len(cards.Tools), cards.Tools)
	}
	if cards.Tools[0].Name != "AskUserQuestion" || cards.Tools[0].ID != "t2" {
		t.Fatalf("expected AskUserQuestion t2 tool, got: %+v", cards.Tools[0])
	}
	perm := cards.Tools[1]
	if perm.Name != "PermissionApproval" || !perm.Done || perm.Status != "error" || perm.Output != "Cancelled" {
		t.Fatalf("PermissionApproval result state not captured: %+v", perm)
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

func TestExtractSummaryCardsFileChanges(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "tool_use", Name: "Write", ID: "w1", FilePath: "/src/new.go", Done: true},
		{Type: "tool_use", Name: "Write", ID: "w2", FilePath: "/src/dup.go", Done: true},
		{Type: "tool_use", Name: "Edit", ID: "e1", FilePath: "/src/a.go", Done: true},
		{Type: "tool_use", Name: "Edit", ID: "e2", FilePath: "/src/a.go", Done: true},
		{Type: "tool_use", Name: "Write", ID: "w3", Done: false, FilePath: "/src/notdone.go"},
		{Type: "tool_use", Name: "Edit", ID: "e3", Done: true, Input: map[string]any{"file_path": "/src/via-input.go"}},
	}
	cards := extractSummaryCards(blocks)
	if len(cards.CreatedFiles) != 2 {
		t.Fatalf("expected 2 created files, got %+v", cards.CreatedFiles)
	}
	if cards.CreatedFiles[0] != "/src/new.go" || cards.CreatedFiles[1] != "/src/dup.go" {
		t.Fatalf("created mismatch: %+v", cards.CreatedFiles)
	}
	if len(cards.ModifiedFiles) != 2 {
		t.Fatalf("expected 2 modified files (dedup + input fallback), got %+v", cards.ModifiedFiles)
	}
	if cards.ModifiedFiles[0] != "/src/a.go" || cards.ModifiedFiles[1] != "/src/via-input.go" {
		t.Fatalf("modified mismatch: %+v", cards.ModifiedFiles)
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
