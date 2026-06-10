package handler

import (
	"strings"
	"testing"

	"clawbench/internal/model"
)

func TestConvertAskQuestionBlocks_JSONFormat(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Here is my analysis.\n\n<ask-question>\n{\"questions\":[{\"header\":\"Approach\",\"multiSelect\":false,\"question\":\"Which approach?\",\"options\":[{\"label\":\"Option A\",\"description\":\"Fast\"},{\"label\":\"Option B\",\"description\":\"Safe\"}]}]}\n</ask-question>"},
	}

	result := convertAskQuestionBlocks(blocks)

	askQCount := 0
	textHasAskTag := false
	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			askQCount++
			questions, ok := b.Input["questions"]
			if !ok {
				t.Error("AskUserQuestion block missing 'questions' field in input")
			}
			questionsArr, ok := questions.([]map[string]any)
			if !ok || len(questionsArr) == 0 {
				t.Errorf("AskUserQuestion 'questions' should be non-empty array, got %v", questions)
			}
			if questionsArr[0]["header"] != "Approach" {
				t.Errorf("First question header = %q, want %q", questionsArr[0]["header"], "Approach")
			}
			if questionsArr[0]["question"] != "Which approach?" {
				t.Errorf("First question text mismatch: got %q", questionsArr[0]["question"])
			}
			opts, ok := questionsArr[0]["options"].([]map[string]any)
			if !ok || len(opts) != 2 {
				t.Errorf("Expected 2 options, got %v", questionsArr[0]["options"])
			}
		}
		if b.Type == "text" && strings.Contains(b.Text, "<ask-question") {
			textHasAskTag = true
		}
	}

	if askQCount != 1 {
		t.Errorf("expected 1 AskUserQuestion tool_use block, got %d", askQCount)
	}
	if textHasAskTag {
		t.Error("text block should NOT contain <ask-question> tag - it must be stripped")
	}
}

func TestConvertAskQuestionBlocks_JSONFormat_MultipleQuestions(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "<ask-question>\n{\"questions\":[{\"header\":\"Q1\",\"multiSelect\":false,\"question\":\"First?\",\"options\":[{\"label\":\"A\"}]},{\"header\":\"Q2\",\"multiSelect\":true,\"question\":\"Second?\",\"options\":[{\"label\":\"B\"},{\"label\":\"C\"}]}]}\n</ask-question>"},
	}

	result := convertAskQuestionBlocks(blocks)

	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			questionsArr, ok := b.Input["questions"].([]map[string]any)
			if !ok || len(questionsArr) != 2 {
				t.Errorf("Expected 2 questions, got %v", b.Input["questions"])
			}
			if questionsArr[1]["multiSelect"] != true {
				t.Error("Second question should be multiSelect=true")
			}
			return
		}
	}
	t.Error("expected to find an AskUserQuestion tool_use block")
}

func TestParseAskQuestionJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantOK  bool
		wantLen int
	}{
		{
			name:    "valid JSON with single question",
			input:   `{"questions":[{"header":"Pick","multiSelect":false,"question":"Which?","options":[{"label":"A","description":"Fast"}]}]}`,
			wantOK:  true,
			wantLen: 1,
		},
		{
			name:    "valid JSON with multiple questions",
			input:   `{"questions":[{"header":"Q1","question":"First?","options":[{"label":"A"}]},{"header":"Q2","question":"Second?","options":[{"label":"B"}]}]}`,
			wantOK:  true,
			wantLen: 2,
		},
		{
			name:    "invalid JSON",
			input:   `{not valid json}`,
			wantOK:  false,
			wantLen: 0,
		},
		{
			name:    "JSON without questions",
			input:   `{"other":"data"}`,
			wantOK:  false,
			wantLen: 0,
		},
		{
			name:    "JSON with empty questions",
			input:   `{"questions":[]}`,
			wantOK:  false,
			wantLen: 0,
		},
		{
			name:    "JSON question without options",
			input:   `{"questions":[{"question":"Q?","options":[]}]}`,
			wantOK:  false,
			wantLen: 0,
		},
		{
			name:    "JSON question without question text",
			input:   `{"questions":[{"options":[{"label":"A"}]}]}`,
			wantOK:  false,
			wantLen: 0,
		},
		{
			name:    "JSON option without label skipped",
			input:   `{"questions":[{"question":"Q?","options":[{"description":"no label"}]}]}`,
			wantOK:  false,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseAskQuestionJSON(tt.input)
			if tt.wantOK {
				if result == nil {
					t.Error("expected non-nil result")
					return
				}
				questions, ok := result["questions"].([]map[string]any)
				if !ok {
					t.Error("questions is not []map[string]any")
					return
				}
				if len(questions) != tt.wantLen {
					t.Errorf("expected %d questions, got %d", tt.wantLen, len(questions))
				}
			} else if result != nil {
				t.Errorf("expected nil result, got %v", result)
			}
		})
	}
}

func TestExtractXMLCandidate_JSON(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		wantOK bool
	}{
		{
			name:   "valid JSON with questions",
			raw:    `{"questions":[{"question":"Which?","options":[{"label":"A"}]}]}`,
			wantOK: true,
		},
		{
			name:   "JSON without questions key",
			raw:    `{"other":"data"}`,
			wantOK: false,
		},
		{
			name:   "JSON with empty questions array",
			raw:    `{"questions":[]}`,
			wantOK: false,
		},
		{
			name:   "invalid JSON",
			raw:    `{not json}`,
			wantOK: false,
		},
		{
			name:   "JSON question without options",
			raw:    `{"questions":[{"question":"Q?"}]}`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractXMLCandidate(tt.raw)
			if tt.wantOK && got == "" {
				t.Error("extractXMLCandidate() returned empty, expected valid content")
			}
			if !tt.wantOK && got != "" {
				t.Errorf("extractXMLCandidate() = %q, expected empty string", got)
			}
		})
	}
}

func TestConvertAskQuestionBlocks_WrongCloseTag_StripsTagFromText(t *testing.T) {
	// Regression test: When Strategy 2 (wrong-close regex) matches a non-standard
	// closing tag instead of the standard </ask-question>, the <ask-question> content
	// must be stripped from the text block.
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Here is my analysis.\n\n---\n\n<ask-question>\n<item><header>Pick</header><multi-select>false</multi-select><question>Which one?</question><option><label>A</label><description>Option A</description></option></item>\n</ask-question>"},
	}

	result := convertAskQuestionBlocks(blocks)

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

	result := convertAskQuestionBlocks(blocks)

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

		result := convertAskQuestionBlocks(blocks)
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
