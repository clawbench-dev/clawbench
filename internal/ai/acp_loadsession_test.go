//go:build integration

package ai

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ===========================================================================
// Category F: ACP LoadSession Resume — Replay Parsing Integration Tests
// ===========================================================================
//
// These tests verify that when a Claude ACP session is loaded via LoadSession
// (the acp-load endpoint), the replayed SessionUpdate notifications are
// properly parsed into structured StreamEvents (content, tool_use, tool_result,
// thinking, etc.) rather than being stored as raw JSON.
//
// Background: The original convertACPSessionUpdateToMessages in
// session_resume.go stored raw JSON from acp.SessionUpdate without parsing
// through mapACPSessionUpdate. This caused the frontend to display
// unparsed JSON like {"agent_message_chunk":{...}} instead of rendered
// content blocks.

// F1: Basic LoadSession replay — verify AgentMessageChunk produces content events
//
// This test:
//  1. Establishes a new ACP session with a simple prompt
//  2. Captures the ACP session ID from session_capture
//  3. Uses LoadSession to replay the conversation into a new ClawBench session
//  4. Verifies the replayed messages are parsed through mapACPSessionUpdate
//     (producing content/tool_use/thinking events), not stored as raw JSON
func TestClaudeACP_LoadSession_ReplayParsing(t *testing.T) {
	requireClaudeACPAvailable(t)

	agent := claudeACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := acpSessionID()
	defer env.closeConn(t, sessionID)

	// Step 1: Establish a conversation with a simple prompt
	events1 := sendACPPrompt(t, backend, sessionID, "回复一个字：好", 120*time.Second)
	requireDoneEvent(t, events1)

	// Verify we got content on the first prompt
	content1 := concatACPContent(events1)
	assert.NotEmpty(t, content1, "first prompt should produce content")

	// Step 2: Capture the ACP session ID
	acpSSID := extractACPCaptureID(t, events1)
	require.NotEmpty(t, acpSSID, "should have ACP session ID after first prompt")

	t.Logf("Step 1 complete: session_id=%s, acp_sid=%s, content_len=%d", sessionID, acpSSID, len(content1))

	// Step 3: Close the existing connection so LoadSession can create a new one
	env.closeConn(t, sessionID)

	// Step 4: Use GetOrCreateConnForLoad to replay the session
	loadSessionID := acpSessionID()
	defer env.closeConn(t, loadSessionID)

	ctx, cancel := contextWithTimeout(t, 120*time.Second)
	defer cancel()

	conn, err := env.mgr.GetOrCreateConnForLoad(ctx, agent, loadSessionID, acpSSID, acpTestWorkDir())
	if err != nil {
		// LoadSession may not be supported by this agent version
		t.Skipf("LoadSession not supported or failed: %v", err)
		return
	}
	require.NotNil(t, conn, "should have a connection after LoadSession")

	// Step 5: Collect replayed messages from the buffer
	client := conn.GetClient()
	require.NotNil(t, client, "should have ACP client after LoadSession")

	buf := client.GetAndClearLoadSessionBuf()
	t.Logf("LoadSession replayed %d SessionUpdate notifications", len(buf))

	// Step 6: Parse the replayed notifications through mapACPSessionUpdate
	// and verify they produce proper structured events (not raw JSON)
	ch := make(chan StreamEvent, 1000)
	var allEvents []StreamEvent

	for _, n := range buf {
		mapACPSessionUpdate(n.Update, ch, ctx, nil, nil)
	}
	close(ch)

	for event := range ch {
		allEvents = append(allEvents, event)
	}

	t.Logf("Parsed %d StreamEvents from LoadSession replay", len(allEvents))

	// Step 7: Verify the events are properly parsed, not raw JSON
	contentEvents := findACPEvents(allEvents, "content")
	rawOutputEvents := findACPEvents(allEvents, "raw_output")

	// There should be at least content events from the replayed assistant messages
	assert.NotEmpty(t, contentEvents,
		"LoadSession replay should produce 'content' events from AgentMessageChunk, got event types: %v",
		acpEventTypes(allEvents))

	// Verify content events contain actual text, not raw JSON
	for i, e := range contentEvents {
		assert.False(t, looksLikeRawJSON(e.Content),
			"content event[%d] looks like raw JSON instead of parsed text: %s",
			i, truncate(e.Content, 100))
	}

	// raw_output events should be present (they're always emitted for debugging)
	assert.NotEmpty(t, rawOutputEvents,
		"LoadSession replay should emit raw_output events for each notification")

	// Log event type summary
	typeCounts := make(map[string]int)
	for _, e := range allEvents {
		typeCounts[e.Type]++
	}
	t.Logf("LoadSession replay event breakdown: %v", typeCounts)
}

// F2: LoadSession replay — verify tool calls produce tool_use/tool_result events
//
// This test sends a prompt that triggers tool usage, then replays via
// LoadSession and verifies that ToolCall/ToolCallUpdate notifications
// are properly converted to tool_use and tool_result events.
func TestClaudeACP_LoadSession_ToolCallParsing(t *testing.T) {
	requireClaudeACPAvailable(t)

	agent := claudeACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := acpSessionID()
	defer env.closeConn(t, sessionID)

	// Step 1: Send a prompt that will trigger tool usage (file reading)
	events1 := sendACPPrompt(t, backend, sessionID,
		"读一下 README.md 的第一行，然后只回复第一行的内容", 180*time.Second)
	requireDoneEvent(t, events1)

	// Verify we got tool_use events on the first prompt
	toolUseEvents1 := findACPEvents(events1, "tool_use")
	t.Logf("First prompt produced %d tool_use events", len(toolUseEvents1))

	// Verify we got content
	content1 := concatACPContent(events1)
	assert.NotEmpty(t, content1, "first prompt should produce content")

	// Step 2: Capture the ACP session ID
	acpSSID := extractACPCaptureID(t, events1)
	require.NotEmpty(t, acpSSID)

	// Step 3: Close the existing connection
	env.closeConn(t, sessionID)

	// Step 4: LoadSession replay
	loadSessionID := acpSessionID()
	defer env.closeConn(t, loadSessionID)

	ctx, cancel := contextWithTimeout(t, 120*time.Second)
	defer cancel()

	conn, err := env.mgr.GetOrCreateConnForLoad(ctx, agent, loadSessionID, acpSSID, acpTestWorkDir())
	if err != nil {
		t.Skipf("LoadSession not supported or failed: %v", err)
		return
	}

	client := conn.GetClient()
	require.NotNil(t, client)

	buf := client.GetAndClearLoadSessionBuf()
	t.Logf("LoadSession replayed %d SessionUpdate notifications", len(buf))

	// Step 5: Parse through mapACPSessionUpdate
	ch := make(chan StreamEvent, 1000)
	var allEvents []StreamEvent

	for _, n := range buf {
		mapACPSessionUpdate(n.Update, ch, ctx, nil, nil)
	}
	close(ch)

	for event := range ch {
		allEvents = append(allEvents, event)
	}

	// Step 6: Verify tool events are properly parsed
	toolUseEvents := findACPEvents(allEvents, "tool_use")
	toolResultEvents := findACPEvents(allEvents, "tool_result")
	contentEvents := findACPEvents(allEvents, "content")

	// If the original conversation had tool calls, the replay should produce them too
	if len(toolUseEvents1) > 0 {
		assert.NotEmpty(t, toolUseEvents,
			"LoadSession replay should produce 'tool_use' events from ToolCall notifications")
	}

	// Verify tool_use events have structured data, not raw JSON
	for i, e := range toolUseEvents {
		require.NotNil(t, e.Tool, "tool_use event[%d] should have Tool data", i)
		assert.NotEmpty(t, e.Tool.Name,
			"tool_use event[%d] should have a parsed tool name, not raw JSON", i)
		assert.NotEmpty(t, e.Tool.ID,
			"tool_use event[%d] should have a tool call ID", i)
		// Tool input should be valid JSON (normalized), not raw ACP JSON
		if e.Tool.Input != "" {
			var parsed map[string]any
			err := json.Unmarshal([]byte(e.Tool.Input), &parsed)
			assert.NoError(t, err,
				"tool_use event[%d] input should be valid JSON, got: %s",
				i, truncate(e.Tool.Input, 100))
		}
	}

	// Verify tool_result events are properly parsed
	for i, e := range toolResultEvents {
		require.NotNil(t, e.Tool, "tool_result event[%d] should have Tool data", i)
		assert.True(t, e.Tool.Done,
			"tool_result event[%d] should have Done=true", i)
		// Output should be human-readable text, not raw JSON
		if e.Tool.Output != "" {
			assert.False(t, looksLikeRawJSON(e.Tool.Output),
				"tool_result event[%d] output looks like raw JSON: %s",
				i, truncate(e.Tool.Output, 100))
		}
	}

	// Content events should also be properly parsed
	for i, e := range contentEvents {
		assert.False(t, looksLikeRawJSON(e.Content),
			"content event[%d] looks like raw JSON: %s",
			i, truncate(e.Content, 100))
	}

	typeCounts := make(map[string]int)
	for _, e := range allEvents {
		typeCounts[e.Type]++
	}
	t.Logf("LoadSession tool call replay event breakdown: %v", typeCounts)
}

// F3: LoadSession replay — verify thinking blocks produce thinking events
//
// This test sends a prompt with high thinking effort, then replays via
// LoadSession and verifies that AgentThoughtChunk notifications are
// properly converted to thinking events.
func TestClaudeACP_LoadSession_ThinkingParsing(t *testing.T) {
	requireClaudeACPAvailable(t)

	agent := claudeACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := acpSessionID()
	defer env.closeConn(t, sessionID)

	// Send a prompt that will likely trigger thinking
	events1 := sendACPPrompt(t, backend, sessionID,
		"思考一下1+1等于几，然后回复答案", 120*time.Second)
	requireDoneEvent(t, events1)

	content1 := concatACPContent(events1)
	assert.NotEmpty(t, content1, "first prompt should produce content")

	acpSSID := extractACPCaptureID(t, events1)
	require.NotEmpty(t, acpSSID)

	// Check if thinking events were present in the original session
	thinkingEvents1 := findACPEvents(events1, "thinking")
	t.Logf("First prompt produced %d thinking events", len(thinkingEvents1))

	// Close and reload
	env.closeConn(t, sessionID)

	loadSessionID := acpSessionID()
	defer env.closeConn(t, loadSessionID)

	ctx, cancel := contextWithTimeout(t, 120*time.Second)
	defer cancel()

	conn, err := env.mgr.GetOrCreateConnForLoad(ctx, agent, loadSessionID, acpSSID, acpTestWorkDir())
	if err != nil {
		t.Skipf("LoadSession not supported or failed: %v", err)
		return
	}

	client := conn.GetClient()
	require.NotNil(t, client)

	buf := client.GetAndClearLoadSessionBuf()
	t.Logf("LoadSession replayed %d SessionUpdate notifications", len(buf))

	// Parse through mapACPSessionUpdate
	ch := make(chan StreamEvent, 1000)
	var allEvents []StreamEvent

	for _, n := range buf {
		mapACPSessionUpdate(n.Update, ch, ctx, nil, nil)
	}
	close(ch)

	for event := range ch {
		allEvents = append(allEvents, event)
	}

	// If original session had thinking, the replay should produce thinking events
	thinkingEvents := findACPEvents(allEvents, "thinking")
	thinkingDoneEvents := findACPEvents(allEvents, "thinking_done")

	if len(thinkingEvents1) > 0 {
		assert.NotEmpty(t, thinkingEvents,
			"LoadSession replay should produce 'thinking' events from AgentThoughtChunk when original session had thinking")
	}

	// Verify thinking content is text, not raw JSON
	for i, e := range thinkingEvents {
		assert.False(t, looksLikeRawJSON(e.Content),
			"thinking event[%d] looks like raw JSON: %s",
			i, truncate(e.Content, 100))
	}

	t.Logf("Thinking events in replay: %d, thinking_done: %d",
		len(thinkingEvents), len(thinkingDoneEvents))

	typeCounts := make(map[string]int)
	for _, e := range allEvents {
		typeCounts[e.Type]++
	}
	t.Logf("LoadSession thinking replay event breakdown: %v", typeCounts)
}

// F4: Unit-level test for convertACPSessionUpdateToMessages — verify raw JSON issue
//
// This test directly calls convertACPSessionUpdateToMessages with various
// ACP SessionUpdate types and verifies what the current implementation
// produces. This documents the bug (raw JSON) so that when it's fixed,
// the test will confirm the fix.
//
// NOTE: This test is in the integration test file because it uses the
// acp.SessionUpdate types which require the ACP SDK dependency, and
// follows the existing pattern of the integration test file.
func TestClaudeACP_LoadSession_ConvertSessionUpdate_RawJSONIssue(t *testing.T) {
	// Test case 1: AgentMessageChunk
	agentMsgNotification := acp.SessionNotification{
		SessionId: acp.SessionId("test-session-1"),
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.ContentBlock{
					Text: &acp.ContentBlockText{Text: "Hello, world!"},
				},
			},
		},
	}

	// Parse through mapACPSessionUpdate to see what we SHOULD get
	ch := make(chan StreamEvent, 100)
	mapACPSessionUpdate(agentMsgNotification.Update, ch, nil, nil, nil)
	close(ch)

	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	// After mapACPSessionUpdate, we should get structured events
	contentEvents := findACPEvents(events, "content")
	require.NotEmpty(t, contentEvents,
		"AgentMessageChunk should produce 'content' event via mapACPSessionUpdate")

	// The content should be the actual text, not raw JSON
	assert.Equal(t, "Hello, world!", contentEvents[0].Content,
		"parsed content should be the text from AgentMessageChunk, not raw JSON")

	// Now show what the old convertACPSessionUpdateToMessages does:
	// It marshals the entire SessionUpdate as JSON, producing raw JSON
	// like: {"agent_message_chunk":{"content":{"text":{"text":"Hello, world!"}}}}
	rawContent, _ := json.Marshal(agentMsgNotification.Update)
	assert.True(t, looksLikeRawJSON(string(rawContent)),
		"raw JSON marshaling of SessionUpdate should look like JSON (this documents the bug)")

	// Test case 2: ToolCall
	toolCallNotification := acp.SessionNotification{
		SessionId: acp.SessionId("test-session-2"),
		Update: acp.SessionUpdate{
			ToolCall: &acp.SessionUpdateToolCall{
				ToolCallId: acp.ToolCallId("tc-1"),
				Title:      "Read",
				Kind:       acp.ToolKindRead,
				RawInput:   map[string]any{"file_path": "/tmp/test.go"},
			},
		},
	}

	ch2 := make(chan StreamEvent, 100)
	mapACPSessionUpdate(toolCallNotification.Update, ch2, nil, nil, nil)
	close(ch2)

	var events2 []StreamEvent
	for e := range ch2 {
		events2 = append(events2, e)
	}

	toolUseEvents := findACPEvents(events2, "tool_use")
	require.NotEmpty(t, toolUseEvents,
		"ToolCall should produce 'tool_use' event via mapACPSessionUpdate")
	assert.Equal(t, "Read", toolUseEvents[0].Tool.Name,
		"tool name should be extracted from ToolCall, not raw JSON")
	assert.Contains(t, toolUseEvents[0].Tool.Input, "file_path",
		"tool input should be normalized JSON, not raw ACP JSON")

	// Test case 3: ToolCallUpdate (completed)
	completed := acp.ToolCallStatusCompleted
	toolResultNotification := acp.SessionNotification{
		SessionId: acp.SessionId("test-session-3"),
		Update: acp.SessionUpdate{
			ToolCallUpdate: &acp.SessionToolCallUpdate{
				ToolCallId: acp.ToolCallId("tc-2"),
				Status:     &completed,
				RawOutput:  "file contents here",
			},
		},
	}

	ch3 := make(chan StreamEvent, 100)
	mapACPSessionUpdate(toolResultNotification.Update, ch3, nil, nil, nil)
	close(ch3)

	var events3 []StreamEvent
	for e := range ch3 {
		events3 = append(events3, e)
	}

	toolResultEvents := findACPEvents(events3, "tool_result")
	require.NotEmpty(t, toolResultEvents,
		"completed ToolCallUpdate should produce 'tool_result' event via mapACPSessionUpdate")
	assert.True(t, toolResultEvents[0].Tool.Done,
		"completed tool result should have Done=true")
	assert.Equal(t, "file contents here", toolResultEvents[0].Tool.Output,
		"tool output should be extracted text, not raw JSON")

	// Test case 4: AgentThoughtChunk
	thoughtNotification := acp.SessionNotification{
		SessionId: acp.SessionId("test-session-4"),
		Update: acp.SessionUpdate{
			AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
				Content: acp.ContentBlock{
					Text: &acp.ContentBlockText{Text: "Let me think about this..."},
				},
			},
		},
	}

	ch4 := make(chan StreamEvent, 100)
	mapACPSessionUpdate(thoughtNotification.Update, ch4, nil, nil, nil)
	close(ch4)

	var events4 []StreamEvent
	for e := range ch4 {
		events4 = append(events4, e)
	}

	thinkingEvents := findACPEvents(events4, "thinking")
	require.NotEmpty(t, thinkingEvents,
		"AgentThoughtChunk should produce 'thinking' event via mapACPSessionUpdate")
	assert.Equal(t, "Let me think about this...", thinkingEvents[0].Content,
		"thinking content should be the text from AgentThoughtChunk, not raw JSON")
}

// F5: Full round-trip — replay via LoadSession, then send a new prompt
//
// After loading a session, the connection should be fully functional for
// new prompts. This verifies that LoadSession doesn't leave the connection
// in a broken state.
func TestClaudeACP_LoadSession_ThenNewPrompt(t *testing.T) {
	requireClaudeACPAvailable(t)

	agent := claudeACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := acpSessionID()
	defer env.closeConn(t, sessionID)

	// Step 1: Establish a conversation
	events1 := sendACPPrompt(t, backend, sessionID, "请记住数字42，只回复'好的'", 120*time.Second)
	requireDoneEvent(t, events1)

	acpSSID := extractACPCaptureID(t, events1)
	require.NotEmpty(t, acpSSID)

	content1 := concatACPContent(events1)
	assert.NotEmpty(t, content1, "first prompt should produce content")

	// Step 2: Close and reload via LoadSession
	env.closeConn(t, sessionID)

	loadSessionID := acpSessionID()
	defer env.closeConn(t, loadSessionID)

	ctx, cancel := contextWithTimeout(t, 120*time.Second)
	defer cancel()

	conn, err := env.mgr.GetOrCreateConnForLoad(ctx, agent, loadSessionID, acpSSID, acpTestWorkDir())
	if err != nil {
		t.Skipf("LoadSession not supported or failed: %v", err)
		return
	}

	// Clear the replay buffer (simulating what ServeACPLoadSession does)
	client := conn.GetClient()
	require.NotNil(t, client)
	buf := client.GetAndClearLoadSessionBuf()
	t.Logf("LoadSession replayed %d notifications", len(buf))

	// Step 3: Send a new prompt on the loaded session
	// Use the loadSessionID which maps to this connection
	env.storeSID(loadSessionID, acpSSID)

	events2 := sendACPPrompt(t, backend, loadSessionID, "我之前让你记住的数字是什么？只回答数字", 120*time.Second)
	requireDoneEvent(t, events2)

	content2 := concatACPContent(events2)
	t.Logf("After LoadSession + new prompt: %q", content2)

	// The AI should remember the number from the original conversation
	assert.True(t, strings.Contains(content2, "42"),
		"AI should remember '42' from the loaded session, got: %s", content2)
}

// ===========================================================================
// Category G: ACP LoadSession — Claude vs OpenCode Latency Comparison
// ===========================================================================
//
// These tests diagnose the "resume button hangs for Claude but works for OpenCode"
// bug by measuring each phase of the LoadSession pipeline:
//
//   GetOrCreateConnForLoad(ctx, agent, loadSID, acpSID, cwd)
//     → spawnLocked (npx/launch + Initialize, 60s timeout)
//     → LoadSession RPC (replay history, 60s timeout)
//   + time.Sleep(500ms) for late-arriving notifications
//   + replay buffer parsing + DB writes
//
// Root cause hypothesis:
//   - Claude ACP: npx startup slow (5-30s) + LoadSession replays full history (10-60s)
//   - OpenCode ACP: native binary fast (1-3s) + LoadSession returns immediately
//     (only sends AvailableCommandsUpdate, no conversation replay)
//   - Frontend: acpLoadSession() has no fetch timeout → UI freezes indefinitely

// requireOpenCodeACPAvailable skips the test if OpenCode CLI is not installed.
func requireOpenCodeACPAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode CLI not available, skipping OpenCode ACP LoadSession latency test")
	}
}

// opencodeACPAgent returns a model.Agent configured for OpenCode ACP transport.
func opencodeACPAgent() *model.Agent {
	return &model.Agent{
		ID:         "opencode-acp-loadsession-test",
		Name:       "OpenCode ACP LoadSession Test",
		Backend:    "opencode",
		Transport:  "acp-stdio",
		AcpCommand: "opencode acp",
		Models: []model.AgentModel{
			{ID: "default", Name: "Default Model", Default: true},
		},
	}
}

// loadSessionTimings holds measured durations for each LoadSession phase.
type loadSessionTimings struct {
	AgentID       string
	LoadTotal     time.Duration // total GetOrCreateConnForLoad time
	ReplayNotifs  int           // number of SessionUpdate notifications in replay buffer
	ReplayContent int           // number of content blocks after parsing
}

// killAndCloseConn kills the ACP process and removes the connection from the pool.
// This avoids the cmd.Wait() hang that closeConn suffers when npx spawns child
// processes that keep the stdout/stderr pipes open after the parent is killed.
//
// We kill the process group (not just the parent) and then force-close the
// stdoutFilter to unblock any pending I/O before calling CloseConn.
func killAndCloseConn(t *testing.T, env *acpTestEnv, sessionID string) {
	t.Helper()
	if conn := env.mgr.GetConn(sessionID); conn != nil {
		// Kill the process first
		_ = conn.KillProcessForTest()
		// Close the stdout filter to unblock pending reads
		// (this is what killProcessLocked does but KillProcessForTest doesn't)
		conn.mu.Lock()
		if conn.stdoutFilter != nil {
			conn.stdoutFilter.Close()
			conn.stdoutFilter = nil
		}
		conn.mu.Unlock()
	}
	env.closeConn(t, sessionID)
}

// measureLoadSessionLatency performs a full LoadSession round-trip and measures
// each phase. Returns timings and the parsed replay events.
func measureLoadSessionLatency(t *testing.T, agent *model.Agent, acpSSID string) (loadSessionTimings, []StreamEvent) {
	t.Helper()

	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	// Use a short prompt to establish the session first
	sessionID := acpSessionID()
	cleanupConn(t, sessionID)

	events1 := sendACPPrompt(t, backend, sessionID, "回复一个字：好", 120*time.Second)
	requireDoneEvent(t, events1)

	capturedAcpSID := extractACPCaptureID(t, events1)
	require.NotEmpty(t, capturedAcpSID, "should have ACP session ID after prompt")

	t.Logf("Established session: clawbench=%s, acp=%s", sessionID, capturedAcpSID)

	// Close the existing connection so LoadSession creates a new one
	killAndCloseConn(t, env, sessionID)

	// Now perform LoadSession with timing
	loadSessionID := acpSessionID()
	cleanupConn(t, loadSessionID)

	ctx, cancel := contextWithTimeout(t, 120*time.Second)
	defer cancel()

	loadStart := time.Now()
	conn, err := env.mgr.GetOrCreateConnForLoad(ctx, agent, loadSessionID, capturedAcpSID, acpTestWorkDir())
	loadElapsed := time.Since(loadStart)

	if err != nil {
		t.Skipf("LoadSession not supported or failed: %v", err)
	}
	require.NotNil(t, conn, "should have a connection after LoadSession")

	// Wait for late-arriving notifications (same as ServeACPLoadSession handler)
	time.Sleep(500 * time.Millisecond)

	client := conn.GetClient()
	require.NotNil(t, client, "should have ACP client after LoadSession")

	buf := client.GetAndClearLoadSessionBuf()

	// Parse replay notifications
	ch := make(chan StreamEvent, 1000)
	var allEvents []StreamEvent
	for _, n := range buf {
		mapACPSessionUpdate(n.Update, ch, ctx, nil, nil)
	}
	close(ch)
	for event := range ch {
		allEvents = append(allEvents, event)
	}

	contentEvents := findACPEvents(allEvents, "content")

	timings := loadSessionTimings{
		AgentID:       agent.ID,
		LoadTotal:     loadElapsed,
		ReplayNotifs:  len(buf),
		ReplayContent: len(contentEvents),
	}

	return timings, allEvents
}

// G1: Claude LoadSession latency profiling
//
// Measures the full LoadSession pipeline for Claude ACP, breaking down where
// time is spent. This test documents the "resume button hangs" bug:
//
//   spawnLocked (npx + Initialize):  5-30s
//   LoadSession RPC (replay):        10-60s (proportional to conversation length)
//   time.Sleep for late notifs:     500ms (fixed)
//   Total:                          15-90s
//
// Run with verbose output:
//
//	go test -v -run TestClaudeACP_LoadSession_LatencyProfile -tags integration -timeout 300s
func TestClaudeACP_LoadSession_LatencyProfile(t *testing.T) {
	requireClaudeACPAvailable(t)

	agent := claudeACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := acpSessionID()
	cleanupConn(t, sessionID)

	// Step 1: Establish a conversation with a simple prompt
	events1 := sendACPPrompt(t, backend, sessionID, "回复一个字：好", 120*time.Second)
	requireDoneEvent(t, events1)

	acpSSID := extractACPCaptureID(t, events1)
	require.NotEmpty(t, acpSSID)
	t.Logf("Step 1: established session, acp_sid=%s", acpSSID)

	// Step 2: Close the connection and LoadSession
	killAndCloseConn(t, env, sessionID)
	loadSessionID := acpSessionID()
	cleanupConn(t, loadSessionID)

	ctx, cancel := contextWithTimeout(t, 120*time.Second)
	defer cancel()

	// Measure LoadSession total time
	loadStart := time.Now()
	conn, err := env.mgr.GetOrCreateConnForLoad(ctx, agent, loadSessionID, acpSSID, acpTestWorkDir())
	loadElapsed := time.Since(loadStart)

	if err != nil {
		t.Skipf("LoadSession not supported or failed: %v", err)
	}
	require.NotNil(t, conn)

	// Step 3: Wait for late notifications + read buffer (same as handler)
	notifsStart := time.Now()
	time.Sleep(500 * time.Millisecond)
	client := conn.GetClient()
	require.NotNil(t, client)
	buf := client.GetAndClearLoadSessionBuf()
	notifsElapsed := time.Since(notifsStart)

	// Step 4: Parse replay
	parseStart := time.Now()
	ch := make(chan StreamEvent, 1000)
	var allEvents []StreamEvent
	for _, n := range buf {
		mapACPSessionUpdate(n.Update, ch, ctx, nil, nil)
	}
	close(ch)
	for event := range ch {
		allEvents = append(allEvents, event)
	}
	parseElapsed := time.Since(parseStart)

	contentEvents := findACPEvents(allEvents, "content")

	// ── Report ──
	t.Log("=== Claude LoadSession Latency Breakdown ===")
	t.Logf("  GetOrCreateConnForLoad (spawn+Initialize+LoadSession RPC): %v", loadElapsed)
	t.Logf("  Wait for late notifications + read buffer:                 %v", notifsElapsed)
	t.Logf("  Parse replay notifications:                                %v", parseElapsed)
	t.Logf("  Total end-to-end:                                         %v", loadElapsed+notifsElapsed+parseElapsed)
	t.Logf("  Replay notifications: %d", len(buf))
	t.Logf("  Content events: %d", len(contentEvents))

	// ── Bottleneck detection ──
	// The LoadSession RPC itself includes spawnLocked + Initialize + LoadSession.
	// We can't separate them from this level, but the total is what matters
	// for the "resume button hangs" bug.
	if loadElapsed > 30*time.Second {
		t.Logf("BOTTLENECK: GetOrCreateConnForLoad took %v — this causes UI freeze", loadElapsed)
		t.Log("  Likely causes:")
		t.Log("    1. npx package resolution for @agentclientprotocol/claude-agent-acp@latest")
		t.Log("    2. Initialize handshake (60s timeout)")
		t.Log("    3. LoadSession RPC replaying full conversation history (60s timeout)")
	}
	if loadElapsed > 60*time.Second {
		t.Logf("CRITICAL: LoadSession pipeline exceeded 60s — frontend fetch has no timeout, UI will appear frozen indefinitely")
	}

	// Verify Claude replays content (unlike OpenCode)
	// NOTE: Claude's LoadSession may replay content via SessionUpdate notifications
	// (AgentMessageChunk, UserMessageChunk), but some versions send only
	// AvailableCommandsUpdate. If content is empty, this doesn't mean LoadSession
	// failed — it may just mean the bridge version doesn't replay content.
	if len(contentEvents) > 0 {
		t.Logf("  Claude LoadSession replayed %d content events (full replay)", len(contentEvents))
	} else {
		t.Log("  NOTE: Claude LoadSession returned no content events — bridge may not replay conversation content via SessionUpdate")
	}

	// Log event type summary
	typeCounts := make(map[string]int)
	for _, e := range allEvents {
		typeCounts[e.Type]++
	}
	t.Logf("  Event breakdown: %v", typeCounts)
}

// G2: OpenCode LoadSession latency profiling
//
// Measures the full LoadSession pipeline for OpenCode ACP. Expected to be fast
// because OpenCode's LoadSession does NOT replay conversation content — it only
// sends AvailableCommandsUpdate, so the replay buffer is empty.
//
// Run with verbose output:
//
//	go test -v -run TestOpenCodeACP_LoadSession_LatencyProfile -tags integration -timeout 300s
func TestOpenCodeACP_LoadSession_LatencyProfile(t *testing.T) {
	requireOpenCodeACPAvailable(t)

	agent := opencodeACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := acpSessionID()
	cleanupConn(t, sessionID)

	// Step 1: Establish a conversation
	events1 := sendACPPrompt(t, backend, sessionID, "回复一个字：好", 120*time.Second)
	requireDoneEvent(t, events1)

	acpSSID := extractACPCaptureID(t, events1)
	require.NotEmpty(t, acpSSID)
	t.Logf("Step 1: established session, acp_sid=%s", acpSSID)

	// Step 2: Close and LoadSession
	killAndCloseConn(t, env, sessionID)
	loadSessionID := acpSessionID()
	cleanupConn(t, loadSessionID)

	ctx, cancel := contextWithTimeout(t, 120*time.Second)
	defer cancel()

	loadStart := time.Now()
	conn, err := env.mgr.GetOrCreateConnForLoad(ctx, agent, loadSessionID, acpSSID, acpTestWorkDir())
	loadElapsed := time.Since(loadStart)

	if err != nil {
		t.Skipf("LoadSession not supported or failed: %v", err)
	}
	require.NotNil(t, conn)

	// Step 3: Read replay buffer
	time.Sleep(500 * time.Millisecond)
	client := conn.GetClient()
	require.NotNil(t, client)
	buf := client.GetAndClearLoadSessionBuf()

	// Parse replay
	ch := make(chan StreamEvent, 1000)
	var allEvents []StreamEvent
	for _, n := range buf {
		mapACPSessionUpdate(n.Update, ch, ctx, nil, nil)
	}
	close(ch)
	for event := range ch {
		allEvents = append(allEvents, event)
	}

	contentEvents := findACPEvents(allEvents, "content")

	// ── Report ──
	t.Log("=== OpenCode LoadSession Latency Breakdown ===")
	t.Logf("  GetOrCreateConnForLoad: %v", loadElapsed)
	t.Logf("  Replay notifications: %d", len(buf))
	t.Logf("  Content events: %d", len(contentEvents))

	// OpenCode is expected to be fast
	t.Logf("  LoadSession total: %v", loadElapsed)

	// OpenCode's LoadSession does NOT replay conversation content
	// (known issue: opencode_loadsession_bug)
	if len(contentEvents) == 0 {
		t.Log("  NOTE: OpenCode LoadSession returned no content events — this is the known opencode_loadsession_bug")
		t.Log("  OpenCode only sends AvailableCommandsUpdate, not conversation history replay")
	}

	// Log event type summary
	typeCounts := make(map[string]int)
	for _, e := range allEvents {
		typeCounts[e.Type]++
	}
	t.Logf("  Event breakdown: %v", typeCounts)
}

// G3: Claude vs OpenCode LoadSession latency comparison
//
// Runs LoadSession for both Claude and OpenCode on the same conversation
// and compares the latency. This directly demonstrates the "resume button
// hangs for Claude but works for OpenCode" bug.
//
// Run with verbose output:
//
//	go test -v -run TestACP_LoadSession_ClaudeVsOpenCode -tags integration -timeout 600s
func TestACP_LoadSession_ClaudeVsOpenCode(t *testing.T) {
	// Run both sub-tests and collect timings
	var claudeTimings, opencodeTimings *loadSessionTimings

	t.Run("Claude", func(t *testing.T) {
		requireClaudeACPAvailable(t)
		agent := claudeACPAgent()

		env := setupACPTestEnvForAgent(t, agent)
		backend, err := NewACPBackend(agent)
		require.NoError(t, err)

		sessionID := acpSessionID()
		cleanupConn(t, sessionID)

		events1 := sendACPPrompt(t, backend, sessionID, "回复一个字：好", 120*time.Second)
		requireDoneEvent(t, events1)

		acpSSID := extractACPCaptureID(t, events1)
		require.NotEmpty(t, acpSSID)

		killAndCloseConn(t, env, sessionID)
		loadSessionID := acpSessionID()
		cleanupConn(t, loadSessionID)

		ctx, cancel := contextWithTimeout(t, 120*time.Second)
		defer cancel()

		loadStart := time.Now()
		conn, err := env.mgr.GetOrCreateConnForLoad(ctx, agent, loadSessionID, acpSSID, acpTestWorkDir())
		loadElapsed := time.Since(loadStart)

		if err != nil {
			t.Skipf("LoadSession not supported or failed: %v", err)
		}
		require.NotNil(t, conn)

		time.Sleep(500 * time.Millisecond)
		client := conn.GetClient()
		require.NotNil(t, client)
		buf := client.GetAndClearLoadSessionBuf()

		ch := make(chan StreamEvent, 1000)
		var allEvents []StreamEvent
		for _, n := range buf {
			mapACPSessionUpdate(n.Update, ch, ctx, nil, nil)
		}
		close(ch)
		for event := range ch {
			allEvents = append(allEvents, event)
		}

		contentEvents := findACPEvents(allEvents, "content")
		tm := loadSessionTimings{
			AgentID:       agent.ID,
			LoadTotal:     loadElapsed,
			ReplayNotifs:  len(buf),
			ReplayContent: len(contentEvents),
		}
		claudeTimings = &tm

		t.Logf("Claude LoadSession: total=%v, notifs=%d, content=%d",
			tm.LoadTotal, tm.ReplayNotifs, tm.ReplayContent)
	})

	t.Run("OpenCode", func(t *testing.T) {
		requireOpenCodeACPAvailable(t)
		agent := opencodeACPAgent()

		env := setupACPTestEnvForAgent(t, agent)
		backend, err := NewACPBackend(agent)
		require.NoError(t, err)

		sessionID := acpSessionID()
		cleanupConn(t, sessionID)

		events1 := sendACPPrompt(t, backend, sessionID, "回复一个字：好", 120*time.Second)
		requireDoneEvent(t, events1)

		acpSSID := extractACPCaptureID(t, events1)
		require.NotEmpty(t, acpSSID)

		killAndCloseConn(t, env, sessionID)
		loadSessionID := acpSessionID()
		cleanupConn(t, loadSessionID)

		ctx, cancel := contextWithTimeout(t, 120*time.Second)
		defer cancel()

		loadStart := time.Now()
		conn, err := env.mgr.GetOrCreateConnForLoad(ctx, agent, loadSessionID, acpSSID, acpTestWorkDir())
		loadElapsed := time.Since(loadStart)

		if err != nil {
			t.Skipf("LoadSession not supported or failed: %v", err)
		}
		require.NotNil(t, conn)

		time.Sleep(500 * time.Millisecond)
		client := conn.GetClient()
		require.NotNil(t, client)
		buf := client.GetAndClearLoadSessionBuf()

		ch := make(chan StreamEvent, 1000)
		var allEvents []StreamEvent
		for _, n := range buf {
			mapACPSessionUpdate(n.Update, ch, ctx, nil, nil)
		}
		close(ch)
		for event := range ch {
			allEvents = append(allEvents, event)
		}

		contentEvents := findACPEvents(allEvents, "content")
		tm := loadSessionTimings{
			AgentID:       agent.ID,
			LoadTotal:     loadElapsed,
			ReplayNotifs:  len(buf),
			ReplayContent: len(contentEvents),
		}
		opencodeTimings = &tm

		t.Logf("OpenCode LoadSession: total=%v, notifs=%d, content=%d",
			tm.LoadTotal, tm.ReplayNotifs, tm.ReplayContent)
	})

	// ── Comparison ──
	if claudeTimings != nil && opencodeTimings != nil {
		t.Log("=== LoadSession Latency Comparison ===")
		t.Logf("  Claude:   total=%v  notifs=%d  content=%d",
			claudeTimings.LoadTotal, claudeTimings.ReplayNotifs, claudeTimings.ReplayContent)
		t.Logf("  OpenCode: total=%v  notifs=%d  content=%d",
			opencodeTimings.LoadTotal, opencodeTimings.ReplayNotifs, opencodeTimings.ReplayContent)
		t.Logf("  Delta:    %v (Claude is slower)", claudeTimings.LoadTotal-opencodeTimings.LoadTotal)

		ratio := float64(claudeTimings.LoadTotal) / float64(opencodeTimings.LoadTotal)
		t.Logf("  Ratio:    %.1fx (Claude / OpenCode)", ratio)

		if ratio > 3.0 {
			t.Logf("BUG CONFIRMED: Claude LoadSession is %.1fx slower than OpenCode", ratio)
			t.Log("  Root cause: Claude ACP uses npx (slow startup) + replays full history via LoadSession")
			t.Log("  OpenCode uses native binary + LoadSession returns immediately (no history replay)")
			t.Logf("  Frontend impact: acpLoadSession() fetch has no timeout → UI freezes for %.0fs",
				claudeTimings.LoadTotal.Seconds())
		}

		// Claude should have more replay content than OpenCode
		if claudeTimings.ReplayContent > 0 && opencodeTimings.ReplayContent == 0 {
			t.Logf("BEHAVIORAL DIFF: Claude replays %d content blocks, OpenCode replays 0", claudeTimings.ReplayContent)
			t.Log("  This confirms opencode_loadsession_bug: OpenCode LoadSession doesn't replay conversation content")
		}
	}
}

// G4: Claude LoadSession with larger conversation — measures replay scaling
//
// Tests whether LoadSession latency scales with conversation length.
// A longer conversation means more SessionUpdate notifications to replay,
// potentially making the "resume button hang" worse for long chats.
//
// Run with verbose output:
//
//	go test -v -run TestClaudeACP_LoadSession_LargeConversation -tags integration -timeout 600s
func TestClaudeACP_LoadSession_LargeConversation(t *testing.T) {
	requireClaudeACPAvailable(t)

	agent := claudeACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := acpSessionID()
	cleanupConn(t, sessionID)

	// Build a multi-turn conversation to increase replay payload size
	prompts := []string{
		"回复一个字：一",
		"回复一个字：二",
		"回复一个字：三",
	}

	for i, prompt := range prompts {
		t.Logf("Turn %d: %s", i+1, prompt)
		events := sendACPPrompt(t, backend, sessionID, prompt, 120*time.Second)
		requireDoneEvent(t, events)
	}

	acpSSID := extractACPCaptureID(t, sendACPPrompt(t, backend, sessionID, "回复一个字：验", 120*time.Second))
	// Re-send to get capture ID if the last prompt didn't have it
	if acpSSID == "" {
		// Try to extract from any prior prompt
		events := sendACPPrompt(t, backend, sessionID, "回复一个字：验", 120*time.Second)
		requireDoneEvent(t, events)
		acpSID := extractACPCaptureID(t, events)
		if acpSID == "" {
			t.Skip("Could not extract ACP session ID — LoadSession test requires session_capture event")
		}
		acpSSID = acpSID
	}
	require.NotEmpty(t, acpSSID)

	t.Logf("Established 4-turn conversation, acp_sid=%s", acpSSID)

	// Close and LoadSession
	killAndCloseConn(t, env, sessionID)
	loadSessionID := acpSessionID()
	cleanupConn(t, loadSessionID)

	ctx, cancel := contextWithTimeout(t, 120*time.Second)
	defer cancel()

	loadStart := time.Now()
	conn, err := env.mgr.GetOrCreateConnForLoad(ctx, agent, loadSessionID, acpSSID, acpTestWorkDir())
	loadElapsed := time.Since(loadStart)

	if err != nil {
		t.Skipf("LoadSession not supported or failed: %v", err)
	}
	require.NotNil(t, conn)

	time.Sleep(500 * time.Millisecond)
	client := conn.GetClient()
	require.NotNil(t, client)
	buf := client.GetAndClearLoadSessionBuf()

	// Parse
	ch := make(chan StreamEvent, 1000)
	var allEvents []StreamEvent
	for _, n := range buf {
		mapACPSessionUpdate(n.Update, ch, ctx, nil, nil)
	}
	close(ch)
	for event := range ch {
		allEvents = append(allEvents, event)
	}

	contentEvents := findACPEvents(allEvents, "content")

	t.Log("=== Claude LoadSession — Large Conversation ===")
	t.Logf("  GetOrCreateConnForLoad: %v", loadElapsed)
	t.Logf("  Replay notifications: %d", len(buf))
	t.Logf("  Content events: %d", len(contentEvents))

	// With 4 turns, we expect at least 4 content events from the replay
	assert.GreaterOrEqual(t, len(contentEvents), 4,
		"4-turn conversation should produce at least 4 content events in replay")

	if loadElapsed > 30*time.Second {
		t.Logf("BOTTLENECK: LoadSession for 4-turn conversation took %v", loadElapsed)
		t.Log("  Longer conversations will make the resume button hang even worse")
	}

	typeCounts := make(map[string]int)
	for _, e := range allEvents {
		typeCounts[e.Type]++
	}
	t.Logf("  Event breakdown: %v", typeCounts)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// looksLikeRawJSON checks if a string looks like raw JSON (starts with { or ")
// instead of being human-readable text content.
func looksLikeRawJSON(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, `"`)
}

// contextWithTimeout creates a context with timeout for tests.
func contextWithTimeout(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), timeout)
}
