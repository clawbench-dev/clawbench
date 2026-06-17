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

// --- extractXMLCandidate ---

func TestExtractXMLCandidate_EmptyString(t *testing.T) {
	if extractXMLCandidate("") != "" {
		t.Fatal("expected empty string for empty input")
	}
	if extractXMLCandidate("   ") != "" {
		t.Fatal("expected empty string for whitespace input")
	}
}

func TestExtractXMLCandidate_XMLWithItemAndQuestionAndOption(t *testing.T) {
	input := `<item><question>Q?</question><option>A</option></item>`
	result := extractXMLCandidate(input)
	if result == "" {
		t.Fatal("expected non-empty result for valid XML with item/question/option")
	}
}

func TestExtractXMLCandidate_XMLWithItemSpace(t *testing.T) {
	input := `<item attr="1"><question>Q?</question><option>A</option></item>`
	result := extractXMLCandidate(input)
	if result == "" {
		t.Fatal("expected non-empty result for XML with <item > tag")
	}
}

func TestExtractXMLCandidate_XMLMissingQuestion(t *testing.T) {
	input := `<item><option>A</option></item>`
	if extractXMLCandidate(input) != "" {
		t.Fatal("expected empty result when <question> is missing")
	}
}

func TestExtractXMLCandidate_XMLMissingOption(t *testing.T) {
	input := `<item><question>Q?</question></item>`
	if extractXMLCandidate(input) != "" {
		t.Fatal("expected empty result when <option> is missing")
	}
}

func TestExtractXMLCandidate_ValidJSON(t *testing.T) {
	input := `{"questions":[{"question":"Q?","options":[{"label":"A"}]}]}`
	result := extractXMLCandidate(input)
	if result == "" {
		t.Fatal("expected non-empty result for valid JSON with questions array")
	}
}

func TestExtractXMLCandidate_InvalidJSON(t *testing.T) {
	if extractXMLCandidate(`{not json}`) != "" {
		t.Fatal("expected empty result for invalid JSON")
	}
}

func TestExtractXMLCandidate_JSONMissingQuestions(t *testing.T) {
	if extractXMLCandidate(`{"other":"value"}`) != "" {
		t.Fatal("expected empty result for JSON without questions")
	}
}

func TestExtractXMLCandidate_JSONEmptyQuestions(t *testing.T) {
	if extractXMLCandidate(`{"questions":[]}`) != "" {
		t.Fatal("expected empty result for empty questions array")
	}
}

func TestExtractXMLCandidate_JSONQuestionsNotArray(t *testing.T) {
	if extractXMLCandidate(`{"questions":"not array"}`) != "" {
		t.Fatal("expected empty result when questions is not an array")
	}
}

func TestExtractXMLCandidate_JSONQuestionItemNotMap(t *testing.T) {
	if extractXMLCandidate(`{"questions":["not a map"]}`) != "" {
		t.Fatal("expected empty result when question item is not a map")
	}
}

func TestExtractXMLCandidate_JSONQuestionWithoutOptions(t *testing.T) {
	if extractXMLCandidate(`{"questions":[{"question":"Q?"}]}`) != "" {
		t.Fatal("expected empty result when question has no options")
	}
}

func TestExtractXMLCandidate_JSONQuestionWithEmptyOptions(t *testing.T) {
	if extractXMLCandidate(`{"questions":[{"question":"Q?","options":[]}]}`) != "" {
		t.Fatal("expected empty result when question has empty options")
	}
}

func TestExtractXMLCandidate_JSONQuestionWithOptions(t *testing.T) {
	input := `{"questions":[{"question":"Q?","options":[{"label":"A"}]}]}`
	if extractXMLCandidate(input) == "" {
		t.Fatal("expected non-empty result for JSON with question and options")
	}
}

func TestExtractXMLCandidate_NonXMLNonJSON(t *testing.T) {
	if extractXMLCandidate("plain text content") != "" {
		t.Fatal("expected empty result for plain text")
	}
}

// --- parseAskQuestionXML ---

func TestParseAskQuestionXML_NoItems(t *testing.T) {
	if parseAskQuestionXML("no items here") != nil {
		t.Fatal("expected nil for content with no <item> tags")
	}
}

func TestParseAskQuestionXML_ItemMissingQuestion(t *testing.T) {
	input := `<item><header>H</header><option><label>A</label></option></item>`
	if parseAskQuestionXML(input) != nil {
		t.Fatal("expected nil when item has no <question>")
	}
}

func TestParseAskQuestionXML_ItemMissingOptions(t *testing.T) {
	input := `<item><question>Q?</question></item>`
	if parseAskQuestionXML(input) != nil {
		t.Fatal("expected nil when item has no <option>")
	}
}

func TestParseAskQuestionXML_OptionMissingLabel(t *testing.T) {
	input := `<item><question>Q?</question><option><description>D</description></option></item>`
	if parseAskQuestionXML(input) != nil {
		t.Fatal("expected nil when option has no <label>")
	}
}

func TestParseAskQuestionXML_MultiSelectTrue(t *testing.T) {
	input := `<item><header>H</header><multi-select>true</multi-select><question>Q?</question><option><label>A</label></option></item>`
	result := parseAskQuestionXML(input)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	questions := result["questions"].([]map[string]any)
	if !questions[0]["multiSelect"].(bool) {
		t.Fatal("expected multiSelect=true")
	}
}

func TestParseAskQuestionXML_OptionWithDescription(t *testing.T) {
	input := `<item><question>Q?</question><option><label>A</label><description>Desc</description></option></item>`
	result := parseAskQuestionXML(input)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	questions := result["questions"].([]map[string]any)
	opts := questions[0]["options"].([]map[string]any)
	if opts[0]["description"] != "Desc" {
		t.Fatalf("expected description 'Desc', got %v", opts[0]["description"])
	}
}

// --- parseAskQuestionJSON ---

func TestParseAskQuestionJSON_InvalidJSON(t *testing.T) {
	if parseAskQuestionJSON("{not json}") != nil {
		t.Fatal("expected nil for invalid JSON")
	}
}

func TestParseAskQuestionJSON_MissingQuestions(t *testing.T) {
	if parseAskQuestionJSON(`{"other":"value"}`) != nil {
		t.Fatal("expected nil for JSON without questions")
	}
}

func TestParseAskQuestionJSON_EmptyQuestions(t *testing.T) {
	if parseAskQuestionJSON(`{"questions":[]}`) != nil {
		t.Fatal("expected nil for empty questions array")
	}
}

func TestParseAskQuestionJSON_QuestionNotMap(t *testing.T) {
	if parseAskQuestionJSON(`{"questions":["string"]}`) != nil {
		t.Fatal("expected nil when question item is not a map")
	}
}

func TestParseAskQuestionJSON_QuestionMissingQuestion(t *testing.T) {
	if parseAskQuestionJSON(`{"questions":[{"header":"H"}]}`) != nil {
		t.Fatal("expected nil when question field is empty")
	}
}

func TestParseAskQuestionJSON_QuestionMissingOptions(t *testing.T) {
	if parseAskQuestionJSON(`{"questions":[{"question":"Q?"}]}`) != nil {
		t.Fatal("expected nil when question has no options")
	}
}

func TestParseAskQuestionJSON_OptionNotMap(t *testing.T) {
	if parseAskQuestionJSON(`{"questions":[{"question":"Q?","options":["string"]}]}`) != nil {
		t.Fatal("expected nil when option is not a map")
	}
}

func TestParseAskQuestionJSON_OptionMissingLabel(t *testing.T) {
	if parseAskQuestionJSON(`{"questions":[{"question":"Q?","options":[{"description":"D"}]}]}`) != nil {
		t.Fatal("expected nil when option has no label")
	}
}

func TestParseAskQuestionJSON_EmptyOptions(t *testing.T) {
	if parseAskQuestionJSON(`{"questions":[{"question":"Q?","options":[]}]}`) != nil {
		t.Fatal("expected nil when options array is empty")
	}
}

func TestParseAskQuestionJSON_OptionWithDescription(t *testing.T) {
	input := `{"questions":[{"question":"Q?","multiSelect":true,"options":[{"label":"A","description":"Desc"}]}]}`
	result := parseAskQuestionJSON(input)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	questions := result["questions"].([]map[string]any)
	if !questions[0]["multiSelect"].(bool) {
		t.Fatal("expected multiSelect=true")
	}
	opts := questions[0]["options"].([]map[string]any)
	if opts[0]["description"] != "Desc" {
		t.Fatalf("expected description 'Desc', got %v", opts[0]["description"])
	}
}

// --- ConvertAskQuestionBlocks additional cases ---

func TestConvertAskQuestionBlocks_WrongCloseTag(t *testing.T) {
	// Non-standard closing tag variant
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<ask-question><item><header>H</header><multi-select>false</multi-select><question>Q?</question><option><label>A</label></option></item></user_query>`},
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
		t.Fatalf("expected AskUserQuestion tool_use block with wrong close tag, got: %+v", result)
	}
}

func TestConvertAskQuestionBlocks_UnclosedTag(t *testing.T) {
	// No closing tag at all — tag runs to end-of-text
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<ask-question><item><question>Q?</question><option><label>A</label></option></item>`},
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
		t.Fatalf("expected AskUserQuestion tool_use block with unclosed tag, got: %+v", result)
	}
}

func TestConvertAskQuestionBlocks_UnparseableContent(t *testing.T) {
	// <ask-question> tag present but content is neither valid XML nor JSON
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<ask-question>garbage content</ask-question>`},
	}
	result := ConvertAskQuestionBlocks(blocks)
	// Should remain as text block since parsing fails
	if len(result) != 1 || result[0].Type != "text" {
		t.Fatalf("expected unchanged text block for unparseable content, got: %+v", result)
	}
}

func TestConvertAskQuestionBlocks_NonTextBlock(t *testing.T) {
	// Non-text blocks should be left untouched
	blocks := []model.ContentBlock{
		{Type: "tool_use", Name: "Read"},
	}
	result := ConvertAskQuestionBlocks(blocks)
	if len(result) != 1 || result[0].Type != "tool_use" {
		t.Fatalf("expected unchanged tool_use block, got: %+v", result)
	}
}

func TestConvertAskQuestionBlocks_OnlyAskQuestionTagNoValidContent(t *testing.T) {
	// Has <ask-question> tag but extractXMLCandidate returns empty
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<ask-question>just some text without proper structure</ask-question>`},
	}
	result := ConvertAskQuestionBlocks(blocks)
	if len(result) != 1 || result[0].Type != "text" {
		t.Fatalf("expected unchanged text block, got: %+v", result)
	}
}

func TestConvertAskQuestionBlocks_RemoveRejectedToolBlocksCalled(t *testing.T) {
	// When ask-question converts to tool_use and there's also a rejected tool_use,
	// the rejected one should be removed by RemoveRejectedToolBlocks
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<ask-question><item><question>Q?</question><option><label>A</label></option></item></ask-question>`},
		{Type: "tool_use", Name: "BadTool", ID: "x", Status: "error", Output: "not found in agent cli"},
	}
	result := ConvertAskQuestionBlocks(blocks)
	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "BadTool" {
			t.Fatal("expected rejected tool block to be removed")
		}
	}
}

func TestConvertAskQuestionBlocks_EmptyCleanText(t *testing.T) {
	// When cleanText is empty, the block should be replaced entirely (not appended)
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<ask-question><item><question>Q?</question><option><label>A</label></option></item></ask-question>`},
	}
	result := ConvertAskQuestionBlocks(blocks)
	toolCount := 0
	textCount := 0
	for _, b := range result {
		if b.Type == "tool_use" {
			toolCount++
		}
		if b.Type == "text" {
			textCount++
		}
	}
	if toolCount != 1 {
		t.Fatalf("expected 1 tool_use block, got %d", toolCount)
	}
	// No empty text block should remain
	if textCount != 0 {
		t.Fatalf("expected 0 text blocks (replaced entirely), got %d", textCount)
	}
}

func TestSendStreamEvent_ChannelFullWithToolEvent(t *testing.T) {
	// Test that toolID is extracted from Tool field when logging dropped events
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "content"} // fill buffer

	result := SendStreamEvent(context.Background(), ch, StreamEvent{Type: "tool_use", Tool: &ToolCall{ID: "tool-123"}})
	if !result {
		t.Fatal("expected true when channel is full (event dropped)")
	}
}

func TestSendStreamEvent_ChannelFullNoTool(t *testing.T) {
	// Test drop path with no Tool field
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "content"} // fill buffer

	result := SendStreamEvent(context.Background(), ch, StreamEvent{Type: "thinking", Content: "hmm"})
	if !result {
		t.Fatal("expected true when channel is full (event dropped)")
	}
}
