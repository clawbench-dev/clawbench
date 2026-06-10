package ai

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

func TestAccumulateBlock_Content(t *testing.T) {
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "content", Content: "Hello"})
	AccumulateBlock(&blocks, StreamEvent{Type: "content", Content: " world"})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "Hello world", blocks[0].Text)
}

func TestAccumulateBlock_Thinking(t *testing.T) {
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking", Content: "Think"})
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking", Content: " more"})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "Think more", blocks[0].Text)
}

func TestAccumulateBlock_ToolUseStart(t *testing.T) {
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Read", ID: "t1", Input: `{"file_path":"/a.go"}`, Done: false},
	})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "tool_use", blocks[0].Type)
	assert.Equal(t, "Read", blocks[0].Name)
	assert.Equal(t, "t1", blocks[0].ID)
	assert.False(t, blocks[0].Done)
	assert.Equal(t, "/a.go", blocks[0].Input["file_path"])
}

func TestAccumulateBlock_ToolUseDone(t *testing.T) {
	blocks := []model.ContentBlock{}
	// Start event
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Read", ID: "t1", Input: `{"file_path":"/a.go"}`, Done: false},
	})
	// Done event (same ID, updates existing block)
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Read", ID: "t1", Input: `{"file_path":"/a.go"}`, Done: true},
	})
	assert.Len(t, blocks, 1)
	assert.True(t, blocks[0].Done)
}

func TestAccumulateBlock_ToolUseWithOutput(t *testing.T) {
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t2", Input: `{"command":"ls"}`, Done: true, Output: "file1.go\nfile2.go", Status: "success"},
	})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "file1.go\nfile2.go", blocks[0].Output)
	assert.Equal(t, "success", blocks[0].Status)
}

func TestAccumulateBlock_ToolUseOutputUpdate(t *testing.T) {
	blocks := []model.ContentBlock{}
	// Start without output
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t2", Input: `{"command":"ls"}`, Done: false},
	})
	// Done event adds output/status
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t2", Done: true, Output: "output text", Status: "success"},
	})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "output text", blocks[0].Output)
	assert.Equal(t, "success", blocks[0].Status)
}

func TestAccumulateBlock_ToolResultUpdatesExisting(t *testing.T) {
	blocks := []model.ContentBlock{}
	// First: tool_use without output
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Read", ID: "t3", Input: `{"file_path":"/a.go"}`, Done: true},
	})
	assert.Equal(t, "", blocks[0].Output)
	assert.Equal(t, "", blocks[0].Status)

	// Then: tool_result event fills in output/status
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_result",
		Tool: &ToolCall{ID: "t3", Output: "file contents here", Status: "success"},
	})
	assert.Len(t, blocks, 1) // No new block added
	assert.Equal(t, "file contents here", blocks[0].Output)
	assert.Equal(t, "success", blocks[0].Status)
}

func TestAccumulateBlock_ToolResultError(t *testing.T) {
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t4", Input: `{"command":"bad-cmd"}`, Done: true},
	})
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_result",
		Tool: &ToolCall{ID: "t4", Output: "command not found", Status: "error"},
	})
	assert.Equal(t, "command not found", blocks[0].Output)
	assert.Equal(t, "error", blocks[0].Status)
}

func TestAccumulateBlock_ToolResultNoMatch(t *testing.T) {
	blocks := []model.ContentBlock{}
	// tool_result for an ID that doesn't match any existing block — silently ignored
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_result",
		Tool: &ToolCall{ID: "nonexistent", Output: "output", Status: "success"},
	})
	assert.Len(t, blocks, 0)
}

func TestAccumulateBlock_ToolResultNilTool(t *testing.T) {
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "tool_result", Tool: nil})
	assert.Len(t, blocks, 0)
}

func TestAccumulateBlock_ToolUseNilTool(t *testing.T) {
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "tool_use", Tool: nil})
	assert.Len(t, blocks, 0)
}

func TestAccumulateBlock_Warning(t *testing.T) {
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "warning", Content: "slow response", Reason: "timeout"})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "warning", blocks[0].Type)
	assert.Equal(t, "slow response", blocks[0].Text)
	assert.Equal(t, "timeout", blocks[0].Reason)
}

func TestAccumulateBlock_ContentAfterToolUse(t *testing.T) {
	// text after tool_use should NOT merge with text before tool_use
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "content", Content: "before"})
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Read", ID: "t5", Input: `{}`, Done: true},
	})
	AccumulateBlock(&blocks, StreamEvent{Type: "content", Content: "after"})
	assert.Len(t, blocks, 3)
	assert.Equal(t, "before", blocks[0].Text)
	assert.Equal(t, "tool_use", blocks[1].Type)
	assert.Equal(t, "after", blocks[2].Text)
}

func TestAccumulateBlock_MultipleToolResults(t *testing.T) {
	// Multiple tool_use blocks + tool_result events that match by ID
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "tool_use", Tool: &ToolCall{Name: "Read", ID: "t1", Input: `{}`, Done: true}})
	AccumulateBlock(&blocks, StreamEvent{Type: "tool_use", Tool: &ToolCall{Name: "Bash", ID: "t2", Input: `{}`, Done: true}})

	// tool_result for t2 arrives (out of order)
	AccumulateBlock(&blocks, StreamEvent{Type: "tool_result", Tool: &ToolCall{ID: "t2", Output: "bash output", Status: "success"}})
	// tool_result for t1
	AccumulateBlock(&blocks, StreamEvent{Type: "tool_result", Tool: &ToolCall{ID: "t1", Output: "read output", Status: "success"}})

	assert.Len(t, blocks, 2)
	assert.Equal(t, "read output", blocks[0].Output)
	assert.Equal(t, "success", blocks[0].Status)
	assert.Equal(t, "bash output", blocks[1].Output)
	assert.Equal(t, "success", blocks[1].Status)
}

func TestAccumulateBlock_ToolResultOverwritesEmptyOutput(t *testing.T) {
	// tool_use with empty output → tool_result fills it in
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Grep", ID: "t6", Input: `{"pattern":"TODO"}`, Done: true, Output: "", Status: ""},
	})
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_result",
		Tool: &ToolCall{ID: "t6", Output: "main.go:42: TODO fix this", Status: "success"},
	})
	assert.Equal(t, "main.go:42: TODO fix this", blocks[0].Output)
	assert.Equal(t, "success", blocks[0].Status)
}

func TestAccumulateBlock_ErrorEvent(t *testing.T) {
	// "error" event type creates a warning ContentBlock with Error and Reason
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "error", Error: "connection lost", Reason: "disconnect"})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "warning", blocks[0].Type, "error event should produce a warning block")
	assert.Equal(t, "connection lost", blocks[0].Text)
	assert.Equal(t, "disconnect", blocks[0].Reason)
}

func TestAccumulateBlock_ToolUseMalformedJSON(t *testing.T) {
	// Malformed JSON input should result in an empty map, not a crash
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t7", Input: `{invalid json`, Done: true},
	})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "tool_use", blocks[0].Type)
	assert.NotNil(t, blocks[0].Input, "input should be non-nil even with malformed JSON")
	assert.Empty(t, blocks[0].Input, "malformed JSON should produce empty input map")
}

func TestAccumulateBlock_ACPToolCallFlow(t *testing.T) {
	// Simulate the ACP tool_call flow observed in message 8858:
	// 1. tool_call: rawInput={}, title="task", status=pending → tool_use with empty input
	// 2. tool_call_update: rawInput={"description":"..."}, status="" → tool_use with input
	// 3. tool_call_update: rawInput=None, status="" → tool_use with empty input (no overwrite)
	// 4. tool_call_update: rawInput=None, status=completed → tool_result with output
	blocks := []model.ContentBlock{}

	// Step 1: Initial tool_call with empty input
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Agent", ID: "call_1", Input: `{}`, Done: false},
	})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "Agent", blocks[0].Name)
	assert.Empty(t, blocks[0].Input, "initial input should be empty")
	assert.False(t, blocks[0].Done)

	// Step 2: tool_call_update with description/prompt (status="" or in_progress)
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Agent", ID: "call_1", Input: `{"description":"Explore project structure","prompt":"Explore the codebase"}`, Done: false},
	})
	assert.Len(t, blocks, 1, "should still be 1 block after update")
	assert.Equal(t, "Explore project structure", blocks[0].Input["description"], "input should be updated from tool_call_update")
	assert.Equal(t, "Explore the codebase", blocks[0].Input["prompt"], "prompt should be in input")

	// Step 3: tool_call_update with empty input (RawInput=None) — should NOT overwrite
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{ID: "call_1", Done: false},
	})
	assert.Len(t, blocks, 1, "should still be 1 block after empty update")
	assert.Equal(t, "Explore project structure", blocks[0].Input["description"], "input should persist after empty update")

	// Step 4: tool_result (completed) — should update output but keep input
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_result",
		Tool: &ToolCall{ID: "call_1", Output: "result text", Status: "success"},
	})
	assert.Len(t, blocks, 1, "should still be 1 block after tool_result")
	assert.Equal(t, "Explore project structure", blocks[0].Input["description"], "input should persist after tool_result")
	assert.Equal(t, "result text", blocks[0].Output)
	assert.Equal(t, "success", blocks[0].Status)
	assert.True(t, blocks[0].Done)
}

func TestAccumulateBlock_ToolResultPreservesInput(t *testing.T) {
	// When tool_result event carries input, AccumulateBlock should merge it
	// into the existing block (especially when the existing block has empty input)
	blocks := []model.ContentBlock{}

	// Step 1: tool_use with empty input (initial ACP tool_call)
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t1", Input: `{}`, Done: false},
	})
	assert.Empty(t, blocks[0].Input)

	// Step 2: tool_result with output AND input
	// This happens when ACP tool_call_update status=completed carries rawInput
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_result",
		Tool: &ToolCall{ID: "t1", Input: `{"command":"ls -la"}`, Output: "file1.go\nfile2.go", Status: "success"},
	})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "ls -la", blocks[0].Input["command"], "tool_result should merge input when existing is empty")
	assert.Equal(t, "file1.go\nfile2.go", blocks[0].Output)
	assert.Equal(t, "success", blocks[0].Status)
	assert.True(t, blocks[0].Done)
}

func TestAccumulateBlock_ThinkingAndContentInterleaved(t *testing.T) {
	// Thinking and content without tool_use boundaries should coalesce correctly
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking", Content: "think1"})
	AccumulateBlock(&blocks, StreamEvent{Type: "content", Content: "text1"})
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking", Content: "think2"})
	AccumulateBlock(&blocks, StreamEvent{Type: "content", Content: "text2"})

	// Without tool_use boundaries, same-type blocks coalesce:
	// thinking: "think1" then coalesce "think2" into first thinking block
	// content: "text1" then coalesce "text2" into first content block
	// But they interleave, so: thinking block, content block, and
	// the second thinking should coalesce into the first thinking block,
	// and second content into first content block.
	assert.Len(t, blocks, 2, "should have thinking and content blocks")
	assert.Equal(t, "thinking", blocks[0].Type)
	assert.Equal(t, "think1think2", blocks[0].Text, "thinking deltas should coalesce across content blocks")
	assert.Equal(t, "text", blocks[1].Type)
	assert.Equal(t, "text1text2", blocks[1].Text, "content deltas should coalesce across thinking blocks")
}

func TestAccumulateBlock_ThinkingDone(t *testing.T) {
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking", Content: "hmm"})
	assert.False(t, blocks[0].Done, "thinking block should not be done initially")

	AccumulateBlock(&blocks, StreamEvent{Type: "thinking_done"})
	assert.True(t, blocks[0].Done, "thinking_done should mark the block as done")
	assert.Equal(t, "hmm", blocks[0].Text, "thinking_done should not alter text")
}

func TestAccumulateBlock_ThinkingDoneMarksLastOnly(t *testing.T) {
	// If multiple thinking blocks exist (separated by tool_use), thinking_done
	// only marks the last one
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking", Content: "first"})
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Read", ID: "t1", Input: `{}`, Done: true},
	})
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking", Content: "second"})

	AccumulateBlock(&blocks, StreamEvent{Type: "thinking_done"})
	assert.False(t, blocks[0].Done, "first thinking block should NOT be marked done")
	assert.True(t, blocks[2].Done, "last thinking block should be marked done")
}

func TestAccumulateBlock_ThinkingDoneNoThinkingBlock(t *testing.T) {
	// thinking_done with no thinking blocks should be a no-op
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking_done"})
	assert.Len(t, blocks, 0)
}

func TestAccumulateBlock_ToolCallUpdateMergeInput(t *testing.T) {
	// When tool_call_update carries partial input, existing fields are preserved
	// and new fields are added/overwritten
	blocks := []model.ContentBlock{}

	// Initial tool_call with command
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t1", Input: `{"command":"ls"}`, Done: false},
	})
	assert.Equal(t, "ls", blocks[0].Input["command"])

	// Update with description only — command should be preserved
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t1", Input: `{"description":"list files"}`, Done: false},
	})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "ls", blocks[0].Input["command"], "existing command field should be preserved on merge")
	assert.Equal(t, "list files", blocks[0].Input["description"], "new description field should be added")
}

func TestAccumulateBlock_ToolCallUpdateOverwritesField(t *testing.T) {
	// When tool_call_update provides a field that already exists, the update wins
	blocks := []model.ContentBlock{}

	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Edit", ID: "t1", Input: `{"file_path":"/old.go","old_string":"foo"}`, Done: false},
	})

	// Update with same key — new value overwrites
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Edit", ID: "t1", Input: `{"old_string":"bar","new_string":"baz"}`, Done: false},
	})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "/old.go", blocks[0].Input["file_path"], "unrelated field should be preserved")
	assert.Equal(t, "bar", blocks[0].Input["old_string"], "old_string should be overwritten by update")
	assert.Equal(t, "baz", blocks[0].Input["new_string"], "new_string should be added")
}

func TestAccumulateBlock_ToolCallUpdateNameUpdate(t *testing.T) {
	// ACP agents may send name in tool_call_update events
	blocks := []model.ContentBlock{}

	// Initial call without name
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{ID: "t1", Input: `{"command":"ls"}`, Done: false},
	})
	assert.Equal(t, "", blocks[0].Name, "initial name should be empty")

	// Update provides name
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t1", Done: false},
	})
	assert.Equal(t, "Bash", blocks[0].Name, "name should be updated from tool_call_update")
}

func TestAccumulateBlock_ToolCallUpdateEmptyNamePreservesExisting(t *testing.T) {
	// tool_call_update with empty name should not clear existing name
	blocks := []model.ContentBlock{}

	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t1", Input: `{}`, Done: false},
	})

	// Update without name (Name="")
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{ID: "t1", Done: true},
	})
	assert.Equal(t, "Bash", blocks[0].Name, "name should be preserved when update has empty name")
}

func TestAccumulateBlock_ToolResultNameUpdate(t *testing.T) {
	// tool_result events can also update name
	blocks := []model.ContentBlock{}

	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{ID: "t1", Input: `{}`, Done: false},
	})

	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_result",
		Tool: &ToolCall{Name: "Read", ID: "t1", Output: "contents", Status: "success"},
	})
	assert.Equal(t, "Read", blocks[0].Name, "name should be updated from tool_result")
}

func TestAccumulateBlock_ToolResultEmptyNamePreservesExisting(t *testing.T) {
	// tool_result with empty name should not clear existing name
	blocks := []model.ContentBlock{}

	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t1", Input: `{}`, Done: false},
	})

	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_result",
		Tool: &ToolCall{ID: "t1", Output: "output", Status: "success"},
	})
	assert.Equal(t, "Bash", blocks[0].Name, "name should be preserved when tool_result has empty name")
}

func TestAccumulateBlock_ToolResultReplacesInput(t *testing.T) {
	// tool_result with input replaces (does not merge) the existing input
	blocks := []model.ContentBlock{}

	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t1", Input: `{"command":"ls"}`, Done: false},
	})

	// tool_result carries different input — replaces entirely
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_result",
		Tool: &ToolCall{ID: "t1", Input: `{"description":"list files"}`, Output: "output", Status: "success"},
	})
	assert.Equal(t, "list files", blocks[0].Input["description"], "input should be replaced by tool_result input")
	assert.Nil(t, blocks[0].Input["command"], "old command key should be gone after tool_result replace")
}

func TestAccumulateBlock_ThinkingAfterToolUseCreatesNewBlock(t *testing.T) {
	// Thinking after tool_use should NOT merge with thinking before tool_use
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking", Content: "before"})
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Read", ID: "t1", Input: `{}`, Done: true},
	})
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking", Content: "after"})

	assert.Len(t, blocks, 3)
	assert.Equal(t, "thinking", blocks[0].Type)
	assert.Equal(t, "before", blocks[0].Text)
	assert.Equal(t, "tool_use", blocks[1].Type)
	assert.Equal(t, "thinking", blocks[2].Type)
	assert.Equal(t, "after", blocks[2].Text)
}

func TestAccumulateBlock_TextAndThinkingWithToolUseBoundary(t *testing.T) {
	// Complex interleaving: text + thinking + tool_use + text + thinking
	// Text after tool_use creates new block; thinking after tool_use creates new block
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "content", Content: "text1"})
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking", Content: "think1"})
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t1", Input: `{}`, Done: true},
	})
	AccumulateBlock(&blocks, StreamEvent{Type: "content", Content: "text2"})
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking", Content: "think2"})

	// Expected: text1, think1, tool_use, text2, think2
	assert.Len(t, blocks, 5)
	assert.Equal(t, "text1", blocks[0].Text)
	assert.Equal(t, "think1", blocks[1].Text)
	assert.Equal(t, "tool_use", blocks[2].Type)
	assert.Equal(t, "text2", blocks[3].Text)
	assert.Equal(t, "think2", blocks[4].Text)
}

func TestAccumulateBlock_ToolUseEmptyInputString(t *testing.T) {
	// tool_use with empty Input string should still create a valid input map
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t1", Input: "", Done: false},
	})
	assert.Len(t, blocks, 1)
	assert.NotNil(t, blocks[0].Input, "input should be non-nil even with empty input string")
	assert.Empty(t, blocks[0].Input, "empty input string should produce empty input map")
}

func TestAccumulateBlock_MultipleToolUseDifferentIDs(t *testing.T) {
	// Multiple tool_use events with different IDs create separate blocks
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Read", ID: "t1", Input: `{"file_path":"/a.go"}`, Done: true},
	})
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Edit", ID: "t2", Input: `{"file_path":"/a.go","old_string":"x"}`, Done: true},
	})
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t3", Input: `{"command":"go test"}`, Done: true},
	})

	assert.Len(t, blocks, 3)
	assert.Equal(t, "t1", blocks[0].ID)
	assert.Equal(t, "t2", blocks[1].ID)
	assert.Equal(t, "t3", blocks[2].ID)
}

func TestAccumulateBlock_ToolUseStatusUpdate(t *testing.T) {
	// Status is updated via tool_call_update
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t1", Input: `{}`, Done: false, Status: "pending"},
	})
	assert.Equal(t, "pending", blocks[0].Status)

	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{ID: "t1", Done: false, Status: "in_progress"},
	})
	assert.Equal(t, "in_progress", blocks[0].Status)
}

func TestAccumulateBlock_ToolUseOutputUpdatePreservesExisting(t *testing.T) {
	// tool_use update with empty output should not clear existing output
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{Name: "Bash", ID: "t1", Input: `{}`, Done: false, Output: "partial output"},
	})

	// Update without output (Output="")
	AccumulateBlock(&blocks, StreamEvent{
		Type: "tool_use",
		Tool: &ToolCall{ID: "t1", Done: true},
	})
	assert.Equal(t, "partial output", blocks[0].Output, "existing output should be preserved when update has empty output")
}

func TestAccumulateBlock_ContentOnEmptyBlocks(t *testing.T) {
	// Content event on empty blocks list creates a new text block
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "content", Content: "first"})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "text", blocks[0].Type)
	assert.Equal(t, "first", blocks[0].Text)
}

func TestAccumulateBlock_ThinkingOnEmptyBlocks(t *testing.T) {
	// Thinking event on empty blocks list creates a new thinking block
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "thinking", Content: "hmm"})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "thinking", blocks[0].Type)
	assert.Equal(t, "hmm", blocks[0].Text)
}

func TestAccumulateBlock_UnknownEventType(t *testing.T) {
	// Unknown event type should be a no-op
	blocks := []model.ContentBlock{}
	AccumulateBlock(&blocks, StreamEvent{Type: "metadata"})
	AccumulateBlock(&blocks, StreamEvent{Type: "done"})
	AccumulateBlock(&blocks, StreamEvent{Type: "unknown_type"})
	assert.Len(t, blocks, 0)
}

func TestMergeConsecutiveThinkingBlocks_Empty(t *testing.T) {
	assert.Nil(t, MergeConsecutiveThinkingBlocks(nil))
	assert.Empty(t, MergeConsecutiveThinkingBlocks([]model.ContentBlock{}))
}

func TestMergeConsecutiveThinkingBlocks_SingleBlock(t *testing.T) {
	blocks := []model.ContentBlock{{Type: "thinking", Text: "hello"}}
	result := MergeConsecutiveThinkingBlocks(blocks)
	assert.Len(t, result, 1)
	assert.Equal(t, "hello", result[0].Text)
}

func TestMergeConsecutiveThinkingBlocks_AdjacentThinking(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "part1"},
		{Type: "thinking", Text: "part2"},
		{Type: "thinking", Text: "part3"},
	}
	result := MergeConsecutiveThinkingBlocks(blocks)
	assert.Len(t, result, 1)
	assert.Equal(t, "part1part2part3", result[0].Text)
}

func TestMergeConsecutiveThinkingBlocks_AcrossToolUse(t *testing.T) {
	// Thinking blocks separated by tool_use are NOT merged — tool_use is a boundary
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "before"},
		{Type: "tool_use", ID: "t1"},
		{Type: "thinking", Text: "after"},
	}
	result := MergeConsecutiveThinkingBlocks(blocks)
	assert.Len(t, result, 3)
	assert.Equal(t, "thinking", result[0].Type)
	assert.Equal(t, "before", result[0].Text)
	assert.Equal(t, "tool_use", result[1].Type)
	assert.Equal(t, "thinking", result[2].Type)
	assert.Equal(t, "after", result[2].Text)
}

func TestMergeConsecutiveThinkingBlocks_PreservesNonThinking(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "thinking", Text: "think1"},
		{Type: "thinking", Text: "think2"},
		{Type: "text", Text: "world"},
	}
	result := MergeConsecutiveThinkingBlocks(blocks)
	assert.Len(t, result, 3)
	assert.Equal(t, "text", result[0].Type)
	assert.Equal(t, "think1think2", result[1].Text)
	assert.Equal(t, "text", result[2].Type)
}

func TestMergeConsecutiveThinkingBlocks_DonePropagation(t *testing.T) {
	// If any merged thinking block is done, the combined block is done
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "part1", Done: false},
		{Type: "thinking", Text: "part2", Done: true},
	}
	result := MergeConsecutiveThinkingBlocks(blocks)
	assert.Len(t, result, 1)
	assert.True(t, result[0].Done, "merged block should be done if any source block is done")
}

func TestMergeConsecutiveThinkingBlocks_SkipsEmptyThinking(t *testing.T) {
	// Empty thinking blocks are not included in result
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: ""},
		{Type: "text", Text: "content"},
	}
	result := MergeConsecutiveThinkingBlocks(blocks)
	assert.Len(t, result, 1)
	assert.Equal(t, "text", result[0].Type)
}

func TestMergeConsecutiveThinkingBlocks_ComplexSequence(t *testing.T) {
	// tool_use is a boundary — thinking blocks on opposite sides are NOT merged
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "think-a"},
		{Type: "tool_use", ID: "t1"},
		{Type: "thinking", Text: "think-b"},
		{Type: "text", Text: "response"},
		{Type: "thinking", Text: "think-c", Done: true},
		{Type: "thinking", Text: "think-d"},
	}
	result := MergeConsecutiveThinkingBlocks(blocks)
	assert.Len(t, result, 5)
	assert.Equal(t, "think-a", result[0].Text, "thinking before tool_use stays separate")
	assert.Equal(t, "tool_use", result[1].Type)
	assert.Equal(t, "think-b", result[2].Text, "thinking after tool_use stays separate")
	assert.Equal(t, "response", result[3].Text)
	assert.Equal(t, "think-cthink-d", result[4].Text, "consecutive thinking blocks at end are merged")
	assert.True(t, result[4].Done, "done flag from think-c should propagate")
}

func TestMergeConsecutiveThinkingBlocks_TwoBlocks(t *testing.T) {
	// Exactly 2 blocks — not skipped by the len<=1 fast path
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "a"},
		{Type: "thinking", Text: "b"},
	}
	result := MergeConsecutiveThinkingBlocks(blocks)
	assert.Len(t, result, 1)
	assert.Equal(t, "ab", result[0].Text)
}
