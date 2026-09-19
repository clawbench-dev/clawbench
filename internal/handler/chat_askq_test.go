package handler

import (
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// JSON is not the documented format, but recovering it beats discarding a
// readable question.
func TestConvertAskQuestionBlocks_JSONContentIsRecovered(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Here is my analysis.\n\n<ask-question>\n{\"questions\":[{\"header\":\"Approach\",\"multiSelect\":false,\"question\":\"Which approach?\",\"options\":[{\"label\":\"Option A\",\"description\":\"Fast\"}]}]}\n</ask-question>"},
	}

	result := ai.ConvertAskQuestionBlocks(blocks)

	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			qs, _ := b.Input["questions"].([]map[string]any)
			if len(qs) != 1 || qs[0]["question"] != "Which approach?" {
				t.Fatalf("unexpected recovered questions: %+v", qs)
			}
			return
		}
	}
	t.Fatalf("expected the JSON payload to be recovered, got: %+v", result)
}

func TestConvertAskQuestionBlocks_WrongCloseTag_StripsTagFromText(t *testing.T) {
	// Regression test: When Strategy 2 (wrong-close regex) matches a non-standard
	// closing tag instead of the standard </ask-question>, the <ask-question> content
	// must be stripped from the text block.
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Here is my analysis.\n\n---\n\n<ask-question>\n<item><header>Pick</header><multi-select>false</multi-select><question>Which one?</question><option><label>A</label><description>Option A</description></option></item>\n</ask-question>"},
	}

	result := ai.ConvertAskQuestionBlocks(blocks)

	askQCount := 0
	textHasAskTag := false
	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			askQCount++
		}
		if b.Type == "text" && strings.Contains(b.Text, "<ask-question") {
			textHasAskTag = true
		}
	}

	if askQCount != 1 {
		t.Errorf("expected 1 AskUserQuestion tool_use block, got %d", askQCount)
	}
	if textHasAskTag {
		t.Error("text block should NOT contain <ask-question> tag - it must be stripped to avoid duplicate cards")
	}
}

func TestConvertAskQuestionBlocks_IDUsesUUID(t *testing.T) {
	// Verify that the tool_use block ID uses UUID format ("ask-" + UUID)
	blocks := []model.ContentBlock{
		{Type: "text", Text: "<ask-question>\n<item><header>Pick</header><multi-select>false</multi-select><question>Which one?</question><option><label>A</label><description>Option A</description></option></item>\n</ask-question>"},
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
			{Type: "text", Text: "<ask-question>\n<item><header>Pick</header><multi-select>false</multi-select><question>Which one?</question><option><label>A</label><description>Option A</description></option></item>\n</ask-question>"},
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
