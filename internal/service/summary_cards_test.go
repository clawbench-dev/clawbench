package service

import (
	"strings"
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
	// PermissionApproval is intentionally excluded: it is actionable-only and
	// cannot be answered from the read-only summary view.
	if len(cards.Tools) != 1 {
		t.Fatalf("expected 1 summary-card tool (AskUserQuestion only), got %d: %+v", len(cards.Tools), cards.Tools)
	}
	if cards.Tools[0].Name != "AskUserQuestion" || cards.Tools[0].ID != "t2" {
		t.Fatalf("expected AskUserQuestion t2 tool, got: %+v", cards.Tools[0])
	}
	// Explicit absence check: the PermissionApproval block (t3) must not leak in.
	for _, tool := range cards.Tools {
		if strings.EqualFold(tool.Name, "PermissionApproval") {
			t.Fatalf("PermissionApproval must not be persisted into summary cards: %+v", tool)
		}
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
	if cards.CreatedFiles[0].Path != "/src/new.go" || cards.CreatedFiles[1].Path != "/src/dup.go" {
		t.Fatalf("created mismatch: %+v", cards.CreatedFiles)
	}
	if len(cards.CreatedFiles[0].ToolIDs) != 1 || cards.CreatedFiles[0].ToolIDs[0] != "w1" {
		t.Fatalf("created[0] toolIDs mismatch: %+v", cards.CreatedFiles[0])
	}
	if len(cards.ModifiedFiles) != 2 {
		t.Fatalf("expected 2 modified files (dedup + input fallback), got %+v", cards.ModifiedFiles)
	}
	if cards.ModifiedFiles[0].Path != "/src/a.go" || cards.ModifiedFiles[1].Path != "/src/via-input.go" {
		t.Fatalf("modified mismatch: %+v", cards.ModifiedFiles)
	}
	if len(cards.ModifiedFiles[0].ToolIDs) != 2 || cards.ModifiedFiles[0].ToolIDs[0] != "e1" || cards.ModifiedFiles[0].ToolIDs[1] != "e2" {
		t.Fatalf("modified[0] toolIDs mismatch (e1,e2 dedup): %+v", cards.ModifiedFiles[0])
	}
	if len(cards.ModifiedFiles[1].ToolIDs) != 1 || cards.ModifiedFiles[1].ToolIDs[0] != "e3" {
		t.Fatalf("modified[1] toolIDs mismatch (via-input): %+v", cards.ModifiedFiles[1])
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

func TestExtractSummaryCardsWarnings(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "warning", Text: "Server restarted, AI response interrupted", Reason: "restart"},
		{Type: "error", Text: "AI backend exited", Reason: "backend_exit", ErrorCode: -32603, HTTPStatus: 500, ErrorSource: "agent"},
		{Type: "text", Text: "ordinary answer"},
		{Type: "tool_use", Name: "Bash", ID: "t1", Done: true, Status: "success"},
	}
	cards := extractSummaryCards(blocks)

	if len(cards.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %+v", len(cards.Warnings), cards.Warnings)
	}
	// Restart warning (amber banner + continue button on the frontend).
	w0 := cards.Warnings[0]
	if w0.Type != "warning" || w0.Text != "Server restarted, AI response interrupted" || w0.Reason != "restart" {
		t.Fatalf("restart warning mismatch: %+v", w0)
	}
	if w0.ErrorCode != 0 || w0.HTTPStatus != 0 || w0.ErrorSource != "" {
		t.Fatalf("restart warning should carry no structured error fields: %+v", w0)
	}
	// Error block (red banner with structured fields preserved).
	w1 := cards.Warnings[1]
	if w1.Type != "error" || w1.Text != "AI backend exited" || w1.Reason != "backend_exit" {
		t.Fatalf("error warning mismatch: %+v", w1)
	}
	if w1.ErrorCode != -32603 || w1.HTTPStatus != 500 || w1.ErrorSource != "agent" {
		t.Fatalf("error warning structured fields lost: %+v", w1)
	}
	// text/tool blocks must not be collected as warnings.
}

func TestExtractSummaryCards_ExcludesSubAgentBlocks(t *testing.T) {
	blocks := []model.ContentBlock{
		// Top-level agent actions — should appear.
		{Type: "tool_use", Name: "Write", ID: "top-w", FilePath: "/src/top.go", Done: true},
		{Type: "tool_use", Name: "AskUserQuestion", ID: "top-ask", Done: true, Input: map[string]any{}},
		// Sub-agent actions (parent_tool_call_id set) — must NOT leak into the
		// top-level summary cards.
		{Type: "tool_use", Name: "Write", ID: "sub-w", FilePath: "/src/sub.go", Done: true, ParentToolCallID: "call_p"},
		{Type: "tool_use", Name: "AskUserQuestion", ID: "sub-ask", Done: true, Input: map[string]any{}, ParentToolCallID: "call_p"},
	}
	cards := extractSummaryCards(blocks)

	for _, f := range cards.CreatedFiles {
		if f.Path == "/src/sub.go" {
			t.Fatalf("sub-agent file change leaked into summary cards: %+v", f)
		}
	}
	for _, tool := range cards.Tools {
		if tool.ID == "sub-ask" {
			t.Fatalf("sub-agent AskUserQuestion leaked into summary cards: %+v", tool)
		}
	}
	if len(cards.CreatedFiles) != 1 || cards.CreatedFiles[0].Path != "/src/top.go" {
		t.Fatalf("expected only the top-level file change, got %+v", cards.CreatedFiles)
	}
	if len(cards.Tools) != 1 || cards.Tools[0].ID != "top-ask" {
		t.Fatalf("expected only the top-level ask card, got %+v", cards.Tools)
	}
}
