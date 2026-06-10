package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func parseGeminiLine(line string) []StreamEvent {
	ch := make(chan StreamEvent, 64)
	parser := &GeminiStreamParser{}
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

func TestGeminiStream_ParseLine_Init(t *testing.T) {
	line := `{"type":"init","timestamp":"2026-04-25T10:00:00.000Z","session_id":"ses_abc123","model":"gemini-3-pro-preview"}`
	events := parseGeminiLine(line)

	// Init events don't emit stream events, they just capture session/model
	if len(events) != 0 {
		t.Fatalf("expected 0 events for init, got %d", len(events))
		return
	}
}

func TestGeminiStream_ParseLine_AssistantMessage(t *testing.T) {
	line := `{"type":"message","timestamp":"2026-04-25T10:00:01.000Z","role":"assistant","content":"Hello, world!","delta":true}`
	events := parseGeminiLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
		return
	}
	if events[0].Type != "content" {
		t.Errorf("expected content event, got %s", events[0].Type)
	}
	if events[0].Content != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", events[0].Content)
	}
}

func TestGeminiStream_ParseLine_UserMessage(t *testing.T) {
	line := `{"type":"message","timestamp":"2026-04-25T10:00:00.000Z","role":"user","content":"Say hello"}`
	events := parseGeminiLine(line)

	// User messages should be skipped (they echo back the input)
	if len(events) != 0 {
		t.Fatalf("expected 0 events for user message, got %d", len(events))
		return
	}
}

func TestGeminiStream_ParseLine_AssistantEmpty(t *testing.T) {
	line := `{"type":"message","timestamp":"2026-04-25T10:00:01.000Z","role":"assistant","content":"","delta":true}`
	events := parseGeminiLine(line)

	if len(events) != 0 {
		t.Fatalf("expected 0 events for empty assistant message, got %d", len(events))
		return
	}
}

func TestGeminiStream_ParseLine_ToolUse(t *testing.T) {
	line := `{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"read_file","tool_id":"call_123","parameters":{"filePath":"/tmp/test.go"}}`
	events := parseGeminiLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
		return
	}
	if events[0].Type != "tool_use" {
		t.Errorf("expected tool_use event, got %s", events[0].Type)
	}
	tool := events[0].Tool
	if tool == nil {
		t.Fatal("expected tool call, got nil")
		return
	}
	if tool.Name != "Read" {
		t.Errorf("expected normalized tool name 'Read', got %q", tool.Name)
	}
	if tool.ID != "call_123" {
		t.Errorf("expected call ID 'call_123', got %q", tool.ID)
	}
	if !tool.Done {
		t.Error("expected Done=true for Gemini tool_use (full input in one event)")
	}
	// Verify input is normalized: filePath → file_path
	var input map[string]any
	if err := json.Unmarshal([]byte(tool.Input), &input); err != nil {
		t.Fatalf("failed to parse tool input: %v", err)
		return
	}
	if input["file_path"] != "/tmp/test.go" {
		t.Errorf("unexpected input: %v", input)
	}
}

func TestGeminiStream_ParseLine_ToolUseNonObjectParams(t *testing.T) {
	// When parameters is valid JSON but not an object (e.g., an array),
	// normalizeToolInput should fail and fall back to string(msg.Parameters)
	line := `{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"list_files","tool_id":"call_arr","parameters":[1,2,3]}`
	events := parseGeminiLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
		return
	}
	tool := events[0].Tool
	if tool == nil {
		t.Fatal("expected tool call, got nil")
		return
	}
	// Should fall back to raw parameter string
	assert.Equal(t, "[1,2,3]", tool.Input)
}

func TestGeminiStream_ParseLine_ToolUseEmptyParams(t *testing.T) {
	line := `{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"list_files","tool_id":"call_456"}`
	events := parseGeminiLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
		return
	}
	tool := events[0].Tool
	if tool.Input != "{}" {
		t.Errorf("expected empty object for missing parameters, got %q", tool.Input)
	}
}

func TestGeminiStream_ParseLine_ToolResult(t *testing.T) {
	line := `{"type":"tool_result","timestamp":"2026-04-25T10:00:03.000Z","tool_id":"call_123","status":"success","output":"file content here"}`
	events := parseGeminiLine(line)

	// Tool results now emit a tool_result stream event
	if len(events) != 1 {
		t.Fatalf("expected 1 event for tool_result, got %d", len(events))
		return
	}
	if events[0].Type != "tool_result" {
		t.Errorf("expected event type 'tool_result', got %q", events[0].Type)
	}
	if events[0].Tool == nil {
		t.Fatal("expected Tool to be non-nil")
		return
	}
	if events[0].Tool.ID != "call_123" {
		t.Errorf("expected tool ID 'call_123', got %q", events[0].Tool.ID)
	}
	if events[0].Tool.Output != "file content here" {
		t.Errorf("expected output 'file content here', got %q", events[0].Tool.Output)
	}
	if events[0].Tool.Status != "success" {
		t.Errorf("expected status 'success', got %q", events[0].Tool.Status)
	}
}

func TestGeminiStream_ParseLine_ErrorWarning(t *testing.T) {
	line := `{"type":"error","timestamp":"2026-04-25T10:00:03.000Z","severity":"warning","message":"Loop detected, stopping execution"}`
	events := parseGeminiLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
		return
	}
	if events[0].Type != "warning" {
		t.Errorf("expected warning event, got %s", events[0].Type)
	}
	if events[0].Content != "Loop detected, stopping execution" {
		t.Errorf("unexpected content: %q", events[0].Content)
	}
}

func TestGeminiStream_ParseLine_ErrorError(t *testing.T) {
	line := `{"type":"error","timestamp":"2026-04-25T10:00:03.000Z","severity":"error","message":"Maximum session turns exceeded"}`
	events := parseGeminiLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
		return
	}
	if events[0].Type != "error" {
		t.Errorf("expected error event, got %s", events[0].Type)
	}
	if events[0].Error != "Maximum session turns exceeded" {
		t.Errorf("unexpected error: %q", events[0].Error)
	}
}

func TestGeminiStream_ParseLine_ResultSuccess(t *testing.T) {
	line := `{"type":"result","timestamp":"2026-04-25T10:00:05.000Z","status":"success","stats":{"total_tokens":500,"input_tokens":400,"output_tokens":100,"cached":0,"input":400,"duration_ms":3000,"tool_calls":2,"models":{"gemini-3-pro-preview":{"total_tokens":500,"input_tokens":400,"output_tokens":100,"cached":0,"input":400}}}}`
	events := parseGeminiLine(line)

	if len(events) != 2 {
		t.Fatalf("expected 2 events (metadata + done), got %d", len(events))
		return
	}
	if events[0].Type != "metadata" {
		t.Errorf("expected metadata event first, got %s", events[0].Type)
	}
	meta := events[0].Meta
	if meta == nil {
		t.Fatal("expected metadata, got nil")
		return
	}
	if meta.InputTokens != 400 {
		t.Errorf("expected input tokens 400, got %d", meta.InputTokens)
	}
	if meta.OutputTokens != 100 {
		t.Errorf("expected output tokens 100, got %d", meta.OutputTokens)
	}
	if meta.DurationMs != 3000 {
		t.Errorf("expected duration 3000ms, got %d", meta.DurationMs)
	}
	if meta.StopReason != "stop" {
		t.Errorf("expected stopReason 'stop', got %q", meta.StopReason)
	}
	if meta.IsError {
		t.Error("expected IsError=false for success result")
	}
	if events[1].Type != "done" {
		t.Errorf("expected done event second, got %s", events[1].Type)
	}
}

func TestGeminiStream_ParseLine_ResultError(t *testing.T) {
	line := `{"type":"result","timestamp":"2026-04-25T10:00:05.000Z","status":"error","error":{"type":"FatalAuthenticationError","message":"Authentication failed"},"stats":{"total_tokens":0,"input_tokens":0,"output_tokens":0,"cached":0,"input":0,"duration_ms":0,"tool_calls":0,"models":{}}}`
	events := parseGeminiLine(line)

	// Result with error: warning event + metadata + done
	if len(events) != 3 {
		t.Fatalf("expected 3 events (warning + metadata + done), got %d", len(events))
		return
	}
	if events[0].Type != "warning" {
		t.Errorf("expected warning event first, got %s", events[0].Type)
	}
	if events[0].Content != "Authentication failed" {
		t.Errorf("unexpected warning content: %q", events[0].Content)
	}
	if events[1].Type != "metadata" {
		t.Errorf("expected metadata event second, got %s", events[1].Type)
	}
	if !events[1].Meta.IsError {
		t.Error("expected IsError=true for error result")
	}
}

func TestGeminiStream_ParseLine_UnparseableLine(t *testing.T) {
	events := parseGeminiLine("not json at all")
	if len(events) != 0 {
		t.Fatalf("expected 0 events for unparseable line, got %d", len(events))
		return
	}
}

func TestGeminiStream_ParseLine_UnknownType(t *testing.T) {
	line := `{"type":"custom_event","timestamp":"2026-04-25T10:00:00.000Z"}`
	events := parseGeminiLine(line)
	if len(events) != 0 {
		t.Fatalf("expected 0 events for unknown type, got %d", len(events))
		return
	}
}

func TestGeminiStream_SessionIDCapture(t *testing.T) {
	parser := &GeminiStreamParser{}
	ch := make(chan StreamEvent, 64)

	// Init captures session ID and model
	line1 := `{"type":"init","timestamp":"2026-04-25T10:00:00.000Z","session_id":"ses_captured123","model":"gemini-3-pro-preview"}`
	parser.ParseLine(line1, ch)

	// Result uses the captured session ID in metadata
	line2 := `{"type":"result","timestamp":"2026-04-25T10:00:05.000Z","status":"success","stats":{"total_tokens":500,"input_tokens":400,"output_tokens":100,"cached":0,"input":400,"duration_ms":3000,"tool_calls":0,"models":{}}}`
	parser.ParseLine(line2, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
		return
	}
	if events[0].Meta.SessionID != "ses_captured123" {
		t.Errorf("expected sessionID 'ses_captured123', got %q", events[0].Meta.SessionID)
	}
	if events[0].Meta.Model != "gemini-3-pro-preview" {
		t.Errorf("expected model 'gemini-3-pro-preview', got %q", events[0].Meta.Model)
	}
}

func TestGeminiStream_FullFlow(t *testing.T) {
	lines := []string{
		`{"type":"init","timestamp":"2026-04-25T10:00:00.000Z","session_id":"ses_full_flow","model":"gemini-3-pro-preview"}`,
		`{"type":"message","timestamp":"2026-04-25T10:00:00.500Z","role":"user","content":"Read the main.go file"}`,
		`{"type":"message","timestamp":"2026-04-25T10:00:01.000Z","role":"assistant","content":"I'll read","delta":true}`,
		`{"type":"message","timestamp":"2026-04-25T10:00:01.500Z","role":"assistant","content":" that file for you.","delta":true}`,
		`{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"read_file","tool_id":"call_001","parameters":{"filePath":"main.go"}}`,
		`{"type":"tool_result","timestamp":"2026-04-25T10:00:03.000Z","tool_id":"call_001","status":"success","output":"package main\n\nfunc main() {}"}`,
		`{"type":"message","timestamp":"2026-04-25T10:00:04.000Z","role":"assistant","content":"The file contains a simple main package.","delta":true}`,
		`{"type":"result","timestamp":"2026-04-25T10:00:05.000Z","status":"success","stats":{"total_tokens":1000,"input_tokens":800,"output_tokens":200,"cached":0,"input":800,"duration_ms":5000,"tool_calls":1,"models":{"gemini-3-pro-preview":{"total_tokens":1000,"input_tokens":800,"output_tokens":200,"cached":0,"input":800}}}}`,
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

	// Expected: content, content, tool_use, tool_result, content, metadata, done
	if len(events) != 7 {
		t.Fatalf("expected 7 events, got %d", len(events))
		return
	}

	// Event 0: content
	if events[0].Type != "content" || events[0].Content != "I'll read" {
		t.Errorf("event 0: unexpected, got type=%s content=%q", events[0].Type, events[0].Content)
	}
	// Event 1: content
	if events[1].Type != "content" || events[1].Content != " that file for you." {
		t.Errorf("event 1: unexpected, got type=%s content=%q", events[1].Type, events[1].Content)
	}
	// Event 2: tool_use
	if events[2].Type != "tool_use" {
		t.Errorf("event 2: expected tool_use, got %s", events[2].Type)
	}
	if events[2].Tool.Name != "Read" {
		t.Errorf("event 2: expected normalized tool 'Read', got %q", events[2].Tool.Name)
	}
	// Event 3: tool_result
	if events[3].Type != "tool_result" {
		t.Errorf("event 3: expected tool_result, got %s", events[3].Type)
	}
	if events[3].Tool.ID != "call_001" {
		t.Errorf("event 3: expected tool ID 'call_001', got %q", events[3].Tool.ID)
	}
	// Event 4: content
	if events[4].Type != "content" || events[4].Content != "The file contains a simple main package." {
		t.Errorf("event 4: unexpected, got type=%s content=%q", events[4].Type, events[4].Content)
	}
	// Event 5: metadata
	if events[5].Type != "metadata" {
		t.Errorf("event 5: expected metadata, got %s", events[5].Type)
	}
	if events[5].Meta.SessionID != "ses_full_flow" {
		t.Errorf("event 5: expected sessionID 'ses_full_flow', got %q", events[5].Meta.SessionID)
	}
	// Event 6: done
	if events[6].Type != "done" {
		t.Errorf("event 6: expected done, got %s", events[6].Type)
	}
}

func TestBuildGeminiStreamArgs_Basic(t *testing.T) {
	req := ChatRequest{
		Prompt:  "say hello",
		WorkDir: "/home/user/project",
		Model:   "gemini-3-pro-preview",
	}
	args := buildGeminiStreamArgs(req)

	expected := []string{"--prompt", "say hello", "--output-format", "stream-json", "--yolo", "--include-directories", "/home/user/project", "--model", "gemini-3-pro-preview"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
		return
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg %d: expected %q, got %q", i, v, args[i])
		}
	}
}

func TestBuildGeminiStreamArgs_ResumeSession(t *testing.T) {
	req := ChatRequest{
		Prompt:    "continue",
		SessionID: "ses_abc123",
		Resume:    true,
		WorkDir:   "/home/user/project",
	}
	args := buildGeminiStreamArgs(req)

	// Should contain --resume latest
	found := false
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) && args[i+1] == "latest" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected --resume latest in args when Resume=true")
	}
}

func TestBuildGeminiStreamArgs_NoResumeWithoutFlag(t *testing.T) {
	req := ChatRequest{
		Prompt:    "new session",
		SessionID: "some-id",
		Resume:    false,
	}
	args := buildGeminiStreamArgs(req)

	for _, a := range args {
		if a == "--resume" {
			t.Error("should not contain --resume when Resume=false")
		}
	}
}

func TestNormalizeGeminiToolName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Existing mappings
		{"read_file", "Read"},
		{"write_file", "Write"},
		{"edit_file", "Edit"},
		{"shell", "Bash"},
		{"run_command", "Bash"},
		{"list_files", "LS"},
		{"search_files", "Grep"},
		// New mappings
		{"replace", "Edit"},
		{"list_directory", "LS"},
		{"glob", "Glob"},
		{"web_fetch", "WebFetch"},
		{"google_web_search", "WebSearch"},
		{"invoke_agent", "Agent"},
		{"enter_plan_mode", "EnterPlanMode"},
		{"activate_skill", "Skill"},
		{"save_memory", "save_memory"},
		// Unknown tool → passthrough
		{"custom_tool", "custom_tool"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeToolName(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeToolName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeGeminiInput_FieldRemapping(t *testing.T) {
	// filePath → file_path
	input1 := json.RawMessage(`{"filePath":"/tmp/test.go"}`)
	norm1, err := normalizeToolInput(input1, map[string]string{"dirPath": "path"})
	if err != nil {
		t.Fatalf("normalizeToolInput failed: %v", err)
		return
	}
	result1 := string(norm1)
	var parsed1 map[string]any
	if unmarshalErr := json.Unmarshal([]byte(result1), &parsed1); unmarshalErr != nil {
		t.Fatalf("failed to parse result: %v", unmarshalErr)
		return
	}
	if _, exists := parsed1["filePath"]; exists {
		t.Error("filePath should be removed")
	}
	if parsed1["file_path"] != "/tmp/test.go" {
		t.Errorf("expected file_path=/tmp/test.go, got %v", parsed1["file_path"])
	}

	// dirPath → path
	input2 := json.RawMessage(`{"dirPath":"./src"}`)
	norm2, err := normalizeToolInput(input2, map[string]string{"dirPath": "path"})
	if err != nil {
		t.Fatalf("normalizeToolInput failed: %v", err)
		return
	}
	result2 := string(norm2)
	var parsed2 map[string]any
	if unmarshalErr := json.Unmarshal([]byte(result2), &parsed2); unmarshalErr != nil {
		t.Fatalf("failed to parse result: %v", unmarshalErr)
		return
	}
	if _, exists := parsed2["dirPath"]; exists {
		t.Error("dirPath should be removed")
	}
	if parsed2["path"] != "./src" {
		t.Errorf("expected path=./src, got %v", parsed2["path"])
	}

	// Combined: filePath + dirPath
	input3 := json.RawMessage(`{"filePath":"main.go","dirPath":"./src"}`)
	norm3, err := normalizeToolInput(input3, map[string]string{"dirPath": "path"})
	if err != nil {
		t.Fatalf("normalizeToolInput failed: %v", err)
		return
	}
	result3 := string(norm3)
	var parsed3 map[string]any
	if err := json.Unmarshal([]byte(result3), &parsed3); err != nil {
		t.Fatalf("failed to parse result: %v", err)
		return
	}
	if parsed3["file_path"] != "main.go" {
		t.Errorf("expected file_path=main.go, got %v", parsed3["file_path"])
	}
	if parsed3["path"] != "./src" {
		t.Errorf("expected path=./src, got %v", parsed3["path"])
	}
}

func TestNormalizeGeminiInput_UnparseableJSON(t *testing.T) {
	bad := json.RawMessage(`not valid json`)
	_, err := normalizeToolInput(bad, map[string]string{"dirPath": "path"})
	if err == nil {
		t.Error("expected error for unparseable JSON")
	}
}

func TestNormalizeGeminiInput_AlreadyCanonical(t *testing.T) {
	input := json.RawMessage(`{"file_path":"/tmp/test.go"}`)
	norm, err := normalizeToolInput(input, map[string]string{"dirPath": "path"})
	if err != nil {
		t.Fatalf("normalizeToolInput failed: %v", err)
		return
	}
	result := string(norm)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
		return
	}
	if parsed["file_path"] != "/tmp/test.go" {
		t.Errorf("expected file_path=/tmp/test.go, got %v", parsed["file_path"])
	}
}

func TestGeminiStream_ParseLine_ToolUse_NewTools(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		expectedTool string
		checkInput   func(t *testing.T, input map[string]any)
	}{
		{
			name:         "glob",
			line:         `{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"glob","tool_id":"call_glob","parameters":{"pattern":"**/*.go"}}`,
			expectedTool: "Glob",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["pattern"] != "**/*.go" {
					t.Errorf("expected pattern='**/*.go', got %v", input["pattern"])
				}
			},
		},
		{
			name:         "web_fetch",
			line:         `{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"web_fetch","tool_id":"call_wf","parameters":{"url":"https://example.com"}}`,
			expectedTool: "WebFetch",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["url"] != "https://example.com" {
					t.Errorf("expected url='https://example.com', got %v", input["url"])
				}
			},
		},
		{
			name:         "google_web_search",
			line:         `{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"google_web_search","tool_id":"call_ws","parameters":{"query":"golang testing"}}`,
			expectedTool: "WebSearch",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["query"] != "golang testing" {
					t.Errorf("expected query='golang testing', got %v", input["query"])
				}
			},
		},
		{
			name:         "invoke_agent",
			line:         `{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"invoke_agent","tool_id":"call_agent","parameters":{"description":"research task"}}`,
			expectedTool: "Agent",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["description"] != "research task" {
					t.Errorf("expected description='research task', got %v", input["description"])
				}
			},
		},
		{
			name:         "enter_plan_mode",
			line:         `{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"enter_plan_mode","tool_id":"call_plan","parameters":{}}`,
			expectedTool: "EnterPlanMode",
		},
		{
			name:         "activate_skill",
			line:         `{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"activate_skill","tool_id":"call_skill","parameters":{"skill":"commit"}}`,
			expectedTool: "Skill",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["skill"] != "commit" {
					t.Errorf("expected skill='commit', got %v", input["skill"])
				}
			},
		},
		{
			name:         "save_memory",
			line:         `{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"save_memory","tool_id":"call_mem","parameters":{"key":"test","value":"data"}}`,
			expectedTool: "save_memory",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["key"] != "test" {
					t.Errorf("expected key='test', got %v", input["key"])
				}
			},
		},
		{
			name:         "replace_as_edit",
			line:         `{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"replace","tool_id":"call_replace","parameters":{"filePath":"main.go","oldString":"old","newString":"new"}}`,
			expectedTool: "Edit",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["file_path"] != "main.go" {
					t.Errorf("expected file_path='main.go', got %v", input["file_path"])
				}
				if _, ok := input["filePath"]; ok {
					t.Error("filePath should be normalized to file_path")
				}
			},
		},
		{
			name:         "list_directory_as_ls",
			line:         `{"type":"tool_use","timestamp":"2026-04-25T10:00:02.000Z","tool_name":"list_directory","tool_id":"call_ls","parameters":{"dirPath":"./src"}}`,
			expectedTool: "LS",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["path"] != "./src" {
					t.Errorf("expected path='./src', got %v", input["path"])
				}
				if _, ok := input["dirPath"]; ok {
					t.Error("dirPath should be normalized to path")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := parseGeminiLine(tt.line)
			if len(events) != 1 {
				t.Fatalf("expected 1 event, got %d", len(events))
				return
			}
			if events[0].Tool == nil {
				t.Fatal("expected tool call, got nil")
				return
			}
			if events[0].Tool.Name != tt.expectedTool {
				t.Errorf("expected tool name %q, got %q", tt.expectedTool, events[0].Tool.Name)
			}
			if tt.checkInput != nil {
				var input map[string]any
				if err := json.Unmarshal([]byte(events[0].Tool.Input), &input); err != nil {
					t.Fatalf("failed to parse tool input: %v", err)
					return
				}
				tt.checkInput(t, input)
			}
		})
	}
}

func TestBuildGeminiStreamArgs_Minimal(t *testing.T) {
	req := ChatRequest{
		Prompt: "hello",
	}
	args := buildGeminiStreamArgs(req)

	expected := []string{"--prompt", "hello", "--output-format", "stream-json", "--yolo"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
		return
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg %d: expected %q, got %q", i, v, args[i])
		}
	}
}

func TestGeminiStream_GetCapturedSessionID_AlwaysEmpty(t *testing.T) {
	// Gemini always returns "" for GetCapturedSessionID since it uses --resume latest
	parser := &GeminiStreamParser{}
	assert.Equal(t, "", parser.GetCapturedSessionID())

	// Even after parsing an init event with session_id
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"init","session_id":"sess-123","model":"gemini-2.5"}`, ch)
	assert.Equal(t, "", parser.GetCapturedSessionID(), "Gemini GetCapturedSessionID should always return empty")
}

func TestGeminiStream_ErrorEmptyMessage(t *testing.T) {
	// Error event with empty message should not produce any event
	parser := &GeminiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"error","severity":"error","message":""}`, ch)

	select {
	case evt := <-ch:
		t.Errorf("error with empty message should be skipped, got %+v", evt)
	default:
		// expected
	}
}

func TestGeminiStream_ToolResultEmptyToolID(t *testing.T) {
	// tool_result with empty tool_id should be skipped
	parser := &GeminiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"tool_result","tool_id":"","output":"result","status":"success"}`, ch)

	select {
	case evt := <-ch:
		t.Errorf("tool_result with empty tool_id should be skipped, got %+v", evt)
	default:
		// expected
	}
}

func TestBuildGeminiStreamArgs_WithSystemPrompt(t *testing.T) {
	req := ChatRequest{
		Prompt:       "hello",
		SystemPrompt: "you are helpful",
	}
	args := buildGeminiStreamArgs(req)

	// Gemini injects system prompt into the user prompt
	found := false
	for _, arg := range args {
		if arg == "[System Instructions: you are helpful]\n\nhello" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected system prompt injection in args, got %v", args)
	}
}

func TestBuildGeminiStreamArgs_NoSystemPrompt(t *testing.T) {
	req := ChatRequest{
		Prompt: "hello",
	}
	args := buildGeminiStreamArgs(req)

	// Without system prompt, the prompt is passed as-is
	for _, arg := range args {
		if arg == "hello" {
			return
		}
	}
	t.Errorf("expected plain prompt 'hello' in args, got %v", args)
}
