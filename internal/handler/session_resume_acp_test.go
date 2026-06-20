package handler

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// ACP LoadSession replay parsing tests
// ---------------------------------------------------------------------------
//
// These tests verify that ACP SessionUpdate notifications are correctly
// parsed into structured content blocks via mapACPSessionUpdate + AccumulateBlock,
// producing {"blocks":[{"type":"text","text":"Hello!"}]} format that the frontend
// can render, instead of raw ACP JSON like {"agent_message_chunk":{...}}.

// parsedMessage represents a single message extracted from ACP replay parsing.
type parsedMessage struct {
	role    string
	content string
}

// parseNotifications simulates the replay parsing logic in ServeACPLoadSession:
// iterate over SessionNotifications, split on role boundaries, accumulate blocks.
func parseNotifications(buf []acp.SessionNotification) []parsedMessage {
	var messages []parsedMessage
	var blocks []model.ContentBlock
	var currentRole string

	flushBlocks := func() {
		if len(blocks) == 0 || currentRole == "" {
			return
		}
		blocks = ai.MergeConsecutiveThinkingBlocks(blocks)
		contentMap := map[string]any{"blocks": blocks}
		if currentRole == "assistant" {
			contentMap["metadata"] = map[string]any{"transport": "acp-stdio"}
		}
		contentJSON, _ := json.Marshal(contentMap)
		messages = append(messages, parsedMessage{
			role:    currentRole,
			content: string(contentJSON),
		})
		blocks = nil
	}

	for _, n := range buf {
		notifRole := "assistant"
		if n.Update.UserMessageChunk != nil {
			notifRole = "user"
		}
		if notifRole != currentRole && currentRole != "" {
			flushBlocks()
		}
		currentRole = notifRole

		// UserMessageChunk is not handled by mapACPSessionUpdate —
		// extract text directly from the ACP notification.
		if n.Update.UserMessageChunk != nil {
			if text := n.Update.UserMessageChunk.Content.Text; text != nil && text.Text != "" {
				ai.AccumulateBlock(&blocks, ai.StreamEvent{Type: "content", Content: text.Text})
			}
			continue
		}

		ch := make(chan ai.StreamEvent, 64)
		ai.MapACPSessionUpdateForTest(n.Update, ch)
		close(ch)
		for event := range ch {
			switch event.Type {
			case "content", "thinking", "thinking_done", "tool_use", "tool_result", "warning", "error":
				ai.AccumulateBlock(&blocks, event)
			}
		}
	}
	flushBlocks()
	return messages
}

// TestLoadSessionParsing_AgentMessageChunk_ProducesTextBlock
// verifies that AgentMessageChunk produces a proper text block with the
// actual text content, not raw ACP JSON.
func TestLoadSessionParsing_AgentMessageChunk_ProducesTextBlock(t *testing.T) {
	buf := []acp.SessionNotification{
		{
			SessionId: "test-session-1",
			Update: acp.SessionUpdate{
				AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{Text: "Hello, world!"},
					},
				},
			},
		},
	}

	messages := parseNotifications(buf)
	require.Len(t, messages, 1, "should produce exactly one message")
	assert.Equal(t, "assistant", messages[0].role)

	// Content should be valid JSON with blocks array
	var parsed map[string]any
	err := json.Unmarshal([]byte(messages[0].content), &parsed)
	require.NoError(t, err)

	blocks, ok := parsed["blocks"].([]any)
	require.True(t, ok, "should have blocks array")
	require.Len(t, blocks, 1, "should have one block")

	block, ok := blocks[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "text", block["type"], "block type should be text")
	assert.Equal(t, "Hello, world!", block["text"], "text content should be extracted, not raw JSON")

	// Verify assistant messages include metadata with transport
	metadata, ok := parsed["metadata"].(map[string]any)
	require.True(t, ok, "assistant messages should include metadata")
	assert.Equal(t, "acp-stdio", metadata["transport"])
}

// TestLoadSessionParsing_ToolCall_ProducesToolUseBlock
// verifies that ToolCall notifications produce proper tool_use blocks
// with canonical tool names and normalized input.
func TestLoadSessionParsing_ToolCall_ProducesToolUseBlock(t *testing.T) {
	buf := []acp.SessionNotification{
		{
			SessionId: "test-session-2",
			Update: acp.SessionUpdate{
				ToolCall: &acp.SessionUpdateToolCall{
					ToolCallId: acp.ToolCallId("tc-read-1"),
					Title:      "Read file contents",
					Kind:       acp.ToolKindRead,
					RawInput:   map[string]any{"file_path": "/tmp/test.go"},
				},
			},
		},
	}

	messages := parseNotifications(buf)
	require.Len(t, messages, 1)

	var parsed map[string]any
	err := json.Unmarshal([]byte(messages[0].content), &parsed)
	require.NoError(t, err)

	blocks, ok := parsed["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, blocks, 1)

	block, ok := blocks[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tool_use", block["type"])
	assert.Equal(t, "Read", block["name"], "tool name should be canonical 'Read'")
	assert.Equal(t, "tc-read-1", block["id"])
}

// TestLoadSessionParsing_ToolCallUpdate_ProducesToolResultBlock
// verifies that completed ToolCallUpdate notifications update the tool_use block
// with output text.
func TestLoadSessionParsing_ToolCallUpdate_ProducesToolResultBlock(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	buf := []acp.SessionNotification{
		{
			SessionId: "test-session-3",
			Update: acp.SessionUpdate{
				ToolCall: &acp.SessionUpdateToolCall{
					ToolCallId: acp.ToolCallId("tc-read-1"),
					Title:      "Read file",
					Kind:       acp.ToolKindRead,
					RawInput:   map[string]any{"file_path": "/tmp/test.go"},
				},
			},
		},
		{
			SessionId: "test-session-3",
			Update: acp.SessionUpdate{
				ToolCallUpdate: &acp.SessionToolCallUpdate{
					ToolCallId: acp.ToolCallId("tc-read-1"),
					Status:     &completed,
					RawOutput:  "file contents here",
				},
			},
		},
	}

	messages := parseNotifications(buf)
	require.Len(t, messages, 1)

	var parsed map[string]any
	err := json.Unmarshal([]byte(messages[0].content), &parsed)
	require.NoError(t, err)

	blocks, ok := parsed["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, blocks, 1, "tool_use + tool_result should merge into one block")

	block, ok := blocks[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tool_use", block["type"])
	assert.Equal(t, true, block["done"], "tool should be marked done after completion")
	// Slim serialization: output is not included in serialized blocks
	// (stored in chat_tool_calls table instead)
	assert.Nil(t, block["output"], "tool output should NOT be in slim serialization")
}

// TestLoadSessionParsing_ThinkingChunk_ProducesThinkingBlock
// verifies that AgentThoughtChunk notifications produce thinking blocks
// with the extracted text content.
func TestLoadSessionParsing_ThinkingChunk_ProducesThinkingBlock(t *testing.T) {
	buf := []acp.SessionNotification{
		{
			SessionId: "test-session-4",
			Update: acp.SessionUpdate{
				AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{Text: "Let me think about this..."},
					},
				},
			},
		},
	}

	messages := parseNotifications(buf)
	require.Len(t, messages, 1)

	var parsed map[string]any
	err := json.Unmarshal([]byte(messages[0].content), &parsed)
	require.NoError(t, err)

	blocks, ok := parsed["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, blocks, 1)

	block, ok := blocks[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "thinking", block["type"])
	assert.Equal(t, "Let me think about this...", block["text"])
}

// TestLoadSessionParsing_UserMessageChunk_SetsUserRole
// verifies that UserMessageChunk notifications produce a user role message.
func TestLoadSessionParsing_UserMessageChunk_SetsUserRole(t *testing.T) {
	buf := []acp.SessionNotification{
		{
			SessionId: "test-session-5",
			Update: acp.SessionUpdate{
				UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{Text: "User says hello"},
					},
				},
			},
		},
	}

	messages := parseNotifications(buf)
	require.Len(t, messages, 1)
	assert.Equal(t, "user", messages[0].role)

	var parsed map[string]any
	err := json.Unmarshal([]byte(messages[0].content), &parsed)
	require.NoError(t, err)

	blocks, ok := parsed["blocks"].([]any)
	require.True(t, ok)
	require.Len(t, blocks, 1)

	block, ok := blocks[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "text", block["type"])
	assert.Equal(t, "User says hello", block["text"])

	// User messages should NOT have metadata
	_, hasMetadata := parsed["metadata"]
	assert.False(t, hasMetadata, "user messages should not include metadata")
}

// TestLoadSessionParsing_NonUserMessage_DefaultsToAssistant
// verifies that non-UserMessageChunk notifications default to "assistant" role.
func TestLoadSessionParsing_NonUserMessage_DefaultsToAssistant(t *testing.T) {
	tests := []struct {
		name string
		n    acp.SessionNotification
	}{
		{
			name: "AgentMessageChunk",
			n: acp.SessionNotification{
				SessionId: "test",
				Update: acp.SessionUpdate{
					AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
						Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "hi"}},
					},
				},
			},
		},
		{
			name: "ToolCall",
			n: acp.SessionNotification{
				SessionId: "test",
				Update: acp.SessionUpdate{
					ToolCall: &acp.SessionUpdateToolCall{
						ToolCallId: "tc-1",
						Title:      "Bash",
						Kind:       acp.ToolKindExecute,
					},
				},
			},
		},
		{
			name: "ToolCallUpdate",
			n: acp.SessionNotification{
				SessionId: "test",
				Update: acp.SessionUpdate{
					ToolCallUpdate: &acp.SessionToolCallUpdate{
						ToolCallId: "tc-1",
					},
				},
			},
		},
		{
			name: "AgentThoughtChunk",
			n: acp.SessionNotification{
				SessionId: "test",
				Update: acp.SessionUpdate{
					AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
						Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "thinking"}},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := parseNotifications([]acp.SessionNotification{tt.n})
			require.Len(t, messages, 1)
			assert.Equal(t, "assistant", messages[0].role,
				"non-UserMessageChunk notifications should default to 'assistant' role")
		})
	}
}

// TestLoadSessionParsing_RoleBoundary_SplitsUserAndAssistant
// verifies that the replay parsing splits messages at role boundaries:
// user messages and assistant messages are persisted separately.
func TestLoadSessionParsing_RoleBoundary_SplitsUserAndAssistant(t *testing.T) {
	buf := []acp.SessionNotification{
		{
			SessionId: "test",
			Update: acp.SessionUpdate{
				UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
					Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "Hello AI!"}},
				},
			},
		},
		{
			SessionId: "test",
			Update: acp.SessionUpdate{
				AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "Hello human!"}},
				},
			},
		},
	}

	messages := parseNotifications(buf)
	require.Len(t, messages, 2, "should produce two messages split at role boundary")
	assert.Equal(t, "user", messages[0].role)
	assert.Equal(t, "assistant", messages[1].role)

	// Verify user message content
	var userParsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(messages[0].content), &userParsed))
	userBlocks := userParsed["blocks"].([]any)
	require.Len(t, userBlocks, 1)
	assert.Equal(t, "Hello AI!", userBlocks[0].(map[string]any)["text"])

	// Verify assistant message content
	var asstParsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(messages[1].content), &asstParsed))
	asstBlocks := asstParsed["blocks"].([]any)
	require.Len(t, asstBlocks, 1)
	assert.Equal(t, "Hello human!", asstBlocks[0].(map[string]any)["text"])
}

// TestLoadSessionParsing_ContentNotRawJSON
// verifies that the parsed content does NOT contain raw ACP JSON keys
// like "agent_message_chunk", "tool_call", etc.
func TestLoadSessionParsing_ContentNotRawJSON(t *testing.T) {
	buf := []acp.SessionNotification{
		{
			SessionId: "test",
			Update: acp.SessionUpdate{
				AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "Hello!"}},
				},
			},
		},
	}

	messages := parseNotifications(buf)
	require.Len(t, messages, 1)
	assert.Equal(t, "assistant", messages[0].role)

	// The content should NOT contain raw ACP protocol keys
	assert.NotContains(t, messages[0].content, "agent_message_chunk",
		"parsed content should not contain raw ACP protocol keys")
	assert.NotContains(t, messages[0].content, "tool_call",
		"parsed content should not contain raw ACP protocol keys")

	// The content SHOULD contain the parsed text
	assert.Contains(t, messages[0].content, "Hello!",
		"parsed content should contain the actual text")
	assert.Contains(t, messages[0].content, `"type":"text"`,
		"parsed content should have proper block type")
}
