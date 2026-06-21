package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseSSEEvents splits an SSE response body into individual event+data pairs.
func parseSSEEvents(body string) []map[string]string {
	var events []map[string]string
	parts := strings.Split(body, "\n\n")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		entry := map[string]string{}
		lines := strings.Split(part, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "event: ") {
				entry["event"] = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				entry["data"] = strings.TrimPrefix(line, "data: ")
			}
		}
		if _, ok := entry["event"]; ok {
			events = append(events, entry)
		}
	}
	return events
}

// setupStreamSession creates a running session with a stream channel for testing.
func setupStreamSession(sessionID string) chan ai.StreamEvent {
	service.SetSessionRunning(sessionID, true)
	ch := service.RegisterSessionStream(sessionID)
	return ch
}

// cleanupStreamSession tears down a test session.
func cleanupStreamSession(sessionID string) {
	service.UnregisterSessionStream(sessionID)
	service.SetSessionRunning(sessionID, false)
	service.UnregisterSessionCancel(sessionID)
}

func TestAIChatStream_MethodNotAllowed(t *testing.T) {
	req := newRequest(t, http.MethodPost, "/api/ai/chat/stream", nil)
	w := callHandler(AIChatStream, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestAIChatStream_MissingProjectCookie(t *testing.T) {
	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id=s1", nil)
	w := callHandler(AIChatStream, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestAIChatStream_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestAIChatStream_SessionNotRunning(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id=not-running", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	events := parseSSEEvents(w.Body.String())
	assert.Len(t, events, 1)
	assert.Equal(t, "error", events[0]["event"])
	assert.Contains(t, events[0]["data"], "Session is not running")
}

func TestAIChatStream_NoStreamChannel(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	service.SetSessionRunning("no-stream", true)
	defer service.SetSessionRunning("no-stream", false)

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id=no-stream", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	assert.Equal(t, http.StatusOK, w.Code)
	events := parseSSEEvents(w.Body.String())
	assert.Len(t, events, 1)
	assert.Equal(t, "error", events[0]["event"])
	assert.Contains(t, events[0]["data"], "Session stream not found")
}

func TestAIChatStream_ContentEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-content"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{Type: "content", Content: "hello world"}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "content", events[0]["event"])
	var data map[string]string
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "hello world", data["content"])
	assert.Equal(t, "done", events[1]["event"])
}

func TestAIChatStream_ThinkingEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-thinking"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{Type: "thinking", Content: "let me think..."}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "thinking", events[0]["event"])
	var data map[string]string
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "let me think...", data["text"])
}

func TestAIChatStream_ToolUseEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-tooluse"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type:     "tool_use",
			Tool:     &ai.ToolCall{Name: "Read", ID: "t1", Input: `{"file_path":"/foo.go"}`, Done: true},
			ToolMeta: &ai.ToolCallMeta{ToolID: "t1", Summary: "foo.go", FilePath: "/foo.go"},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "tool_use", events[0]["event"])
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "Read", data["name"])
	assert.Equal(t, "t1", data["id"])
	assert.Equal(t, true, data["done"])
	// Slim SSE: no input/output
	assert.Nil(t, data["input"], "slim SSE should not include input")
	// But summary/file_path should be present
	assert.Equal(t, "foo.go", data["summary"])
	assert.Equal(t, "/foo.go", data["file_path"])
}

func TestAIChatStream_ToolUseEventWithOutput(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-tooluse-output"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type:     "tool_use",
			Tool:     &ai.ToolCall{Name: "Bash", ID: "t3", Input: `{"command":"ls"}`, Done: true, Output: "file1.go\nfile2.go", Status: "success"},
			ToolMeta: &ai.ToolCallMeta{ToolID: "t3", Summary: "ls"},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "tool_use", events[0]["event"])
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "Bash", data["name"])
	assert.Equal(t, "success", data["status"])
	// Slim SSE: no output in the event
	assert.Nil(t, data["output"], "slim SSE should not include output")
	assert.Equal(t, "ls", data["summary"])
}

func TestAIChatStream_ToolResultEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-toolresult"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type:     "tool_result",
			Tool:     &ai.ToolCall{ID: "t5", Output: "file contents here", Status: "success"},
			ToolMeta: &ai.ToolCallMeta{ToolID: "t5", Summary: "result"},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "tool_result", events[0]["event"])
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "t5", data["id"])
	assert.Equal(t, "success", data["status"])
	// Slim SSE: no output
	assert.Nil(t, data["output"], "slim SSE should not include output")
	assert.Equal(t, "result", data["summary"])
}

func TestAIChatStream_ToolResultEventError(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-toolresult-err"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type:     "tool_result",
			Tool:     &ai.ToolCall{ID: "t6", Output: "command not found", Status: "error"},
			ToolMeta: &ai.ToolCallMeta{ToolID: "t6"},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "tool_result", events[0]["event"])
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "t6", data["id"])
	assert.Equal(t, "error", data["status"])
	// Slim SSE: no output
	assert.Nil(t, data["output"], "slim SSE should not include output")
}

func TestAIChatStream_ToolResultEventNilTool(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-toolresult-nil"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{Type: "tool_result", Tool: nil}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	// tool_result with nil Tool should be silently skipped
	assert.Len(t, events, 1)
	assert.Equal(t, "done", events[0]["event"])
}

func TestAIChatStream_ToolResultEventNoOutput(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-toolresult-nooutput"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		// tool_result with no output/status — should only emit id
		ch <- ai.StreamEvent{
			Type: "tool_result",
			Tool: &ai.ToolCall{ID: "t7"},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "tool_result", events[0]["event"])
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "t7", data["id"])
	assert.Nil(t, data["output"])
	assert.Nil(t, data["status"])
}

func TestAIChatStream_ToolUseEvent_EmptyInput(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-tooluse-empty"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type: "tool_use",
			Tool: &ai.ToolCall{Name: "Bash", ID: "t2", Input: "", Done: false},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "tool_use", events[0]["event"])
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	// Slim SSE: no input field at all for non-interactive tools
	assert.Nil(t, data["input"], "slim SSE should not include input")
}

func TestAIChatStream_ToolUseEvent_StringInput(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-tooluse-string-input"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		// Tool call with input that deserializes to a string (e.g., partial JSON
		// from input_json_delta's first chunk "{"). With slim SSE, non-interactive
		// tools don't include input at all, so this no longer needs the coercion.
		ch <- ai.StreamEvent{
			Type: "tool_use",
			Tool: &ai.ToolCall{Name: "Bash", ID: "t3", Input: `"<partial>"`, Done: false},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "tool_use", events[0]["event"])
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	// Slim SSE: no input for non-interactive tools
	assert.Nil(t, data["input"], "slim SSE should not include input")
}

func TestAIChatStream_ToolUseEvent_InteractiveToolIncludesInput(t *testing.T) {
	// Interactive tools (AskUserQuestion, PermissionApproval) must include input
	// in the SSE event so the frontend can render the question UI without
	// waiting for a DB round-trip.
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-tooluse-interactive"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type:     "tool_use",
			Tool:     &ai.ToolCall{Name: "AskUserQuestion", ID: "t4", Input: `{"questions":[{"header":"Choose","question":"A or B?"}]}`, Done: false},
			ToolMeta: &ai.ToolCallMeta{ToolID: "t4", Summary: "Choose"},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "tool_use", events[0]["event"])
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "AskUserQuestion", data["name"])
	// Interactive tools still include input
	input, ok := data["input"].(map[string]any)
	assert.True(t, ok, "interactive tool SSE should include input")
	assert.NotNil(t, input["questions"])
	assert.Equal(t, "Choose", data["summary"])
}

func TestAIChatStream_ToolUseEvent_NilTool(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-tooluse-nil"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{Type: "tool_use", Tool: nil}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Len(t, events, 1)
	assert.Equal(t, "done", events[0]["event"])
}

func TestAIChatStream_MetadataEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-metadata"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type: "metadata",
			Meta: &ai.Metadata{Model: "gpt-4", InputTokens: 100, OutputTokens: 50},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "metadata", events[0]["event"])
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "gpt-4", data["model"])
}

func TestAIChatStream_DoneEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-done"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Len(t, events, 1)
	assert.Equal(t, "done", events[0]["event"])
	assert.Equal(t, "{}", events[0]["data"])
}

func TestAIChatStream_CancelledEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-cancelled"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{Type: "cancelled"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "cancelled", events[0]["event"])
	assert.Contains(t, events[0]["data"], "cancelled")
}

func TestAIChatStream_ErrorEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-error"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{Type: "error", Error: "something broke"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "error", events[0]["event"])
	var data map[string]string
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "something broke", data["error"])
}

func TestAIChatStream_ErrorEventWithReason(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-error-reason"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{Type: "error", Error: "timeout", Reason: "timeout"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	var data map[string]string
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "timeout", data["error"])
	assert.Equal(t, "timeout", data["reason"])
}

func TestAIChatStream_ResumeSplitEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-resume-split"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{Type: "content", Content: "phase 1 content"}
		ch <- ai.StreamEvent{Type: "resume_split"}
		ch <- ai.StreamEvent{Type: "content", Content: "phase 2 content"}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, 4, len(events), "expected content, resume_split, content, done events")

	// First event: content from phase 1
	assert.Equal(t, "content", events[0]["event"])
	var data1 map[string]string
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data1))
	assert.Equal(t, "phase 1 content", data1["content"])

	// Second event: resume_split — no streaming message in DB, so data is {}
	assert.Equal(t, "resume_split", events[1]["event"])
	assert.Equal(t, "{}", events[1]["data"])

	// Third event: content from phase 2
	assert.Equal(t, "content", events[2]["event"])
	var data2 map[string]string
	require.NoError(t, json.Unmarshal([]byte(events[2]["data"]), &data2))
	assert.Equal(t, "phase 2 content", data2["content"])

	// Final event: done
	assert.Equal(t, "done", events[3]["event"])
}

func TestAIChatStream_ResumeSplitEventWithMessageID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-resume-split-msgid"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	// Create a streaming assistant message in the DB so GetStreamingMessageID
	// returns a non-zero ID. This simulates the scenario where resume_split
	// occurs after the backend has already created the Phase 2 streaming placeholder.
	streamingMsgID, err := service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[]}`, nil, true, "")
	require.NoError(t, err)
	require.Greater(t, streamingMsgID, int64(0), "streaming message should have a positive ID")

	go func() {
		ch <- ai.StreamEvent{Type: "content", Content: "phase 1 content"}
		ch <- ai.StreamEvent{Type: "resume_split"}
		ch <- ai.StreamEvent{Type: "content", Content: "phase 2 content"}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	// stream_start is also sent because GetStreamingMessageID > 0
	assert.GreaterOrEqual(t, len(events), 4, "expected at least content, resume_split, content, done events")

	// Find the resume_split event
	var resumeSplitData string
	for _, ev := range events {
		if ev["event"] == "resume_split" {
			resumeSplitData = ev["data"]
			break
		}
	}
	require.NotEmpty(t, resumeSplitData, "should find a resume_split event")

	// resume_split should contain message_id
	var resumeData map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(resumeSplitData), &resumeData))
	msgID, ok := resumeData["message_id"]
	require.True(t, ok, "resume_split data should contain message_id")
	// JSON numbers are float64 by default
	assert.Equal(t, float64(streamingMsgID), msgID, "resume_split message_id should match the streaming message")
}

func TestAIChatStream_WarningEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-warning"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{Type: "warning", Content: "slow response", Reason: "timeout"}
		ch <- ai.StreamEvent{Type: "content", Content: "actual content"}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Len(t, events, 3)
	assert.Equal(t, "warning", events[0]["event"])
	var warnData map[string]string
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &warnData))
	assert.Equal(t, "slow response", warnData["text"])
	assert.Equal(t, "timeout", warnData["reason"])
	assert.Equal(t, "content", events[1]["event"])
	assert.Equal(t, "done", events[2]["event"])
}

func TestAIChatStream_QueueDrainEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-queue-drain"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type:       "queue_drain",
			QueueEvent: &ai.QueueEventData{Text: "hello", FilePaths: []string{"/test.go"}, Files: []string{"/a.txt"}, Queue: []model.QueuedMessage{{Text: "next"}}},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "queue_drain", events[0]["event"])
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "hello", data["text"])
	filePaths, _ := data["filePaths"].([]any)
	assert.Equal(t, "/test.go", filePaths[0])
	files, _ := data["files"].([]any)
	assert.Equal(t, "/a.txt", files[0])
	queue, _ := data["queue"].([]any)
	assert.Len(t, queue, 1)
}

func TestAIChatStream_QueueUpdateEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-queue-update"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type:       "queue_update",
			QueueEvent: &ai.QueueEventData{Queue: []model.QueuedMessage{{Text: "enqueued"}}},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "queue_update", events[0]["event"])
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	queue, _ := data["queue"].([]any)
	assert.Len(t, queue, 1)
}

func TestAIChatStream_ChannelClosed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-closed"
	ch := setupStreamSession(sessionID)
	service.SetSessionRunning(sessionID, true)
	defer service.SetSessionRunning(sessionID, false)

	go func() {
		close(ch)
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Len(t, events, 1)
	assert.Equal(t, "done", events[0]["event"])
}

func TestAIChatStream_ClientDisconnect(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-disconnect"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	// Register a cancel function — should NOT be called on SSE disconnect
	_, cancel := context.WithCancel(context.Background())
	service.RegisterSessionCancel(sessionID, cancel)

	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel2()
		// Send a terminal event so drainStreamChannel can exit;
		// without this the drain goroutine blocks forever.
		time.Sleep(50 * time.Millisecond)
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	req = req.WithContext(ctx2)

	w := httptest.NewRecorder()
	AIChatStream(w, req)

	// After SSE disconnect, the AI session should continue running
	// (no ForceCancelSession called), but the disconnect reason is recorded
	// so the session finalizer knows the SSE client went away.
	reason := service.GetAndClearCancelReason(sessionID)
	assert.Equal(t, "disconnect", reason)
	// Session should still be running
	assert.True(t, service.IsSessionRunning(sessionID))
}

func TestAIChatStream_ClientDisconnectDuringStream(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-disconnect-active"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	// Register a cancel function
	_, cancel := context.WithCancel(context.Background())
	service.RegisterSessionCancel(sessionID, cancel)

	// Send a content event, then cancel the client context to simulate disconnect
	go func() {
		ch <- ai.StreamEvent{Type: "content", Content: "partial"}
		// Give the SSE handler time to receive the content event
		time.Sleep(100 * time.Millisecond)
		// Cancel the client context (simulates disconnect)
		// The SSE handler should detect ctx.Done() and record "disconnect" reason
	}()

	ctx2, cancel2 := context.WithCancel(context.Background())
	// Cancel after a short delay to allow content to be sent
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel2()
		// Send terminal event so drainStreamChannel can exit
		time.Sleep(50 * time.Millisecond)
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	req = req.WithContext(ctx2)

	w := httptest.NewRecorder()
	AIChatStream(w, req)

	// After client disconnect, cancel reason should be "disconnect" (not "user")
	reason := service.GetAndClearCancelReason(sessionID)
	assert.Equal(t, "disconnect", reason)
}

// ---------- SSE claim: reject second client when stream is busy ----------

func TestAIChatStream_SecondClientRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-busy"
	_ = setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	// First client claims the stream
	require.True(t, service.TryClaimSSEStream(sessionID), "first claim should succeed")
	defer service.ReleaseSSEStream(sessionID)

	// Second client should get an SSE error event with reason "sse_busy"
	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	events := parseSSEEvents(w.Body.String())
	assert.Len(t, events, 1)
	assert.Equal(t, "error", events[0]["event"])
	assert.Contains(t, events[0]["data"], "sse_busy")
}

func TestAIChatStream_ClaimReleasedAfterDisconnect(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-claim-release"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	// First client connects and then disconnects (releases claim)
	require.True(t, service.TryClaimSSEStream(sessionID))
	service.ReleaseSSEStream(sessionID)

	// Second client should now be able to claim the stream.
	// Send a done event so the handler exits after connecting.
	go func() {
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	// Should NOT get sse_busy error — should get normal stream events (done in this case)
	for _, e := range events {
		assert.NotEqual(t, "error", e["event"], "should not get sse_busy error after claim released")
	}
	// Should see the done event
	assert.Equal(t, "done", events[len(events)-1]["event"])
}

// ---------- Session ownership validation (ISS-180) ----------

func TestAIChatStream_PlanUpdateEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-plan-update"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type: "plan_update",
			Plan: &ai.PlanState{
				Entries: []ai.PlanEntry{
					{Content: "Read project files", Priority: "high", Status: "completed"},
					{Content: "Implement feature", Priority: "high", Status: "in_progress"},
					{Content: "Write tests", Priority: "medium", Status: "pending"},
				},
			},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "plan_update", events[0]["event"])

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))

	entries, ok := data["entries"].([]any)
	require.True(t, ok, "entries should be a slice")
	assert.Len(t, entries, 3)

	// First entry: completed/high
	entry0 := entries[0].(map[string]any)
	assert.Equal(t, "Read project files", entry0["content"])
	assert.Equal(t, "high", entry0["priority"])
	assert.Equal(t, "completed", entry0["status"])

	// Second entry: in_progress/high
	entry1 := entries[1].(map[string]any)
	assert.Equal(t, "Implement feature", entry1["content"])
	assert.Equal(t, "high", entry1["priority"])
	assert.Equal(t, "in_progress", entry1["status"])

	// Third entry: pending/medium
	entry2 := entries[2].(map[string]any)
	assert.Equal(t, "Write tests", entry2["content"])
	assert.Equal(t, "medium", entry2["priority"])
	assert.Equal(t, "pending", entry2["status"])

	assert.Equal(t, "done", events[1]["event"])
}

func TestAIChatStream_PlanUpdateEventNilPlan(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-plan-update-nil"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		// plan_update with nil Plan should be silently skipped
		ch <- ai.StreamEvent{Type: "plan_update", Plan: nil}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Len(t, events, 1)
	assert.Equal(t, "done", events[0]["event"])
}

func TestAIChatStream_SessionBelongsToDifferentProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session that belongs to a different project
	otherProject := "/other-project"
	sessionID, err := service.CreateSession(otherProject, "claude", "Other Session", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Try to stream that session using the current project's cookie
	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir) // different project
	w := callHandler(AIChatStream, req)

	assertStatus(t, w, http.StatusForbidden)
}

// ---------- ACP SSE event types ----------

func TestAIChatStream_ModeUpdateEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-mode-update"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type: "mode_update",
			Mode: &ai.ModeState{
				CurrentModeID: "code",
				AvailableModes: []ai.ModeDef{
					{ID: "ask", Name: "Ask"},
					{ID: "architect", Name: "Architect"},
					{ID: "code", Name: "Code"},
				},
			},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "mode_update", events[0]["event"])

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "code", data["currentModeId"])

	modes, ok := data["availableModes"].([]any)
	require.True(t, ok, "availableModes should be a slice")
	assert.Len(t, modes, 3)

	mode0 := modes[0].(map[string]any)
	assert.Equal(t, "ask", mode0["id"])
	assert.Equal(t, "Ask", mode0["name"])

	assert.Equal(t, "done", events[1]["event"])
}

func TestAIChatStream_ThinkingEffortUpdateEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-thinking-effort-update"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type: "thinking_effort_update",
			ThinkingEffort: &ai.ThinkingEffortState{
				CurrentID: "high",
				AvailableLevels: []ai.ThinkingEffortDef{
					{ID: "low", Name: "Low"},
					{ID: "medium", Name: "Medium"},
					{ID: "high", Name: "High"},
				},
			},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "thinking_effort_update", events[0]["event"])

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "high", data["currentId"])

	levels, ok := data["availableLevels"].([]any)
	require.True(t, ok, "availableLevels should be a slice")
	assert.Len(t, levels, 3)

	level0 := levels[0].(map[string]any)
	assert.Equal(t, "low", level0["id"])
	assert.Equal(t, "Low", level0["name"])

	assert.Equal(t, "done", events[1]["event"])
}

func TestAIChatStream_CommandsUpdateEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-commands-update"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type: "commands_update",
			Commands: []ai.AvailableCommandInfo{
				{Name: "compact", Description: "Compact conversation history"},
				{Name: "clear", Description: "Clear conversation", InputHint: "[args]"},
			},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "commands_update", events[0]["event"])

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))

	cmds, ok := data["commands"].([]any)
	require.True(t, ok, "commands should be a slice")
	assert.Len(t, cmds, 2)

	cmd0 := cmds[0].(map[string]any)
	assert.Equal(t, "compact", cmd0["name"])
	assert.Equal(t, "Compact conversation history", cmd0["description"])

	cmd1 := cmds[1].(map[string]any)
	assert.Equal(t, "clear", cmd1["name"])
	assert.Equal(t, "[args]", cmd1["inputHint"])

	assert.Equal(t, "done", events[1]["event"])
}

func TestAIChatStream_ModelListUpdateEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-model-list-update"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type: "model_list_update",
			ModelList: &ai.ModelListState{
				CurrentModelID: "claude-sonnet-4-20250514",
				Models: []model.AgentModel{
					{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", Default: true},
					{ID: "claude-opus-4-20250514", Name: "Claude Opus 4", Default: false},
				},
			},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "model_list_update", events[0]["event"])

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "claude-sonnet-4-20250514", data["currentModelId"])

	models, ok := data["models"].([]any)
	require.True(t, ok, "models should be a slice")
	assert.Len(t, models, 2)

	model0 := models[0].(map[string]any)
	assert.Equal(t, "claude-sonnet-4-20250514", model0["id"])
	assert.Equal(t, "Claude Sonnet 4", model0["name"])
	assert.Equal(t, true, model0["default"])

	assert.Equal(t, "done", events[1]["event"])
}

func TestAIChatStream_SessionCaptureEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-session-capture"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		// session_capture is NOT handled by the SSE switch — it is consumed
		// by the chat handler for external_session_id persistence but not
		// forwarded to the SSE client. So only "done" should appear.
		ch <- ai.StreamEvent{
			Type:    "session_capture",
			Content: "ext-sid-12345",
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	// session_capture has no SSE handler case — only done should be emitted
	assert.Len(t, events, 1)
	assert.Equal(t, "done", events[0]["event"])
}

func TestAIChatStream_ThinkingDoneEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-thinking-done"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{Type: "thinking_done"}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "thinking_done", events[0]["event"])
	assert.Equal(t, "{}", events[0]["data"])
	assert.Equal(t, "done", events[1]["event"])
}

func TestAIChatStream_ConfigUpdateEvent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID := "stream-config-update"
	ch := setupStreamSession(sessionID)
	defer cleanupStreamSession(sessionID)

	go func() {
		ch <- ai.StreamEvent{
			Type: "config_update",
			Config: &ai.ConfigOptionState{
				ConfigID:  "mode",
				CurrentID: "code",
				Options: []ai.ConfigOptionDef{
					{
						ID:       "mode",
						Name:     "Mode",
						Category: "mode",
						Values: []ai.ConfigOptionValue{
							{ID: "ask", Name: "Ask"},
							{ID: "code", Name: "Code"},
						},
					},
				},
			},
		}
		ch <- ai.StreamEvent{Type: "done"}
	}()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/stream?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChatStream, req)

	events := parseSSEEvents(w.Body.String())
	assert.Equal(t, "config_update", events[0]["event"])

	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0]["data"]), &data))
	assert.Equal(t, "mode", data["configId"])
	assert.Equal(t, "code", data["currentValueId"])

	options, ok := data["options"].([]any)
	require.True(t, ok, "options should be a slice")
	assert.Len(t, options, 1)

	opt0 := options[0].(map[string]any)
	assert.Equal(t, "mode", opt0["id"])
	assert.Equal(t, "Mode", opt0["name"])
	assert.Equal(t, "mode", opt0["category"])

	values, ok := opt0["values"].([]any)
	require.True(t, ok, "values should be a slice")
	assert.Len(t, values, 2)

	val0 := values[0].(map[string]any)
	assert.Equal(t, "ask", val0["id"])
	assert.Equal(t, "Ask", val0["name"])

	assert.Equal(t, "done", events[1]["event"])
}
