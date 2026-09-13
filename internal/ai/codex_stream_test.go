package ai

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func parseCodexLine(line string) []StreamEvent {
	ch := make(chan StreamEvent, 64)
	parser := &CodexStreamParser{}
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

// --- ParseLine tests ---

func TestCodexStream_ThreadStarted(t *testing.T) {
	line := `{"type":"thread.started","thread_id":"019dc744-1f6e-75d0-9877-99c8d2f134da"}`
	events := parseCodexLine(line)
	if len(events) != 0 {
		t.Fatalf("expected 0 events for thread.started, got %d", len(events))
	}
}

func TestCodexStream_GetCapturedSessionID(t *testing.T) {
	parser := &CodexStreamParser{}

	// Before any parsing, thread ID is empty
	if id := parser.GetCapturedSessionID(); id != "" {
		t.Errorf("expected empty thread ID before parsing, got %q", id)
	}

	// thread.started captures thread ID
	ch := make(chan StreamEvent, 64)
	parser.ParseLine(`{"type":"thread.started","thread_id":"019dc744-1f6e-75d0-9877-99c8d2f134da"}`, ch)
	if id := parser.GetCapturedSessionID(); id != "019dc744-1f6e-75d0-9877-99c8d2f134da" {
		t.Errorf("expected thread ID after thread.started, got %q", id)
	}
}

func TestCodexStream_AgentMessageTextOnly(t *testing.T) {
	line := `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Hello, world!"}}`
	events := parseCodexLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "content" {
		t.Errorf("expected content event, got %s", events[0].Type)
	}
	if events[0].Content != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", events[0].Content)
	}
}

func TestCodexStream_AgentMessageWithThinking(t *testing.T) {
	line := `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Let me think about this.\n\nHere is my answer."}}`
	events := parseCodexLine(line)

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "thinking" {
		t.Errorf("expected thinking event, got %s", events[0].Type)
	}
	if events[0].Content != "Let me think about this." {
		t.Errorf("expected thinking content, got %q", events[0].Content)
	}
	if events[1].Type != "content" {
		t.Errorf("expected content event, got %s", events[1].Type)
	}
	if events[1].Content != "Here is my answer." {
		t.Errorf("expected content, got %q", events[1].Content)
	}
}

func TestCodexStream_AgentMessageOnlyThinking(t *testing.T) {
	// Text ends with \n\n but no content after it
	line := `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Just thinking...\n\n"}}`
	events := parseCodexLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event (thinking only), got %d", len(events))
	}
	if events[0].Type != "thinking" {
		t.Errorf("expected thinking event, got %s", events[0].Type)
	}
	if events[0].Content != "Just thinking..." {
		t.Errorf("expected 'Just thinking...', got %q", events[0].Content)
	}
}

func TestCodexStream_AgentMessageEmpty(t *testing.T) {
	line := `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":""}}`
	events := parseCodexLine(line)

	if len(events) != 0 {
		t.Fatalf("expected 0 events for empty text, got %d", len(events))
	}
}

func TestCodexStream_AgentMessageMultipleParagraphs(t *testing.T) {
	// Only the first \n\n splits thinking from content
	line := `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Thinking here.\n\nFirst paragraph.\n\nSecond paragraph."}}`
	events := parseCodexLine(line)

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "thinking" || events[0].Content != "Thinking here." {
		t.Errorf("expected thinking, got type=%s content=%q", events[0].Type, events[0].Content)
	}
	if events[1].Type != "content" {
		t.Errorf("expected content event, got %s", events[1].Type)
	}
	// Content should include remaining \n\n
	if events[1].Content != "First paragraph.\n\nSecond paragraph." {
		t.Errorf("expected multi-paragraph content, got %q", events[1].Content)
	}
}

func TestCodexStream_CommandExecutionStarted(t *testing.T) {
	line := `{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"bash -lc 'ls -la'","aggregated_output":"","exit_code":null,"status":"in_progress"}}`
	events := parseCodexLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "tool_use" {
		t.Errorf("expected tool_use event, got %s", events[0].Type)
	}
	if events[0].Tool.Done {
		t.Error("expected tool to not be done")
	}
	if events[0].Tool.Name != "Bash" {
		t.Errorf("expected normalized tool name 'Bash', got %q", events[0].Tool.Name)
	}
	if events[0].Tool.Input != `{"command":"bash -lc 'ls -la'"}` {
		t.Errorf("expected canonical JSON input, got %q", events[0].Tool.Input)
	}
}

func TestCodexStream_CommandExecutionCompleted(t *testing.T) {
	line := `{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"bash -lc 'ls -la'","aggregated_output":"file1.txt\nfile2.txt","exit_code":0,"status":"completed"}}`
	events := parseCodexLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "tool_use" {
		t.Errorf("expected tool_use event, got %s", events[0].Type)
	}
	if !events[0].Tool.Done {
		t.Error("expected tool to be done")
	}
	if events[0].Tool.Name != "Bash" {
		t.Errorf("expected normalized tool name 'Bash', got %q", events[0].Tool.Name)
	}
}

func TestCodexStream_CommandExecutionFailed(t *testing.T) {
	line := `{"type":"item.completed","item":{"id":"item_2","type":"command_execution","command":"bash -lc 'exit 1'","aggregated_output":"error: something went wrong","exit_code":1,"status":"completed"}}`
	events := parseCodexLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if !events[0].Tool.Done {
		t.Error("expected tool to be done even on failure")
	}
	if !strings.Contains(events[0].Tool.Input, "exit 1") {
		t.Errorf("expected command in input, got %q", events[0].Tool.Input)
	}
	// Output is now in Tool.Output, not in Tool.Input
	if !strings.Contains(events[0].Tool.Output, "error: something went wrong") {
		t.Errorf("expected output in Tool.Output, got %q", events[0].Tool.Output)
	}
	if events[0].Tool.Status != "error" {
		t.Errorf("expected status 'error' for non-zero exit code, got %q", events[0].Tool.Status)
	}
}

func TestCodexStream_CommandExecutionNoOutput(t *testing.T) {
	line := `{"type":"item.completed","item":{"id":"item_3","type":"command_execution","command":"bash -lc 'true'","aggregated_output":"","exit_code":0,"status":"completed"}}`
	events := parseCodexLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	// Input should be canonical JSON with command only when no output
	if events[0].Tool.Input != `{"command":"bash -lc 'true'"}` {
		t.Errorf("expected canonical JSON input, got %q", events[0].Tool.Input)
	}
}

func TestCodexStream_ItemCompletedNilItem(t *testing.T) {
	line := `{"type":"item.completed"}`
	events := parseCodexLine(line)

	if len(events) != 0 {
		t.Fatalf("expected 0 events for nil item, got %d", len(events))
	}
}

func TestCodexStream_ItemStartedNotCommandExecution(t *testing.T) {
	// agent_message items in item.started should be ignored
	line := `{"type":"item.started","item":{"id":"item_0","type":"agent_message","text":""}}`
	events := parseCodexLine(line)

	if len(events) != 0 {
		t.Fatalf("expected 0 events for non-command item.started, got %d", len(events))
	}
}

func TestCodexStream_TurnCompleted(t *testing.T) {
	line := `{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":50,"output_tokens":200}}`
	events := parseCodexLine(line)

	if len(events) != 2 {
		t.Fatalf("expected 2 events (metadata + done), got %d", len(events))
	}
	if events[0].Type != "metadata" {
		t.Errorf("expected metadata event, got %s", events[0].Type)
	}
	if events[0].Meta.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", events[0].Meta.InputTokens)
	}
	if events[0].Meta.OutputTokens != 200 {
		t.Errorf("expected 200 output tokens, got %d", events[0].Meta.OutputTokens)
	}
	if events[1].Type != "done" {
		t.Errorf("expected done event, got %s", events[1].Type)
	}
}

func TestCodexStream_TurnCompletedNoUsage(t *testing.T) {
	line := `{"type":"turn.completed","usage":{"input_tokens":0,"cached_input_tokens":0,"output_tokens":0}}`
	events := parseCodexLine(line)

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Meta.InputTokens != 0 {
		t.Errorf("expected 0 input tokens, got %d", events[0].Meta.InputTokens)
	}
}

func TestCodexStream_UnknownType(t *testing.T) {
	line := `{"type":"unknown_event","data":"something"}`
	events := parseCodexLine(line)

	if len(events) != 0 {
		t.Fatalf("expected 0 events for unknown type, got %d", len(events))
	}
}

func TestCodexStream_UnparseableLine(t *testing.T) {
	line := `not json at all`
	events := parseCodexLine(line)

	if len(events) != 0 {
		t.Fatalf("expected 0 events for unparseable line, got %d", len(events))
	}
}

func TestCodexStream_TurnStarted(t *testing.T) {
	line := `{"type":"turn.started"}`
	events := parseCodexLine(line)

	if len(events) != 0 {
		t.Fatalf("expected 0 events for turn.started, got %d", len(events))
	}
}

// --- Multi-event / stateful tests ---

func TestCodexStream_ThreadIDCapture(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &CodexStreamParser{}

	threadLine := `{"type":"thread.started","thread_id":"019dc744-1f6e-75d0-9877-99c8d2f134da"}`
	parser.ParseLine(threadLine, ch)

	turnLine := `{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":20}}`
	parser.ParseLine(turnLine, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Meta.SessionID != "019dc744-1f6e-75d0-9877-99c8d2f134da" {
		t.Errorf("expected thread_id in metadata, got %q", events[0].Meta.SessionID)
	}
}

func TestCodexStream_FullFlow(t *testing.T) {
	ch := make(chan StreamEvent, 64)
	parser := &CodexStreamParser{}

	lines := []string{
		`{"type":"thread.started","thread_id":"019dc744-1f6e-75d0-9877-99c8d2f134da"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Hmm...\n\nLet me check the files."}}`,
		`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"ls","aggregated_output":"","exit_code":null,"status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"ls","aggregated_output":"a.go b.go","exit_code":0,"status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Here are the files."}}`,
		`{"type":"turn.completed","usage":{"input_tokens":500,"cached_input_tokens":0,"output_tokens":100}}`,
	}

	for _, line := range lines {
		parser.ParseLine(line, ch)
	}
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 7 {
		t.Fatalf("expected 7 events in full flow, got %d", len(events))
	}

	// Verify order and types
	expectedTypes := []string{"thinking", "content", "tool_use", "tool_use", "content", "metadata", "done"}
	for i, ev := range events {
		if ev.Type != expectedTypes[i] {
			t.Errorf("event %d: expected type %s, got %s", i, expectedTypes[i], ev.Type)
		}
	}

	// Verify metadata carries thread_id
	if events[5].Meta.SessionID != "019dc744-1f6e-75d0-9877-99c8d2f134da" {
		t.Errorf("expected thread_id in metadata, got %q", events[5].Meta.SessionID)
	}
	if events[5].Meta.InputTokens != 500 {
		t.Errorf("expected 500 input tokens, got %d", events[5].Meta.InputTokens)
	}
}

// --- buildCodexStreamArgs tests ---

func TestBuildCodexStreamArgs_NewSession(t *testing.T) {
	req := ChatRequest{
		Prompt:    "hello world",
		WorkDir:   "/tmp/project",
		Model:     "codex-MiniMax-M2.7",
		SessionID: "test-session",
		Resume:    false,
	}
	args := buildCodexStreamArgs(req)

	if args[0] != "--json" {
		t.Errorf("expected first arg '--json', got %q", args[0])
	}

	assertArg := func(name, value string) {
		for i, a := range args {
			if a == name && i+1 < len(args) && args[i+1] == value {
				return
			}
		}
		t.Errorf("expected %s %s in args", name, value)
	}

	assertArg("-C", "/tmp/project")
	assertArg("-m", "codex-MiniMax-M2.7")

	// Last arg should be prompt
	if args[len(args)-1] != "hello world" {
		t.Errorf("expected last arg to be prompt, got %q", args[len(args)-1])
	}
}

func TestBuildCodexStreamArgs_WithModel(t *testing.T) {
	req := ChatRequest{
		Prompt:  "test",
		WorkDir: "/tmp",
		Model:   "MiniMax-M2.5-highspeed",
	}
	args := buildCodexStreamArgs(req)

	foundModel := false
	for i, a := range args {
		if a == "-m" && i+1 < len(args) && args[i+1] == "MiniMax-M2.5-highspeed" {
			foundModel = true
		}
	}
	if !foundModel {
		t.Error("expected -m MiniMax-M2.5-highspeed in args")
	}
}

func TestBuildCodexStreamArgs_NoModel(t *testing.T) {
	req := ChatRequest{
		Prompt:  "test",
		WorkDir: "/tmp",
		Model:   "",
	}
	args := buildCodexStreamArgs(req)

	for _, a := range args {
		if a == "-m" {
			t.Error("did not expect -m flag when no model specified")
		}
	}
}

func TestBuildCodexStreamArgs_NoWorkDir(t *testing.T) {
	req := ChatRequest{
		Prompt: "test",
	}
	args := buildCodexStreamArgs(req)

	for _, a := range args {
		if a == "-C" {
			t.Error("did not expect -C flag when no workdir specified")
		}
	}
}

func TestBuildCodexStreamArgs_WithSystemPrompt(t *testing.T) {
	req := ChatRequest{
		Prompt:       "test",
		SystemPrompt: "You are a helpful coding assistant.",
	}
	args := buildCodexStreamArgs(req)

	// System prompt should be prepended to the last arg (prompt)
	lastArg := args[len(args)-1]
	if !strings.HasPrefix(lastArg, "[System Instructions: You are a helpful coding assistant.]") {
		t.Errorf("expected prompt to start with system instructions, got %q", lastArg)
	}
	if !strings.Contains(lastArg, "test") {
		t.Errorf("expected prompt to contain original prompt, got %q", lastArg)
	}
}

func TestBuildCodexStreamArgs_NoSystemPrompt(t *testing.T) {
	req := ChatRequest{
		Prompt:       "test",
		SystemPrompt: "",
	}
	args := buildCodexStreamArgs(req)

	// Without system prompt, last arg should be the prompt as-is
	lastArg := args[len(args)-1]
	if lastArg != "test" {
		t.Errorf("expected prompt to be 'test', got %q", lastArg)
	}
}

// --- buildCodexResumeArgs tests ---

func TestBuildCodexResumeArgs(t *testing.T) {
	req := ChatRequest{
		Prompt: "continue this task",
		Model:  "codex-MiniMax-M2.7",
	}
	args := buildCodexResumeArgs(req, "019dc744-1f6e-75d0-9877-99c8d2f134da")

	if args[0] != "resume" {
		t.Errorf("expected first arg 'resume', got %q", args[0])
	}

	// Resume args must include sandbox_permissions override (equivalent to --dangerously-bypass-approvals-and-sandbox)
	foundSandbox := false
	foundModelConfig := false
	foundThreadID := false
	foundPrompt := false
	for i, arg := range args {
		if arg == "-c" && i+1 < len(args) && strings.Contains(args[i+1], "sandbox_permissions") {
			foundSandbox = true
		}
		if strings.Contains(arg, "model=") {
			foundModelConfig = true
		}
		if arg == "019dc744-1f6e-75d0-9877-99c8d2f134da" {
			foundThreadID = true
		}
		if arg == "continue this task" {
			foundPrompt = true
		}
	}
	if !foundSandbox {
		t.Error("expected -c sandbox_permissions= override in resume args")
	}
	if !foundModelConfig {
		t.Error("expected -c model= override in resume args")
	}
	// model_provider should NOT be hardcoded — let Codex resolve it from its own config (ISS-049)
	for _, arg := range args {
		if strings.HasPrefix(arg, "model_provider=") {
			t.Errorf("unexpected model_provider= in resume args: %q (ISS-049: provider should not be hardcoded)", arg)
		}
	}
	if !foundThreadID {
		t.Error("expected thread_id in args")
	}
	if !foundPrompt {
		t.Error("expected prompt in args")
	}
}

func TestBuildCodexResumeArgs_NoModel(t *testing.T) {
	req := ChatRequest{
		Prompt: "test",
	}
	args := buildCodexResumeArgs(req, "thread-123")

	// Still must have sandbox_permissions override even without model
	foundSandbox := false
	for i, a := range args {
		if a == "-c" && i+1 < len(args) && strings.Contains(args[i+1], "sandbox_permissions") {
			foundSandbox = true
		}
		if strings.Contains(a, "model=") || strings.Contains(a, "model_provider=") {
			t.Errorf("resume args should not contain model config when no model specified, got %q", a)
		}
	}
	if !foundSandbox {
		t.Error("expected -c sandbox_permissions= override in resume args even without model")
	}
}

func TestBuildCodexResumeArgs_WithSystemPrompt(t *testing.T) {
	req := ChatRequest{
		Prompt:       "continue this task",
		Model:        "codex-MiniMax-M2.7",
		SystemPrompt: "You are a senior developer.",
	}
	args := buildCodexResumeArgs(req, "thread-abc")

	// System prompt should be prepended to the last arg (prompt)
	lastArg := args[len(args)-1]
	if !strings.HasPrefix(lastArg, "[System Instructions: You are a senior developer.]") {
		t.Errorf("expected prompt to start with system instructions, got %q", lastArg)
	}
	if !strings.Contains(lastArg, "continue this task") {
		t.Errorf("expected prompt to contain original prompt, got %q", lastArg)
	}
}

func TestBuildCodexResumeArgs_NoSystemPrompt(t *testing.T) {
	req := ChatRequest{
		Prompt:       "test",
		SystemPrompt: "",
	}
	args := buildCodexResumeArgs(req, "thread-123")

	// Without system prompt, last arg should be the prompt as-is
	lastArg := args[len(args)-1]
	if lastArg != "test" {
		t.Errorf("expected prompt to be 'test', got %q", lastArg)
	}
}

// --- parseCodexResumeOutput tests ---

// Codex resume mode uses lasse...lassie tags for thinking blocks
const (
	codexThinkStart = "<think>"
	codexThinkEnd   = "</think>"
)

func parseResumeOutput(input string) []StreamEvent {
	ch := make(chan StreamEvent, 64)
	scanner := bufio.NewScanner(strings.NewReader(input))
	parseCodexResumeOutput(scanner, ch, "test-session-id")
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

func TestCodexResumeOutput_BasicContent(t *testing.T) {
	input := `OpenAI Codex v0.57.0 (research preview)
--------
workdir: /tmp
model: codex-MiniMax-M2.7
--------
user
hello
codex
Hi there! How can I help?`
	events := parseResumeOutput(input)

	// Should have: 1 content + 1 metadata + 1 done = 3
	var contentEvents []StreamEvent
	for _, ev := range events {
		if ev.Type == "content" {
			contentEvents = append(contentEvents, ev)
		}
	}
	if len(contentEvents) == 0 {
		t.Fatal("expected at least one content event, got none")
	}
	if !strings.Contains(contentEvents[0].Content, "Hi there") {
		t.Errorf("expected content to contain 'Hi there', got %q", contentEvents[0].Content)
	}
}

func TestCodexResumeOutput_ERRORLine(t *testing.T) {
	input := `OpenAI Codex v0.57.0 (research preview)
--------
workdir: /tmp
model: codex-MiniMax-M2.7
--------
user
hello
ERROR: Missing environment variable: MINIMAX_API_KEY`
	events := parseResumeOutput(input)

	// Should have: 1 error event (stops parsing after ERROR)
	var errorEvents []StreamEvent
	var doneEvents []StreamEvent
	for _, ev := range events {
		if ev.Type == "error" {
			errorEvents = append(errorEvents, ev)
		}
		if ev.Type == "done" {
			doneEvents = append(doneEvents, ev)
		}
	}
	if len(errorEvents) == 0 {
		t.Fatal("expected an error event for ERROR line, got none")
	}
	if !strings.Contains(errorEvents[0].Error, "Missing environment variable") {
		t.Errorf("expected error to mention env var, got %q", errorEvents[0].Error)
	}
	// ISS-079: ERROR must emit a done event so the SSE stream doesn't hang
	if len(doneEvents) == 0 {
		t.Fatal("expected a done event after ERROR line, got none")
	}
}

func TestCodexResumeOutput_WithThinking(t *testing.T) {
	// Codex uses <think>...</think> tags for thinking in resume mode
	input := "OpenAI Codex v0.57.0 (research preview)\n--------\nuser\nexplain foo\ncodex\n" +
		"<think>\nI need to think about this\n</think>\n" +
		"Here is my explanation of foo."
	events := parseResumeOutput(input)

	var thinkingEvents []StreamEvent
	var contentEvents []StreamEvent
	for _, ev := range events {
		if ev.Type == "thinking" {
			thinkingEvents = append(thinkingEvents, ev)
		}
		if ev.Type == "content" {
			contentEvents = append(contentEvents, ev)
		}
	}
	if len(thinkingEvents) == 0 {
		t.Fatal("expected at least one thinking event")
	}
	if !strings.Contains(thinkingEvents[0].Content, "think about this") {
		t.Errorf("expected thinking content, got %q", thinkingEvents[0].Content)
	}
	if len(contentEvents) == 0 {
		t.Fatal("expected at least one content event")
	}
	if !strings.Contains(contentEvents[0].Content, "explanation") {
		t.Errorf("expected content, got %q", contentEvents[0].Content)
	}
}

// --- turn.failed tests ---

func TestCodexStream_TurnFailed(t *testing.T) {
	line := `{"type":"turn.failed","error":{"message":"Rate limit exceeded"}}`
	events := parseCodexLine(line)

	if len(events) != 2 {
		t.Fatalf("expected 2 events (error + done), got %d", len(events))
	}
	if events[0].Type != "error" {
		t.Errorf("expected error event, got %s", events[0].Type)
	}
	if events[0].Error != "Rate limit exceeded" {
		t.Errorf("expected 'Rate limit exceeded', got %q", events[0].Error)
	}
	if events[1].Type != "done" {
		t.Errorf("expected done event, got %s", events[1].Type)
	}
}

func TestCodexStream_TurnFailedNoMessage(t *testing.T) {
	line := `{"type":"turn.failed"}`
	events := parseCodexLine(line)

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "error" {
		t.Errorf("expected error event, got %s", events[0].Type)
	}
	if events[0].Error != "AI request failed" {
		t.Errorf("expected default error message, got %q", events[0].Error)
	}
	if events[0].Reason != ReasonRequestFailed {
		t.Errorf("expected reason 'request_failed', got %q", events[0].Reason)
	}
}

func TestCodexStream_ErrorType(t *testing.T) {
	line := `{"type":"error","message":"Something went wrong"}`
	events := parseCodexLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "warning" {
		t.Errorf("error type messages should be forwarded as warning, got %s", events[0].Type)
	}
	if events[0].Content != "Something went wrong" {
		t.Errorf("expected 'Something went wrong', got %q", events[0].Content)
	}
	if events[0].Reason != ReasonRequestFailed {
		t.Errorf("expected reason 'request_failed', got %q", events[0].Reason)
	}
}

// --- Second session (resume) comprehensive tests ---
// These test the full codex resume flow using real stderr output format.
// In resume mode, codex outputs the formatted transcript to stderr (not stdout).

func TestCodexResumeOutput_FullResumeSession(t *testing.T) {
	// Simulates real stderr output from "codex exec resume <thread_id> <prompt>"
	input := "OpenAI Codex v0.57.0 (research preview)\n" +
		"--------\n" +
		"workdir: /home/user/project\n" +
		"model: codex-MiniMax-M2.7\n" +
		"provider: minimax\n" +
		"approval: never\n" +
		"sandbox: read-only\n" +
		"session id: 019dc814-0f5e-7260-a32b-b274fee09be1\n" +
		"--------\n" +
		"user\n" +
		"what is 1+1\n" +
		"codex\n" +
		codexThinkStart + "\nThe user is asking a simple math question.\n" + codexThinkEnd + "\n" +
		"1 + 1 = 2\n"
	events := parseResumeOutput(input)

	// Expected events: thinking, content, metadata, done
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events (thinking + content + metadata + done), got %d", len(events))
	}

	// Verify thinking
	foundThinking := false
	foundContent := false
	foundMetadata := false
	foundDone := false
	for _, ev := range events {
		switch ev.Type {
		case "thinking":
			foundThinking = true
			if !strings.Contains(ev.Content, "simple math") {
				t.Errorf("expected thinking about math, got %q", ev.Content)
			}
		case "content":
			foundContent = true
			if !strings.Contains(ev.Content, "1 + 1 = 2") {
				t.Errorf("expected content '1 + 1 = 2', got %q", ev.Content)
			}
		case "metadata":
			foundMetadata = true
		case "done":
			foundDone = true
		}
	}
	if !foundThinking {
		t.Error("expected a thinking event")
	}
	if !foundContent {
		t.Error("expected a content event")
	}
	if !foundMetadata {
		t.Error("expected a metadata event")
	}
	if !foundDone {
		t.Error("expected a done event")
	}
}

func TestCodexResumeOutput_ResumeWithCommandExecution(t *testing.T) {
	// Resume session where codex executes a command
	input := "OpenAI Codex v0.57.0 (research preview)\n" +
		"--------\n" +
		"session id: thread-abc\n" +
		"--------\n" +
		"user\n" +
		"list files\n" +
		"codex\n" +
		"Let me check.\n" +
		"exec\n" +
		"bash -c 'ls' in /tmp succeeded in 10ms:\n" +
		"file1.txt\n" +
		"file2.txt\n" +
		"codex\n" +
		"Here are the files.\n"
	events := parseResumeOutput(input)

	// Expected: content("Let me check"), tool_use(started), tool_use(completed), content("Here are the files"), metadata, done
	var contentCount, toolUseCount int
	for _, ev := range events {
		switch ev.Type {
		case "content":
			contentCount++
		case "tool_use":
			toolUseCount++
		}
	}
	if contentCount < 2 {
		t.Errorf("expected at least 2 content events, got %d", contentCount)
	}
	if toolUseCount < 2 {
		t.Errorf("expected at least 2 tool_use events (started + completed), got %d", toolUseCount)
	}

	// Verify tool_use has JSON input with command and output
	var completedTools []StreamEvent
	for _, ev := range events {
		if ev.Type == "tool_use" && ev.Tool != nil && ev.Tool.Done {
			completedTools = append(completedTools, ev)
		}
	}
	if len(completedTools) == 0 {
		t.Fatal("expected at least one completed tool_use")
	}

	// Verify tool has an ID for deduplication
	if completedTools[0].Tool.ID == "" {
		t.Error("expected tool_use to have an ID for deduplication")
	}

	// Verify input is valid JSON with command field
	var toolInput map[string]any
	if err := json.Unmarshal([]byte(completedTools[0].Tool.Input), &toolInput); err != nil {
		t.Fatalf("expected tool input to be valid JSON, got %q: %v", completedTools[0].Tool.Input, err)
	}
	if cmd, _ := toolInput["command"].(string); !strings.Contains(cmd, "bash") {
		t.Errorf("expected JSON command field to contain 'bash', got %q", cmd)
	}
	// Output is now in Tool.Output, not in Tool.Input
	if !strings.Contains(completedTools[0].Tool.Output, "file1.txt") {
		t.Errorf("expected Tool.Output to contain 'file1.txt', got %q", completedTools[0].Tool.Output)
	}
}

func TestCodexResumeOutput_MultipleCodexTurns(t *testing.T) {
	// Session with multiple codex responses (e.g., think -> respond -> tool -> respond)
	input := "--------\n" +
		"user\n" +
		"check the code\n" +
		"codex\n" +
		"I'll look at the code.\n" +
		"codex\n" +
		"Here's what I found.\n"
	events := parseResumeOutput(input)

	var contentCount int
	for _, ev := range events {
		if ev.Type == "content" {
			contentCount++
		}
	}
	if contentCount < 2 {
		t.Errorf("expected at least 2 content events from 2 codex turns, got %d", contentCount)
	}
}

func TestCodexResumeOutput_EmptyResponse(t *testing.T) {
	// Session where codex returns only header, no content (e.g., interrupted)
	input := "OpenAI Codex v0.57.0 (research preview)\n" +
		"--------\n" +
		"session id: thread-xyz\n" +
		"--------\n" +
		"user\n" +
		"hello\n"
	events := parseResumeOutput(input)

	// Should still have metadata + done even with no content
	var foundDone, foundMetadata bool
	for _, ev := range events {
		if ev.Type == "done" {
			foundDone = true
		}
		if ev.Type == "metadata" {
			foundMetadata = true
		}
	}
	if !foundDone {
		t.Error("expected done event even on empty response")
	}
	if !foundMetadata {
		t.Error("expected metadata event even on empty response")
	}
}

func TestCodexResumeOutput_AnSIColorCodes(t *testing.T) {
	// Codex may output ANSI color codes around role markers.
	// ANSI-prefixed markers won't match the bare "codex"/"user" comparison,
	// so no content should be extracted. This tests the parser's resilience
	// to ANSI — it still produces metadata + done without panicking.
	input := "--------\n" +
		"\x1b[36muser\x1b[0m\n" + // ANSI-colored "user" — NOT a bare "user" line
		"hello\n" +
		"\x1b[32mcodex\x1b[0m\n" + // ANSI-colored "codex" — NOT a bare "codex" line
		"Hi there!\n"
	events := parseResumeOutput(input)

	var contentCount int
	for _, ev := range events {
		if ev.Type == "content" {
			contentCount++
		}
	}
	// ANSI-prefixed markers won't match, so no content is expected.
	// If ANSI stripping is added later, this test should be updated.
	assert.Equal(t, 0, contentCount, "ANSI-prefixed role markers should not produce content events")

	// Parser should still produce metadata + done
	var foundMetadata, foundDone bool
	for _, ev := range events {
		if ev.Type == "metadata" {
			foundMetadata = true
		}
		if ev.Type == "done" {
			foundDone = true
		}
	}
	assert.True(t, foundMetadata, "metadata event should still be produced")
	assert.True(t, foundDone, "done event should still be produced")
}

func TestCodexResumeOutput_MetadataSessionID(t *testing.T) {
	input := "--------\n" +
		"user\n" +
		"test\n" +
		"codex\n" +
		"Response here.\n"
	events := parseResumeOutput(input)

	for _, ev := range events {
		if ev.Type == "metadata" && ev.Meta != nil {
			if ev.Meta.SessionID != "test-session-id" {
				t.Errorf("expected session ID 'test-session-id', got %q", ev.Meta.SessionID)
			}
			return
		}
	}
	t.Error("expected metadata event with session ID")
}

func TestCodexResumeOutput_ExecBlockFlushedAtEOF(t *testing.T) {
	// exec block at end of output without a following "codex" marker
	input := "--------\n" +
		"user\n" +
		"run it\n" +
		"codex\n" +
		"Running now.\n" +
		"exec\n" +
		"bash -c 'echo hi' in /tmp succeeded in 5ms:\n" +
		"hi\n"
	events := parseResumeOutput(input)

	// The exec block should be flushed at EOF with JSON input
	var completedTools []StreamEvent
	for _, ev := range events {
		if ev.Type == "tool_use" && ev.Tool != nil && ev.Tool.Done {
			completedTools = append(completedTools, ev)
		}
	}
	if len(completedTools) == 0 {
		t.Fatal("expected at least one completed tool_use from EOF flush")
	}
	var toolInput map[string]any
	if err := json.Unmarshal([]byte(completedTools[0].Tool.Input), &toolInput); err != nil {
		t.Fatalf("expected JSON input, got %q: %v", completedTools[0].Tool.Input, err)
	}
	if cmd, _ := toolInput["command"].(string); !strings.Contains(cmd, "echo hi") {
		t.Errorf("expected JSON command to contain 'echo hi', got %q", cmd)
	}
	// Output is now in Tool.Output, not in Tool.Input
	if !strings.Contains(completedTools[0].Tool.Output, "hi") {
		t.Errorf("expected Tool.Output to contain 'hi', got %q", completedTools[0].Tool.Output)
	}
}

func TestCodexResumeOutput_ExecBlockThenNoCodexResponse(t *testing.T) {
	// Scenario: AI executes a read-only command (cat) and then the CLI exits
	// without producing a text response. This happens when sandbox is read-only
	// and the AI wants to Edit/Write but can't — it just stops.
	// The parser must still flush the exec block and emit metadata+done.
	input := "--------\n" +
		"user\n" +
		"edit this file\n" +
		"codex\n" +
		codexThinkStart + "I need to read the file first." + codexThinkEnd + "\n" +
		"exec\n" +
		"cat /home/user/config.yaml in /home/user succeeded in 9ms:\n" +
		"key: value\n" +
		"another: setting\n"
	events := parseResumeOutput(input)

	// Must have: thinking, tool_use (started), tool_use (completed), metadata, done
	var thinkingCount, toolStarted, toolCompleted, metadataCount, doneCount int
	for _, ev := range events {
		switch ev.Type {
		case "thinking":
			thinkingCount++
		case "tool_use":
			if ev.Tool != nil {
				if ev.Tool.Done {
					toolCompleted++
				} else {
					toolStarted++
				}
			}
		case "metadata":
			metadataCount++
		case "done":
			doneCount++
		}
	}

	if thinkingCount == 0 {
		t.Error("expected thinking event")
	}
	if toolStarted == 0 {
		t.Error("expected tool_use started event")
	}
	if toolCompleted == 0 {
		t.Error("expected tool_use completed event (flushed at EOF)")
	}
	if metadataCount == 0 {
		t.Error("expected metadata event")
	}
	if doneCount == 0 {
		t.Error("expected done event")
	}
}

// --- End-to-end resume flow: first session -> second session ---
// Tests that simulate the complete two-session lifecycle.

func TestCodexResumeOutput_SecondSession_ResumeFlow(t *testing.T) {
	// This test simulates what happens in the second codex session:
	// 1. First session created a thread_id (captured via metadata)
	// 2. Second session uses that thread_id to resume
	// 3. Resume output (from stderr) is parsed into events

	// Simulate first session JSONL output
	firstSessionLines := []string{
		`{"type":"thread.started","thread_id":"019dc814-0f5e-7260-a32b-b274fee09be1"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Hello! How can I help?"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":50,"cached_input_tokens":0,"output_tokens":20}}`,
	}

	ch1 := make(chan StreamEvent, 64)
	parser := &CodexStreamParser{}
	for _, line := range firstSessionLines {
		parser.ParseLine(line, ch1)
	}
	close(ch1)

	var firstEvents []StreamEvent
	var threadID string
	for ev := range ch1 {
		_ = append(firstEvents, ev)
		if ev.Type == "metadata" && ev.Meta != nil && ev.Meta.SessionID != "" {
			threadID = ev.Meta.SessionID
		}
	}
	if threadID != "019dc814-0f5e-7260-a32b-b274fee09be1" {
		t.Fatalf("expected thread_id from first session, got %q", threadID)
	}

	// Now simulate second session: resume using thread_id
	// This is the stderr output from "codex exec resume <thread_id> <prompt>"
	resumeInput := "OpenAI Codex v0.57.0 (research preview)\n" +
		"--------\n" +
		"workdir: /home/user/project\n" +
		"model: codex-MiniMax-M2.7\n" +
		"provider: minimax\n" +
		"approval: never\n" +
		"sandbox: read-only\n" +
		"session id: " + threadID + "\n" +
		"--------\n" +
		"user\n" +
		"what is 2+2\n" +
		"codex\n" +
		codexThinkStart + "\nSimple math question.\n" + codexThinkEnd + "\n" +
		"2 + 2 = 4\n"

	secondEvents := parseResumeOutput(resumeInput)

	// Verify second session produces content
	var contentEvents []StreamEvent
	var thinkingEvents []StreamEvent
	for _, ev := range secondEvents {
		if ev.Type == "content" {
			contentEvents = append(contentEvents, ev)
		}
		if ev.Type == "thinking" {
			thinkingEvents = append(thinkingEvents, ev)
		}
	}
	if len(thinkingEvents) == 0 {
		t.Error("second session: expected thinking event")
	}
	if len(contentEvents) == 0 {
		t.Fatal("second session: expected at least one content event — this is the core bug!")
	}
	if !strings.Contains(contentEvents[0].Content, "2 + 2 = 4") {
		t.Errorf("second session: expected '2 + 2 = 4', got %q", contentEvents[0].Content)
	}
}

func TestCodexResumeOutput_SecondSession_WithToolUse(t *testing.T) {
	// Second session where codex resumes and executes a command
	resumeInput := "OpenAI Codex v0.57.0 (research preview)\n" +
		"--------\n" +
		"session id: 019dc814-0f5e-7260-a32b-b274fee09be1\n" +
		"--------\n" +
		"user\n" +
		"check git status\n" +
		"codex\n" +
		codexThinkStart + "\nI should check git status for the user.\n" + codexThinkEnd + "\n" +
		"exec\n" +
		"bash -c 'git status' in /home/user/project succeeded in 100ms:\n" +
		"On branch main\n" +
		"nothing to commit\n" +
		"codex\n" +
		"Your repo is clean, nothing to commit.\n"

	events := parseResumeOutput(resumeInput)

	// Expected: thinking, tool_use(started), tool_use(completed with output), content, metadata, done
	var thinkingCount, contentCount, toolUseStarted, toolUseCompleted int
	for _, ev := range events {
		switch ev.Type {
		case "thinking":
			thinkingCount++
		case "content":
			contentCount++
		case "tool_use":
			if ev.Tool != nil {
				if ev.Tool.Done {
					toolUseCompleted++
				} else {
					toolUseStarted++
				}
			}
		}
	}
	if thinkingCount == 0 {
		t.Error("expected thinking event before command execution")
	}
	if toolUseStarted == 0 {
		t.Error("expected tool_use started event")
	}
	if toolUseCompleted == 0 {
		t.Error("expected tool_use completed event")
	}
	if contentCount == 0 {
		t.Fatal("expected content event after tool execution — core bug scenario!")
	}
}

func TestBuildCodexResumeArgs_SandboxPermissionsPresent(t *testing.T) {
	req := ChatRequest{
		Prompt: "test",
		Model:  "codex-MiniMax-M2.7",
	}
	args := buildCodexResumeArgs(req, "thread-id")

	// Verify sandbox_permissions is in the args and comes before thread_id
	foundSandboxIdx := -1
	foundThreadIdx := -1
	for i, arg := range args {
		if arg == "-c" && i+1 < len(args) && strings.Contains(args[i+1], "sandbox_permissions") {
			foundSandboxIdx = i
		}
		if arg == "thread-id" {
			foundThreadIdx = i
		}
	}
	if foundSandboxIdx < 0 {
		t.Fatal("expected -c sandbox_permissions in resume args")
	}
	if foundThreadIdx < 0 {
		t.Fatal("expected thread-id in resume args")
	}
	if foundSandboxIdx > foundThreadIdx {
		t.Error("sandbox_permissions should come before thread_id in args")
	}
}

func TestBuildCodexResumeArgs_SandboxWriteAccess(t *testing.T) {
	// Resume sandbox must include write access to match the new-session
	// sandbox level (--dangerously-bypass-approvals-and-sandbox).
	// Without write access, commands like Edit/Write are silently blocked,
	// causing the AI to exit without producing a text response.
	req := ChatRequest{
		Prompt: "test",
		Model:  "codex-MiniMax-M2.7",
	}
	args := buildCodexResumeArgs(req, "thread-id")

	foundSandbox := false
	for i, arg := range args {
		if arg == "-c" && i+1 < len(args) {
			val := args[i+1]
			if strings.Contains(val, "sandbox_permissions") {
				foundSandbox = true
				if !strings.Contains(val, "disk-full-write-access") {
					t.Errorf("sandbox_permissions must include disk-full-write-access, got: %s", val)
				}
				if !strings.Contains(val, "disk-full-read-access") {
					t.Errorf("sandbox_permissions must include disk-full-read-access, got: %s", val)
				}
			}
		}
	}
	if !foundSandbox {
		t.Fatal("expected -c sandbox_permissions in resume args")
	}
}

// --- Thinking tag on same line tests (bug fix) ---
// These test the fix for the bug where  anticicontent... antici on one line
// caused the parser to never exit thinking mode.

func TestCodexResumeOutput_ThinkingOpenCloseSameLine(t *testing.T) {
	//  anticicontent... antici all on one line, content follows on next lines
	input := "--------\n" +
		"user\n" +
		"hello\n" +
		"codex\n" +
		codexThinkStart + "Let me think about this." + codexThinkEnd + "\n" +
		"\n" +
		"Here is my answer.\n"
	events := parseResumeOutput(input)

	var thinkingEvents, contentEvents []StreamEvent
	for _, ev := range events {
		if ev.Type == "thinking" {
			thinkingEvents = append(thinkingEvents, ev)
		}
		if ev.Type == "content" {
			contentEvents = append(contentEvents, ev)
		}
	}
	if len(thinkingEvents) == 0 {
		t.Fatal("expected at least one thinking event")
	}
	if !strings.Contains(thinkingEvents[0].Content, "Let me think about this") {
		t.Errorf("expected thinking content, got %q", thinkingEvents[0].Content)
	}
	if len(contentEvents) == 0 {
		t.Fatal("expected at least one content event (BUG: all content treated as thinking)")
	}
	if !strings.Contains(contentEvents[0].Content, "Here is my answer") {
		t.Errorf("expected content, got %q", contentEvents[0].Content)
	}
}

func TestCodexResumeOutput_ThinkingOpenCloseSeparateLines(t *testing.T) {
	//  antici on its own line, content lines, then  antici on its own line
	input := "--------\n" +
		"user\n" +
		"hello\n" +
		"codex\n" +
		codexThinkStart + "\n" +
		"First thought\n" +
		"Second thought\n" +
		codexThinkEnd + "\n" +
		"\n" +
		"The actual response.\n"
	events := parseResumeOutput(input)

	var thinkingEvents, contentEvents []StreamEvent
	for _, ev := range events {
		if ev.Type == "thinking" {
			thinkingEvents = append(thinkingEvents, ev)
		}
		if ev.Type == "content" {
			contentEvents = append(contentEvents, ev)
		}
	}
	if len(thinkingEvents) == 0 {
		t.Fatal("expected at least one thinking event")
	}
	if !strings.Contains(thinkingEvents[0].Content, "First thought") {
		t.Errorf("expected thinking content, got %q", thinkingEvents[0].Content)
	}
	if len(contentEvents) == 0 {
		t.Fatal("expected at least one content event")
	}
	if !strings.Contains(contentEvents[0].Content, "actual response") {
		t.Errorf("expected content, got %q", contentEvents[0].Content)
	}
}

func TestCodexResumeOutput_ThinkingCloseInlineInMiddle(t *testing.T) {
	//  antici starts thinking, then on a subsequent line the  antici
	// closing tag appears at the end (not at the start of the line)
	input := "--------\n" +
		"user\n" +
		"hello\n" +
		"codex\n" +
		codexThinkStart + "\n" +
		"I am thinking" + codexThinkEnd + "\n" +
		"\n" +
		"My response.\n"
	events := parseResumeOutput(input)

	var thinkingEvents, contentEvents []StreamEvent
	for _, ev := range events {
		if ev.Type == "thinking" {
			thinkingEvents = append(thinkingEvents, ev)
		}
		if ev.Type == "content" {
			contentEvents = append(contentEvents, ev)
		}
	}
	if len(thinkingEvents) == 0 {
		t.Fatal("expected at least one thinking event")
	}
	if len(contentEvents) == 0 {
		t.Fatal("expected at least one content event (BUG: inline close tag not detected)")
	}
	if !strings.Contains(contentEvents[0].Content, "My response") {
		t.Errorf("expected content, got %q", contentEvents[0].Content)
	}
}

func TestCodexSplitThinking_WithTagPairs(t *testing.T) {
	text := codexThinkOpen + "I am thinking" + codexThinkClose + "\n\nThe response"
	thinking, content := codexSplitThinking(text)
	if !strings.Contains(thinking, "I am thinking") {
		t.Errorf("expected thinking, got %q", thinking)
	}
	if !strings.Contains(content, "The response") {
		t.Errorf("expected content, got %q", content)
	}
}

func TestCodexSplitThinking_NewlineOnly(t *testing.T) {
	text := "Just thinking\n\nThe content"
	thinking, content := codexSplitThinking(text)
	if thinking != "Just thinking" {
		t.Errorf("expected 'Just thinking', got %q", thinking)
	}
	if content != "The content" {
		t.Errorf("expected 'The content', got %q", content)
	}
}

func TestCodexSplitThinking_NoSeparator(t *testing.T) {
	text := "Just content"
	thinking, content := codexSplitThinking(text)
	if thinking != "" {
		t.Errorf("expected empty thinking, got %q", thinking)
	}
	if content != "Just content" {
		t.Errorf("expected 'Just content', got %q", content)
	}
}

func TestCodexSplitThinking_OnlyOpenTag(t *testing.T) {
	text := codexThinkOpen + "Just thinking, no close tag"
	thinking, content := codexSplitThinking(text)
	if !strings.Contains(thinking, "Just thinking") {
		t.Errorf("expected thinking content, got %q", thinking)
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

// --- Scanner error (ISS-080) test ---

// errorReader reads a limited number of bytes, then returns an error.
type errorReader struct {
	data   []byte
	err    error
	offset int
}

func (r *errorReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, r.err
	}
	n := copy(p, r.data[r.offset:])
	r.offset += n
	if r.offset >= len(r.data) {
		return n, r.err
	}
	return n, nil
}

func TestCodexResumeOutput_ScannerReadError(t *testing.T) {
	// Simulate a partial read followed by an I/O error
	partial := `OpenAI Codex v0.57.0 (research preview)
--------
workdir: /tmp
model: codex-MiniMax-M2.7
--------
user
hello
codex
Hi the`
	reader := &errorReader{data: []byte(partial), err: fmt.Errorf("i/o timeout")}
	scanner := bufio.NewScanner(reader)
	ch := make(chan StreamEvent, 64)
	parseCodexResumeOutput(scanner, ch, "")
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should have a warning event for the read error
	hasWarning := false
	for _, ev := range events {
		if ev.Type == "warning" {
			hasWarning = true
			assert.Contains(t, ev.Content, "read error", "warning should mention read error")
		}
	}
	assert.True(t, hasWarning, "expected a warning event for scanner.Err()")

	// Should still have a done event so the consumer can finalize
	hasDone := false
	for _, ev := range events {
		if ev.Type == "done" {
			hasDone = true
		}
	}
	assert.True(t, hasDone, "expected a done event after scanner read error")
}
