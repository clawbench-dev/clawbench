package summarize

import (
	"context"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// --- TaskSummarizer short text ---

func TestTaskSummarizer_ShortText(t *testing.T) {
	// Short text should return empty string (no summarization needed)
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		return "should not be called", nil
	}

	pipeline := NewPipelineWithOpts(passFn, taskSummarizePrompt, SummarizeOption{PreserveMarkdown: true})
	s := NewTaskSummarizerFromPipeline(pipeline)

	result, err := s.Summarize(context.Background(), "短文本", "")
	assert.NoError(t, err)
	assert.Equal(t, "", result) // empty = no summarization needed
}

// --- TaskSummarizer via pipeline (API backend) ---

func TestTaskSummarizer_ViaPipeline(t *testing.T) {
	// Create a pipeline with PreserveMarkdown=true and task prompt
	var capturedPrompt string
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		capturedPrompt = systemPrompt
		return "## 保留格式的总结", nil
	}

	pipeline := NewPipelineWithOpts(passFn, taskSummarizePrompt, SummarizeOption{PreserveMarkdown: true})
	s := NewTaskSummarizerFromPipeline(pipeline)

	longText := strings.Repeat("这是一段较长的AI回复内容。", 30)
	result, err := s.Summarize(context.Background(), longText, "")

	assert.NoError(t, err)
	assert.Contains(t, result, "保留格式")
	assert.Contains(t, capturedPrompt, "精简总结")
}

func TestTaskSummarizer_ViaPipeline_ShortText(t *testing.T) {
	passFn := func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
		return "should not be called", nil
	}

	pipeline := NewPipelineWithOpts(passFn, taskSummarizePrompt, SummarizeOption{PreserveMarkdown: true})
	s := NewTaskSummarizerFromPipeline(pipeline)

	result, err := s.Summarize(context.Background(), "短文本", "")
	assert.NoError(t, err)
	// TaskSummarizer returns empty string for short text (meaning "no summarization needed")
	assert.Equal(t, "", result)
}

func TestTaskSummarizer_NoPipeline(t *testing.T) {
	s := &TaskSummarizer{}

	longText := strings.Repeat("这是一段较长的AI回复内容。", 30)
	_, err := s.Summarize(context.Background(), longText, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no pipeline configured")
}

// --- ExtractTextFromBlocks ---

func TestExtractTextFromBlocks_TextOnly(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Hello world"},
		{Type: "text", Text: "Second paragraph"},
	}
	result := ExtractTextFromBlocks(blocks)
	assert.Equal(t, "Hello world\n\nSecond paragraph", result)
}

func TestExtractTextFromBlocks_SkipsNonText(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Important content"},
		{Type: "thinking", Text: "internal reasoning"},
		{Type: "tool_use", Name: "Bash", ID: "1"},
		{Type: "warning", Text: "some warning"},
		{Type: "error", Text: "some error"},
		{Type: "text", Text: "More content"},
	}
	result := ExtractTextFromBlocks(blocks)
	assert.Equal(t, "Important content\n\nMore content", result)
}

func TestExtractTextFromBlocks_Empty(t *testing.T) {
	blocks := []model.ContentBlock{}
	result := ExtractTextFromBlocks(blocks)
	assert.Equal(t, "", result)
}

func TestExtractTextFromBlocks_NoTextBlocks(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "tool_use", Name: "Read", ID: "1"},
		{Type: "thinking", Text: "hmm"},
	}
	result := ExtractTextFromBlocks(blocks)
	assert.Equal(t, "", result)
}

func TestExtractTextFromBlocks_EmptyTextSkipped(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Content"},
		{Type: "text", Text: ""}, // empty text should be skipped
		{Type: "text", Text: "More"},
	}
	result := ExtractTextFromBlocks(blocks)
	assert.Equal(t, "Content\n\nMore", result)
}

// --- ExtractLastAnswerFromBlocks ---

func TestExtractLastAnswerFromBlocks_TextAfterToolUse(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Let me check."},
		{Type: "tool_use", Name: "Bash", ID: "1"},
		{Type: "text", Text: "Here is the answer."},
	}
	result := ExtractLastAnswerFromBlocks(blocks)
	assert.Equal(t, "Here is the answer.", result)
}

func TestExtractLastAnswerFromBlocks_MultipleToolUse(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Searching..."},
		{Type: "tool_use", Name: "Grep", ID: "1"},
		{Type: "text", Text: "Still looking..."},
		{Type: "tool_use", Name: "Read", ID: "2"},
		{Type: "text", Text: "Final answer here."},
	}
	result := ExtractLastAnswerFromBlocks(blocks)
	assert.Equal(t, "Final answer here.", result)
}

func TestExtractLastAnswerFromBlocks_NoToolUse(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Short intro."},
		{Type: "text", Text: "This is a much longer and more substantive answer that contains the actual content."},
	}
	result := ExtractLastAnswerFromBlocks(blocks)
	assert.Equal(t, "This is a much longer and more substantive answer that contains the actual content.", result) // falls back to longest text block
}

func TestExtractLastAnswerFromBlocks_NoTextAfterToolUse(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Let me check."},
		{Type: "tool_use", Name: "Bash", ID: "1"},
	}
	result := ExtractLastAnswerFromBlocks(blocks)
	assert.Equal(t, "Let me check.", result) // falls back to longest (only) text block
}

func TestExtractLastAnswerFromBlocks_OnlyToolUse(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "tool_use", Name: "Bash", ID: "1"},
		{Type: "tool_use", Name: "Read", ID: "2"},
	}
	result := ExtractLastAnswerFromBlocks(blocks)
	assert.Equal(t, "", result)
}

func TestExtractLastAnswerFromBlocks_Empty(t *testing.T) {
	result := ExtractLastAnswerFromBlocks([]model.ContentBlock{})
	assert.Equal(t, "", result)
}

func TestExtractLastAnswerFromBlocks_SkipsThinking(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "tool_use", Name: "Bash", ID: "1"},
		{Type: "thinking", Text: "Let me think..."},
		{Type: "text", Text: "The result is 42."},
	}
	result := ExtractLastAnswerFromBlocks(blocks)
	assert.Equal(t, "The result is 42.", result)
}

// Simulates the message 11578 pattern: intro → many tool_use/text pairs →
// substantive answer → final AskUserQuestion with no trailing text.
func TestExtractLastAnswerFromBlocks_LongAnswerBeforeTerminalToolUse(t *testing.T) {
	longAnswer := "Now I have the complete picture. Here is the full analysis of the terminal reconnection bug. When the user switches away from the terminal view and comes back, the current directory path gets output again on top of what was already there. This is caused by the PTY sending a fresh prompt on reconnect."
	blocks := []model.ContentBlock{
		{Type: "text", Text: "我来查看一下终端重连逻辑，以理解这个问题。"},
		{Type: "tool_use", Name: "Grep", ID: "1"},
		{Type: "tool_use", Name: "Read", ID: "2"},
		{Type: "text", Text: "Now let me check the handler."},
		{Type: "tool_use", Name: "Read", ID: "3"},
		{Type: "text", Text: longAnswer},
		{Type: "tool_use", Name: "AskUserQuestion", ID: "4"},
	}
	result := ExtractLastAnswerFromBlocks(blocks)
	assert.Equal(t, longAnswer, result) // picks the longest text block, not the intro
}
