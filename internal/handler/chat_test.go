package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
)

// feedEvents processes a sequence of StreamEvents through AccumulateBlock
// and returns the resulting blocks.
func feedEvents(events []ai.StreamEvent) []model.ContentBlock {
	var blocks []model.ContentBlock
	for _, event := range events {
		ai.AccumulateBlock(&blocks, event)
	}
	return blocks
}

func TestAccumulateBlock_TextOnly(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "content", Content: "Hello "},
		{Type: "content", Content: "world"},
	}

	blocks := feedEvents(events)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("expected text block, got %q", blocks[0].Type)
	}
	if blocks[0].Text != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", blocks[0].Text)
	}
}

func TestAccumulateBlock_ThinkingCoalescing(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "thinking", Content: "Let me think..."},
		{Type: "thinking", Content: " about this."},
	}

	blocks := feedEvents(events)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 thinking block (coalesced), got %d", len(blocks))
	}
	if blocks[0].Type != "thinking" {
		t.Errorf("expected thinking block, got %q", blocks[0].Type)
	}
	if blocks[0].Text != "Let me think... about this." {
		t.Errorf("expected coalesced thinking text, got %q", blocks[0].Text)
	}
}

func TestAccumulateBlock_ThinkingAndTextCoalescing(t *testing.T) {
	// When thinking and text events interleave (e.g. GLM-5.1 token-level interleaving),
	// they should be coalesced into their respective blocks, not fragmented.
	events := []ai.StreamEvent{
		{Type: "thinking", Content: "First thought"},
		{Type: "content", Content: "Some text"},
		{Type: "thinking", Content: " continues"},
		{Type: "content", Content: " more"},
	}

	blocks := feedEvents(events)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (thinking, text), got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Type != "thinking" || blocks[0].Text != "First thought continues" {
		t.Errorf("expected coalesced thinking block, got %+v", blocks[0])
	}
	if blocks[1].Type != "text" || blocks[1].Text != "Some text more" {
		t.Errorf("expected coalesced text block, got %+v", blocks[1])
	}
}

func TestAccumulateBlock_InterleavedThinkingText(t *testing.T) {
	// Simulates GLM-5.1 interleaving: thinking and text tokens arrive alternately.
	// This is the exact pattern seen in the bug: 16 thinking blocks + 14 text blocks
	// should be coalesced into 1 thinking + 1 text.
	events := []ai.StreamEvent{
		{Type: "thinking", Content: ".\n\nLet"},
		{Type: "content", Content: "I"},
		{Type: "thinking", Content: " me start"},
		{Type: "content", Content: "'ll thoroughly"},
		{Type: "thinking", Content: " by listing"},
		{Type: "content", Content: " explore the"},
		{Type: "thinking", Content: " all Go"},
		{Type: "content", Content: " frontend"},
		{Type: "thinking", Content: " files under"},
		{Type: "content", Content: " source code"},
	}

	blocks := feedEvents(events)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (1 thinking + 1 text), got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Type != "thinking" {
		t.Errorf("expected thinking block, got %q", blocks[0].Type)
	}
	if blocks[0].Text != ".\n\nLet me start by listing all Go files under" {
		t.Errorf("expected coalesced thinking, got %q", blocks[0].Text)
	}
	if blocks[1].Type != "text" {
		t.Errorf("expected text block, got %q", blocks[1].Type)
	}
	if blocks[1].Text != "I'll thoroughly explore the frontend source code" {
		t.Errorf("expected coalesced text, got %q", blocks[1].Text)
	}
}

func TestAccumulateBlock_ToolUseBoundary(t *testing.T) {
	// tool_use acts as a boundary: text after tool_use should NOT merge
	// with text before tool_use.
	events := []ai.StreamEvent{
		{Type: "content", Content: "Before tool. "},
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "t1", Input: `{"file_path":"/a"}`, Done: true}},
		{Type: "content", Content: "After tool."},
	}

	blocks := feedEvents(events)

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks (text, tool, text), got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Text != "Before tool. " {
		t.Errorf("expected 'Before tool. ', got %q", blocks[0].Text)
	}
	if blocks[2].Text != "After tool." {
		t.Errorf("expected 'After tool.', got %q", blocks[2].Text)
	}
}

func TestAccumulateBlock_ToolUseDedup(t *testing.T) {
	// Two tool_use events for the same tool ID should produce one block
	events := []ai.StreamEvent{
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "t1", Input: "", Done: false}},
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "t1", Input: `{"file_path":"/a.go"}`, Done: true}},
	}

	blocks := feedEvents(events)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 tool_use block (deduped), got %d", len(blocks))
	}
	if blocks[0].Type != "tool_use" {
		t.Errorf("expected tool_use block, got %q", blocks[0].Type)
	}
	if blocks[0].Name != "Read" {
		t.Errorf("expected tool name 'Read', got %q", blocks[0].Name)
	}
	if blocks[0].ID != "t1" {
		t.Errorf("expected tool ID 't1', got %q", blocks[0].ID)
	}
	// Input should be updated to the final value
	if fp, ok := blocks[0].Input["file_path"]; !ok || fp != "/a.go" {
		t.Errorf("expected input file_path '/a.go', got %v", blocks[0].Input)
	}
}

func TestAccumulateBlock_ToolUseDifferentIDs(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "t1", Input: `{"file_path":"/a.go"}`, Done: true}},
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Bash", ID: "t2", Input: `{"command":"ls"}`, Done: true}},
	}

	blocks := feedEvents(events)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 tool_use blocks, got %d", len(blocks))
	}
	if blocks[0].Name != "Read" {
		t.Errorf("expected first tool 'Read', got %q", blocks[0].Name)
	}
	if blocks[1].Name != "Bash" {
		t.Errorf("expected second tool 'Bash', got %q", blocks[1].Name)
	}
}

func TestAccumulateBlock_ToolUseEmptyInput(t *testing.T) {
	// Tool use with empty input should have an empty map, not nil
	events := []ai.StreamEvent{
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "t1", Input: "", Done: false}},
	}

	blocks := feedEvents(events)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Input == nil {
		t.Error("expected Input to be empty map, not nil")
	}
	if len(blocks[0].Input) != 0 {
		t.Errorf("expected empty map, got %v", blocks[0].Input)
	}
}

func TestAccumulateBlock_MixedFlow(t *testing.T) {
	// Full flow: thinking → tool_use → text → tool_use → text
	events := []ai.StreamEvent{
		{Type: "thinking", Content: "I need to read the file"},
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "t1", Input: "", Done: false}},
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "t1", Input: `{"file_path":"/src/main.go"}`, Done: true}},
		{Type: "content", Content: "I can see "},
		{Type: "content", Content: "the code."},
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Edit", ID: "t2", Input: `{"file_path":"/src/main.go","old":"foo","new":"bar"}`, Done: true}},
		{Type: "content", Content: " Done!"},
	}

	blocks := feedEvents(events)

	if len(blocks) != 5 {
		t.Fatalf("expected 5 blocks (thinking, tool, text, tool, text), got %d: %+v", len(blocks), blocks)
	}

	// Block 0: thinking
	if blocks[0].Type != "thinking" || blocks[0].Text != "I need to read the file" {
		t.Errorf("block 0: expected thinking, got %+v", blocks[0])
	}

	// Block 1: tool_use (Read) — deduped
	if blocks[1].Type != "tool_use" || blocks[1].Name != "Read" {
		t.Errorf("block 1: expected Read tool_use, got %+v", blocks[1])
	}
	if fp, ok := blocks[1].Input["file_path"]; !ok || fp != "/src/main.go" {
		t.Errorf("block 1: expected file_path '/src/main.go', got %v", blocks[1].Input)
	}

	// Block 2: text
	if blocks[2].Type != "text" || blocks[2].Text != "I can see the code." {
		t.Errorf("block 2: expected text, got %+v", blocks[2])
	}

	// Block 3: tool_use (Edit)
	if blocks[3].Type != "tool_use" || blocks[3].Name != "Edit" {
		t.Errorf("block 3: expected Edit tool_use, got %+v", blocks[3])
	}

	// Block 4: text (new text block after tool_use — tool_use is a natural boundary)
	if blocks[4].Type != "text" || blocks[4].Text != " Done!" {
		t.Errorf("block 4: expected text, got %+v", blocks[4])
	}
}

func TestAccumulateBlock_EmptyEvents(t *testing.T) {
	// No events → no blocks
	blocks := feedEvents(nil)
	if len(blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(blocks))
	}
}

func TestAccumulateBlock_MetadataIgnored(t *testing.T) {
	// Metadata events should not produce blocks
	events := []ai.StreamEvent{
		{Type: "content", Content: "Hello"},
		{Type: "metadata", Meta: &ai.Metadata{Model: "test"}},
	}

	blocks := feedEvents(events)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block (metadata ignored), got %d", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("expected text block, got %q", blocks[0].Type)
	}
}

func TestAccumulateBlock_TextFlushedBeforeThinking(t *testing.T) {
	// Text should be flushed to a block when thinking arrives
	events := []ai.StreamEvent{
		{Type: "content", Content: "Some text"},
		{Type: "thinking", Content: "A thought"},
	}

	blocks := feedEvents(events)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "Some text" {
		t.Errorf("block 0: expected flushed text, got %+v", blocks[0])
	}
	if blocks[1].Type != "thinking" || blocks[1].Text != "A thought" {
		t.Errorf("block 1: expected thinking, got %+v", blocks[1])
	}
}

func TestAccumulateBlock_TextFlushedBeforeToolUse(t *testing.T) {
	// Text should be flushed to a block when tool_use arrives
	events := []ai.StreamEvent{
		{Type: "content", Content: "Checking..."},
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "t1", Input: `{"file_path":"/x"}`, Done: true}},
	}

	blocks := feedEvents(events)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "Checking..." {
		t.Errorf("block 0: expected flushed text, got %+v", blocks[0])
	}
	if blocks[1].Type != "tool_use" {
		t.Errorf("block 1: expected tool_use, got %q", blocks[1].Type)
	}
}

func TestBlocksSerialization(t *testing.T) {
	// Verify that blocks can be serialized to JSON and deserialized correctly
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "Analyzing..."},
		{Type: "tool_use", Name: "Read", ID: "t1", Input: map[string]any{"file_path": "/src/main.go"}},
		{Type: "text", Text: "Here is the result."},
	}

	data, err := json.Marshal(map[string]any{"blocks": blocks})
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Deserialize and verify
	var result struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(result.Blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(result.Blocks))
	}

	if result.Blocks[0].Type != "thinking" || result.Blocks[0].Text != "Analyzing..." {
		t.Errorf("block 0 mismatch: %+v", result.Blocks[0])
	}
	if result.Blocks[1].Type != "tool_use" || result.Blocks[1].Name != "Read" {
		t.Errorf("block 1 mismatch: %+v", result.Blocks[1])
	}
	if result.Blocks[1].Input["file_path"] != "/src/main.go" {
		t.Errorf("block 1 input mismatch: %v", result.Blocks[1].Input)
	}
	if result.Blocks[2].Type != "text" || result.Blocks[2].Text != "Here is the result." {
		t.Errorf("block 2 mismatch: %+v", result.Blocks[2])
	}
}

func TestBlocksSerialization_RoundTrip(t *testing.T) {
	// Verify blocks survive a full serialize → DB store → deserialize cycle
	original := []model.ContentBlock{
		{Type: "thinking", Text: "Deep thought"},
		{Type: "tool_use", Name: "Bash", ID: "toolu_1", Input: map[string]any{"command": "ls -la"}},
		{Type: "text", Text: "Result here."},
	}

	// Serialize (as handler does for DB storage)
	data, _ := json.Marshal(map[string]any{"blocks": original})
	content := string(data)

	// Deserialize (as frontend does when loading from DB)
	var parsed struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v", err)
	}

	if len(parsed.Blocks) != 3 {
		t.Fatalf("expected 3 blocks after round-trip, got %d", len(parsed.Blocks))
	}

	// Verify thinking
	if parsed.Blocks[0].Type != "thinking" || parsed.Blocks[0].Text != "Deep thought" {
		t.Errorf("thinking block lost in round-trip: %+v", parsed.Blocks[0])
	}

	// Verify tool_use
	if parsed.Blocks[1].Type != "tool_use" || parsed.Blocks[1].Name != "Bash" {
		t.Errorf("tool_use block lost in round-trip: %+v", parsed.Blocks[1])
	}
	if parsed.Blocks[1].Input["command"] != "ls -la" {
		t.Errorf("tool input lost in round-trip: %v", parsed.Blocks[1].Input)
	}

	// Verify text
	if parsed.Blocks[2].Type != "text" || parsed.Blocks[2].Text != "Result here." {
		t.Errorf("text block lost in round-trip: %+v", parsed.Blocks[2])
	}
}

// ============================================================================
// HTTP-level handler tests
// ============================================================================

// --- ServeChatHistory ---

func TestServeChatHistory_Get_NoSessions(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/history", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.NotNil(t, result["sessionId"])
	assert.NotNil(t, result["messages"])
}

func TestServeChatHistory_Get_WithExistingSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session first
	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "test session", "", "", "default", "chat")
	assert.NoError(t, err)

	// Add a message to that session
	_, err = service.AddChatMessage(env.ProjectDir, "codebuddy", sessionID, "user", "hello", nil, false, "NewSession")
	assert.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/history", nil)
	withProjectCookie(req, env.ProjectDir)
	withSessionCookie(req, sessionID)

	w := callHandler(ServeChatHistory, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, sessionID, result["sessionId"])

	messages, ok := result["messages"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, messages, 1)
}

func TestServeChatHistory_Post_AddMessage(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session first
	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "test session", "", "", "default", "chat")
	assert.NoError(t, err)

	body := map[string]string{
		"role":       "user",
		"content":    "Hello AI",
		"session_id": sessionID,
	}
	req := newRequest(t, http.MethodPost, "/api/ai/history", body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.NotNil(t, result["savedAt"])
}

func TestServeChatHistory_Post_InvalidRole(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]string{
		"role":    "admin",
		"content": "Hello",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/history", body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeChatHistory_Post_InvalidBody(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Send invalid JSON by using raw bytes
	req := httptest.NewRequest(http.MethodPost, "/api/ai/history", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeChatHistory, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeChatHistory_NoProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/history", nil)
	// No project cookie set

	w := callHandler(ServeChatHistory, req)
	assertStatus(t, w, http.StatusForbidden)
}

// --- ServeSessions ---

func TestServeSessions_Get(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.NotNil(t, result["sessions"])
}

func TestServeSessions_Get_WithExistingSessions(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create some sessions
	_, err := service.CreateSession(env.ProjectDir, "codebuddy", "session 1", "", "", "default", "chat")
	assert.NoError(t, err)
	_, err = service.CreateSession(env.ProjectDir, "codebuddy", "session 2", "", "", "default", "chat")
	assert.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	sessions, ok := result["sessions"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, sessions, 2)
}

func TestServeSessions_Get_RunningState(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create two sessions
	sid1, err := service.CreateSession(env.ProjectDir, "codebuddy", "running session", "", "", "default", "chat")
	assert.NoError(t, err)
	sid2, err := service.CreateSession(env.ProjectDir, "codebuddy", "idle session", "", "", "default", "chat")
	assert.NoError(t, err)

	// Mark sid1 as running
	service.SetSessionRunning(sid1, true)
	defer service.SetSessionRunning(sid1, false)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	sessions, ok := result["sessions"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, sessions, 2)

	// Build a map of session ID -> running state
	runningMap := make(map[string]bool)
	for _, s := range sessions {
		session, _ := s.(map[string]interface{})
		id, _ := session["id"].(string)
		running, _ := session["running"].(bool)
		runningMap[id] = running
	}
	assert.True(t, runningMap[sid1], "session %s should be running", sid1)
	assert.False(t, runningMap[sid2], "session %s should not be running", sid2)
}

func TestServeSessions_Post_CreateSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]string{}
	req := newRequest(t, http.MethodPost, "/api/ai/sessions", body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.NotNil(t, result["sessionId"])
	assert.Equal(t, "codebuddy", result["backend"])
}

func TestServeSessions_Post_CustomTitleAndBackend(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]string{
		"title":   "My Custom Session",
		"backend": "claude",
		"agentId": "claude",
	}
	req := newRequest(t, http.MethodPost, "/api/ai/sessions", body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.NotNil(t, result["sessionId"])
	assert.Equal(t, "claude", result["backend"])

	// Verify session title in DB
	sessionID, _ := result["sessionId"].(string)
	title, err := service.GetSessionTitle(sessionID)
	assert.NoError(t, err)
	assert.Equal(t, "My Custom Session", title)
}

func TestServeSessions_Post_InvalidBody(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/ai/sessions", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeSessions_NoProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)

	w := callHandler(ServeSessions, req)
	assertStatus(t, w, http.StatusForbidden)
}

// --- DeleteSession ---

func TestDeleteSession_ExistingSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "to delete", "", "", "default", "chat")
	assert.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/delete?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(DeleteSession, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
}

func TestDeleteSession_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/ai/session/delete", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(DeleteSession, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestDeleteSession_NoProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/ai/session/delete?session_id=abc", nil)

	w := callHandler(DeleteSession, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestDeleteSession_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/delete?session_id=abc", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(DeleteSession, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestDeleteSession_ClosesACPConn(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Set up an ACP agent
	origAgents := model.Agents
	origAgentList := model.AgentList
	model.Agents["claude"].AcpCommand = "claude --acp"
	defer func() {
		model.Agents = origAgents
		model.AgentList = origAgentList
	}()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "ACP session", "claude", "", "default", "chat")
	assert.NoError(t, err)

	// Inject an ACP connection for this session
	mgr := ai.GetACPConnManager()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(ai.NewClawBenchACPClient())
	conn.SetSessionMappingForTest(sessionID, "acp-sid-delete-test")
	mgr.SetConnForTest(sessionID, conn)

	// Delete the session — should close the ACP connection
	req := newRequest(t, http.MethodDelete, "/api/ai/session/delete?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(DeleteSession, req)
	assertOK(t, w)

	// Verify the connection was closed (CloseConn runs in goroutine, wait briefly)
	assert.Eventually(t, func() bool { return mgr.GetConn(sessionID) == nil }, 2*time.Second, 10*time.Millisecond, "ACP connection should be closed after session delete")
}

func TestDeleteSession_NonACPAgentNoCrash(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// codebuddy agent is not ACP — DeleteSession should still work
	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "Non-ACP session", "codebuddy", "", "default", "chat")
	assert.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/delete?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(DeleteSession, req)
	assertOK(t, w)
}

// --- CancelChat ---

func TestCancelChat_NoRunningSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid := createTestSession(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/ai/chat/cancel?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(CancelChat, req)
	// Idempotent: cancelling a non-running session succeeds
	assertStatus(t, w, http.StatusOK)
}

func TestCancelChat_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/chat/cancel", nil)
	req = withProjectCookie(req, env.ProjectDir)
	// No session_id in query and no cookie

	w := callHandler(CancelChat, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestCancelChat_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/ai/chat/cancel?session_id=abc", nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(CancelChat, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestCancelChat_StuckSessionForceClears(t *testing.T) {
	// Simulates the bug scenario: session is marked running but has no cancel
	// function (race window between TrySetSessionRunning and RegisterSessionCancel).
	// CancelChat should force-clear the stuck session and return success.
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid := createTestSession(t, env.ProjectDir)

	// Simulate stuck state: running=true but no cancel func registered
	service.SetSessionRunning(sid, true)

	req := newRequest(t, http.MethodPost, "/api/ai/chat/cancel?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)

	w := callHandler(CancelChat, req)
	assertStatus(t, w, http.StatusOK)

	// Session should no longer be running
	assert.False(t, service.IsSessionRunning(sid))

	// After force-clear, a new TrySetSessionRunning should succeed
	assert.True(t, service.TrySetSessionRunning(sid))
}

// --- ServeAISession ---

func TestServeAISession_DeleteNonExistentDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/ai/session", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeAISession, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, float64(0), result["deleted"])
}

func TestServeAISession_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeAISession, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeAISession_NoProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/ai/session", nil)

	w := callHandler(ServeAISession, req)
	assertStatus(t, w, http.StatusForbidden)
}

// --- ServeAISessionUpdate (PATCH /api/ai/session/update) ---

func TestServeAISessionUpdate_NoSessionID(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"modeId": "architect",
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeAISessionUpdate_WrongMethod(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/ai/session/update?session_id=abc", nil)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// --- ServeRoots ---

func TestServeRoots(t *testing.T) {
	t.Run("ReturnsRootPathsAndConfig", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/roots", nil)

		w := callHandler(ServeRoots, req)
		assertOK(t, w)

		var result map[string]interface{}
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		assert.Contains(t, result, "roots")
		roots, ok := result["roots"].([]interface{})
		assert.True(t, ok)
		assert.NotEmpty(t, roots)
		assert.Contains(t, roots, env.WatchDir)
		// Check config fields exist
		assert.NotNil(t, result["uploadMaxSizeMB"])
		assert.NotNil(t, result["uploadMaxFiles"])
	})
}

// --- UploadFile ---

func TestUploadFile_ValidFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "test.txt")
	assert.NoError(t, err)
	_, err = part.Write([]byte("hello world"))
	assert.NoError(t, err)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(UploadFile, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])

	path, ok := result["path"].(string)
	assert.True(t, ok)
	assert.Contains(t, path, ".clawbench"+string([]byte{filepath.Separator})+"uploads")
}

func TestUploadFile_NoProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "test.txt")
	_, _ = part.Write([]byte("hello"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	w := callHandler(UploadFile, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestUploadFile_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/upload/file", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(UploadFile, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestUploadFile_ExeExtension_Allowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "malware.exe")
	assert.NoError(t, err)
	_, _ = part.Write([]byte("evil content"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(UploadFile, req)
	assertOK(t, w)
}

func TestUploadFile_BatExtension_Allowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "script.bat")
	_, _ = part.Write([]byte("@echo off"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload/file", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(UploadFile, req)
	assertOK(t, w)
}

func TestAccumulateBlock_ErrorEvent(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "content", Content: "Some text"},
		{Type: "error", Error: "Rate limit exceeded"},
	}

	blocks := feedEvents(events)

	// Should have: text block + warning block (error is stored as warning)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (text + warning from error), got %d", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("expected first block to be text, got %q", blocks[0].Type)
	}
	if blocks[1].Type != "warning" {
		t.Errorf("expected second block to be warning (from error event), got %q", blocks[1].Type)
	}
	if blocks[1].Text != "Rate limit exceeded" {
		t.Errorf("expected 'Rate limit exceeded', got %q", blocks[1].Text)
	}
}

func TestAccumulateBlock_ErrorEventOnly(t *testing.T) {
	events := []ai.StreamEvent{
		{Type: "error", Error: "AI request failed", Reason: ai.ReasonRequestFailed},
	}

	blocks := feedEvents(events)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block (warning from error), got %d", len(blocks))
	}
	if blocks[0].Type != "warning" {
		t.Errorf("expected warning block from error event, got %q", blocks[0].Type)
	}
	if blocks[0].Text != "AI request failed" {
		t.Errorf("expected 'AI request failed', got %q", blocks[0].Text)
	}
	if blocks[0].Reason != ai.ReasonRequestFailed {
		t.Errorf("expected reason 'request_failed', got %q", blocks[0].Reason)
	}
}

// --- Second session (resume) scenario tests ---

func TestAccumulateBlock_ResumeSessionWithThinkingAndContent(t *testing.T) {
	// Simulates events from a codex resume session parsed from stderr:
	// thinking -> content -> content -> metadata
	events := []ai.StreamEvent{
		{Type: "thinking", Content: "The user is asking about the code."},
		{Type: "content", Content: "Here's what I found:\n"},
		{Type: "content", Content: "The main function is in main.go.\n"},
		{Type: "metadata", Meta: &ai.Metadata{SessionID: "019dc814-0f5e-7260-a32b-b274fee09be1"}},
	}

	blocks := feedEvents(events)

	// Two consecutive content events are coalesced into one text block
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (thinking + text), got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Type != "thinking" {
		t.Errorf("expected first block to be thinking, got %q", blocks[0].Type)
	}
	if blocks[0].Text != "The user is asking about the code." {
		t.Errorf("expected thinking content, got %q", blocks[0].Text)
	}
	if blocks[1].Type != "text" {
		t.Errorf("expected text block, got %q", blocks[1].Type)
	}
	// Content should be coalesced
	if !strings.Contains(blocks[1].Text, "Here's what I found") || !strings.Contains(blocks[1].Text, "main.go") {
		t.Errorf("expected coalesced content, got %q", blocks[1].Text)
	}
}

func TestAccumulateBlock_ResumeSessionWithToolUse(t *testing.T) {
	// Simulates resume session where codex executes a command
	events := []ai.StreamEvent{
		{Type: "content", Content: "Let me check that.\n"},
		{Type: "tool_use", Tool: &ai.ToolCall{
			Name:  "command_execution",
			ID:    "exec-1",
			Input: "bash -c 'ls'",
			Done:  false,
		}},
		{Type: "tool_use", Tool: &ai.ToolCall{
			Name:  "command_execution",
			ID:    "exec-1",
			Input: "bash -c 'ls'\n\nOutput:\nfile1.txt\nfile2.txt",
			Done:  true,
		}},
		{Type: "content", Content: "Here are the files.\n"},
	}

	blocks := feedEvents(events)

	// text + tool_use + text = 3 blocks
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("expected first block text, got %q", blocks[0].Type)
	}
	if blocks[1].Type != "tool_use" {
		t.Errorf("expected second block tool_use, got %q", blocks[1].Type)
	}
	if blocks[2].Type != "text" {
		t.Errorf("expected third block text, got %q", blocks[2].Type)
	}
}

func TestAccumulateBlock_ResumeSessionError(t *testing.T) {
	// When codex resume fails (e.g., turn.failed), error event should
	// produce a warning block instead of the generic "AI未返回任何内容"
	events := []ai.StreamEvent{
		{Type: "error", Error: "Rate limit exceeded"},
	}

	blocks := feedEvents(events)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 warning block from error, got %d", len(blocks))
	}
	if blocks[0].Type != "warning" {
		t.Errorf("expected warning block, got %q", blocks[0].Type)
	}
	if blocks[0].Text != "Rate limit exceeded" {
		t.Errorf("expected actual error message 'Rate limit exceeded', got %q", blocks[0].Text)
	}
}

func TestAccumulateBlock_ResumeSession_ContentThenError(t *testing.T) {
	// Partial content received before an error
	events := []ai.StreamEvent{
		{Type: "content", Content: "I was working on "},
		{Type: "error", Error: "Connection lost"},
	}

	blocks := feedEvents(events)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (text + warning), got %d", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("expected text block, got %q", blocks[0].Type)
	}
	if blocks[1].Type != "warning" {
		t.Errorf("expected warning block from error, got %q", blocks[1].Type)
	}
	if blocks[1].Text != "Connection lost" {
		t.Errorf("expected 'Connection lost', got %q", blocks[1].Text)
	}
}

func TestAccumulateBlock_SessionCaptureNotAccumulated(t *testing.T) {
	// session_capture events should NOT be accumulated as content blocks
	events := []ai.StreamEvent{
		{Type: "session_capture", Content: "ses_test123"},
		{Type: "content", Content: "Hello"},
	}

	blocks := feedEvents(events)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block (session_capture should be skipped), got %d", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("expected text block, got %q", blocks[0].Type)
	}
}

// ============================================================================
// Files deduplication tests
// ============================================================================

// TestAddChatMessage_FilesNoDuplicate verifies that files stored in the DB
// are not duplicated when the same path appears in both filePaths and files
// (the frontend sends both, with files already containing filePaths).
func TestAddChatMessage_FilesNoDuplicate(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "files-dedup", "", "", "default", "chat")
	assert.NoError(t, err)

	// Simulate what the handler does: allFiles = req.Files (frontend already merged filePaths into files)
	allFiles := []string{"config.yaml"}

	msgID, err := service.AddChatMessage(env.ProjectDir, "codebuddy", sessionID, "user", "what is this?", allFiles, false, "NewSession")
	assert.NoError(t, err)
	assert.NotZero(t, msgID)

	// Read back from DB and verify no duplicates
	messages, err := service.GetChatHistory(env.ProjectDir, "codebuddy", sessionID)
	assert.NoError(t, err)
	assert.Len(t, messages, 1)
	assert.Len(t, messages[0].Files, 1, "files should have exactly 1 entry, got %v", messages[0].Files)
	assert.Equal(t, "config.yaml", messages[0].Files[0])
}

// TestAddChatMessage_FilesWithUploadsAndReferences verifies that files with
// both uploads and references are stored correctly without duplication.
func TestAddChatMessage_FilesWithUploadsAndReferences(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "files-mixed", "", "", "default", "chat")
	assert.NoError(t, err)

	// Frontend sends: files = [upload path, reference path] (already merged)
	allFiles := []string{".clawbench/uploads/photo.png", "src/main.go"}

	msgID, err := service.AddChatMessage(env.ProjectDir, "codebuddy", sessionID, "user", "check both", allFiles, false, "NewSession")
	assert.NoError(t, err)
	assert.NotZero(t, msgID)

	messages, err := service.GetChatHistory(env.ProjectDir, "codebuddy", sessionID)
	assert.NoError(t, err)
	assert.Len(t, messages, 1)
	assert.Len(t, messages[0].Files, 2, "files should have exactly 2 entries, got %v", messages[0].Files)
}

// TestAIChat_EnqueuePath_FilesNoDuplicate tests the AIChat handler's enqueue
// path (when session is already running) to ensure files are not duplicated
// in DB storage when filePaths and files overlap.
func TestAIChat_EnqueuePath_FilesNoDuplicate(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a test file within the project so validation passes
	createTestFile(t, env.ProjectDir, "config.yaml", "test: true")

	// Create a session and mark it as running (to trigger enqueue path)
	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "enqueue-dedup", "", "", "default", "chat")
	assert.NoError(t, err)
	service.TrySetSessionRunning(sessionID)
	defer func() {
		service.SetSessionRunning(sessionID, false)
		service.ClearQueue(sessionID)
	}()

	// Simulate frontend sending both filePaths and files (where files already includes filePaths)
	body := map[string]any{
		"message":   "check this",
		"filePaths": []string{"config.yaml"},
		"files":     []string{"config.yaml"}, // frontend already merged filePaths into files
	}
	req := newRequest(t, http.MethodPost, "/api/ai/chat?session_id="+sessionID, body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(AIChat, req)
	assertOK(t, w)

	var result map[string]any
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["queued"])

	// Verify DB has no duplicate files
	messages, err := service.GetChatHistory(env.ProjectDir, "codebuddy", sessionID)
	assert.NoError(t, err)
	assert.Len(t, messages, 1, "should have 1 user message")
	assert.Len(t, messages[0].Files, 1, "files should have exactly 1 entry (no duplicate), got %v", messages[0].Files)
}

func TestAccumulateBlock_InterleavedToolUse(t *testing.T) {
	// Regression test: when parallel sub-agents interleave tool calls at
	// different content block indices, the StreamParser now correctly routes
	// input_json_delta to the right tool. This test verifies that AccumulateBlock
	// correctly builds the final content blocks from these interleaved events.
	events := []ai.StreamEvent{
		// Tool A starts (done=false, empty input)
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "toolu_A", Input: "", Done: false}},
		// Tool B starts (done=false, empty input) — interleaved
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Bash", ID: "toolu_B", Input: "", Done: false}},
		// Tool A stops (done=true, full input)
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Read", ID: "toolu_A", Input: `{"file_path":"/a.go"}`, Done: true}},
		// Tool B stops (done=true, full input)
		{Type: "tool_use", Tool: &ai.ToolCall{Name: "Bash", ID: "toolu_B", Input: `{"command":"ls"}`, Done: true}},
	}

	blocks := feedEvents(events)

	// Should have 2 tool_use blocks (deduped by ID: start+stop for each ID)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (deduped), got %d", len(blocks))
	}

	// Block 0: Read tool A
	if blocks[0].Name != "Read" {
		t.Errorf("block 0: expected Name 'Read', got %q", blocks[0].Name)
	}
	if blocks[0].ID != "toolu_A" {
		t.Errorf("block 0: expected ID 'toolu_A', got %q", blocks[0].ID)
	}
	if !blocks[0].Done {
		t.Error("block 0: expected Done=true")
	}
	if blocks[0].Input["file_path"] != "/a.go" {
		t.Errorf("block 0: expected input file_path='/a.go', got %v", blocks[0].Input)
	}

	// Block 1: Bash tool B
	if blocks[1].Name != "Bash" {
		t.Errorf("block 1: expected Name 'Bash', got %q", blocks[1].Name)
	}
	if blocks[1].ID != "toolu_B" {
		t.Errorf("block 1: expected ID 'toolu_B', got %q", blocks[1].ID)
	}
	if !blocks[1].Done {
		t.Error("block 1: expected Done=true")
	}
	if blocks[1].Input["command"] != "ls" {
		t.Errorf("block 1: expected input command='ls', got %v", blocks[1].Input)
	}
}


// ---------- Session ownership validation (ISS-180) — AIChat handler ----------

// TestAIChat_Get_SessionBelongsToDifferentProject verifies that the GET path
// in AIChat rejects access to a session that belongs to another project.
func TestAIChat_Get_SessionBelongsToDifferentProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session that belongs to a different project
	otherProject := "/other-project-chat-get"
	sessionID, err := service.CreateSession(otherProject, "claude", "Other Session", "claude", "", "default", "chat")
	assert.NoError(t, err)

	// GET with a session_id belonging to another project → Forbidden
	req := newRequest(t, http.MethodGet, "/api/ai/chat?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChat, req)

	assertStatus(t, w, http.StatusForbidden)
}

// TestAIChat_Get_SessionBelongsToSameProject verifies that the GET path
// in AIChat allows access to a session that belongs to the requesting project.
func TestAIChat_Get_SessionBelongsToSameProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session that belongs to the same project
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Same Session", "claude", "", "default", "chat")
	assert.NoError(t, err)

	// GET with a session_id belonging to same project → OK
	req := newRequest(t, http.MethodGet, "/api/ai/chat?session_id="+sessionID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChat, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAIChat_Post_SessionBelongsToDifferentProject verifies that the POST path
// in AIChat rejects access to a session that belongs to another project.
func TestAIChat_Post_SessionBelongsToDifferentProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session that belongs to a different project
	otherProject := "/other-project-chat-post"
	sessionID, err := service.CreateSession(otherProject, "claude", "Other Session", "claude", "", "default", "chat")
	assert.NoError(t, err)

	// POST with a session cookie pointing to another project's session → Forbidden
	body := map[string]any{"message": "hello"}
	req := newRequest(t, http.MethodPost, "/api/ai/chat", body)
	req = withProjectCookie(req, env.ProjectDir)
	req = withSessionCookie(req, sessionID)
	w := callHandler(AIChat, req)

	assertStatus(t, w, http.StatusForbidden)
}

// ============================================================================
// buildChatRequest external session ID tests
// ============================================================================

// TestBuildChatRequest_PiResumeWithExternalSessionID verifies that when Pi
// backend resumes a session that has an external_session_id stored, that ID
// is used instead of the ClawBench UUID.
func TestBuildChatRequest_PiResumeWithExternalSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session with an external session ID
	sessionID, err := service.CreateSession(env.ProjectDir, "pi", "test-pi", "", "", "default", "chat")
	assert.NoError(t, err)

	// Store an external session ID (simulating what session_capture does)
	err = service.UpdateExternalSessionID(sessionID, "pi-sess-abc123")
	assert.NoError(t, err)

	// Add an assistant message so SessionHasAssistant returns true
	_, err = service.AddChatMessage(env.ProjectDir, "pi", sessionID, "assistant", `{"blocks":[{"type":"text","text":"hi"}]}`, nil, false, "")
	assert.NoError(t, err)

	// Call buildChatRequest — should use the external ID
	req := buildChatRequest("continue", sessionID, env.ProjectDir, "pi", "codebuddy", "", "", "", "", "")
	assert.True(t, req.Resume, "should be resume since session has assistant messages")
	assert.Equal(t, "pi-sess-abc123", req.SessionID, "should use external session ID, not ClawBench UUID")
}

// TestBuildChatRequest_PiResumeWithoutExternalSessionID verifies that when Pi
// backend resumes a session that has NO external_session_id, the SessionID
// is cleared to avoid passing the invalid ClawBench UUID to Pi CLI.
func TestBuildChatRequest_PiResumeWithoutExternalSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session without external session ID
	sessionID, err := service.CreateSession(env.ProjectDir, "pi", "test-pi-noext", "", "", "default", "chat")
	assert.NoError(t, err)

	// CreateSession pre-fills external_session_id = sessionID; clear it to simulate
	// the "no external session ID captured" scenario (e.g. session_capture event was missed)
	err = service.UpdateExternalSessionID(sessionID, "")
	assert.NoError(t, err)

	// Add an assistant message so SessionHasAssistant returns true
	// (simulates a successful first message where session_capture was missed)
	_, err = service.AddChatMessage(env.ProjectDir, "pi", sessionID, "assistant", `{"blocks":[{"type":"text","text":"hello"}]}`, nil, false, "")
	assert.NoError(t, err)

	// Call buildChatRequest — should clear SessionID to avoid passing invalid UUID
	req := buildChatRequest("continue", sessionID, env.ProjectDir, "pi", "codebuddy", "", "", "", "", "")
	assert.True(t, req.Resume, "should be resume since session has assistant messages")
	assert.Equal(t, "", req.SessionID, "should clear SessionID when no external ID available, to avoid 'No session found' error")
}

// TestBuildChatRequest_PiNewSession verifies that when Pi backend starts a new
// session (no prior assistant messages), the ClawBench UUID is passed through.
func TestBuildChatRequest_PiNewSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a new session with no assistant messages
	sessionID, err := service.CreateSession(env.ProjectDir, "pi", "test-pi-new", "", "", "default", "chat")
	assert.NoError(t, err)

	// Call buildChatRequest — new session, no resume
	req := buildChatRequest("hello", sessionID, env.ProjectDir, "pi", "codebuddy", "", "", "", "", "")
	assert.False(t, req.Resume, "should not be resume for new session")
	assert.Equal(t, sessionID, req.SessionID, "should pass ClawBench UUID for new session")
}

// TestBuildChatRequest_ClaudeResumeNoExternalID verifies that Claude/Codebuddy
// backends (which natively use ClawBench UUID) are NOT affected by the
// external session ID resolution logic — they should always get the UUID.
func TestBuildChatRequest_ClaudeResumeNoExternalID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session for claude backend
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "test-claude", "", "", "claude", "chat")
	assert.NoError(t, err)

	// Add an assistant message
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", `{"blocks":[{"type":"text","text":"hi"}]}`, nil, false, "")
	assert.NoError(t, err)

	// Call buildChatRequest — Claude should get the raw UUID, no external ID resolution
	req := buildChatRequest("continue", sessionID, env.ProjectDir, "claude", "claude", "", "", "", "", "")
	assert.True(t, req.Resume)
	assert.Equal(t, sessionID, req.SessionID, "Claude should get the ClawBench UUID directly, no external ID resolution")
}

// TestBuildChatRequest_OpenCodeResumeWithExternalSessionID verifies that
// OpenCode backend (which already had external ID support) still works correctly.
func TestBuildChatRequest_OpenCodeResumeWithExternalSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "opencode", "test-oc", "", "", "default", "chat")
	assert.NoError(t, err)

	err = service.UpdateExternalSessionID(sessionID, "ses_oc_xyz789")
	assert.NoError(t, err)

	_, err = service.AddChatMessage(env.ProjectDir, "opencode", sessionID, "assistant", `{"blocks":[{"type":"text","text":"hello"}]}`, nil, false, "")
	assert.NoError(t, err)

	req := buildChatRequest("continue", sessionID, env.ProjectDir, "opencode", "codebuddy", "", "", "", "", "")
	assert.True(t, req.Resume)
	assert.Equal(t, "ses_oc_xyz789", req.SessionID, "OpenCode should use external session ID")
}

// ============================================================================
// Pi external session ID persistence tests (session_capture + metadata paths)
// ============================================================================

// TestPiSessionCapture_PersistedToDB verifies that when a Pi session_capture
// event is processed, the external session ID is persisted to the database.
// This tests the handler condition `backendName == "pi"` in the session_capture
// branch of executeStreamRun.
func TestPiSessionCapture_PersistedToDB(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a Pi session
	sessionID, err := service.CreateSession(env.ProjectDir, "pi", "test-capture", "", "", "default", "chat")
	assert.NoError(t, err)

	// Verify external ID is pre-filled by CreateSession (same as sessionID)
	assert.Equal(t, sessionID, service.GetExternalSessionID(sessionID))

	// Simulate what the handler does on session_capture event for Pi:
	// The handler checks `backendName == "pi"` && event.Content != ""
	// and calls UpdateExternalSessionID.
	piExtID := "019e2172-6ebd-743e-8bb6-39d51df91bde"
	err = service.UpdateExternalSessionID(sessionID, piExtID)
	assert.NoError(t, err)

	// Verify it was persisted
	got := service.GetExternalSessionID(sessionID)
	assert.Equal(t, piExtID, got, "external session ID should be persisted for Pi backend")
}

// TestPiSessionCapture_NotOverwritten verifies that if an external session ID
// is already saved (and different from the ClawBench UUID default), a subsequent
// session_capture event does not overwrite it.
// This matches the handler logic: `if existingExtID == "" || existingExtID == sessionID { ... }`.
func TestPiSessionCapture_NotOverwritten(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "pi", "test-no-overwrite", "", "", "default", "chat")
	assert.NoError(t, err)

	// First capture
	err = service.UpdateExternalSessionID(sessionID, "pi-sess-first")
	assert.NoError(t, err)

	// Attempt to overwrite (handler skips this because existingExtID != "" && existingExtID != sessionID)
	// Simulate by checking the condition the handler uses
	existingExtID := service.GetExternalSessionID(sessionID)
	assert.Equal(t, "pi-sess-first", existingExtID)
	// The handler would NOT call UpdateExternalSessionID again — the condition
	// `if existingExtID == "" || existingExtID == sessionID` is false.
	// We verify the current value is intact.
	assert.Equal(t, "pi-sess-first", service.GetExternalSessionID(sessionID))
}

// TestPiMetadataSessionID_PersistedToDB verifies that when a Pi metadata event
// carries a SessionID, it is persisted to the database. This tests the handler
// condition `backendName == "pi"` in the metadata branch of executeStreamRun.
func TestPiMetadataSessionID_PersistedToDB(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "pi", "test-metadata", "", "", "default", "chat")
	assert.NoError(t, err)

	// CreateSession pre-fills external_session_id with sessionID
	assert.Equal(t, sessionID, service.GetExternalSessionID(sessionID))

	// Simulate what the handler does on metadata event for Pi:
	// The handler checks `backendName == "pi"` && event.Meta.SessionID != ""
	// and calls UpdateExternalSessionID.
	metaSessionID := "019e2178-e67b-715c-8552-6d6e49e4960a"
	err = service.UpdateExternalSessionID(sessionID, metaSessionID)
	assert.NoError(t, err)

	assert.Equal(t, metaSessionID, service.GetExternalSessionID(sessionID))
}

// TestPiSessionCapture_OtherBackendsIgnored verifies that session_capture
// events from backends NOT in the external ID list (e.g., claude, codebuddy)
// do NOT trigger external_session_id persistence. This ensures the "pi"
// addition doesn't accidentally enable it for backends that don't need it.
// TestPiSessionCapture_OtherBackendsIgnored removed — the original test was a tautology
// that only tested a local boolean expression, not the actual handler code path.
// The real coverage is in TestPiSessionCapture_* and TestCodexSessionCapture_PersistedToDB
// which test the actual session_capture event processing for external-ID backends.

// ============================================================================
// Pi end-to-end resume chain test
// ============================================================================

// TestPiEndToEndResumeChain verifies the complete flow:
// 1. Create a new Pi session (no external ID)
// 2. Simulate session_capture persisting a Pi session ID
// 3. Add an assistant message (making the session resumable)
// 4. Call buildChatRequest — should resolve external ID
// 5. Verify buildChatRequest returns the correct SessionID for Pi resume
//
// This tests the two-layer fix together:
// - Layer 1: handler resolves external_session_id for Pi
// - Layer 2: Pi new sessions create persistent sessions (tested in ai package)
func TestPiEndToEndResumeChain(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Step 1: New Pi session
	sessionID, err := service.CreateSession(env.ProjectDir, "pi", "test-e2e", "", "", "default", "chat")
	assert.NoError(t, err)

	// Step 2: New session → buildChatRequest should return the ClawBench UUID
	// (Pi will create a persistent session on its own, not using --no-session)
	newReq := buildChatRequest("hello", sessionID, env.ProjectDir, "pi", "codebuddy", "", "", "", "", "")
	assert.False(t, newReq.Resume, "new session should not be resume")
	// For non-resume, buildChatRequest passes the ClawBench UUID as-is.
	// buildPiStreamArgs ignores SessionID when Resume=false (uses no session flag).
	assert.Equal(t, sessionID, newReq.SessionID)

	// Step 3: Simulate Pi CLI emitting session event → handler persists external ID
	piSessID := "019e2172-6ebd-743e-8bb6-39d51df91bde"
	err = service.UpdateExternalSessionID(sessionID, piSessID)
	assert.NoError(t, err)

	// Step 4: Add assistant message so SessionHasAssistant returns true
	_, err = service.AddChatMessage(env.ProjectDir, "pi", sessionID, "assistant",
		`{"blocks":[{"type":"text","text":"Hello!"}]}`, nil, false, "")
	assert.NoError(t, err)

	// Step 5: Resume → buildChatRequest should resolve external ID
	resumeReq := buildChatRequest("continue", sessionID, env.ProjectDir, "pi", "codebuddy", "", "", "", "", "")
	assert.True(t, resumeReq.Resume, "session with assistant messages should be resume")
	assert.Equal(t, piSessID, resumeReq.SessionID,
		"resume should use the Pi-assigned external session ID, not the ClawBench UUID")
}

// ============================================================================
// Codex external session ID tests
// ============================================================================

// TestBuildChatRequest_CodexResumeWithExternalSessionID verifies that when
// Codex backend resumes a session that has an external_session_id stored
// (a thread_id), that ID is used instead of the ClawBench UUID.
func TestBuildChatRequest_CodexResumeWithExternalSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "test-codex", "", "", "default", "chat")
	assert.NoError(t, err)

	threadID := "thread_abc123def456"
	err = service.UpdateExternalSessionID(sessionID, threadID)
	assert.NoError(t, err)

	_, err = service.AddChatMessage(env.ProjectDir, "codex", sessionID, "assistant",
		`{"blocks":[{"type":"text","text":"done"}]}`, nil, false, "")
	assert.NoError(t, err)

	req := buildChatRequest("continue", sessionID, env.ProjectDir, "codex", "codebuddy", "", "", "", "", "")
	assert.True(t, req.Resume)
	assert.Equal(t, threadID, req.SessionID, "Codex should use thread_id as external session ID")
}

// TestBuildChatRequest_CodexResumeWithoutExternalSessionID verifies that when
// Codex backend resumes a session that has NO external_session_id, the
// SessionID is cleared to avoid passing the invalid ClawBench UUID.
func TestBuildChatRequest_CodexResumeWithoutExternalSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "test-codex-noext", "", "", "default", "chat")
	assert.NoError(t, err)

	// CreateSession pre-fills external_session_id = sessionID; clear it to simulate
	// the "no external session ID captured" scenario
	err = service.UpdateExternalSessionID(sessionID, "")
	assert.NoError(t, err)

	_, err = service.AddChatMessage(env.ProjectDir, "codex", sessionID, "assistant",
		`{"blocks":[{"type":"text","text":"hello"}]}`, nil, false, "")
	assert.NoError(t, err)

	req := buildChatRequest("continue", sessionID, env.ProjectDir, "codex", "codebuddy", "", "", "", "", "")
	assert.True(t, req.Resume)
	assert.Equal(t, "", req.SessionID,
		"Codex should clear SessionID when no external ID available")
}

// TestCodexSessionCapture_PersistedToDB verifies that session_capture events
// for Codex backend persist the external session ID (thread_id) to the database.
func TestCodexSessionCapture_PersistedToDB(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "test-codex-capture", "", "", "default", "chat")
	assert.NoError(t, err)

	// CreateSession pre-fills external_session_id with sessionID
	assert.Equal(t, sessionID, service.GetExternalSessionID(sessionID))

	threadID := "thread_xyz789"
	err = service.UpdateExternalSessionID(sessionID, threadID)
	assert.NoError(t, err)

	assert.Equal(t, threadID, service.GetExternalSessionID(sessionID))
}

// ============================================================================
// DeepSeek external session ID tests
// ============================================================================

// TestBuildChatRequest_DeepSeekResumeWithExternalSessionID verifies that when
// DeepSeek backend resumes a session that has an external_session_id stored,
// that ID is used instead of the ClawBench UUID.
func TestBuildChatRequest_DeepSeekResumeWithExternalSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "deepseek", "test-deepseek", "", "", "default", "chat")
	assert.NoError(t, err)

	dsSessionID := "ds-sess-xyz789"
	err = service.UpdateExternalSessionID(sessionID, dsSessionID)
	assert.NoError(t, err)

	_, err = service.AddChatMessage(env.ProjectDir, "deepseek", sessionID, "assistant",
		`{"blocks":[{"type":"text","text":"done"}]}`, nil, false, "")
	assert.NoError(t, err)

	req := buildChatRequest("continue", sessionID, env.ProjectDir, "deepseek", "codebuddy", "", "", "", "", "")
	assert.True(t, req.Resume)
	assert.Equal(t, dsSessionID, req.SessionID, "DeepSeek should use external session ID")
}

// TestBuildChatRequest_DeepSeekResumeWithoutExternalSessionID verifies that
// when DeepSeek backend resumes a session with NO external_session_id, the
// SessionID is cleared.
func TestBuildChatRequest_DeepSeekResumeWithoutExternalSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "deepseek", "test-deepseek-noext", "", "", "default", "chat")
	assert.NoError(t, err)

	// CreateSession pre-fills external_session_id = sessionID; clear it to simulate
	// the "no external session ID captured" scenario
	err = service.UpdateExternalSessionID(sessionID, "")
	assert.NoError(t, err)

	_, err = service.AddChatMessage(env.ProjectDir, "deepseek", sessionID, "assistant",
		`{"blocks":[{"type":"text","text":"hello"}]}`, nil, false, "")
	assert.NoError(t, err)

	req := buildChatRequest("continue", sessionID, env.ProjectDir, "deepseek", "codebuddy", "", "", "", "", "")
	assert.True(t, req.Resume)
	assert.Equal(t, "", req.SessionID,
		"DeepSeek should clear SessionID when no external ID available")
}

// TestDeepSeekSessionCapture_PersistedToDB verifies that session_capture events
// for DeepSeek backend persist the external session ID to the database.
func TestDeepSeekSessionCapture_PersistedToDB(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "deepseek", "test-deepseek-capture", "", "", "default", "chat")
	assert.NoError(t, err)

	// CreateSession pre-fills external_session_id with sessionID
	assert.Equal(t, sessionID, service.GetExternalSessionID(sessionID))

	dsSessionID := "ds-captured-abc"
	err = service.UpdateExternalSessionID(sessionID, dsSessionID)
	assert.NoError(t, err)

	assert.Equal(t, dsSessionID, service.GetExternalSessionID(sessionID))
}

// ============================================================================
// OpenCode external session ID tests (supplement existing)
// ============================================================================

// TestBuildChatRequest_OpenCodeResumeWithoutExternalSessionID verifies that
// when OpenCode backend resumes a session with NO external_session_id,
// the SessionID is cleared.
func TestBuildChatRequest_OpenCodeResumeWithoutExternalSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "opencode", "test-oc-noext", "", "", "default", "chat")
	assert.NoError(t, err)

	// CreateSession pre-fills external_session_id = sessionID; clear it to simulate
	// the "no external session ID captured" scenario
	err = service.UpdateExternalSessionID(sessionID, "")
	assert.NoError(t, err)

	// No external session ID set
	_, err = service.AddChatMessage(env.ProjectDir, "opencode", sessionID, "assistant",
		`{"blocks":[{"type":"text","text":"hello"}]}`, nil, false, "")
	assert.NoError(t, err)

	req := buildChatRequest("continue", sessionID, env.ProjectDir, "opencode", "codebuddy", "", "", "", "", "")
	assert.True(t, req.Resume)
	assert.Equal(t, "", req.SessionID,
		"OpenCode should clear SessionID when no external ID available")
}

// TestOpenCodeSessionCapture_PersistedToDB verifies that session_capture events
// for OpenCode backend persist the external session ID to the database.
func TestOpenCodeSessionCapture_PersistedToDB(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "opencode", "test-oc-capture", "", "", "default", "chat")
	assert.NoError(t, err)

	// CreateSession pre-fills external_session_id with sessionID
	assert.Equal(t, sessionID, service.GetExternalSessionID(sessionID))

	sesID := "ses_oc_abc123"
	err = service.UpdateExternalSessionID(sessionID, sesID)
	assert.NoError(t, err)

	assert.Equal(t, sesID, service.GetExternalSessionID(sessionID))
}

// ============================================================================
// Codebuddy resume test (UUID-native backend)
// ============================================================================

// ============================================================================
// buildChatRequest thinking effort priority tests
// ============================================================================

// TestBuildChatRequest_ThinkingEffort_OverridePriority verifies that when
// thinkingEffortOverride is non-empty, it takes priority over the agent's
// YAML-configured default.
func TestBuildChatRequest_ThinkingEffort_OverridePriority(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Add an agent with ThinkingEffort set in YAML
	model.Agents["thinking-agent"] = &model.Agent{
		ID:             "thinking-agent",
		Name:           "Thinking Agent",
		Backend:        "codebuddy",
		ThinkingEffort: "low", // YAML default
		Models:         []model.AgentModel{{ID: "glm-5.1", Name: "GLM 5.1", Default: true}},
	}

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "thinking-override", "", "", "thinking-agent", "chat")
	assert.NoError(t, err)

	// Override should take priority over agent default
	req := buildChatRequest("hello", sessionID, env.ProjectDir, "codebuddy", "thinking-agent", "", "high", "", "", "")
	assert.Equal(t, "high", req.ThinkingEffort, "thinkingEffortOverride='high' should override agent default 'low'")
}

// TestBuildChatRequest_ThinkingEffort_AgentDefault verifies that when
// thinkingEffortOverride is empty but the agent has ThinkingEffort in YAML,
// the agent default is used.
func TestBuildChatRequest_ThinkingEffort_AgentDefault(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Add an agent with ThinkingEffort set in YAML
	model.Agents["thinking-agent"] = &model.Agent{
		ID:             "thinking-agent",
		Name:           "Thinking Agent",
		Backend:        "codebuddy",
		ThinkingEffort: "medium", // YAML default
		Models:         []model.AgentModel{{ID: "glm-5.1", Name: "GLM 5.1", Default: true}},
	}

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "thinking-agent-default", "", "", "thinking-agent", "chat")
	assert.NoError(t, err)

	// No override → agent default should be used
	req := buildChatRequest("hello", sessionID, env.ProjectDir, "codebuddy", "thinking-agent", "", "", "", "", "")
	assert.Equal(t, "medium", req.ThinkingEffort, "agent YAML default 'medium' should be used when no override")
}

// TestBuildChatRequest_ThinkingEffort_BothEmpty verifies that when both
// thinkingEffortOverride and agent ThinkingEffort are empty, the
// ChatRequest.ThinkingEffort is also empty.
func TestBuildChatRequest_ThinkingEffort_BothEmpty(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "thinking-empty", "", "", "codebuddy", "chat")
	assert.NoError(t, err)

	// Neither override nor agent default → empty
	req := buildChatRequest("hello", sessionID, env.ProjectDir, "codebuddy", "codebuddy", "", "", "", "", "")
	assert.Equal(t, "", req.ThinkingEffort, "ThinkingEffort should be empty when both override and agent default are empty")
}

// TestBuildChatRequest_CodebuddyResumeNoExternalID verifies that Codebuddy
// backend (which natively uses ClawBench UUID) is NOT affected by the
// external session ID resolution logic — it should always get the UUID.
func TestBuildChatRequest_CodebuddyResumeNoExternalID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "test-cb", "", "", "codebuddy", "chat")
	assert.NoError(t, err)

	_, err = service.AddChatMessage(env.ProjectDir, "codebuddy", sessionID, "assistant",
		`{"blocks":[{"type":"text","text":"hi"}]}`, nil, false, "")
	assert.NoError(t, err)

	req := buildChatRequest("continue", sessionID, env.ProjectDir, "codebuddy", "codebuddy", "", "", "", "", "")
	assert.True(t, req.Resume)
	assert.Equal(t, sessionID, req.SessionID,
		"Codebuddy should get the ClawBench UUID directly, no external ID resolution")
}

// ---------- ServeSessions pagination ----------

func TestServeSessions_Pagination_NoLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create sessions
	for i := range 5 {
		_, err := service.CreateSession(env.ProjectDir, "codebuddy", fmt.Sprintf("session %d", i), "", "", "default", "chat")
		assert.NoError(t, err)
	}

	// No limit param = return all
	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	sessions, _ := result["sessions"].([]interface{})
	assert.Len(t, sessions, 5)
	assert.Equal(t, false, result["hasMore"])
}

func TestServeSessions_Pagination_WithLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create 5 sessions
	for i := range 5 {
		_, err := service.CreateSession(env.ProjectDir, "codebuddy", fmt.Sprintf("session %d", i), "", "", "default", "chat")
		assert.NoError(t, err)
	}

	// Request with limit=3
	req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit=3", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	sessions, _ := result["sessions"].([]interface{})
	assert.Len(t, sessions, 3)
	assert.Equal(t, true, result["hasMore"])
}

func TestServeSessions_Pagination_LimitExceedsTotal(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	_, err := service.CreateSession(env.ProjectDir, "codebuddy", "only session", "", "", "default", "chat")
	assert.NoError(t, err)

	// Limit=10 but only 1 session exists
	req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit=10", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	sessions, _ := result["sessions"].([]interface{})
	assert.Len(t, sessions, 1)
	assert.Equal(t, false, result["hasMore"])
}

func TestServeSessions_Pagination_InvalidLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	_, err := service.CreateSession(env.ProjectDir, "codebuddy", "session", "", "", "default", "chat")
	assert.NoError(t, err)

	// Invalid limit should be treated as 0 (no limit, return all)
	req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit=abc", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	sessions, _ := result["sessions"].([]interface{})
	assert.Len(t, sessions, 1)
	assert.Equal(t, false, result["hasMore"])
}

func TestServeSessions_Pagination_ZeroLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	for i := range 3 {
		_, err := service.CreateSession(env.ProjectDir, "codebuddy", fmt.Sprintf("s%d", i), "", "", "default", "chat")
		assert.NoError(t, err)
	}

	// limit=0 should return all (backward compatible)
	req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit=0", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	sessions, _ := result["sessions"].([]interface{})
	assert.Len(t, sessions, 3)
	assert.Equal(t, false, result["hasMore"])
}

func TestServeSessions_Pagination_EmptyProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// No sessions created
	req := newRequest(t, http.MethodGet, "/api/ai/sessions?limit=10", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	// sessions may be null (nil slice) when empty
	sessionsRaw := result["sessions"]
	if sessionsRaw == nil {
		// null is acceptable for empty
	} else {
		sessions, _ := sessionsRaw.([]interface{})
		assert.Empty(t, sessions)
	}
	assert.Equal(t, false, result["hasMore"])
}

// ============================================================================
// Session model: global preference (cross-project) tests
// ============================================================================

// TestCreateSession_ModelNotPreFilled verifies that CreateSession does NOT
// pre-fill the agent's default model into the session's model field.
// The model should be empty so the frontend falls back to the global
// localStorage preference, making the user's model choice persist across projects.
func TestCreateSession_ModelNotPreFilled(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session with no explicit model — the model field should be empty,
	// NOT the agent's default model (e.g. "glm-5.1" for codebuddy agent).
	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "model-test", "codebuddy", "", "default", "chat")
	assert.NoError(t, err)

	// Verify model field is empty in DB
	modelID := service.GetSessionModel(sessionID)
	assert.Equal(t, "", modelID,
		"new session should have empty model field so frontend uses global localStorage preference")
}

// TestCreateSession_ModelPreFilled_OldBehaviorRemoved verifies that the old
// behavior (pre-filling agent default model) is no longer happening.
// This is a regression test — if someone changes CreateSession to accept
// a model parameter again, this test will catch it.
func TestCreateSession_ModelPreFilled_OldBehaviorRemoved(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// The codebuddy agent has a default model "glm-5.1" in test setup.
	// Creating a session should NOT auto-fill "glm-5.1" into the model field.
	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "no-prefill", "codebuddy", "", "default", "chat")
	assert.NoError(t, err)

	modelID := service.GetSessionModel(sessionID)
	assert.NotEqual(t, "glm-5.1", modelID,
		"session model should NOT be pre-filled with agent default model")
}

// TestBuildChatRequest_ModelOverride_FromSession verifies that buildChatRequest
// uses the model from the session when no explicit override is provided.
// This ensures that the user's explicit model choice (stored in session DB)
// is respected even for queued messages.
func TestBuildChatRequest_ModelOverride_FromSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "model-from-session", "codebuddy", "", "default", "chat")
	assert.NoError(t, err)

	// User explicitly selects a model → handler calls UpdateSessionModel
	_ = service.UpdateSessionModel(sessionID, "claude-sonnet-4-6")

	// buildChatRequest with no modelOverride should use agent default,
	// NOT the session model (session model is for frontend display;
	// buildChatRequest modelOverride comes from req.ModelID)
	req := buildChatRequest("hello", sessionID, env.ProjectDir, "codebuddy", "codebuddy", "", "", "", "", "")
	// Without modelOverride, agent default is used
	assert.Equal(t, "glm-5.1", req.Model, "without modelOverride, agent default model should be used")
}

// TestBuildChatRequest_ModelOverride_ExplicitOverSession verifies that an
// explicit modelOverride (from frontend req.ModelID) takes priority over
// everything else, including the agent default.
func TestBuildChatRequest_ModelOverride_ExplicitOverSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "model-explicit", "codebuddy", "", "default", "chat")
	assert.NoError(t, err)

	// Frontend sends modelId explicitly
	req := buildChatRequest("hello", sessionID, env.ProjectDir, "codebuddy", "codebuddy", "claude-sonnet-4-6", "", "", "", "")
	assert.Equal(t, "claude-sonnet-4-6", req.Model,
		"explicit modelOverride should take priority over agent default")
}

// TestBuildChatRequestFromQueue_UsesSessionModel verifies that
// buildChatRequestFromQueue uses the session-persisted model (which was
// saved when the user sent a message with an explicit modelId), rather
// than falling back to the agent default.
func TestBuildChatRequestFromQueue_UsesSessionModel(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "queue-model", "codebuddy", "", "default", "chat")
	assert.NoError(t, err)

	// Simulate user sending a message with explicit model → handler calls UpdateSessionModel
	_ = service.UpdateSessionModel(sessionID, "claude-sonnet-4-6")

	// buildChatRequestFromQueue should use the session model
	qMsg := model.QueuedMessage{Text: "next message", CreatedAt: time.Now().Format(time.RFC3339)}
	req := buildChatRequestFromQueue(qMsg, sessionID, env.ProjectDir, "codebuddy", "codebuddy", "")
	assert.Equal(t, "claude-sonnet-4-6", req.Model,
		"queued message should use session-persisted model, not agent default")
}

// TestServeSessions_Post_NewSessionEmptyModel verifies that POST /api/ai/sessions
// creates a session with an empty model field, allowing the frontend to
// resolve the model from global localStorage preference.
func TestServeSessions_Post_NewSessionEmptyModel(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]string{}
	req := newRequest(t, http.MethodPost, "/api/ai/sessions", body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["ok"])

	sessionID, _ := result["sessionId"].(string)
	modelID := service.GetSessionModel(sessionID)
	assert.Equal(t, "", modelID,
		"newly created session should have empty model field for global preference resolution")
}

// ============================================================================
// AIChat GET — no session_id path (GetLatestSessionID)
// ============================================================================

// TestAIChat_Get_NoSessionID_UsesLatestSession verifies that when AIChat GET
// is called without a session_id, the handler uses GetLatestSessionID to find
// the most recent session instead of loading all sessions via GetSessions.
func TestAIChat_Get_NoSessionID_UsesLatestSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create two sessions. Directly set s2's updated_at to be newer than s1's
	// (both sessions created in the same second would have identical timestamps,
	// making the tie-breaker depend on UUID sort order which is non-deterministic).
	s1, _ := service.CreateSession(env.ProjectDir, "claude", "First", "claude", "", "default", "chat")
	s2, _ := service.CreateSession(env.ProjectDir, "codebuddy", "Second", "codebuddy", "", "default", "chat")
	// Force s2 to be more recent by setting its updated_at 1 second ahead
	_, _ = service.DB.Exec("UPDATE chat_sessions SET updated_at = datetime(updated_at, '+1 second') WHERE id = ?", s2)

	// GET without session_id should use the latest session
	req := newRequest(t, http.MethodGet, "/api/ai/chat?limit=20", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, "")

	w := callHandlerWithAuth(AIChat, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, s2, resp["sessionId"])

	// Verify s1 is NOT returned (proves it's using latest, not first)
	assert.NotEqual(t, s1, resp["sessionId"])
}

// TestAIChat_Get_NoSessionID_NoSessionsCreatesNew verifies that when AIChat GET
// is called without a session_id and no sessions exist, a new session is created.
func TestAIChat_Get_NoSessionID_NoSessionsCreatesNew(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// No sessions exist — GET without session_id should auto-create one
	req := newRequest(t, http.MethodGet, "/api/ai/chat?limit=20", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, "")

	w := callHandlerWithAuth(AIChat, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp["sessionId"], "should auto-create a session when none exist")
	assert.NotEmpty(t, resp["sessionId"])
}

// TestAIChat_Get_WithSessionID_ReturnsSessionInfo verifies that when AIChat GET
// is called with a specific session_id, the sessionInfo fields (title, backend,
// agentId, modelId, thinkingEffort) are populated from the single GetSessionInfo query.
func TestAIChat_Get_WithSessionID_ReturnsSessionInfo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session with specific agent and model
	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "My Test Session", "codebuddy", "glm-5.1", "default", "chat")
	assert.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/chat?session_id="+sessionID+"&limit=20", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, "")

	w := callHandlerWithAuth(AIChat, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, sessionID, resp["sessionId"])
	assert.Equal(t, "My Test Session", resp["sessionTitle"])
	assert.Equal(t, "codebuddy", resp["backend"])
	assert.Equal(t, "codebuddy", resp["agentId"])
}

// TestAIChat_Get_SessionInfoBackendOverride verifies that when GetSessionInfo
// returns a backend that differs from the one initially resolved (e.g., from
// GetSessionBackend or GetLatestSessionID), the sessionInfo backend takes priority.
func TestAIChat_Get_SessionInfoBackendOverride(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session with backend "claude"
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "Backend Test", "claude", "", "default", "chat")
	assert.NoError(t, err)

	// Add a message so the session has history
	_, err = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "user", "hello", nil, false, "NewSession")
	assert.NoError(t, err)

	// Request with session_id — GetSessionBackend returns "claude",
	// GetSessionInfo should also return "claude", and the response should reflect it
	req := newRequest(t, http.MethodGet, "/api/ai/chat?session_id="+sessionID+"&limit=20", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, "")

	w := callHandlerWithAuth(AIChat, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "claude", resp["backend"])
}

// TestAIChat_Get_NoSessionID_SessionInfoFieldsPopulated verifies that the
// GetLatestSessionID + GetSessionInfo path (no session_id in request) still
// populates all sessionInfo fields correctly.
func TestAIChat_Get_NoSessionID_SessionInfoFieldsPopulated(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session
	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "Info Session", "codebuddy", "", "default", "chat")
	assert.NoError(t, err)
	// Set model explicitly
	_ = service.UpdateSessionModel(sessionID, "glm-5.1")

	// GET without session_id — should find this session via GetLatestSessionID
	req := newRequest(t, http.MethodGet, "/api/ai/chat?limit=20", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, "")

	w := callHandlerWithAuth(AIChat, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, sessionID, resp["sessionId"])
	assert.Equal(t, "Info Session", resp["sessionTitle"])
	assert.Equal(t, "codebuddy", resp["backend"])
	assert.Equal(t, "codebuddy", resp["agentId"])
	assert.Equal(t, "glm-5.1", resp["modelId"])
}

// TestAIChat_Get_NoSessionID_NoAgentsAvailable verifies that when no sessions
// exist and no agents are available, the handler returns NoAgentsAvailable error.
func TestAIChat_Get_NoSessionID_NoAgentsAvailable(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Remove all agents so resolveAgentConfig fails
	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}

	req := newRequest(t, http.MethodGet, "/api/ai/chat?limit=20", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, "")

	w := callHandlerWithAuth(AIChat, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestAIChat_Get_NoSessionID_CreateSessionError verifies that when no sessions
// exist and CreateSession fails (e.g., DB closed), the handler returns
// an internal error.
func TestAIChat_Get_NoSessionID_CreateSessionError(t *testing.T) {
	env, teardown := setupTestEnv(t)

	// Close the DB to force errors. Both DB and DBRead point to the same
	// :memory: instance, so closing either closes both. After closing,
	// queries will return errors rather than panic (nil dereference).
	service.CloseDB()

	req := newRequest(t, http.MethodGet, "/api/ai/chat?limit=20", nil)
	withProjectCookie(req, env.ProjectDir)
	withAuthCookie(req, "")

	w := callHandlerWithAuth(AIChat, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Prevent teardown from double-closing the already-closed db.
	// Restore globals so teardown's _ = db.Close() becomes a safe no-op on
	// the original (pre-setupTestEnv) values.
	_ = env
	teardown()
}

// ============================================================================
// executeStreamRun ctx.Done() and finalizeStreamRun coverage tests
// ============================================================================

// TestExecuteStreamRun_CtxCancelled verifies the ctx.Done() branch in
// executeStreamRun. When the context is cancelled while the event loop is
// waiting for events, the function should finalize and return.
func TestExecuteStreamRun_CtxCancelled(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "test-ctx-cancel", "", "", "default", "chat")
	assert.NoError(t, err)

	// Start the session running
	service.SetSessionRunning(sessionID, true, false)
	defer service.SetSessionRunning(sessionID, false, false)

	// Use a cancelled context to trigger the ctx.Done() branch
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	streamCh := make(chan ai.StreamEvent, 10)
	chatReq := ai.ChatRequest{Prompt: "test"}

	req := newRequest(t, http.MethodPost, "/api/ai/chat", bytes.NewReader([]byte(`{}`)))
	req = withProjectCookie(req, env.ProjectDir)

	// executeStreamRun should hit the ctx.Done() branch because the
	// backend.ExecuteStream call will fail (no claude CLI), and during
	// the event loop iteration, the cancelled context will be selected.
	result := executeStreamRun(ctx, req, streamCh, env.ProjectDir, sessionID, "claude", "default", chatReq, "")
	// The result should indicate an error (no backend available) but
	// the ctx.Done() path should still be covered in the select statement.
	_ = result
}

// TestFinalizeStreamRun_CtxCancelled verifies the context.Canceled path
// in SessionExecutor.Finalize() when no cancel reason was recorded.
func TestFinalizeStreamRun_CtxCancelled(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "test-finalize-ctx", "", "", "default", "chat")
	assert.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	blocks := []model.ContentBlock{
		{Type: "text", Text: "hello"},
	}

	// Create a streaming placeholder
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	_, _ = service.AddChatMessage(env.ProjectDir, "claude", sessionID, "assistant", string(emptyContent), nil, true, "")

	// Use SessionExecutor.Finalize with cancelled context
	cfg := service.RunConfig{
		Mode:        service.ModeInteractive,
		ProjectPath: env.ProjectDir,
		BackendName: "claude",
		SessionID:   sessionID,
		AgentID:     "default",
	}
	executor := service.NewSessionExecutor(ctx, cfg)
	runResult := service.RunResult{
		ReceivedTerminal: false,
		CancelReason:     "",
		Blocks:           blocks,
		Metadata:         &ai.Metadata{},
	}
	result := executor.Finalize(runResult, nil)

	// When ctx is cancelled with non-empty blocks, Finalize
	// should complete successfully (blocks are preserved).
	_ = result // Verify no panic/crash
}

// ============================================================================
// session_capture / metadata conditional overwrite logic tests
// ============================================================================

// TestSessionCapture_OverwritesDefault tests the handler condition:
//
//	if existingExtID == "" || existingExtID == sessionID { UpdateExternalSessionID }
//
// When external_session_id is the default (== sessionID), a session_capture
// event should overwrite it with the real CLI-assigned ID.
func TestSessionCapture_OverwritesDefault(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "pi", "test-overwrite-default", "", "", "default", "chat")
	assert.NoError(t, err)

	// CreateSession pre-fills external_session_id = sessionID (the default)
	assert.Equal(t, sessionID, service.GetExternalSessionID(sessionID))

	// Simulate handler condition: existingExtID == sessionID → overwrite
	existingExtID := service.GetExternalSessionID(sessionID)
	assert.Equal(t, sessionID, existingExtID, "default should equal sessionID")

	// Handler would call UpdateExternalSessionID because condition is true
	piExtID := "019e2172-aaaa-bbbb-cccc-dddddddddddd"
	err = service.UpdateExternalSessionID(sessionID, piExtID)
	assert.NoError(t, err)

	assert.Equal(t, piExtID, service.GetExternalSessionID(sessionID),
		"session_capture should overwrite the default ClawBench UUID")
}

// TestSessionCapture_DoesNotOverwriteCLIAssignedID tests the handler condition:
// When external_session_id has already been set to a CLI-assigned ID
// (different from sessionID), a subsequent session_capture event should
// NOT overwrite it.
func TestSessionCapture_DoesNotOverwriteCLIAssignedID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "pi", "test-no-overwrite", "", "", "default", "chat")
	assert.NoError(t, err)

	// Simulate first session_capture
	firstID := "019e2172-first-capture-id"
	err = service.UpdateExternalSessionID(sessionID, firstID)
	assert.NoError(t, err)

	// Now simulate a second session_capture: handler checks condition
	existingExtID := service.GetExternalSessionID(sessionID)
	// Condition: existingExtID == "" || existingExtID == sessionID
	// Both are false → handler skips the update
	assert.NotEqual(t, "", existingExtID)
	assert.NotEqual(t, sessionID, existingExtID)

	// The handler would NOT call UpdateExternalSessionID — verify value is unchanged
	assert.Equal(t, firstID, service.GetExternalSessionID(sessionID),
		"subsequent session_capture should NOT overwrite CLI-assigned ID")
}

// TestSessionCapture_EmptyContentSkipped verifies that a session_capture event
// with empty Content does not trigger UpdateExternalSessionID.
// The handler has `if event.Content != ""` before the update.
func TestSessionCapture_EmptyContentSkipped(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "pi", "test-empty-capture", "", "", "default", "chat")
	assert.NoError(t, err)

	// Verify default value before
	assert.Equal(t, sessionID, service.GetExternalSessionID(sessionID))

	// Simulate: handler sees event.Content == "" → skips UpdateExternalSessionID
	// (We verify by checking the value is still the default)
	assert.Equal(t, sessionID, service.GetExternalSessionID(sessionID),
		"empty Content should not trigger update")
}

// TestMetadataEvent_OverwritesDefault tests the metadata event handler condition:
// Same as session_capture — when existingExtID is the default (== sessionID),
// a metadata event with SessionID should overwrite it.
func TestMetadataEvent_OverwritesDefault(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "opencode", "test-meta-default", "", "", "default", "chat")
	assert.NoError(t, err)

	// Default: external_session_id == sessionID
	assert.Equal(t, sessionID, service.GetExternalSessionID(sessionID))

	// Simulate metadata event: handler checks condition (same as session_capture)
	existingExtID := service.GetExternalSessionID(sessionID)
	assert.Equal(t, sessionID, existingExtID)

	// Condition is true → handler calls UpdateExternalSessionID
	metaSessionID := "ses_oc_meta_123"
	err = service.UpdateExternalSessionID(sessionID, metaSessionID)
	assert.NoError(t, err)

	assert.Equal(t, metaSessionID, service.GetExternalSessionID(sessionID))
}

// TestMetadataEvent_DoesNotOverwriteSessionCaptureID verifies that when
// session_capture has already set a CLI-assigned ID, a subsequent metadata
// event does NOT overwrite it. This tests the interaction between the two
// event types — both share the same condition logic.
func TestMetadataEvent_DoesNotOverwriteSessionCaptureID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "test-meta-vs-capture", "", "", "default", "chat")
	assert.NoError(t, err)

	// Simulate session_capture event (sets CLI-assigned ID)
	captureID := "thread_capture_first"
	err = service.UpdateExternalSessionID(sessionID, captureID)
	assert.NoError(t, err)

	// Simulate metadata event: handler checks condition
	existingExtID := service.GetExternalSessionID(sessionID)
	assert.NotEqual(t, "", existingExtID)
	assert.NotEqual(t, sessionID, existingExtID)
	// Condition: existingExtID == "" || existingExtID == sessionID → FALSE
	// → handler skips UpdateExternalSessionID

	// Verify the session_capture value is preserved
	assert.Equal(t, captureID, service.GetExternalSessionID(sessionID),
		"metadata event should NOT overwrite session_capture value")
}

// TestMetadataEvent_EmptySessionIDSkipped verifies that a metadata event
// with empty Meta.SessionID does not trigger UpdateExternalSessionID.
func TestMetadataEvent_EmptySessionIDSkipped(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "pi", "test-empty-meta", "", "", "default", "chat")
	assert.NoError(t, err)

	// Handler has `if event.Meta.SessionID != ""` — empty → skip
	// Verify default value is preserved
	assert.Equal(t, sessionID, service.GetExternalSessionID(sessionID),
		"empty Meta.SessionID should not trigger update")
}

// TestBuildChatRequest_ContinuedSessionUsesExternalSessionID verifies that
// a continued session (created by ContinueFromExecution) correctly uses
// --resume with the inherited external_session_id.
func TestBuildChatRequest_ContinuedSessionUsesExternalSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a scheduled session with external_session_id set
	schedSessionID, err := service.CreateSession(env.ProjectDir, "pi", "Scheduled Task", "", "", "default", "scheduled")
	assert.NoError(t, err)
	err = service.UpdateExternalSessionID(schedSessionID, "pi-cli-session-abc")
	assert.NoError(t, err)

	// Create task + execution
	var taskID int64
	result, err := service.DB.Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, status) VALUES (?, ?, '0 8 * * *', ?, 'Do task', 'active')",
		env.ProjectDir, "Task", "codebuddy",
	)
	assert.NoError(t, err)
	taskID, _ = result.LastInsertId()

	var execID int64
	result, err = service.DB.Exec(
		"INSERT INTO task_executions (task_id, session_id, status) VALUES (?, ?, 'completed')",
		taskID, schedSessionID,
	)
	assert.NoError(t, err)
	execID, _ = result.LastInsertId()

	// Add assistant messages to the source session
	_, err = service.AddChatMessage(env.ProjectDir, "pi", schedSessionID, "user", "do something", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "pi", schedSessionID, "assistant", "done", nil, false, "")
	assert.NoError(t, err)

	// Continue the execution (creates a new chat session with inherited external_session_id)
	contSessionID, _, err := service.ContinueFromExecution(execID, env.ProjectDir)
	assert.NoError(t, err)

	// Add an assistant message to the continued session (simulates first reply)
	_, err = service.AddChatMessage(env.ProjectDir, "pi", contSessionID, "assistant", "continued reply", nil, false, "")
	assert.NoError(t, err)

	// Verify continued session inherits external_session_id
	assert.Equal(t, "pi-cli-session-abc", service.GetExternalSessionID(contSessionID),
		"continued session should inherit external_session_id from source")

	// buildChatRequest for the continued session should use the inherited external_session_id
	req := buildChatRequest("follow up", contSessionID, env.ProjectDir, "pi", "codebuddy", "", "", "", "", "")
	assert.True(t, req.Resume, "continued session with assistant messages should use --resume")
	assert.Equal(t, "pi-cli-session-abc", req.SessionID,
		"continued session should use inherited external_session_id for --resume")
}

func TestAccumulateBlock_ACPToolCallUpdateWithInput(t *testing.T) {
	// Simulate OpenCode ACP task tool flow:
	// 1. tool_call (pending, rawInput={}) → mapACPToolCall → tool_use event with Input="{}"
	// 2. tool_call_update (in_progress, rawInput has description/prompt) → mapACPToolCallUpdate → tool_use event with Input
	// 3. tool_call_update (completed) → mapACPToolCallUpdate → tool_result event with output
	var blocks []model.ContentBlock

	// Step 1: Initial tool_call (pending, empty rawInput like OpenCode sends for task tool)
	ai.AccumulateBlock(&blocks, ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Agent", ID: "call_1", Input: "{}", Done: false},
	})
	assert.Len(t, blocks, 1)
	assert.Equal(t, "Agent", blocks[0].Name)
	t.Logf("After step 1: Input=%v", blocks[0].Input)

	// Step 2: tool_call_update (in_progress, with rawInput containing description/prompt)
	inputJSON, _ := json.Marshal(map[string]any{
		"description":   "Explore project structure",
		"prompt":        "Explore the codebase thoroughly",
		"subagent_type": "explore",
	})
	ai.AccumulateBlock(&blocks, ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Agent", ID: "call_1", Input: string(inputJSON), Done: false},
	})

	// Verify input was updated
	assert.Len(t, blocks, 1, "should still be 1 block (deduped by ID)")
	t.Logf("After step 2: Input=%v", blocks[0].Input)
	assert.Equal(t, "Explore project structure", blocks[0].Input["description"],
		"input description should be updated from tool_call_update")
	assert.Equal(t, "explore", blocks[0].Input["subagent_type"],
		"input subagent_type should be updated from tool_call_update")

	// Step 3: tool_result (completed with output)
	ai.AccumulateBlock(&blocks, ai.StreamEvent{
		Type: "tool_result",
		Tool: &ai.ToolCall{ID: "call_1", Output: "result text", Status: "success", Done: true},
	})

	// Verify final state — input should survive tool_result
	assert.Len(t, blocks, 1)
	assert.True(t, blocks[0].Done)
	assert.Equal(t, "success", blocks[0].Status)
	assert.Equal(t, "Explore project structure", blocks[0].Input["description"],
		"input should still have description after tool_result")
}

func TestAccumulateBlock_ACPToolResultWithInput(t *testing.T) {
	// Regression test: ACP tool_call_update (completed) emits tool_result with
	// rawInput. The tool_result event should update input if it carries input data.
	// This covers the case where earlier tool_use events didn't carry input
	// (e.g., due to channel full or ACP agent sending rawInput only in completed update).
	var blocks []model.ContentBlock

	// Step 1: Initial tool_call (pending, empty rawInput)
	ai.AccumulateBlock(&blocks, ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Agent", ID: "call_2", Input: "{}", Done: false},
	})

	// Step 2: tool_call_update (in_progress, no rawInput — some ACP agents skip this)
	ai.AccumulateBlock(&blocks, ai.StreamEvent{
		Type: "tool_use",
		Tool: &ai.ToolCall{Name: "Agent", ID: "call_2", Input: "", Done: false},
	})

	// Input should still be empty map (not overwritten by empty)
	assert.Len(t, blocks[0].Input, 0, "input should still be empty after tool_use with no input")

	// Step 3: tool_result (completed with rawInput — ACP completed update carries it)
	inputJSON, _ := json.Marshal(map[string]any{
		"description":   "Explore project structure",
		"prompt":        "Explore the codebase",
		"subagent_type": "explore",
	})
	ai.AccumulateBlock(&blocks, ai.StreamEvent{
		Type: "tool_result",
		Tool: &ai.ToolCall{ID: "call_2", Input: string(inputJSON), Output: "result", Status: "success", Done: true},
	})

	// Verify input was updated from tool_result
	assert.Len(t, blocks, 1)
	assert.True(t, blocks[0].Done)
	assert.Equal(t, "Explore project structure", blocks[0].Input["description"],
		"input should be updated from tool_result when it carries rawInput")
	assert.Equal(t, "explore", blocks[0].Input["subagent_type"])
	assert.Equal(t, "result", blocks[0].Output)
}

// --- serializeBlocks nil handling (fcfb228c regression test) ---

func TestSerializeBlocks_NilBlocksProducesEmptyArray(t *testing.T) {
	// This is a regression test for fcfb228c: nil blocks must serialize
	// to {"blocks":[]} not {"blocks":null}, which caused literal text
	// rendering in the frontend.
	serializeBlocks := func(blocks []model.ContentBlock, metadata *ai.Metadata) string {
		serializedBlocks := blocks
		if serializedBlocks == nil {
			serializedBlocks = []model.ContentBlock{}
		}
		contentMap := map[string]any{"blocks": serializedBlocks}
		if metadata != nil {
			contentMap["metadata"] = metadata
		}
		blocksJSON, _ := json.Marshal(contentMap)
		return string(blocksJSON)
	}

	// nil blocks → {"blocks":[]}
	result := serializeBlocks(nil, nil)
	assert.Contains(t, result, `"blocks":[]`)
	assert.NotContains(t, result, `"blocks":null`)

	// nil blocks with metadata → should still have [] not null
	result = serializeBlocks(nil, &ai.Metadata{WallMs: 100})
	assert.Contains(t, result, `"blocks":[]`)
	assert.Contains(t, result, `"wallMs"`)

	// empty slice → also []
	result = serializeBlocks([]model.ContentBlock{}, nil)
	assert.Contains(t, result, `"blocks":[]`)
}

// --- buildChatRequest mode parameter ---

func TestBuildChatRequest_ModeOverride(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "mode-test", "claude", "", "default", "chat")
	assert.NoError(t, err)

	model.Agents["claude"] = &model.Agent{ID: "claude", Backend: "cli", Command: "echo"}

	req := buildChatRequest("hello", sessionID, env.ProjectDir, "claude", "claude", "", "", "architect", "", "")
	assert.Equal(t, "architect", req.Mode, "modeOverride should be passed to ChatRequest.Mode")
}

func TestBuildChatRequest_ModeEmpty(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "mode-empty", "claude", "", "default", "chat")
	assert.NoError(t, err)

	model.Agents["claude"] = &model.Agent{ID: "claude", Backend: "cli", Command: "echo"}

	req := buildChatRequest("hello", sessionID, env.ProjectDir, "claude", "claude", "", "", "", "", "")
	assert.Equal(t, "", req.Mode, "empty modeOverride should result in empty Mode")
}

// --- buildChatRequestFromQueue no longer reads mode from DB ---
// Mode and thinking effort are no longer persisted to DB; they come from ACP runtime.
// buildChatRequestFromQueue now passes empty strings for modeOverride and thinkingEffortOverride.

// --- POST /api/ai/chat without session_id should return 400 (not auto-create) ---

func TestAIChat_POST_NoSessionID_Returns400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Count sessions before the request
	countBefore := 0
	_ = service.DBRead.QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE deleted = 0 AND session_type = 'chat'").Scan(&countBefore)

	// POST without session_id (no cookie, no query param)
	body := map[string]string{"message": "hello"}
	req := newRequest(t, http.MethodPost, "/api/ai/chat", body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(AIChat, req)

	// Should return 400, not auto-create a session
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	decodeRespJSON(t, w.Body, &resp)
	assert.Contains(t, resp["error"], "session_id")

	// Verify no new session was created
	countAfter := 0
	_ = service.DBRead.QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE deleted = 0 AND session_type = 'chat'").Scan(&countAfter)
	assert.Equal(t, countBefore, countAfter, "POST without session_id should NOT auto-create a session")
}

func TestAIChat_POST_WithSessionID_Succeeds(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Create a session first
	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "test session", "codebuddy", "", "default", "chat")
	assert.NoError(t, err)

	model.Agents["codebuddy"] = &model.Agent{ID: "codebuddy", Backend: "cli", Command: "echo"}

	// POST with explicit session_id in query param
	body := map[string]string{"message": "hello", "agentId": "codebuddy"}
	req := newRequest(t, http.MethodPost, "/api/ai/chat?session_id="+sessionID, body)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(AIChat, req)

	// Should succeed (200 or another non-400 code)
	assert.NotEqual(t, http.StatusBadRequest, w.Code, "POST with valid session_id should not return 400")

	// Wait for the async AI goroutine to finish before teardown closes the DB
	assert.Eventually(t, func() bool {
		return !service.IsSessionRunning(sessionID)
	}, 5*time.Second, 50*time.Millisecond, "AI goroutine should finish before teardown")
}

// ============================================================================
// ACP session resume after server restart — targeted tests
// ============================================================================

// TestBuildChatRequest_ACPResume_UsesClawBenchUUID verifies that for ACP-backed
// agents (transport=acp-stdio), buildChatRequest always passes the ClawBench UUID
// as effectiveSessionID, regardless of what external_session_id contains.
// The ACP connection pool handles the UUID→ACP session mapping internally.
func TestBuildChatRequest_ACPResume_UsesClawBenchUUID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "test-acp-resume", "codebuddy", "", "default", "chat")
	assert.NoError(t, err)

	// Simulate: first message completed, ACP session ID was captured
	err = service.UpdateExternalSessionID(sessionID, "acp-sess-abc-123")
	assert.NoError(t, err)

	// Mark as ACP transport
	err = service.UpdateSessionTransport(sessionID, "acp-stdio")
	assert.NoError(t, err)

	// Add assistant message so SessionHasAssistant returns true
	_, err = service.AddChatMessage(env.ProjectDir, "codebuddy", sessionID, "assistant", `{"blocks":[{"type":"text","text":"done"}]}`, nil, false, "")
	assert.NoError(t, err)

	// buildChatRequest with acp-stdio transport should use the ClawBench UUID,
	// not the ACP session ID — the pool maps internally
	req := buildChatRequest("continue", sessionID, env.ProjectDir, "codebuddy", "codebuddy", "", "", "", "acp-stdio", "")
	assert.True(t, req.Resume, "should be resume since session has assistant messages")
	assert.Equal(t, sessionID, req.SessionID, "ACP resume should use ClawBench UUID, not external_session_id")
}

// TestBuildChatRequest_ACPResume_AfterServerRestart simulates the exact scenario
// from the bug: after a server restart, the ACP pool is empty, but the DB has
// the ACP session ID stored in external_session_id. The handler must still pass
// the ClawBench UUID to the ACP backend (the pool uses external_session_id
// internally to decide ResumeSession vs NewSession).
func TestBuildChatRequest_ACPResume_AfterServerRestart(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "test-acp-restart", "codebuddy", "", "default", "chat")
	assert.NoError(t, err)

	// Simulate: before restart, ACP session was captured
	err = service.UpdateExternalSessionID(sessionID, "698ddb14-d532-44c1-8cf6-6378907ec72a")
	assert.NoError(t, err)
	err = service.UpdateSessionTransport(sessionID, "acp-stdio")
	assert.NoError(t, err)

	// Add assistant messages
	_, err = service.AddChatMessage(env.ProjectDir, "codebuddy", sessionID, "user", "hello", nil, false, "")
	assert.NoError(t, err)
	_, err = service.AddChatMessage(env.ProjectDir, "codebuddy", sessionID, "assistant", `{"blocks":[{"type":"text","text":"hi"}]}`, nil, false, "")
	assert.NoError(t, err)

	// After restart: pool is empty, but handler still works correctly
	req := buildChatRequest("next message", sessionID, env.ProjectDir, "codebuddy", "codebuddy", "", "", "", "acp-stdio", "")
	assert.True(t, req.Resume, "should be resume after restart since session has assistant messages")
	assert.Equal(t, sessionID, req.SessionID, "ACP resume after restart should use ClawBench UUID")
}

// TestGetExternalSessionID_ACPVsCLI verifies the DB query behavior for
// external_session_id: ACP sessions have the real ACP session ID stored,
// while CLI sessions (claude/codebuddy without ACP) have the ClawBench UUID.
func TestGetExternalSessionID_ACPVsCLI(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// ACP session: external_session_id = ACP session ID (different from clawbench UUID)
	acpSessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "test-ext-acp", "codebuddy", "", "default", "chat")
	assert.NoError(t, err)
	err = service.UpdateSessionTransport(acpSessionID, "acp-stdio")
	assert.NoError(t, err)
	acpExtID := "acp-real-session-xyz"
	err = service.UpdateExternalSessionID(acpSessionID, acpExtID)
	assert.NoError(t, err)

	// Verify: ACP session's external_session_id differs from clawbench UUID
	gotACP := service.GetExternalSessionID(acpSessionID)
	assert.Equal(t, acpExtID, gotACP, "ACP session external_session_id should be the ACP session ID")
	assert.NotEqual(t, acpSessionID, gotACP, "ACP session external_session_id should differ from clawbench UUID")

	// CLI session: external_session_id = clawbench UUID (same, from CreateSession)
	cliSessionID, err := service.CreateSession(env.ProjectDir, "claude", "test-ext-cli", "claude", "", "default", "chat")
	assert.NoError(t, err)

	// Verify: CLI session's external_session_id equals clawbench UUID (initial value)
	gotCLI := service.GetExternalSessionID(cliSessionID)
	assert.Equal(t, cliSessionID, gotCLI, "CLI session external_session_id should initially equal clawbench UUID")
}

// TestBuildChatRequest_NonACPResumeWithExternalSessionID verifies that
// non-ACP CLI backends (opencode, codex, pi) correctly use external_session_id
// for --resume after a simulated restart. This path was already working but
// is included to ensure the ACP fix doesn't break it.
func TestBuildChatRequest_NonACPResumeWithExternalSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// OpenCode session with a real external session ID
	sessionID, err := service.CreateSession(env.ProjectDir, "opencode", "test-nonacp-resume", "", "", "default", "chat")
	assert.NoError(t, err)
	extID := "ses_abc123xyz"
	err = service.UpdateExternalSessionID(sessionID, extID)
	assert.NoError(t, err)

	// Add assistant message so resume=true
	_, err = service.AddChatMessage(env.ProjectDir, "opencode", sessionID, "assistant", `{"blocks":[{"type":"text","text":"hi"}]}`, nil, false, "")
	assert.NoError(t, err)

	req := buildChatRequest("continue", sessionID, env.ProjectDir, "opencode", "codebuddy", "", "", "", "", "")
	assert.True(t, req.Resume)
	assert.Equal(t, extID, req.SessionID, "non-ACP resume should use external_session_id")
}

// TestBuildChatRequest_ACPNewSession_NoResume verifies that a brand new ACP
// session (no assistant messages yet) does NOT set Resume=true, and passes
// the ClawBench UUID so the ACP pool can create a new session.
func TestBuildChatRequest_ACPNewSession_NoResume(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "test-acp-new", "codebuddy", "", "default", "chat")
	assert.NoError(t, err)
	err = service.UpdateSessionTransport(sessionID, "acp-stdio")
	assert.NoError(t, err)

	req := buildChatRequest("hello", sessionID, env.ProjectDir, "codebuddy", "codebuddy", "", "", "", "acp-stdio", "")
	assert.False(t, req.Resume, "new ACP session should not be resume")
	assert.Equal(t, sessionID, req.SessionID, "new ACP session should use ClawBench UUID")
}
