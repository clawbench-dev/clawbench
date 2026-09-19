package ai

import (
	"strings"
	"testing"

	"clawbench/internal/model"
)

// --- StringsContainsAnyBlock ---

func TestStringsContainsAnyBlock_Found(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "hmm"},
		{Type: "text", Text: "before <clawbench-ask-question> after"},
	}
	if !StringsContainsAnyBlock(blocks, "<clawbench-ask-question") {
		t.Fatal("expected to find <clawbench-ask-question in text block")
	}
}

func TestStringsContainsAnyBlock_NotFound(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "plain text"},
	}
	if StringsContainsAnyBlock(blocks, "<clawbench-ask-question") {
		t.Fatal("expected not to find <clawbench-ask-question")
	}
}

func TestStringsContainsAnyBlock_Empty(t *testing.T) {
	if StringsContainsAnyBlock(nil, "<clawbench-ask-question") {
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
		{Type: "text", Text: `<clawbench-ask-question>
**Choice**
Which one?
- A — First
</clawbench-ask-question>`},
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

func TestConvertAskQuestionBlocks_JSONPayloadDegrades(t *testing.T) {
	// JSON is not the documented format and is not recovered: the payload is
	// shown as text so the malformed output is visible rather than masked.
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<clawbench-ask-question>
{"questions":[{"question":"Pick one","options":[{"label":"A"}]}]}
</clawbench-ask-question>`},
	}
	result := ConvertAskQuestionBlocks(blocks)

	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			t.Fatalf("JSON must not become a card, got %+v", b)
		}
	}
	if len(result) != 1 || !strings.Contains(result[0].Text, "Pick one") {
		t.Fatalf("the payload text must be retained, got %+v", result)
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
		{Type: "text", Text: "before <clawbench-ask-question>\n**H**\nQ?\n- A — D\n</clawbench-ask-question> after"},
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

// --- ConvertAskQuestionBlocks additional cases ---

// 27% of production text blocks contain more than one <clawbench-ask-question> tag. The
// previous implementation converted only the last one and leaked the rest as
// raw XML.
func TestConvertAskQuestionBlocks_MultipleTagsMergeIntoOneBlock(t *testing.T) {
	one := `<clawbench-ask-question>
**Q1**
第一个?
- A
</clawbench-ask-question>`
	two := `<clawbench-ask-question>
**Q2**
第二个?
- B
</clawbench-ask-question>`
	blocks := []model.ContentBlock{
		{Type: "text", Text: one + "\n中间\n" + two},
	}

	result := ConvertAskQuestionBlocks(blocks)

	toolCount := 0
	var questions []map[string]any
	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			toolCount++
			qs, _ := b.Input["questions"].([]map[string]any)
			questions = qs
		}
		if b.Type == "text" && strings.Contains(b.Text, "<clawbench-ask-question") {
			t.Errorf("no raw tag may remain in the text block, got %q", b.Text)
		}
	}
	if toolCount != 1 {
		t.Fatalf("expected exactly 1 merged tool block, got %d", toolCount)
	}
	if len(questions) != 2 {
		t.Fatalf("expected both tags' questions merged, got %d", len(questions))
	}
	if questions[0]["header"] != "Q1" || questions[1]["header"] != "Q2" {
		t.Errorf("unexpected question order: %+v", questions)
	}
}

// An unparseable payload must stay in the text block: its raw text is the only
// remaining copy of the question.
func TestConvertAskQuestionBlocks_UnparseableTagIsRetained(t *testing.T) {
	payload := `<clawbench-ask-question>
这里没有列表，只是一段说明。
</clawbench-ask-question>`
	blocks := []model.ContentBlock{
		{Type: "text", Text: "前言\n" + payload + "\n后记"},
	}

	result := ConvertAskQuestionBlocks(blocks)

	if len(result) != 1 || result[0].Type != "text" {
		t.Fatalf("expected a single unchanged text block, got %+v", result)
	}
	if !strings.Contains(result[0].Text, "这里没有列表") {
		t.Fatalf("the unparseable payload text must be retained, got %q", result[0].Text)
	}
}

// The over-strip regression: an unclosed tag followed by a <details> block used
// to consume the details block (the next closing token) and delete real prose.
// An unclosed tag has no payload, so it degrades to text.
func TestConvertAskQuestionBlocks_UnclosedTagDoesNotSwallowFollowingBlock(t *testing.T) {
	text := "分析如下\n<clawbench-ask-question>\n**H**\nQ?\n- A\n\n" +
		"<details>\n<summary>更多</summary>\n正文内容必须保留\n</details>"
	blocks := []model.ContentBlock{{Type: "text", Text: text}}

	result := ConvertAskQuestionBlocks(blocks)

	foundTool := false
	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			foundTool = true
		}
		if b.Type == "text" && !strings.Contains(b.Text, "正文内容必须保留") {
			t.Fatalf("the details body was swallowed, got %q", b.Text)
		}
	}
	if foundTool {
		t.Fatal("an unclosed tag has no payload and must not convert")
	}
}

// An option without a <label> (12% of production payloads use attributes) must
// now parse rather than being silently rejected.
func TestConvertAskQuestionBlocks_OptionAttributeIsUnderstood(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<clawbench-ask-question>
**Pick**
Which?
- restore_only
</clawbench-ask-question>`},
	}

	result := ConvertAskQuestionBlocks(blocks)

	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			qs, _ := b.Input["questions"].([]map[string]any)
			if len(qs) != 1 {
				t.Fatalf("expected 1 question, got %+v", qs)
			}
			opts, _ := qs[0]["options"].([]map[string]any)
			if len(opts) != 1 || opts[0]["label"] != "restore_only" {
				t.Fatalf("expected the attribute label to be used, got %+v", opts)
			}
			return
		}
	}
	t.Fatal("expected an AskUserQuestion block")
}

func TestConvertAskQuestionBlocks_WrongCloseTag(t *testing.T) {
	// Non-standard closing tag variant
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<clawbench-ask-question>
**H**
Q?
- A
</clawbench-ask-question>`},
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
		{Type: "text", Text: `<clawbench-ask-question>
Q?
- A
</clawbench-ask-question>`},
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
	// Tag present but the payload is not a question
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<clawbench-ask-question>
garbage content
</clawbench-ask-question>`},
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
	// Has the tag but no parseable payload
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<clawbench-ask-question>
just some text without proper structure
</clawbench-ask-question>`},
	}
	result := ConvertAskQuestionBlocks(blocks)
	if len(result) != 1 || result[0].Type != "text" {
		t.Fatalf("expected unchanged text block, got: %+v", result)
	}
}

func TestConvertAskQuestionBlocks_RejectedToolNotRemoved(t *testing.T) {
	// ConvertAskQuestionBlocks only handles clawbench-ask-question conversion.
	// RemoveRejectedToolBlocks is called separately by postProcessBlocks,
	// so rejected tool blocks are preserved here.
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<clawbench-ask-question>
Q?
- A
</clawbench-ask-question>`},
		{Type: "tool_use", Name: "BadTool", ID: "x", Status: "error", Output: "not found in agent cli"},
	}
	result := ConvertAskQuestionBlocks(blocks)
	// The rejected tool block should still be present — removal is
	// the caller's responsibility (postProcessBlocks).
	found := false
	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "BadTool" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected rejected tool block to be preserved (removal is postProcessBlocks' job)")
	}
}

func TestConvertAskQuestionBlocks_EmptyCleanText(t *testing.T) {
	// When cleanText is empty, the block should be replaced entirely (not appended)
	blocks := []model.ContentBlock{
		{Type: "text", Text: `<clawbench-ask-question>
Q?
- A
</clawbench-ask-question>`},
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

// TestConvertAskQuestionBlocks_DefensiveCopy verifies that ConvertAskQuestionBlocks
// does not mutate the caller's slice elements. This is a regression test: the
// function used to modify blocks[i].Text in-place, which shared the underlying
// array with the caller (e.g. SessionExecutor.e.blocks). When buildResult called
// postProcessBlocks first, it stripped <clawbench-ask-question> tags from e.blocks via this
// mutation, then Finalize's postProcessBlocks couldn't detect the tags anymore —
// causing the AskUserQuestion tool_use block to be lost from chat_history.content.
func TestConvertAskQuestionBlocks_DefensiveCopy(t *testing.T) {
	originalText := "Before <clawbench-ask-question>\n**H**\nQ?\n- A — D\n</clawbench-ask-question> After"
	blocks := []model.ContentBlock{
		{Type: "text", Text: originalText},
	}

	// Call ConvertAskQuestionBlocks and verify it returns 2 blocks
	result := ConvertAskQuestionBlocks(blocks)
	if len(result) != 2 {
		t.Fatalf("expected 2 blocks (text + tool_use), got %d", len(result))
	}
	if result[0].Type != "text" {
		t.Fatalf("expected first block to be text, got %s", result[0].Type)
	}
	if result[1].Type != "tool_use" {
		t.Fatalf("expected second block to be tool_use, got %s", result[1].Type)
	}

	// Critical check: original blocks slice must NOT be mutated
	if blocks[0].Text != originalText {
		t.Fatalf("original blocks[0].Text was mutated!\ngot:  %q\nwant: %q", blocks[0].Text, originalText)
	}

	// Call ConvertAskQuestionBlocks again on the same original slice —
	// it should still detect <clawbench-ask-question> tags
	result2 := ConvertAskQuestionBlocks(blocks)
	if len(result2) != 2 {
		t.Fatalf("second call: expected 2 blocks, got %d — tags were stripped by first call", len(result2))
	}
	if result2[1].Type != "tool_use" {
		t.Fatalf("second call: expected tool_use block, got %s", result2[1].Type)
	}
}

// --- ExtractToolCallMeta ---

// Under the native-Markdown contract an unparseable payload degrades to
// readable text: the wrapper is stripped so no raw tag reaches the user, and
// the payload's own text survives.
func TestConvertAskQuestionBlocks_UnparseableDegradesToMarkdown(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "前言\n<clawbench-ask-question>\n这里没有列表，只是一段说明。\n</clawbench-ask-question>\n后记"},
	}

	result := ConvertAskQuestionBlocks(blocks)

	if len(result) != 1 || result[0].Type != "text" {
		t.Fatalf("expected a single text block, got %+v", result)
	}
	if strings.Contains(result[0].Text, "<clawbench-ask-question") {
		t.Errorf("the wrapper must be stripped, got %q", result[0].Text)
	}
	if !strings.Contains(result[0].Text, "这里没有列表") {
		t.Errorf("the payload text must survive, got %q", result[0].Text)
	}
}

// The current format is native Markdown inside the tag.
func TestConvertAskQuestionBlocks_MarkdownFormat(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "前言\n<clawbench-ask-question>\n**方案选择**\n你更倾向哪种？\n- 方案 A — 快\n- 方案 B — 安全\n</clawbench-ask-question>\n后记"},
	}

	result := ConvertAskQuestionBlocks(blocks)

	var got map[string]any
	for _, b := range result {
		if b.Type == "tool_use" && b.Name == "AskUserQuestion" {
			got = b.Input
		}
		if b.Type == "text" && strings.Contains(b.Text, "<clawbench-ask-question") {
			t.Errorf("no raw tag may remain, got %q", b.Text)
		}
	}
	if got == nil {
		t.Fatalf("expected an AskUserQuestion block, got %+v", result)
	}
	qs, _ := got["questions"].([]map[string]any)
	if len(qs) != 1 || qs[0]["header"] != "方案选择" {
		t.Fatalf("unexpected questions: %+v", qs)
	}
	opts, _ := qs[0]["options"].([]map[string]any)
	if len(opts) != 2 || opts[0]["label"] != "方案 A" || opts[0]["description"] != "快" {
		t.Fatalf("unexpected options: %+v", opts)
	}
}
