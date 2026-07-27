package rag

import (
	"encoding/json"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

func TestExtractTextFromContent_UserMessage(t *testing.T) {
	got := ExtractTextFromContent("hello world", "user")
	assert.Equal(t, "hello world", got)
}

func TestExtractTextFromContent_UserMessage_Trimmed(t *testing.T) {
	got := ExtractTextFromContent("  hello world  ", "user")
	assert.Equal(t, "hello world", got)
}

func TestExtractTextFromContent_AssistantMessage_ConclusionOnly(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Let me check the code."},
		{Type: "tool_use", ID: "t1", Name: "Read"},
		{Type: "text", Text: "The fix is to add a null check."},
	}
	content, _ := json.Marshal(map[string]any{"blocks": blocks})

	got := ExtractTextFromContent(string(content), "assistant")
	assert.Equal(t, "The fix is to add a null check.", got)
}

func TestExtractTextFromContent_AssistantMessage_NoToolUse(t *testing.T) {
	// No tool_use blocks → falls back to longest text block
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Hi"},
		{Type: "text", Text: "Here is a detailed explanation of the problem and its solution."},
	}
	content, _ := json.Marshal(map[string]any{"blocks": blocks})

	got := ExtractTextFromContent(string(content), "assistant")
	assert.Equal(t, "Here is a detailed explanation of the problem and its solution.", got)
}

func TestExtractTextFromContent_AssistantMessage_InvalidJSON(t *testing.T) {
	got := ExtractTextFromContent("plain text fallback", "assistant")
	assert.Equal(t, "plain text fallback", got)
}

func TestExtractTextFromContent_AssistantMessage_MarkdownPreserved(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "tool_use", ID: "t1", Name: "Read"},
		{Type: "text", Text: "## Result\n\n- Item 1\n- Item 2\n\n```go\nfmt.Println()\n```"},
	}
	content, _ := json.Marshal(map[string]any{"blocks": blocks})

	got := ExtractTextFromContent(string(content), "assistant")
	assert.Contains(t, got, "## Result")
	assert.Contains(t, got, "```go")
}

func TestExtractTextFromContent_AssistantMessage_ThinkingSkipped(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "internal reasoning"},
		{Type: "text", Text: "Visible answer"},
	}
	content, _ := json.Marshal(map[string]any{"blocks": blocks})

	got := ExtractTextFromContent(string(content), "assistant")
	assert.Equal(t, "Visible answer", got)
}
