package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// assertArg verifies that a --flag value pair exists in args
func assertArg(t *testing.T, args []string, name, value string) {
	t.Helper()
	for i, a := range args {
		if a == name && i+1 < len(args) && args[i+1] == value {
			return
		}
	}
	t.Errorf("expected %s %s in args", name, value)
}

// assertNotArg verifies that a flag is NOT present in args
func assertNotArg(t *testing.T, args []string, name string) {
	t.Helper()
	for _, a := range args {
		if a == name {
			t.Errorf("did not expect %s in args", name)
		}
	}
}

// --- Argument Builder Tests ---

func TestBuildQoderStreamArgs_NewSession(t *testing.T) {
	req := ChatRequest{
		Prompt:    "hello",
		SessionID: "550e8400-e29b-41d4-a716-446655440000",
		WorkDir:   "/home/user/project",
	}
	args := buildQoderStreamArgs(req)
	assert.Contains(t, args, "--print")
	assertArg(t, args, "--output-format", "stream-json")
	assertArg(t, args, "--session-id", "550e8400-e29b-41d4-a716-446655440000")
	assertArg(t, args, "--cwd", "/home/user/project")
	assert.Contains(t, args, "--dangerously-skip-permissions")
}

func TestBuildQoderStreamArgs_ResumeSession(t *testing.T) {
	req := ChatRequest{
		Prompt:    "continue",
		SessionID: "550e8400-e29b-41d4-a716-446655440000",
		WorkDir:   "/home/user/project",
		Resume:    true,
	}
	args := buildQoderStreamArgs(req)
	assertArg(t, args, "--resume", "550e8400-e29b-41d4-a716-446655440000")
	assertNotArg(t, args, "--session-id")
}

func TestBuildQoderStreamArgs_WithModel(t *testing.T) {
	req := ChatRequest{
		Prompt:    "hello",
		SessionID: "550e8400-e29b-41d4-a716-446655440000",
		Model:     "gpt-4o",
	}
	args := buildQoderStreamArgs(req)
	assertArg(t, args, "--model", "gpt-4o")
}

func TestBuildQoderStreamArgs_WithSystemPrompt(t *testing.T) {
	req := ChatRequest{
		Prompt:       "hello",
		SessionID:    "550e8400-e29b-41d4-a716-446655440000",
		SystemPrompt: "You are a helper",
	}
	args := buildQoderStreamArgs(req)
	assertArg(t, args, "--system-prompt", "You are a helper")
}

func TestBuildQoderStreamArgs_NoDisallowedTools(t *testing.T) {
	req := ChatRequest{
		Prompt:    "hello",
		SessionID: "550e8400-e29b-41d4-a716-446655440000",
	}
	args := buildQoderStreamArgs(req)
	assertNotArg(t, args, "--disallowed-tools")
}

func TestBuildQoderStreamArgs_NoWorkDir(t *testing.T) {
	req := ChatRequest{
		Prompt:    "hello",
		SessionID: "550e8400-e29b-41d4-a716-446655440000",
	}
	args := buildQoderStreamArgs(req)
	assertNotArg(t, args, "--cwd")
}

func TestBuildQoderStreamArgs_NoSessionID(t *testing.T) {
	req := ChatRequest{
		Prompt: "hello",
	}
	args := buildQoderStreamArgs(req)
	assertNotArg(t, args, "--session-id")
	assertNotArg(t, args, "--resume")
}

// --- Stream Parser Compatibility Tests ---
// These confirm that the existing StreamParser handles Qoder's stream-json output
// without modification, since Qoder uses the same format as Claude/Codebuddy.

func TestQoderStreamParser_SystemInit(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	parser := &StreamParser{}
	line := `{"type":"system","subtype":"init","apiKeySource":"none","qodercli_version":"0.2.6","session_id":"abc-123","tools":["Bash","Read"],"model":"auto"}`
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	assert.Empty(t, events, "system init should not emit events")
}

func TestQoderStreamParser_AssistantText(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	parser := &StreamParser{}
	line := `{"type":"assistant","message":{"id":"msg-1","role":"assistant","content":[{"type":"text","text":"Hello world"}]},"session_id":"s-1"}`
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	assert.Len(t, events, 1)
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "Hello world", events[0].Content)
}

func TestQoderStreamParser_AssistantThinking(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	parser := &StreamParser{}
	line := `{"type":"assistant","message":{"id":"msg-2","role":"assistant","content":[{"type":"thinking","thinking":"Let me think..."}]},"session_id":"s-1"}`
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	assert.Len(t, events, 1)
	assert.Equal(t, "thinking", events[0].Type)
	assert.Equal(t, "Let me think...", events[0].Content)
}

func TestQoderStreamParser_AssistantToolUse(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	parser := &StreamParser{}
	line := `{"type":"assistant","message":{"id":"msg-3","role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"tool-1","input":{"command":"ls"}}]},"session_id":"s-1"}`
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	assert.Len(t, events, 1)
	assert.Equal(t, "tool_use", events[0].Type)
	assert.NotNil(t, events[0].Tool)
	assert.Equal(t, "Bash", events[0].Tool.Name)
	assert.Equal(t, "tool-1", events[0].Tool.ID)
	assert.True(t, events[0].Tool.Done)
}

func TestQoderStreamParser_Result(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	parser := &StreamParser{}
	line := `{"type":"result","subtype":"success","is_error":false,"session_id":"s-1","duration_ms":1000,"total_cost_usd":0.001,"usage":{"input_tokens":100,"output_tokens":50},"stop_reason":"stop_sequence"}`
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	assert.Len(t, events, 2)
	assert.Equal(t, "metadata", events[0].Type)
	assert.Equal(t, "s-1", events[0].Meta.SessionID)
	assert.Equal(t, 1000, events[0].Meta.DurationMs)
	assert.Equal(t, 0.001, events[0].Meta.CostUSD)
	assert.Equal(t, 100, events[0].Meta.InputTokens)
	assert.Equal(t, 50, events[0].Meta.OutputTokens)
	assert.Equal(t, "done", events[1].Type)
}

func TestQoderStreamParser_ResultError(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	parser := &StreamParser{}
	line := `{"type":"result","subtype":"success","is_error":true,"result":"Not logged in","session_id":"s-2","duration_ms":0,"total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0},"stop_reason":"stop_sequence"}`
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	// Should emit warning + metadata + done
	assert.Len(t, events, 3)
	assert.Equal(t, "warning", events[0].Type)
	assert.Contains(t, events[0].Content, "Not logged in")
	assert.Equal(t, "metadata", events[1].Type)
	assert.True(t, events[1].Meta.IsError)
	assert.Equal(t, "done", events[2].Type)
}

func TestQoderStreamParser_MixedContent(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	parser := &StreamParser{}
	line := `{"type":"assistant","message":{"id":"msg-4","role":"assistant","content":[{"type":"thinking","thinking":"Hmm"},{"type":"text","text":"Here is the answer"},{"type":"tool_use","name":"Read","id":"t-1","input":{"file_path":"/tmp/a.txt"}}]},"session_id":"s-1"}`
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	// thinking + content + tool_use
	assert.Len(t, events, 3)
	assert.Equal(t, "thinking", events[0].Type)
	assert.Equal(t, "content", events[1].Type)
	assert.Equal(t, "tool_use", events[2].Type)
}

// TestQoderStreamParser_FullFlow simulates a complete Qoder session without
// incremental streaming (--include-partial-messages is not supported).
// Content arrives as complete blocks per assistant turn.
func TestQoderStreamParser_FullFlow(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"flow-1","tools":["Bash","Read","Edit"],"model":"auto"}`,
		`{"type":"assistant","message":{"id":"msg-f1","role":"assistant","content":[{"type":"thinking","thinking":"I need to check the files"}]},"session_id":"flow-1"}`,
		`{"type":"assistant","message":{"id":"msg-f2","role":"assistant","content":[{"type":"text","text":"Let me check the files."}]},"session_id":"flow-1"}`,
		`{"type":"assistant","message":{"id":"msg-f3","role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"tool-f1","input":{"command":"ls -la"}}]},"session_id":"flow-1"}`,
		`{"type":"assistant","message":{"id":"msg-f4","role":"assistant","content":[{"type":"text","text":"Here are the files in your directory."}]},"session_id":"flow-1"}`,
		`{"type":"result","subtype":"success","is_error":false,"session_id":"flow-1","duration_ms":5000,"total_cost_usd":0.01,"usage":{"input_tokens":200,"output_tokens":150},"stop_reason":"stop_sequence"}`,
	}
	for _, line := range lines {
		parser.ParseLine(line, ch)
	}
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	assert.Len(t, events, 6)
	assert.Equal(t, "thinking", events[0].Type)
	assert.Equal(t, "content", events[1].Type)
	assert.Equal(t, "tool_use", events[2].Type)
	assert.Equal(t, "content", events[3].Type)
	assert.Equal(t, "metadata", events[4].Type)
	assert.Equal(t, "flow-1", events[4].Meta.SessionID)
	assert.Equal(t, "done", events[5].Type)
}

// TestQoderStreamParser_MultiTurnFlow verifies that consecutive assistant
// messages without stream_event deltas are not suppressed by the dedup logic.
// This is the typical Qoder pattern since --include-partial-messages is absent.
func TestQoderStreamParser_MultiTurnFlow(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}
	lines := []string{
		// Turn 1: thinking + tool_use
		`{"type":"assistant","message":{"id":"msg-t1a","role":"assistant","content":[{"type":"thinking","thinking":"I should read the config"},{"type":"tool_use","name":"Read","id":"tool-t1","input":{"file_path":"/tmp/config.yaml"}}]},"session_id":"multi-1"}`,
		// Turn 2: text response after tool result
		`{"type":"assistant","message":{"id":"msg-t2a","role":"assistant","content":[{"type":"text","text":"The config looks good."}]},"session_id":"multi-1"}`,
		// Turn 3: thinking + text
		`{"type":"assistant","message":{"id":"msg-t3a","role":"assistant","content":[{"type":"thinking","thinking":"One more thing"},{"type":"text","text":"You might also want to check the env file."}]},"session_id":"multi-1"}`,
		// Result
		`{"type":"result","subtype":"success","is_error":false,"session_id":"multi-1","duration_ms":3000,"total_cost_usd":0.005,"usage":{"input_tokens":150,"output_tokens":100},"stop_reason":"stop_sequence"}`,
	}
	for _, line := range lines {
		parser.ParseLine(line, ch)
	}
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	assert.Len(t, events, 7)
	assert.Equal(t, "thinking", events[0].Type)
	assert.Equal(t, "tool_use", events[1].Type)
	assert.Equal(t, "content", events[2].Type)
	assert.Equal(t, "thinking", events[3].Type) // not suppressed
	assert.Equal(t, "content", events[4].Type)  // not suppressed
	assert.Equal(t, "metadata", events[5].Type)
	assert.Equal(t, "done", events[6].Type)
}

func TestQoderStreamParser_UnparseableLine(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	parser := &StreamParser{}
	parser.ParseLine("not json at all", ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	assert.Empty(t, events, "unparseable lines should be silently skipped")
}

// TestQoderStreamParser_ErrorDuringExecution verifies that Qoder's
// error_during_execution result type (with errors array instead of result field)
// properly emits a warning event with the error message.
func TestQoderStreamParser_ErrorDuringExecution(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	parser := &StreamParser{}
	line := `{"type":"result","subtype":"error_during_execution","duration_ms":5168,"is_error":true,"num_turns":1,"stop_reason":null,"total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0},"errors":["unknown certificate verification error"],"session_id":"6453c2ba"}`
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	// Should emit warning + metadata + done (same as ResultError but using errors[] field)
	assert.Len(t, events, 3)
	assert.Equal(t, "warning", events[0].Type)
	assert.Contains(t, events[0].Content, "unknown certificate verification error")
	assert.Equal(t, "metadata", events[1].Type)
	assert.True(t, events[1].Meta.IsError)
	assert.Contains(t, events[1].Meta.ErrorMessage, "unknown certificate verification error")
	assert.Equal(t, "done", events[2].Type)
}

// TestQoderStreamParser_ResultWithBothResultAndErrors verifies that when both
// result and errors fields are present, result takes priority (Claude/Codebuddy behavior).
func TestQoderStreamParser_ResultWithBothResultAndErrors(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	parser := &StreamParser{}
	line := `{"type":"result","subtype":"success","is_error":true,"result":"Model overloaded","errors":["rate_limit"],"session_id":"s-3","duration_ms":0,"total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0},"stop_reason":"stop_sequence"}`
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	assert.Len(t, events, 3)
	assert.Equal(t, "warning", events[0].Type)
	assert.Equal(t, "Model overloaded", events[0].Content, "result field should take priority over errors")
}
