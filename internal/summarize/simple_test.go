package summarize

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewSimple(t *testing.T) {
	s := NewSimple()
	assert.NotNil(t, s)
}

func TestSimpleSummarizer_ShortText(t *testing.T) {
	s := NewSimple()
	text := "Hello, this is a short text."

	result, err := s.Summarize(context.Background(), text, "zh")
	assert.NoError(t, err)
	assert.Equal(t, text, result)
}

func TestSimpleSummarizer_LongText_Truncation(t *testing.T) {
	s := NewSimple()

	longText := strings.Repeat("a", 1500)
	result, err := s.Summarize(context.Background(), longText, "zh")
	assert.NoError(t, err)

	assert.LessOrEqual(t, len([]rune(result)), SimpleMaxSummarizeRunes)
	assert.Equal(t, strings.Repeat("a", 1000), result)
}

func TestSimpleSummarizer_BoundaryExactly1000(t *testing.T) {
	s := NewSimple()

	text := strings.Repeat("x", 1000)
	result, err := s.Summarize(context.Background(), text, "zh")
	assert.NoError(t, err)
	assert.Equal(t, text, result)
}

func TestSimpleSummarizer_MarkdownStripped(t *testing.T) {
	s := NewSimple()

	text := "**bold** and *italic* and `code`"
	result, err := s.Summarize(context.Background(), text, "zh")
	assert.NoError(t, err)
	assert.NotContains(t, result, "**")
	assert.NotContains(t, result, "*")
	assert.NotContains(t, result, "`")
}

func TestSimpleSummarizer_LanguageIgnored(t *testing.T) {
	s := NewSimple()

	text := "same text regardless of language"
	resultZh, _ := s.Summarize(context.Background(), text, "zh")
	resultEn, _ := s.Summarize(context.Background(), text, "en")

	assert.Equal(t, resultZh, resultEn)
}

func TestSimpleSummarizer_EmptyText(t *testing.T) {
	s := NewSimple()

	result, err := s.Summarize(context.Background(), "", "zh")
	assert.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestNewSimplePreserveMarkdown(t *testing.T) {
	s := NewSimplePreserveMarkdown()
	assert.NotNil(t, s)
	assert.True(t, s.preserveMarkdown)
}

func TestSimpleSummarizer_PreserveMarkdown_KeepsCodeBlocks(t *testing.T) {
	s := NewSimplePreserveMarkdown()

	text := "```\n╔══════════════╗\n║  Deploy v0.60 ║\n╚══════════════╝\n```"
	result, err := s.Summarize(context.Background(), text, "zh")
	assert.NoError(t, err)
	// Code block content must be preserved (not stripped by StripMarkdown)
	assert.Contains(t, result, "╔══")
	assert.Contains(t, result, "Deploy v0.60")
	assert.Contains(t, result, "╚══")
}

func TestSimpleSummarizer_PreserveMarkdown_KeepsFormatting(t *testing.T) {
	s := NewSimplePreserveMarkdown()

	text := "**bold** and *italic* and `code`"
	result, err := s.Summarize(context.Background(), text, "zh")
	assert.NoError(t, err)
	// Markdown formatting must be preserved
	assert.Contains(t, result, "**bold**")
	assert.Contains(t, result, "*italic*")
	assert.Contains(t, result, "`code`")
}

func TestSimpleSummarizer_Default_StripsCodeBlocks(t *testing.T) {
	s := NewSimple()

	text := "```\n╔══════════════╗\n║  Deploy v0.60 ║\n╚══════════════╝\n```"
	result, err := s.Summarize(context.Background(), text, "zh")
	assert.NoError(t, err)
	// TTS mode: code block content must be stripped
	assert.NotContains(t, result, "╔══")
	assert.NotContains(t, result, "Deploy v0.60")
}

func TestSimpleSummarizer_PreserveMarkdown_Truncation(t *testing.T) {
	s := NewSimplePreserveMarkdown()

	longText := strings.Repeat("a", 1500)
	result, err := s.Summarize(context.Background(), longText, "zh")
	assert.NoError(t, err)
	assert.LessOrEqual(t, len([]rune(result)), SimpleMaxSummarizeRunes)
}
