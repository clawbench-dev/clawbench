package ws

import (
	"sync"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestStreamHub_EmitSkipsNilPayload(t *testing.T) {
	mgr, hub := newTestStreamHub()

	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client1", "")
	hub.Subscribe("client1", "session1")

	// resume_split returns nil payload from StreamEventToPayload — Emit should skip it
	hub.Emit("session1", ai.StreamEvent{Type: "resume_split"})
}

// --- EmitToSession ---

func TestEmitToSession_NilManager(t *testing.T) {
	orig := defaultManager
	defaultManager = nil
	defer func() { defaultManager = orig }()

	// Should not panic when global manager is nil
	EmitToSession("session1", ai.StreamEvent{Type: "content", Content: "hello"})
}

func TestEmitToSession_NilHub(t *testing.T) {
	orig := defaultManager
	defer func() { defaultManager = orig }()

	mgr := &Manager{subscriptions: make(map[string]*ClientSubscription)} // no StreamHub
	defaultManager = mgr

	// Should not panic when hub is nil
	EmitToSession("session1", ai.StreamEvent{Type: "content", Content: "hello"})
}

func TestEmitToSession_NoSubscribers(t *testing.T) {
	orig := defaultManager
	defer func() { defaultManager = orig }()

	mgr := NewManagerForTest()
	SetManagerForTest(mgr)

	// No subscribers for session1 — should return early without panic
	EmitToSession("session1", ai.StreamEvent{Type: "content", Content: "hello"})
}

func TestEmitToSession_WithSubscribers(t *testing.T) {
	orig := defaultManager
	defer func() { defaultManager = orig }()

	mgr := NewManagerForTest()
	SetManagerForTest(mgr)

	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client1", "")
	mgr.StreamHub().Subscribe("client1", "session1")

	// Should not panic with subscribers
	EmitToSession("session1", ai.StreamEvent{Type: "content", Content: "hello"})

	assert.True(t, mgr.StreamHub().HasSubscribers("session1"))
}

// --- StreamEventToPayload: basic types ---

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

func TestStreamEventToPayload_ThinkingDone(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "thinking_done"})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.Empty(t, m)
}

func TestStreamEventToPayload_Done(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "done"})
	_, ok := payload.(map[string]any)
	assert.True(t, ok)
}

func TestStreamEventToPayload_ReplayDone(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "replay_done"})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.Empty(t, m)
}

func TestStreamEventToPayload_Cancelled(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "cancelled"})
	m, ok := payload.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "cancelled", m["reason"])
}

func TestStreamEventToPayload_Metadata(t *testing.T) {
	meta := &ai.Metadata{Model: "gpt-4", InputTokens: 100}
	payload := StreamEventToPayload(ai.StreamEvent{Type: "metadata", Meta: meta})
	result, ok := payload.(*ai.Metadata)
	assert.True(t, ok)
	assert.Equal(t, "gpt-4", result.Model)
}

func TestStreamEventToPayload_ResumeSplit(t *testing.T) {
	// resume_split is handled by EmitResumeSplitEvent, not StreamEventToPayload
	payload := StreamEventToPayload(ai.StreamEvent{Type: "resume_split"})
	assert.Nil(t, payload, "resume_split should return nil since it's handled separately")
}

// --- StreamEventToPayload: error and warning ---

func TestStreamEventToPayload_Error(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "error", Error: "oops", Reason: "timeout"})
	m, ok := payload.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "oops", m["error"])
	assert.Equal(t, "timeout", m["reason"])
}

func TestStreamEventToPayload_ErrorNoReason(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "error", Error: "oops"})
	m, ok := payload.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "oops", m["error"])
	_, hasReason := m["reason"]
	assert.False(t, hasReason, "reason should be omitted when empty")
}

func TestStreamEventToPayload_Warning(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "warning", Content: "slow response", Reason: "timeout"})
	m, ok := payload.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "slow response", m["text"])
	assert.Equal(t, "timeout", m["reason"])
}

func TestStreamEventToPayload_WarningNoReason(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "warning", Content: "slow"})
	m, ok := payload.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "slow", m["text"])
	_, hasReason := m["reason"]
	assert.False(t, hasReason, "reason should be omitted when empty")
}

// --- acpStatePayload ---

func TestStreamEventToPayload_ModeUpdate(t *testing.T) {
	mode := &ai.ModeState{CurrentModeID: "code"}
	payload := StreamEventToPayload(ai.StreamEvent{Type: "mode_update", Mode: mode})
	result, ok := payload.(*ai.ModeState)
	assert.True(t, ok)
	assert.Equal(t, "code", result.CurrentModeID)
}

func TestStreamEventToPayload_ConfigUpdate(t *testing.T) {
	config := &ai.ConfigOptionState{ConfigID: "permissions", CurrentID: "default"}
	payload := StreamEventToPayload(ai.StreamEvent{Type: "config_update", Config: config})
	result, ok := payload.(*ai.ConfigOptionState)
	assert.True(t, ok)
	assert.Equal(t, "permissions", result.ConfigID)
}

func TestStreamEventToPayload_CommandsUpdate(t *testing.T) {
	commands := []ai.AvailableCommandInfo{{Name: "/help", Description: "Show help"}}
	payload := StreamEventToPayload(ai.StreamEvent{Type: "commands_update", Commands: commands})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	cmds, _ := m["commands"].([]ai.AvailableCommandInfo)
	assert.Len(t, cmds, 1)
	assert.Equal(t, "/help", cmds[0].Name)
}

func TestStreamEventToPayload_CommandsUpdateNil(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "commands_update", Commands: nil})
	assert.Nil(t, payload, "nil commands should return nil")
}

func TestStreamEventToPayload_ThinkingEffortUpdate(t *testing.T) {
	effort := &ai.ThinkingEffortState{CurrentID: "high"}
	payload := StreamEventToPayload(ai.StreamEvent{Type: "thinking_effort_update", ThinkingEffort: effort})
	result, ok := payload.(*ai.ThinkingEffortState)
	assert.True(t, ok)
	assert.Equal(t, "high", result.CurrentID)
}

func TestStreamEventToPayload_ModelListUpdate(t *testing.T) {
	ml := &ai.ModelListState{CurrentModelID: "gpt-4"}
	payload := StreamEventToPayload(ai.StreamEvent{Type: "model_list_update", ModelList: ml})
	result, ok := payload.(*ai.ModelListState)
	assert.True(t, ok)
	assert.Equal(t, "gpt-4", result.CurrentModelID)
}

func TestStreamEventToPayload_PlanUpdate(t *testing.T) {
	plan := &ai.PlanState{Entries: []ai.PlanEntry{{Content: "Step 1", Status: "pending"}}}
	payload := StreamEventToPayload(ai.StreamEvent{Type: "plan_update", Plan: plan})
	result, ok := payload.(*ai.PlanState)
	assert.True(t, ok)
	assert.Len(t, result.Entries, 1)
}

func TestStreamEventToPayload_UsageUpdate(t *testing.T) {
	usage := &ai.UsageState{Used: 50000, Size: 200000}
	payload := StreamEventToPayload(ai.StreamEvent{Type: "usage_update", Usage: usage})
	result, ok := payload.(*ai.UsageState)
	assert.True(t, ok)
	assert.Equal(t, 50000, result.Used)
}

func TestStreamEventToPayload_ACPUnknownType(t *testing.T) {
	// Unknown types that fall through both switches should return nil
	payload := StreamEventToPayload(ai.StreamEvent{Type: "something_unrecognized"})
	assert.Nil(t, payload)
}

// --- toolUsePayload ---

func TestStreamEventToPayload_ToolUseNilTool(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "tool_use", Tool: nil})
	assert.Nil(t, payload)
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

func TestStreamEventToPayload_ToolUseWithStatus(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Bash", ID: "t3", Done: false, Status: "running"},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "running", m["status"])
}

func TestStreamEventToPayload_ToolUseEmptyStatus(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Read", ID: "t4", Done: true},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	_, hasStatus := m["status"]
	assert.False(t, hasStatus, "status should be omitted when empty")
}

func TestStreamEventToPayload_ToolUseInteractive(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "AskUserQuestion", ID: "t2", Done: true, Input: `{"questions":[]}`},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	input, _ := m["input"].(map[string]any)
	assert.NotNil(t, input, "interactive tools should include input")
}

func TestStreamEventToPayload_ToolUsePermissionApproval(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "PermissionApproval", ID: "t5", Done: true, Input: `{"tool":"Bash","input":"rm -rf /"}`},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	input, _ := m["input"].(map[string]any)
	assert.NotNil(t, input)
	assert.Equal(t, "Bash", input["tool"])
}

func TestStreamEventToPayload_ToolUseInteractiveEmptyInput(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "AskUserQuestion", ID: "t6", Done: true, Input: ""},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	input, _ := m["input"].(map[string]any)
	assert.NotNil(t, input, "empty input should default to empty map")
	assert.Empty(t, input)
}

func TestStreamEventToPayload_ToolUseInteractiveNonObjectInput(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "AskUserQuestion", ID: "t7", Done: true, Input: `"just a string"`},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	input, _ := m["input"].(map[string]any)
	assert.NotNil(t, input, "non-object JSON input should default to empty map")
}

// --- toolResultPayload ---

func TestStreamEventToPayload_ToolResultNilTool(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "tool_result", Tool: nil})
	assert.Nil(t, payload)
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

func TestStreamEventToPayload_ToolResultMinimal(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{
		Type: "tool_result",
		Tool: &ai.ToolCall{ID: "t10"},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "t10", m["id"])
	_, hasName := m["name"]
	assert.False(t, hasName, "name should be omitted when empty")
	_, hasStatus := m["status"]
	assert.False(t, hasStatus, "status should be omitted when empty")
}

// --- attachToolMeta ---

func TestAttachToolMeta_WithAllFields(t *testing.T) {
	meta := ai.ToolCallMeta{DisplayName: "Edit File", Summary: "editing", FilePath: "/tmp/a.go"}
	payload := StreamEventToPayload(ai.StreamEvent{
		Type:     "tool_use",
		ToolMeta: &meta,
		Tool:     &ai.ToolCall{Name: "Edit", ID: "t8", Done: true},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "Edit File", m["display_name"])
	assert.Equal(t, "editing", m["summary"])
	assert.Equal(t, "/tmp/a.go", m["file_path"])
}

func TestAttachToolMeta_NilMeta(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{
		Type:     "tool_use",
		ToolMeta: nil,
		Tool:     &ai.ToolCall{Name: "Read", ID: "t9", Done: true},
	})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	_, hasSummary := m["summary"]
	assert.False(t, hasSummary, "summary should be absent when meta is nil")
	_, hasDisplayName := m["display_name"]
	assert.False(t, hasDisplayName)
	_, hasFilePath := m["file_path"]
	assert.False(t, hasFilePath)
}

func TestAttachToolMeta_EmptyMeta(t *testing.T) {
	payload := map[string]any{}
	meta := &ai.ToolCallMeta{} // all fields empty
	attachToolMeta(payload, meta)
	_, hasSummary := payload["summary"]
	assert.False(t, hasSummary, "empty summary should not be attached")
	_, hasDisplayName := payload["display_name"]
	assert.False(t, hasDisplayName)
	_, hasFilePath := payload["file_path"]
	assert.False(t, hasFilePath)
}

func TestAttachToolMeta_NilMetaDirect(t *testing.T) {
	payload := map[string]any{}
	attachToolMeta(payload, nil)
	assert.Empty(t, payload, "nil meta should not modify payload")
}

// --- userMessagePayload ---

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

// --- queueDrainPayload ---

func TestStreamEventToPayload_QueueDrain(t *testing.T) {
	qe := &ai.QueueEventData{
		SessionID: "sess1",
		QueueID:   "q1",
		Text:      "hello",
		MessageID: 99,
		FilePaths: []string{"/a.go"},
		Files:     []model.FileEntry{{Path: "/a.go"}},
		Queue:     []model.QueuedMessage{{QueueID: "q1", Text: "hello"}},
	}
	payload := StreamEventToPayload(ai.StreamEvent{Type: "queue_drain", QueueEvent: qe})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "sess1", m["sessionId"])
	assert.Equal(t, "q1", m["queueId"])
	assert.Equal(t, "hello", m["text"])
	assert.Equal(t, int64(99), m["messageId"])
}

func TestStreamEventToPayload_QueueDrainNil(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "queue_drain", QueueEvent: nil})
	assert.Nil(t, payload)
}

// --- queueCancelPayload ---

func TestStreamEventToPayload_QueueCancel(t *testing.T) {
	qe := &ai.QueueEventData{
		SessionID: "sess2",
		QueueIDs:  []string{"q1", "q2"},
	}
	payload := StreamEventToPayload(ai.StreamEvent{Type: "queue_cancel", QueueEvent: qe})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "sess2", m["sessionId"])
	ids, _ := m["queueIds"].([]string)
	assert.Equal(t, []string{"q1", "q2"}, ids)
}

func TestStreamEventToPayload_QueueCancelNoQueueIDs(t *testing.T) {
	qe := &ai.QueueEventData{SessionID: "sess3"}
	payload := StreamEventToPayload(ai.StreamEvent{Type: "queue_cancel", QueueEvent: qe})
	m, ok := payload.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "sess3", m["sessionId"])
	_, hasQueueIDs := m["queueIds"]
	assert.False(t, hasQueueIDs, "queueIds should be omitted when empty")
}

func TestStreamEventToPayload_QueueCancelNil(t *testing.T) {
	payload := StreamEventToPayload(ai.StreamEvent{Type: "queue_cancel", QueueEvent: nil})
	assert.Nil(t, payload)
}

// --- EmitStreamStartEvent ---

func TestStreamHub_EmitStreamStartEvent(t *testing.T) {
	mgr, hub := newTestStreamHub()

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "client-start", "")
	hub.Subscribe("client-start", "session-start")

	hub.EmitStreamStartEvent("client-start", "session-start", 42)

	buffered := sub.GetBufferedEvents()
	require.NotEmpty(t, buffered, "expected at least one buffered event")
	data, ok := buffered[0].Data.(ChatStreamData)
	require.True(t, ok, "expected ChatStreamData")
	assert.Equal(t, "stream_start", data.EventType)
	assert.Equal(t, "session-start", data.SessionID)
}

// --- EmitResumeSplitEvent ---

func TestStreamHub_EmitResumeSplitEvent_WithSubscribers(t *testing.T) {
	mgr, hub := newTestStreamHub()

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "client-resume", "")
	hub.Subscribe("client-resume", "session-resume")

	hub.EmitResumeSplitEvent("session-resume", 99)

	buffered := sub.GetBufferedEvents()
	require.NotEmpty(t, buffered, "expected at least one buffered event")
	data, ok := buffered[0].Data.(ChatStreamData)
	require.True(t, ok, "expected ChatStreamData")
	assert.Equal(t, "resume_split", data.EventType)
	payload, _ := data.Payload.(map[string]any)
	assert.Equal(t, int64(99), payload["message_id"])
}

func TestStreamHub_EmitResumeSplitEvent_NoSubscribers(t *testing.T) {
	_, hub := newTestStreamHub()

	// Should return early without panic
	hub.EmitResumeSplitEvent("session-none", 1)
}

func TestStreamHub_EmitResumeSplitEvent_ZeroMessageID(t *testing.T) {
	mgr, hub := newTestStreamHub()

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "client-zero", "")
	hub.Subscribe("client-zero", "session-zero")

	hub.EmitResumeSplitEvent("session-zero", 0)

	buffered := sub.GetBufferedEvents()
	require.NotEmpty(t, buffered, "expected at least one buffered event")
	data, ok := buffered[0].Data.(ChatStreamData)
	require.True(t, ok, "expected ChatStreamData")
	payload, _ := data.Payload.(map[string]any)
	_, hasMessageID := payload["message_id"]
	assert.False(t, hasMessageID, "message_id should be omitted when 0")
}

// --- EmitACPStateEvents ---

func TestStreamHub_EmitACPStateEvents_NoState(t *testing.T) {
	mgr, hub := newTestStreamHub()

	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client1", "")

	// No ACP connection exists for this session — should not panic
	hub.EmitACPStateEvents("client1", "session-no-conn")
}

func TestStreamHub_EmitACPStateEvents_WithCachedState(t *testing.T) {
	acpMgr := ai.GetACPConnManager()
	agent := &model.Agent{ID: "test-acp-emit", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := ai.NewACPConnForTest(agent, "session-acp-emit")
	conn.SetCachedModeState(&ai.ModeState{CurrentModeID: "code", AvailableModes: []ai.ModeDef{{ID: "code", Name: "Code"}}})
	conn.SetCachedConfigState(&ai.ConfigOptionState{ConfigID: "permissions", CurrentID: "default"})
	conn.SetCachedThinkingEffortState(&ai.ThinkingEffortState{CurrentID: "high"})
	conn.SetCachedPlanState(&ai.PlanState{Entries: []ai.PlanEntry{{Content: "Step 1", Status: "pending"}}})
	conn.SetCachedModelListState(&ai.ModelListState{CurrentModelID: "gpt-4"})
	conn.SetCachedUsageState(&ai.UsageState{Used: 50000, Size: 200000})
	acpMgr.SetConnForTest("session-acp-emit", conn)
	defer acpMgr.CloseConn("session-acp-emit")

	wsMgr, hub := newTestStreamHub()
	var writeMu sync.Mutex
	sub := wsMgr.Subscribe(nil, &writeMu, "client1", "")

	hub.EmitACPStateEvents("client1", "session-acp-emit")

	// Should have emitted multiple state events
	buffered := sub.GetBufferedEvents()
	require.NotEmpty(t, buffered, "expected cached state events")
}

func TestStreamHub_EmitACPStateEvents_WithCommandsOnly(t *testing.T) {
	acpMgr := ai.GetACPConnManager()
	agentID := "test-acp-cmds"
	agent := &model.Agent{ID: agentID, Backend: "acp-stdio", AcpCommand: "echo"}
	conn := ai.NewACPConnForTest(agent, "session-acp-cmds")
	// Register commands via the capability registry
	ai.GetAgentCapabilityRegistry().UpdateCommands(agentID, []ai.AvailableCommandInfo{{Name: "/help", Description: "Show help"}})
	acpMgr.SetConnForTest("session-acp-cmds", conn)
	defer acpMgr.CloseConn("session-acp-cmds")

	wsMgr, hub := newTestStreamHub()
	var writeMu sync.Mutex
	sub := wsMgr.Subscribe(nil, &writeMu, "client1", "")

	hub.EmitACPStateEvents("client1", "session-acp-cmds")

	buffered := sub.GetBufferedEvents()
	require.NotEmpty(t, buffered, "expected commands_update event")
	// Find the commands_update event among buffered events
	found := false
	for _, evt := range buffered {
		data, ok := evt.Data.(ChatStreamData)
		if ok && data.EventType == "commands_update" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected commands_update event in buffered events")
}

func TestStreamHub_EmitACPStateEvents_OrphanedUsage(t *testing.T) {
	acpMgr := ai.GetACPConnManager()
	// Create a connection with usage state, then close it to orphan the usage
	agent := &model.Agent{ID: "test-acp-orphan", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := ai.NewACPConnForTest(agent, "session-orphan")
	conn.SetCachedUsageState(&ai.UsageState{Used: 999, Size: 200000})
	acpMgr.SetConnForTest("session-orphan", conn)
	// CloseConn preserves usage into orphanedUsage
	acpMgr.CloseConn("session-orphan")

	wsMgr, hub := newTestStreamHub()
	var writeMu sync.Mutex
	sub := wsMgr.Subscribe(nil, &writeMu, "client1", "")

	hub.EmitACPStateEvents("client1", "session-orphan")

	buffered := sub.GetBufferedEvents()
	require.NotEmpty(t, buffered, "expected orphaned usage_update event")
	data, ok := buffered[0].Data.(ChatStreamData)
	require.True(t, ok)
	assert.Equal(t, "usage_update", data.EventType)
	usage, _ := data.Payload.(*ai.UsageState)
	require.NotNil(t, usage)
	assert.Equal(t, 999, usage.Used)
}
