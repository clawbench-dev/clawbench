package ai

import (
	"context"
	"testing"

	"clawbench/internal/model"
)

// --- SendEvent / SendFinalEvent ---

func TestSendEvent_ChannelAcceptsEvent(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	event := StreamEvent{Type: "content", Content: "hello"}

	result := SendStreamEvent(context.Background(), ch, event)

	if !result {
		t.Fatal("expected SendStreamEvent to return true when channel accepts event")
	}
	select {
	case got := <-ch:
		if got.Type != "content" || got.Content != "hello" {
			t.Fatalf("unexpected event: %+v", got)
		}
	default:
		t.Fatal("expected event on channel")
	}
}

func TestSendEvent_ContextCancelled(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// When the channel can accept AND context is cancelled, Go's select
	// picks randomly. This test verifies the function handles cancelled
	// context by returning false when the channel is full (no room to send).
	ch <- StreamEvent{Type: "content"} // fill buffer so send must block

	result := SendStreamEvent(ctx, ch, StreamEvent{Type: "thinking"})

	if result {
		t.Fatal("expected SendStreamEvent to return false when context is cancelled and channel is full")
	}
}

func TestSendEvent_ChannelFull(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "content"} // fill buffer

	result := SendStreamEvent(context.Background(), ch, StreamEvent{Type: "thinking"})

	if !result {
		t.Fatal("expected SendStreamEvent to return true (drop) when channel is full")
	}
	// The original event should still be there, not the new one
	got := <-ch
	if got.Type != "content" {
		t.Fatalf("expected original 'content' event, got %q", got.Type)
	}
}

func TestSendFinalEvent_DeliversToChannel(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	event := StreamEvent{Type: "done"}

	SendFinalStreamEvent(ch, event)

	select {
	case got := <-ch:
		if got.Type != "done" {
			t.Fatalf("expected 'done', got %q", got.Type)
		}
	default:
		t.Fatal("expected event on channel")
	}
}

func TestSendFinalEvent_ChannelFull(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "content"} // fill buffer

	// Should not block
	SendFinalStreamEvent(ch, StreamEvent{Type: "done"})

	// Original event should still be there
	got := <-ch
	if got.Type != "content" {
		t.Fatalf("expected original 'content' event, got %q", got.Type)
	}
}

// --- StringsContainsAnyBlock ---

func TestStringsContainsAnyBlock_Found(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "hmm"},
		{Type: "text", Text: "before <ask-question> after"},
	}
	if !StringsContainsAnyBlock(blocks, "<ask-question") {
		t.Fatal("expected to find <ask-question in text block")
	}
}

func TestStringsContainsAnyBlock_NotFound(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "plain text"},
	}
	if StringsContainsAnyBlock(blocks, "<ask-question") {
		t.Fatal("expected not to find <ask-question")
	}
}

func TestStringsContainsAnyBlock_Empty(t *testing.T) {
	if StringsContainsAnyBlock(nil, "<ask-question") {
		t.Fatal("expected false for nil blocks")
	}
}

// --- RemoveRejectedToolBlocks ---

func TestRemoveRejectedToolBlocks_NoRejected(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "tool_use", Name: "Read", ID: "1", Status: "success"},
	}
	result := RemoveRejectedToolBlocks(blocks)
	if len(result) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(result))
	}
}

func TestRemoveRejectedToolBlocks_RemovesRejectedTool(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "tool_use", Name: "BadTool", ID: "2", Status: "error", Output: "not found in agent cli"},
		{Type: "warning", Text: "Tool BadTool not found in agent cli"},
	}
	result := RemoveRejectedToolBlocks(blocks)
	if len(result) != 1 {
		t.Fatalf("expected 1 block (text only), got %d: %+v", len(result), result)
	}
	if result[0].Type != "text" {
		t.Fatalf("expected text block, got %q", result[0].Type)
	}
}

func TestRemoveRejectedToolBlocks_KeepsNonRejectedErrors(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "tool_use", Name: "GoodTool", ID: "3", Status: "error", Output: "permission denied"},
	}
	result := RemoveRejectedToolBlocks(blocks)
	if len(result) != 1 {
		t.Fatalf("expected 1 block, got %d", len(result))
	}
}

// --- ConvertAskQuestionBlocks ---

func TestConvertAskQuestionBlocks_XMLFormat(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<ask-question><item><header>Choice</header><multi-select>false</multi-select><question>Which one?</question><option><label>A</label><description>First</description></option></item></ask-question>`},
	}
	result := ConvertAskQuestionBlocks(blocks)

	// Should contain a tool_use block
	found := false
	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			found = true
			if b.Input == nil {
				t.Fatal("expected Input to be populated")
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected AskUserQuestion tool_use block, got: %+v", result)
	}
}

func TestConvertAskQuestionBlocks_JSONFormat(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<ask-question>{"questions":[{"question":"Pick one","header":"Choice","multiSelect":false,"options":[{"label":"A","description":"First"}]}]}</ask-question>`},
	}
	result := ConvertAskQuestionBlocks(blocks)

	found := false
	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected AskUserQuestion tool_use block, got: %+v", result)
	}
}

func TestConvertAskQuestionBlocks_NoTags(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "just normal text"},
	}
	result := ConvertAskQuestionBlocks(blocks)
	if len(result) != 1 || result[0].Type != "text" {
		t.Fatalf("expected unchanged text block, got: %+v", result)
	}
}

func TestConvertAskQuestionBlocks_TextBeforeAndAfter(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: `before <ask-question><item><header>H</header><multi-select>false</multi-select><question>Q?</question><option><label>A</label><description>D</description></option></item></ask-question> after`},
	}
	result := ConvertAskQuestionBlocks(blocks)

	// Should have both text and tool_use
	textCount := 0
	toolCount := 0
	for _, b := range result {
		if b.Type == "text" {
			textCount++
		}
		if b.Type == "tool_use" {
			toolCount++
		}
	}
	if textCount != 1 {
		t.Fatalf("expected 1 text block, got %d", textCount)
	}
	if toolCount != 1 {
		t.Fatalf("expected 1 tool_use block, got %d", toolCount)
	}
}

// --- isValidXMLCandidate ---

func TestIsValidXMLCandidate_ValidStructure(t *testing.T) {
	s := `<item><question>Q?</question><option>A</option></item>`
	if !isValidXMLCandidate(s) {
		t.Fatal("expected true for valid XML with <item>, <question>, and <option>")
	}
}

func TestIsValidXMLCandidate_ValidWithItemAttribute(t *testing.T) {
	s := `<item id="1"><question>Q?</question><option>A</option></item>`
	if !isValidXMLCandidate(s) {
		t.Fatal("expected true for XML with <item > (space after tag name)")
	}
}

func TestIsValidXMLCandidate_MissingItem(t *testing.T) {
	s := `<question>Q?</question><option>A</option>`
	if isValidXMLCandidate(s) {
		t.Fatal("expected false when <item> is missing")
	}
}

func TestIsValidXMLCandidate_MissingQuestion(t *testing.T) {
	s := `<item><option>A</option></item>`
	if isValidXMLCandidate(s) {
		t.Fatal("expected false when <question> is missing")
	}
}

func TestIsValidXMLCandidate_MissingOption(t *testing.T) {
	s := `<item><question>Q?</question></item>`
	if isValidXMLCandidate(s) {
		t.Fatal("expected false when <option> is missing")
	}
}

func TestIsValidXMLCandidate_EmptyString(t *testing.T) {
	if isValidXMLCandidate("") {
		t.Fatal("expected false for empty string")
	}
}

// --- validateJSONCandidate ---

func TestValidateJSONCandidate_Valid(t *testing.T) {
	s := `{"questions":[{"question":"Q?","options":[{"label":"A"}]}]}`
	result := validateJSONCandidate(s)
	if result != s {
		t.Fatalf("expected original string, got %q", result)
	}
}

func TestValidateJSONCandidate_InvalidJSON(t *testing.T) {
	s := `{not json}`
	result := validateJSONCandidate(s)
	if result != "" {
		t.Fatalf("expected empty string for invalid JSON, got %q", result)
	}
}

func TestValidateJSONCandidate_MissingQuestionsKey(t *testing.T) {
	s := `{"other":[]}`
	result := validateJSONCandidate(s)
	if result != "" {
		t.Fatalf("expected empty string when 'questions' key missing, got %q", result)
	}
}

func TestValidateJSONCandidate_QuestionsNotArray(t *testing.T) {
	s := `{"questions":"not array"}`
	result := validateJSONCandidate(s)
	if result != "" {
		t.Fatalf("expected empty string when questions is not an array, got %q", result)
	}
}

func TestValidateJSONCandidate_EmptyQuestionsArray(t *testing.T) {
	s := `{"questions":[]}`
	result := validateJSONCandidate(s)
	if result != "" {
		t.Fatalf("expected empty string for empty questions array, got %q", result)
	}
}

func TestValidateJSONCandidate_NoValidQuestion(t *testing.T) {
	s := `{"questions":[{"no_question":true}]}`
	result := validateJSONCandidate(s)
	if result != "" {
		t.Fatalf("expected empty string when no valid question in array, got %q", result)
	}
}

func TestValidateJSONCandidate_NotStartingWithBrace(t *testing.T) {
	s := `[]`
	result := validateJSONCandidate(s)
	if result != "" {
		t.Fatalf("expected empty string for non-object JSON, got %q", result)
	}
}

// --- hasValidQuestion ---

func TestHasValidQuestion_ValidQuestion(t *testing.T) {
	questions := []any{
		map[string]any{"question": "Q?", "options": []any{"A"}},
	}
	if !hasValidQuestion(questions) {
		t.Fatal("expected true for valid question with options")
	}
}

func TestHasValidQuestion_NoOptions(t *testing.T) {
	questions := []any{
		map[string]any{"question": "Q?", "options": []any{}},
	}
	if hasValidQuestion(questions) {
		t.Fatal("expected false when options is empty")
	}
}

func TestHasValidQuestion_OptionsNotArray(t *testing.T) {
	questions := []any{
		map[string]any{"question": "Q?", "options": "not array"},
	}
	if hasValidQuestion(questions) {
		t.Fatal("expected false when options is not an array")
	}
}

func TestHasValidQuestion_NoQuestionKey(t *testing.T) {
	questions := []any{
		map[string]any{"options": []any{"A"}},
	}
	if hasValidQuestion(questions) {
		t.Fatal("expected false when 'question' key is missing")
	}
}

func TestHasValidQuestion_ItemNotMap(t *testing.T) {
	questions := []any{"not a map"}
	if hasValidQuestion(questions) {
		t.Fatal("expected false when item is not a map")
	}
}

func TestHasValidQuestion_EmptySlice(t *testing.T) {
	if hasValidQuestion([]any{}) {
		t.Fatal("expected false for empty slice")
	}
}

// --- parseJSONQuestionItem ---

func TestParseJSONQuestionItem_Valid(t *testing.T) {
	item := map[string]any{
		"question":    "Which?",
		"header":      "Choice",
		"multiSelect": true,
		"options":     []any{map[string]any{"label": "A", "description": "First"}},
	}
	result := parseJSONQuestionItem(item)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["question"] != "Which?" {
		t.Fatalf("expected question='Which?', got %v", result["question"])
	}
	if result["header"] != "Choice" {
		t.Fatalf("expected header='Choice', got %v", result["header"])
	}
	if result["multiSelect"] != true {
		t.Fatalf("expected multiSelect=true, got %v", result["multiSelect"])
	}
	opts, ok := result["options"].([]map[string]any)
	if !ok || len(opts) != 1 || opts[0]["label"] != "A" {
		t.Fatalf("expected options with label A, got %v", result["options"])
	}
}

func TestParseJSONQuestionItem_NotMap(t *testing.T) {
	result := parseJSONQuestionItem("not a map")
	if result != nil {
		t.Fatal("expected nil for non-map input")
	}
}

func TestParseJSONQuestionItem_EmptyQuestion(t *testing.T) {
	item := map[string]any{
		"question": "",
		"options":  []any{map[string]any{"label": "A"}},
	}
	result := parseJSONQuestionItem(item)
	if result != nil {
		t.Fatal("expected nil when question is empty")
	}
}

func TestParseJSONQuestionItem_MissingQuestion(t *testing.T) {
	item := map[string]any{
		"options": []any{map[string]any{"label": "A"}},
	}
	result := parseJSONQuestionItem(item)
	if result != nil {
		t.Fatal("expected nil when question key is missing")
	}
}

func TestParseJSONQuestionItem_NoOptions(t *testing.T) {
	item := map[string]any{
		"question": "Q?",
	}
	result := parseJSONQuestionItem(item)
	if result != nil {
		t.Fatal("expected nil when options key is missing")
	}
}

func TestParseJSONQuestionItem_EmptyOptions(t *testing.T) {
	item := map[string]any{
		"question": "Q?",
		"options":  []any{},
	}
	result := parseJSONQuestionItem(item)
	if result != nil {
		t.Fatal("expected nil when options is empty")
	}
}

func TestParseJSONQuestionItem_OptionsAllInvalid(t *testing.T) {
	item := map[string]any{
		"question": "Q?",
		"options":  []any{map[string]any{"no_label": true}},
	}
	result := parseJSONQuestionItem(item)
	if result != nil {
		t.Fatal("expected nil when all options are invalid (no label)")
	}
}

// --- parseJSONOptions ---

func TestParseJSONOptions_ValidWithDescription(t *testing.T) {
	raw := []any{
		map[string]any{"label": "A", "description": "First"},
		map[string]any{"label": "B"},
	}
	result := parseJSONOptions(raw)
	if len(result) != 2 {
		t.Fatalf("expected 2 options, got %d", len(result))
	}
	if result[0]["label"] != "A" || result[0]["description"] != "First" {
		t.Fatalf("expected first option with label A and description, got %v", result[0])
	}
	if result[1]["label"] != "B" {
		t.Fatalf("expected second option with label B, got %v", result[1])
	}
	if _, hasDesc := result[1]["description"]; hasDesc {
		t.Fatal("expected no description for second option")
	}
}

func TestParseJSONOptions_EmptyLabel(t *testing.T) {
	raw := []any{
		map[string]any{"label": ""},
	}
	result := parseJSONOptions(raw)
	if len(result) != 0 {
		t.Fatalf("expected 0 options for empty label, got %d", len(result))
	}
}

func TestParseJSONOptions_NotMap(t *testing.T) {
	raw := []any{"not a map"}
	result := parseJSONOptions(raw)
	if len(result) != 0 {
		t.Fatalf("expected 0 options for non-map item, got %d", len(result))
	}
}

func TestParseJSONOptions_EmptyDescription(t *testing.T) {
	raw := []any{
		map[string]any{"label": "A", "description": ""},
	}
	result := parseJSONOptions(raw)
	if len(result) != 1 {
		t.Fatalf("expected 1 option, got %d", len(result))
	}
	if _, hasDesc := result[0]["description"]; hasDesc {
		t.Fatal("expected no description key when description is empty string")
	}
}

func TestParseJSONOptions_NilSlice(t *testing.T) {
	result := parseJSONOptions(nil)
	if result != nil {
		t.Fatalf("expected nil for nil input, got %v", result)
	}
}

// --- extractXMLCandidate ---

func TestExtractXMLCandidate_EmptyString(t *testing.T) {
	result := extractXMLCandidate("")
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestExtractXMLCandidate_WhitespaceOnly(t *testing.T) {
	result := extractXMLCandidate("   ")
	if result != "" {
		t.Fatalf("expected empty string for whitespace, got %q", result)
	}
}

func TestExtractXMLCandidate_ValidXML(t *testing.T) {
	raw := `<item><question>Q?</question><option>A</option></item>`
	result := extractXMLCandidate(raw)
	if result != raw {
		t.Fatalf("expected %q, got %q", raw, result)
	}
}

func TestExtractXMLCandidate_ValidJSON(t *testing.T) {
	raw := `{"questions":[{"question":"Q?","options":[{"label":"A"}]}]}`
	result := extractXMLCandidate(raw)
	if result != raw {
		t.Fatalf("expected %q, got %q", raw, result)
	}
}

func TestExtractXMLCandidate_InvalidContent(t *testing.T) {
	result := extractXMLCandidate("just some random text")
	if result != "" {
		t.Fatalf("expected empty string for invalid content, got %q", result)
	}
}
