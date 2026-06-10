package ai

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClineBackend_Fields(t *testing.T) {
	b := clineBackend()

	assert.Equal(t, "cline", b.Name())
	assert.Equal(t, "cline", b.defaultCommand)
	assert.NotNil(t, b.buildArgs)
	assert.NotNil(t, b.newParser)
	assert.Nil(t, b.filterLine, "cline uses default empty-line filter (nil)")
	assert.NotNil(t, b.preStart, "cline should set preStart for stdin injection")

	// Verify parser is a StreamParser
	parser := b.newParser()
	assert.IsType(t, &StreamParser{}, parser)

	// Verify preStart sets stdin from prompt
	cmd := fakeCmd()
	req := ChatRequest{Prompt: "hello from cline"}
	b.preStart(cmd, req)
	assert.NotNil(t, cmd.Stdin)
	stdinReader, ok := cmd.Stdin.(*strings.Reader)
	require.True(t, ok, "Stdin should be a strings.Reader")
	content, err := io.ReadAll(stdinReader)
	require.NoError(t, err)
	assert.Equal(t, "hello from cline", string(content))
}

// --- buildClineStreamArgs tests ---

func TestBuildClineStreamArgs_BasicPrompt(t *testing.T) {
	args := buildClineStreamArgs(ChatRequest{Prompt: "hello world"})

	assert.Contains(t, args, "--json")
	assert.Contains(t, args, "--auto-approve")
	assert.Contains(t, args, "true")
}

func TestBuildClineStreamArgs_WithResume(t *testing.T) {
	args := buildClineStreamArgs(ChatRequest{
		Prompt:    "follow-up",
		SessionID: "sess-123",
		Resume:    true,
	})

	found := false
	for i, a := range args {
		if a == "--id" && i+1 < len(args) && args[i+1] == "sess-123" {
			found = true
		}
	}
	assert.True(t, found, "expected --id sess-123 in args")
}

func TestBuildClineStreamArgs_SessionIDWithoutResume(t *testing.T) {
	args := buildClineStreamArgs(ChatRequest{
		Prompt:    "hello",
		SessionID: "sess-123",
		Resume:    false,
	})

	for _, a := range args {
		if a == "--id" {
			t.Error("should not have --id when Resume=false")
		}
	}
}

func TestBuildClineStreamArgs_EmptySessionIDWithResume(t *testing.T) {
	args := buildClineStreamArgs(ChatRequest{
		Prompt:    "hello",
		SessionID: "",
		Resume:    true,
	})

	for _, a := range args {
		if a == "--id" {
			t.Error("should not have --id when SessionID is empty even if Resume=true")
		}
	}
}

func TestBuildClineStreamArgs_WithWorkDir(t *testing.T) {
	args := buildClineStreamArgs(ChatRequest{
		Prompt:  "hello",
		WorkDir: "/tmp/project",
	})

	found := false
	for i, a := range args {
		if a == "--cwd" && i+1 < len(args) && args[i+1] == "/tmp/project" {
			found = true
		}
	}
	assert.True(t, found, "expected --cwd /tmp/project in args")
}

func TestBuildClineStreamArgs_WithoutWorkDir(t *testing.T) {
	args := buildClineStreamArgs(ChatRequest{Prompt: "hello"})

	for _, a := range args {
		if a == "--cwd" {
			t.Error("should not have --cwd when WorkDir is empty")
		}
	}
}

func TestBuildClineStreamArgs_WithModel(t *testing.T) {
	args := buildClineStreamArgs(ChatRequest{
		Prompt: "hello",
		Model:  "claude-sonnet-4-6",
	})

	found := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "claude-sonnet-4-6" {
			found = true
		}
	}
	assert.True(t, found, "expected --model claude-sonnet-4-6 in args")
}

func TestBuildClineStreamArgs_WithoutModel(t *testing.T) {
	args := buildClineStreamArgs(ChatRequest{Prompt: "hello"})

	for _, a := range args {
		if a == "--model" {
			t.Error("should not have --model when Model is empty")
		}
	}
}

func TestBuildClineStreamArgs_WithThinkingEffort(t *testing.T) {
	args := buildClineStreamArgs(ChatRequest{
		Prompt:         "hello",
		ThinkingEffort: "high",
	})

	found := false
	for i, a := range args {
		if a == "--thinking" && i+1 < len(args) && args[i+1] == "high" {
			found = true
		}
	}
	assert.True(t, found, "expected --thinking high in args")
}

func TestBuildClineStreamArgs_WithoutThinkingEffort(t *testing.T) {
	args := buildClineStreamArgs(ChatRequest{Prompt: "hello"})

	for _, a := range args {
		if a == "--thinking" {
			t.Error("should not have --thinking when ThinkingEffort is empty")
		}
	}
}

func TestBuildClineStreamArgs_AllOptions(t *testing.T) {
	args := buildClineStreamArgs(ChatRequest{
		Prompt:         "fix the bug",
		SessionID:      "sess-456",
		Resume:         true,
		WorkDir:        "/home/user/project",
		Model:          "claude-sonnet-4-6",
		ThinkingEffort: "medium",
	})

	argsStr := strings.Join(args, " ")
	assert.Contains(t, argsStr, "--id sess-456")
	assert.Contains(t, argsStr, "--cwd /home/user/project")
	assert.Contains(t, argsStr, "--model claude-sonnet-4-6")
	assert.Contains(t, argsStr, "--thinking medium")
}

func TestBuildClineStreamArgs_Minimal(t *testing.T) {
	args := buildClineStreamArgs(ChatRequest{Prompt: "hello"})

	expected := []string{"--json", "--auto-approve", "true"}
	require.Equal(t, len(expected), len(args), "minimal args should only contain base flags")
	for i, v := range expected {
		assert.Equal(t, v, args[i], "arg %d mismatch", i)
	}
}

// --- StreamParser tests for Cline ---
// Cline uses StreamParser (Claude-family format). Test that the parser
// correctly handles the JSON output format Cline produces with --json.

func TestClineStreamParser_AssistantText(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"I'll help you with that."}]}}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "I'll help you with that.", events[0].Content)
}

func TestClineStreamParser_AssistantToolUse(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_1","name":"read_file","input":{"filePath":"/tmp/test.go"}}]}}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, "tool_use", events[0].Type)
	require.NotNil(t, events[0].Tool)
	assert.Equal(t, "read_file", events[0].Tool.Name, "StreamParser passes tool names as-is in assistant messages")
	assert.Equal(t, "tool_1", events[0].Tool.ID)
	assert.True(t, events[0].Tool.Done)
}

func TestClineStreamParser_ResultSuccess(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}
	line := `{"type":"result","session_id":"sess-cline-1","duration_ms":5000,"total_cost_usd":0.01,"stop_reason":"stop","usage":{"input_tokens":400,"output_tokens":100}}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 2)
	assert.Equal(t, "metadata", events[0].Type)
	require.NotNil(t, events[0].Meta)
	assert.Equal(t, "sess-cline-1", events[0].Meta.SessionID)
	assert.Equal(t, 400, events[0].Meta.InputTokens)
	assert.Equal(t, 100, events[0].Meta.OutputTokens)
	assert.Equal(t, 5000, events[0].Meta.DurationMs)
	assert.Equal(t, "stop", events[0].Meta.StopReason)
	assert.False(t, events[0].Meta.IsError)

	assert.Equal(t, "done", events[1].Type)
}

func TestClineStreamParser_ResultError(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}
	line := `{"type":"result","is_error":true,"result":"Authentication failed","session_id":"sess-cline-err"}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Error result: warning + metadata + done
	require.Len(t, events, 3)
	assert.Equal(t, "warning", events[0].Type)
	assert.Equal(t, "Authentication failed", events[0].Content)

	assert.Equal(t, "metadata", events[1].Type)
	require.NotNil(t, events[1].Meta)
	assert.True(t, events[1].Meta.IsError)
	assert.Equal(t, "Authentication failed", events[1].Meta.ErrorMessage)

	assert.Equal(t, "done", events[2].Type)
}

func TestClineStreamParser_UnparseableLine(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}
	parser.ParseLine("not json at all", ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.Empty(t, events, "unparseable lines should produce no events")
}

func TestClineStreamParser_GetCapturedSessionID(t *testing.T) {
	parser := &StreamParser{}
	assert.Equal(t, "", parser.GetCapturedSessionID(), "StreamParser always returns empty for GetCapturedSessionID")
}

func TestClineStreamParser_CodebuddyFormatText(t *testing.T) {
	// Codebuddy-format simple text subtype (no message field)
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}
	line := `{"type":"assistant","subtype":"text","text":"Simple text output"}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "Simple text output", events[0].Content)
}

func TestClineStreamParser_FullFlow(t *testing.T) {
	lines := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Let me read that file."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_1","name":"read_file","input":{"filePath":"main.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_result","tool_use_id":"tool_1","content":"package main\n\nfunc main() {}"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"The file contains a simple main package."}]}}`,
		`{"type":"result","session_id":"sess-flow","duration_ms":3000,"usage":{"input_tokens":300,"output_tokens":50}}`,
	}

	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}
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
	assert.Equal(t, "Let me read that file.", events[0].Content)

	assert.Equal(t, "tool_use", events[1].Type)
	assert.Equal(t, "read_file", events[1].Tool.Name)

	assert.Equal(t, "tool_result", events[2].Type)
	assert.Equal(t, "tool_1", events[2].Tool.ID)

	assert.Equal(t, "content", events[3].Type)
	assert.Equal(t, "The file contains a simple main package.", events[3].Content)

	assert.Equal(t, "metadata", events[4].Type)
	assert.Equal(t, "sess-flow", events[4].Meta.SessionID)

	assert.Equal(t, "done", events[5].Type)
}

func TestClineStreamParser_StreamEventTextDelta(t *testing.T) {
	// Test stream_event format with text_delta
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}

	line := `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "Hello ", events[0].Content)
}

func TestClineStreamParser_StreamEventThinkingDelta(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}

	line := `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}}`
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, "thinking", events[0].Type)
	assert.Equal(t, "Let me think...", events[0].Content)
}

func TestClineStreamParser_StreamEventToolUseStartAndStop(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}

	// content_block_start for tool_use
	line1 := `{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_abc","name":"bash","input":{}}}}`
	parser.ParseLine(line1, ch)

	// input_json_delta
	line2 := `{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"ls\"}"}}}`
	parser.ParseLine(line2, ch)

	// content_block_stop
	line3 := `{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`
	parser.ParseLine(line3, ch)

	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Expected: tool_use (start, no input), tool_use (stop, Done=true with accumulated input)
	require.Len(t, events, 2)

	assert.Equal(t, "tool_use", events[0].Type)
	assert.Equal(t, "bash", events[0].Tool.Name)
	assert.Equal(t, "tool_abc", events[0].Tool.ID)
	assert.False(t, events[0].Tool.Done, "start event should have Done=false")

	assert.Equal(t, "tool_use", events[1].Type)
	assert.Equal(t, "tool_abc", events[1].Tool.ID)
	assert.Equal(t, `{"command":"ls"}`, events[1].Tool.Input)
	assert.True(t, events[1].Tool.Done, "stop event should have Done=true")
}

func TestClineStreamParser_StreamEventMessageStart(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}

	// message_start captures model name
	line1 := `{"type":"stream_event","event":{"type":"message_start","message":{"model":"claude-sonnet-4-6"}}}`
	parser.ParseLine(line1, ch)

	// result uses the captured model
	line2 := `{"type":"result","session_id":"sess-ms","stop_reason":"stop","usage":{"input_tokens":100,"output_tokens":50}}`
	parser.ParseLine(line2, ch)

	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 2)
	assert.Equal(t, "metadata", events[0].Type)
	assert.Equal(t, "claude-sonnet-4-6", events[0].Meta.Model)
}
