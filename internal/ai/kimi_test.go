package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKimiBackend_Fields(t *testing.T) {
	b := kimiBackend()

	assert.Equal(t, "kimi", b.Name())
	assert.Equal(t, "kimi", b.defaultCommand)
	assert.NotNil(t, b.buildArgs)
	assert.NotNil(t, b.newParser)
	assert.NotNil(t, b.filterLine, "kimi should have filterSkipNonJSON filter")
	assert.Nil(t, b.preStart, "kimi does not use preStart")

	// Verify parser is a GeminiStreamParser
	parser := b.newParser()
	assert.IsType(t, &GeminiStreamParser{}, parser)

	// Verify filterLine skips non-JSON lines
	line, ok := b.filterLine("")
	assert.False(t, ok, "empty line should be filtered")
	assert.Empty(t, line)

	_, ok = b.filterLine("not json")
	assert.False(t, ok, "non-JSON line should be filtered")

	line, ok = b.filterLine(`{"type":"result"}`)
	assert.True(t, ok, "JSON line should pass filter")
	assert.Equal(t, `{"type":"result"}`, line)

	// Non-JSON starting with letter
	_, ok = b.filterLine("info: processing request")
	assert.False(t, ok, "non-JSON info line should be filtered")
}

// --- Kimi uses GeminiStreamParser. Test that Kimi's parser correctly
// handles the Gemini-family stream-json output format. ---

func TestKimiStreamParser_Init(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"init","timestamp":"2026-05-01T10:00:00.000Z","session_id":"kimi-sess-1","model":"moonshot-v1"}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.Empty(t, events, "init events should not emit stream events")
}

func TestKimiStreamParser_AssistantMessage(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"message","timestamp":"2026-05-01T10:00:01.000Z","role":"assistant","content":"Hello from Kimi!","delta":true}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "Hello from Kimi!", events[0].Content)
}

func TestKimiStreamParser_UserMessageSkipped(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"message","timestamp":"2026-05-01T10:00:00.000Z","role":"user","content":"Read the file"}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.Empty(t, events, "user messages should be skipped")
}

func TestKimiStreamParser_AssistantEmptyContent(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"message","timestamp":"2026-05-01T10:00:01.000Z","role":"assistant","content":"","delta":true}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.Empty(t, events, "empty assistant content should produce no events")
}

func TestKimiStreamParser_ToolUse(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"tool_use","timestamp":"2026-05-01T10:00:02.000Z","tool_name":"read_file","tool_id":"call_k1","parameters":{"filePath":"main.go"}}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, "tool_use", events[0].Type)
	require.NotNil(t, events[0].Tool)
	assert.Equal(t, "Read", events[0].Tool.Name, "tool name should be normalized")
	assert.Equal(t, "call_k1", events[0].Tool.ID)
	assert.True(t, events[0].Tool.Done, "Gemini-format tool_use should have Done=true")
}

func TestKimiStreamParser_ToolUseEmptyParams(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"tool_use","timestamp":"2026-05-01T10:00:02.000Z","tool_name":"list_files","tool_id":"call_k2"}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, "tool_use", events[0].Type)
	assert.Equal(t, "{}", events[0].Tool.Input, "missing parameters should default to {}")
}

func TestKimiStreamParser_ToolResult(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"tool_result","timestamp":"2026-05-01T10:00:03.000Z","tool_id":"call_k1","status":"success","output":"file content"}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, "tool_result", events[0].Type)
	require.NotNil(t, events[0].Tool)
	assert.Equal(t, "call_k1", events[0].Tool.ID)
	assert.Equal(t, "file content", events[0].Tool.Output)
	assert.Equal(t, "success", events[0].Tool.Status)
}

func TestKimiStreamParser_ToolResultEmptyToolID(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"tool_result","tool_id":"","output":"result","status":"success"}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.Empty(t, events, "tool_result with empty tool_id should be skipped")
}

func TestKimiStreamParser_ErrorWarning(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"error","severity":"warning","message":"Loop detected"}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, "warning", events[0].Type)
	assert.Equal(t, "Loop detected", events[0].Content)
}

func TestKimiStreamParser_ErrorError(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"error","severity":"error","message":"Session expired"}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, "error", events[0].Type)
	assert.Equal(t, "Session expired", events[0].Error)
}

func TestKimiStreamParser_ErrorEmptyMessage(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"error","severity":"error","message":""}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.Empty(t, events, "error with empty message should be skipped")
}

func TestKimiStreamParser_ResultSuccess(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"result","timestamp":"2026-05-01T10:00:05.000Z","status":"success","stats":{"total_tokens":500,"input_tokens":400,"output_tokens":100,"cached":0,"input":400,"duration_ms":2000,"tool_calls":1,"models":{}}}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 2)
	assert.Equal(t, "metadata", events[0].Type)
	require.NotNil(t, events[0].Meta)
	assert.Equal(t, 400, events[0].Meta.InputTokens)
	assert.Equal(t, 100, events[0].Meta.OutputTokens)
	assert.Equal(t, 2000, events[0].Meta.DurationMs)
	assert.Equal(t, "stop", events[0].Meta.StopReason)
	assert.False(t, events[0].Meta.IsError)

	assert.Equal(t, "done", events[1].Type)
}

func TestKimiStreamParser_ResultError(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"result","status":"error","error":{"type":"FatalError","message":"API key invalid"},"stats":{"total_tokens":0,"input_tokens":0,"output_tokens":0,"cached":0,"input":0,"duration_ms":0,"tool_calls":0,"models":{}}}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Error result: warning + metadata + done
	require.Len(t, events, 3)
	assert.Equal(t, "warning", events[0].Type)
	assert.Equal(t, "API key invalid", events[0].Content)

	assert.Equal(t, "metadata", events[1].Type)
	require.NotNil(t, events[1].Meta)
	assert.True(t, events[1].Meta.IsError)
	assert.Equal(t, "API key invalid", events[1].Meta.ErrorMessage)

	assert.Equal(t, "done", events[2].Type)
}

func TestKimiStreamParser_UnparseableLine(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	parser.ParseLine("not json at all", ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.Empty(t, events, "unparseable lines should produce no events")
}

func TestKimiStreamParser_UnknownType(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	line := `{"type":"custom_event","timestamp":"2026-05-01T10:00:00.000Z"}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.Empty(t, events, "unknown message type should produce no events")
}

func TestKimiStreamParser_GetCapturedSessionID(t *testing.T) {
	parser := &GeminiStreamParser{}
	assert.Equal(t, "", parser.GetCapturedSessionID(), "GeminiStreamParser always returns empty for GetCapturedSessionID")

	// Even after parsing an init event with session_id
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"init","session_id":"kimi-sess","model":"moonshot-v1"}`, ch)
	assert.Equal(t, "", parser.GetCapturedSessionID(), "GetCapturedSessionID should always return empty for GeminiStreamParser")
}

func TestKimiStreamParser_SessionIDCapture(t *testing.T) {
	parser := &GeminiStreamParser{}
	ch := make(chan StreamEvent, 64)

	// Init captures session ID and model
	line1 := `{"type":"init","timestamp":"2026-05-01T10:00:00.000Z","session_id":"kimi-captured-1","model":"moonshot-v1"}`
	parser.ParseLine(line1, ch)

	// Result uses the captured session ID in metadata
	line2 := `{"type":"result","status":"success","stats":{"total_tokens":100,"input_tokens":80,"output_tokens":20,"cached":0,"input":80,"duration_ms":1000,"tool_calls":0,"models":{}}}`
	parser.ParseLine(line2, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 2)
	assert.Equal(t, "metadata", events[0].Type)
	assert.Equal(t, "kimi-captured-1", events[0].Meta.SessionID)
	assert.Equal(t, "moonshot-v1", events[0].Meta.Model)
}

func TestKimiStreamParser_FullFlow(t *testing.T) {
	lines := []string{
		`{"type":"init","timestamp":"2026-05-01T10:00:00.000Z","session_id":"kimi-flow-1","model":"moonshot-v1"}`,
		`{"type":"message","timestamp":"2026-05-01T10:00:01.000Z","role":"assistant","content":"I'll read the file.","delta":true}`,
		`{"type":"tool_use","timestamp":"2026-05-01T10:00:02.000Z","tool_name":"read_file","tool_id":"call_flow_1","parameters":{"filePath":"main.go"}}`,
		`{"type":"tool_result","timestamp":"2026-05-01T10:00:03.000Z","tool_id":"call_flow_1","status":"success","output":"package main"}`,
		`{"type":"message","timestamp":"2026-05-01T10:00:04.000Z","role":"assistant","content":"Here's the content.","delta":true}`,
		`{"type":"result","timestamp":"2026-05-01T10:00:05.000Z","status":"success","stats":{"total_tokens":500,"input_tokens":400,"output_tokens":100,"cached":0,"input":400,"duration_ms":3000,"tool_calls":1,"models":{}}}`,
	}

	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	for _, line := range lines {
		parser.ParseLine(line, ch)
	}
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Expected: content, tool_use, tool_result, content, metadata, done
	require.Len(t, events, 6)

	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "I'll read the file.", events[0].Content)

	assert.Equal(t, "tool_use", events[1].Type)
	assert.Equal(t, "Read", events[1].Tool.Name)

	assert.Equal(t, "tool_result", events[2].Type)
	assert.Equal(t, "call_flow_1", events[2].Tool.ID)

	assert.Equal(t, "content", events[3].Type)
	assert.Equal(t, "Here's the content.", events[3].Content)

	assert.Equal(t, "metadata", events[4].Type)
	assert.Equal(t, "kimi-flow-1", events[4].Meta.SessionID)
	assert.Equal(t, "moonshot-v1", events[4].Meta.Model)

	assert.Equal(t, "done", events[5].Type)
}
