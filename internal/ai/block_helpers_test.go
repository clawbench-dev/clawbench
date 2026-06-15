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
