package codex

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// This file covers edge cases for codexSplitThinking not covered by
// the tag-pair tests in codex_stream_test.go (TestCodexSplitThinking_*).

func TestCodexSplitThinkingEdge_EmptyInput(t *testing.T) {
	thinking, content := codexSplitThinking("")
	assert.Equal(t, "", thinking)
	assert.Equal(t, "", content)
}

func TestCodexSplitThinkingEdge_WhitespaceOnly(t *testing.T) {
	// Whitespace-only input: no tags, no \n\n separator → entire text is content (untrimmed)
	thinking, content := codexSplitThinking("   \t  ")
	assert.Equal(t, "", thinking)
	assert.Equal(t, "   \t  ", content)
}

func TestCodexSplitThinkingEdge_TagsNotAtStart(t *testing.T) {
	text := "prefix " + codexThinkOpen + "thinking" + codexThinkClose + " content"
	thinking, content := codexSplitThinking(text)
	assert.Equal(t, "thinking", thinking)
	assert.Equal(t, "content", content)
}

func TestCodexSplitThinkingEdge_MultipleDoubleNewlines(t *testing.T) {
	text := "part1\n\npart2\n\npart3"
	thinking, content := codexSplitThinking(text)
	assert.Equal(t, "part1", thinking)
	assert.Equal(t, "part2\n\npart3", content)
}

func TestCodexSplitThinkingEdge_UnicodeContent(t *testing.T) {
	text := "正在思考问题\n\n最终的答案是42"
	thinking, content := codexSplitThinking(text)
	assert.Equal(t, "正在思考问题", thinking)
	assert.Equal(t, "最终的答案是42", content)
}

func TestCodexSplitThinkingEdge_FullTagsWithWhitespace(t *testing.T) {
	text := codexThinkOpen + "  padded thinking  " + codexThinkClose + "  padded content  "
	thinking, content := codexSplitThinking(text)
	assert.Equal(t, "padded thinking", thinking)
	assert.Equal(t, "padded content", content)
}
