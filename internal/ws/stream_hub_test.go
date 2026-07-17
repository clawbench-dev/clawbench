package ws

import (
	"sync"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

func newTestStreamHub() (*Manager, *StreamHub) {
	mgr := NewManagerForTest()
	hub := mgr.StreamHub()
	return mgr, hub
}

func TestStreamHub_Subscribe(t *testing.T) {
	_, hub := newTestStreamHub()

	hub.Subscribe("client1", "session1")
	assert.True(t, hub.HasSubscribers("session1"))

	hub.Subscribe("client2", "session1")
	assert.True(t, hub.HasSubscribers("session1"))

	// Multiple subscribers for same session
	hub.mu.RLock()
	count := len(hub.subscribers["session1"])
	hub.mu.RUnlock()
	assert.Equal(t, 2, count)
}

func TestStreamHub_Unsubscribe(t *testing.T) {
	_, hub := newTestStreamHub()

	hub.Subscribe("client1", "session1")
	hub.Subscribe("client2", "session1")
	hub.Unsubscribe("client1", "session1")

	assert.True(t, hub.HasSubscribers("session1"))

	hub.Unsubscribe("client2", "session1")
	assert.False(t, hub.HasSubscribers("session1"))
}

func TestStreamHub_UnsubscribeAll(t *testing.T) {
	_, hub := newTestStreamHub()

	hub.Subscribe("client1", "session1")
	hub.Subscribe("client1", "session2")
	hub.Subscribe("client2", "session1")

	hub.UnsubscribeAll("client1")

	assert.True(t, hub.HasSubscribers("session1"))  // client2 still subscribed
	assert.False(t, hub.HasSubscribers("session2")) // client1 was only subscriber

	hub.UnsubscribeAll("client2")
	assert.False(t, hub.HasSubscribers("session1"))
}

func TestStreamHub_HasSubscribers(t *testing.T) {
	_, hub := newTestStreamHub()

	assert.False(t, hub.HasSubscribers("nonexistent"))

	hub.Subscribe("client1", "session1")
	assert.True(t, hub.HasSubscribers("session1"))
	assert.False(t, hub.HasSubscribers("session2"))
}

func TestStreamHub_EmitNoSubscribers(t *testing.T) {
	mgr, hub := newTestStreamHub()
	_ = mgr // hub.Emit uses mgr.SendToClient but with no subscribers, it returns early

	// Should not panic with no subscribers
	hub.Emit("session1", ai.StreamEvent{Type: "content", Content: "hello"})
}

func TestStreamHub_EmitWithSubscribers(t *testing.T) {
	mgr, hub := newTestStreamHub()

	// Create a subscription for client1
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client1", "")
	hub.Subscribe("client1", "session1")

	// Emit should try to send to client1 (will fail since conn is nil, but shouldn't panic)
	hub.Emit("session1", ai.StreamEvent{Type: "content", Content: "hello"})

	// Verify no panic
	assert.True(t, hub.HasSubscribers("session1"))
}

func TestStreamHub_EmitDoesNotSendToUnsubscribed(t *testing.T) {
	mgr, hub := newTestStreamHub()

	// Create subscription for client1 only
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client1", "")
	mgr.Subscribe(nil, &writeMu, "client2", "")

	hub.Subscribe("client1", "session1")
	// client2 is NOT subscribed to session1

	// Emit should only try to send to client1
	hub.Emit("session1", ai.StreamEvent{Type: "content", Content: "hello"})

	// Verify subscriber list is correct
	hub.mu.RLock()
	subs := hub.subscribers["session1"]
	hub.mu.RUnlock()
	_, hasClient2 := subs["client2"]
	assert.False(t, hasClient2, "client2 should not be subscribed to session1")
}

func TestStreamEventToPayload_Content(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "content", Content: "hello"})
	m, ok := payload.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "hello", m["content"])
}

func TestStreamEventToPayload_Thinking(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "thinking", Content: "thought"})
	m, ok := payload.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "thought", m["text"])
}

func TestStreamEventToPayload_Done(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "done"})
	_, ok := payload.(map[string]any)
	assert.True(t, ok)
}

func TestStreamEventToPayload_Cancelled(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "cancelled"})
	m, ok := payload.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "cancelled", m["reason"])
}

func TestStreamEventToPayload_Error(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "error", Error: "oops", Reason: "timeout"})
	m, ok := payload.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "oops", m["error"])
	assert.Equal(t, "timeout", m["reason"])
}

func TestStreamEventToPayload_ToolUse(t *testing.T) {
	meta := ai.ToolCallMeta{Summary: "reading file", FilePath: "/tmp/test.go"}
	payload := StreamEventToPayload(ai.StreamEvent{
		Type:     "tool_use",
		ToolMeta: &meta,
		Tool:     &ai.ToolCall{Name: "Read", ID: "t1", Done: true},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "Read", m["name"])
	assert.Equal(t, "t1", m["id"])
	assert.Equal(t, true, m["done"])
	assert.Equal(t, "reading file", m["summary"])
	assert.Equal(t, "/tmp/test.go", m["file_path"])
}

func TestStreamEventToPayload_ToolUseInteractive(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "AskUserQuestion", ID: "t2", Done: true, Input: `{"questions":[]}`},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.NotNil(t, m["input"], "interactive tools should include input")
}

func TestStreamEventToPayload_ToolResult(t *testing.T) {
	meta := ai.ToolCallMeta{Summary: "file read done"}
	payload := StreamEventToPayload(ai.StreamEvent{
		Type:     "tool_result",
		ToolMeta: &meta,
		Tool:     &ai.ToolCall{ID: "t1", Name: "Read", Status: "success"},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "t1", m["id"])
	assert.Equal(t, "Read", m["name"])
	assert.Equal(t, "success", m["status"])
	assert.Equal(t, "file read done", m["summary"])
}

func TestStreamEventToPayload_Warning(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "warning", Content: "slow response", Reason: "timeout"})
	m, ok := payload.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "slow response", m["text"])
	assert.Equal(t, "timeout", m["reason"])
}

func TestStreamEventToPayload_Metadata(t *testing.T) {
	meta := &ai.Metadata{Model: "gpt-4", InputTokens: 100}
	payload := StreamEventToPayload(ai.StreamEvent{Type: "metadata", Meta: meta})
	result, ok := payload.(*ai.Metadata)
	assert.True(t, ok)
	assert.Equal(t, "gpt-4", result.Model)
}

func TestStreamEventToPayload_Unknown(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "unknown_type"})
	assert.Nil(t, payload)
}

func TestStreamEventToPayload_UserMessage(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{
		Type: "user_message",
		UserMessage: &ai.UserMessageData{
			MessageID: 42,
			Content:   "hello from phone",
			Files:     []model.FileEntry{{Path: "/tmp/a.go", IsDir: false}},
		},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, int64(42), m["messageId"])
	assert.Equal(t, "hello from phone", m["content"])
	files, _ := m["files"].([]model.FileEntry)
	assert.Len(t, files, 1)
	assert.Equal(t, "/tmp/a.go", files[0].Path)
}

func TestStreamEventToPayload_UserMessage_NoFiles(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{
		Type: "user_message",
		UserMessage: &ai.UserMessageData{
			MessageID:      10,
			Content:        "simple text",
			SenderClientID: "client-abc",
			QueueID:        "pending-123",
		},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, int64(10), m["messageId"])
	assert.Equal(t, "simple text", m["content"])
	assert.Equal(t, "client-abc", m["senderClientId"])
	assert.Equal(t, "pending-123", m["queueId"])
	_, hasFiles := m["files"]
	assert.False(t, hasFiles, "files should be omitted when empty")
}

func TestStreamEventToPayload_UserMessage_Nil(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "user_message", UserMessage: nil})
	assert.Nil(t, payload)
}

func TestStreamEventToPayload_ResumeSplit(t *testing.T) {
	// resume_split is handled by EmitResumeSplitEvent, not StreamEventToPayload
	payload := StreamEventToPayload(ai.StreamEvent{Type: "resume_split"})
	assert.Nil(t, payload, "resume_split should return nil since it's handled separately")
}

func TestStreamEventToPayload_ToolUseNilTool(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "tool_use", Tool: nil})
	assert.Nil(t, payload)
}

func TestStreamEventToPayload_ToolResultNilTool(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "tool_result", Tool: nil})
	assert.Nil(t, payload)
}
