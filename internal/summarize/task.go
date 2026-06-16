package summarize

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// blockTypeText is the ContentBlock type identifier for text blocks.
const blockTypeText = "text"

// taskSummarizePrompt is the system prompt for task execution summarization.
// It preserves Markdown formatting and condenses the output to ~30% length.
const taskSummarizePrompt = `你是一个精简总结助手。请对以下 AI 助手的输出进行精简总结，要求：
1. 保留 Markdown 格式（标题、列表、代码块、加粗等）
2. 保留关键代码片段（但删减冗余的重复代码）
3. 保留核心结论和操作结果
4. 删减详细的推理过程、中间步骤、冗长的解释
5. 保留重要的错误信息和警告
6. 目标长度不超过原文的 30%
7. 使用与原文相同的语言输出`

// TaskSummarizePrompt returns the task summarization system prompt.
// Exported for use in initTaskSummarizer.
func TaskSummarizePrompt() string {
	return taskSummarizePrompt
}

// TaskSummarizer generates Markdown-preserving summaries for scheduled task executions.
// Unlike the TTS summarization pipeline (ttsPipeline), it does NOT strip markdown
// from input or output — the summary retains formatting for readability.
type TaskSummarizer struct {
	// When using an AI CLI backend (claude/codebuddy/opencode etc.):
	Backend ai.AIBackend // exported for test construction
	model   string       // model ID override (empty = use backend default)

	// When using an API backend (OpenAI/Anthropic) via pipeline:
	pipeline *ttsPipeline
}

// NewTaskSummarizer creates a TaskSummarizer using the specified AI CLI backend type.
// For API backends (OpenAI/Anthropic), use NewTaskSummarizerFromPipeline instead.
func NewTaskSummarizer(backendType, model string) (*TaskSummarizer, error) {
	backend, err := ai.NewBackend(backendType)
	if err != nil {
		return nil, fmt.Errorf("failed to create AI backend for task summarization: %w", err)
	}
	return &TaskSummarizer{
		Backend: backend,
		model:   model,
	}, nil
}

// NewTaskSummarizerFromPipeline creates a TaskSummarizer that delegates to a
// pre-configured ttsPipeline (with PreserveMarkdown=true and task-specific prompt).
// Used for API backends (OpenAI/Anthropic) where we can't shell out to a CLI.
func NewTaskSummarizerFromPipeline(p ttsPipeline) *TaskSummarizer {
	return &TaskSummarizer{
		pipeline: &p,
	}
}

// Summarize generates a Markdown-preserving summary of the text.
// Short text (< ShortTextThreshold) returns an empty string, indicating no
// summarization is needed — the caller should display the original content.
// The language parameter is currently unused; the prompt instructs the AI
// to match the source language.
func (t *TaskSummarizer) Summarize(ctx context.Context, text string, language string) (string, error) {
	// Short text bypass
	if utf8.RuneCountInString(text) < ShortTextThreshold {
		return "", nil
	}

	// If we have a pipeline (API backend), delegate to it
	if t.pipeline != nil {
		return t.pipeline.Summarize(ctx, text, language)
	}

	// Truncate long input (preserve raw markdown, not stripped)
	inputText := text
	runes := []rune(inputText)
	if len(runes) > MaxSummarizeRunes {
		inputText = string(runes[len(runes)-MaxSummarizeRunes:])
	}

	req := ai.ChatRequest{
		Prompt:       inputText,
		SessionID:    "",
		WorkDir:      "",
		SystemPrompt: taskSummarizePrompt,
		Model:        t.model,
		Command:      "",
		AgentID:      "",
		Resume:       false,
	}

	ch, err := t.Backend.ExecuteStream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("task summarization backend failed to start: %w", err)
	}

	var buf strings.Builder
	for event := range ch {
		switch event.Type {
		case "content":
			buf.WriteString(event.Content)
		case "done":
			// completed
		case "error":
			return "", fmt.Errorf("task summarization backend error: %s", event.Error)
		}
	}

	result := strings.TrimSpace(buf.String())
	if result == "" {
		return "", fmt.Errorf("task summarization returned empty output")
	}

	slog.Info(
		"task summarization completed",
		slog.Int("result_len", len([]rune(result))),
	)

	return result, nil
}

// ExtractTextFromBlocks extracts plain text from ContentBlock array.
// Only text-type blocks are included; tool_use, thinking, etc. are skipped.
// Text blocks are joined with double newlines.
func ExtractTextFromBlocks(blocks []model.ContentBlock) string {
	var buf strings.Builder
	for _, b := range blocks {
		if b.Type == blockTypeText && b.Text != "" {
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
			buf.WriteString(b.Text)
		}
	}
	return buf.String()
}

// ExtractLastAnswerFromBlocks extracts text from blocks after the last tool_use block.
// This captures the AI's final answer rather than intermediate reasoning or tool-call commentary.
// If no tool_use blocks exist, falls back to the first non-empty text block.
// Returns empty string if no suitable text is found.
func ExtractLastAnswerFromBlocks(blocks []model.ContentBlock) string {
	lastToolIdx := -1
	for i, b := range blocks {
		if b.Type == "tool_use" {
			lastToolIdx = i
		}
	}
	// Find first text block after the last tool_use (only if tool_use exists)
	if lastToolIdx >= 0 {
		for i := lastToolIdx + 1; i < len(blocks); i++ {
			if blocks[i].Type == "text" && blocks[i].Text != "" {
				return blocks[i].Text
			}
		}
	}
	// No text after last tool_use — fall back to the longest text block.
	// This handles the case where the AI gives a substantive answer followed
	// by a terminal tool_use (e.g. AskUserQuestion) with no trailing text.
	// Falling back to the first text block would typically return the intro
	// sentence ("Let me check...") rather than the actual answer.
	var bestText string
	for _, b := range blocks {
		if b.Type == blockTypeText && b.Text != "" && utf8.RuneCountInString(b.Text) > utf8.RuneCountInString(bestText) {
			bestText = b.Text
		}
	}
	return bestText
}
