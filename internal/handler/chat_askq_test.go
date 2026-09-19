package handler

import (
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// JSON is not the documented format and is not recovered: the payload degrades
// to visible text so malformed output is surfaced rather than masked.
func TestConvertAskQuestionBlocks_JSONPayloadDegrades(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Here is my analysis.\n\n<clawbench-ask-question>\n{\"questions\":[{\"question\":\"Which approach?\",\"options\":[{\"label\":\"Option A\"}]}]}\n</clawbench-ask-question>"},
	}

	result := ai.ConvertAskQuestionBlocks(blocks)

	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			t.Fatalf("JSON must not become a card, got %+v", b)
		}
	}
	if len(result) != 1 || !strings.Contains(result[0].Text, "Which approach?") {
		t.Fatalf("the payload text must be retained, got %+v", result)
	}
}

func TestConvertAskQuestionBlocks_StripsTagFromText(t *testing.T) {
	// A converted tag must be removed from the text block, otherwise the card
	// and the raw markup both render.
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Here is my analysis.\n\n---\n\n<clawbench-ask-question>\n**Pick**\nWhich one?\n- A — Option A\n</clawbench-ask-question>"},
	}

	result := ai.ConvertAskQuestionBlocks(blocks)

	askQCount := 0
	textHasAskTag := false
	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			askQCount++
		}
		if b.Type == "text" && strings.Contains(b.Text, "<clawbench-ask-question") {
			textHasAskTag = true
		}
	}

	if askQCount != 1 {
		t.Errorf("expected 1 AskUserQuestion tool_use block, got %d", askQCount)
	}
	if textHasAskTag {
		t.Error("text block should NOT contain the tag - it must be stripped to avoid duplicate cards")
	}
}

func TestConvertAskQuestionBlocks_IDUsesUUID(t *testing.T) {
	// Verify that the tool_use block ID uses UUID format ("ask-" + UUID)
	blocks := []model.ContentBlock{
		{Type: "text", Text: "<clawbench-ask-question>\n**Pick**\nWhich one?\n- A — Option A\n</clawbench-ask-question>"},
	}

	result := ai.ConvertAskQuestionBlocks(blocks)

	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			if !strings.HasPrefix(b.ID, "ask-") {
				t.Errorf("expected ID to start with 'ask-', got %q", b.ID)
			}
			uuidPart := strings.TrimPrefix(b.ID, "ask-")
			if len(uuidPart) != 36 {
				t.Errorf("expected UUID part to be 36 chars, got %d (ID=%q)", len(uuidPart), b.ID)
			}
			for i, c := range uuidPart {
				switch i {
				case 8, 13, 18, 23:
					if c != '-' {
						t.Errorf("expected dash at position %d in UUID, got %c (ID=%q)", i, c, b.ID)
					}
				default:
					if c < '0' || c > '9' && c < 'a' || c > 'f' {
						t.Errorf("expected hex digit at position %d in UUID, got %c (ID=%q)", i, c, b.ID)
					}
				}
			}
			return
		}
	}
	t.Error("expected to find an AskUserQuestion tool_use block")
}

func TestConvertAskQuestionBlocks_IDsAreUnique(t *testing.T) {
	ids := make(map[string]bool)
	for range 10 {
		blocks := []model.ContentBlock{
			{Type: "text", Text: "<clawbench-ask-question>\n**Pick**\nWhich one?\n- A — Option A\n</clawbench-ask-question>"},
		}

		result := ai.ConvertAskQuestionBlocks(blocks)
		for _, b := range result {
			if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
				if ids[b.ID] {
					t.Errorf("duplicate ID generated: %q", b.ID)
				}
				ids[b.ID] = true
			}
		}
	}
	if len(ids) != 10 {
		t.Errorf("expected 10 unique IDs, got %d", len(ids))
	}
}
