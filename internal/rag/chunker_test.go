package rag

import (
	"encoding/json"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
)

func TestExtractTextFromContent_UserMessage(t *testing.T) {
	got := ExtractTextFromContent("hello world", "user", "")
	assert.Equal(t, "hello world", got)
}

func TestExtractTextFromContent_UserMessage_Trimmed(t *testing.T) {
	got := ExtractTextFromContent("  hello world  ", "user", "")
	assert.Equal(t, "hello world", got)
}

// A user's private note (密送) lives in the message TEXT as a `private` mention
// tag. In a group session it must be stripped before the text is embedded into
// a searchable chunk — otherwise the note becomes retrievable from ANY session,
// which is the exact confidentiality break the feature exists to prevent.
//
// The user branch used to return the content verbatim (the assistant branch was
// the only one that stripped), so this asserts the group gate explicitly.
func TestExtractTextFromContent_UserMessage_GroupStripsPrivateNote(t *testing.T) {
	text := `<clawbench-mention targets="m-b"></clawbench-mention> <clawbench-mention targets="m-b" private>你的词是西瓜</clawbench-mention> 你先说`
	got := ExtractTextFromContent(text, "user", store.SessionTypeGroup)
	assert.NotContains(t, got, "你的词是西瓜", "the private note must never be embedded")
	assert.NotContains(t, got, "clawbench-mention", "the protocol tag must not survive")
	assert.Contains(t, got, "你先说", "the public prose must survive")
}

// A single chat has no protocol, so a user who literally types the tag text must
// keep it verbatim (the same "don't truncate a discussion of the syntax" rule
// the assistant branch follows).
func TestExtractTextFromContent_UserMessage_SingleChatKeepsText(t *testing.T) {
	text := `看这个 <clawbench-mention targets="A" private>字面</clawbench-mention> 语法`
	got := ExtractTextFromContent(text, "user", "")
	assert.Equal(t, text, got)
}

func TestExtractTextFromContent_AssistantMessage_ConclusionOnly(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Let me check the code."},
		{Type: "tool_use", ID: "t1", Name: "Read"},
		{Type: "text", Text: "The fix is to add a null check."},
	}
	content, _ := json.Marshal(map[string]any{"blocks": blocks})

	got := ExtractTextFromContent(string(content), "assistant", "")
	assert.Equal(t, "The fix is to add a null check.", got)
}

func TestExtractTextFromContent_AssistantMessage_NoToolUse(t *testing.T) {
	// No tool_use blocks → falls back to longest text block
	blocks := []model.ContentBlock{
		{Type: "text", Text: "Hi"},
		{Type: "text", Text: "Here is a detailed explanation of the problem and its solution."},
	}
	content, _ := json.Marshal(map[string]any{"blocks": blocks})

	got := ExtractTextFromContent(string(content), "assistant", "")
	assert.Equal(t, "Here is a detailed explanation of the problem and its solution.", got)
}

func TestExtractTextFromContent_AssistantMessage_InvalidJSON(t *testing.T) {
	got := ExtractTextFromContent("plain text fallback", "assistant", "")
	assert.Equal(t, "plain text fallback", got)
}

func TestExtractTextFromContent_AssistantMessage_MarkdownPreserved(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "tool_use", ID: "t1", Name: "Read"},
		{Type: "text", Text: "## Result\n\n- Item 1\n- Item 2\n\n```go\nfmt.Println()\n```"},
	}
	content, _ := json.Marshal(map[string]any{"blocks": blocks})

	got := ExtractTextFromContent(string(content), "assistant", "")
	assert.Contains(t, got, "## Result")
	assert.Contains(t, got, "```go")
}

func TestExtractTextFromContent_AssistantMessage_ThinkingSkipped(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "internal reasoning"},
		{Type: "text", Text: "Visible answer"},
	}
	content, _ := json.Marshal(map[string]any{"blocks": blocks})

	got := ExtractTextFromContent(string(content), "assistant", "")
	assert.Equal(t, "Visible answer", got)
}

// A group timeline's private note must never be embedded into a searchable
// chunk (fail-closed).
func TestExtractTextFromContent_GroupSessionStripsPrivateNote(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "公开 <clawbench-mention targets=\"A\" private>只有A看</clawbench-mention> 结束"},
	}
	content, _ := json.Marshal(map[string]any{"blocks": blocks})

	got := ExtractTextFromContent(string(content), "assistant", store.SessionTypeGroup)
	assert.NotContains(t, got, "只有A看")
	assert.NotContains(t, got, "clawbench-mention")
	assert.Contains(t, got, "公开")
	assert.Contains(t, got, "结束")
}

// A single chat has no group protocol: a reply that merely DISCUSSES the tag
// syntax (an unclosed literal tag in prose) must be indexed intact — the 58879
// regression (the old unconditional strip dropped the whole tail).
func TestExtractTextFromContent_SingleChatKeepsLiteralTag(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "用户的消息里写 `<clawbench-mention private>` 不会被落库，后面的内容必须保留"},
	}
	content, _ := json.Marshal(map[string]any{"blocks": blocks})

	got := ExtractTextFromContent(string(content), "assistant", store.SessionTypeChat)
	assert.Contains(t, got, "后面的内容必须保留")
	assert.Contains(t, got, "clawbench-mention")
}

func TestChunkText_EmptyInput(t *testing.T) {
	assert.Nil(t, ChunkText("", 100, 20))
	assert.Nil(t, ChunkText("hello", 0, 20))
}

func TestChunkText_ShortText(t *testing.T) {
	chunks := ChunkText("hello world", 100, 20)
	assert.Len(t, chunks, 1)
	assert.Equal(t, "hello world", chunks[0].Text)
	assert.Equal(t, 0, chunks[0].Index)
}

func TestChunkText_LongText_MultipleChunks(t *testing.T) {
	// Create text that will need multiple chunks
	text := "First paragraph with enough content to fill a chunk. " +
		"Second paragraph with more content to fill another chunk. " +
		"Third paragraph to ensure we get multiple chunks from the text."
	chunks := ChunkText(text, 10, 2)
	assert.GreaterOrEqual(t, len(chunks), 2)
	// Verify indices are sequential
	for i, c := range chunks {
		assert.Equal(t, i, c.Index, "chunk index mismatch")
	}
}

func TestChunkText_ParagraphBreak(t *testing.T) {
	text := "First paragraph here.\n\nSecond paragraph here."
	chunks := ChunkText(text, 5, 1)
	assert.GreaterOrEqual(t, len(chunks), 1)
}

func TestChunkText_SentenceBreak(t *testing.T) {
	text := "This is sentence one. This is sentence two. This is sentence three."
	chunks := ChunkText(text, 5, 1)
	assert.GreaterOrEqual(t, len(chunks), 1)
}

func TestChunkText_CJK(t *testing.T) {
	text := "这是一段中文文本，用来测试CJK字符的分块功能。这是第二部分，继续测试。"
	chunks := ChunkText(text, 10, 2)
	assert.GreaterOrEqual(t, len(chunks), 1)
}

func TestEstimateTokens(t *testing.T) {
	// Pure English
	enTokens := estimateTokens("hello world")
	assert.Greater(t, enTokens, 0)

	// Pure CJK
	cjkTokens := estimateTokens("你好世界")
	assert.Greater(t, cjkTokens, 0)

	// Mixed
	mixedTokens := estimateTokens("hello 你好 world 世界")
	assert.Greater(t, mixedTokens, 0)
}

func TestEstimateCharsPerToken(t *testing.T) {
	// English text should have higher chars-per-token
	enRatio := estimateCharsPerToken("hello world test")
	assert.Greater(t, enRatio, 0.0)

	// CJK text should have lower chars-per-token
	cjkRatio := estimateCharsPerToken("你好世界测试")
	assert.Greater(t, cjkRatio, 0.0)
	assert.Less(t, cjkRatio, enRatio)

	// Empty-ish text defaults to 4.0
	zeroRatio := estimateCharsPerToken("")
	assert.Equal(t, 4.0, zeroRatio)
}
