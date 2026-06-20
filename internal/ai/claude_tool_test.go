package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseClaudeStreamToolEvent(t *testing.T) {
	t.Run("tool_use content_block_start", func(t *testing.T) {
		state := ClaudeStreamToolState{
			ActiveTools: make(map[int]*ToolCall),
		}
		ch := make(chan StreamEvent, 10)

		msg := &ClaudeStreamMessage{
			Type: "stream_event",
			Event: &StreamEventData{
				Type:  "content_block_start",
				Index: 0,
				ContentBlock: &StreamContentBlock{
					Type: "tool_use",
					Name: "Read",
					ID:   "toolu_01",
				},
			},
		}

		parseClaudeStreamToolEvent(msg, &state, ch)
		close(ch)

		var events []StreamEvent
		for ev := range ch {
			events = append(events, ev)
		}
		assert.Len(t, events, 1)
		assert.Equal(t, "tool_use", events[0].Type)
		assert.Equal(t, "Read", events[0].Tool.Name)
		assert.Equal(t, "toolu_01", events[0].Tool.ID)
		assert.False(t, events[0].Tool.Done)
	})

	t.Run("input_json_delta accumulates input", func(t *testing.T) {
		state := ClaudeStreamToolState{
			ActiveTools: map[int]*ToolCall{
				0: {Name: "Edit", ID: "toolu_01", Input: ""},
			},
		}
		ch := make(chan StreamEvent, 10)

		msg := &ClaudeStreamMessage{
			Type: "stream_event",
			Event: &StreamEventData{
				Type:  "content_block_delta",
				Index: 0,
				Delta: &StreamDelta{
					Type:        "input_json_delta",
					PartialJSON: `{"file_path":"/test.go"`,
				},
			},
		}

		parseClaudeStreamToolEvent(msg, &state, ch)
		assert.Equal(t, `{"file_path":"/test.go"`, state.ActiveTools[0].Input)
	})

	t.Run("content_block_stop finalizes tool", func(t *testing.T) {
		state := ClaudeStreamToolState{
			ActiveTools: map[int]*ToolCall{
				0: {Name: "Edit", ID: "toolu_01", Input: `{"file_path":"/test.go"}`},
			},
		}
		ch := make(chan StreamEvent, 10)

		msg := &ClaudeStreamMessage{
			Type: "stream_event",
			Event: &StreamEventData{
				Type:  "content_block_stop",
				Index: 0,
			},
		}

		parseClaudeStreamToolEvent(msg, &state, ch)
		close(ch)

		var events []StreamEvent
		for ev := range ch {
			events = append(events, ev)
		}
		assert.Len(t, events, 1)
		assert.Equal(t, "tool_use", events[0].Type)
		assert.True(t, events[0].Tool.Done)
		assert.Equal(t, "toolu_01", events[0].Tool.ID)
	})

	t.Run("thinking content_block_stop emits thinking_done", func(t *testing.T) {
		state := ClaudeStreamToolState{
			ActiveThinkingBlocks: map[int]bool{1: true},
		}
		ch := make(chan StreamEvent, 10)

		msg := &ClaudeStreamMessage{
			Type: "stream_event",
			Event: &StreamEventData{
				Type:  "content_block_stop",
				Index: 1,
			},
		}

		parseClaudeStreamToolEvent(msg, &state, ch)
		close(ch)

		var events []StreamEvent
		for ev := range ch {
			events = append(events, ev)
		}
		assert.Len(t, events, 1)
		assert.Equal(t, "thinking_done", events[0].Type)
	})

	t.Run("tool_result content_block_start tracks accumulator", func(t *testing.T) {
		state := ClaudeStreamToolState{}
		ch := make(chan StreamEvent, 10)

		msg := &ClaudeStreamMessage{
			Type: "stream_event",
			Event: &StreamEventData{
				Type:  "content_block_start",
				Index: 2,
				ContentBlock: &StreamContentBlock{
					Type:      "tool_result",
					ToolUseID: "toolu_01",
				},
			},
		}

		parseClaudeStreamToolEvent(msg, &state, ch)
		assert.NotNil(t, state.ActiveToolResults)
		assert.Contains(t, state.ActiveToolResults, 2)
		assert.Equal(t, "toolu_01", state.ActiveToolResults[2].ToolUseID)
	})

	t.Run("text_delta in tool_result block is suppressed", func(t *testing.T) {
		state := ClaudeStreamToolState{
			ActiveToolResults: map[int]*toolResultAccum{
				2: {ToolUseID: "toolu_01"},
			},
		}
		ch := make(chan StreamEvent, 10)

		msg := &ClaudeStreamMessage{
			Type: "stream_event",
			Event: &StreamEventData{
				Type:  "content_block_delta",
				Index: 2,
				Delta: &StreamDelta{
					Type: "text_delta",
					Text: "tool output text",
				},
			},
		}

		parseClaudeStreamToolEvent(msg, &state, ch)
		close(ch)

		// No events emitted — text accumulated into toolResultAccum
		var events []StreamEvent
		for ev := range ch {
			events = append(events, ev)
		}
		assert.Empty(t, events)
		assert.Equal(t, "tool output text", state.ActiveToolResults[2].Output.String())
	})
}

func TestParseClaudeAssistantToolUse(t *testing.T) {
	t.Run("emits tool_use and tool_result blocks", func(t *testing.T) {
		msg := &ClaudeStreamMessage{
			Type: "assistant",
			Message: &ClaudeStreamMessageBody{
				Content: []ClaudeContentBlock{
					{Type: "tool_use", Name: "Read", ID: "toolu_01", Input: []byte(`{"file_path":"/test.go"}`)},
					{Type: "tool_result", ID: "toolu_01_r", ToolUseID: "toolu_01", Content: []byte(`"file contents"`)},
					{Type: "text", Text: "hello"},
				},
			},
		}
		state := ClaudeStreamToolState{}

		events := parseClaudeAssistantToolUse(msg, &state)
		assert.Len(t, events, 3)
		assert.Equal(t, "tool_use", events[0].Type)
		assert.Equal(t, "tool_result", events[1].Type)
		assert.Equal(t, "content", events[2].Type)
	})

	t.Run("supplements empty input when receivedPartialToolUse", func(t *testing.T) {
		msg := &ClaudeStreamMessage{
			Type: "assistant",
			Message: &ClaudeStreamMessageBody{
				Content: []ClaudeContentBlock{
					{Type: "tool_use", Name: "Edit", ID: "toolu_02", Input: []byte(`{"file_path":"/test.go","old_string":"foo","new_string":"bar"}`)},
				},
			},
		}
		state := ClaudeStreamToolState{
			ReceivedPartialToolUse: true,
			EmittedToolInputEmpty:  map[string]bool{"toolu_02": true},
		}

		events := parseClaudeAssistantToolUse(msg, &state)
		assert.Len(t, events, 1)
		assert.Equal(t, "tool_use", events[0].Type)
		assert.Contains(t, events[0].Tool.Input, "old_string")
	})

	t.Run("nil message returns nil", func(t *testing.T) {
		msg := &ClaudeStreamMessage{Type: "assistant"}
		state := ClaudeStreamToolState{}
		events := parseClaudeAssistantToolUse(msg, &state)
		assert.Nil(t, events)
	})
}

func TestParseClaudeUserToolResult(t *testing.T) {
	t.Run("extracts tool_result from user message", func(t *testing.T) {
		msg := &ClaudeStreamMessage{
			Type: "user",
			Message: &ClaudeStreamMessageBody{
				Content: []ClaudeContentBlock{
					{Type: "tool_result", ToolUseID: "toolu_01", Content: []byte(`"output text"`)},
					{Type: "text", Text: "ignored"},
				},
			},
		}

		events := parseClaudeUserToolResult(msg)
		assert.Len(t, events, 1)
		assert.Equal(t, "tool_result", events[0].Type)
		assert.Equal(t, "toolu_01", events[0].Tool.ID)
	})

	t.Run("nil message returns nil", func(t *testing.T) {
		msg := &ClaudeStreamMessage{Type: "user"}
		events := parseClaudeUserToolResult(msg)
		assert.Nil(t, events)
	})
}
